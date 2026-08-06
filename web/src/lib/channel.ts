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
  next_retry_at?: string
  retry_step?: number
}

export type ChannelEmit = {
  export?: 'auto' | 'enabled' | 'disabled' | ''
  dedupe?: boolean
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
  region?: string
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
  filter_reasons?: string[]
  excluded: boolean
  presence?: string
  description?: string
  health?: ChannelHealth
  emit?: ChannelEmit
  emit_defaults?: EmitDefaults
}

export const FILTER_REASON_DRM = 'DRM'
export const FILTER_REASON_NEEDS_PROXY =
  'needs FASTProxy (proxy_base_url not configured)'
export const FILTER_REASON_UNHEALTHY = 'unhealthy (exclude_unhealthy)'
export const FILTER_REASON_EMIT_DISABLED = 'emit disabled'
export const FILTER_REASON_DUPLICATE = 'duplicate'
export const FILTER_REASON_ABSENT = 'absent from provider'
export const FILTER_REASON_MISSING_IDENTITY = 'missing identity'
export const FILTER_REASON_MISSING_STREAM = 'missing stream'
export const FILTER_REASON_UNSUPPORTED = 'unsupported classification'
/** Prefix of FilterReason set when a channel's group was disabled in the taxonomy. */
export const FILTER_REASON_DISABLED_GROUP = 'disabled group'

export type LineupStatusKind =
  | 'in-lineup'
  | 'proxied'
  | 'needs-proxy'
  | 'drm'
  | 'unsupported'
  | 'duplicate'
  | 'absent'
  | 'disabled-group'
  | 'unhealthy'
  | 'emit-disabled'
  | 'exclusion'
  | 'missing-identity'
  | 'missing-stream'
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

/** Effective filter reason strings (multi or legacy single). */
export function effectiveFilterReasons(
  ch: Pick<Channel, 'filter_reason' | 'filter_reasons'>,
): string[] {
  if (ch.filter_reasons && ch.filter_reasons.length > 0) return ch.filter_reasons
  if (ch.filter_reason) return [ch.filter_reason]
  return []
}

/** Map one wire FilterReason string to a status kind. */
export function filterReasonKind(reason: string): LineupStatusKind {
  if (reason === FILTER_REASON_DRM) return 'drm'
  if (reason === FILTER_REASON_NEEDS_PROXY) return 'needs-proxy'
  if (reason === FILTER_REASON_UNHEALTHY) return 'unhealthy'
  if (reason === FILTER_REASON_EMIT_DISABLED) return 'emit-disabled'
  if (reason === FILTER_REASON_DUPLICATE) return 'duplicate'
  if (reason === FILTER_REASON_ABSENT) return 'absent'
  if (reason === FILTER_REASON_MISSING_IDENTITY) return 'missing-identity'
  if (reason === FILTER_REASON_MISSING_STREAM) return 'missing-stream'
  if (reason === FILTER_REASON_UNSUPPORTED) return 'unsupported'
  if (reason.startsWith(FILTER_REASON_DISABLED_GROUP)) return 'disabled-group'
  if (reason.startsWith('exclusion ')) return 'exclusion'
  return 'excluded'
}

const BADGE_BY_KIND: Record<
  Exclude<LineupStatusKind, 'in-lineup' | 'proxied'>,
  Omit<LineupBadge, 'title'> & { titleFor: (reason: string) => string }
