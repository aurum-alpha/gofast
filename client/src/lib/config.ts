/** Types + fetch/save helpers for the settings editor (/api/config). */

export type FieldSource = 'default' | 'file' | 'env'

export type ConfigField = {
  value: unknown
  source: FieldSource
  editable: boolean
  env?: string
  restart_required?: boolean
}

export type ProviderSettings = {
  id: string
  enabled: boolean
  label: string
  channel_number_offset: number
  synthesize_channel_numbers: number
  min_channels: number
  refresh_interval: string
  expected_guide_horizon?: string
  exclusions?: string[]
  slug_template?: string
  region?: string
  channels_url?: string
  epg_url?: string
  m3u_url?: string
  user_agent?: string
  headers?: Record<string, string>
}

export type ConfigProviderEntry = {
  settings: ProviderSettings
  configured: boolean
  field_support: string[]
}

export type ArtworkTLS = {
  host: string
  ca_pem_set: boolean
  insecure_skip_verify: boolean
}

export type ProbeSchedule = {
  l1_interval: string
  last_l1_at?: string
  next_l1_at?: string
  l1_running?: boolean
  l2_enabled?: boolean
  l2_interval?: string
  last_l2_at?: string
  next_l2_at?: string
  l2_running?: boolean
}

export type ConfigResponse = {
  revision: string
  source: { path: string; from_file: boolean; writable: boolean }
  fields: Record<string, ConfigField>
  probe_schedule?: ProbeSchedule
  artwork_tls: ArtworkTLS[]
  providers: ConfigProviderEntry[]
}

export type PathOp = {
  path: string
  value?: unknown
  remove?: boolean
}

export type ReloadResult = { name: string; error?: string }

export type SaveResponse = { revision: string; reloads: ReloadResult[] }

/** Save failure with the HTTP status so callers can special-case 409. */
export class ConfigSaveError extends Error {
  status: number

  constructor(status: number, message: string) {
    super(message)
    this.status = status
  }
}

export async function fetchConfig(): Promise<ConfigResponse> {
  const res = await fetch('/api/config')
  if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
  return (await res.json()) as ConfigResponse
}

export async function saveConfig(revision: string, ops: PathOp[]): Promise<SaveResponse> {
  const res = await fetch('/api/config', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ revision, ops }),
  })
  if (!res.ok) {
    const text = (await res.text()).trim()
    throw new ConfigSaveError(res.status, text || `${res.status} ${res.statusText}`)
  }
  return (await res.json()) as SaveResponse
}

/** Human summary of a save's reload report ("applied live" vs failures). */
export function reloadSummary(reloads: ReloadResult[]): { ok: boolean; message: string } {
  const failed = reloads.filter((r) => r.error)
  if (failed.length === 0) {
    return { ok: true, message: 'Saved and applied live — no restart needed.' }
  }
  const detail = failed.map((r) => `${r.name}: ${r.error}`).join('; ')
  return { ok: false, message: `Saved, but some subsystems failed to reload — ${detail}` }
}
