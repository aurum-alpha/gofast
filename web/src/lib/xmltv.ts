// Minimal XMLTV parser for the Guide page. Parses the subset gofast emits:
// <channel id><display-name><lcn><icon src> and <programme channel start stop><title><desc>.

export type XmltvChannel = {
  id: string
  displayName: string
  number: number
  logo: string
}

export type XmltvProgramme = {
  channel: string
  title: string
  desc: string
  start: Date
  stop: Date
}

export type Xmltv = {
  channels: XmltvChannel[]
  programmes: XmltvProgramme[]
}

// parseXmltvTime parses "YYYYMMDDHHMMSS +ZZZZ" (offset optional, assumed UTC).
export function parseXmltvTime(raw: string): Date {
  const m = raw
    .trim()
    .match(/^(\d{4})(\d{2})(\d{2})(\d{2})(\d{2})(\d{2})(?:\s*([+-]\d{4}))?$/)
  if (!m) return new Date(NaN)
  const [, y, mo, d, h, mi, s, off] = m
  const tz = off ? `${off.slice(0, 3)}:${off.slice(3)}` : 'Z'
  return new Date(`${y}-${mo}-${d}T${h}:${mi}:${s}${tz}`)
}

export function parseXMLTV(text: string): Xmltv {
  const doc = new DOMParser().parseFromString(text, 'application/xml')
  if (doc.querySelector('parsererror')) {
    throw new Error('invalid XMLTV document')
  }
  const channels: XmltvChannel[] = [...doc.querySelectorAll('tv > channel')].map(
    (el) => ({
      id: el.getAttribute('id') ?? '',
      displayName: el.querySelector('display-name')?.textContent?.trim() ?? '',
      number: Number(el.querySelector('lcn')?.textContent?.trim() ?? '') || 0,
      logo: el.querySelector('icon')?.getAttribute('src') ?? '',
    }),
  )
  const programmes: XmltvProgramme[] = [
    ...doc.querySelectorAll('tv > programme'),
  ].map((el) => ({
    channel: el.getAttribute('channel') ?? '',
    title: el.querySelector('title')?.textContent?.trim() ?? '',
    desc: el.querySelector('desc')?.textContent?.trim() ?? '',
    start: parseXmltvTime(el.getAttribute('start') ?? ''),
    stop: parseXmltvTime(el.getAttribute('stop') ?? ''),
  }))
  return { channels, programmes }
}

// combinedId mirrors Go's format.CombinedID for joining the aggregate guide
// (provider-namespaced ids) against /api/channels rows.
export function combinedId(provider: string, normalizedId: string): string {
  return `${provider}.${normalizedId}`
}

// namespaceXmltv prefixes bare per-provider channel/programme ids with
// "{provider}." so they match /api/channels combined ids.
export function namespaceXmltv(provider: string, doc: Xmltv): Xmltv {
  return {
    channels: doc.channels.map((ch) => ({
      ...ch,
      id: combinedId(provider, ch.id),
    })),
    programmes: doc.programmes.map((p) => ({
      ...p,
      channel: combinedId(provider, p.channel),
    })),
  }
}
