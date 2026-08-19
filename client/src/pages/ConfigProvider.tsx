import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link, useParams } from 'react-router-dom'
import {
  ConfigSaveError,
  fetchConfig,
  reloadSummary,
  saveConfig,
  type ConfigProviderEntry,
  type ConfigResponse,
  type PathOp,
} from '../lib/config'

/** Draft mirrors the form controls: everything is a string except enabled. */
type Draft = {
  enabled: boolean
  label: string
  channel_number_offset: string
  synthesize_channel_numbers: string
  min_channels: string
  refresh_interval: string
  exclusions: string
  slug_template: string
  region: string
  channels_url: string
  epg_url: string
  m3u_url: string
  user_agent: string
  headers: string
}

function draftFrom(entry: ConfigProviderEntry): Draft {
  const s = entry.settings
  return {
    enabled: s.enabled,
    label: s.label ?? '',
    // 0 / unset → blank in the form ("off"); blank saves back as 0.
    channel_number_offset:
      s.channel_number_offset != null && s.channel_number_offset !== 0
        ? String(s.channel_number_offset)
        : '',
    synthesize_channel_numbers:
      s.synthesize_channel_numbers != null && s.synthesize_channel_numbers !== 0
        ? String(s.synthesize_channel_numbers)
        : '',
    min_channels: String(s.min_channels ?? 1),
    refresh_interval: s.refresh_interval ?? '',
    exclusions: (s.exclusions ?? []).join('\n'),
    slug_template: s.slug_template ?? '',
    region: s.region ?? '',
    channels_url: s.channels_url ?? '',
    epg_url: s.epg_url ?? '',
    m3u_url: s.m3u_url ?? '',
    user_agent: s.user_agent ?? '',
    headers: Object.entries(s.headers ?? {})
      .map(([k, v]) => `${k}: ${v}`)
      .join('\n'),
  }
}

function parseLines(text: string): string[] {
  return text
    .split('\n')
    .map((s) => s.trim())
    .filter((s) => s !== '')
}

function parseHeaders(text: string): Record<string, string> | null {
  const out: Record<string, string> = {}
  for (const line of parseLines(text)) {
    const i = line.indexOf(':')
    if (i <= 0) return null
    out[line.slice(0, i).trim()] = line.slice(i + 1).trim()
  }
  return out
}

function parseIntStrict(raw: string): number | null {
  const n = Number.parseInt(raw, 10)
  return Number.isFinite(n) && String(n) === raw.trim() ? n : null
}

/** Integer field where blank means 0 (feature off). */
function parseOptionalInt(raw: string): number | null {
  const t = raw.trim()
  if (t === '') return 0
  return parseIntStrict(t)
}

// Optional adapter fields keyed by field_support names, with control metadata.
// Region is system-wide (Config → Regions); not edited per provider.
const OPTIONAL_FIELDS: Array<{ key: keyof Draft & string; label: string; hint?: string }> = [
  { key: 'slug_template', label: 'Slug template', hint: 'Stream slug override (e.g. plu-{id}.m3u8)' },
  { key: 'channels_url', label: 'Channels URL' },
  { key: 'epg_url', label: 'EPG URL' },
  { key: 'm3u_url', label: 'M3U URL' },
  { key: 'user_agent', label: 'User agent' },
]

