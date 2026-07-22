import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  channelDetailPath,
  classBadge,
  displayNumber,
  exportBadge,
  exportKind,
} from '../lib/channel'
import type { Channel } from '../lib/channel'

type ChannelsResponse = {
  channels: Channel[]
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
        ch.id.toLowerCase().includes(needle) ||
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
        Live lineup from the last successful refresh. Click a row for export
        status, reasons, URLs, and identity. Class is probed at refresh (NATIVE /
        BEACON / DRM).
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
                  rows.map((ch) => {
                    const cls = classBadge(ch.classification)
                    const exp = exportBadge(exportKind(ch))
                    return (
                      <tr
                        key={`${ch.provider}:${ch.normalized_id}`}
                        className={ch.excluded ? 'excluded' : undefined}
                      >
                        <td>{displayNumber(ch.offset_number)}</td>
                        <td>{displayNumber(ch.number)}</td>
                        <td>
                          <Link
                            to={channelDetailPath(ch)}
                            className="channel-name channel-link"
                          >
                            {ch.logo_url && (
                              <img
                                className="channel-logo"
                                src={ch.logo_url}
                                alt=""
                                loading="lazy"
                                onError={(e) => {
                                  e.currentTarget.style.display = 'none'
                                }}
                              />
                            )}
                            <span>{ch.name}</span>
                          </Link>
                        </td>
                        <td>
                          <code>{ch.provider}</code>
                        </td>
                        <td>{ch.group || '—'}</td>
                        <td>
                          <span className={`badge badge-${cls.kind}`}>{cls.label}</span>
                        </td>
                        <td>
                          <span className={`badge ${exp.className}`}>{exp.label}</span>
                        </td>
                      </tr>
                    )
                  })
                )}
              </tbody>
            </table>
          </div>
        </>
      )}
    </>
  )
}
