import {
  canonicalClassification,
  lineupStatus,
  type Channel,
} from './channel'

export type PreviewSource = 'emitted' | 'raw'

export type PreviewURLs = {
  raw?: string
  emitted?: string
}

/** Resolve raw vs emitted playback URLs for the channel preview player. */
export function previewURLs(ch: Pick<Channel, 'stream_url' | 'emitted_url' | 'excluded'>): PreviewURLs {
  const raw = ch.stream_url?.trim() || undefined
  const emitted = ch.excluded ? undefined : ch.emitted_url?.trim() || undefined
  return { raw, emitted }
}

/** Whether both sources exist and differ (show the toggle). */
export function showPreviewSourceToggle(urls: PreviewURLs): boolean {
  return Boolean(urls.raw && urls.emitted && urls.raw !== urls.emitted)
}

/** Default source: emitted when available, otherwise raw. */
export function defaultPreviewSource(urls: PreviewURLs): PreviewSource {
  if (urls.emitted) return 'emitted'
  return 'raw'
}

export function previewURLForSource(urls: PreviewURLs, source: PreviewSource): string | undefined {
  return source === 'emitted' ? urls.emitted : urls.raw
}

/**
 * Static reason not to start the player. Empty string means the chosen URL
 * may be attempted (browser CORS/codec may still fail at runtime).
 */
export function previewBlockReason(
  ch: Pick<Channel, 'classification' | 'excluded' | 'filter_reason' | 'stream_url' | 'emitted_url'>,
  source: PreviewSource,
): string {
  if (canonicalClassification(ch.classification) === 'DRM') {
    return 'DRM channels cannot play in the browser preview.'
  }
  const urls = previewURLs(ch)
  const url = previewURLForSource(urls, source)
  if (!url) {
    if (source === 'emitted') {
      return ch.excluded
        ? 'Not emitted — switch to Raw to try the upstream URL, or include the channel first.'
        : 'No emitted playback URL for this channel.'
    }
    return 'No upstream stream URL.'
  }
  return ''
}

/** Soft warning when attempting raw while the lineup still needs FASTProxy. */
export function previewNeedsProxyWarning(
  ch: Pick<Channel, 'classification' | 'excluded' | 'filter_reason' | 'stream_url' | 'emitted_url'>,
  source: PreviewSource,
): string {
  if (source !== 'raw') return ''
  if (lineupStatus(ch) !== 'needs-proxy') return ''
  return 'This channel normally needs FASTProxy; raw upstream often fails in the browser (CORS / SSAI).'
}