> = {
  'needs-proxy': {
    kind: 'needs-proxy',
    label: 'Needs proxy',
    className: 'badge-drm',
    titleFor: (r) => r || FILTER_REASON_NEEDS_PROXY,
  },
  drm: {
    kind: 'drm',
    label: 'DRM blocked',
    className: 'badge-drm',
    titleFor: (r) => r || FILTER_REASON_DRM,
  },
  unsupported: {
    kind: 'unsupported',
    label: 'Unsupported',
    className: 'badge-drm',
    titleFor: (r) => r || FILTER_REASON_UNSUPPORTED,
  },
  duplicate: {
    kind: 'duplicate',
    label: 'Duplicate',
    className: 'badge-beacon',
    titleFor: (r) => r || 'Dropped by Dedupes',
  },
  absent: {
    kind: 'absent',
    label: 'Absent',
    className: 'badge-none',
    titleFor: (r) => r || FILTER_REASON_ABSENT,
  },
  'disabled-group': {
    kind: 'disabled-group',
    label: 'Disabled group',
    className: 'badge-none',
    titleFor: (r) => r || 'Channel is in a disabled group',
  },
  unhealthy: {
    kind: 'unhealthy',
    label: 'Unhealthy',
    className: 'badge-drm',
    titleFor: (r) => r || FILTER_REASON_UNHEALTHY,
  },
  'emit-disabled': {
    kind: 'emit-disabled',
    label: 'Emit disabled',
    className: 'badge-none',
    titleFor: (r) => r || FILTER_REASON_EMIT_DISABLED,
  },
  exclusion: {
    kind: 'exclusion',
    label: 'Regex exclusion',
    className: 'badge-none',
    titleFor: (r) => r || 'Matched an exclusion regex',
  },
  'missing-identity': {
    kind: 'missing-identity',
    label: 'Missing id',
    className: 'badge-drm',
    titleFor: (r) => r || FILTER_REASON_MISSING_IDENTITY,
  },
  'missing-stream': {
    kind: 'missing-stream',
    label: 'Missing stream',
    className: 'badge-drm',
    titleFor: (r) => r || FILTER_REASON_MISSING_STREAM,
  },
  excluded: {
    kind: 'excluded',
    label: 'Excluded',
    className: 'badge-none',
    titleFor: (r) => r || 'Excluded from export',
  },
}

/** All status kinds that apply (in-lineup/proxied XOR exclusion chips). */
export function lineupStatusKinds(
  ch: Pick<
    Channel,
    | 'excluded'
    | 'emitted_url'
    | 'stream_url'
    | 'filter_reason'
    | 'filter_reasons'
    | 'classification'
  >,
): LineupStatusKind[] {
  if (!ch.excluded) {
    return [
      ch.emitted_url && ch.emitted_url !== ch.stream_url ? 'proxied' : 'in-lineup',
    ]
  }
  const reasons = effectiveFilterReasons(ch)
  const kinds: LineupStatusKind[] = []
  const seen = new Set<LineupStatusKind>()
  for (const r of reasons) {
    const k = filterReasonKind(r)
    if (!seen.has(k)) {
      seen.add(k)
      kinds.push(k)
    }
  }
  if (kinds.length === 0) {
    if (ch.classification === 'DRM') return ['drm']
    return ['excluded']
  }
  return kinds
}

/** Primary status (first kind) — for sort / single-status callers. */
export function lineupStatus(
  ch: Pick<
    Channel,
    | 'excluded'
    | 'emitted_url'
    | 'stream_url'
    | 'filter_reason'
    | 'filter_reasons'
    | 'classification'
  >,
): LineupStatusKind {
  return lineupStatusKinds(ch)[0] ?? 'excluded'
}

export function lineupBadges(ch: Channel): LineupBadge[] {
  if (!ch.excluded) {
    const kind = lineupStatus(ch)
    if (kind === 'proxied') {
      return [
        {
          kind,
          label: 'Via proxy',
          className: 'badge-beacon',
          title: 'Included via FASTProxy playback URL',
        },
      ]
    }
    return [
      {
        kind: 'in-lineup',
        label: 'In lineup',
        className: 'badge-native',
        title: 'Included in M3U / Jellyfin lineup',
      },
    ]
  }
  const reasons = effectiveFilterReasons(ch)
  if (reasons.length === 0) {
    return [lineupBadge(ch)]
  }
  const out: LineupBadge[] = []
  const seen = new Set<LineupStatusKind>()
  for (const r of reasons) {
    const k = filterReasonKind(r)
    if (seen.has(k)) continue
    seen.add(k)
    if (k === 'in-lineup' || k === 'proxied') continue
    const meta = BADGE_BY_KIND[k]
    out.push({
      kind: k,
      label: meta.label,
      className: meta.className,
      title: meta.titleFor(r),
    })
  }
  return out
}

export function lineupBadge(ch: Channel): LineupBadge {
  return lineupBadges(ch)[0]
}

/** Fixed Class filter options (stream dialect). */
export const CLASS_FILTERS = [
  'NATIVE',
  'AMAGI_SSAI',
  'SESSION',
  'DISTRO_RESOLVE',
  'XUMO_SSAI',
  'DRM',
] as const

