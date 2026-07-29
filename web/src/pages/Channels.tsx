import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
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
  lineupBadges,
  lineupStatusKinds,
  STATUS_FILTERS,
} from '../lib/channel'
import type { Channel, HealthFilterValue, LineupStatusKind } from '../lib/channel'
import {
  channelsFiltersFromSearch,
  channelsListDirty,
  channelsListToSearch,
  channelsSortFromSearch,
  clearStoredChannelsFilters,
  compareChannels,
  DEFAULT_CHANNELS_FILTERS,
  nextChannelsSort,
  readStoredChannelsList,
  searchHasChannelsListState,
  writeStoredChannelsList,
  type ChannelsLocationState,
  type ChannelsSort,
  type ChannelsSortKey,
} from '../lib/channelsFilters'

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

function SortTh({
  label,
  col,
  sort,
  onSort,
}: {
  label: string
  col: ChannelsSortKey
  sort: ChannelsSort
  onSort: (key: ChannelsSortKey) => void
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

export function ChannelsPage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const [data, setData] = useState<ChannelsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const didHydrate = useRef(false)

  // Hydrate from sessionStorage when the URL has no list keys (nav to bare /).
  useEffect(() => {
    if (didHydrate.current) return
    didHydrate.current = true
    if (searchHasChannelsListState(searchParams)) {
      writeStoredChannelsList(
        channelsFiltersFromSearch(searchParams),
        channelsSortFromSearch(searchParams),
      )
      return
    }
    const stored = readStoredChannelsList()
    if (stored && channelsListDirty(stored.filters, stored.sort)) {
      setSearchParams(channelsListToSearch(stored.filters, stored.sort), {
        replace: true,
      })
    }
  }, [searchParams, setSearchParams])

  const filters = useMemo(() => channelsFiltersFromSearch(searchParams), [searchParams])
  const sort = useMemo(() => channelsSortFromSearch(searchParams), [searchParams])
  const providerFilter = filters.provider
  const groupFilter = filters.group
  const classFilter = filters.class
  const statusFilter = filters.status
  const healthFilter = filters.health
  const q = filters.q

  function patchList(
    nextFilters: typeof DEFAULT_CHANNELS_FILTERS,
    nextSort: ChannelsSort,
  ) {
    const params = channelsListToSearch(nextFilters, nextSort)
    setSearchParams(params, { replace: true })
    writeStoredChannelsList(nextFilters, nextSort)
  }

  function patchFilters(patch: Partial<typeof DEFAULT_CHANNELS_FILTERS>) {
    patchList({ ...filters, ...patch }, sort)
  }

  function patchSort(key: ChannelsSortKey) {
    patchList(filters, nextChannelsSort(sort, key))
  }

  function resetFilters() {
    clearStoredChannelsFilters()
    setSearchParams({}, { replace: true })
  }

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
    return data.channels
      .filter((ch) => {
        if (providerFilter !== 'all' && ch.provider !== providerFilter) {
          return false
        }
        if (groupFilter !== 'all' && groupKey(ch.group) !== groupFilter) {
          return false
        }
        if (classFilter !== 'all') {
          if (canonicalClassification(ch.classification) !== classFilter) return false
        }
        if (
          statusFilter !== 'all' &&
          !lineupStatusKinds(ch).includes(statusFilter as LineupStatusKind)
        ) {
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
      .slice()
      .sort((a, b) => compareChannels(a, b, sort))
  }, [
    data,
    providerFilter,
    groupFilter,
    classFilter,
    statusFilter,
    healthFilter,
    q,
    sort,
  ])

  const detailState: ChannelsLocationState = {
    channelsSearch: searchParams.toString(),
  }
  const listDirty = channelsListDirty(filters, sort)

  return (
    <>
      <h1>Channels</h1>
      <p className="lead">
        Live lineup from the last successful refresh. Click a row for export
        status, reasons, URLs, health history, and identity. Class is the stream
        dialect (NATIVE / Amagi SSAI / SESSION / Xumo SSAI / DRM); Health comes from
        segment/ffprobe checks. Click a column header to sort.
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
                onChange={(e) => patchFilters({ provider: e.target.value })}
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
                onChange={(e) => patchFilters({ group: e.target.value })}
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
                onChange={(e) => patchFilters({ class: e.target.value })}
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
                  patchFilters({
                    status: e.target.value as 'all' | LineupStatusKind,
                  })
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
                  patchFilters({
                    health: e.target.value as 'all' | HealthFilterValue,
                  })
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
                onChange={(e) => patchFilters({ q: e.target.value })}
                placeholder="name, id, group, number…"
              />
            </label>
            <button
              type="button"
              className="toolbar-reset"
              onClick={resetFilters}
              disabled={!listDirty}
            >
              Reset filters
            </button>
            <span className="meta">
              {rows.length} of {data.channels.length}
            </span>
          </div>

          <div className="table-wrap">
            <table className="channels">
              <thead>
                <tr>
                  <SortTh label="#" col="number" sort={sort} onSort={patchSort} />
                  <SortTh label="Prov #" col="prov" sort={sort} onSort={patchSort} />
                  <th scope="col" className="channel-logo-col">
                    Logo
                  </th>
                  <SortTh label="Name" col="name" sort={sort} onSort={patchSort} />
                  <SortTh
                    label="Provider"
                    col="provider"
                    sort={sort}
                    onSort={patchSort}
                  />
                  <SortTh label="Group" col="group" sort={sort} onSort={patchSort} />
                  <SortTh label="Class" col="class" sort={sort} onSort={patchSort} />
                  <SortTh label="Health" col="health" sort={sort} onSort={patchSort} />
                  <SortTh label="Status" col="status" sort={sort} onSort={patchSort} />
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
                    const statuses = lineupBadges(ch)
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
                            state={detailState}
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
                          <span className="badge-row">
                            {statuses.map((status) => (
                              <span
                                key={status.kind}
                                className={`badge ${status.className}`}
                                title={status.title}
                              >
                                {status.label}
                              </span>
                            ))}
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
