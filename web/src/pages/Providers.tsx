import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

type ProviderStats = {
  fetched_at?: string
  last_attempt_at?: string
  last_error?: string
  last_error_at?: string
  exported_channels?: number
  excluded_channels?: number
  total_channels?: number
  exported_programmes?: number
  guide_hours_ahead?: number
  refresh_interval_configured?: string
  refresh_interval_effective?: string
  refresh_interval_clamped?: boolean
}

type ProviderRow = {
  id: string
  enabled: boolean
  label: string
  channel_number_offset: number
  synthesize_channel_numbers: number
  min_channels: number
  refresh_interval: string
  exclusions?: string[]
  slug_template?: string
  region?: string
  stats?: ProviderStats
}

type ProvidersResponse = {
  providers: ProviderRow[]
}

function notesFor(p: ProviderRow): string {
  const parts: string[] = []
  if (p.region) {
    parts.push(`region ${p.region}`)
  }
  if (p.slug_template) {
    parts.push(p.slug_template)
  }
  if (p.synthesize_channel_numbers > 0) {
    parts.push(`synthesize from ${p.synthesize_channel_numbers}`)
  }
  return parts.join(' · ') || '—'
}

function validDate(value?: string): Date | null {
  if (!value) return null
  const date = new Date(value)
  return Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1 ? null : date
}

function relativeTime(value?: string): string {
  const date = validDate(value)
  if (!date) return 'never'
  const seconds = Math.round((date.getTime() - Date.now()) / 1000)
  const formatter = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' })
  const units: Array<[Intl.RelativeTimeFormatUnit, number]> = [
    ['day', 86400],
    ['hour', 3600],
    ['minute', 60],
  ]
  for (const [unit, size] of units) {
    if (Math.abs(seconds) >= size) {
      return formatter.format(Math.round(seconds / size), unit)
    }
  }
  return formatter.format(seconds, 'second')
}

function guideLabel(hours?: number): string {
  if (hours === undefined || !Number.isFinite(hours) || hours <= 0) return '—'
  if (hours >= 48) return `${(hours / 24).toFixed(1)}d`
  return `${hours.toFixed(1)}h`
}

function isStale(stats?: ProviderStats): boolean {
  return Boolean(stats?.last_error)
}

