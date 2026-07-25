import { useEffect, useRef, useState } from 'react'
import type HlsType from 'hls.js'
import type { Channel } from '../lib/channel'
import {
  defaultPreviewSource,
  previewBlockReason,
  previewNeedsProxyWarning,
  previewURLForSource,
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
  const [playing, setPlaying] = useState(false)
  const [error, setError] = useState('')
  const videoRef = useRef<HTMLVideoElement | null>(null)
  const hlsRef = useRef<HlsType | null>(null)

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
    const url = previewURLForSource(urls, source)
    if (!url) {
      setError('No stream URL.')
      return
    }
    const video = videoRef.current
    if (!video) return

    destroyHls()
    setError('')
    setPlaying(true)

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

    const { default: Hls } = await import('hls.js')
    if (!Hls.isSupported()) {
      setError('HLS is not supported in this browser.')
      setPlaying(false)
      return
    }

    const hls = new Hls({
      enableWorker: true,
      xhrSetup: (xhr) => {
        xhr.withCredentials = false
      },
    })
    hlsRef.current = hls
    hls.on(Hls.Events.ERROR, (_event, data) => {
      if (!data.fatal) return
      const detail =
        data.response?.code != null
          ? `${data.type} / ${data.details} (HTTP ${data.response.code})`
          : `${data.type} / ${data.details}`
      let message = detail
      if (
        data.details === Hls.ErrorDetails.MANIFEST_LOAD_ERROR ||
        data.details === Hls.ErrorDetails.MANIFEST_LOAD_TIMEOUT
      ) {
        message = `${detail}. Often CORS or network — try Emitted (proxy) if available, or open the URL in VLC.`
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
  }

  const block = previewBlockReason(channel, source)
  const warn = previewNeedsProxyWarning(channel, source)
  const activeURL = previewURLForSource(urls, source)

  return (
    <section className="detail-section channel-preview">
      <h2>Preview</h2>
      <p className="meta">
        Audition HLS in the browser using the emitted playback URL or the raw
        upstream URL. Browser CORS and codecs differ from Jellyfin/VLC — prefer
        Emitted when the channel is via FASTProxy.
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
