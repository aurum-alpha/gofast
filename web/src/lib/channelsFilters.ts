/** Channels list filter/sort ↔ URL query + sessionStorage helpers. */

import {
  CLASS_FILTERS,
  HEALTH_FILTERS,
  STATUS_FILTERS,
  canonicalClassification,
  healthStatus,
  lineupStatus,
  type Channel,
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

export type ChannelsSortKey =
  | 'number'
  | 'prov'
  | 'name'
  | 'provider'
  | 'group'
  | 'class'
  | 'health'
  | 'status'

export type ChannelsSortDir = 'asc' | 'desc'

export type ChannelsSort = {
  key: ChannelsSortKey | ''
  dir: ChannelsSortDir
}

export const DEFAULT_CHANNELS_FILTERS: ChannelsFilters = {
  provider: 'all',
  group: 'all',
  class: 'all',
  status: 'all',
  health: 'all',
  q: '',
}

/** Empty key = current default order (export #, then provider, then id). */
export const DEFAULT_CHANNELS_SORT: ChannelsSort = {
  key: '',
  dir: 'asc',
}

const STORAGE_KEY = 'gofast.channels.filters'

const CLASS_SET = new Set<string>(CLASS_FILTERS)
const STATUS_SET = new Set<string>(STATUS_FILTERS.map((s) => s.value))
const HEALTH_SET = new Set<string>(HEALTH_FILTERS.map((s) => s.value))
const SORT_KEY_SET = new Set<string>([
  'number',
  'prov',
  'name',
  'provider',
  'group',
  'class',
  'health',
  'status',
])

const FILTER_KEYS = ['provider', 'group', 'class', 'status', 'health', 'q'] as const
const LIST_KEYS = [...FILTER_KEYS, 'sort', 'dir'] as const

const HEALTH_RANK: Record<string, number> = {
  healthy: 0,
  degraded: 1,
  down: 2,
  untested: 3,
}

const STATUS_RANK: Record<string, number> = Object.fromEntries(
  STATUS_FILTERS.map((s, i) => [s.value, i]),
)

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

export function channelsSortActive(s: ChannelsSort): boolean {
  return s.key !== ''
}

export function channelsListDirty(f: ChannelsFilters, s: ChannelsSort): boolean {
  return channelsFiltersActive(f) || channelsSortActive(s)
}

export function searchHasChannelsFilters(params: URLSearchParams): boolean {
  return FILTER_KEYS.some((k) => params.has(k))
}

export function searchHasChannelsListState(params: URLSearchParams): boolean {
  return LIST_KEYS.some((k) => params.has(k))
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

export function channelsSortFromSearch(params: URLSearchParams): ChannelsSort {
  const keyRaw = params.get('sort') || ''
  const key = SORT_KEY_SET.has(keyRaw) ? (keyRaw as ChannelsSortKey) : ''
  if (!key) return { ...DEFAULT_CHANNELS_SORT }
  const dir = params.get('dir') === 'desc' ? 'desc' : 'asc'
  return { key, dir }
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

export function channelsListToSearch(
  f: ChannelsFilters,
  s: ChannelsSort = DEFAULT_CHANNELS_SORT,
): URLSearchParams {
  const params = channelsFiltersToSearch(f)
  if (s.key) {
    params.set('sort', s.key)
    params.set('dir', s.dir)
  }
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

type StoredChannelsList = Partial<ChannelsFilters> & {
  sort?: string
  dir?: string
}

export function readStoredChannelsFilters(): ChannelsFilters | null {
  const stored = readStoredChannelsList()
  return stored?.filters ?? null
}

export function readStoredChannelsList(): {
  filters: ChannelsFilters
  sort: ChannelsSort
} | null {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as StoredChannelsList
    const filters = channelsFiltersFromSearch(
      channelsFiltersToSearch({
        ...DEFAULT_CHANNELS_FILTERS,
        ...parsed,
      }),
    )
    const sort = channelsSortFromSearch(
      new URLSearchParams({
        ...(parsed.sort ? { sort: parsed.sort } : {}),
        ...(parsed.dir ? { dir: parsed.dir } : {}),
      }),
    )
    if (!channelsListDirty(filters, sort)) return null
    return { filters, sort }
  } catch {
    return null
  }
}

export function writeStoredChannelsFilters(f: ChannelsFilters): void {
  writeStoredChannelsList(f, DEFAULT_CHANNELS_SORT)
}

export function writeStoredChannelsList(f: ChannelsFilters, s: ChannelsSort): void {
  try {
    if (!channelsListDirty(f, s)) {
      sessionStorage.removeItem(STORAGE_KEY)
      return
    }
    const payload: StoredChannelsList = { ...f }
    if (s.key) {
      payload.sort = s.key
      payload.dir = s.dir
    }
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(payload))
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
  const stored = readStoredChannelsList()
  if (stored && channelsListDirty(stored.filters, stored.sort)) {
    return channelsListPath(channelsListToSearch(stored.filters, stored.sort))
  }
  return '/'
}

export type ChannelsLocationState = {
  channelsSearch?: string
}

function numberSortKey(n: number): number {
  return n > 0 ? n : Number.POSITIVE_INFINITY
}

/** Compare two channels under the active sort (default = export # / provider / id). */
export function compareChannels(a: Channel, b: Channel, sort: ChannelsSort): number {
  const mul = sort.key && sort.dir === 'desc' ? -1 : 1
  let cmp = 0

  if (!sort.key) {
    cmp = numberSortKey(a.offset_number) - numberSortKey(b.offset_number)
    if (cmp !== 0) return cmp
    if (a.provider !== b.provider) return a.provider.localeCompare(b.provider)
    return a.normalized_id.localeCompare(b.normalized_id)
  }

  switch (sort.key) {
    case 'number':
      cmp = numberSortKey(a.offset_number) - numberSortKey(b.offset_number)
      break
    case 'prov':
      cmp = numberSortKey(a.number) - numberSortKey(b.number)
      break
    case 'name':
      cmp = a.name.localeCompare(b.name)
      break
    case 'provider':
      cmp = a.provider.localeCompare(b.provider)
      break
    case 'group':
      cmp = (a.group || '').localeCompare(b.group || '')
      break
    case 'class':
      cmp = canonicalClassification(a.classification).localeCompare(
        canonicalClassification(b.classification),
      )
      break
    case 'health':
      cmp = (HEALTH_RANK[healthStatus(a)] ?? 99) - (HEALTH_RANK[healthStatus(b)] ?? 99)
      break
    case 'status':
      cmp =
        (STATUS_RANK[lineupStatus(a)] ?? 99) - (STATUS_RANK[lineupStatus(b)] ?? 99)
      break
  }

  if (cmp !== 0) return cmp * mul
  if (a.provider !== b.provider) return a.provider.localeCompare(b.provider)
  return a.normalized_id.localeCompare(b.normalized_id)
}

export function nextChannelsSort(
  current: ChannelsSort,
  key: ChannelsSortKey,
): ChannelsSort {
  if (current.key === key) {
    return { key, dir: current.dir === 'asc' ? 'desc' : 'asc' }
  }
  return { key, dir: 'asc' }
}
