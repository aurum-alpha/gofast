import { useCallback, useEffect, useState, Fragment, type ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  classBadge,
  displayNumber,
  formatHealthWhen,
  healthBadge,
  lineupBadge,
} from '../lib/channel'
import type { Channel, ChannelHealth } from '../lib/channel'

type HistoryEvent = {
  at: string
  source?: string
  value: ChannelHealth | string
}

type HistoryResponse = {
  events: HistoryEvent[]
  success_rate_30d: number | null
}

type ProbeResponse = {
  check: {
    result: string
    failure_class?: string
    detail?: string
    http_status?: number
    at: string
    source?: string
  }
  health: ChannelHealth
}

type ProbeSchedule = {
  l2_interval: string
  last_l2_at?: string
  next_l2_at?: string
  l2_running?: boolean
  l3_enabled?: boolean
  l3_interval?: string
  last_l3_at?: string
  next_l3_at?: string
  l3_running?: boolean
}

function CellValue({ children }: { children: ReactNode }) {
  return <div className="compare-value">{children}</div>
}

function Plain({ value }: { value?: string }) {
  if (!value) return <span className="subtle">—</span>
  return <>{value}</>
}

function Code({ value }: { value?: string }) {
  if (!value) return <span className="subtle">—</span>
  return <code>{value}</code>
}

function Url({ value }: { value?: string }) {
  if (!value) return <span className="subtle">—</span>
  return (
    <a href={value} target="_blank" rel="noreferrer">
      <code className="url-break">{value}</code>
    </a>
  )
}

function LogoPreview({ src }: { src?: string }) {
  if (!src) return <span className="subtle">—</span>
  return (
    <img
      className="channel-logo-full"
      src={src}
      alt=""
      onError={(e) => {
        e.currentTarget.style.display = 'none'
      }}
    />
  )
}

function parseHistoryValue(value: HistoryEvent['value']): ChannelHealth {
  if (value && typeof value === 'object') return value
  if (typeof value === 'string') {
    try {
      return JSON.parse(value) as ChannelHealth
    } catch {
      return {}
    }
  }
  return {}
}

