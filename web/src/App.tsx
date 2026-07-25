import { useEffect, useState } from 'react'
import { NavLink, Route, Routes } from 'react-router-dom'
import { ChannelDetailPage } from './pages/ChannelDetail'
import { ChannelsPage } from './pages/Channels'
import { ConfigPage } from './pages/Config'
import { ConfigProviderPage } from './pages/ConfigProvider'
import { AccessPage } from './pages/Access'
import { ProxyPage } from './pages/Proxy'
import { CategoriesPage } from './pages/Categories'
import { GroupsPage } from './pages/Groups'
import { GuidePage } from './pages/Guide'
import { HostsPage } from './pages/Hosts'
import { ProvidersPage } from './pages/Providers'
import { ProviderDetailPage } from './pages/ProviderDetail'
import { StatusPage } from './pages/Status'
import './App.css'

type StatusResponse = {
  ready: boolean
  logos: {
    running: boolean
    done: number
    total: number
    provider?: string
  }
}

function LogoProgressBanner() {
  const [status, setStatus] = useState<StatusResponse | null>(null)

  useEffect(() => {
    let cancelled = false
    let timer: number | undefined

    const poll = () => {
      fetch('/api/status')
        .then(async (res) => {
          if (!res.ok) throw new Error(String(res.status))
          return res.json() as Promise<StatusResponse>
        })
        .then((body) => {
          if (cancelled) return
          setStatus(body)
          const delay = body.logos.running ? 1000 : 5000
          timer = window.setTimeout(poll, delay)
        })
        .catch(() => {
          if (!cancelled) {
            timer = window.setTimeout(poll, 5000)
          }
        })
    }
    poll()
    return () => {
      cancelled = true
      if (timer !== undefined) window.clearTimeout(timer)
    }
  }, [])

  if (!status?.logos.running) return null
  const { done, total, provider } = status.logos
  const label =
    total > 0
      ? `Caching logos ${done}/${total}${provider ? ` · ${provider}` : ''}`
      : 'Caching logos…'

  return (
    <div className="boot-banner" role="status">
      {label}
    </div>
  )
}

export default function App() {
  return (
    <div className="shell">
      <header className="top">
        <NavLink to="/" className="brand" end>
          GoFAST
        </NavLink>
        <nav className="nav" aria-label="Primary">
          <NavLink to="/status">Status</NavLink>
          <NavLink to="/access">Access</NavLink>
          <NavLink to="/proxy">Proxy</NavLink>
          <NavLink to="/providers">Providers</NavLink>
          <NavLink to="/" end>
            Channels
          </NavLink>
          <NavLink to="/hosts">Hosts</NavLink>
          <NavLink to="/guide">Guide</NavLink>
          <NavLink to="/groups">Groups</NavLink>
          <NavLink to="/categories">Categories</NavLink>
          <NavLink to="/config">Config</NavLink>
        </nav>
      </header>
      <LogoProgressBanner />
      <main className="main">
        <Routes>
          <Route path="/" element={<ChannelsPage />} />
          <Route path="/channels" element={<ChannelsPage />} />
          <Route
            path="/channels/:provider/:normalizedId"
            element={<ChannelDetailPage />}
          />
          <Route path="/guide" element={<GuidePage />} />
          <Route path="/hosts" element={<HostsPage />} />
          <Route path="/status" element={<StatusPage />} />
          <Route path="/access" element={<AccessPage />} />
          <Route path="/proxy" element={<ProxyPage />} />
          <Route path="/providers" element={<ProvidersPage />} />
          <Route path="/providers/:id" element={<ProviderDetailPage />} />
          <Route path="/groups" element={<GroupsPage />} />
          <Route path="/categories" element={<CategoriesPage />} />
          <Route path="/config" element={<ConfigPage />} />
          <Route path="/config/providers/:id" element={<ConfigProviderPage />} />
        </Routes>
      </main>
    </div>
  )
}
