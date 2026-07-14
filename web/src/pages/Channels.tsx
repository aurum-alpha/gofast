import { useEffect, useMemo, useState } from 'react'

type Channel = {
  provider: string
  id: string
  normalized_id: string
  name: string
  group: string
  number: number
  offset_number: number
  stream_url: string
  logo_url?: string
  classification?: string
  filter_reason?: string
  excluded: boolean
}

type ChannelsResponse = {
  channels: Channel[]
}

function exportLabel(ch: Channel): string {
  if (ch.excluded) {
    return ch.filter_reason ? `filtered · ${ch.filter_reason}` : 'filtered'
  }
  return 'exported'
}

function displayNumber(n: number): string {
  return n > 0 ? String(n) : '—'
}

export function ChannelsPage() {
  const [data, setData] = useState<ChannelsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [providerFilter, setProviderFilter] = useState('all')
  const [q, setQ] = useState('')

  useEffect(() => {
    let cancelled = false
    fetch('/api/channels')
      .then(async (res) => {
        if (!res.ok) {
          throw new Error(`${res.status} ${res.statusText}`)
        }
        return res.json() as Promise<ChannelsResponse>
      })
      .then((body) => {
        if (!cancelled) {
          setData(body)
          setError(null)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err))
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

  const providers = useMemo(() => {
    if (!data) return [] as string[]
    return [...new Set(data.channels.map((c) => c.provider))].sort()
  }, [data])

  const rows = useMemo(() => {
    if (!data) return [] as Channel[]
    const needle = q.trim().toLowerCase()
    return data.channels.filter((ch) => {
      if (providerFilter !== 'all' && ch.provider !== providerFilter) {
        return false
      }
      if (!needle) return true
      return (
        ch.name.toLowerCase().includes(needle) ||
        ch.normalized_id.toLowerCase().includes(needle) ||
        ch.group.toLowerCase().includes(needle) ||
        String(ch.number).includes(needle) ||
        String(ch.offset_number).includes(needle)
      )
    })
  }, [data, providerFilter, q])

  return (
    <>
      <h1>Channels</h1>
      <p className="lead">
        Live lineup from the last successful refresh. Classification badges
        arrive with the stream classifier; until then Class shows —.
      </p>

      {error && (
        <div className="empty-panel" role="alert">
          <p>Failed to load channels: {error}</p>
        </div>
      )}

      {!error && !data && (
        <div className="empty-panel" role="status">
          <p>Loading…</p>
        </div>
      )}

      {data && (
        <>
          <div className="toolbar">
            <label>
              Provider{' '}
              <select
                value={providerFilter}
                onChange={(e) => setProviderFilter(e.target.value)}
              >
                <option value="all">all</option>
                {providers.map((p) => (
                  <option key={p} value={p}>
                    {p}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Search{' '}
              <input
                type="search"
                value={q}
                onChange={(e) => setQ(e.target.value)}
                placeholder="name, id, group, number…"
              />
            </label>
            <span className="meta">
              {rows.length} of {data.channels.length}
            </span>
          </div>

          <div className="table-wrap">
            <table className="channels">
              <thead>
                <tr>
                  <th scope="col">#</th>
                  <th scope="col">Prov #</th>
                  <th scope="col">Name</th>
                  <th scope="col">Provider</th>
                  <th scope="col">Group</th>
                  <th scope="col">Class</th>
                  <th scope="col">Export</th>
                </tr>
              </thead>
              <tbody>
                {rows.length === 0 ? (
                  <tr className="empty">
                    <td colSpan={7}>
                      No channels yet — wait for a successful provider refresh
                      (e.g. LG), or clear filters.
                    </td>
                  </tr>
                ) : (
                  rows.map((ch) => (
                    <tr
                      key={`${ch.provider}:${ch.normalized_id}`}
                      className={ch.excluded ? 'excluded' : undefined}
                    >
                      <td>{displayNumber(ch.offset_number)}</td>
                      <td>{displayNumber(ch.number)}</td>
                      <td>
                        {ch.name}
                        <div className="subtle">
                          <code>{ch.normalized_id}</code>
                        </div>
                      </td>
                      <td>
                        <code>{ch.provider}</code>
                      </td>
                      <td>{ch.group || '—'}</td>
                      <td>{ch.classification || '—'}</td>
                      <td>{exportLabel(ch)}</td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </>
      )}
    </>
  )
}
