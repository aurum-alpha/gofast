import { useCallback, useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'
import { formatHealthWhen } from '../lib/channel'
import {
  ConfigSaveError,
  fetchConfig,
  reloadSummary,
  saveConfig,
  type ConfigField,
  type ConfigResponse,
  type PathOp,
} from '../lib/config'

type FieldKind = 'text' | 'bool' | 'int' | 'float' | 'duration' | 'select'

type FieldSpec = {
  path: string
  label: string
  kind: FieldKind
  options?: string[]
  hint?: string
}

const GENERAL_FIELDS: FieldSpec[] = [
  {
    path: 'base_url',
    label: 'Base URL',
    kind: 'text',
    hint: 'Public origin clients use for logos and absolute links',
  },
  {
    path: 'proxy_base_url',
    label: 'Proxy base URL',
    kind: 'text',
    hint: 'FASTProxy origin; empty drops needs-proxy channels',
  },
  { path: 'proxy_all', label: 'Proxy all streams', kind: 'bool' },
  { path: 'cache_logos', label: 'Cache logos', kind: 'bool' },
  {
    path: 'timeouts.http_client',
    label: 'HTTP client timeout',
    kind: 'duration',
    hint: 'Outbound provider/logo fetches (e.g. 60s)',
  },
  {
    path: 'logging.level',
    label: 'Log level',
    kind: 'select',
    options: ['debug', 'info', 'warn', 'error'],
  },
]

const HEALTH_FIELDS: FieldSpec[] = [
  {
    path: 'health.consecutive_failures',
    label: 'Failures → down',
    kind: 'int',
    hint: 'Consecutive probe failures before a channel is DOWN',
  },
  { path: 'health.exclude_unhealthy', label: 'Exclude unhealthy', kind: 'bool' },
  { path: 'health.l1_interval', label: 'L1 interval', kind: 'duration' },
  { path: 'health.l1_workers', label: 'L1 workers', kind: 'int' },
  { path: 'health.l2_enabled', label: 'L2 (ffprobe) enabled', kind: 'bool' },
  { path: 'health.l2_interval', label: 'L2 interval', kind: 'duration' },
  { path: 'health.l2_workers', label: 'L2 workers', kind: 'int' },
  { path: 'health.l2_timeout', label: 'L2 timeout', kind: 'duration' },
  {
    path: 'health.l2_healthy_sample',
    label: 'L2 healthy sample',
    kind: 'float',
    hint: 'Fraction of healthy channels probed per L2 sweep (0–1)',
  },
  { path: 'health.max_per_host', label: 'Max probes per host', kind: 'int' },
  { path: 'health.soft_retries', label: 'Soft retries', kind: 'int' },
  { path: 'health.ffprobe_path', label: 'ffprobe path', kind: 'text' },
]

const DEPLOYMENT_FIELDS: FieldSpec[] = [
  { path: 'listen', label: 'Listen', kind: 'text' },
  { path: 'data_dir', label: 'Data dir', kind: 'text' },
]

type DraftValue = string | boolean

function draftFrom(fields: Record<string, ConfigField>, specs: FieldSpec[]): Record<string, DraftValue> {
  const out: Record<string, DraftValue> = {}
  for (const spec of specs) {
    const f = fields[spec.path]
    if (!f) continue
    out[spec.path] = spec.kind === 'bool' ? Boolean(f.value) : String(f.value ?? '')
  }
  return out
}

/** Converts a draft input back to a typed op value; null means invalid. */
function typedValue(spec: FieldSpec, raw: DraftValue): unknown | null {
  switch (spec.kind) {
    case 'bool':
      return raw === true
    case 'int': {
      const n = Number.parseInt(String(raw), 10)
      return Number.isFinite(n) && String(n) === String(raw).trim() ? n : null
    }
    case 'float': {
      const n = Number.parseFloat(String(raw))
      return Number.isFinite(n) ? n : null
    }
    default:
      return String(raw)
  }
}

function SourceBadge({ field }: { field: ConfigField }) {
  if (field.source === 'env') {
    return (
      <span className="badge badge-none" title="Environment always wins; unset the variable to edit here">
        set by {field.env}
      </span>
    )
  }
  if (field.restart_required) {
    return (
      <span className="badge badge-none" title="Edit config.yaml and restart to change">
        restart required
      </span>
    )
  }
  return null
}

function FieldControl({
  spec,
  field,
  value,
  onChange,
}: {
  spec: FieldSpec
  field: ConfigField
  value: DraftValue
  onChange: (v: DraftValue) => void
}) {
  const disabled = !field.editable
  if (spec.kind === 'bool') {
    return (
      <input
        type="checkbox"
        checked={value === true}
        disabled={disabled}
        onChange={(e) => onChange(e.target.checked)}
      />
    )
  }
  if (spec.kind === 'select') {
    return (
      <select value={String(value)} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
        {(spec.options ?? []).map((o) => (
          <option key={o} value={o}>
            {o}
          </option>
        ))}
      </select>
    )
  }
  return (
    <input
      type="text"
      inputMode={spec.kind === 'int' || spec.kind === 'float' ? 'decimal' : undefined}
      value={String(value)}
      disabled={disabled}
      onChange={(e) => onChange(e.target.value)}
    />
  )
}

function FieldRow({
  spec,
  field,
  value,
  dirty,
  onChange,
}: {
  spec: FieldSpec
  field: ConfigField
  value: DraftValue
  dirty: boolean
  onChange: (v: DraftValue) => void
}) {
  return (
    <div className={`config-field${dirty ? ' dirty' : ''}`}>
      <span className="config-field-label">
        {spec.label} <SourceBadge field={field} />
      </span>
      <FieldControl spec={spec} field={field} value={value} onChange={onChange} />
      {spec.hint ? <span className="field-hint">{spec.hint}</span> : null}
    </div>
  )
}

export function ConfigPage() {
  const [data, setData] = useState<ConfigResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [draft, setDraft] = useState<Record<string, DraftValue>>({})
  const [saving, setSaving] = useState(false)
  const [toast, setToast] = useState<{ ok: boolean; message: string } | null>(null)

  const editableSpecs = useMemo(() => [...GENERAL_FIELDS, ...HEALTH_FIELDS], [])

  const hydrate = useCallback(
    (body: ConfigResponse) => {
      setData(body)
      setDraft(draftFrom(body.fields, editableSpecs))
    },
    [editableSpecs],
  )

  const load = useCallback(async () => {
    const body = await fetchConfig()
    hydrate(body)
    setError(null)
  }, [hydrate])

  useEffect(() => {
    let cancelled = false
    load().catch((err: unknown) => {
      if (!cancelled) setError(err instanceof Error ? err.message : String(err))
    })
    return () => {
      cancelled = true
    }
  }, [load])

  const dirtyPaths = useMemo(() => {
    if (!data) return []
    return editableSpecs
      .filter((spec) => {
        const f = data.fields[spec.path]
        if (!f || !f.editable) return false
        const original = spec.kind === 'bool' ? Boolean(f.value) : String(f.value ?? '')
        return draft[spec.path] !== original
      })
      .map((spec) => spec.path)
  }, [data, draft, editableSpecs])

  async function save() {
    if (!data) return
    setSaving(true)
    setToast(null)
    try {
      const ops: PathOp[] = []
      for (const spec of editableSpecs) {
        if (!dirtyPaths.includes(spec.path)) continue
        const value = typedValue(spec, draft[spec.path])
        if (value === null) {
          setToast({ ok: false, message: `${spec.label}: invalid value` })
          setSaving(false)
          return
        }
        ops.push({ path: spec.path, value })
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
        <h1>Settings</h1>
        <div className="empty-panel" role="alert">
          Failed to load config: {error}
        </div>
      </>
    )
  }
  if (!data) {
    return (
      <>
        <h1>Settings</h1>
        <div className="empty-panel" role="status">
          Loading…
        </div>
      </>
    )
  }

  const setField = (path: string) => (v: DraftValue) =>
    setDraft((prev) => ({ ...prev, [path]: v }))

  const renderFields = (specs: FieldSpec[]) =>
    specs.map((spec) => {
      const f = data.fields[spec.path]
      if (!f) return null
      return (
        <FieldRow
          key={spec.path}
          spec={spec}
          field={f}
          value={draft[spec.path] ?? (spec.kind === 'bool' ? false : '')}
          dirty={dirtyPaths.includes(spec.path)}
          onChange={setField(spec.path)}
        />
      )
    })

  return (
    <>
      <h1>Settings</h1>
      <p className="lead">
        Edits here save to <code>config.yaml</code> and apply live — no restart.
        Hand-edits to the file itself need a restart. Fields set by environment
        variables are locked (env always wins). The <Link to="/groups">Groups</Link>{' '}
        taxonomy has its own editor.
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
          disabled={saving || !data.source.writable || dirtyPaths.length === 0}
        >
          {saving ? 'Saving…' : `Save & apply${dirtyPaths.length > 0 ? ` (${dirtyPaths.length})` : ''}`}
        </button>
        {toast ? (
          <span className={`meta config-toast${toast.ok ? '' : ' config-toast-error'}`} role="status">
            {toast.message}
          </span>
        ) : null}
      </div>

      <section className="detail-section">
        <h2>General</h2>
        <div className="config-form">{renderFields(GENERAL_FIELDS)}</div>
      </section>

      <section className="detail-section">
        <h2>Health probes</h2>
        <div className="config-form">{renderFields(HEALTH_FIELDS)}</div>
        {data.probe_schedule ? (
          <p className="meta">
            L1: last {formatHealthWhen(data.probe_schedule.last_l1_at)}, next{' '}
            {data.probe_schedule.l1_running
              ? 'running now'
              : formatHealthWhen(data.probe_schedule.next_l1_at)}
            {data.probe_schedule.l2_enabled ? (
              <>
                {' · '}L2: last {formatHealthWhen(data.probe_schedule.last_l2_at)}, next{' '}
                {data.probe_schedule.l2_running
                  ? 'running now'
                  : formatHealthWhen(data.probe_schedule.next_l2_at)}
              </>
            ) : null}
          </p>
        ) : null}
      </section>

      <section className="detail-section">
        <h2>Providers</h2>
        <p className="meta">
          Enable, label, schedule, and source overrides per provider — all apply
          live. Disabled providers keep their cache for instant re-enable.
        </p>
        <div className="table-wrap">
          <table className="channels">
            <thead>
              <tr>
                <th scope="col">ID</th>
                <th scope="col">Enabled</th>
                <th scope="col">Label</th>
                <th scope="col">Refresh</th>
                <th scope="col">Offset</th>
                <th scope="col">Min channels</th>
                <th scope="col"></th>
              </tr>
            </thead>
            <tbody>
              {data.providers.map((p) => (
                <tr key={p.settings.id}>
                  <td>
                    <Link to={`/config/providers/${encodeURIComponent(p.settings.id)}`}>
                      <code>{p.settings.id}</code>
                    </Link>
                  </td>
                  <td>
                    <span className={`badge ${p.settings.enabled ? 'badge-native' : 'badge-none'}`}>
                      {p.settings.enabled ? 'yes' : 'no'}
                    </span>
                  </td>
                  <td>{p.settings.label || '—'}</td>
                  <td>{p.settings.refresh_interval || '—'}</td>
                  <td className="number-cell">{p.settings.channel_number_offset || '—'}</td>
                  <td className="number-cell">{p.settings.min_channels}</td>
                  <td>
                    <Link to={`/config/providers/${encodeURIComponent(p.settings.id)}`}>
                      Edit settings
                    </Link>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section className="detail-section">
        <h2>Deployment</h2>
        <p className="meta">
          Restart-only: edit <code>{data.source.path}</code> (or the environment)
          and restart the server. Kept out of the editor so "in the UI = live"
          stays true.
        </p>
        <div className="config-form">
          {DEPLOYMENT_FIELDS.map((spec) => {
            const f = data.fields[spec.path]
            if (!f) return null
            return (
              <div className="config-field" key={spec.path}>
                <span className="config-field-label">
                  {spec.label} <SourceBadge field={f} />
                </span>
                <code>{String(f.value)}</code>
              </div>
            )
          })}
          <div className="config-field">
            <span className="config-field-label">Config path</span>
            <code>{data.source.path}</code>
          </div>
        </div>
      </section>

      <section className="detail-section">
        <h2>Artwork TLS</h2>
        {data.artwork_tls.length === 0 ? (
          <p className="meta">
            No per-host TLS exceptions (file-only: <code>artwork_tls</code> in{' '}
            <code>config.yaml</code>).
          </p>
        ) : (
          <div className="table-wrap">
            <table className="channels">
              <thead>
                <tr>
                  <th scope="col">Host</th>
                  <th scope="col">CA PEM</th>
                  <th scope="col">Insecure skip verify</th>
                </tr>
              </thead>
              <tbody>
                {data.artwork_tls.map((row) => (
                  <tr key={row.host}>
                    <td>
                      <code>{row.host}</code>
                    </td>
                    <td>{row.ca_pem_set ? 'set' : '—'}</td>
                    <td>{row.insecure_skip_verify ? 'yes' : 'no'}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>
    </>
  )
}
