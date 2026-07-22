/** Shared channel types and lineup-status helpers for list + detail pages. */

export type Channel = {
  provider: string
  id: string
  normalized_id: string
  name: string
  group: string
  number: number
  offset_number: number
  stream_url: string
  emitted_url?: string
  logo_url?: string
  logo_source_url?: string
  logo_error?: string
  classification?: string
  license_url?: string
  filter_reason?: string
  excluded: boolean
  description?: string
}

export const FILTER_REASON_DRM = 'DRM'
export const FILTER_REASON_NEEDS_PROXY =
  'needs FASTProxy (proxy_base_url not configured)'

export type LineupStatusKind =
  | 'in-lineup'
  | 'proxied'
  | 'needs-proxy'
  | 'drm'
  | 'excluded'

export type LineupBadge = {
  kind: LineupStatusKind
  label: string
  className: string
  title: string
}

export function lineupStatus(ch: Channel): LineupStatusKind {
  if (!ch.excluded) {
    return ch.emitted_url && ch.emitted_url !== ch.stream_url ? 'proxied' : 'in-lineup'
  }
  if (ch.filter_reason === FILTER_REASON_NEEDS_PROXY) return 'needs-proxy'
  if (ch.filter_reason === FILTER_REASON_DRM || ch.classification === 'DRM') return 'drm'
  return 'excluded'
}

export function lineupBadge(ch: Channel): LineupBadge {
  const kind = lineupStatus(ch)
  switch (kind) {
    case 'in-lineup':
      return {
        kind,
        label: 'In lineup',
        className: 'badge-native',
        title: 'Included in M3U / Jellyfin lineup',
      }
    case 'proxied':
      return {
        kind,
        label: 'Via proxy',
        className: 'badge-beacon',
        title: 'Included via FASTProxy playback URL',
      }
    case 'needs-proxy':
      return {
        kind,
        label: 'Needs proxy',
        className: 'badge-drm',
        title: ch.filter_reason || FILTER_REASON_NEEDS_PROXY,
      }
    case 'drm':
      return {
        kind,
        label: 'DRM blocked',
        className: 'badge-drm',
        title: ch.filter_reason || FILTER_REASON_DRM,
      }
    case 'excluded':
      return {
        kind,
        label: 'Excluded',
        className: 'badge-none',
        title: ch.filter_reason || 'Excluded from export',
      }
  }
}

export function classBadge(classification?: string): { label: string; kind: string } {
  switch (classification) {
    case 'NATIVE':
      return { label: 'NATIVE', kind: 'native' }
    case 'BEACON':
      return { label: 'BEACON', kind: 'beacon' }
    case 'DRM':
      return { label: 'DRM', kind: 'drm' }
    default:
      return { label: '—', kind: 'none' }
  }
}

export function displayNumber(n: number): string {
  return n > 0 ? String(n) : '—'
}

export function channelDetailPath(ch: Pick<Channel, 'provider' | 'normalized_id'>): string {
  return `/channels/${encodeURIComponent(ch.provider)}/${encodeURIComponent(ch.normalized_id)}`
}
