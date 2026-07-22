import { combinedId, namespaceXmltv, parseXMLTV, type Xmltv } from './xmltv'

export type ChannelMeta = {
  provider: string
  id: string
  normalized_id: string
  name: string
  group: string
  number: number
  offset_number: number
  logo_url?: string
  classification?: string
  excluded: boolean
}

export type GuideRow = {
  id: string
  name: string
  number: number
  logo: string
  provider: string
  group: string
  rawId: string
  normalizedId: string
  classification: string
  excluded: boolean
  programmes: Xmltv['programmes']
}

export type ProviderPhase =
  | 'pending'
  | 'fetching'
  | 'parsing'
  | 'ready'
  | 'empty'
  | 'error'

export type ProviderStatus = {
  id: string
  phase: ProviderPhase
  error?: string
}

export type ChannelsResponse = { channels: ChannelMeta[] }

export type ProvidersResponse = {
  providers: { id: string; enabled: boolean; label: string }[]
}

export function sortGuideRows(rows: GuideRow[]): GuideRow[] {
  return rows.slice().sort((a, b) => {
    const an = a.number > 0 ? a.number : Number.POSITIVE_INFINITY
    const bn = b.number > 0 ? b.number : Number.POSITIVE_INFINITY
    if (an !== bn) return an - bn
    if (a.provider !== b.provider) return a.provider.localeCompare(b.provider)
    return a.id.localeCompare(b.id)
  })
}

export function rowsFromProviderGuide(
  provider: string,
  guide: Xmltv,
  metaById: Map<string, ChannelMeta>,
): GuideRow[] {
  const namespaced = namespaceXmltv(provider, guide)
  const progs = new Map<string, Xmltv['programmes']>()
  for (const p of namespaced.programmes) {
    const arr = progs.get(p.channel)
    if (arr) arr.push(p)
    else progs.set(p.channel, [p])
  }
  return namespaced.channels.map((xc) => {
    const m = metaById.get(xc.id)
    const list = (progs.get(xc.id) ?? [])
      .slice()
      .sort((a, b) => a.start.getTime() - b.start.getTime())
    return {
      id: xc.id,
      name: xc.displayName || m?.name || xc.id,
      number: xc.number || m?.offset_number || m?.number || 0,
      logo: xc.logo || m?.logo_url || '',
      provider: m?.provider ?? provider,
      group: m?.group ?? '',
      rawId: m?.id ?? '',
      normalizedId:
        m?.normalized_id ??
        (xc.id.startsWith(`${provider}.`) ? xc.id.slice(provider.length + 1) : xc.id),
      classification: m?.classification ?? '',
      excluded: m?.excluded ?? false,
      programmes: list,
    }
  })
}

export function metaIndex(channels: ChannelMeta[]): Map<string, ChannelMeta> {
  const meta = new Map<string, ChannelMeta>()
  for (const c of channels) {
    meta.set(combinedId(c.provider, c.normalized_id), c)
  }
  return meta
}

export function enabledProviderIds(
  providers: ProvidersResponse['providers'],
  providerFilter: string,
): string[] {
  const enabled = providers
    .filter((p) => p.enabled)
    .map((p) => p.id)
    .sort()
  if (providerFilter === 'all') return enabled
  if (enabled.includes(providerFilter)) return [providerFilter]
  return [providerFilter]
}

export function summarizeLoad(statuses: ProviderStatus[]): string {
  if (statuses.length === 0) return 'Loading providers…'
  const ready = statuses.filter((s) => s.phase === 'ready' || s.phase === 'empty').length
  const active = statuses.find(
    (s) => s.phase === 'fetching' || s.phase === 'parsing',
  )
  const errors = statuses.filter((s) => s.phase === 'error')
  if (active) {
    const idx = statuses.findIndex((s) => s.id === active.id) + 1
    const verb = active.phase === 'fetching' ? 'Fetching' : 'Parsing'
    return `${verb} ${active.id} (${idx}/${statuses.length})…`
  }
  const pending = statuses.some((s) => s.phase === 'pending')
  if (pending) {
    return `Loading guides (${ready}/${statuses.length})…`
  }
  if (errors.length > 0) {
    const names = errors.map((e) => e.id).join(', ')
    return `Ready · ${ready}/${statuses.length} providers (${names} failed)`
  }
  return `Ready · ${ready}/${statuses.length} providers`
}

type LoadHooks = {
  signal: AbortSignal
  onStatuses: (statuses: ProviderStatus[]) => void
  onProviderRows: (provider: string, rows: GuideRow[]) => void
}

// loadProviderGuides fetches XMLTV per provider sequentially. Already-loaded
// ids in `skip` are left as ready without refetching.
export async function loadProviderGuides(
  providerIds: string[],
  metaById: Map<string, ChannelMeta>,
  skip: ReadonlySet<string>,
  hooks: LoadHooks,
): Promise<void> {
  const statuses: ProviderStatus[] = providerIds.map((id) => ({
    id,
    phase: skip.has(id) ? 'ready' : 'pending',
  }))
  hooks.onStatuses(statuses.slice())

  for (let i = 0; i < providerIds.length; i++) {
    if (hooks.signal.aborted) return
    const id = providerIds[i]
    if (skip.has(id)) continue

    statuses[i] = { id, phase: 'fetching' }
    hooks.onStatuses(statuses.slice())

    try {
      const res = await fetch(`/api/guide/${encodeURIComponent(id)}.xml?includeAll=true`, {
        signal: hooks.signal,
      })
      if (hooks.signal.aborted) return
      if (res.status === 404 || res.status === 503) {
        statuses[i] = { id, phase: 'empty' }
        hooks.onStatuses(statuses.slice())
        continue
      }
      if (!res.ok) {
        throw new Error(`${res.status} ${res.statusText}`)
      }
      const text = await res.text()
      if (hooks.signal.aborted) return

      statuses[i] = { id, phase: 'parsing' }
      hooks.onStatuses(statuses.slice())

      const guide = parseXMLTV(text)
      if (guide.channels.length === 0) {
        statuses[i] = { id, phase: 'empty' }
        hooks.onStatuses(statuses.slice())
        continue
      }
      const rows = rowsFromProviderGuide(id, guide, metaById)
      hooks.onProviderRows(id, rows)
      statuses[i] = { id, phase: 'ready' }
      hooks.onStatuses(statuses.slice())
    } catch (err) {
      if (hooks.signal.aborted) return
      statuses[i] = {
        id,
        phase: 'error',
        error: err instanceof Error ? err.message : String(err),
      }
      hooks.onStatuses(statuses.slice())
    }
  }
}
