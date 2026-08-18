import { useEffect, useMemo, useRef, useState } from 'react'
import { Link, useSearchParams } from 'react-router-dom'
import {
  CLASS_FILTERS,
  canonicalClassification,
  channelDetailPath,
  classBadge,
} from '../lib/channel'
import {
  clearStoredGuideFilters,
  DEFAULT_GUIDE_FILTERS,
  guideFiltersActive,
  guideFiltersFromSearch,
  guideFiltersToSearch,
  readStoredGuideFilters,
  searchHasGuideFilters,
  writeStoredGuideFilters,
  type TimePreset,
} from '../lib/guideFilters'
import {
  categorySlug,
  categoryStyle,
} from '../lib/categoryStyle'
import {
  enabledProviderIds,
  loadProviderGuides,
  metaIndex,
  sortGuideRows,
  summarizeLoad,
  type ChannelMeta,
  type GuideRow,
  type ProviderStatus,
} from '../lib/guideLoad'
import type { XmltvProgramme } from '../lib/xmltv'

const PX_PER_MIN = 4
const LABEL_W = 220
const ROW_H = 48
const TIMEBAR_H = 30
const MIN_PROG_W = 40
const HOUR_MS = 3_600_000
const ROW_OVERSCAN = 10
const PROG_PAD_PX = 240

type ProgDetail = {
  channelName: string
  title: string
  desc: string
  categories: string[]
  start: Date
  stop: Date
  left: number
  top: number
  pinned: boolean
}

function displayNumber(n: number): string {
  return n > 0 ? String(n) : '—'
}

function floorHour(ms: number): number {
  const d = new Date(ms)
  d.setMinutes(0, 0, 0)
  return d.getTime()
}

function ceilHour(ms: number): number {
  const f = floorHour(ms)
  return f === ms ? ms : f + HOUR_MS
}

function tickLabel(ms: number): string {
  const d = new Date(ms)
  if (d.getHours() === 0) {
    return d.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })
  }
  return d.toLocaleTimeString([], { hour: 'numeric' })
}

function fmtRange(start: Date, stop: Date): string {
  const day = start.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })
  const st = start.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  const et = stop.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  return `${day} ${st}–${et}`
}

function timeBounds(
  preset: TimePreset,
  now: number,
  dataMin: number,
  dataMax: number,
): { start: number; end: number } {
  switch (preset) {
    case 'pm6':
      return { start: now - 6 * HOUR_MS, end: now + 6 * HOUR_MS }
    case 'pm12':
      return { start: now - 12 * HOUR_MS, end: now + 12 * HOUR_MS }
    case 'today': {
      const d = new Date(now)
      d.setHours(0, 0, 0, 0)
      const start = d.getTime()
      return { start, end: start + 24 * HOUR_MS }
    }
    case 'next24':
      return { start: now, end: now + 24 * HOUR_MS }
    case 'all': {
      if (!isFinite(dataMin) || !isFinite(dataMax) || dataMax <= dataMin) {
        const n = floorHour(now)
        return { start: n, end: n + 6 * HOUR_MS }
      }
      return { start: floorHour(dataMin), end: ceilHour(dataMax) }
    }
  }
}

function overlaps(start: number, stop: number, winStart: number, winEnd: number): boolean {
  return stop > winStart && start < winEnd
}

function phaseClass(phase: ProviderStatus['phase']): string {
  switch (phase) {
    case 'ready':
      return 'ready'
    case 'empty':
      return 'empty'
    case 'error':
      return 'error'
    case 'fetching':
    case 'parsing':
      return 'active'
    default:
      return 'pending'
  }
}

