import { useCallback, useEffect, useMemo, useState, Fragment, type ReactNode } from 'react'
import { Link, useLocation, useParams } from 'react-router-dom'
import {
  channelOnScheduledL1,
  classBadge,
  displayNumber,
  formatHealthSource,
  formatHealthWhen,
  healthBadge,
  nextL1Label,
  lineupBadge,
} from '../lib/channel'
import type { Channel, ChannelEmit, ChannelHealth } from '../lib/channel'
import {
  channelsBackHref,
  type ChannelsLocationState,
} from '../lib/channelsFilters'
import {
  ConfigSaveError,
  fetchChannelEmit,
  reloadSummary,
  saveChannelEmit,
} from '../lib/channelEmit'
import { exportCategories } from '../lib/categoryStyle'
import {
  fetchChannelProgrammes,
  formatProgrammeRange,
  isProgrammeNow,
  programmeNext,
  programmeNow,
  type Programme,
} from '../lib/channelProgrammes'
import { ChannelPlayer } from '../components/ChannelPlayer'

type EmitDraft = {
  nameOn: boolean
  name: string
  groupOn: boolean
  group: string
  numberOn: boolean
  number: string
  logoOn: boolean
  logo: string
  exportOn: boolean
  exportMode: 'enabled' | 'disabled'
}

function draftFromChannel(ch: Channel): EmitDraft {
  const em = ch.emit
  const d = ch.emit_defaults
  return {
    nameOn: Boolean(em?.name),
    name: em?.name ?? d?.name ?? ch.emitted_name ?? ch.name ?? '',
    groupOn: Boolean(em?.group),
    group: em?.group ?? d?.group ?? ch.emitted_group ?? ch.group ?? '',
    numberOn: em?.number != null,
    number: String(em?.number ?? d?.number ?? ch.offset_number ?? 0),
    logoOn: Boolean(em?.logo_url),
    logo: em?.logo_url ?? d?.logo_url ?? ch.logo_url ?? '',
    exportOn: em?.export === 'enabled' || em?.export === 'disabled',
    exportMode: em?.export === 'disabled' ? 'disabled' : 'enabled',
  }
}

function emitPayload(draft: EmitDraft): ChannelEmit | null {
  const out: ChannelEmit = {}
  if (draft.nameOn) out.name = draft.name.trim()
  if (draft.groupOn) out.group = draft.group.trim()
  if (draft.numberOn) {
    const n = Number.parseInt(draft.number.trim(), 10)
    if (!Number.isFinite(n) || String(n) !== draft.number.trim()) {
      throw new Error('Channel number must be an integer')
    }
    out.number = n
  }
  if (draft.logoOn) out.logo_url = draft.logo.trim()
  if (draft.exportOn) out.export = draft.exportMode
  if (
    !out.name &&
    !out.group &&
    out.number == null &&
    !out.logo_url &&
    !out.export
  ) {
    return null
  }
  return out
}

function draftsEqual(a: EmitDraft, b: EmitDraft): boolean {
  return (
    a.nameOn === b.nameOn &&
    a.name === b.name &&
    a.groupOn === b.groupOn &&
    a.group === b.group &&
    a.numberOn === b.numberOn &&
    a.number === b.number &&
    a.logoOn === b.logoOn &&
    a.logo === b.logo &&
    a.exportOn === b.exportOn &&
    a.exportMode === b.exportMode
  )
}

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
  l1_interval: string
  last_l1_at?: string
  next_l1_at?: string
  l1_running?: boolean
  l2_enabled?: boolean
  l2_interval?: string
  last_l2_at?: string
  next_l2_at?: string
  l2_running?: boolean
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

function ProgrammeSlot({
  label,
  programme,
}: {
  label: string
  programme: Programme | null
}) {
  const cats = programme ? exportCategories(programme) : []
  return (
    <div className="guide-slot">
      <div className="guide-slot-label">{label}</div>
      {programme ? (
        <div className="guide-slot-body">
          <div className="guide-slot-title">{programme.title}</div>
          <div className="meta">{formatProgrammeRange(programme.start, programme.stop)}</div>
          {cats.length > 0 ? (
            <div className="epg-detail-cats">
              {cats.map((c) => (
                <span key={c} className="epg-cat-chip">
                  {c}
                </span>
              ))}
            </div>
          ) : null}
          {programme.desc ? (
            <p className="guide-slot-desc">{programme.desc}</p>
          ) : null}
        </div>
      ) : (
        <div className="subtle">—</div>
      )}
    </div>
  )
}

