import { ConfigSaveError, reloadSummary, type ReloadResult } from './config'
import type { Channel, ChannelEmit } from './channel'

export type ChannelEmitResponse = {
  revision: string
  writable: boolean
  channel: Channel
  reloads?: ReloadResult[]
}

export async function fetchChannelEmit(
  provider: string,
  normalizedId: string,
): Promise<ChannelEmitResponse> {
  const path = `/api/channels/${encodeURIComponent(provider)}/${encodeURIComponent(normalizedId)}/emit`
  const res = await fetch(path)
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return (await res.json()) as ChannelEmitResponse
}

export async function saveChannelEmit(
  provider: string,
  normalizedId: string,
  revision: string,
  emit: ChannelEmit | null,
): Promise<ChannelEmitResponse> {
  const path = `/api/channels/${encodeURIComponent(provider)}/${encodeURIComponent(normalizedId)}/emit`
  const res = await fetch(path, {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ revision, emit }),
  })
  if (!res.ok) {
    const text = (await res.text()).trim()
    throw new ConfigSaveError(res.status, text || `${res.status} ${res.statusText}`)
  }
  return (await res.json()) as ChannelEmitResponse
}

export { ConfigSaveError, reloadSummary }
