import { useEffect, useMemo, useState } from 'react'
import { Link } from 'react-router-dom'

type ExportMode = 'auto' | 'enabled' | 'disabled'

type Member = {
  provider: string
  id: string
  normalized_id: string
  name: string
  emitted_group?: string
  region?: string
  number: number
  offset_number: number
  classification?: string
  health?: string
  exportable: boolean
  export: ExportMode
  filter_reason?: string
}

type Cluster = {
  key: string
  title: string
  status: 'unresolved' | 'resolved'
  exportable_count: number
  keep_all: boolean
  members: Member[]
}

type DedupesResponse = {
  revision: string
  read_only: boolean
  preferred_providers: string[]
  keep_all_keys: string[]
  summary: { clusters: number; unresolved: number; channels_involved: number }
  clusters: Cluster[]
}

type PendingAction = {
  provider: string
  id: string
  export: ExportMode
}

type FilterMode = 'needs_review' | 'resolved' | 'keep_all' | 'all'

const DEFAULT_PREFERRED = [
  'samsung',
  'pluto',
  'xumo',
  'tcl',
  'tubi',
  'lg',
  'roku',
  'plex',
  'localnow',
]

function memberKey(m: Member) {
  return `${m.provider}/${m.normalized_id}`
}

export function DedupesPage() {
  const [server, setServer] = useState<DedupesResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [preferred, setPreferred] = useState<string[]>([])
  const [keepAllKeys, setKeepAllKeys] = useState<string[]>([])
  const [actions, setActions] = useState<PendingAction[]>([])
  const [filter, setFilter] = useState<FilterMode>('needs_review')
  const [search, setSearch] = useState('')
  const [selectedKey, setSelectedKey] = useState<string | null>(null)
  const [selectedMembers, setSelectedMembers] = useState<Set<string>>(new Set())
  const [saving, setSaving] = useState(false)
  const [toast, setToast] = useState<string | null>(null)
  const [editingPreferred, setEditingPreferred] = useState(false)
  const [preferredDraft, setPreferredDraft] = useState('')

  function hydrate(body: DedupesResponse) {
    setServer(body)
    setPreferred(
      body.preferred_providers?.length
        ? [...body.preferred_providers]
        : [...DEFAULT_PREFERRED],
    )
    setKeepAllKeys([...(body.keep_all_keys ?? [])])
    setActions([])
    setSelectedMembers(new Set())
  }

  function reload() {
    return fetch('/api/dedupes')
      .then(async (res) => {
        if (!res.ok) throw new Error(`${res.status} ${res.statusText}`)
        return res.json() as Promise<DedupesResponse>
      })
      .then((body) => {
        hydrate(body)
        setError(null)
        if (!selectedKey && body.clusters[0]) {
          setSelectedKey(body.clusters[0].key)
        }
      })
  }

  useEffect(() => {
    let cancelled = false
    reload().catch((err: unknown) => {
      if (!cancelled) setError(err instanceof Error ? err.message : String(err))
    })
    return () => {
      cancelled = true
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  const keepAllSet = useMemo(() => new Set(keepAllKeys), [keepAllKeys])

  const actionMap = useMemo(() => {
    const m = new Map<string, ExportMode>()
    for (const a of actions) m.set(`${a.provider}/${a.id}`, a.export)
    return m
  }, [actions])

  function effectiveExport(m: Member): ExportMode {
    return actionMap.get(memberKey(m)) ?? m.export ?? 'auto'
  }

  function effectiveExportable(m: Member): boolean {
    const exp = effectiveExport(m)
    if (exp === 'disabled') return false
    if (exp === 'enabled') return true
    return m.exportable
  }

  const clusters = useMemo(() => {
    if (!server) return []
    return server.clusters.map((c) => {
      const exportableCount = c.members.filter(effectiveExportable).length
      const status: Cluster['status'] = exportableCount >= 2 ? 'unresolved' : 'resolved'
      return {
        ...c,
        status,
        exportable_count: exportableCount,
        keep_all: keepAllSet.has(c.key),
      }
    })
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [server, actionMap, keepAllSet])

  const visible = useMemo(() => {
    const q = search.trim().toLowerCase()
    return clusters.filter((c) => {
      if (filter === 'needs_review' && (c.status !== 'unresolved' || c.keep_all)) return false
      if (filter === 'resolved' && c.status !== 'resolved') return false
      if (filter === 'keep_all' && !c.keep_all) return false
      if (!q) return true
      if (c.title.toLowerCase().includes(q) || c.key.includes(q)) return true
      return c.members.some(
        (m) =>
          m.provider.includes(q) ||
          m.name.toLowerCase().includes(q) ||
          m.normalized_id.toLowerCase().includes(q),
      )
    })
  }, [clusters, filter, search])

  const selected = clusters.find((c) => c.key === selectedKey) ?? visible[0] ?? null

  useEffect(() => {
    if (selected && selected.key !== selectedKey) setSelectedKey(selected.key)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [visible])

  const pendingCount = actions.length

  function queueActions(next: PendingAction[]) {
    setActions((prev) => {
      const m = new Map(prev.map((a) => [`${a.provider}/${a.id}`, a]))
      for (const a of next) m.set(`${a.provider}/${a.id}`, a)
      return [...m.values()]
    })
  }

  function keepMembers(cluster: Cluster, keepIds: Set<string>) {
    const next: PendingAction[] = []
    for (const m of cluster.members) {
      const key = memberKey(m)
      if (keepIds.has(key)) {
        next.push({ provider: m.provider, id: m.normalized_id, export: 'enabled' })
      } else {
        next.push({ provider: m.provider, id: m.normalized_id, export: 'disabled' })
      }
    }
    queueActions(next)
    setKeepAllKeys((prev) => prev.filter((k) => k !== cluster.key))
    setSelectedMembers(new Set())
    setToast(`Queued keep ${keepIds.size} of ${cluster.members.length} for ${cluster.title}`)
  }

  function keepSelected() {
    if (!selected || selectedMembers.size === 0) return
    keepMembers(selected, selectedMembers)
  }

  function keepPreferredInCluster(cluster: Cluster) {
    let winner: Member | null = null
    for (const p of preferred) {
      const hit = cluster.members.find((m) => m.provider === p && effectiveExportable(m))
      if (hit) {
        winner = hit
        break
      }
    }
    if (!winner) {
      // Fall back to first exportable member
      winner = cluster.members.find((m) => effectiveExportable(m)) ?? null
    }
    if (!winner) {
      setToast(`No exportable member in ${cluster.title}`)
      return
    }
    keepMembers(cluster, new Set([memberKey(winner)]))
  }

  function keepPreferredVisible() {
    const next: PendingAction[] = []
    const dropKeepAll = new Set<string>()
    let n = 0
    for (const c of visible) {
      if (c.status !== 'unresolved' || c.keep_all) continue
      let winner: Member | null = null
      for (const p of preferred) {
        const hit = c.members.find((m) => m.provider === p && effectiveExportable(m))
        if (hit) {
          winner = hit
          break
        }
      }
      if (!winner) winner = c.members.find((m) => effectiveExportable(m)) ?? null
      if (!winner) continue
      const keep = memberKey(winner)
      for (const m of c.members) {
        next.push({
          provider: m.provider,
          id: m.normalized_id,
          export: memberKey(m) === keep ? 'enabled' : 'disabled',
        })
      }
      dropKeepAll.add(c.key)
      n++
    }
    if (n === 0) {
      setToast('No unresolved clusters in view')
      return
    }
    queueActions(next)
    setKeepAllKeys((prev) => prev.filter((k) => !dropKeepAll.has(k)))
    setToast(`Queued keep-preferred for ${n} clusters`)
  }

  function keepAll() {
    if (!selected) return
    const next: PendingAction[] = selected.members.map((m) => ({
      provider: m.provider,
      id: m.normalized_id,
      export: 'auto' as ExportMode,
    }))
    queueActions(next)
    setKeepAllKeys((prev) => (prev.includes(selected.key) ? prev : [...prev, selected.key]))
    setSelectedMembers(new Set())
    setToast(`Queued keep-all for ${selected.title}`)
  }

  function dropAll() {
    if (!selected) return
    const next: PendingAction[] = selected.members.map((m) => ({
      provider: m.provider,
      id: m.normalized_id,
      export: 'disabled' as ExportMode,
    }))
    queueActions(next)
    setKeepAllKeys((prev) => prev.filter((k) => k !== selected.key))
    setSelectedMembers(new Set())
    setToast(`Queued drop-all for ${selected.title}`)
  }

  function toggleMember(m: Member) {
    const key = memberKey(m)
    setSelectedMembers((prev) => {
      const next = new Set(prev)
      if (next.has(key)) next.delete(key)
      else next.add(key)
      return next
    })
  }

  function savePreferredDraft() {
    const ids = preferredDraft
      .split(/[,\s]+/)
      .map((s) => s.trim().toLowerCase())
      .filter(Boolean)
    const seen = new Set<string>()
    const out: string[] = []
    for (const id of ids) {
      if (seen.has(id)) continue
      seen.add(id)
      out.push(id)
    }
    setPreferred(out.length ? out : [...DEFAULT_PREFERRED])
    setEditingPreferred(false)
  }

  async function apply() {
    if (!server || server.read_only) return
    setSaving(true)
    setToast(null)
    try {
      const res = await fetch('/api/dedupes/apply', {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          revision: server.revision,
          preferred_providers: preferred,
          keep_all_keys: keepAllKeys,
          actions,
        }),
      })
      if (!res.ok) throw new Error(await res.text())
      const body = (await res.json()) as DedupesResponse
      hydrate(body)
      setToast('Applied — export updated')
    } catch (err: unknown) {
      setToast(err instanceof Error ? err.message : String(err))
    } finally {
      setSaving(false)
    }
  }

  if (error) {
    return (
      <section className="panel">
        <h1>Dedupes</h1>
        <p className="error">{error}</p>
      </section>
    )
  }

  if (!server) {
    return (
      <section className="panel">
        <h1>Dedupes</h1>
        <p className="subtle">Loading…</p>
      </section>
    )
  }

  return (
    <section className="panel dedupes-page">
      <div className="groups-toolbar">
        <div>
          <h1>Dedupes</h1>
          <p className="subtle">
            Same display title across providers (exact match after folding). BET / BET Pluto TV /
            BET Her stay separate. Keep/drop writes channel emit export — nothing is purged
            silently.
          </p>
        </div>
        <div className="groups-toolbar-actions">
          <label>
            Filter{' '}
            <select value={filter} onChange={(e) => setFilter(e.target.value as FilterMode)}>
              <option value="needs_review">Needs review</option>
              <option value="resolved">Resolved</option>
              <option value="keep_all">Keep-all</option>
              <option value="all">All</option>
            </select>
          </label>
          <input
            type="search"
            placeholder="Search title / provider…"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
          />
          <button
            type="button"
            className="btn"
            onClick={() => setEditingPreferred((v) => !v)}
          >
            Preferred: {preferred.slice(0, 3).join(', ')}
            {preferred.length > 3 ? '…' : ''}
          </button>
          <button
            type="button"
            className="btn"
            onClick={keepPreferredVisible}
            disabled={server.read_only}
          >
            Keep preferred (visible)
          </button>
          <button
            type="button"
            className="btn primary"
            onClick={apply}
            disabled={
              server.read_only ||
              saving ||
              (pendingCount === 0 && !preferredChanged(server, preferred, keepAllKeys))
            }
          >
            {saving ? 'Applying…' : `Apply${pendingCount ? ` (${pendingCount})` : ''}`}
          </button>
        </div>
      </div>

      {editingPreferred && (
        <div className="dedupe-preferred-edit">
          <label>
            Preferred provider order (comma or space separated)
            <input
              value={preferredDraft || preferred.join(', ')}
              onChange={(e) => setPreferredDraft(e.target.value)}
              onFocus={() => setPreferredDraft(preferred.join(', '))}
            />
          </label>
          <button type="button" className="btn" onClick={savePreferredDraft}>
            Set order
          </button>
        </div>
      )}

      <p className="subtle">
        {server.summary.clusters} clusters · {server.summary.channels_involved} channels ·{' '}
        {server.summary.unresolved} unresolved
        {server.read_only ? ' · config read-only' : ''}
        {pendingCount ? ` · ${pendingCount} pending emit changes` : ''}
      </p>

      {toast && <p className="toast">{toast}</p>}

      <div className="dedupe-board">
        <div className="dedupe-list">
          {visible.length === 0 && <p className="subtle">No clusters match this filter.</p>}
          {visible.map((c) => (
            <button
              key={c.key}
              type="button"
              className={`dedupe-cluster-row${selected?.key === c.key ? ' active' : ''}${c.keep_all ? ' keep-all' : ''}`}
              onClick={() => {
                setSelectedKey(c.key)
                setSelectedMembers(new Set())
              }}
            >
              <span className="dedupe-cluster-title">{c.title}</span>
              <span className="subtle">
                {c.members.length} · {providerList(c)} · {c.status}
                {c.keep_all ? ' · keep-all' : ''}
              </span>
            </button>
          ))}
        </div>

        <div className="dedupe-detail">
          {!selected && <p className="subtle">Select a cluster.</p>}
          {selected && (
            <>
              <h2>{selected.title}</h2>
              <p className="subtle">
                key <code>{selected.key}</code> · {selected.exportable_count} exportable ·{' '}
                {selected.status}
              </p>
              <table className="dedupe-members">
                <thead>
                  <tr>
                    <th></th>
                    <th>Provider</th>
                    <th>Region</th>
                    <th>Name</th>
                    <th>Prov #</th>
                    <th>Emit #</th>
                    <th>Group</th>
                    <th>Class</th>
                    <th>Health</th>
                    <th>Export</th>
                  </tr>
                </thead>
                <tbody>
                  {selected.members.map((m) => {
                    const key = memberKey(m)
                    const exp = effectiveExport(m)
                    const exportable = effectiveExportable(m)
                    return (
                      <tr key={key} className={exportable ? '' : 'muted'}>
                        <td>
                          <input
                            type="checkbox"
                            checked={selectedMembers.has(key)}
                            onChange={() => toggleMember(m)}
                            disabled={server.read_only}
                          />
                        </td>
                        <td>
                          <Link to={`/channels/${m.provider}/${encodeURIComponent(m.normalized_id)}`}>
                            {m.provider}
                          </Link>
                        </td>
                        <td>{m.region || '—'}</td>
                        <td className="dedupe-member-name">{m.name || '—'}</td>
                        <td>{m.number || '—'}</td>
                        <td>{m.offset_number || '—'}</td>
                        <td>{m.emitted_group || '—'}</td>
                        <td>{m.classification || '—'}</td>
                        <td>{m.health || '—'}</td>
                        <td>
                          {exp}
                          {!exportable && m.filter_reason ? (
                            <span className="subtle"> ({m.filter_reason})</span>
                          ) : null}
                        </td>
                      </tr>
                    )
                  })}
                </tbody>
              </table>
              <div className="dedupe-actions">
                <button
                  type="button"
                  className="btn"
                  onClick={keepSelected}
                  disabled={server.read_only || selectedMembers.size === 0}
                >
                  Keep selected
                </button>
                <button
                  type="button"
                  className="btn"
                  onClick={() => keepPreferredInCluster(selected)}
                  disabled={server.read_only}
                >
                  Keep preferred
                </button>
                <button type="button" className="btn" onClick={keepAll} disabled={server.read_only}>
                  Keep all
                </button>
                <button type="button" className="btn" onClick={dropAll} disabled={server.read_only}>
                  Drop all
                </button>
              </div>
            </>
          )}
        </div>
      </div>
    </section>
  )
}

function providerList(c: Cluster) {
  const ids = [...new Set(c.members.map((m) => m.provider))]
  return `${ids.length} providers`
}

function preferredChanged(server: DedupesResponse, preferred: string[], keepAll: string[]) {
  const a = (server.preferred_providers ?? []).join(',')
  const b = preferred.join(',')
  const ka = [...(server.keep_all_keys ?? [])].sort().join(',')
  const kb = [...keepAll].sort().join(',')
  // Treat empty server preferred as matching our client default so Apply isn't
  // forced dirty on first load.
  const serverPref = a || DEFAULT_PREFERRED.join(',')
  return serverPref !== b || ka !== kb
}
