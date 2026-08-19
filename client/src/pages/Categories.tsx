import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

type DiscoveredProvider = {
  id: string
  label: string
  count: number
}

type DiscoveredCategory = {
  name: string
  providers: DiscoveredProvider[]
  total: number
  auto_merged: boolean
  assigned_to?: string
}

type MergeView = {
  name: string
  members: string[]
}

type CategoriesResponse = {
  enabled: boolean
  merges: MergeView[]
  discovered: DiscoveredCategory[]
  preview: Record<string, { programme_count: number }>
  read_only: boolean
}

const norm = (s: string) => s.trim().toLowerCase()

export function CategoriesPage() {
  const [server, setServer] = useState<CategoriesResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  const [enabled, setEnabled] = useState(false)
  const [merges, setMerges] = useState<MergeView[]>([])

  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [newName, setNewName] = useState('')
  const [saving, setSaving] = useState(false)
  const [toast, setToast] = useState<string | null>(null)

  function hydrate(body: CategoriesResponse) {
    setServer(body)
    setEnabled(body.enabled)
    setMerges(body.merges.map((m) => ({ ...m, members: [...m.members] })))
    setSelected(new Set())
    setNewName('')
  }

  useEffect(() => {
    let cancelled = false
    fetch('/api/categories')
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        return res.json() as Promise<CategoriesResponse>
      })
      .then((body) => {
        if (!cancelled) {
          hydrate(body)
          setError(null)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(err instanceof Error ? err.message : String(err))
      })
    return () => {
      cancelled = true
    }
  }, [])

  const discovered = server?.discovered ?? []

  const assignment = useMemo(() => {
    const m = new Map<string, string>()
    for (const g of merges) {
      for (const mem of g.members) m.set(norm(mem), g.name)
    }
    return m
  }, [merges])

  // Live client-side preview of effective category buckets / programme counts.
  const preview = useMemo(() => {
    const buckets = new Map<string, number>()
    for (const g of discovered) {
      const canonical = assignment.get(norm(g.name))
      const key = enabled ? canonical ?? g.name : g.name
      buckets.set(key, (buckets.get(key) ?? 0) + g.total)
    }
    return [...buckets.entries()].sort((a, b) => a[0].localeCompare(b[0]))
  }, [discovered, assignment, enabled])

  function toggleSelected(name: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  function addToMerge(mergeName: string, names: string[]) {
    if (names.length === 0) return
    setMerges((prev) =>
      prev.map((g) => {
        if (g.name !== mergeName) {
          return {
            ...g,
            members: g.members.filter((mem) => !names.some((n) => norm(n) === norm(mem))),
          }
        }
        const have = new Set(g.members.map(norm))
        const additions = names.filter((n) => !have.has(norm(n)))
        return { ...g, members: [...g.members, ...additions] }
      }),
    )
    setSelected(new Set())
  }

  function createMerge() {
    const name = newName.trim()
    if (!name) return
    if (merges.some((g) => norm(g.name) === norm(name))) {
      setToast(`Category "${name}" already exists`)
      return
    }
    const members = [...selected]
    setMerges((prev) => [
      ...prev.map((g) => ({
        ...g,
        members: g.members.filter((mem) => !members.some((n) => norm(n) === norm(mem))),
      })),
      { name, members },
    ])
    setNewName('')
    setSelected(new Set())
  }

  function removeMember(mergeName: string, member: string) {
    setMerges((prev) =>
      prev.map((g) =>
        g.name === mergeName
          ? { ...g, members: g.members.filter((m) => norm(m) !== norm(member)) }
          : g,
      ),
    )
  }

  function renameMerge(oldName: string, next: string) {
    setMerges((prev) => prev.map((g) => (g.name === oldName ? { ...g, name: next } : g)))
  }

  function deleteMerge(mergeName: string) {
    setMerges((prev) => prev.filter((g) => g.name !== mergeName))
  }

  async function save() {
    setSaving(true)
    setToast(null)
    try {
      const body = {
        enabled,
        merges: merges.map((g) => ({
          name: g.name.trim(),
          members: g.members,
        })),
      }
      const res = await fetch('/api/categories', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text || `${res.status} ${res.statusText}`)
      }
      const refreshed = (await res.json()) as CategoriesResponse
      hydrate(refreshed)
      setToast('Saved and applied — no restart needed.')
    } catch (err: unknown) {
      setToast(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  if (error) {
    return (
      <>
        <h1>Categories</h1>
        <div className="empty-panel" role="alert">
          Failed to load categories: {error}
        </div>
      </>
    )
  }
  if (!server) {
    return (
      <>
        <h1>Categories</h1>
        <div className="empty-panel" role="status">
          Loading…
        </div>
      </>
    )
  }

  const mergeNames = merges.map((g) => g.name)

  return (
    <>
      <h1>Categories</h1>
      <p className="lead">
        Normalize messy provider programme categories (<code>Movie</code> /{' '}
        <code>Movies</code> / <code>Film</code>) into canonical names for XMLTV
        and <Link to="/guide">Guide</Link> colors. There is no disable — a
        category is a label on an airing. Saving writes the{' '}
        <code>categories</code> block to <code>config.yaml</code> and applies
        live.
      </p>

      {server.read_only ? (
        <div className="empty-panel" role="alert">
          <strong>Config is read-only.</strong> Mount <code>/data</code> (or the
          config path) read-write to save category changes.
        </div>
      ) : null}

      <div className="groups-toolbar">
        <label className="groups-enable">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
          />{' '}
          Enable category taxonomy
        </label>
        <button type="button" onClick={save} disabled={saving || server.read_only}>
          {saving ? 'Saving…' : 'Save & apply'}
        </button>
        {toast ? (
          <span className="meta groups-toast" role="status">
            {toast}
          </span>
        ) : null}
      </div>

      {!enabled ? (
        <p className="meta">
          Taxonomy is off: Guide and XMLTV keep each provider&apos;s raw category
          strings. Turn it on to merge labels.
        </p>
      ) : null}

      <div className="groups-board">
        <section className="groups-col">
          <h2>Upstream categories</h2>
          <div className="groups-actions">
            <input
              type="text"
              value={newName}
              placeholder="New category name…"
              onChange={(e) => setNewName(e.target.value)}
            />
            <button type="button" onClick={createMerge} disabled={!newName.trim()}>
              Create{selected.size > 0 ? ` from ${selected.size}` : ''}
            </button>
            {mergeNames.length > 0 ? (
              <select
                defaultValue=""
                onChange={(e) => {
                  if (e.target.value) addToMerge(e.target.value, [...selected])
                  e.target.value = ''
                }}
                disabled={selected.size === 0}
              >
                <option value="">Add selected to…</option>
                {mergeNames.map((n) => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            ) : null}
          </div>
          <ul className="groups-pool">
            {discovered.map((g) => {
              const assignedTo = assignment.get(norm(g.name))
              return (
                <li
                  key={g.name}
                  className={`pool-card${assignedTo ? ' placed' : ''}`}
                >
                  <div className="pool-card-head">
                    <label className="pool-card-name">
                      <input
                        type="checkbox"
                        checked={selected.has(g.name)}
                        onChange={() => toggleSelected(g.name)}
                        disabled={!enabled}
                      />{' '}
                      <span>{g.name}</span>
                    </label>
                    <span className="meta">{g.total}</span>
                  </div>
                  <div className="pool-card-badges">
                    {g.providers.map((p) => (
                      <span key={p.id} className="badge badge-beacon" title={p.label}>
                        {p.id} {p.count}
                      </span>
                    ))}
                    {g.auto_merged ? (
                      <span className="badge badge-native">auto-merged</span>
                    ) : null}
                    {assignedTo ? (
                      <span className="badge badge-none">→ {assignedTo}</span>
                    ) : null}
                  </div>
                </li>
              )
            })}
            {discovered.length === 0 ? (
              <li className="meta">
                No upstream categories yet — wait for a provider with XMLTV{' '}
                <code>&lt;category&gt;</code> (e.g. Pluto) to refresh.
              </li>
            ) : null}
          </ul>
        </section>

        <section className="groups-col">
          <h2>Your merges</h2>
          {merges.length === 0 ? (
            <p className="meta">
              No merges yet. Select upstream cards and create one.
            </p>
          ) : null}
          <ul className="groups-list">
            {merges.map((g) => (
              <li key={g.name} className="group-bucket">
                <div className="group-bucket-head">
                  <input
                    className="group-name-input"
                    type="text"
                    value={g.name}
                    onChange={(e) => renameMerge(g.name, e.target.value)}
                  />
                  <button
                    type="button"
                    className="linklike danger"
                    onClick={() => deleteMerge(g.name)}
                  >
                    delete
                  </button>
                </div>
                <div className="group-members">
                  {g.members.length === 0 ? (
                    <span className="meta">no members</span>
                  ) : (
                    g.members.map((m) => (
                      <span key={m} className="member-chip">
                        {m}
                        <button
                          type="button"
                          aria-label={`Remove ${m}`}
                          onClick={() => removeMember(g.name, m)}
                        >
                          ×
                        </button>
                      </span>
                    ))
                  )}
                </div>
              </li>
            ))}
          </ul>
        </section>

        <section className="groups-col">
          <h2>Effective categories</h2>
          <p className="meta">
            Live preview of canonical labels and programme counts (before /
            after save).
          </p>
          <ul className="groups-preview">
            {preview.map(([name, count]) => (
              <li key={name}>
                <span className="preview-name">{name || '(none)'}</span>
                <span className="badge badge-native">{count}</span>
              </li>
            ))}
            {preview.length === 0 ? (
              <li className="meta">Nothing to categorize yet.</li>
            ) : null}
          </ul>
        </section>
      </div>
    </>
  )
}
