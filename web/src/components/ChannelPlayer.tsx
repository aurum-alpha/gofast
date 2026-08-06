import { useEffect, useRef, useState } from 'react'
import type HlsType from 'hls.js'
import type { Channel } from '../lib/channel'
import { fetchConfig } from '../lib/config'
import {
  browserPreviewURL,
  defaultPreviewSource,
  previewBlockReason,
  previewNeedsProxyWarning,
  previewURLs,
  showPreviewSourceToggle,
  type PreviewSource,
} from '../lib/channelPlayer'

type Props = {
  channel: Channel
}

function supportsNativeHLS(video: HTMLVideoElement): boolean {
  return video.canPlayType('application/vnd.apple.mpegurl') !== ''
}

export function ChannelPlayer({ channel }: Props) {
  const urls = previewURLs(channel)
  const canToggle = showPreviewSourceToggle(urls)
  const [source, setSource] = useState<PreviewSource>(() => defaultPreviewSource(urls))
  const [proxyBaseURL, setProxyBaseURL] = useState<string | undefined>()
  const [playing, setPlaying] = useState(false)
  const [error, setError] = useState('')
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const hlsRef = useRef<HlsType | null>(null)

  useEffect(() => {
    let cancelled = false
    void fetchConfig()
      .then((cfg) => {
        if (cancelled) return
        const v = cfg.fields.proxy_base_url?.value
        setProxyBaseURL(typeof v === 'string' && v.trim() ? v.trim() : undefined)
      })
      .catch(() => {
        if (!cancelled) setProxyBaseURL(undefined)
      })
    return () => {
      cancelled = true
    }
  }, [])

  function destroyHls() {
    if (hlsRef.current) {
      hlsRef.current.destroy()
      hlsRef.current = null
    }
  }

  function stopPlayback() {
    destroyHls()
    const video = videoRef.current
    if (video) {
      video.pause()
      video.removeAttribute('src')
      video.load()
    }
    setPlaying(false)
    setError('')
  }

  // Keep source valid and tear down playback when the channel identity changes.
  useEffect(() => {
    const next = previewURLs(channel)
    setSource((prev) => {
      if (prev === 'emitted' && next.emitted) return 'emitted'
      if (prev === 'raw' && next.raw) return 'raw'
      return defaultPreviewSource(next)
    })
    if (hlsRef.current) {
      hlsRef.current.destroy()
      hlsRef.current = null
    }
    const video = videoRef.current
    if (video) {
      video.pause()
      video.removeAttribute('src')
      video.load()
    }
    setPlaying(false)
    setError('')
  }, [
    channel.provider,
    channel.normalized_id,
    channel.stream_url,
    channel.emitted_url,
    channel.excluded,
  ])

  useEffect(() => {
    return () => {
      if (hlsRef.current) {
        hlsRef.current.destroy()
        hlsRef.current = null
      }
    }
  }, [])

  async function startPlayback() {
    const block = previewBlockReason(channel, source)
    if (block) {
      setError(block)
      setPlaying(false)
      return
    }
    const url = browserPreviewURL(channel, source, proxyBaseURL)
    if (!url) {
      setError('No stream URL.')
      return
    }
    const video = videoRef.current
    if (!video) return

    destroyHls()
    setError('')
    setPlaying(true)

    const { default: Hls } = await import('hls.js')
    // Prefer MSE/hls.js whenever available. Some Chromium embeds (incl. Cursor)
    // report canPlayType('application/vnd.apple.mpegurl') as "maybe" but cannot
    // demux HLS — that yields MEDIA_ERR_SRC_NOT_SUPPORTED / "no supported source".
    if (Hls.isSupported()) {
      const hls = new Hls({
        enableWorker: true,
        // Prefer the lowest rung first; ABR can still climb. Stable /seg tokens
        // (same upstream URL → same token) keep sliding playlists coherent.
        startLevel: 0,
        xhrSetup: (xhr) => {
          xhr.withCredentials = false
        },
      })
      hlsRef.current = hls
      hls.on(Hls.Events.ERROR, (_event, data) => {
        if (!data.fatal) return
        // Soft-recover media/level parse blips before hard-stopping the preview.
        if (
          data.details === Hls.ErrorDetails.LEVEL_PARSING_ERROR ||
          data.type === Hls.ErrorTypes.MEDIA_ERROR
        ) {
          try {
            hls.recoverMediaError()
            return
          } catch {
            // fall through to hard stop
          }
        }
        const detail =
          data.response?.code != null
            ? `${data.type} / ${data.details} (HTTP ${data.response.code})`
            : `${data.type} / ${data.details}`
        let message = detail
        if (
          data.details === Hls.ErrorDetails.MANIFEST_LOAD_ERROR ||
          data.details === Hls.ErrorDetails.MANIFEST_LOAD_TIMEOUT
        ) {
          message = proxyBaseURL
            ? `${detail}. Manifest fetch failed via FASTProxy — check proxy logs / origin.`
            : `${detail}. Often CORS — set proxy_base_url so preview can audition via FASTProxy, or open the URL in VLC.`
        }
        setError(message)
        destroyHls()
        setPlaying(false)
      })
      hls.loadSource(url)
      hls.attachMedia(video)
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        void video.play().catch((err: unknown) => {
          setError(err instanceof Error ? err.message : String(err))
        })
      })
      return
    }

    if (supportsNativeHLS(video)) {
      video.src = url
      try {
        await video.play()
      } catch (err: unknown) {
        setError(err instanceof Error ? err.message : String(err))
        setPlaying(false)
      }
      return
    }

    setError('HLS is not supported in this browser.')
    setPlaying(false)
  }

  const block = previewBlockReason(channel, source)
  const warn = previewNeedsProxyWarning(channel, source)
  const activeURL = browserPreviewURL(channel, source, proxyBaseURL)

  return (
    <section className="detail-section channel-preview">
      <h2>Preview</h2>
      <p className="meta">
        Audition HLS in the browser using the emitted playback URL or the raw
        upstream URL. Browser CORS and codecs differ from Jellyfin/VLC — prefer
        Emitted when the channel is via FASTProxy. Cross-origin NATIVE streams
        are routed through FASTProxy (<code>?browser=1</code>) when configured.
      </p>

      <div className="preview-toolbar">
        {canToggle ? (
          <div className="preview-source" role="group" aria-label="Stream source">
            <button
              type="button"
              className={source === 'emitted' ? undefined : 'button-secondary'}
              aria-pressed={source === 'emitted'}
              disabled={playing}
              onClick={() => {
                setSource('emitted')
                setError('')
              }}
            >
              Emitted
            </button>
            <button
              type="button"
              className={source === 'raw' ? undefined : 'button-secondary'}
              aria-pressed={source === 'raw'}
              disabled={playing}
              onClick={() => {
                setSource('raw')
                setError('')
              }}
            >
              Raw
            </button>
          </div>
        ) : (
          <span className="meta">
            Source: {urls.emitted ? 'Emitted' : urls.raw ? 'Raw' : 'none'}
          </span>
        )}
        {!playing ? (
          <button type="button" onClick={() => void startPlayback()} disabled={Boolean(block)}>
            Play
          </button>
        ) : (
          <button type="button" className="button-secondary" onClick={() => stopPlayback()}>
            Stop
          </button>
        )}
      </div>

      {activeURL ? (
        <p className="meta preview-url">
          <code className="url-break">{activeURL}</code>
        </p>
      ) : null}

      {block ? (
        <p className="status-reason" role="status">
          {block}
        </p>
      ) : null}
      {!block && warn ? (
        <p className="status-reason" role="status">
          {warn}
        </p>
      ) : null}
      {error ? (
        <p className="compare-error" role="alert">
          {error}
        </p>
      ) : null}

      <div className="preview-video-wrap">
        <video ref={videoRef} className="preview-video" controls playsInline />
      </div>
    </section>
  )
}