export function GuidePage() {
  const [searchParams, setSearchParams] = useSearchParams()
  const didHydrate = useRef(false)

  useEffect(() => {
    if (didHydrate.current) return
    didHydrate.current = true
    if (searchHasGuideFilters(searchParams)) {
      writeStoredGuideFilters(guideFiltersFromSearch(searchParams))
      return
    }
    const stored = readStoredGuideFilters()
    if (stored && guideFiltersActive(stored)) {
      setSearchParams(guideFiltersToSearch(stored), { replace: true })
    }
  }, [searchParams, setSearchParams])

  const filters = useMemo(() => guideFiltersFromSearch(searchParams), [searchParams])
  const providerFilter = filters.provider
  const groupFilter = filters.group
  const regionFilter = filters.region
  const classFilter = filters.class
  const hideExcluded = filters.hideExcluded
  const timePreset = filters.time
  const channelQ = filters.channelQ
  const programmeQ = filters.programmeQ

  function patchFilters(patch: Partial<typeof DEFAULT_GUIDE_FILTERS>) {
    let next = { ...filters, ...patch }
    // DRM channels are always excluded — DRM filter is meaningless while hiding excluded.
    if (next.hideExcluded && next.class === 'DRM') {
      next = { ...next, class: 'all' }
    }
    const params = guideFiltersToSearch(next)
    setSearchParams(params, { replace: true })
    writeStoredGuideFilters(next)
  }

  function resetFilters() {
    clearStoredGuideFilters()
    setSearchParams({}, { replace: true })
    setDetail(null)
  }

  const [channels, setChannels] = useState<ChannelMeta[] | null>(null)
  const [rowsById, setRowsById] = useState<Map<string, GuideRow>>(() => new Map())
  const [statuses, setStatuses] = useState<ProviderStatus[]>([])
  const [bootError, setBootError] = useState<string | null>(null)
  const [booting, setBooting] = useState(true)
  const [detail, setDetail] = useState<ProgDetail | null>(null)

  const loadedRef = useRef(new Set<string>())
  const scrollRef = useRef<HTMLDivElement>(null)
  const centered = useRef(false)
  const [viewport, setViewport] = useState({
    top: 0,
    left: 0,
    height: 480,
    width: 800,
  })

  // Bootstrap channels + providers, then load XMLTV per provider.
  useEffect(() => {
    const ac = new AbortController()
    let cancelled = false

    ;(async () => {
      setBooting(true)
      setBootError(null)
      try {
        const [chansRes, provRes] = await Promise.all([
          fetch('/api/channels', { signal: ac.signal }),
          fetch('/api/providers', { signal: ac.signal }),
        ])
        if (!chansRes.ok) {
          throw new Error(`channels: ${chansRes.status} ${chansRes.statusText}`)
        }
        if (!provRes.ok) {
          throw new Error(`providers: ${provRes.status} ${provRes.statusText}`)
        }
        const chansBody = (await chansRes.json()) as { channels: ChannelMeta[] }
        const provBody = (await provRes.json()) as {
          providers: { id: string; enabled: boolean; label: string }[]
        }
        if (cancelled) return
        setChannels(chansBody.channels)
        setBooting(false)

        const ids = enabledProviderIds(provBody.providers, providerFilter)
        const meta = metaIndex(chansBody.channels)
        await loadProviderGuides(ids, meta, loadedRef.current, {
          signal: ac.signal,
          onStatuses: (next) => {
            if (!cancelled) setStatuses(next)
          },
          onProviderRows: (provider, rows) => {
            if (cancelled) return
            loadedRef.current.add(provider)
            setRowsById((prev) => {
              const next = new Map(prev)
              for (const row of rows) next.set(row.id, row)
              return next
            })
          },
        })
      } catch (err) {
        if (cancelled || (err instanceof DOMException && err.name === 'AbortError')) return
        setBootError(err instanceof Error ? err.message : String(err))
        setBooting(false)
      }
    })()

    return () => {
      cancelled = true
      ac.abort()
    }
  }, [providerFilter])

  const allRows = useMemo(() => sortGuideRows([...rowsById.values()]), [rowsById])

  const providers = useMemo(() => {
    const fromChannels = channels?.map((c) => c.provider) ?? []
    const fromRows = allRows.map((r) => r.provider)
    return [...new Set([...fromChannels, ...fromRows].filter(Boolean))].sort()
  }, [channels, allRows])

  const groups = useMemo(() => {
    const src = channels ?? allRows.map((r) => ({ group: r.group }))
    return [...new Set(src.map((c) => c.group).filter(Boolean))].sort()
  }, [channels, allRows])

  const regions = useMemo(() => {
    const src = channels ?? allRows.map((r) => ({ region: r.region }))
    return [
      ...new Set(
        src
          .map((c) => ('region' in c ? c.region : '') || '')
          .filter(Boolean),
      ),
    ].sort()
  }, [channels, allRows])

  const dataExtent = useMemo(() => {
    let min = Infinity
    let max = -Infinity
    for (const r of allRows) {
      for (const p of r.programmes) {
        const s = p.start.getTime()
        const e = p.stop.getTime()
        if (s < min) min = s
        if (e > max) max = e
      }
    }
    return { min, max }
  }, [allRows])

  const timeWindow = useMemo(() => {
    const bounds = timeBounds(timePreset, Date.now(), dataExtent.min, dataExtent.max)
    return { start: floorHour(bounds.start), end: ceilHour(bounds.end) }
  }, [timePreset, dataExtent.min, dataExtent.max])

  const now = Date.now()

  const rows = useMemo(() => {
    const channelNeedle = channelQ.trim().toLowerCase()
    const progNeedle = programmeQ.trim().toLowerCase()
    return allRows
      .filter((r) => {
        if (providerFilter !== 'all' && r.provider !== providerFilter) return false
        if (hideExcluded && r.excluded) return false
        if (groupFilter !== 'all' && r.group !== groupFilter) return false
        if (regionFilter !== 'all' && (r.region || '') !== regionFilter) return false
        if (classFilter !== 'all' && canonicalClassification(r.classification) !== classFilter)
          return false
        if (channelNeedle) {
          const hit =
            r.name.toLowerCase().includes(channelNeedle) ||
            r.rawId.toLowerCase().includes(channelNeedle) ||
            r.normalizedId.toLowerCase().includes(channelNeedle) ||
            r.group.toLowerCase().includes(channelNeedle) ||
            (r.region || '').toLowerCase().includes(channelNeedle) ||
            String(r.number).includes(channelNeedle)
          if (!hit) return false
        }
        return true
      })
      .map((r) => {
        let programmes = r.programmes.filter((p) =>
          overlaps(p.start.getTime(), p.stop.getTime(), timeWindow.start, timeWindow.end),
        )
        if (progNeedle) {
          programmes = programmes.filter((p) =>
            p.title.toLowerCase().includes(progNeedle),
          )
        }
        return { ...r, programmes }
      })
      .filter((r) => !progNeedle || r.programmes.length > 0)
  }, [
    allRows,
    providerFilter,
    hideExcluded,
    groupFilter,
    regionFilter,
    classFilter,
    channelQ,
    programmeQ,
    timeWindow,
  ])

  const totalMin = (timeWindow.end - timeWindow.start) / 60_000
  const timelineW = Math.max(totalMin * PX_PER_MIN, 1)

  const ticks = useMemo(() => {
    const out: { x: number; label: string; midnight: boolean }[] = []
    for (let t = timeWindow.start; t <= timeWindow.end; t += HOUR_MS) {
      out.push({
        x: ((t - timeWindow.start) / 60_000) * PX_PER_MIN,
        label: tickLabel(t),
        midnight: new Date(t).getHours() === 0,
      })
    }
    return out
  }, [timeWindow])

  const nowX =
    now >= timeWindow.start && now <= timeWindow.end
      ? ((now - timeWindow.start) / 60_000) * PX_PER_MIN
      : null

  const loadSummary = summarizeLoad(statuses)
  const loading = booting || statuses.some((s) =>
    s.phase === 'pending' || s.phase === 'fetching' || s.phase === 'parsing',
  )
  const hasPaint = allRows.length > 0
  const filtersDirty = guideFiltersActive(filters)

  function showProgrammeDetail(
    el: HTMLElement,
    channelName: string,
    p: XmltvProgramme,
    pinned: boolean,
  ) {
    const rect = el.getBoundingClientRect()
    const left = Math.min(rect.left, window.innerWidth - 320)
    const top = Math.min(rect.bottom + 6, window.innerHeight - 160)
    setDetail({
      channelName,
      title: p.title,
      desc: p.desc,
      categories: p.categories ?? [],
      start: p.start,
      stop: p.stop,
      left: Math.max(8, left),
      top: Math.max(8, top),
      pinned,
    })
  }

  useEffect(() => {
    if (!detail) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setDetail(null)
    }
    const onPointer = (e: PointerEvent) => {
      const target = e.target as HTMLElement | null
      if (target?.closest('.epg-prog') || target?.closest('.epg-detail')) return
      setDetail(null)
    }
    window.addEventListener('keydown', onKey)
    window.addEventListener('pointerdown', onPointer)
    return () => {
      window.removeEventListener('keydown', onKey)
      window.removeEventListener('pointerdown', onPointer)
    }
  }, [detail])

  // Viewport tracking for lazy DOM.
  useEffect(() => {
    const el = scrollRef.current
    if (!el) return
    let raf = 0
    const update = () => {
      setViewport({
        top: el.scrollTop,
        left: el.scrollLeft,
        height: el.clientHeight,
        width: el.clientWidth,
      })
    }
    const onScroll = () => {
      cancelAnimationFrame(raf)
      raf = requestAnimationFrame(update)
    }
    el.addEventListener('scroll', onScroll, { passive: true })
    const ro = new ResizeObserver(onScroll)
    ro.observe(el)
    update()
    return () => {
      cancelAnimationFrame(raf)
      el.removeEventListener('scroll', onScroll)
      ro.disconnect()
    }
  }, [hasPaint])

  // Center on "now" once we have a painted grid.
  useEffect(() => {
    if (centered.current || !hasPaint) return
    const el = scrollRef.current
    if (!el) return
    const t = Date.now()
    if (t < timeWindow.start || t > timeWindow.end) return
    const x = ((t - timeWindow.start) / 60_000) * PX_PER_MIN
    el.scrollLeft = Math.max(0, x - 30 * PX_PER_MIN)
    centered.current = true
  }, [hasPaint, timeWindow])

  const rowScrollTop = Math.max(0, viewport.top - TIMEBAR_H)
  const startIdx = Math.max(0, Math.floor(rowScrollTop / ROW_H) - ROW_OVERSCAN)
  const endIdx = Math.min(
    rows.length,
    Math.ceil((rowScrollTop + viewport.height) / ROW_H) + ROW_OVERSCAN,
  )
  const visibleRows = rows.slice(startIdx, endIdx)
  const topSpacer = startIdx * ROW_H
  const bottomSpacer = Math.max(0, (rows.length - endIdx) * ROW_H)

  const viewLeft = viewport.left - LABEL_W - PROG_PAD_PX
  const viewRight = viewport.left - LABEL_W + viewport.width + PROG_PAD_PX

  return (
    <>
      <h1>Guide</h1>
      <p className="lead">
        Programme schedule parsed from per-provider XMLTV (
        <code>/api/guide/{'{provider}'}.xml</code>). Providers load one at a
        time; the grid paints as soon as the first guide is ready.
      </p>

      <div className="guide-status" role="status" aria-live="polite">
        <div className="guide-status-summary">
          {bootError
            ? `Failed to load: ${bootError}`
            : booting
              ? 'Loading channels…'
              : loadSummary}
        </div>
        {statuses.length > 0 && (
          <div className="guide-status-chips">
            {statuses.map((s) => (
              <span
                key={s.id}
                className={`guide-chip guide-chip-${phaseClass(s.phase)}`}
                title={s.error ?? s.phase}
              >
                {s.id}
                <span className="guide-chip-phase">{s.phase}</span>
              </span>
            ))}
          </div>
        )}
      </div>

      {(hasPaint || !booting) && !bootError && (
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
                    {g}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Region{' '}
              <select
                value={regionFilter}
                onChange={(e) => patchFilters({ region: e.target.value })}
              >
                <option value="all">all</option>
                {regions.map((r) => (
                  <option key={r} value={r}>
                    {r}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Classification{' '}
              <select
                value={classFilter}
                onChange={(e) => patchFilters({ class: e.target.value })}
              >
                <option value="all">all</option>
                {CLASS_FILTERS.map((c) => (
                  <option key={c} value={c} disabled={hideExcluded && c === 'DRM'}>
                    {classBadge(c).label}
                  </option>
                ))}
              </select>
            </label>
            <label>
              Time{' '}
              <select
                value={timePreset}
                onChange={(e) =>
                  patchFilters({ time: e.target.value as TimePreset })
                }
              >
                <option value="pm6">Now ±6h</option>
                <option value="pm12">Now ±12h</option>
                <option value="today">Today</option>
                <option value="next24">Next 24h</option>
                <option value="all">All loaded</option>
              </select>
            </label>
            <label className="toolbar-check">
              <input
                type="checkbox"
                checked={hideExcluded}
                onChange={(e) => patchFilters({ hideExcluded: e.target.checked })}
              />
              Hide excluded
            </label>
            <label>
              Channels{' '}
              <input
                type="search"
                value={channelQ}
                onChange={(e) => patchFilters({ channelQ: e.target.value })}
                placeholder="name, id, group, number…"
              />
            </label>
            <label>
              Programmes{' '}
              <input
                type="search"
                value={programmeQ}
                onChange={(e) => patchFilters({ programmeQ: e.target.value })}
                placeholder="title…"
              />
            </label>
            <button
              type="button"
              className="toolbar-reset"
              onClick={resetFilters}
              disabled={!filtersDirty}
            >
              Reset filters
            </button>
            <span className="meta">
              {rows.length} of {allRows.length} channels
              {loading ? ' · loading…' : ''}
            </span>
          </div>

          {!hasPaint && loading ? (
            <div className="empty-panel" role="status">
              <p>Waiting for the first provider guide…</p>
            </div>
          ) : rows.length === 0 ? (
            <div className="empty-panel" role="status">
              <p>
                No guide rows match the current filters
                {loading ? ' (still loading providers)' : ''}.
              </p>
            </div>
          ) : (
            <div className="epg" ref={scrollRef}>
              <div className="epg-inner" style={{ width: LABEL_W + timelineW }}>
                <div className="epg-timebar" style={{ height: TIMEBAR_H }}>
                  <div className="epg-corner" style={{ width: LABEL_W }}>
                    Channel
                  </div>
                  <div className="epg-ticks" style={{ width: timelineW }}>
                    {ticks.map((t, i) => (
                      <span
                        key={i}
                        className={`epg-tick${t.midnight ? ' day' : ''}`}
                        style={{ left: t.x }}
                      >
                        {t.label}
                      </span>
                    ))}
                  </div>
                </div>

                {nowX !== null && (
                  <div
                    className="epg-now"
                    style={{ left: LABEL_W + nowX }}
                    aria-hidden
                  />
                )}

                {topSpacer > 0 && (
                  <div className="epg-spacer" style={{ height: topSpacer }} aria-hidden />
                )}

                {visibleRows.map((r) => (
                  <div
                    key={r.id}
                    className={`epg-row${r.excluded ? ' excluded' : ''}`}
                    style={{ height: ROW_H }}
                  >
                    <div className="epg-label" style={{ width: LABEL_W }}>
                      <span className="epg-num">{displayNumber(r.number)}</span>
                      {r.logo && (
                        <img
                          className="epg-logo"
                          src={r.logo}
                          alt=""
                          loading="lazy"
                          onError={(e) => {
                            e.currentTarget.style.display = 'none'
                          }}
                        />
                      )}
                      <Link
                        to={channelDetailPath({
                          provider: r.provider,
                          normalized_id: r.normalizedId,
                        })}
                        className="epg-name channel-link"
                        title={r.name}
                      >
                        {r.name}
                      </Link>
                    </div>
                    <div className="epg-track" style={{ width: timelineW }}>
                      {r.programmes.map((p, i) => {
                        const s = p.start.getTime()
                        const e = p.stop.getTime()
                        let left = ((s - timeWindow.start) / 60_000) * PX_PER_MIN
                        let width = ((e - s) / 60_000) * PX_PER_MIN
                        if (left < 0) {
                          width += left
                          left = 0
                        }
                        if (left + width > timelineW) {
                          width = timelineW - left
                        }
                        if (width < 1) return null
                        if (left + width < viewLeft || left > viewRight) return null
                        const onNow = s <= now && now < e
                        const firstCat = p.categories[0]
                        const catClass = firstCat
                          ? ` epg-cat-${categorySlug(firstCat)}`
                          : ''
                        const colors = categoryStyle(firstCat)
                        return (
                          <div
                            key={i}
                            role="button"
                            tabIndex={0}
                            className={`epg-prog${catClass}${onNow ? ' now' : ''}`}
                            style={{
                              left,
                              width: Math.max(width, MIN_PROG_W),
                              background: colors.background,
                              borderColor: colors.borderColor,
                              borderLeft: colors.borderLeft,
                            }}
                            onMouseEnter={(e) => {
                              if (detail?.pinned) return
                              showProgrammeDetail(e.currentTarget, r.name, p, false)
                            }}
                            onMouseLeave={() => {
                              setDetail((cur) => (cur && !cur.pinned ? null : cur))
                            }}
                            onClick={(e) => {
                              e.stopPropagation()
                              showProgrammeDetail(e.currentTarget, r.name, p, true)
                            }}
                            onKeyDown={(e) => {
                              if (e.key === 'Enter' || e.key === ' ') {
                                e.preventDefault()
                                showProgrammeDetail(e.currentTarget, r.name, p, true)
                              }
                            }}
                          >
                            <span className="epg-prog-title">{p.title}</span>
                          </div>
                        )
                      })}
                    </div>
                  </div>
                ))}

                {bottomSpacer > 0 && (
                  <div
                    className="epg-spacer"
                    style={{ height: bottomSpacer }}
                    aria-hidden
                  />
                )}
              </div>
            </div>
          )}
        </>
      )}

      {detail && (
        <div
          className="epg-detail"
          role="dialog"
          aria-label="Programme details"
          style={{ left: detail.left, top: detail.top }}
        >
          <div className="epg-detail-channel">{detail.channelName}</div>
          <div className="epg-detail-title">{detail.title}</div>
          <div className="epg-detail-time">{fmtRange(detail.start, detail.stop)}</div>
          {detail.categories.length > 0 ? (
            <div className="epg-detail-cats">
              {detail.categories.map((c) => (
                <span key={c} className="epg-cat-chip">
                  {c}
                </span>
              ))}
            </div>
          ) : null}
          {detail.desc ? <p className="epg-detail-desc">{detail.desc}</p> : null}
          {detail.pinned ? (
            <button
              type="button"
              className="epg-detail-close"
              onClick={() => setDetail(null)}
            >
              Close
            </button>
          ) : null}
        </div>
      )}
    </>
  )
}
