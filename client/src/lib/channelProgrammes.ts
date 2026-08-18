/** Channel detail programmes fetch + Now/Next helpers. */

export type Programme = {
  channel_id: string
  title: string
  desc?: string
  categories?: string[]
  emitted_categories?: string[]
  start: string
  stop: string
}

export type ProgrammesResponse = {
  programmes: Programme[]
}

export async function fetchChannelProgrammes(
  provider: string,
  normalizedId: string,
): Promise<Programme[]> {
  const path = `/api/channels/${encodeURIComponent(provider)}/${encodeURIComponent(normalizedId)}/programmes`
  const res = await fetch(path)
  if (!res.ok) {
    throw new Error(`${res.status} ${res.statusText}`)
  }
  const body = (await res.json()) as ProgrammesResponse
  return Array.isArray(body.programmes) ? body.programmes : []
}

export function formatProgrammeWhen(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return '—'
  return d.toLocaleString()
}

export function formatProgrammeRange(start: string, stop: string): string {
  return `${formatProgrammeWhen(start)} – ${formatProgrammeWhen(stop)}`
}

/** Current programme: start <= now < stop; if several, latest start. */
export function programmeNow(list: Programme[], now = new Date()): Programme | null {
  let best: Programme | null = null
  let bestStart = 0
  const t = now.getTime()
  for (const p of list) {
    const start = Date.parse(p.start)
    const stop = Date.parse(p.stop)
    if (Number.isNaN(start) || Number.isNaN(stop)) continue
    if (start <= t && t < stop) {
      if (!best || start >= bestStart) {
        best = p
        bestStart = start
      }
    }
  }
  return best
}

/** Next programme after now (or after Now's stop when Now exists). */
export function programmeNext(
  list: Programme[],
  now = new Date(),
  current: Programme | null = null,
): Programme | null {
  const floor = current ? Date.parse(current.stop) : now.getTime()
  let best: Programme | null = null
  let bestStart = Number.POSITIVE_INFINITY
  for (const p of list) {
    if (current && p.start === current.start && p.stop === current.stop && p.title === current.title) {
      continue
    }
    const start = Date.parse(p.start)
    if (Number.isNaN(start) || start < floor) continue
    if (start < bestStart) {
      best = p
      bestStart = start
    }
  }
  return best
}

export function isProgrammeNow(p: Programme, now = new Date()): boolean {
  const t = now.getTime()
  const start = Date.parse(p.start)
  const stop = Date.parse(p.stop)
  if (Number.isNaN(start) || Number.isNaN(stop)) return false
  return start <= t && t < stop
}
