import { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'

type DirStats = { files: number; bytes: number }

type GenerationInfo = {
  name: string
  bytes: number
  files: number
  is_current: boolean
  is_staging: boolean
}

type ProviderInventory = {
  id: string
  current?: string
  generations: GenerationInfo[]
  logos: DirStats
  bytes_total: number
  orphan_staging: number
  known: boolean
}

type ChannelAttrStats = {
  db_path: string
  db_bytes: number
  current_rows: number
  event_rows: number
  kinds?: Record<string, number>
  oldest_event_at?: string
  newest_event_at?: string
  sibling_files?: { name: string; bytes: number }[]
}

type CacheResponse = {
  providers: ProviderInventory[]
  aggregate?: ProviderInventory
  bytes_total: number
  logo_bytes: number
  logo_files: number
  generation_count: number
  unknown_dirs?: string[]
  channelattr: ChannelAttrStats
}

type ClearStats = {
  deleted_files: number
  deleted_bytes: number
  refresh?: string
}

function formatBytes(n?: number): string {
  if (n == null || n <= 0) return '0 B'
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KiB`
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(1)} MiB`
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GiB`
}

function formatWhen(value?: string): string {
  if (!value) return '—'
  const date = new Date(value)
  if (Number.isNaN(date.getTime()) || date.getUTCFullYear() <= 1) return '—'
  return date.toLocaleString()
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="stat">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  )
}

export function CachePage() {
  const [data, setData] = useState<CacheResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [busy, setBusy] = useState<string | null>(null)
  const [note, setNote] = useState<string | null>(null)
  const [actionError, setActionError] = useState<string | null>(null)

  const load = useCallback(async () => {
    setError(null)
    try {
      const res = await fetch('/api/cache')
      if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
      setData((await res.json()) as CacheResponse)
    } catch (err: unknown) {
      setError(err instanceof Error ? err.message : String(err))
    }
  }, [])

  useEffect(() => {
    void load()
  }, [load])

  async function runAction(
    key: string,
    confirmMsg: string,
    fn: () => Promise<Response>,
  ) {
    if (!window.confirm(confirmMsg)) return
    setBusy(key)
    setNote(null)
    setActionError(null)
    try {
      const res = await fn()
      const body = (await res.json().catch(() => ({}))) as ClearStats & {
        message?: string
      }
      if (res.status === 409) {
        setActionError('Refresh already in progress')
        return
      }
      if (res.status === 403) {
        setActionError('Request blocked (same-origin check)')
        return
      }
      if (!res.ok) {
        throw new Error(
          typeof body === 'object' && body && 'message' in body
            ? String(body.message)
            : `${res.status} ${res.statusText}`,
        )
      }
      const files = body.deleted_files ?? 0
      const bytes = body.deleted_bytes ?? 0
      const refresh = body.refresh ? ` · refresh ${body.refresh}` : ''
      setNote(`Removed ${files} files (${formatBytes(bytes)})${refresh}`)
      await load()
    } catch (err: unknown) {
      setActionError(err instanceof Error ? err.message : String(err))
    } finally {
      setBusy(null)
    }
  }

  if (error) {
    return (
      <div className="empty-panel" role="alert">
        Failed to load cache inventory: {error}
      </div>
    )
  }
  if (!data) {
    return (
      <div className="empty-panel" role="status">
        Loading…
      </div>
    )
  }

  const attr = data.channelattr
  const rows: ProviderInventory[] = [
    ...(data.aggregate ? [data.aggregate] : []),
    ...data.providers,
  ]

  return (
    <>
      <div className="detail-heading">
        <div>
          <h1>Cache</h1>
          <p className="lead">
            On-disk generations, logos, and channel-attribute history. Soft purge
            keeps the serving generation until refresh commits a new one.
          </p>
        </div>
      </div>

      <p className="probe-actions">
        <button
          type="button"
          disabled={busy !== null}
          onClick={() => {
            void load()
          }}
        >
          Refresh inventory
        </button>
        <button
          type="button"
          disabled={busy !== null}
          onClick={() => {
            void runAction(
              'purge-all',
              'Purge all non-current cache generations, sweep orphans, and refresh enabled providers?',
              () => fetch('/api/cache/purge', { method: 'POST' }),
            )
          }}
        >
          {busy === 'purge-all' ? 'Purging…' : 'Purge all & refresh'}
        </button>
        <button
          type="button"
          disabled={busy !== null}
          onClick={() => {
            void runAction(
              'logos-all',
              'Clear all cached logos? They will re-warm if logo caching is enabled.',
              () => fetch('/api/logos', { method: 'DELETE' }),
            )
          }}
        >
          {busy === 'logos-all' ? 'Clearing…' : 'Clear all logos'}
        </button>
      </p>
      {note ? (
        <p className="meta" role="status">
          {note}
        </p>
      ) : null}
      {actionError ? (
        <div className="empty-panel" role="alert">
          <p>{actionError}</p>
        </div>
      ) : null}

      <section className="detail-section">
        <h2>Summary</h2>
        <div className="stat-grid">
          <Metric label="Cache total" value={formatBytes(data.bytes_total)} />
          <Metric label="Logo files" value={String(data.logo_files ?? 0)} />
          <Metric label="Logo size" value={formatBytes(data.logo_bytes)} />
          <Metric label="Generations" value={String(data.generation_count)} />
          <Metric label="Attr DB" value={formatBytes(attr.db_bytes)} />
          <Metric label="Attr current" value={String(attr.current_rows)} />
          <Metric label="Attr events" value={String(attr.event_rows)} />
        </div>
        <p className="meta">
          History span: {formatWhen(attr.oldest_event_at)} →{' '}
          {formatWhen(attr.newest_event_at)}
          {data.unknown_dirs && data.unknown_dirs.length > 0
            ? ` · Unknown dirs: ${data.unknown_dirs.join(', ')}`
            : null}
        </p>
      </section>

      <section className="detail-section">
        <h2>Providers</h2>
        <div className="table-wrap">
          <table className="channels">
            <thead>
              <tr>
                <th scope="col">Id</th>
                <th scope="col">Current</th>
                <th scope="col">Gens</th>
                <th scope="col">Gen size</th>
                <th scope="col">Logos</th>
                <th scope="col">Staging</th>
                <th scope="col">Actions</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((p) => {
                const genBytes = p.generations
                  .filter((g) => !g.is_staging)
                  .reduce((sum, g) => sum + g.bytes, 0)
                const genCount = p.generations.filter((g) => !g.is_staging).length
                const isAgg = p.id === 'aggregate'
                return (
                  <tr key={p.id}>
                    <td>
                      {isAgg ? (
                        <code>{p.id}</code>
                      ) : (
                        <Link to={`/providers/${encodeURIComponent(p.id)}`}>
                          <code>{p.id}</code>
                        </Link>
                      )}
                      {!p.known ? (
                        <span className="meta"> · unknown</span>
                      ) : null}
                    </td>
                    <td>
                      <code>{p.current || '—'}</code>
                    </td>
                    <td>{genCount}</td>
                    <td>{formatBytes(genBytes)}</td>
                    <td>
                      {p.logos.files} · {formatBytes(p.logos.bytes)}
                    </td>
                    <td>{p.orphan_staging || '—'}</td>
                    <td>
                      {isAgg ? (
                        <span className="meta">via purge all</span>
                      ) : (
                        <span className="probe-actions">
                          <button
                            type="button"
                            disabled={busy !== null}
                            onClick={() => {
                              void runAction(
                                `purge-${p.id}`,
                                `Soft-purge non-current generations for ${p.id} and refresh?`,
                                () =>
                                  fetch(
                                    `/api/providers/${encodeURIComponent(p.id)}/cache/purge`,
                                    { method: 'POST' },
                                  ),
                              )
                            }}
                          >
                            {busy === `purge-${p.id}` ? '…' : 'Purge & refresh'}
                          </button>
                          <button
                            type="button"
                            disabled={busy !== null}
                            onClick={() => {
                              void runAction(
                                `logos-${p.id}`,
                                `Clear logos for ${p.id}?`,
                                () =>
                                  fetch(
                                    `/api/logos/${encodeURIComponent(p.id)}`,
                                    { method: 'DELETE' },
                                  ),
                              )
                            }}
                          >
                            {busy === `logos-${p.id}` ? '…' : 'Clear logos'}
                          </button>
                        </span>
                      )}
                    </td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        </div>

        {rows.map((p) =>
          p.generations.length > 0 ? (
            <details key={`${p.id}-gens`} className="cache-gens">
              <summary>
                {p.id} generations ({p.generations.length})
              </summary>
              <ul className="meta">
                {p.generations.map((g) => (
                  <li key={g.name}>
                    <code>{g.name}</code>
                    {g.is_current ? ' · current' : null}
                    {g.is_staging ? ' · staging' : null}
                    {' · '}
                    {formatBytes(g.bytes)} · {g.files} files
                  </li>
                ))}
              </ul>
            </details>
          ) : null,
        )}
      </section>

      <section className="detail-section">
        <h2>Channel attributes</h2>
        <p className="meta">
          <code>{attr.db_path}</code> · {formatBytes(attr.db_bytes)} ·{' '}
          {attr.current_rows} current · {attr.event_rows} events
        </p>
        {attr.kinds && Object.keys(attr.kinds).length > 0 ? (
          <ul className="meta">
            {Object.entries(attr.kinds)
              .sort(([a], [b]) => a.localeCompare(b))
              .map(([kind, n]) => (
                <li key={kind}>
                  <code>{kind}</code>: {n}
                </li>
              ))}
          </ul>
        ) : null}
        {attr.sibling_files && attr.sibling_files.length > 0 ? (
          <ul className="meta">
            {attr.sibling_files.map((f) => (
              <li key={f.name}>
                <code>{f.name}</code>: {formatBytes(f.bytes)}
              </li>
            ))}
          </ul>
        ) : null}
        <p className="meta">
          Purge actions never delete channelattr — inventory only.
        </p>
      </section>
    </>
  )
}
