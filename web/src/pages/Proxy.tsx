import { useEffect, useMemo, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'

type DemuxStableSession = {
  provider?: string
  channel_id?: string
  started_at?: string
  bytes_out?: number
  bytes_per_sec?: number
  pid?: number
  state?: string
}

type ProxySnapshot = {
  at?: string
  proxy_id?: string
  active_sessions?: number
  active_seg_tokens?: number
  stream_opens?: number
  stream_302s?: number
  playlist_ok?: number
  playlist_fail?: number
  seg_ok?: number
  seg_fail?: number
  seg_bytes?: number
  events_dropped?: number
  demux_stable_active?: number
  demux_stable_max?: number
  demux_stable_bytes_total?: number
  demux_stable_bytes_per_sec?: number
  demux_stable_starts?: number
  demux_stable_fails?: number
  demux_stable_sessions?: DemuxStableSession[]
}

type ProxyEvent = {
  kind: string
  at?: string
  provider?: string
  channel_id?: string
  reason?: string
  message?: string
  status?: number
  duration_ms?: number
}

type ProxyStatusResponse = {
  snapshot?: ProxySnapshot | null
  heartbeat?: string
  heartbeat_count?: number
  stale?: boolean
}

type SortKey = 'at' | 'kind' | 'provider' | 'reason' | 'status' | 'duration_ms'
type SortDir = 'asc' | 'desc'
type Sort = { key: SortKey; dir: SortDir }

const KIND_OPTIONS = [
  '',
  'playlist_ok',
  'playlist_fail',
  'origin_miss',
  'seg_ok',
  'seg_fail',
  'stream_open',
  'stream_302',
  'demux_stable_open',
  'demux_stable_close',
  'demux_stable_fail',
  'demux_stable_stall',
] as const

function formatWhen(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return '—'
  return date.toLocaleString()
}

function formatAge(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return '—'
  const sec = Math.max(0, Math.round((Date.now() - date.getTime()) / 1000))
  if (sec < 60) return `${sec}s ago`
  const min = Math.round(sec / 60)
  if (min < 60) return `${min}m ago`
  const hr = Math.round(min / 60)
  return `${hr}h ago`
}

function formatBytes(n?: number): string {
  if (n == null || n <= 0) return '0'
  if (n < 1024) return String(n)
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`
  return `${(n / (1024 * 1024)).toFixed(1)} MiB`
}

function Metric({
  label,
  value,
  warn,
  title,
}: {
  label: string
  value: string | number
  warn?: boolean
  title?: string
}) {
  return (
    <div className={`stat${warn ? ' stat-warn' : ''}`} title={title}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
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

function compareEvents(a: ProxyEvent, b: ProxyEvent, sort: Sort): number {
  let cmp = 0
  switch (sort.key) {
    case 'at':
      cmp = Date.parse(a.at ?? '') - Date.parse(b.at ?? '')
      break
    case 'kind':
      cmp = a.kind.localeCompare(b.kind)
      break
    case 'provider':
      cmp = `${a.provider ?? ''}/${a.channel_id ?? ''}`.localeCompare(
        `${b.provider ?? ''}/${b.channel_id ?? ''}`,
      )
      break
    case 'reason':
      cmp = (a.reason ?? '').localeCompare(b.reason ?? '')
      break
    case 'status':
      cmp = (a.status ?? 0) - (b.status ?? 0)
      break
    case 'duration_ms':
      cmp = (a.duration_ms ?? 0) - (b.duration_ms ?? 0)
      break
  }
  if (cmp === 0) {
    cmp = Date.parse(b.at ?? '') - Date.parse(a.at ?? '')
  }
  return sort.dir === 'asc' ? cmp : -cmp
}

export function ProxyPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [status, setStatus] = useState<ProxyStatusResponse | null>(null)
  const [events, setEvents] = useState<ProxyEvent[] | null>(null)
  const [error, setError] = useState<string | null>(null)

  const kind = searchParams.get('kind') ?? ''
  const provider = searchParams.get('provider') ?? ''
  const failures = searchParams.get('failures') === '1'
  const sortKey = (searchParams.get('sort') as SortKey | null) ?? 'at'
  const sortDir = (searchParams.get('dir') as SortDir | null) ?? 'desc'
  const sort: Sort = {
    key: ['at', 'kind', 'provider', 'reason', 'status', 'duration_ms'].includes(sortKey)
      ? sortKey
      : 'at',
    dir: sortDir === 'asc' ? 'asc' : 'desc',
  }

  useEffect(() => {
    let cancelled = false
    let timer: number | undefined

    const schedule = (delay: number) => {
      // No polling while the tab is hidden; visibilitychange resumes it.
      if (cancelled || document.hidden) return
      timer = window.setTimeout(load, delay)
    }

    const load = () => {
      if (cancelled || document.hidden) return
      if (timer !== undefined) {
        window.clearTimeout(timer)
        timer = undefined
      }
      const q = new URLSearchParams()
      q.set('limit', '1000')
      if (kind) q.set('kind', kind)
      if (provider.trim()) q.set('provider', provider.trim())
      if (failures) q.set('failures', '1')

      Promise.all([
        fetch('/api/proxy/status').then(async (res) => {
          if (!res.ok) throw new Error(`status ${res.status}`)
          return res.json() as Promise<ProxyStatusResponse>
        }),
        fetch(`/api/proxy/events?${q}`).then(async (res) => {
          if (!res.ok) throw new Error(`events ${res.status}`)
          return res.json() as Promise<{ events: ProxyEvent[] }>
        }),
      ])
        .then(([st, ev]) => {
          if (cancelled) return
          setStatus(st)
          setEvents(ev.events ?? [])
          setError(null)
          schedule(2000)
        })
        .catch((err: unknown) => {
          if (cancelled) return
          setError(err instanceof Error ? err.message : String(err))
          schedule(5000)
        })
    }

    const onVisibility = () => {
      if (document.hidden) {
        if (timer !== undefined) {
          window.clearTimeout(timer)
          timer = undefined
        }
      } else {
        load()
      }
    }

    document.addEventListener('visibilitychange', onVisibility)
    load()
    return () => {
      cancelled = true
      document.removeEventListener('visibilitychange', onVisibility)
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [kind, provider, failures])

  const providerOptions = useMemo(() => {
    const names = new Set<string>()
    for (const e of events ?? []) {
      if (e.provider) names.add(e.provider)
    }
    return [...names].sort((a, b) => a.localeCompare(b))
  }, [events])

  const rows = useMemo(() => {
    const list = [...(events ?? [])]
    list.sort((a, b) => compareEvents(a, b, sort))
    return list
  }, [events, sort])

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
    patch({ sort: key, dir: key === 'at' ? 'desc' : 'asc' })
  }

  function resetFilters() {
    setSearchParams({}, { replace: true })
  }

  const filtersDirty = Boolean(
    kind ||
      provider.trim() ||
      failures ||
      sort.key !== 'at' ||
      sort.dir !== 'desc',
  )
  const empty =
    !status?.snapshot &&
    (status?.heartbeat_count ?? 0) === 0 &&
    (events?.length ?? 0) === 0
  const snap = status?.snapshot

  return (
    <>
      <h1>Proxy</h1>
      <p className="lead">
        FASTProxy activity glass (gen is source of truth). Glance metrics on{' '}
        <Link to="/status">Status</Link>. Playlist/origin failures update channel
        health with source <code>playback</code>.
      </p>

      {error ? (
        <div className="empty-panel" role="alert">
          Failed to load proxy activity: {error}
        </div>
      ) : null}

      {empty ? (
        <div className="empty-panel" role="status">
          <p>
            No proxy heartbeat yet. Enable compose <code>--profile proxy</code>{' '}
            and set <code>proxy_base_url</code> / <code>FASTPROXY_GEN_URL</code>.
          </p>
        </div>
      ) : (
        <>
          <div className="stat-grid">
            <Metric
              label="Heartbeat age"
              value={formatAge(status?.heartbeat)}
              warn={Boolean(status?.stale)}
              title={
                status?.stale
                  ? 'Snapshot older than 2 minutes'
                  : formatWhen(status?.heartbeat)
              }
            />
            <Metric
              label="Heartbeats"
              value={(status?.heartbeat_count ?? 0).toLocaleString()}
            />
            <Metric label="Proxy ID" value={snap?.proxy_id || '—'} />
            <Metric label="Sessions" value={snap?.active_sessions ?? 0} />
            <Metric label="Seg tokens" value={snap?.active_seg_tokens ?? 0} />
            <Metric label="Opens" value={snap?.stream_opens ?? 0} />
            <Metric label="302s" value={snap?.stream_302s ?? 0} />
            <Metric label="Playlist OK" value={snap?.playlist_ok ?? 0} />
            <Metric
              label="Playlist fail"
              value={snap?.playlist_fail ?? 0}
              warn={(snap?.playlist_fail ?? 0) > 0}
            />
            <Metric label="Seg OK" value={snap?.seg_ok ?? 0} />
            <Metric
              label="Seg fail"
              value={snap?.seg_fail ?? 0}
              warn={(snap?.seg_fail ?? 0) > 0}
            />
            <Metric label="Bytes" value={formatBytes(snap?.seg_bytes)} />
            <Metric
              label="Demux-stable active"
              value={`${snap?.demux_stable_active ?? 0}/${snap?.demux_stable_max ?? '—'}`}
              title="Class B ffmpeg encode slots in use"
            />
            <Metric
              label="Demux-stable starts"
              value={snap?.demux_stable_starts ?? 0}
            />
            <Metric
              label="Demux-stable fail"
              value={snap?.demux_stable_fails ?? 0}
              warn={(snap?.demux_stable_fails ?? 0) > 0}
            />
            <Metric
              label="Demux-stable bytes"
              value={formatBytes(snap?.demux_stable_bytes_total)}
            />
            <Metric
              label="Demux-stable rate"
              value={
                snap?.demux_stable_bytes_per_sec != null &&
                snap.demux_stable_bytes_per_sec > 0
                  ? `${formatBytes(Math.round(snap.demux_stable_bytes_per_sec))}/s`
                  : '—'
              }
              title="Aggregate output rate across active Class B encodes"
            />
            <Metric
              label="Dropped"
              value={snap?.events_dropped ?? 0}
              warn={(snap?.events_dropped ?? 0) > 0}
            />
          </div>

          {(snap?.demux_stable_sessions?.length ?? 0) > 0 ? (
            <section className="panel" style={{ marginTop: '1rem' }}>
              <h2>Active demux-stable encodes</h2>
              <table className="data-table">
                <thead>
                  <tr>
                    <th>Provider</th>
                    <th>Channel</th>
                    <th>State</th>
                    <th>Bytes</th>
                    <th>Rate</th>
                    <th>PID</th>
                  </tr>
                </thead>
                <tbody>
                  {snap!.demux_stable_sessions!.map((s) => (
                    <tr key={`${s.provider}/${s.channel_id}/${s.pid}`}>
                      <td>{s.provider || '—'}</td>
                      <td>{s.channel_id || '—'}</td>
                      <td>{s.state || '—'}</td>
                      <td>{formatBytes(s.bytes_out)}</td>
                      <td>
                        {s.bytes_per_sec != null && s.bytes_per_sec > 0
                          ? `${formatBytes(Math.round(s.bytes_per_sec))}/s`
                          : '—'}
                      </td>
                      <td>{s.pid ?? '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </section>
          ) : null}

          <div className="filters access-filters">
            <label>
              Kind
              <select
                value={kind}
                onChange={(e) => patch({ kind: e.target.value || null })}
              >
                <option value="">all</option>
                {KIND_OPTIONS.filter(Boolean).map((k) => (
                  <option key={k} value={k}>
                    {k}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Provider
              <select
                value={provider}
                onChange={(e) => patch({ provider: e.target.value || null })}
              >
                <option value="">all</option>
                {providerOptions.map((p) => (
                  <option key={p} value={p}>
                    {p}
                  </option>
                ))}
              </select>
            </label>
            <label>
              View
              <select
                value={failures ? 'failures' : 'all'}
                onChange={(e) =>
                  patch({
                    failures: e.target.value === 'failures' ? '1' : null,
                    kind: e.target.value === 'failures' ? null : kind || null,
                  })
                }
              >
                <option value="all">all events</option>
                <option value="failures">failures only</option>
              </select>
            </label>
            <button type="button" onClick={resetFilters} disabled={!filtersDirty}>
              Reset
            </button>
            <span className="meta">
              {events ? `${rows.length.toLocaleString()} events` : 'Loading…'}
            </span>
          </div>

          {events && rows.length === 0 ? (
            <div className="empty-panel" role="status">
              No events match the current filters.
            </div>
          ) : null}

          {rows.length > 0 ? (
            <div className="table-wrap">
              <table className="channels client-access-table">
                <thead>
                  <tr>
                    <SortTh label="When" col="at" sort={sort} onSort={onSort} />
                    <SortTh label="Kind" col="kind" sort={sort} onSort={onSort} />
                    <SortTh
                      label="Channel"
                      col="provider"
                      sort={sort}
                      onSort={onSort}
                    />
                    <SortTh
                      label="Reason"
                      col="reason"
                      sort={sort}
                      onSort={onSort}
                    />
                    <SortTh
                      label="Status"
                      col="status"
                      sort={sort}
                      onSort={onSort}
                    />
                    <SortTh
                      label="ms"
                      col="duration_ms"
                      sort={sort}
                      onSort={onSort}
                    />
                    <th>Message</th>
                  </tr>
                </thead>
                <tbody>
                  {rows.map((ev, i) => (
                    <tr key={`${ev.at}-${ev.kind}-${ev.channel_id}-${i}`}>
                      <td>{formatWhen(ev.at)}</td>
                      <td>
                        <code>{ev.kind}</code>
                      </td>
                      <td>
                        <code>
                          {ev.provider || '—'}/{ev.channel_id || '—'}
                        </code>
                      </td>
                      <td>{ev.reason || '—'}</td>
                      <td>{ev.status ? ev.status : '—'}</td>
                      <td>{ev.duration_ms != null && ev.duration_ms > 0 ? ev.duration_ms : '—'}</td>
                      <td title={ev.message}>{ev.message || '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : null}
        </>
      )}
    </>
  )
}
