/** Shared channel types and lineup-status helpers for list + detail pages. */

export type ChannelHealth = {
  status?: string
  consecutive_failures?: number
  last_check_at?: string
  last_check?: string
  last_failure_class?: string
  last_failure_detail?: string
  last_http_status?: number
  last_duration_ms?: number
  last_final_url?: string
  last_bytes_read?: number
  last_range_used?: boolean
  last_range_retried?: boolean
}

export type ChannelEmit = {
  export?: 'auto' | 'enabled' | 'disabled' | ''
  name?: string
  group?: string
  number?: number
  logo_url?: string
}

export type EmitDefaults = {
  name: string
  group: string
  number: number
  logo_url: string
}

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
  emitted_name?: string
  emitted_group?: string
  logo_url?: string
  logo_source_url?: string
  logo_error?: string
  classification?: string
  license_url?: string
  filter_reason?: string
  excluded: boolean
  description?: string
  health?: ChannelHealth
  emit?: ChannelEmit
  emit_defaults?: EmitDefaults
}

export const FILTER_REASON_DRM = 'DRM'
export const FILTER_REASON_NEEDS_PROXY =
  'needs FASTProxy (proxy_base_url not configured)'
/** Prefix of FilterReason set when a channel's group was disabled in the taxonomy. */
export const FILTER_REASON_DISABLED_GROUP = 'disabled group'

export type LineupStatusKind =
  | 'in-lineup'
  | 'proxied'
  | 'needs-proxy'
  | 'drm'
  | 'disabled-group'
  | 'excluded'

export type LineupBadge = {
  kind: LineupStatusKind
  label: string
  className: string
  title: string
}

export type HealthFilterValue = 'healthy' | 'degraded' | 'down' | 'untested'

export function healthStatus(ch: Channel): HealthFilterValue {
  const s = (ch.health?.status || '').toLowerCase()
  if (s === 'healthy' || s === 'degraded' || s === 'down') return s
  return 'untested'
}

export function healthBadge(status?: string): { label: string; kind: string } {
  switch ((status || '').toLowerCase()) {
    case 'healthy':
      return { label: 'healthy', kind: 'native' }
    case 'degraded':
      return { label: 'degraded', kind: 'beacon' }
    case 'down':
      return { label: 'down', kind: 'drm' }
    default:
      return { label: 'untested', kind: 'none' }
  }
}

/** Fixed Health filter options. */
export const HEALTH_FILTERS: { value: HealthFilterValue; label: string }[] = [
  { value: 'healthy', label: 'Healthy' },
  { value: 'degraded', label: 'Degraded' },
  { value: 'down', label: 'Down' },
  { value: 'untested', label: 'Untested' },
]

export function lineupStatus(ch: Channel): LineupStatusKind {
  if (!ch.excluded) {
    return ch.emitted_url && ch.emitted_url !== ch.stream_url ? 'proxied' : 'in-lineup'
  }
  if (ch.filter_reason === FILTER_REASON_NEEDS_PROXY) return 'needs-proxy'
  if (ch.filter_reason === FILTER_REASON_DRM || ch.classification === 'DRM') return 'drm'
  if (ch.filter_reason?.startsWith(FILTER_REASON_DISABLED_GROUP)) return 'disabled-group'
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
    case 'disabled-group':
      return {
        kind,
        label: 'Disabled group',
        className: 'badge-none',
        title: ch.filter_reason || 'Channel is in a disabled group',
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

/** Fixed Class filter options (stream dialect). */
export const CLASS_FILTERS = [
  'NATIVE',
  'AMAGI_SSAI',
  'SESSION',
  'XUMO_SSAI',
  'DRM',
] as const

/** Fixed Status filter options with display labels. */
export const STATUS_FILTERS: { value: LineupStatusKind; label: string }[] = [
  { value: 'in-lineup', label: 'In lineup' },
  { value: 'proxied', label: 'Via proxy' },
  { value: 'needs-proxy', label: 'Needs proxy' },
  { value: 'drm', label: 'DRM blocked' },
  { value: 'disabled-group', label: 'Disabled group' },
  { value: 'excluded', label: 'Excluded' },
]

/** Map legacy BEACON → AMAGI_SSAI for filters and display. */
export function canonicalClassification(classification?: string): string {
  if (!classification) return ''
  if (classification === 'BEACON') return 'AMAGI_SSAI'
  return classification
}

export function classBadge(classification?: string): { label: string; kind: string } {
  switch (canonicalClassification(classification)) {
    case 'NATIVE':
      return { label: 'NATIVE', kind: 'native' }
    case 'AMAGI_SSAI':
      return { label: 'Amagi SSAI', kind: 'beacon' }
    case 'SESSION':
      return { label: 'SESSION', kind: 'beacon' }
    case 'XUMO_SSAI':
      return { label: 'Xumo SSAI', kind: 'beacon' }
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

export function formatHealthWhen(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  // Go zero time ("0001-01-01…") and other garbage → em dash, not "12/31/1".
  if (Number.isNaN(d.getTime()) || d.getUTCFullYear() <= 1) return '—'
  return d.toLocaleString()
}
