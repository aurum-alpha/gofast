import { useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'

type ProviderSettings = {
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
}

type ProviderStats = {
  fetched_at: string
  last_attempt_at?: string
  last_error?: string
  last_error_at?: string
  total_channels: number
  exported_channels: number
  excluded_channels: number
  total_programmes: number
  exported_programmes: number
  by_classification: Record<string, number>
  by_group: Record<string, number>
  filter_reasons: Record<string, number>
  guide_start: string
  guide_end: string
  guide_hours_ahead?: number
  refresh_interval_configured?: string
  refresh_interval_effective?: string
  refresh_interval_clamped?: boolean
}

type ProviderDetail = {
  settings: ProviderSettings
  stats: ProviderStats
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

function guideCoverage(stats: ProviderStats): string {
  const start = validDate(stats.guide_start)
  const end = validDate(stats.guide_end)
  if (!start || !end) return 'No exported guide data'
  const hours = (end.getTime() - Date.now()) / 3_600_000
  const ahead =
    hours >= 48
      ? `${(hours / 24).toFixed(1)} days ahead`
      : `${Math.max(0, hours).toFixed(1)} hours ahead`
  return `${start.toLocaleString()} – ${end.toLocaleString()} · ${ahead}`
}

function Breakdown({
  title,
  values,
  classifications = false,
}: {
  title: string
  values: Record<string, number>
  classifications?: boolean
}) {
  const rows = useMemo(
    () => Object.entries(values).sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0])),
    [values],
  )
  return (
    <section>
      <h2>{title}</h2>
      <div className="table-wrap">
        <table className="channels">
          <tbody>
            {rows.length === 0 ? (
              <tr className="empty">
                <td>No data</td>
              </tr>
            ) : (
              rows.map(([name, count]) => (
                <tr key={name}>
                  <td>
                    {classifications ? (
                      <span className={`badge badge-${name.toLowerCase()}`}>{name}</span>
                    ) : (
                      name
                    )}
                  </td>
                  <td className="number-cell">{count.toLocaleString()}</td>
                </tr>
              ))
            )}
          </tbody>
        </table>
      </div>
    </section>
  )
}

