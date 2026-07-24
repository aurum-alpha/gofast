import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  channelDetailPath,
  classBadge,
  CLASS_FILTERS,
  canonicalClassification,
  displayNumber,
  formatHealthWhen,
  healthBadge,
  HEALTH_FILTERS,
  healthStatus,
  lineupBadge,
  lineupStatus,
  STATUS_FILTERS,
} from '../lib/channel'
import type { Channel, HealthFilterValue, LineupStatusKind } from '../lib/channel'

type ChannelsResponse = {
  channels: Channel[]
}

const EMPTY_GROUP = '__none__'

function groupKey(group: string): string {
  return group || EMPTY_GROUP
}

function groupLabel(key: string): string {
  return key === EMPTY_GROUP ? '(none)' : key
}

export function ChannelsPage() {
  const [data, setData] = useState<ChannelsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [providerFilter, setProviderFilter] = useState('all')
  const [groupFilter, setGroupFilter] = useState('all')
  const [classFilter, setClassFilter] = useState('all')
  const [statusFilter, setStatusFilter] = useState('all')
  const [healthFilter, setHealthFilter] = useState('all')
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

  const groups = useMemo(() => {
    if (!data) return [] as string[]
    const set = new Set<string>()
    for (const c of data.channels) {
      set.add(groupKey(c.group))
    }
    return [...set].sort((a, b) => {
      if (a === EMPTY_GROUP) return 1
      if (b === EMPTY_GROUP) return -1
      return a.localeCompare(b)
    })
  }, [data])

  const rows = useMemo(() => {
    if (!data) return [] as Channel[]
    const needle = q.trim().toLowerCase()
    return data.channels.filter((ch) => {
      if (providerFilter !== 'all' && ch.provider !== providerFilter) {
        return false
      }
      if (groupFilter !== 'all' && groupKey(ch.group) !== groupFilter) {
        return false
      }
      if (classFilter !== 'all') {
        if (canonicalClassification(ch.classification) !== classFilter) return false
      }
      if (statusFilter !== 'all' && lineupStatus(ch) !== statusFilter) {
        return false
      }
      if (healthFilter !== 'all' && healthStatus(ch) !== healthFilter) {
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
  }, [data, providerFilter, groupFilter, classFilter, statusFilter, healthFilter, q])

  return (
    <>
      <h1>Channels</h1>
      <p className="lead">
        Live lineup from the last successful refresh. Click a row for export
        status, reasons, URLs, health history, and identity. Class is the stream
        dialect (NATIVE / Amagi SSAI / SESSION / Xumo SSAI / DRM); Health comes from
        segment/ffprobe checks.
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
              Group{' '}
              <select
                value={groupFilter}
                onChange={(e) => setGroupFilter(e.target.value)}
              >
                <option value="all">all</option>
                {groups.map((g) => (
                  <option key={g} value={g}>
                    {groupLabel(g)}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Class{' '}
              <select
                value={classFilter}
                onChange={(e) => setClassFilter(e.target.value)}
              >
                <option value="all">all</option>
                {CLASS_FILTERS.map((c) => (
                  <option key={c} value={c}>
                    {classBadge(c).label}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Status{' '}
              <select
                value={statusFilter}
                onChange={(e) =>
                  setStatusFilter(e.target.value as 'all' | LineupStatusKind)
                }
              >
                <option value="all">all</option>
                {STATUS_FILTERS.map((s) => (
                  <option key={s.value} value={s.value}>
                    {s.label}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Health{' '}
              <select
                value={healthFilter}
                onChange={(e) =>
                  setHealthFilter(e.target.value as 'all' | HealthFilterValue)
                }
              >
                <option value="all">all</option>
                {HEALTH_FILTERS.map((s) => (
                  <option key={s.value} value={s.value}>
                    {s.label}
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
                  <th scope="col" className="channel-logo-col">
                    Logo
                  </th>
                  <th scope="col">Name</th>
                  <th scope="col">Provider</th>
                  <th scope="col">Group</th>
                  <th scope="col">Class</th>
                  <th scope="col">Health</th>
                  <th scope="col">Status</th>
                </tr>
              </thead>
              <tbody>
                {rows.length === 0 ? (
                  <tr className="empty">
                    <td colSpan={9}>
                      No channels yet — wait for a successful provider refresh
                      (e.g. LG), or clear filters.
                    </td>
                  </tr>
                ) : (
                  rows.map((ch) => {
                    const cls = classBadge(ch.classification)
                    const hb = healthBadge(ch.health?.status)
                    const status = lineupBadge(ch)
                    const healthTitle = [
                      ch.health?.last_check_at
                        ? `Last check ${formatHealthWhen(ch.health.last_check_at)}`
                        : null,
                      ch.health?.consecutive_failures
                        ? `Streak ${ch.health.consecutive_failures}`
                        : null,
                      ch.health?.last_failure_class || null,
                      ch.health?.last_failure_detail || null,
                    ]
                      .filter(Boolean)
                      .join(' · ')
                    return (
                      <tr
                        key={`${ch.provider}:${ch.normalized_id}`}
                        className={ch.excluded ? 'excluded' : undefined}
                      >
                        <td>{displayNumber(ch.offset_number)}</td>
                        <td>{displayNumber(ch.number)}</td>
                        <td className="channel-logo-col">
                          {ch.logo_url ? (
                            <img
                              className="channel-logo"
                              src={ch.logo_url}
                              alt=""
                              loading="lazy"
                              onError={(e) => {
                                e.currentTarget.style.display = 'none'
                              }}
                            />
                          ) : (
                            '—'
                          )}
                        </td>
                        <td>
                          <Link
                            to={channelDetailPath(ch)}
                            className="channel-link"
                          >
                            {ch.name}
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
                          <span
                            className={`badge badge-${hb.kind}`}
                            title={healthTitle || undefined}
                          >
                            {hb.label}
                          </span>
                          {ch.health?.consecutive_failures ? (
                            <span className="meta health-streak">
                              {' '}
                              ×{ch.health.consecutive_failures}
                            </span>
                          ) : null}
                        </td>
                        <td>
                          <span
                            className={`badge ${status.className}`}
                            title={status.title}
                          >
                            {status.label}
                          </span>
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
