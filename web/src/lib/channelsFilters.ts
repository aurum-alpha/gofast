/** Channels list filter ↔ URL query + sessionStorage helpers. */

import {
  CLASS_FILTERS,
  HEALTH_FILTERS,
  STATUS_FILTERS,
  type HealthFilterValue,
  type LineupStatusKind,
} from './channel'

export type ChannelsFilters = {
  provider: string
  group: string
  class: string
  status: string
  health: string
  q: string
}

export const DEFAULT_CHANNELS_FILTERS: ChannelsFilters = {
  provider: 'all',
  group: 'all',
  class: 'all',
  status: 'all',
  health: 'all',
  q: '',
}

const STORAGE_KEY = 'gofast.channels.filters'

const CLASS_SET = new Set<string>(CLASS_FILTERS)
const STATUS_SET = new Set<string>(STATUS_FILTERS.map((s) => s.value))
const HEALTH_SET = new Set<string>(HEALTH_FILTERS.map((s) => s.value))

const FILTER_KEYS = ['provider', 'group', 'class', 'status', 'health', 'q'] as const

export function channelsFiltersActive(f: ChannelsFilters): boolean {
  return (
    f.provider !== 'all' ||
    f.group !== 'all' ||
    f.class !== 'all' ||
    f.status !== 'all' ||
    f.health !== 'all' ||
    f.q.trim() !== ''
  )
}

export function searchHasChannelsFilters(params: URLSearchParams): boolean {
  return FILTER_KEYS.some((k) => params.has(k))
}

export function channelsFiltersFromSearch(params: URLSearchParams): ChannelsFilters {
  const provider = params.get('provider') || 'all'
  const group = params.get('group') || 'all'
  const classRaw = params.get('class') || 'all'
  const statusRaw = params.get('status') || 'all'
  const healthRaw = params.get('health') || 'all'
  const q = params.get('q') || ''

  const classFilter =
    classRaw === 'all' || CLASS_SET.has(classRaw) || classRaw === 'BEACON'
      ? classRaw === 'BEACON'
        ? 'AMAGI_SSAI'
        : classRaw
      : 'all'
  const statusFilter =
    statusRaw === 'all' || STATUS_SET.has(statusRaw as LineupStatusKind) ? statusRaw : 'all'
  const healthFilter =
    healthRaw === 'all' || HEALTH_SET.has(healthRaw as HealthFilterValue) ? healthRaw : 'all'

  return {
    provider: provider || 'all',
    group: group || 'all',
    class: classFilter,
    status: statusFilter,
    health: healthFilter,
    q,
  }
}

/** Only non-default keys — keeps URLs short. */
export function channelsFiltersToSearch(f: ChannelsFilters): URLSearchParams {
  const params = new URLSearchParams()
  if (f.provider !== 'all' && f.provider) params.set('provider', f.provider)
  if (f.group !== 'all' && f.group) params.set('group', f.group)
  if (f.class !== 'all' && f.class) params.set('class', f.class)
  if (f.status !== 'all' && f.status) params.set('status', f.status)
  if (f.health !== 'all' && f.health) params.set('health', f.health)
  const q = f.q.trim()
  if (q) params.set('q', q)
  return params
}

export function channelsListPath(search?: string | URLSearchParams): string {
  const s =
    typeof search === 'string'
      ? search.replace(/^\?/, '')
      : search
        ? search.toString()
        : ''
  return s ? `/?${s}` : '/'
}

export function readStoredChannelsFilters(): ChannelsFilters | null {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<ChannelsFilters>
    return channelsFiltersFromSearch(
      channelsFiltersToSearch({
        ...DEFAULT_CHANNELS_FILTERS,
        ...parsed,
      }),
    )
  } catch {
    return null
  }
}

export function writeStoredChannelsFilters(f: ChannelsFilters): void {
  try {
    if (!channelsFiltersActive(f)) {
      sessionStorage.removeItem(STORAGE_KEY)
      return
    }
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(f))
  } catch {
    // private mode / quota — URL still works
  }
}

export function clearStoredChannelsFilters(): void {
  try {
    sessionStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}

/** Href for ← Channels: explicit return search, else sessionStorage, else bare list. */
export function channelsBackHref(channelsSearch?: string): string {
  if (channelsSearch !== undefined) {
    return channelsListPath(channelsSearch)
  }
  const stored = readStoredChannelsFilters()
  if (stored && channelsFiltersActive(stored)) {
    return channelsListPath(channelsFiltersToSearch(stored))
  }
  return '/'
}

export type ChannelsLocationState = {
  channelsSearch?: string
}