export function ConfigProviderPage() {
  const { id = '' } = useParams()
  const [data, setData] = useState<ConfigResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft | null>(null)
  const [saving, setSaving] = useState(false)
  const [toast, setToast] = useState<{ ok: boolean; message: string } | null>(null)

  const load = useCallback(async () => {
    const body = await fetchConfig()
    setData(body)
    const entry = body.providers.find((p) => p.settings.id === id)
    if (entry) setDraft(draftFrom(entry))
    setError(null)
    return body
  }, [id])

  useEffect(() => {
    let cancelled = false
    load().catch((err: unknown) => {
      if (!cancelled) setError(err instanceof Error ? err.message : String(err))
    })
    return () => {
      cancelled = true
    }
  }, [load])

  const entry = useMemo(
    () => data?.providers.find((p) => p.settings.id === id) ?? null,
    [data, id],
  )

  const original = useMemo(() => (entry ? draftFrom(entry) : null), [entry])

  const dirty = useMemo(() => {
    if (!draft || !original) return []
    return (Object.keys(draft) as Array<keyof Draft>).filter((k) => draft[k] !== original[k])
  }, [draft, original])

  async function save() {
    if (!data || !draft || !original || dirty.length === 0) return
    setSaving(true)
    setToast(null)
    try {
      const prefix = `providers.${id}.`
      const ops: PathOp[] = []
      // Always pin enabled explicitly: creating a providers.<id> block without
      // it would default the provider to enabled.
      ops.push({ path: `${prefix}enabled`, value: draft.enabled })
      const pushText = (key: keyof Draft & string) => {
        if (draft[key] !== original[key]) ops.push({ path: prefix + key, value: draft[key] })
      }
      const pushOptionalInt = (key: keyof Draft & string, label: string): boolean => {
        if (draft[key] === original[key]) return true
        const n = parseOptionalInt(String(draft[key]))
        if (n === null) {
          setToast({ ok: false, message: `${label}: must be an integer (blank = 0 / off)` })
          return false
        }
        ops.push({ path: prefix + key, value: n })
        return true
      }
      if (!pushOptionalInt('channel_number_offset', 'Channel number offset')) return
      if (!pushOptionalInt('synthesize_channel_numbers', 'Synthesize channel numbers')) return
      if (draft.min_channels !== original.min_channels) {
        const n = parseIntStrict(String(draft.min_channels))
        if (n === null || n < 1) {
          setToast({ ok: false, message: 'Min channels: must be an integer ≥ 1' })
          setSaving(false)
          return
        }
        ops.push({ path: `${prefix}min_channels`, value: n })
      }
      pushText('label')
      pushText('refresh_interval')
      for (const f of OPTIONAL_FIELDS) pushText(f.key)
      if (draft.exclusions !== original.exclusions) {
        ops.push({ path: `${prefix}exclusions`, value: parseLines(draft.exclusions) })
      }
      if (draft.headers !== original.headers) {
        const headers = parseHeaders(draft.headers)
        if (headers === null) {
          setToast({ ok: false, message: 'Headers: use one "Name: value" per line' })
          return
        }
        ops.push({ path: `${prefix}headers`, value: headers })
      }
      const result = await saveConfig(data.revision, ops)
      await load()
      setToast(reloadSummary(result.reloads))
    } catch (err: unknown) {
      if (err instanceof ConfigSaveError && err.status === 409) {
        await load().catch(() => {})
        setToast({
          ok: false,
          message: 'Config changed elsewhere — reloaded the latest values; re-apply your edits.',
        })
      } else {
        setToast({ ok: false, message: err instanceof Error ? err.message : String(err) })
      }
    } finally {
      setSaving(false)
    }
  }

  if (error) {
    return (
      <>
        <h1>Provider settings</h1>
        <div className="empty-panel" role="alert">
          Failed to load config: {error}
        </div>
      </>
    )
  }
  if (!data || !draft) {
    return (
      <>
        <h1>Provider settings</h1>
        <div className="empty-panel" role="status">
          Loading…
        </div>
      </>
    )
  }
  if (!entry) {
    return (
      <>
        <h1>Provider settings</h1>
        <div className="empty-panel" role="alert">
          Unknown provider <code>{id}</code>. <Link to="/config">Back to settings</Link>
        </div>
      </>
    )
  }

  const support = new Set(entry.field_support)
  const set = <K extends keyof Draft>(key: K, value: Draft[K]) =>
    setDraft((prev) => (prev ? { ...prev, [key]: value } : prev))

  const textField = (
    key: keyof Draft & string,
    label: string,
    hint?: string,
  ) => (
    <div className="config-field" key={key}>
      <span className="config-field-label">{label}</span>
      <input
        type="text"
        value={String(draft[key])}
        disabled={!data.source.writable}
        onChange={(e) => set(key, e.target.value)}
      />
      {hint ? <span className="field-hint">{hint}</span> : null}
    </div>
  )

  return (
    <>
      <p>
        <Link className="back-link" to="/config">
          ← Settings
        </Link>
      </p>
      <h1>
        Provider settings: <code>{id}</code>
      </h1>
      <p className="lead">
        Applies live on save. Disabling stops fetches, hides its channels, and
        404s <code>/{id}.m3u</code> / <code>/{id}.xml</code>; the cache is kept so
        re-enabling restores instantly. Triage stats live on the{' '}
        <Link to={`/providers/${encodeURIComponent(id)}`}>provider page</Link>.
      </p>

      {!data.source.writable ? (
        <div className="empty-panel" role="alert">
          <strong>Config is read-only.</strong> Mount <code>{data.source.path}</code>{' '}
          read-write to save settings.
        </div>
      ) : null}

      <div className="config-toolbar">
        <button
          type="button"
          onClick={() => {
            void save()
          }}
          disabled={saving || !data.source.writable || dirty.length === 0}
        >
          {saving ? 'Saving…' : 'Save & apply'}
        </button>
        {toast ? (
          <span className={`meta config-toast${toast.ok ? '' : ' config-toast-error'}`} role="status">
            {toast.message}
          </span>
        ) : null}
      </div>

      <section className="detail-section">
        <h2>Core</h2>
        <div className="config-form">
          <div className="config-field">
            <span className="config-field-label">Enabled</span>
            <input
              type="checkbox"
              checked={draft.enabled}
              disabled={!data.source.writable}
              onChange={(e) => set('enabled', e.target.checked)}
            />
            <span className="field-hint">Live start/stop — no restart</span>
          </div>
          {textField('label', 'Label', 'Playlist display name')}
          {textField(
            'channel_number_offset',
            'Channel number offset',
            'Added to upstream numbers for export; blank or 0 = off',
          )}
          {textField(
            'synthesize_channel_numbers',
            'Synthesize channel numbers',
            'Base for providers without numbers; blank or 0 = off',
          )}
          {textField(
            'min_channels',
            'Min channels',
            'Reject a refresh whose upstream catalog is smaller than this (before Dedupes / export filters)',
          )}
          {textField('refresh_interval', 'Refresh interval', 'Go duration, e.g. 3h')}
          <div className="config-field">
            <span className="config-field-label">Exclusions</span>
            <textarea
              rows={4}
              value={draft.exclusions}
              disabled={!data.source.writable}
              onChange={(e) => set('exclusions', e.target.value)}
            />
            <span className="field-hint">One case-insensitive regex per line</span>
          </div>
        </div>
      </section>

      <section className="detail-section">
        <h2>Source overrides</h2>
        <p className="meta">Only the fields this adapter reads are shown.</p>
        <div className="config-form">
          {OPTIONAL_FIELDS.filter((f) => support.has(f.key)).map((f) =>
            textField(f.key, f.label, f.hint),
          )}
          {support.has('headers') ? (
            <div className="config-field">
              <span className="config-field-label">Headers</span>
              <textarea
                rows={4}
                value={draft.headers}
                disabled={!data.source.writable}
                onChange={(e) => set('headers', e.target.value)}
              />
              <span className="field-hint">One "Name: value" per line</span>
            </div>
          ) : null}
        </div>
      </section>
    </>
  )
}
