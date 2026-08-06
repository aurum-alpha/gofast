/** Guide page filter ↔ URL query + sessionStorage helpers. */

import { CLASS_FILTERS } from './channel'

export type TimePreset = 'pm6' | 'pm12' | 'today' | 'next24' | 'all'

export type GuideFilters = {
  provider: string
  group: string
  region: string
  class: string
  hideExcluded: boolean
  time: TimePreset
  channelQ: string
  programmeQ: string
}

export const DEFAULT_GUIDE_FILTERS: GuideFilters = {
  provider: 'all',
  group: 'all',
  region: 'all',
  class: 'all',
  hideExcluded: true,
  time: 'pm12',
  channelQ: '',
  programmeQ: '',
}

const STORAGE_KEY = 'gofast.guide.filters'
const CLASS_SET = new Set<string>(CLASS_FILTERS)
const TIME_SET = new Set<TimePreset>(['pm6', 'pm12', 'today', 'next24', 'all'])

const FILTER_KEYS = [
  'provider',
  'group',
  'region',
  'class',
  'showExcluded',
  'time',
  'cq',
  'pq',
] as const

export function guideFiltersActive(f: GuideFilters): boolean {
  return (
    f.provider !== 'all' ||
    f.group !== 'all' ||
    f.region !== 'all' ||
    f.class !== 'all' ||
    !f.hideExcluded ||
    f.time !== DEFAULT_GUIDE_FILTERS.time ||
    f.channelQ.trim() !== '' ||
    f.programmeQ.trim() !== ''
  )
}

export function searchHasGuideFilters(params: URLSearchParams): boolean {
  return FILTER_KEYS.some((k) => params.has(k))
}

export function guideFiltersFromSearch(params: URLSearchParams): GuideFilters {
  const provider = params.get('provider') || 'all'
  const group = params.get('group') || 'all'
  const region = params.get('region') || 'all'
  const classRaw = params.get('class') || 'all'
  const timeRaw = params.get('time') || DEFAULT_GUIDE_FILTERS.time
  const channelQ = params.get('cq') || ''
  const programmeQ = params.get('pq') || ''

  const classFilter =
    classRaw === 'all' || CLASS_SET.has(classRaw) || classRaw === 'BEACON'
      ? classRaw === 'BEACON'
        ? 'AMAGI_SSAI'
        : classRaw
      : 'all'
  const time: TimePreset = TIME_SET.has(timeRaw as TimePreset)
    ? (timeRaw as TimePreset)
    : DEFAULT_GUIDE_FILTERS.time

  // Default hideExcluded=true; showExcluded=1 means include excluded channels.
  const hideExcluded = params.get('showExcluded') !== '1'

  return {
    provider: provider || 'all',
    group: group || 'all',
    region: region || 'all',
    class: classFilter,
    hideExcluded,
    time,
    channelQ,
    programmeQ,
  }
}

/** Only non-default keys — keeps URLs short. */
export function guideFiltersToSearch(f: GuideFilters): URLSearchParams {
  const params = new URLSearchParams()
  if (f.provider !== 'all' && f.provider) params.set('provider', f.provider)
  if (f.group !== 'all' && f.group) params.set('group', f.group)
  if (f.region !== 'all' && f.region) params.set('region', f.region)
  if (f.class !== 'all' && f.class) params.set('class', f.class)
  if (!f.hideExcluded) params.set('showExcluded', '1')
  if (f.time !== DEFAULT_GUIDE_FILTERS.time) params.set('time', f.time)
  const cq = f.channelQ.trim()
  if (cq) params.set('cq', cq)
  const pq = f.programmeQ.trim()
  if (pq) params.set('pq', pq)
  return params
}

export function readStoredGuideFilters(): GuideFilters | null {
  try {
    const raw = sessionStorage.getItem(STORAGE_KEY)
    if (!raw) return null
    const parsed = JSON.parse(raw) as Partial<GuideFilters>
    return guideFiltersFromSearch(
      guideFiltersToSearch({
        ...DEFAULT_GUIDE_FILTERS,
        ...parsed,
      }),
    )
  } catch {
    return null
  }
}

export function writeStoredGuideFilters(f: GuideFilters): void {
  try {
    if (!guideFiltersActive(f)) {
      sessionStorage.removeItem(STORAGE_KEY)
      return
    }
    sessionStorage.setItem(STORAGE_KEY, JSON.stringify(f))
  } catch {
    // private mode / quota — URL still works
  }
}

export function clearStoredGuideFilters(): void {
  try {
    sessionStorage.removeItem(STORAGE_KEY)
  } catch {
    // ignore
  }
}