export function ChannelDetailPage() {
  const { provider = '', normalizedId = '' } = useParams()
  const [channel, setChannel] = useState<Channel | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [history, setHistory] = useState<HistoryResponse | null>(null)
  const [historyError, setHistoryError] = useState<string | null>(null)
  const [probeBusy, setProbeBusy] = useState<'l2' | 'l3' | null>(null)
  const [probeError, setProbeError] = useState<string | null>(null)
  const [probeNote, setProbeNote] = useState<string | null>(null)
  const [schedule, setSchedule] = useState<ProbeSchedule | null>(null)
  const [expandedHistory, setExpandedHistory] = useState<Record<string, boolean>>({})

  const loadHistory = useCallback(() => {
    const path = `/api/channels/${encodeURIComponent(provider)}/${encodeURIComponent(normalizedId)}/health/history`
    fetch(path)
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        return res.json() as Promise<HistoryResponse>
      })
      .then((body) => {
        setHistory(body)
        setHistoryError(null)
      })
      .catch((err: unknown) => {
        setHistoryError(err instanceof Error ? err.message : String(err))
      })
  }, [provider, normalizedId])

  useEffect(() => {
    let cancelled = false
    const path = `/api/channels/${encodeURIComponent(provider)}/${encodeURIComponent(normalizedId)}`
    fetch(path)
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        return res.json() as Promise<Channel>
      })
      .then((body) => {
        if (!cancelled) {
          setChannel(body)
          setError(null)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      })
    return () => {
      cancelled = true
    }
  }, [provider, normalizedId])

  useEffect(() => {
    loadHistory()
  }, [loadHistory])

  useEffect(() => {
    let cancelled = false
    fetch('/api/health/schedule')
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        return res.json() as Promise<ProbeSchedule>
      })
      .then((body) => {
        if (!cancelled) setSchedule(body)
      })
      .catch(() => {
        if (!cancelled) setSchedule(null)
      })
    return () => {
      cancelled = true
    }
  }, [])

  async function runProbe(kind: 'l2' | 'l3') {
    setProbeBusy(kind)
    setProbeError(null)
    setProbeNote(null)
    const suffix = kind === 'l2' ? '/health/probe/l2' : '/health/probe'
    const path = `/api/channels/${encodeURIComponent(provider)}/${encodeURIComponent(normalizedId)}${suffix}`
    try {
      const res = await fetch(path, { method: 'POST' })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text || `${res.status} ${res.statusText}`)
      }
      const body = (await res.json()) as ProbeResponse
      setChannel((prev) => (prev ? { ...prev, health: body.health } : prev))
      const label = kind === 'l2' ? 'L2' : 'L3'
      const http =
        body.check.http_status != null && body.check.http_status > 0
          ? ` (HTTP ${body.check.http_status})`
          : ''
      setProbeNote(
        body.check.result === 'success'
          ? `${label} probe succeeded${http}`
          : `${label} probe failed${body.check.failure_class ? `: ${body.check.failure_class}` : ''}${http}`,
      )
      loadHistory()
    } catch (err: unknown) {
      setProbeError(err instanceof Error ? err.message : String(err))
    } finally {
      setProbeBusy(null)
    }
  }

  if (error) {
    return (
      <>
        <Link to="/" className="back-link">
          ← Channels
        </Link>
        <div className="empty-panel" role="alert">
          Failed to load channel: {error}
        </div>
      </>
    )
  }
  if (!channel) {
    return (
      <div className="empty-panel" role="status">
        Loading…
      </div>
    )
  }

  const status = lineupBadge(channel)
  const cls = classBadge(channel.classification)
  const hb = healthBadge(channel.health?.status)
  const providerLogo = channel.logo_source_url || channel.logo_url
  const exportedPlayback = channel.excluded
    ? undefined
    : channel.emitted_url || channel.stream_url
  const exportedLogo = channel.logo_url || undefined
  const inLineup = status.kind === 'in-lineup' || status.kind === 'proxied'

  return (
    <>
      <Link to="/" className="back-link">
        ← Channels
      </Link>
      <div className="detail-heading">
        <div>
          <h1>{channel.name}</h1>
          <p className="lead">
            <Link to={`/providers/${encodeURIComponent(channel.provider)}`}>
              <code>{channel.provider}</code>
            </Link>
          </p>
          {channel.description ? (
            <p className="channel-description">{channel.description}</p>
          ) : null}
        </div>
        <div className="status-block">
          <div className="badge-row">
            <span className={`badge badge-${cls.kind}`}>{cls.label}</span>
            <span className={`badge badge-${hb.kind}`}>{hb.label}</span>
            <span className={`badge ${status.className}`} title={status.title}>
              {status.label}
            </span>
          </div>
          {!inLineup && channel.filter_reason ? (
            <p className="status-reason">{channel.filter_reason}</p>
          ) : null}
          {status.kind === 'needs-proxy' ? (
            <p className="status-reason">
              Configure <code>proxy_base_url</code> / FASTProxy so Amagi SSAI streams can
              be emitted.
            </p>
          ) : null}
          {status.kind === 'drm' && channel.license_url ? (
            <p className="status-reason">
              DRM license evidence:{' '}
              <code className="url-break">{channel.license_url}</code>
            </p>
          ) : null}
        </div>
      </div>

      <section className="detail-section">
        <h2>Provider vs Fastgen</h2>
        <div className="table-wrap">
          <table className="compare-table">
            <thead>
              <tr>
                <th scope="col">Field</th>
                <th scope="col">From provider</th>
                <th scope="col">Fastgen export</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <th scope="row">Channel id</th>
                <td>
                  <CellValue>
                    <span className="field-hint">Upstream id</span>
                    <Code value={channel.id} />
                  </CellValue>
                </td>
                <td>
                  <CellValue>
                    <span className="field-hint">Normalized id (tvg-id / XMLTV)</span>
                    <Code value={channel.normalized_id} />
                  </CellValue>
                </td>
              </tr>
              <tr>
                <th scope="row">Channel number</th>
                <td>
                  <CellValue>
                    <span className="field-hint">Provider number</span>
                    <Plain value={displayNumber(channel.number)} />
                  </CellValue>
                </td>
                <td>
                  <CellValue>
                    <span className="field-hint">Export number (tvg-chno / LCN)</span>
                    <Plain value={displayNumber(channel.offset_number)} />
                  </CellValue>
                </td>
              </tr>
              <tr>
                <th scope="row">Group</th>
                <td>
                  <CellValue>
                    <span className="field-hint">Provider group</span>
                    <Plain value={channel.group || undefined} />
                  </CellValue>
                </td>
                <td>
                  <CellValue>
                    <span className="field-hint">group-title (same unless labeled)</span>
                    <Plain value={channel.group || undefined} />
                  </CellValue>
                </td>
              </tr>
              <tr>
                <th scope="row">Stream</th>
                <td>
                  <CellValue>
                    <span className="field-hint">Upstream stream URL</span>
                    <Url value={channel.stream_url} />
                  </CellValue>
                </td>
                <td>
                  <CellValue>
                    <span className="field-hint">Emitted playback URL</span>
                    {channel.excluded ? (
                      <span className="subtle">not emitted</span>
                    ) : (
                      <Url value={exportedPlayback} />
                    )}
                  </CellValue>
                </td>
              </tr>
              <tr>
                <th scope="row">Logo</th>
                <td>
                  <CellValue>
                    <span className="field-hint">Provider artwork URL</span>
                    <LogoPreview src={providerLogo} />
                    <Url value={providerLogo} />
                  </CellValue>
                </td>
                <td>
                  <CellValue>
                    <span className="field-hint">Exported tvg-logo / icon</span>
                    {channel.logo_error && (
                      <p className="compare-error" role="status">
                        {channel.logo_error}
                      </p>
                    )}
                    {exportedLogo && exportedLogo !== providerLogo ? (
                      <LogoPreview src={exportedLogo} />
                    ) : null}
                    {exportedLogo ? (
                      <Url value={exportedLogo} />
                    ) : (
                      <span className="subtle">
                        {channel.logo_error ? 'cleared (not exported)' : '—'}
                      </span>
                    )}
                  </CellValue>
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </section>

      <section className="detail-section">
        <h2>Health / probes</h2>
        <div className="health-current">
          <div className="badge-row">
            <span className={`badge badge-${hb.kind}`}>{hb.label}</span>
          </div>
          <dl className="health-meta">
            <div>
              <dt>Last check</dt>
              <dd>{formatHealthWhen(channel.health?.last_check_at)}</dd>
            </div>
            <div>
              <dt>Last result</dt>
              <dd>{channel.health?.last_check || '—'}</dd>
            </div>
            <div>
              <dt>HTTP status</dt>
              <dd>
                {channel.health?.last_http_status
                  ? channel.health.last_http_status
                  : '—'}
              </dd>
            </div>
            <div>
              <dt>Duration</dt>
              <dd>
                {channel.health?.last_duration_ms
                  ? `${channel.health.last_duration_ms} ms`
                  : '—'}
              </dd>
            </div>
            <div>
              <dt>Bytes read</dt>
              <dd>
                {channel.health?.last_bytes_read
                  ? channel.health.last_bytes_read
                  : '—'}
              </dd>
            </div>
            <div>
              <dt>Range</dt>
              <dd>
                {channel.health?.last_range_retried
                  ? 'retried (416→plain)'
                  : channel.health?.last_range_used
                    ? 'used'
                    : '—'}
              </dd>
            </div>
            <div className="health-meta-wide">
              <dt>Final URL</dt>
              <dd>
                {channel.health?.last_final_url ? (
                  <code className="url-break">{channel.health.last_final_url}</code>
                ) : (
                  '—'
                )}
              </dd>
            </div>
            <div>
              <dt>Failure streak</dt>
              <dd>{channel.health?.consecutive_failures ?? 0}</dd>
            </div>
            <div>
              <dt>Failure class</dt>
              <dd>{channel.health?.last_failure_class || '—'}</dd>
            </div>
            <div className="health-meta-wide">
              <dt>Failure detail</dt>
              <dd>
                {channel.health?.last_failure_detail ? (
                  <code className="url-break">{channel.health.last_failure_detail}</code>
                ) : (
                  '—'
                )}
              </dd>
            </div>
            <div>
              <dt>30-day success</dt>
              <dd>
                {history?.success_rate_30d == null
                  ? '—'
                  : `${Math.round(history.success_rate_30d * 100)}%`}
              </dd>
            </div>
            <div>
              <dt>Next L2 sweep</dt>
              <dd>
                {channel.classification === 'NATIVE'
                  ? schedule?.l2_running
                    ? 'running now'
                    : formatHealthWhen(schedule?.next_l2_at)
                  : 'not scheduled (L2 is NATIVE-only)'}
              </dd>
            </div>
            <div>
              <dt>Last L2 sweep</dt>
              <dd>
                {channel.classification === 'NATIVE'
                  ? formatHealthWhen(schedule?.last_l2_at)
                  : '—'}
              </dd>
            </div>
            {schedule?.l3_enabled ? (
              <>
                <div>
                  <dt>Next L3 sweep</dt>
                  <dd>
                    {schedule.l3_running
                      ? 'running now'
                      : formatHealthWhen(schedule.next_l3_at)}
                  </dd>
                </div>
                <div>
                  <dt>Last L3 sweep</dt>
                  <dd>{formatHealthWhen(schedule.last_l3_at)}</dd>
                </div>
              </>
            ) : (
              <div>
                <dt>Scheduled L3</dt>
                <dd>off (Test now still runs one L3)</dd>
              </div>
            )}
          </dl>
          <p className="probe-actions">
            <button
              type="button"
              onClick={() => runProbe('l2')}
              disabled={probeBusy !== null}
            >
              {probeBusy === 'l2' ? 'Probing L2…' : 'Probe L2'}
            </button>
            <button
              type="button"
              onClick={() => runProbe('l3')}
              disabled={probeBusy !== null}
            >
              {probeBusy === 'l3' ? 'Probing L3…' : 'Test now (L3)'}
            </button>
            <span className="meta">
              {' '}
              L2 = first media segment; L3 = ffprobe decode.
            </span>
          </p>
          {probeNote ? <p className="meta" role="status">{probeNote}</p> : null}
          {probeError ? (
            <p className="compare-error" role="alert">
              {probeError}
            </p>
          ) : null}
        </div>

        <h3>History</h3>
        {historyError ? (
          <p className="compare-error" role="alert">
            {historyError}
          </p>
        ) : null}
        {!historyError && !history ? <p className="meta">Loading history…</p> : null}
        {history && history.events.length === 0 ? (
          <p className="meta">No probe events yet.</p>
        ) : null}
        {history && history.events.length > 0 ? (
          <div className="table-wrap">
            <table className="compare-table history-table">
              <thead>
                <tr>
                  <th scope="col" className="history-expand-col">
                    <span className="visually-hidden">Expand</span>
                  </th>
                  <th scope="col">When</th>
                  <th scope="col">Source</th>
                  <th scope="col">Status</th>
                  <th scope="col">Check</th>
                  <th scope="col">HTTP</th>
                  <th scope="col">ms</th>
                  <th scope="col">Bytes</th>
                  <th scope="col">Failure</th>
                </tr>
              </thead>
              <tbody>
                {history.events.map((ev, i) => {
                  const h = parseHistoryValue(ev.value)
                  const rowBadge = healthBadge(h.status)
                  const rowKey = `${ev.at}-${i}`
                  const detail = h.last_failure_detail
                  const open = Boolean(detail && expandedHistory[rowKey])
                  return (
                    <Fragment key={rowKey}>
                      <tr className={detail ? 'history-row-expandable' : undefined}>
                        <td className="history-expand-col">
                          {detail ? (
                            <button
                              type="button"
                              className="history-expand-btn"
                              aria-expanded={open}
                              aria-controls={`history-detail-${i}`}
                              onClick={() =>
                                setExpandedHistory((prev) => ({
                                  ...prev,
                                  [rowKey]: !prev[rowKey],
                                }))
                              }
                            >
                              {open ? '▾' : '▸'}
                              <span className="visually-hidden">
                                {open ? 'Hide' : 'Show'} failure detail
                              </span>
                            </button>
                          ) : (
                            <span className="subtle">·</span>
                          )}
                        </td>
                        <td>{formatHealthWhen(ev.at)}</td>
                        <td>
                          <code>{ev.source || '—'}</code>
                        </td>
                        <td>
                          <span className={`badge badge-${rowBadge.kind}`}>
                            {rowBadge.label}
                          </span>
                        </td>
                        <td>{h.last_check || '—'}</td>
                        <td>
                          {h.last_http_status ? (
                            <code>{h.last_http_status}</code>
                          ) : (
                            '—'
                          )}
                        </td>
                        <td>
                          {h.last_duration_ms != null && h.last_duration_ms > 0
                            ? h.last_duration_ms
                            : '—'}
                        </td>
                        <td>
                          {h.last_bytes_read != null && h.last_bytes_read > 0
                            ? h.last_bytes_read
                            : '—'}
                        </td>
                        <td>
                          {h.last_failure_class || '—'}
                          {h.last_range_retried ? (
                            <span className="subtle"> · 416↻</span>
                          ) : null}
                        </td>
                      </tr>
                      {open && detail ? (
                        <tr id={`history-detail-${i}`} className="history-detail-row">
                          <td colSpan={9}>
                            <pre className="history-detail-body">
                              {h.last_final_url
                                ? `Final-URL: ${h.last_final_url}\n\n${detail}`
                                : detail}
                            </pre>
                          </td>
                        </tr>
                      ) : null}
                    </Fragment>
                  )
                })}
              </tbody>
            </table>
          </div>
        ) : null}
      </section>
    </>
  )
}
