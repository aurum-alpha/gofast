import { useEffect, useState } from 'react'

type HostNode = {
  label: string
  count: number
  children: HostNode[]
}

type HostsResponse = {
  url_field: string
  tree: HostNode[]
  unparsed: number
}

function displayLabel(label: string): string {
  // IPs stay literal; DNS labels get a trailing dot (reverse-DNS style).
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(label) || label.includes(':')) {
    return label
  }
  return `${label}.`
}

function HostTreeRows({
  nodes,
  path,
  expanded,
  toggle,
}: {
  nodes: HostNode[]
  path: string
  expanded: Record<string, boolean>
  toggle: (key: string) => void
}) {
  return (
    <>
      {nodes.map((n) => {
        const key = path ? `${path}/${n.label}` : n.label
        const hasKids = n.children.length > 0
        const open = Boolean(expanded[key])
        const depth = path ? path.split('/').length : 0
        return (
          <div key={key} className="host-tree-block">
            <div
              className="host-tree-row"
              style={{ paddingLeft: `${depth * 1.25}rem` }}
            >
              {hasKids ? (
                <button
                  type="button"
                  className="host-expand"
                  aria-expanded={open}
                  onClick={() => toggle(key)}
                >
                  {open ? '▾' : '▸'}{' '}
                  <code>{displayLabel(n.label)}</code>
                </button>
              ) : (
                <span className="host-leaf">
                  <code>{displayLabel(n.label)}</code>
                </span>
              )}
              <span className="host-tree-count">{n.count.toLocaleString()}</span>
            </div>
            {hasKids && open ? (
              <HostTreeRows
                nodes={n.children}
                path={key}
                expanded={expanded}
                toggle={toggle}
              />
            ) : null}
          </div>
        )
      })}
    </>
  )
}

export function HostsPage() {
  const [data, setData] = useState<HostsResponse | null>(null)
  const [error, setError] = useState<string | null>(null)
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})

  useEffect(() => {
    let cancelled = false
    fetch('/api/channels/hosts')
      .then(async (res) => {
        if (!res.ok) {
          throw new Error(`${res.status} ${res.statusText}`)
        }
        return res.json() as Promise<HostsResponse>
      })
      .then((body) => {
        if (!cancelled) {
          setData(body)
          setError(null)
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setError(err instanceof Error ? err.message : String(err))
        }
      })
    return () => {
      cancelled = true
    }
  }, [])

  function toggle(key: string) {
    setExpanded((prev) => ({ ...prev, [key]: !prev[key] }))
  }

  return (
    <>
      <h1>Hosts</h1>
      <p className="lead">
        Live lineup rollup as a reverse-DNS tree: TLD first, then expand into
        domains and subdomains. Counts recompute on each load from the
        in-memory channel list.
      </p>

      {error && (
        <div className="empty-panel" role="alert">
          <p>Failed to load hosts: {error}</p>
        </div>
      )}

      {!error && !data && (
        <div className="empty-panel" role="status">
          <p>Loading…</p>
        </div>
      )}

      {data && (
        <>
          <p className="meta">
            Field: <code>{data.url_field}</code>
            {data.unparsed > 0
              ? ` · ${data.unparsed.toLocaleString()} unparsed URL${data.unparsed === 1 ? '' : 's'}`
              : null}
          </p>

          <div className="table-wrap host-tree">
            <div className="host-tree-header">
              <span>Label</span>
              <span className="host-tree-count">Channels</span>
            </div>
            {data.tree.length === 0 ? (
              <p className="meta">
                No parseable stream URLs yet — wait for a provider refresh.
              </p>
            ) : (
              <HostTreeRows
                nodes={data.tree}
                path=""
                expanded={expanded}
                toggle={toggle}
              />
            )}
          </div>
        </>
      )}
    </>
  )
}
