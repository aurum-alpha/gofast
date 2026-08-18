import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

type ClientAccessEvent = {
  file: string
  at: string
  ip: string
  status: number
  user_agent?: string
}

type ClientAccessResponse = {
  summary: { file: string }[]
  recent: ClientAccessEvent[]
}

type SortKey = 'at' | 'file' | 'ip' | 'status'
type SortDir = 'asc' | 'desc'

type Sort = { key: SortKey; dir: SortDir }

function formatWhen(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return '—'
  return date.toLocaleString()
}

function SortTh({
  label,
  col,
  sort,
  onSort,
}: {
  label: string
  col: SortKey
  sort: Sort
  onSort: (key: SortKey) => void
}) {
  const active = sort.key === col
  const ariaSort = active ? (sort.dir === 'asc' ? 'ascending' : 'descending') : 'none'
  return (
    <th scope="col" aria-sort={ariaSort}>
      <button
        type="button"
        className={`sort-th${active ? ' sort-th-active' : ''}`}
        onClick={() => onSort(col)}
      >
        {label}
        <span className="sort-indicator" aria-hidden="true">
          {active ? (sort.dir === 'asc' ? '▲' : '▼') : ''}
        </span>
      </button>
    </th>
  )
}

function compareEvents(a: ClientAccessEvent, b: ClientAccessEvent, sort: Sort): number {
  let cmp = 0
  switch (sort.key) {
    case 'at':
      cmp = Date.parse(a.at) - Date.parse(b.at)
      break
    case 'file':
      cmp = a.file.localeCompare(b.file)
      break
    case 'ip':
      cmp = a.ip.localeCompare(b.ip)
      break
    case 'status':
      cmp = a.status - b.status
      break
  }
  if (cmp === 0) {
    cmp = Date.parse(b.at) - Date.parse(a.at)
  }
  return sort.dir === 'asc' ? cmp : -cmp
}

export function AccessPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [data, setData] = useState<ClientAccessResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  const file = searchParams.get('file') ?? ''
  const ip = searchParams.get('ip') ?? ''
  const status = searchParams.get('status') ?? ''
  const sortKey = (searchParams.get('sort') as SortKey | null) ?? 'at'
  const sortDir = (searchParams.get('dir') as SortDir | null) ?? 'desc'
  const sort: Sort = {
    key: ['at', 'file', 'ip', 'status'].includes(sortKey) ? sortKey : 'at',
    dir: sortDir === 'asc' ? 'asc' : 'desc',
  }

  useEffect(() => {
    let cancelled = false
    const q = new URLSearchParams()
    q.set('limit', '1000')
    if (file) q.set('file', file)
    if (ip.trim()) q.set('ip', ip.trim())
    if (status === '200' || status === '304') q.set('status', status)

    fetch(`/api/client-access?${q}`)
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        return res.json() as Promise<ClientAccessResponse>
      })
      .then((body) => {
        if (!cancelled) {
          setData(body)
          setError(null)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      })
    return () => {
      cancelled = true
    }
  }, [file, ip, status])

  const fileOptions = useMemo(() => {
    const names = new Set<string>()
    for (const s of data?.summary ?? []) names.add(s.file)
    for (const e of data?.recent ?? []) names.add(e.file)
    return [...names].sort((a, b) => a.localeCompare(b))
  }, [data])

  const rows = useMemo(() => {
    const list = [...(data?.recent ?? [])]
    list.sort((a, b) => compareEvents(a, b, sort))
    return list
  }, [data, sort])

  function patch(next: Record<string, string | null>) {
    const p = new URLSearchParams(searchParams)
    for (const [k, v] of Object.entries(next)) {
      if (v == null || v === '') p.delete(k)
      else p.set(k, v)
    }
    setSearchParams(p, { replace: true })
  }

  function onSort(key: SortKey) {
    if (sort.key === key) {
      patch({ sort: key, dir: sort.dir === 'asc' ? 'desc' : 'asc' })
      return
    }
    // Time defaults newest-first; other columns start ascending.
    patch({ sort: key, dir: key === 'at' ? 'desc' : 'asc' })
  }

  function resetFilters() {
    setSearchParams({}, { replace: true })
  }

  const filtersDirty = Boolean(file || ip.trim() || status || sort.key !== 'at' || sort.dir !== 'desc')

  return (
    <>
      <h1>Access</h1>
      <p className="lead">
        Playlist and EPG pull history (last 30 days). Summary lives on{' '}
        <Link to="/status">Status</Link>.
      </p>

      {error ? (
        <div className="empty-panel" role="alert">
          Failed to load access log: {error}
        </div>
      ) : null}

      <div className="filters access-filters">
        <label>
          File
          <select
            value={file}
            onChange={(e) => patch({ file: e.target.value || null })}
          >
            <option value="">all</option>
            {fileOptions.map((f) => (
              <option key={f} value={f}>
                {f}
              </option>
            ))}
          </select>
        </label>
        <label>
          IP
          <input
            type="search"
            value={ip}
            placeholder="contains…"
            onChange={(e) => patch({ ip: e.target.value || null })}
          />
        </label>
        <label>
          Status
          <select
            value={status}
            onChange={(e) => patch({ status: e.target.value || null })}
          >
            <option value="">all</option>
            <option value="200">200</option>
            <option value="304">304</option>
          </select>
        </label>
        <button type="button" onClick={resetFilters} disabled={!filtersDirty}>
          Reset
        </button>
        <span className="meta">
          {data ? `${rows.length.toLocaleString()} events` : 'Loading…'}
        </span>
      </div>

      {!error && !data ? (
        <div className="empty-panel" role="status">
          Loading…
        </div>
      ) : null}

      {data && rows.length === 0 ? (
        <div className="empty-panel" role="status">
          No pulls match the current filters.
        </div>
      ) : null}

      {rows.length > 0 ? (
        <div className="table-wrap">
          <table className="channels client-access-table">
            <thead>
              <tr>
                <SortTh label="When" col="at" sort={sort} onSort={onSort} />
                <SortTh label="File" col="file" sort={sort} onSort={onSort} />
                <SortTh label="IP" col="ip" sort={sort} onSort={onSort} />
                <SortTh label="Status" col="status" sort={sort} onSort={onSort} />
              </tr>
            </thead>
            <tbody>
              {rows.map((ev, i) => (
                <tr
                  key={`${ev.at}-${ev.file}-${ev.ip}-${ev.status}-${i}`}
                  title={ev.user_agent?.trim() || undefined}
                >
                  <td>{formatWhen(ev.at)}</td>
                  <td>
                    <code>{ev.file}</code>
                  </td>
                  <td>
                    <code>{ev.ip}</code>
                  </td>
                  <td>{ev.status}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      ) : null}
    </>
  )
}
