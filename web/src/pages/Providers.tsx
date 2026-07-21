import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

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
        Configured lineup sources. Only providers with a registered adapter
        (currently LG) refresh into playlists and the channel table.
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
                <th>Refresh</th>
                <th>Offset</th>
                <th>Exclusions</th>
                <th>Notes</th>
              </tr>
            </thead>
            <tbody>
              {data.providers.length === 0 ? (
                <tr className="empty">
                  <td colSpan={7}>
                    No providers in config. Copy config.example.yaml to
                    /data/config.yaml and restart.
                  </td>
                </tr>
              ) : (
                data.providers.map((p) => (
                  <tr key={p.id}>
                    <td>
                      <Link to={`/providers/${encodeURIComponent(p.id)}`}>
                        <code>{p.id}</code>
                      </Link>
                    </td>
                    <td>{p.enabled ? 'yes' : 'no'}</td>
                    <td>{p.label || '—'}</td>
                    <td>{p.refresh_interval}</td>
                    <td>
                      {p.channel_number_offset > 0
                        ? p.channel_number_offset
                        : '—'}
                    </td>
                    <td>{p.exclusions?.length ?? 0}</td>
                    <td>{notesFor(p)}</td>
                  </tr>
                ))
              )}
            </tbody>
          </table>
        </div>
      )}
    </>
  )
}