export function ChannelDetailPage() {
  const { provider = '', normalizedId = '' } = useParams()
  const location = useLocation()
  const channelsHref = channelsBackHref(
    (location.state as ChannelsLocationState | null)?.channelsSearch,
  )
  const [channel, setChannel] = useState<Channel | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [history, setHistory] = useState<HistoryResponse | null>(null)
  const [historyError, setHistoryError] = useState<string | null>(null)
  const [probeBusy, setProbeBusy] = useState<'l1' | 'l2' | null>(null)
  const [probeError, setProbeError] = useState<string | null>(null)
  const [probeNote, setProbeNote] = useState<string | null>(null)
  const [schedule, setSchedule] = useState<ProbeSchedule | null>(null)
  const [expandedHistory, setExpandedHistory] = useState<Record<string, boolean>>({})
  const [revision, setRevision] = useState('')
  const [writable, setWritable] = useState(false)
  const [emitDraft, setEmitDraft] = useState<EmitDraft | null>(null)
  const [emitBaseline, setEmitBaseline] = useState<EmitDraft | null>(null)
  const [emitSaving, setEmitSaving] = useState(false)
  const [emitToast, setEmitToast] = useState<{ ok: boolean; message: string } | null>(null)
  const [programmes, setProgrammes] = useState<Programme[] | null>(null)
  const [programmesError, setProgrammesError] = useState<string | null>(null)
  const [guideExpanded, setGuideExpanded] = useState(false)

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

  const loadChannel = useCallback(async () => {
    const body = await fetchChannelEmit(provider, normalizedId)
    setChannel(body.channel)
    setRevision(body.revision)
    setWritable(body.writable)
    const draft = draftFromChannel(body.channel)
    setEmitDraft(draft)
    setEmitBaseline(draft)
    setError(null)
    return body
  }, [provider, normalizedId])

  useEffect(() => {
    let cancelled = false
    loadChannel().catch((err: unknown) => {
      if (!cancelled) setError(err instanceof Error ? err.message : String(err))
    })
    return () => {
      cancelled = true
    }
  }, [loadChannel])

  useEffect(() => {
    loadHistory()
  }, [loadHistory])

  useEffect(() => {
    let cancelled = false
    setProgrammes(null)
    setProgrammesError(null)
    setGuideExpanded(false)
    fetchChannelProgrammes(provider, normalizedId)
      .then((list) => {
        if (!cancelled) {
          setProgrammes(list)
          setProgrammesError(null)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setProgrammes([])
          setProgrammesError(err instanceof Error ? err.message : String(err))
        }
      })
    return () => {
      cancelled = true
    }
  }, [provider, normalizedId])

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

  const emitDirty = useMemo(() => {
    if (!emitDraft || !emitBaseline) return false
    return !draftsEqual(emitDraft, emitBaseline)
  }, [emitDraft, emitBaseline])

  const guideNow = useMemo(
    () => (programmes ? programmeNow(programmes) : null),
    [programmes],
  )
  const guideNext = useMemo(
    () => (programmes ? programmeNext(programmes, new Date(), guideNow) : null),
    [programmes, guideNow],
  )

  async function saveEmit() {
    if (!emitDraft || !revision) return
    setEmitSaving(true)
    setEmitToast(null)
    try {
      const payload = emitPayload(emitDraft)
      const result = await saveChannelEmit(provider, normalizedId, revision, payload)
      setChannel(result.channel)
      setRevision(result.revision)
      setWritable(result.writable)
      const draft = draftFromChannel(result.channel)
      setEmitDraft(draft)
      setEmitBaseline(draft)
      setEmitToast(reloadSummary(result.reloads ?? []))
    } catch (err: unknown) {
      if (err instanceof ConfigSaveError && err.status === 409) {
        await loadChannel().catch(() => {})
        setEmitToast({
          ok: false,
          message: 'Config changed elsewhere — reloaded latest values; re-apply your edits.',
        })
      } else {
        setEmitToast({ ok: false, message: err instanceof Error ? err.message : String(err) })
      }
    } finally {
      setEmitSaving(false)
    }
  }

  async function runProbe(kind: 'l1' | 'l2') {
    setProbeBusy(kind)
    setProbeError(null)
    setProbeNote(null)
    const suffix = kind === 'l1' ? '/health/probe/l1' : '/health/probe'
    const path = `/api/channels/${encodeURIComponent(provider)}/${encodeURIComponent(normalizedId)}${suffix}`
    try {
      const res = await fetch(path, { method: 'POST' })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text || `${res.status} ${res.statusText}`)
      }
      const body = (await res.json()) as ProbeResponse
      setChannel((prev) => (prev ? { ...prev, health: body.health } : prev))
      const label = kind === 'l1' ? 'L1' : 'L2'
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
        <Link to={channelsHref} className="back-link">
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
  const nextL1 = nextL1Label(channel, schedule ?? undefined)
  const providerLogo = channel.logo_source_url || channel.logo_url
  const exportedPlayback = channel.excluded
    ? undefined
    : channel.emitted_url || channel.stream_url
  const exportedLogo = channel.logo_url || undefined
  const inLineup = status.kind === 'in-lineup' || status.kind === 'proxied'
  const defaults = channel.emit_defaults
  const setEmit = <K extends keyof EmitDraft>(key: K, value: EmitDraft[K]) =>
    setEmitDraft((prev) => (prev ? { ...prev, [key]: value } : prev))

  return (
    <>
      <Link to={channelsHref} className="back-link">
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
          {status.kind === 'disabled-group' ? (
            <p className="status-reason">
              This channel's group is disabled in the{' '}
              <Link to="/groups">Groups</Link> editor, so it is not emitted to
              the M3U/XMLTV. Re-enable the group to include it.
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

      <ChannelPlayer channel={channel} />

      <section className="detail-section">
        <h2>Provider vs Fastgen</h2>
        <p className="meta">
          Customize Fastgen export values for this channel. Uncheck to use the
          default fastgen produces. Does not change the upstream feed.
        </p>
        {!writable ? (
          <div className="empty-panel" role="alert">
            Config is read-only — mount the config path read-write to customize emit.
          </div>
        ) : null}
        <div className="config-toolbar">
          <button
            type="button"
            onClick={() => {
              void saveEmit()
            }}
            disabled={emitSaving || !writable || !emitDirty || !emitDraft}
          >
            {emitSaving ? 'Saving…' : 'Save & apply'}
          </button>
          <button
            type="button"
            className="button-secondary"
            onClick={() => emitBaseline && setEmitDraft(emitBaseline)}
            disabled={!emitDirty || !emitBaseline}
          >
            Discard
          </button>
          {emitToast ? (
            <span
              className={`meta config-toast${emitToast.ok ? '' : ' config-toast-error'}`}
              role="status"
            >
              {emitToast.message}
            </span>
          ) : null}
        </div>
        {channel.emit?.export === 'enabled' && channel.excluded && channel.filter_reason ? (
          <p className="status-reason" role="status">
            Cannot emit: {channel.filter_reason}
          </p>
        ) : null}
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
                <th scope="row">Name</th>
                <td>
                  <CellValue>
                    <span className="field-hint">Provider name</span>
                    <Plain value={channel.name} />
                  </CellValue>
                </td>
                <td>
                  <CellValue>
                    <label className="emit-customize">
                      <input
                        type="checkbox"
                        checked={emitDraft?.nameOn ?? false}
                        disabled={!writable || !emitDraft}
                        onChange={(e) => setEmit('nameOn', e.target.checked)}
                      />
                      Customize
                    </label>
                    {emitDraft?.nameOn ? (
                      <input
                        type="text"
                        value={emitDraft.name}
                        disabled={!writable}
                        onChange={(e) => setEmit('name', e.target.value)}
                      />
                    ) : (
                      <>
                        <span className="field-hint">Emitted display-name</span>
                        <Plain value={defaults?.name || channel.name} />
                      </>
                    )}
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
                    <label className="emit-customize">
                      <input
                        type="checkbox"
                        checked={emitDraft?.numberOn ?? false}
                        disabled={!writable || !emitDraft}
                        onChange={(e) => setEmit('numberOn', e.target.checked)}
                      />
                      Customize
                    </label>
                    {emitDraft?.numberOn ? (
                      <input
                        type="text"
                        inputMode="numeric"
                        value={emitDraft.number}
                        disabled={!writable}
                        onChange={(e) => setEmit('number', e.target.value)}
                      />
                    ) : (
                      <>
                        <span className="field-hint">Export number (tvg-chno / LCN)</span>
                        <Plain
                          value={displayNumber(defaults?.number ?? channel.offset_number)}
                        />
                      </>
                    )}
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
                    <label className="emit-customize">
                      <input
                        type="checkbox"
                        checked={emitDraft?.groupOn ?? false}
                        disabled={!writable || !emitDraft}
                        onChange={(e) => setEmit('groupOn', e.target.checked)}
                      />
                      Customize
                    </label>
                    {emitDraft?.groupOn ? (
                      <input
                        type="text"
                        value={emitDraft.group}
                        disabled={!writable}
                        onChange={(e) => setEmit('group', e.target.value)}
                      />
                    ) : (
                      <>
                        <span className="field-hint">group-title</span>
                        <Plain
                          value={
                            defaults?.group ||
                            channel.emitted_group ||
                            channel.group ||
                            undefined
                          }
                        />
                      </>
                    )}
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
                    <label className="emit-customize">
                      <input
                        type="checkbox"
                        checked={emitDraft?.logoOn ?? false}
                        disabled={!writable || !emitDraft}
                        onChange={(e) => setEmit('logoOn', e.target.checked)}
                      />
                      Customize
                    </label>
                    {emitDraft?.logoOn ? (
                      <input
                        type="text"
                        value={emitDraft.logo}
                        disabled={!writable}
                        onChange={(e) => setEmit('logo', e.target.value)}
                      />
                    ) : (
                      <>
                        <span className="field-hint">Exported tvg-logo / icon</span>
                        {channel.logo_error && (
                          <p className="compare-error" role="status">
                            {channel.logo_error}
                          </p>
                        )}
                        {(defaults?.logo_url || exportedLogo) &&
                        (defaults?.logo_url || exportedLogo) !== providerLogo ? (
                          <LogoPreview src={defaults?.logo_url || exportedLogo} />
                        ) : null}
                        {defaults?.logo_url || exportedLogo ? (
                          <Url value={defaults?.logo_url || exportedLogo} />
                        ) : (
                          <span className="subtle">
                            {channel.logo_error ? 'cleared (not exported)' : '—'}
                          </span>
                        )}
                      </>
                    )}
                  </CellValue>
                </td>
              </tr>
              <tr>
                <th scope="row">In export</th>
                <td>
                  <CellValue>
                    <span className="field-hint">Pipeline decision</span>
                    <Plain value={inLineup ? 'included' : 'excluded'} />
                  </CellValue>
                </td>
                <td>
                  <CellValue>
                    <label className="emit-customize">
                      <input
                        type="checkbox"
                        checked={emitDraft?.exportOn ?? false}
                        disabled={!writable || !emitDraft}
                        onChange={(e) => setEmit('exportOn', e.target.checked)}
                      />
                      Customize
                    </label>
                    {emitDraft?.exportOn ? (
                      <select
                        value={emitDraft.exportMode}
                        disabled={!writable}
                        onChange={(e) =>
                          setEmit(
                            'exportMode',
                            e.target.value === 'disabled' ? 'disabled' : 'enabled',
                          )
                        }
                      >
                        <option value="enabled">Include</option>
                        <option value="disabled">Exclude</option>
                      </select>
                    ) : (
                      <>
                        <span className="field-hint">auto (pipeline decides)</span>
                        <Plain value={inLineup ? 'included' : 'excluded'} />
                      </>
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
              <dt>{nextL1.title}</dt>
              <dd>{nextL1.value}</dd>
            </div>
            <div>
              <dt>Last L1 sweep</dt>
              <dd>
                {!channelOnScheduledL1(channel)
                  ? '—'
                  : formatHealthWhen(schedule?.last_l1_at)}
              </dd>
            </div>
            {schedule?.l2_enabled ? (
              <>
                <div>
                  <dt>Next L2 sweep</dt>
                  <dd>
                    {schedule.l2_running
                      ? 'running now'
                      : formatHealthWhen(schedule.next_l2_at)}
                  </dd>
                </div>
                <div>
                  <dt>Last L2 sweep</dt>
                  <dd>{formatHealthWhen(schedule.last_l2_at)}</dd>
                </div>
              </>
            ) : (
              <div>
                <dt>Scheduled L2</dt>
                <dd>off (Test now still runs one L2)</dd>
              </div>
            )}
          </dl>
          <p className="probe-actions">
            <button
              type="button"
              onClick={() => runProbe('l1')}
              disabled={probeBusy !== null}
            >
              {probeBusy === 'l1' ? 'Probing L1…' : 'Probe L1'}
            </button>
            <button
              type="button"
              onClick={() => runProbe('l2')}
              disabled={probeBusy !== null}
            >
              {probeBusy === 'l2' ? 'Probing L2…' : 'Test now (L2)'}
            </button>
            <span className="meta">
              {' '}
              Health L1 = first media segment; Health L2 = ffprobe decode.
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
        {history && (history.events?.length ?? 0) === 0 ? (
          <p className="meta">No probe events yet.</p>
        ) : null}
        {history && (history.events?.length ?? 0) > 0 ? (
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
                          <code title={ev.source || undefined}>
                            {formatHealthSource(ev.source)}
                          </code>
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

      <section className="detail-section">
        <h2>Guide</h2>
        <p className="meta">
          In-memory programmes for this channel (default window: last hour through
          next 12 hours).
        </p>
        {programmesError ? (
          <p className="compare-error" role="alert">
            Failed to load programmes: {programmesError}
          </p>
        ) : null}
        {programmes === null && !programmesError ? (
          <p className="meta" role="status">
            Loading programmes…
          </p>
        ) : null}
        {programmes && programmes.length === 0 && !programmesError ? (
          <p className="meta" role="status">
            No programmes in the current window for this channel.
          </p>
        ) : null}
        {programmes && programmes.length > 0 ? (
          <>
            <div className="guide-strip">
              <ProgrammeSlot label="Now" programme={guideNow} />
              <ProgrammeSlot label="Next" programme={guideNext} />
            </div>
            <p className="probe-actions">
              <button
                type="button"
                className="button-secondary"
                onClick={() => setGuideExpanded((v) => !v)}
              >
                {guideExpanded ? 'Hide programmes' : 'Show all programmes'}
              </button>
              <span className="meta"> {programmes.length} in window</span>
            </p>
            {guideExpanded ? (
              <ol className="guide-programme-list">
                {programmes.map((p) => {
                  const current = isProgrammeNow(p)
                  return (
                    <li
                      key={`${p.start}-${p.stop}-${p.title}`}
                      className={current ? 'guide-programme-now' : undefined}
                    >
                      <div className="guide-programme-row">
                        <div className="guide-programme-main">
                          {current ? (
                            <span className="badge badge-native">Now</span>
                          ) : null}{' '}
                          <span className="guide-slot-title">{p.title}</span>
                        </div>
                        <div className="meta">
                          {formatProgrammeRange(p.start, p.stop)}
                        </div>
                        {exportCategories(p).length > 0 ? (
                          <div className="epg-detail-cats">
                            {exportCategories(p).map((c) => (
                              <span key={c} className="epg-cat-chip">
                                {c}
                              </span>
                            ))}
                          </div>
                        ) : null}
                        {p.desc ? <p className="guide-slot-desc">{p.desc}</p> : null}
                      </div>
                    </li>
                  )
                })}
              </ol>
            ) : null}
          </>
        ) : null}
      </section>
    </>
  )
}
