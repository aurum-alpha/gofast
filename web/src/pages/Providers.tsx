import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

type ProviderStats = {
  fetched_at?: string
  last_error?: string
  last_error_at?: string
  exported_channels?: number
  excluded_channels?: number
  total_channels?: number
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

function isStale(stats?: ProviderStats): boolean {
  return Boolean(stats?.last_error)
}

export function ProvidersPage() {
  const [data, setData] = useState<ProvidersResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    fetch('/api/providers')
      .then(async (res) => {
        if (!res.ok) {
          throw new Error(`${res.status} ${res.statusText}`)
        }
        return res.json() as Promise<ProvidersResponse>
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

  return (
    <>
      <h1>Providers</h1>
      <p className="lead">
        Configured lineup sources with triage counts. Open a provider for full
        rollups (classifications, filter reasons, guide coverage).
      </p>

      {error && (
        <div className="empty-panel" role="alert">
          <p>Failed to load providers: {error}</p>
        </div>
      )}

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
                <th>Exported</th>
                <th>Excluded</th>
                <th>Status</th>
                <th>Last success</th>
                <th>Notes</th>
              </tr>
            </thead>
            <tbody>
              {data.providers.length === 0 ? (
                <tr className="empty">
                  <td colSpan={8}>
                    No providers in config. Copy config.example.yaml to
                    /data/config.yaml and restart.
                  </td>
                </tr>
              ) : (
                data.providers.map((p) => {
                  const stale = isStale(p.stats)
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
                      <td>{notesFor(p)}</td>
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
