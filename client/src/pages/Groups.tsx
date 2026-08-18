import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

type DiscoveredProvider = {
  id: string
  label: string
  group: string
  count: number
  disabled: boolean
}

type DiscoveredGroup = {
  name: string
  providers: DiscoveredProvider[]
  total: number
  auto_merged: boolean
  assigned_to?: string
  disabled: boolean
}

type MergeView = {
  name: string
  members: string[]
  enabled: boolean
}

type GroupsResponse = {
  enabled: boolean
  merges: MergeView[]
  disabled: string[]
  discovered: DiscoveredGroup[]
  preview: Record<string, { emitted_count: number; disabled_count: number }>
  read_only: boolean
}

const norm = (s: string) => s.trim().toLowerCase()

// providerSelector builds the "providerID/groupName" disable selector.
const providerSelector = (id: string, group: string) => `${id}/${group}`

export function GroupsPage() {
  const [server, setServer] = useState<GroupsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)

  // Editable draft state.
  const [enabled, setEnabled] = useState(false)
  const [merges, setMerges] = useState<MergeView[]>([])
  const [disabled, setDisabled] = useState<string[]>([])

  const [selected, setSelected] = useState<Set<string>>(new Set())
  const [newName, setNewName] = useState('')
  const [expanded, setExpanded] = useState<Set<string>>(new Set())
  const [saving, setSaving] = useState(false)
  const [toast, setToast] = useState<string | null>(null)

  function hydrate(body: GroupsResponse) {
    setServer(body)
    setEnabled(body.enabled)
    setMerges(body.merges.map((m) => ({ ...m, members: [...m.members] })))
    setDisabled([...body.disabled])
    setSelected(new Set())
    setNewName('')
  }

  useEffect(() => {
    let cancelled = false
    fetch('/api/groups')
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        return res.json() as Promise<GroupsResponse>
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

  // Map normalized upstream name -> canonical group it is assigned to (draft).
  const assignment = useMemo(() => {
    const m = new Map<string, string>()
    for (const g of merges) {
      for (const mem of g.members) m.set(norm(mem), g.name)
    }
    return m
  }, [merges])

  const disabledSet = useMemo(() => new Set(disabled.map((d) => norm(d))), [disabled])

  function isGloballyDisabled(name: string): boolean {
    return disabledSet.has(norm(name))
  }

  function isMergeDisabled(name: string): boolean {
    const canonical = assignment.get(norm(name))
    if (!canonical) return false
    const g = merges.find((x) => x.name === canonical)
    return g ? !g.enabled : false
  }

  // Live client-side preview so effective folders update before Save.
  const preview = useMemo(() => {
    const buckets = new Map<string, { emitted: number; disabled: number }>()
    const add = (key: string, emitted: number, dis: number) => {
      const b = buckets.get(key) ?? { emitted: 0, disabled: 0 }
      b.emitted += emitted
      b.disabled += dis
      buckets.set(key, b)
    }
    for (const g of discovered) {
      const canonical = assignment.get(norm(g.name))
      const key = enabled ? canonical ?? g.name : g.name
      const globallyOff = enabled && (isGloballyDisabled(g.name) || isMergeDisabled(g.name))
      for (const p of g.providers) {
        const perProviderOff =
          enabled && disabledSet.has(norm(providerSelector(p.id, g.name)))
        if (globallyOff || perProviderOff) add(key, 0, p.count)
        else add(key, p.count, 0)
      }
    }
    return [...buckets.entries()].sort((a, b) => a[0].localeCompare(b[0]))
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [discovered, assignment, disabledSet, enabled, merges])

  function toggleSelected(name: string) {
    setSelected((prev) => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  function toggleExpanded(name: string) {
    setExpanded((prev) => {
      const next = new Set(prev)
      if (next.has(name)) next.delete(name)
      else next.add(name)
      return next
    })
  }

  function toggleDisabledSelector(sel: string) {
    setDisabled((prev) =>
      prev.some((d) => norm(d) === norm(sel))
        ? prev.filter((d) => norm(d) !== norm(sel))
        : [...prev, sel],
    )
  }

  function addToGroup(groupName: string, names: string[]) {
    if (names.length === 0) return
    setMerges((prev) =>
      prev.map((g) => {
        if (g.name !== groupName) return g
        const have = new Set(g.members.map(norm))
        const additions = names.filter((n) => !have.has(norm(n)))
        return { ...g, members: [...g.members, ...additions] }
      }),
    )
    // Remove the added names from any other group (a member belongs to one group).
    setMerges((prev) =>
      prev.map((g) =>
        g.name === groupName
          ? g
          : { ...g, members: g.members.filter((mem) => !names.some((n) => norm(n) === norm(mem))) },
      ),
    )
    setSelected(new Set())
  }

  function createGroup() {
    const name = newName.trim()
    if (!name) return
    if (merges.some((g) => norm(g.name) === norm(name))) {
      setToast(`Group "${name}" already exists`)
      return
    }
    const members = [...selected]
    // A member belongs to one group: pull it from any existing group.
    setMerges((prev) => [
      ...prev.map((g) => ({
        ...g,
        members: g.members.filter((mem) => !members.some((n) => norm(n) === norm(mem))),
      })),
      { name, members, enabled: true },
    ])
    setNewName('')
    setSelected(new Set())
  }

  function removeMember(groupName: string, member: string) {
    setMerges((prev) =>
      prev.map((g) =>
        g.name === groupName
          ? { ...g, members: g.members.filter((m) => norm(m) !== norm(member)) }
          : g,
      ),
    )
  }

  function renameGroup(oldName: string, next: string) {
    setMerges((prev) => prev.map((g) => (g.name === oldName ? { ...g, name: next } : g)))
  }

  function toggleGroupEnabled(groupName: string) {
    setMerges((prev) =>
      prev.map((g) => (g.name === groupName ? { ...g, enabled: !g.enabled } : g)),
    )
  }

  function deleteGroup(groupName: string) {
    setMerges((prev) => prev.filter((g) => g.name !== groupName))
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
          enabled: g.enabled,
        })),
        disabled,
      }
      const res = await fetch('/api/groups', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      })
      if (!res.ok) {
        const text = await res.text()
        throw new Error(text || `${res.status} ${res.statusText}`)
      }
      const refreshed = (await res.json()) as GroupsResponse
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
        <h1>Groups</h1>
        <div className="empty-panel" role="alert">
          Failed to load groups: {error}
        </div>
      </>
    )
  }
  if (!server) {
    return (
      <>
        <h1>Groups</h1>
        <div className="empty-panel" role="status">
          Loading…
        </div>
      </>
    )
  }

  const groupNames = merges.map((g) => g.name)

  return (
    <>
      <h1>Groups</h1>
      <p className="lead">
        Merge providers' upstream groups into your own named folders and disable
        the ones you don't want. Identical upstream names are auto-merged. Saving
        writes the <code>groups</code> block to <code>config.yaml</code> and
        applies live — <Link to="/channels">channels</Link> re-emit without a
        restart.
      </p>

      {server.read_only ? (
        <div className="empty-panel" role="alert">
          <strong>Config is read-only.</strong> Mount <code>/data</code> (or the
          config path) read-write to save group changes.
        </div>
      ) : null}

      <div className="groups-toolbar">
        <label className="groups-enable">
          <input
            type="checkbox"
            checked={enabled}
            onChange={(e) => setEnabled(e.target.checked)}
          />{' '}
          Enable group taxonomy
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
          Taxonomy is off: playlists keep the legacy{' '}
          <code>{'{label}: {group}'}</code> folders. Turn it on to merge and
          disable.
        </p>
      ) : null}

      <div className="groups-board">
        <section className="groups-col">
          <h2>Upstream groups</h2>
          <div className="groups-actions">
            <input
              type="text"
              value={newName}
              placeholder="New group name…"
              onChange={(e) => setNewName(e.target.value)}
            />
            <button type="button" onClick={createGroup} disabled={!newName.trim()}>
              Create{selected.size > 0 ? ` from ${selected.size}` : ''}
            </button>
            {groupNames.length > 0 ? (
              <select
                defaultValue=""
                onChange={(e) => {
                  if (e.target.value) addToGroup(e.target.value, [...selected])
                  e.target.value = ''
                }}
                disabled={selected.size === 0}
              >
                <option value="">Add selected to…</option>
                {groupNames.map((n) => (
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
              const cardDisabled =
                enabled && (isGloballyDisabled(g.name) || isMergeDisabled(g.name))
              const isOpen = expanded.has(g.name)
              return (
                <li
                  key={g.name}
                  className={`pool-card${assignedTo ? ' placed' : ''}${
                    cardDisabled ? ' disabled' : ''
                  }`}
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
                  <div className="pool-card-controls">
                    <label>
                      <input
                        type="checkbox"
                        checked={isGloballyDisabled(g.name)}
                        onChange={() => toggleDisabledSelector(g.name)}
                        disabled={!enabled}
                      />{' '}
                      Disable everywhere
                    </label>
                    {g.providers.length > 1 ? (
                      <button
                        type="button"
                        className="linklike"
                        onClick={() => toggleExpanded(g.name)}
                      >
                        {isOpen ? 'Hide providers' : 'Per-provider…'}
                      </button>
                    ) : null}
                  </div>
                  {isOpen ? (
                    <ul className="pool-card-providers">
                      {g.providers.map((p) => {
                        const sel = providerSelector(p.id, g.name)
                        return (
                          <li key={p.id}>
                            <label>
                              <input
                                type="checkbox"
                                checked={disabledSet.has(norm(sel))}
                                onChange={() => toggleDisabledSelector(sel)}
                                disabled={!enabled}
                              />{' '}
                              Disable {p.label} ({p.count})
                            </label>
                          </li>
                        )
                      })}
                    </ul>
                  ) : null}
                </li>
              )
            })}
            {discovered.length === 0 ? (
              <li className="meta">
                No upstream groups yet — wait for a successful provider refresh.
              </li>
            ) : null}
          </ul>
        </section>

        <section className="groups-col">
          <h2>Your groups</h2>
          {merges.length === 0 ? (
            <p className="meta">
              No merged groups yet. Select upstream cards and create one.
            </p>
          ) : null}
          <ul className="groups-list">
            {merges.map((g) => (
              <li key={g.name} className={`group-bucket${g.enabled ? '' : ' disabled'}`}>
                <div className="group-bucket-head">
                  <input
                    className="group-name-input"
                    type="text"
                    value={g.name}
                    onChange={(e) => renameGroup(g.name, e.target.value)}
                  />
                  <label className="meta">
                    <input
                      type="checkbox"
                      checked={g.enabled}
                      onChange={() => toggleGroupEnabled(g.name)}
                    />{' '}
                    enabled
                  </label>
                  <button
                    type="button"
                    className="linklike danger"
                    onClick={() => deleteGroup(g.name)}
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
          <h2>Effective folders</h2>
          <p className="meta">
            Live preview of Jellyfin folders and channel counts (before / after
            save).
          </p>
          <ul className="groups-preview">
            {preview.map(([name, counts]) => (
              <li key={name}>
                <span className="preview-name">{name || '(none)'}</span>
                <span className="badge badge-native">{counts.emitted}</span>
                {counts.disabled > 0 ? (
                  <span className="badge badge-none" title="disabled channels">
                    {counts.disabled} off
                  </span>
                ) : null}
              </li>
            ))}
            {preview.length === 0 ? <li className="meta">Nothing to emit yet.</li> : null}
          </ul>
        </section>
      </div>
    </>
  )
}
