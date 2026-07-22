import { useEffect, useState, type ReactNode } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  classBadge,
  displayNumber,
  exportBadge,
  exportKind,
  FILTER_REASON_NEEDS_PROXY,
} from '../lib/channel'
import type { Channel } from '../lib/channel'

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

export function ChannelDetailPage() {
  const { provider = '', normalizedId = '' } = useParams()
  const [channel, setChannel] = useState<Channel | null>(null)
  const [error, setError] = useState<string | null>(null)

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

  const kind = exportKind(channel)
  const exp = exportBadge(kind)
  const cls = classBadge(channel.classification)
  const providerLogo = channel.logo_source_url || channel.logo_url
  const exportedPlayback = channel.excluded
    ? undefined
    : channel.emitted_url || channel.stream_url
  const exportedLogo = channel.logo_url || undefined

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
        </div>
        <div className="badge-row">
          <span className={`badge badge-${cls.kind}`}>{cls.label}</span>
          <span className={`badge ${exp.className}`}>{exp.label}</span>
        </div>
      </div>

      <section className="detail-section">
        <h2>Export decision</h2>
        <p className="meta">
          {channel.excluded ? (
            <>
              Not in the M3U / Jellyfin lineup
              {channel.filter_reason ? (
                <>
                  {' '}
                  — <strong>{channel.filter_reason}</strong>
                </>
              ) : null}
              .
            </>
          ) : (
            <>Included in export ({exp.label}).</>
          )}
        </p>
        {channel.filter_reason === FILTER_REASON_NEEDS_PROXY && (
          <p className="meta">
            Configure <code>proxy_base_url</code> / FASTProxy so BEACON streams can be
            emitted.
          </p>
        )}
        {channel.license_url && (
          <p className="meta">
            DRM license evidence:{' '}
            <code className="url-break">{channel.license_url}</code>
          </p>
        )}
      </section>

      <section className="detail-section">
        <h2>Provider vs Fastgen</h2>
        <p className="lead compare-lead">
          Left is what the provider sent. Right is what Fastgen puts in playlists
          and guides (after normalize, offset, logo cache, and emission rules).
        </p>
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
        <p className="meta">Stream health probes land in M3 — not available yet.</p>
      </section>
    </>
  )
}
