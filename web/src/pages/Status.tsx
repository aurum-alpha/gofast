import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import {
  type Channel,
  canonicalClassification,
  healthStatus,
  lineupStatus,
} from '../lib/channel'

type HealthzProvider = {
  id: string
  label?: string
  stale: boolean
  exported_channels: number
  exported_programmes: number
}

type BuildVersion = {
  build?: string
  commit?: string
  built_at?: string
}

type HealthzResponse = {
  ok: boolean
  version?: BuildVersion
  providers?: HealthzProvider[]
}

type ApiStatusResponse = {
  ready: boolean
  version?: BuildVersion
  logos: {
    running: boolean
    done: number
    total: number
    provider?: string
  }
}

function formatBuildVersion(v?: BuildVersion): string {
  if (!v?.build) return '—'
  const commit = v.commit?.trim()
  return commit ? `build ${v.build} · ${commit}` : `build ${v.build}`
}

type ChannelsResponse = {
  channels: Channel[]
}

type HealthSchedule = {
  l1_interval?: string
  last_l1_at?: string
  next_l1_at?: string
  l1_running?: boolean
  l2_enabled?: boolean
  l2_interval?: string
  last_l2_at?: string
  next_l2_at?: string
  l2_running?: boolean
}

type ClientAccessSummary = {
  file: string
  hits_30d: number
  last_at?: string
  last_ip?: string
  last_status?: number
}

type ClientAccessResponse = {
  summary: ClientAccessSummary[]
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
  recent?: ProxyEvent[]
  recent_failures?: ProxyEvent[]
}

function validDate(value?: string): Date | null {
  if (!value) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1 ? null : date
}

function formatWhen(value?: string): string {
  const date = validDate(value)
  if (!date) return '—'
  return date.toLocaleString()
}