/** Fixed Status filter options with display labels. */
export const STATUS_FILTERS: { value: LineupStatusKind; label: string }[] = [
  { value: 'in-lineup', label: 'In lineup' },
  { value: 'proxied', label: 'Via proxy' },
  { value: 'duplicate', label: 'Duplicate' },
  { value: 'absent', label: 'Absent' },
  { value: 'needs-proxy', label: 'Needs proxy' },
  { value: 'drm', label: 'DRM blocked' },
  { value: 'unsupported', label: 'Unsupported class' },
  { value: 'disabled-group', label: 'Disabled group' },
  { value: 'unhealthy', label: 'Unhealthy' },
  { value: 'emit-disabled', label: 'Emit disabled' },
  { value: 'exclusion', label: 'Regex exclusion' },
  { value: 'missing-identity', label: 'Missing id' },
  { value: 'missing-stream', label: 'Missing stream' },
  { value: 'excluded', label: 'Excluded (other)' },
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
    case 'DISTRO_RESOLVE':
      return { label: 'Distro resolve', kind: 'beacon' }
    case 'XUMO_SSAI':
      return { label: 'Xumo SSAI', kind: 'beacon' }
    case 'DRM':
      return { label: 'DRM', kind: 'drm' }
    default:
      return { label: '—', kind: 'none' }
  }
}

/** Matches gen l1ShouldSchedule: Amagi only with emitted proxy URL; SESSION/Distro never. */
export function channelOnScheduledL1(ch: Pick<Channel, 'classification' | 'emitted_url' | 'stream_url'>): boolean {
  const cls = canonicalClassification(ch.classification)
  if (cls === 'SESSION' || cls === 'DISTRO_RESOLVE' || cls === 'DRM' || !cls) return false
  if (cls === 'AMAGI_SSAI') return Boolean(ch.emitted_url)
  return Boolean(ch.emitted_url || ch.stream_url)
}

export function l1SkipReason(ch: Pick<Channel, 'classification' | 'emitted_url'>): string {
  const cls = canonicalClassification(ch.classification)
  if (cls === 'SESSION') return 'not scheduled (SESSION mint — Manual / playback only)'
  if (cls === 'DISTRO_RESOLVE') return 'not scheduled (Distro resolve — Manual / playback only)'
  if (cls === 'AMAGI_SSAI' && !ch.emitted_url) {
    return 'not scheduled (Amagi needs proxy EmittedURL)'
  }
  if (cls === 'DRM') return 'not scheduled (DRM)'
  return 'not scheduled'
}

/** True when channel health has an armed L1 retry-lane due time. */
export function channelHasL1Retry(health?: ChannelHealth): boolean {
  if (!health?.next_retry_at) return false
  const d = new Date(health.next_retry_at)
  return !Number.isNaN(d.getTime()) && d.getUTCFullYear() > 1
}

/** Channel-detail copy for the next L1 probe (retry lane vs fleet sweep). */
export function nextL1Label(
  ch: Pick<Channel, 'classification' | 'emitted_url' | 'stream_url' | 'health'>,
  schedule?: { l1_running?: boolean; next_l1_at?: string },
): { title: string; value: string } {
  if (channelHasL1Retry(ch.health)) {
    const step = ch.health?.retry_step
    const stepHint =
      step != null && step > 0 ? ` (backoff step ${step})` : ''
    return {
      title: 'Next L1 retry',
      value: `${formatHealthWhen(ch.health?.next_retry_at)}${stepHint}`,
    }
  }
  if (!channelOnScheduledL1(ch)) {
    return { title: 'Next L1 sweep', value: l1SkipReason(ch) }
  }
  if (schedule?.l1_running) {
    return { title: 'Next L1 sweep', value: 'running now' }
  }
  return { title: 'Next L1 sweep', value: formatHealthWhen(schedule?.next_l1_at) }
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

/** Human label for channelattr Event.Source (probe vs proxy playback). */
export function formatHealthSource(source?: string): string {
  if (!source) return '—'
  switch (source) {
    case 'playback':
      return 'playback (proxy)'
    case 'health_l1':
      return 'health L1'
    case 'health_l1_retry':
      return 'health L1 retry'
    case 'health_l2':
      return 'health L2'
    case 'probe':
      return 'probe'
    default:
      return source
  }
}