export function ProviderDetailPage() {
  const { id = '' } = useParams()
  const [data, setData] = useState<ProviderDetail | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [refreshBusy, setRefreshBusy] = useState(false)
  const [refreshNote, setRefreshNote] = useState<string | null>(null)
  const [refreshError, setRefreshError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    setRefreshNote(null)
    setRefreshError(null)
    fetch(`/api/providers/${encodeURIComponent(id)}`)
      .then(async (response) => {
        if (!response.ok) throw new Error(`${response.status} ${response.statusText}`)
        return response.json() as Promise<ProviderDetail>
      })
      .then((detail) => {
        if (!cancelled) {
          setData(detail)
          setError(null)
        }
      })
      .catch((reason: unknown) => {
        if (!cancelled) setError(reason instanceof Error ? reason.message : String(reason))
      })
    return () => {
      cancelled = true
    }
  }, [id])

  async function refreshNow() {
    setRefreshBusy(true)
    setRefreshNote(null)
    setRefreshError(null)
    try {
      const res = await fetch(`/api/providers/${encodeURIComponent(id)}/refresh`, {
        method: 'POST',
      })
      if (res.status === 409) {
        setRefreshError('Refresh already in progress')
        return
      }
      if (res.status === 404) {
        setRefreshError('Provider not found or disabled')
        return
      }
      if (!res.ok) {
        throw new Error(`${res.status} ${res.statusText}`)
      }
      setRefreshNote('Refresh started — stats update when the fetch finishes')
      window.setTimeout(() => {
        void fetch(`/api/providers/${encodeURIComponent(id)}`)
          .then(async (r) => {
            if (!r.ok) return
            setData(await r.json())
          })
          .catch(() => {})
      }, 3000)
    } catch (err: unknown) {
      setRefreshError(err instanceof Error ? err.message : String(err))
    } finally {
      setRefreshBusy(false)
    }
  }

  if (error) {
    return (
      <>
        <Link to="/providers" className="back-link">← Providers</Link>
        <div className="empty-panel" role="alert">Failed to load provider: {error}</div>
      </>
    )
  }
  if (!data) return <div className="empty-panel" role="status">Loading…</div>

  const { settings, stats } = data
  return (
    <>
      <Link to="/providers" className="back-link">← Providers</Link>
      <div className="detail-heading">
        <div>
          <h1>{settings.label || settings.id}</h1>
          <p className="lead"><code>{settings.id}</code></p>
        </div>
        <span className={`badge ${settings.enabled ? 'badge-native' : 'badge-none'}`}>
          {settings.enabled ? 'Enabled' : 'Disabled'}
        </span>
      </div>

      <p className="probe-actions">
        <button
          type="button"
          disabled={!settings.enabled || refreshBusy}
          onClick={() => {
            void refreshNow()
          }}
        >
          {refreshBusy ? 'Starting…' : 'Refresh now'}
        </button>
        <Link to={`/config/providers/${encodeURIComponent(settings.id)}`}>
          Edit settings
        </Link>
      </p>
      {refreshNote ? <p className="meta" role="status">{refreshNote}</p> : null}
      {refreshError ? (
        <div className="empty-panel" role="alert">
          <p>{refreshError}</p>
        </div>
      ) : null}

      {stats.last_error && (
        <div className="error-banner" role="alert">
          <strong>Last refresh failed {relativeTime(stats.last_error_at)}</strong>
          <span>{stats.last_error}</span>
        </div>
      )}

      {stats.refresh_interval_clamped &&
        stats.refresh_interval_configured &&
        stats.refresh_interval_effective && (
        <div className="error-banner" role="status">
          <strong>
            Refresh interval adjusted for guide horizon:{' '}
            <code>{stats.refresh_interval_configured}</code>
            {' → '}
            <code>{stats.refresh_interval_effective}</code>
          </strong>
          <span>
            Capped to half the EPG ahead-horizon so the guide cannot expire before the next fetch.
          </span>
        </div>
      )}

      <div className="stat-grid">
        <div className="stat"><span>Last success</span><strong>{relativeTime(stats.fetched_at)}</strong></div>
        <div className="stat"><span>Last attempt</span><strong>{relativeTime(stats.last_attempt_at)}</strong></div>
        <div className="stat"><span>Channels</span><strong>{stats.total_channels.toLocaleString()}</strong></div>
        <div className="stat"><span>Exported</span><strong>{stats.exported_channels.toLocaleString()}</strong></div>
        <div className="stat"><span>Excluded</span><strong>{stats.excluded_channels.toLocaleString()}</strong></div>
        <div className="stat"><span>Programmes</span><strong>{stats.total_programmes.toLocaleString()}</strong></div>
        <div className="stat"><span>Exported programmes</span><strong>{stats.exported_programmes.toLocaleString()}</strong></div>
      </div>

      <section className="detail-section">
        <h2>Guide coverage</h2>
        <p className="meta">{guideCoverage(stats)}</p>
      </section>

      <section className="detail-section">
        <h2>Settings</h2>
        <p className="meta">
          <Link to={`/config/providers/${encodeURIComponent(settings.id)}`}>
            Edit settings
          </Link>
        </p>
        <dl className="settings-grid">
          <div><dt>Refresh interval</dt><dd>{settings.refresh_interval}</dd></div>
          <div>
            <dt>Channel offset</dt>
            <dd>
              {settings.channel_number_offset != null
                ? settings.channel_number_offset
                : '—'}
            </dd>
          </div>
          <div>
            <dt>Synthetic number base</dt>
            <dd>
              {settings.synthesize_channel_numbers != null
                ? settings.synthesize_channel_numbers
                : '—'}
            </dd>
          </div>
          <div><dt>Minimum channels</dt><dd>{settings.min_channels}</dd></div>
          <div><dt>Region</dt><dd>{settings.region || '—'}</dd></div>
          <div><dt>Slug template</dt><dd>{settings.slug_template || '—'}</dd></div>
          <div><dt>Exclusions</dt><dd>{settings.exclusions?.length ?? 0}</dd></div>
        </dl>
      </section>

      <div className="breakdown-grid">
        <Breakdown title="Classifications" values={stats.by_classification} classifications />
        <Breakdown title="Groups" values={stats.by_group} />
        <Breakdown title="Filter reasons" values={stats.filter_reasons} />
      </div>
    </>
  )
}

