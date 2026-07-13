import { useEffect, useState } from 'react'

type ProviderRow = {
  id: string
  enabled: boolean
  label: string
  chno_offset: number
  synthesize_chno: number
  min_channels: number
  refresh_interval: string
  exclusions: number
  slug_template?: string
  region?: string
}

type ProvidersResponse = {
  path: string
  from_file: boolean
  listen: string
  base_url: string
  data_dir: string
  providers: ProviderRow[]
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
        Loaded from runtime config. Adapters and refresh status land later; this
        page confirms what fastgen parsed from your YAML / env.
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
        <>
          <p className="meta">
            Config path <code>{data.path}</code>
            {data.from_file ? '' : ' (file missing — defaults + env only)'}
            {data.base_url ? (
              <>
                {' '}
                · base_url <code>{data.base_url}</code>
              </>
            ) : null}
          </p>

          <div className="table-wrap">
            <table className="channels">
              <thead>
                <tr>
                  <th>ID</th>
                  <th>Enabled</th>
                  <th>Label</th>
                  <th>Refresh</th>
                  <th>Chno</th>
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
                        <code>{p.id}</code>
                      </td>
                      <td>{p.enabled ? 'yes' : 'no'}</td>
                      <td>{p.label || '—'}</td>
                      <td>{p.refresh_interval}</td>
                      <td>
                        {p.synthesize_chno > 0
                          ? `synth ${p.synthesize_chno}`
                          : `offset ${p.chno_offset}`}
                      </td>
                      <td>{p.exclusions}</td>
                      <td>
                        {[p.region && `region ${p.region}`, p.slug_template]
                          .filter(Boolean)
                          .join(' · ') || '—'}
                      </td>
                    </tr>
                  ))
                )}
              </tbody>
            </table>
          </div>
        </>
      )}
    </>
  )
}
