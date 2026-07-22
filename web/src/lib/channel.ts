/** Shared channel types and export-status helpers for list + detail pages. */

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

export type ExportKind = 'exported' | 'proxied' | 'filtered' | 'needs-proxy' | 'drm'

export function exportKind(ch: Channel): ExportKind {
  if (!ch.excluded) {
    return ch.emitted_url && ch.emitted_url !== ch.stream_url ? 'proxied' : 'exported'
  }
  if (ch.filter_reason === FILTER_REASON_NEEDS_PROXY) return 'needs-proxy'
  if (ch.filter_reason === FILTER_REASON_DRM || ch.classification === 'DRM') return 'drm'
  return 'filtered'
}

export function exportBadge(kind: ExportKind): { label: string; className: string } {
  switch (kind) {
    case 'exported':
      return { label: 'exported', className: 'badge-native' }
    case 'proxied':
      return { label: 'proxied', className: 'badge-beacon' }
    case 'needs-proxy':
      return { label: 'needs proxy', className: 'badge-drm' }
    case 'drm':
      return { label: 'DRM', className: 'badge-drm' }
    default:
      return { label: 'filtered', className: 'badge-none' }
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
