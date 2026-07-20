import { useEffect, useMemo, useRef, useState } from 'react'

type Programme = {
  channel_id: string
  title: string
  desc?: string
  start: string
  stop: string
}

type GuideChannel = {
  provider: string
  id: string
  normalized_id: string
  name: string
  group: string
  number: number
  offset_number: number
  logo_url?: string
  excluded: boolean
  programmes: Programme[]
}

type GuideResponse = {
  channels: GuideChannel[]
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

function fmtRange(startISO: string, stopISO: string): string {
  const s = new Date(startISO)
  const e = new Date(stopISO)
  const day = s.toLocaleDateString([], { weekday: 'short', month: 'short', day: 'numeric' })
  const st = s.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  const et = e.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
  return `${day} ${st}–${et}`
}

export function GuidePage() {
  const [data, setData] = useState<GuideResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [providerFilter, setProviderFilter] = useState('all')
  const [q, setQ] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const centered = useRef(false)

  useEffect(() => {
    let cancelled = false
    fetch('/api/guide')
      .then(async (res) => {
        if (!res.ok) {
          throw new Error(`${res.status} ${res.statusText}`)
        }
        return res.json() as Promise<GuideResponse>
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
    if (!data) return [] as GuideChannel[]
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

  // Time window spanning every programme in the filtered rows (snapped to hours).
  const window = useMemo(() => {
    let min = Infinity
    let max = -Infinity
    for (const ch of rows) {
      for (const p of ch.programmes) {
        const s = new Date(p.start).getTime()
        const e = new Date(p.stop).getTime()
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

  // On first load, scroll the timeline so "now" sits near the left edge
  // (with a little past context) instead of the start of the guide.
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
        Programme schedule from the last successful refresh (the same data
        served as XMLTV at <code>/{'{provider}'}.xml</code>). Scroll horizontally
        to move through time.
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
              {rows.length} of {data.channels.length} channels
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
              <div
                className="epg-inner"
                style={{ width: LABEL_W + timelineW }}
              >
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

                {rows.map((ch) => (
                  <div
                    key={`${ch.provider}:${ch.normalized_id}`}
                    className={`epg-row${ch.excluded ? ' excluded' : ''}`}
                    style={{ height: ROW_H }}
                  >
                    <div className="epg-label" style={{ width: LABEL_W }}>
                      <span className="epg-num">
                        {displayNumber(ch.offset_number)}
                      </span>
                      {ch.logo_url && (
                        <img
                          className="epg-logo"
                          src={ch.logo_url}
                          alt=""
                          loading="lazy"
                          onError={(e) => {
                            e.currentTarget.style.display = 'none'
                          }}
                        />
                      )}
                      <span className="epg-name" title={ch.name}>
                        {ch.name}
                      </span>
                    </div>
                    <div className="epg-track" style={{ width: timelineW }}>
                      {ch.programmes.map((p, i) => {
                        const s = new Date(p.start).getTime()
                        const e = new Date(p.stop).getTime()
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