export function ProvidersPage() {
  const [data, setData] = useState<ProvidersResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busyId, setBusyId] = useState<string | null>(null)
  const [refreshNote, setRefreshNote] = useState<string | null>(null)
  const [refreshError, setRefreshError] = useState<string | null>(null)

  const load = useCallback(async () => {
    const res = await fetch('/api/providers')
    if (!res.ok) {
      throw new Error(`${res.status} ${res.statusText}`)
    }
    const body = (await res.json()) as ProvidersResponse
    setData(body)
    setError(null)
    return body
  }, [])

  useEffect(() => {
    let cancelled = false
    load().catch((err: unknown) => {
      if (!cancelled) {
        setError(err instanceof Error ? err.message : String(err))
      }
    })
    return () => {
      cancelled = true
    }
  }, [load])

  async function refreshProvider(id: string) {
    setRefreshError(null)
    setRefreshNote(null)
    setBusyId(id)
    try {
      const res = await fetch(`/api/providers/${encodeURIComponent(id)}/refresh`, {
        method: 'POST',
      })
      if (res.status === 409) {
        setRefreshError(`${id}: refresh already in progress`)
        return
      }
      if (res.status === 404) {
        setRefreshError(`${id}: provider not found or disabled`)
        return
      }
      if (!res.ok) {
        throw new Error(`${res.status} ${res.statusText}`)
      }
      setRefreshNote(
        `${id}: refresh started (fetch + classify runs in the background)`,
      )
      window.setTimeout(() => {
        void load().catch(() => {})
      }, 2000)
      window.setTimeout(() => {
        void load().catch(() => {})
      }, 8000)
    } catch (err: unknown) {
      setRefreshError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusyId(null)
      try {
        await load()
      } catch {
        // keep list; load errors surface via initial error path
      }
    }
  }

  return (
    <>
      <h1>Providers</h1>
      <p className="lead">
        Configured lineup sources with triage counts. Open a provider for full
        rollups (classifications, filter reasons, guide coverage). Use Refresh
        to force a fetch + classify now (does not wait for the schedule).
      </p>

      {error && (
        <div className="empty-panel" role="alert">
          <p>Failed to load providers: {error}</p>
        </div>
      )}

      {refreshNote ? (
        <p className="meta" role="status">
          {refreshNote}
        </p>
      ) : null}
      {refreshError ? (
        <div className="empty-panel" role="alert">
          <p>{refreshError}</p>
        </div>
      ) : null}

      {!error && !data && (
        <div className="empty-panel" role="status">
          <p>Loading…</p>
        </div>
      )}

      {data && (
        <div className="table-wrap">
          <table className="channels">
            <thead>
              <tr>
                <th>ID</th>
                <th>Enabled</th>
                <th>Label</th>
                <th className="number-cell">Exported</th>
                <th className="number-cell">Programmes</th>
                <th className="number-cell">Excluded</th>
                <th>Status</th>
                <th>Last success</th>
                <th>Last attempt</th>
                <th className="number-cell">Guide</th>
                <th>Interval</th>
                <th>Notes</th>
                <th>Actions</th>
              </tr>
            </thead>
            <tbody>
              {data.providers.length === 0 ? (
                <tr className="empty">
                  <td colSpan={13}>
                    No providers in config. Copy config.example.yaml to
                    /data/config.yaml and restart.
                  </td>
                </tr>
              ) : (
                data.providers.map((p) => {
                  const stale = isStale(p.stats)
                  const busy = busyId === p.id
                  return (
                    <tr key={p.id} className={stale ? 'excluded' : undefined}>
                      <td>
                        <Link to={`/providers/${encodeURIComponent(p.id)}`}>
                          <code>{p.id}</code>
                        </Link>
                      </td>
                      <td>{p.enabled ? 'yes' : 'no'}</td>
                      <td>{p.label || '—'}</td>
                      <td className="number-cell">
                        {(p.stats?.exported_channels ?? 0).toLocaleString()}
                      </td>
                      <td className="number-cell">
                        {(p.stats?.exported_programmes ?? 0).toLocaleString()}
                      </td>
                      <td className="number-cell">
                        {(p.stats?.excluded_channels ?? 0).toLocaleString()}
                      </td>
                      <td>
                        {stale ? (
                          <span className="badge badge-drm" title={p.stats?.last_error}>
                            stale
                          </span>
                        ) : (
                          <span className="badge badge-native">ok</span>
                        )}
                      </td>
                      <td>{relativeTime(p.stats?.fetched_at)}</td>
                      <td>{relativeTime(p.stats?.last_attempt_at)}</td>
                      <td className="number-cell">
                        {guideLabel(p.stats?.guide_hours_ahead)}
                      </td>
                      <td>
                        <code>
                          {p.stats?.refresh_interval_effective ||
                            p.stats?.refresh_interval_configured ||
                            p.refresh_interval ||
                            '—'}
                        </code>
                        {p.stats?.refresh_interval_clamped ? (
                          <span
                            className="badge badge-beacon"
                            title={`Configured ${p.stats.refresh_interval_configured || '—'}`}
                          >
                            {' '}
                            clamped
                          </span>
                        ) : null}
                      </td>
                      <td>{notesFor(p)}</td>
                      <td>
                        <span className="probe-actions">
                          <button
                            type="button"
                            disabled={!p.enabled || busyId !== null}
                            onClick={() => {
                              void refreshProvider(p.id)
                            }}
                          >
                            {busy ? 'Starting…' : 'Refresh'}
                          </button>
                        </span>
                      </td>
                    </tr>
                  )
                })
              )}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
