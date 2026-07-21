import { useEffect, useMemo, useRef, useState } from 'react'
import {
  combinedId,
  parseXMLTV,
  type XmltvProgramme,
  type Xmltv,
} from '../lib/xmltv'

// Channel-level meta from /api/channels used to augment the XMLTV (non-standard fields).
type ChannelRow = {
  provider: string
  id: string
  normalized_id: string
  name: string
  group: string
  number: number
  offset_number: number
  logo_url?: string
  classification?: string
  excluded: boolean
}

type Row = {
  id: string // namespaced tvg-id from the aggregate guide
  name: string
  number: number
  logo: string
  provider: string
  group: string
  rawId: string
  normalizedId: string
  excluded: boolean
  programmes: XmltvProgramme[]
}

const PX_PER_MIN = 4 // 240px per hour
const LABEL_W = 220
const ROW_H = 48
const MIN_PROG_W = 40
const HOUR_MS = 3_600_000

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

export function GuidePage() {
  const [data, setData] = useState<{ channels: ChannelRow[]; guide: Xmltv } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [providerFilter, setProviderFilter] = useState('all')
  const [q, setQ] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const centered = useRef(false)

  useEffect(() => {
    let cancelled = false
    Promise.all([
      fetch('/api/channels').then(async (res) => {
        if (!res.ok) throw new Error(`channels: ${res.status} ${res.statusText}`)
        return (await res.json()) as { channels: ChannelRow[] }
      }),
      fetch('/api/guide.xml?includeAll=true').then(async (res) => {
        if (!res.ok) throw new Error(`guide: ${res.status} ${res.statusText}`)
        return parseXMLTV(await res.text())
      }),
    ])
      .then(([chans, guide]) => {
        if (!cancelled) {
          setData({ channels: chans.channels, guide })
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

  // Merge XMLTV (primary: id, name, number, logo, programmes) with /api/channels
  // (augment: provider, group, excluded, raw id) joined on the namespaced id.
  const allRows = useMemo<Row[]>(() => {
    if (!data) return []
    const meta = new Map<string, ChannelRow>()
    for (const c of data.channels) {
      meta.set(combinedId(c.provider, c.normalized_id), c)
    }
    const progs = new Map<string, XmltvProgramme[]>()
    for (const p of data.guide.programmes) {
      const arr = progs.get(p.channel)
      if (arr) arr.push(p)
      else progs.set(p.channel, [p])
    }
    return data.guide.channels.map((xc) => {
      const m = meta.get(xc.id)
      const list = (progs.get(xc.id) ?? [])
        .slice()
        .sort((a, b) => a.start.getTime() - b.start.getTime())
      return {
        id: xc.id,
        name: xc.displayName,
        number: xc.number,
        logo: xc.logo,
        provider: m?.provider ?? '',
        group: m?.group ?? '',
        rawId: m?.id ?? '',
        normalizedId: m?.normalized_id ?? xc.id,
        excluded: m?.excluded ?? false,
        programmes: list,
      }
    })
  }, [data])

  const providers = useMemo(() => {
    return [...new Set(allRows.map((r) => r.provider).filter(Boolean))].sort()
  }, [allRows])

  const rows = useMemo(() => {
    const needle = q.trim().toLowerCase()
    return allRows.filter((r) => {
      if (providerFilter !== 'all' && r.provider !== providerFilter) {
        return false
      }
      if (!needle) return true
      return (
        r.name.toLowerCase().includes(needle) ||
        r.rawId.toLowerCase().includes(needle) ||
        r.normalizedId.toLowerCase().includes(needle) ||
        r.group.toLowerCase().includes(needle) ||
        String(r.number).includes(needle)
      )
    })
  }, [allRows, providerFilter, q])

  // Time window spanning every programme in the filtered rows (snapped to hours).
  const window = useMemo(() => {
    let min = Infinity
    let max = -Infinity
    for (const r of rows) {
      for (const p of r.programmes) {
        const s = p.start.getTime()
        const e = p.stop.getTime()
        if (s < min) min = s
        if (e > max) max = e
      }
    }
    if (!isFinite(min) || !isFinite(max) || max <= min) {
      const now = floorHour(Date.now())
      return { start: now, end: now + 6 * HOUR_MS }
    }
    return { start: floorHour(min), end: ceilHour(max) }
  }, [rows])

  const totalMin = (window.end - window.start) / 60_000
  const timelineW = totalMin * PX_PER_MIN

  const ticks = useMemo(() => {
    const out: { x: number; label: string; midnight: boolean }[] = []
    for (let t = window.start; t <= window.end; t += HOUR_MS) {
      out.push({
        x: ((t - window.start) / 60_000) * PX_PER_MIN,
        label: tickLabel(t),
        midnight: new Date(t).getHours() === 0,
      })
    }
    return out
  }, [window])

  const now = Date.now()
  const nowX =
    now >= window.start && now <= window.end
      ? ((now - window.start) / 60_000) * PX_PER_MIN
      : null

  // On first load, scroll the timeline so "now" sits near the left edge.
  useEffect(() => {
    if (centered.current) return
    const el = scrollRef.current
    if (!el) return
    const t = Date.now()
    if (t < window.start || t > window.end) return
    const x = ((t - window.start) / 60_000) * PX_PER_MIN
    el.scrollLeft = Math.max(0, x - 30 * PX_PER_MIN)
    centered.current = true
  }, [data, window])

  return (
    <>
      <h1>Guide</h1>
      <p className="lead">
        Programme schedule parsed from XMLTV (the same artifact served at{' '}
        <code>/api/guide.xml</code> and <code>/{'{provider}'}.xml</code>). Scroll
        horizontally to move through time.
      </p>

      {error && (
        <div className="empty-panel" role="alert">
          <p>Failed to load guide: {error}</p>
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
                placeholder="name, raw/normalized id, group, number…"
              />
            </label>
            <span className="meta">
              {rows.length} of {allRows.length} channels
            </span>
          </div>

          {rows.length === 0 ? (
            <div className="empty-panel" role="status">
              <p>
                No guide data yet — wait for a successful provider refresh (e.g.
                LG), or clear filters.
              </p>
            </div>
          ) : (
            <div className="epg" ref={scrollRef}>
              <div className="epg-inner" style={{ width: LABEL_W + timelineW }}>
                <div className="epg-timebar" style={{ height: 30 }}>
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

                {rows.map((r) => (
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
                      <span className="epg-name" title={r.name}>
                        {r.name}
                      </span>
                    </div>
                    <div className="epg-track" style={{ width: timelineW }}>
                      {r.programmes.map((p, i) => {
                        const s = p.start.getTime()
                        const e = p.stop.getTime()
                        let left = ((s - window.start) / 60_000) * PX_PER_MIN
                        let width = ((e - s) / 60_000) * PX_PER_MIN
                        if (left < 0) {
                          width += left
                          left = 0
                        }
                        if (left + width > timelineW) {
                          width = timelineW - left
                        }
                        if (width < 1) return null
                        const onNow = s <= now && now < e
                        return (
                          <div
                            key={i}
                            className={`epg-prog${onNow ? ' now' : ''}`}
                            style={{ left, width: Math.max(width, MIN_PROG_W) }}
                            title={`${fmtRange(p.start, p.stop)} — ${p.title}${
                              p.desc ? `\n\n${p.desc}` : ''
                            }`}
                          >
                            <span className="epg-prog-title">{p.title}</span>
                          </div>
                        )
                      })}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}
        </>
      )}
    </>
  )
}
