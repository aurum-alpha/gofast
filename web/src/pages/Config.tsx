import { useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { formatHealthWhen } from '../lib/channel'

type ArtworkTLS = {
  host: string
  ca_pem_set: boolean
  insecure_skip_verify: boolean
}

type ProviderSettings = {
  id: string
  enabled: boolean
  label: string
  channel_number_offset: number
  synthesize_channel_numbers: number
  min_channels: number
  refresh_interval: string
  region?: string
  exclusions?: string[]
}

type ActiveConfig = {
  source: { path: string; from_file: boolean }
  listen: string
  base_url: string
  data_dir: string
  proxy_base_url: string
  proxy_all: boolean
  cache_logos: boolean
  http_client_timeout: string
  log_level: string
  health: {
    consecutive_failures: number
    exclude_unhealthy: boolean
    l2_interval: string
    l2_workers: number
    l3_enabled: boolean
    l3_interval: string
    l3_workers: number
    l3_timeout: string
    l3_healthy_sample: number
    max_per_host: number
    soft_retries: number
    ffprobe_path: string
  }
  probe_schedule?: {
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
  artwork_tls: ArtworkTLS[]
  providers: ProviderSettings[]
}

function Bool({ value }: { value: boolean }) {
  return (
    <span className={`badge ${value ? 'badge-native' : 'badge-none'}`}>
      {value ? 'yes' : 'no'}
    </span>
  )
}

function Empty({ value }: { value?: string }) {
  if (!value) return <span className="subtle">—</span>
  return <code>{value}</code>
}

export function ConfigPage() {
  const [data, setData] = useState<ActiveConfig | null>(null)
  const [error, setError] = useState<string | null>(null)

  useEffect(() => {
    let cancelled = false
    fetch('/api/config')
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        return res.json() as Promise<ActiveConfig>
      })
      .then((body) => {
        if (!cancelled) {
          setData(body)
          setError(null)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      })
    return () => {
      cancelled = true
    }
  }, [])

  if (error) {
    return (
      <>
        <h1>Config</h1>
        <div className="empty-panel" role="alert">
          Failed to load config: {error}
        </div>
      </>
    )
  }
  if (!data) {
    return (
      <>
        <h1>Config</h1>
        <div className="empty-panel" role="status">
          Loading…
        </div>
      </>
    )
  }

  const { health } = data

  return (
    <>
      <h1>Config</h1>
      <p className="lead">
        Effective settings for this process (defaults → YAML → env). Read-only —
        edit <code>config.yaml</code> / env and restart to change.
      </p>

      <section className="detail-section">
        <h2>Source</h2>
        <dl className="settings-grid">
          <div>
            <dt>Config path</dt>
            <dd>
              <code>{data.source.path}</code>
            </dd>
          </div>
          <div>
            <dt>Loaded from file</dt>
            <dd>
              <Bool value={data.source.from_file} />
            </dd>
          </div>
        </dl>
      </section>

      <section className="detail-section">
        <h2>Deploy</h2>
        <dl className="settings-grid">
          <div>
            <dt>Listen</dt>
            <dd>
              <code>{data.listen}</code>
            </dd>
          </div>
          <div>
            <dt>Base URL</dt>
            <dd>
              <Empty value={data.base_url} />
            </dd>
          </div>
          <div>
            <dt>Data dir</dt>
            <dd>
              <code>{data.data_dir}</code>
            </dd>
          </div>
          <div>
            <dt>HTTP client timeout</dt>
            <dd>{data.http_client_timeout}</dd>
          </div>
          <div>
            <dt>Log level</dt>
            <dd>
              <code>{data.log_level}</code>
            </dd>
          </div>
        </dl>
      </section>

      <section className="detail-section">
        <h2>Proxy & logos</h2>
        <dl className="settings-grid">
          <div>
            <dt>Proxy base URL</dt>
            <dd>
              <Empty value={data.proxy_base_url} />
            </dd>
          </div>
          <div>
            <dt>Proxy all streams</dt>
            <dd>
              <Bool value={data.proxy_all} />
            </dd>
          </div>
          <div>
            <dt>Cache logos</dt>
            <dd>
              <Bool value={data.cache_logos} />
            </dd>
          </div>
        </dl>
      </section>

      <section className="detail-section">
        <h2>Health probes</h2>
        <dl className="settings-grid">
          <div>
            <dt>Consecutive failures → down</dt>
            <dd>{health.consecutive_failures}</dd>
          </div>
          <div>
            <dt>Exclude unhealthy</dt>
            <dd>
              <Bool value={health.exclude_unhealthy} />
            </dd>
          </div>
          <div>
            <dt>L2 interval</dt>
            <dd>{health.l2_interval}</dd>
          </div>
          <div>
            <dt>L2 workers</dt>
            <dd>{health.l2_workers}</dd>
          </div>
          <div>
            <dt>Soft retries</dt>
            <dd>{health.soft_retries}</dd>
          </div>
          <div>
            <dt>Max per host</dt>
            <dd>{health.max_per_host}</dd>
          </div>
          <div>
            <dt>L3 enabled</dt>
            <dd>
              <Bool value={health.l3_enabled} />
            </dd>
          </div>
          <div>
            <dt>L3 interval</dt>
            <dd>{health.l3_interval}</dd>
          </div>
          <div>
            <dt>L3 workers</dt>
            <dd>{health.l3_workers}</dd>
          </div>
          <div>
            <dt>L3 timeout</dt>
            <dd>{health.l3_timeout}</dd>
          </div>
          <div>
            <dt>L3 healthy sample</dt>
            <dd>{health.l3_healthy_sample}</dd>
          </div>
          <div>
            <dt>ffprobe path</dt>
            <dd>
              <code>{health.ffprobe_path}</code>
            </dd>
          </div>
          {data.probe_schedule ? (
            <>
              <div>
                <dt>Last L2 sweep</dt>
                <dd>{formatHealthWhen(data.probe_schedule.last_l2_at)}</dd>
              </div>
              <div>
                <dt>Next L2 sweep</dt>
                <dd>
                  {data.probe_schedule.l2_running
                    ? 'running now'
                    : formatHealthWhen(data.probe_schedule.next_l2_at)}
                </dd>
              </div>
              {data.probe_schedule.l3_enabled ? (
                <>
                  <div>
                    <dt>Last L3 sweep</dt>
                    <dd>{formatHealthWhen(data.probe_schedule.last_l3_at)}</dd>
                  </div>
                  <div>
                    <dt>Next L3 sweep</dt>
                    <dd>
                      {data.probe_schedule.l3_running
                        ? 'running now'
                        : formatHealthWhen(data.probe_schedule.next_l3_at)}
                    </dd>
                  </div>
                </>
              ) : null}
            </>
          ) : null}
        </dl>
      </section>

      <section className="detail-section">
        <h2>Artwork TLS</h2>
        {data.artwork_tls.length === 0 ? (
          <p className="meta">No per-host TLS exceptions.</p>
        ) : (
          <div className="table-wrap">
            <table className="channels">
              <thead>
                <tr>
                  <th scope="col">Host</th>
                  <th scope="col">CA PEM</th>
                  <th scope="col">Insecure skip verify</th>
                </tr>
              </thead>
              <tbody>
                {data.artwork_tls.map((row) => (
                  <tr key={row.host}>
                    <td>
                      <code>{row.host}</code>
                    </td>
                    <td>{row.ca_pem_set ? 'set' : '—'}</td>
                    <td>
                      <Bool value={row.insecure_skip_verify} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      <section className="detail-section">
        <h2>Providers</h2>
        <p className="meta">
          Effective settings after package defaults + YAML overlay. Open a row for
          triage stats.
        </p>
        <div className="table-wrap">
          <table className="channels">
            <thead>
              <tr>
                <th scope="col">ID</th>
                <th scope="col">Label</th>
                <th scope="col">Enabled</th>
                <th scope="col">Refresh</th>
                <th scope="col">Offset</th>
                <th scope="col">Min channels</th>
              </tr>
            </thead>
            <tbody>
              {data.providers.map((p) => (
                <tr key={p.id}>
                  <td>
                    <Link to={`/providers/${encodeURIComponent(p.id)}`}>
                      <code>{p.id}</code>
                    </Link>
                  </td>
                  <td>{p.label || '—'}</td>
                  <td>
                    <Bool value={p.enabled} />
                  </td>
                  <td>{p.refresh_interval || '—'}</td>
                  <td className="number-cell">{p.channel_number_offset || '—'}</td>
                  <td className="number-cell">{p.min_channels}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </>
  )
}