function formatAge(value?: string): string {
  const date = validDate(value)
  if (!date) return '—'
  const sec = Math.max(0, Math.round((Date.now() - date.getTime()) / 1000))
  if (sec < 60) return `${sec}s ago`
  const min = Math.round(sec / 60)
  if (min < 60) return `${min}m ago`
  const hr = Math.round(min / 60)
  return `${hr}h ago`
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

export function StatusPage() {
  const [healthz, setHealthz] = useState<HealthzResponse | null>(null)
  const [boot, setBoot] = useState<ApiStatusResponse | null>(null)
  const [channels, setChannels] = useState<Channel[] | null>(null)
  const [schedule, setSchedule] = useState<HealthSchedule | null>(null)
  const [access, setAccess] = useState<ClientAccessResponse | null>(null)
  const [proxyStatus, setProxyStatus] = useState<ProxyStatusResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    let timer: number | undefined

    const poll = () => {
      Promise.all([
        fetch('/healthz').then(async (res) => {
          if (!res.ok) throw new Error(`healthz ${res.status}`)
          return res.json() as Promise<HealthzResponse>
        }),
        fetch('/api/status').then(async (res) => {
          if (!res.ok) throw new Error(`status ${res.status}`)
          return res.json() as Promise<ApiStatusResponse>
        }),
        fetch('/api/channels').then(async (res) => {
          if (!res.ok) throw new Error(`channels ${res.status}`)
          return res.json() as Promise<ChannelsResponse>
        }),
        fetch('/api/health/schedule').then(async (res) => {
          if (res.status === 503) return null
          if (!res.ok) throw new Error(`schedule ${res.status}`)
          return res.json() as Promise<HealthSchedule>
        }),
        fetch('/api/client-access').then(async (res) => {
          if (!res.ok) throw new Error(`client-access ${res.status}`)
          return res.json() as Promise<ClientAccessResponse>
        }),
        fetch('/api/proxy/status').then(async (res) => {
          if (res.status === 503) return null
          if (!res.ok) throw new Error(`proxy-status ${res.status}`)
          return res.json() as Promise<ProxyStatusResponse>
        }),
      ])
        .then(([hz, st, ch, sched, ca, px]) => {
          if (cancelled) return
          setHealthz(hz)
          setBoot(st)
          setChannels(ch.channels)
          setSchedule(sched)
          setAccess(ca)
          setProxyStatus(px)
          setError(null)
          const delay = st.logos.running ? 1000 : 5000
          timer = window.setTimeout(poll, delay)
        })
        .catch((err: unknown) => {
          if (cancelled) return
          setError(err instanceof Error ? err.message : String(err))
          timer = window.setTimeout(poll, 5000)
        })
    }
    poll()
    return () => {
      cancelled = true
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [])

  const providers = healthz?.providers ?? []
  const staleCount = providers.filter((p) => p.stale).length
  const logosRunning = Boolean(boot?.logos.running)

  const rollups = useMemo(() => {
    const health = { healthy: 0, degraded: 0, down: 0, untested: 0 }
    const lineup = {
      'in-lineup': 0,
      proxied: 0,
      'needs-proxy': 0,
      drm: 0,
      'disabled-group': 0,
      excluded: 0,
    }
    const dialect = {
      NATIVE: 0,
      AMAGI_SSAI: 0,
      SESSION: 0,
      XUMO_SSAI: 0,
      DRM: 0,
      other: 0,
    }
    if (!channels) {
      return { health, lineup, dialect, total: 0 }
    }
    for (const ch of channels) {
      health[healthStatus(ch)]++
      lineup[lineupStatus(ch)]++
      const cls = canonicalClassification(ch.classification)
      if (cls in dialect) {
        dialect[cls as keyof typeof dialect]++
      } else if (cls) {
        dialect.other++
      } else {
        dialect.other++
      }
    }
    return { health, lineup, dialect, total: channels.length }
  }, [channels])

  return (
    <>
      <h1>Status</h1>
      <p className="lead">
        Jellyfin-facing ops snapshot: process health, lineup problems, and probe
        schedule. Per-provider detail and Refresh live on{' '}
        <Link to="/providers">Providers</Link>.
      </p>

      {error && (
        <div className="empty-panel" role="alert">
          <p>Failed to load status: {error}</p>
        </div>
      )}

      {!error && !healthz && (
        <div className="empty-panel" role="status">
          <p>Loading…</p>
        </div>
      )}

      {healthz && (
        <>
          <h2 className="status-section-title">System</h2>
          <div className="stat-grid">
            <Metric
              label="Process"
              value={healthz.ok ? 'up' : 'down'}
              title="HTTP process is serving — not all-providers-healthy"
            />
            <Metric
              label="Build"
              value={formatBuildVersion(healthz.version)}
              title={
                healthz.version?.built_at
                  ? `Built ${healthz.version.built_at}`
                  : 'CI run number + short git SHA (ldflags)'
              }
            />
            <Metric
              label="Logo warm"
              value={
                logosRunning
                  ? boot!.logos.total > 0
                    ? `${boot!.logos.done}/${boot!.logos.total}${boot!.logos.provider ? ` · ${boot!.logos.provider}` : ''}`
                    : 'running'
                  : boot?.ready
                    ? 'idle'
                    : '—'
              }
            />
            <Metric
              label="Providers stale"
              value={`${staleCount} / ${providers.length}`}
              warn={staleCount > 0}
            />
            <Metric label="Channels in memory" value={rollups.total.toLocaleString()} />
          </div>
          <p className="meta">
            Process <code>ok</code> means fastgen is up. Stale providers keep
            last-known-good and do not flip the process down.
          </p>

          <h2 className="status-section-title">Lineup problems</h2>
          <p className="meta">
            Live channel rollup for Jellyfin risk.{' '}
            <Link to="/">Open Channels</Link> to filter and inspect.
          </p>
          <div className="stat-grid">
            <Metric
              label="Health down"
              value={rollups.health.down.toLocaleString()}
              warn={rollups.health.down > 0}
            />
            <Metric
              label="Health degraded"
              value={rollups.health.degraded.toLocaleString()}
              warn={rollups.health.degraded > 0}
            />
            <Metric
              label="Health untested"
              value={rollups.health.untested.toLocaleString()}
            />
            <Metric
              label="Health healthy"
              value={rollups.health.healthy.toLocaleString()}
            />
            <Metric
              label="Needs proxy"
              value={rollups.lineup['needs-proxy'].toLocaleString()}
              warn={rollups.lineup['needs-proxy'] > 0}
              title="Blocked from export until FASTProxy / proxy_base_url"
            />
            <Metric
              label="DRM blocked"
              value={rollups.lineup.drm.toLocaleString()}
              warn={rollups.lineup.drm > 0}
            />
            <Metric
              label="In lineup"
              value={(
                rollups.lineup['in-lineup'] + rollups.lineup.proxied
              ).toLocaleString()}
            />
            <Metric
              label="Disabled group"
              value={rollups.lineup['disabled-group'].toLocaleString()}
              title="Dropped because their group is disabled in the taxonomy"
            />
            <Metric
              label="Excluded (other)"
              value={rollups.lineup.excluded.toLocaleString()}
            />
          </div>

          <h2 className="status-section-title">Dialects</h2>
          <div className="stat-grid">
            <Metric label="NATIVE" value={rollups.dialect.NATIVE.toLocaleString()} />
            <Metric
              label="Amagi SSAI"
              value={rollups.dialect.AMAGI_SSAI.toLocaleString()}
              warn={rollups.dialect.AMAGI_SSAI > 0}
              title="Usually need FASTProxy for playback"
            />
            <Metric label="SESSION" value={rollups.dialect.SESSION.toLocaleString()} />
            <Metric
              label="Xumo SSAI"
              value={rollups.dialect.XUMO_SSAI.toLocaleString()}
            />
            <Metric label="DRM" value={rollups.dialect.DRM.toLocaleString()} warn={rollups.dialect.DRM > 0} />
          </div>

          <h2 className="status-section-title">Health probes</h2>
          {schedule ? (
            <div className="stat-grid">
              <Metric
                label="L1 segment"
                value={
                  schedule.l1_running
                    ? 'running'
                    : `next ${formatWhen(schedule.next_l1_at)}`
                }
                title={`Last ${formatWhen(schedule.last_l1_at)} · interval ${schedule.l1_interval || '—'}`}
              />
              <Metric
                label="L2 ffprobe"
                value={
                  !schedule.l2_enabled
                    ? 'off'
                    : schedule.l2_running
                      ? 'running'
                      : `next ${formatWhen(schedule.next_l2_at)}`
                }
                title={
                  schedule.l2_enabled
                    ? `Last ${formatWhen(schedule.last_l2_at)} · interval ${schedule.l2_interval || '—'}`
                    : 'Scheduled L2 disabled; Test now still works on channel detail'
                }
              />
            </div>
          ) : (
            <p className="meta">Probe schedule unavailable.</p>
          )}

          <h2 className="status-section-title">Proxy</h2>
          <p className="meta">
            <Link to="/proxy">Open Proxy</Link>
            {' · '}
            Playlist/origin failures also update channel health (
            <code>source=playback</code>).
          </p>
          {!proxyStatus ||
          (!proxyStatus.snapshot && (proxyStatus.heartbeat_count ?? 0) === 0) ? (
            <div className="empty-panel" role="status">
              <p>
                No proxy heartbeat yet. Enable the compose proxy profile and set
                proxy_base_url.
              </p>
            </div>
          ) : (
            <div className="stat-grid">
              <Metric
                label="Heartbeat age"
                value={formatAge(proxyStatus.heartbeat)}
                warn={Boolean(proxyStatus.stale)}
                title={
                  proxyStatus.stale
                    ? 'Snapshot older than 2 minutes'
                    : formatWhen(proxyStatus.heartbeat)
                }
              />
              <Metric
                label="Heartbeats"
                value={(proxyStatus.heartbeat_count ?? 0).toLocaleString()}
              />
              <Metric
                label="Sessions"
                value={proxyStatus.snapshot?.active_sessions ?? 0}
              />
              <Metric
                label="Playlist fail"
                value={proxyStatus.snapshot?.playlist_fail ?? 0}
                warn={(proxyStatus.snapshot?.playlist_fail ?? 0) > 0}
              />
              <Metric
                label="Seg fail"
                value={proxyStatus.snapshot?.seg_fail ?? 0}
                warn={(proxyStatus.snapshot?.seg_fail ?? 0) > 0}
              />
            </div>
          )}

          <h2 className="status-section-title">Client access</h2>
          <p className="meta">
            <Link to="/access">Pull history</Link>
          </p>
          {!access || access.summary.length === 0 ? (
            <div className="empty-panel" role="status">
              <p>No playlist/EPG pulls recorded yet.</p>
            </div>
          ) : (
            <div className="table-wrap">
              <table className="channels client-access-table">
                <thead>
                  <tr>
                    <th>File</th>
                    <th>Hits (30d)</th>
                    <th>Last at</th>
                    <th>Last IP</th>
                    <th>Status</th>
                  </tr>
                </thead>
                <tbody>
                  {access.summary.map((row) => (
                    <tr key={row.file}>
                      <td>
                        <code>{row.file}</code>
                      </td>
                      <td>{row.hits_30d.toLocaleString()}</td>
                      <td>{formatWhen(row.last_at)}</td>
                      <td>
                        <code>{row.last_ip || '—'}</code>
                      </td>
                      <td>{row.last_status ?? '—'}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}

          <h2 className="status-section-title">Providers</h2>
          <p className="meta">
            Compact refresh health. Open Providers for programmes, guide horizon,
            intervals, and Refresh now.
          </p>
          {providers.length === 0 ? (
            <div className="empty-panel" role="status">
              <p>No enabled providers.</p>
            </div>
          ) : (
            <div className="provider-chip-grid">
              {providers.map((p) => (
                <Link
                  key={p.id}
                  to={`/providers/${encodeURIComponent(p.id)}`}
                  className={`provider-chip${p.stale ? ' provider-chip-stale' : ''}`}
                >
                  <div className="provider-chip-head">
                    <strong>{p.label || p.id}</strong>
                    {p.stale ? (
                      <span className="badge badge-drm">stale</span>
                    ) : (
                      <span className="badge badge-native">ok</span>
                    )}
                  </div>
                  <code>{p.id}</code>
                  <div className="provider-chip-counts">
                    <span>{p.exported_channels.toLocaleString()} ch</span>
                    <span>{p.exported_programmes.toLocaleString()} prog</span>
                  </div>
                </Link>
              ))}
            </div>
          )}
        </>
      )}
    </>
  )
}
