import { NavLink, Route, Routes } from 'react-router-dom'
import { ChannelDetailPage } from './pages/ChannelDetail'
import { ChannelsPage } from './pages/Channels'
import { CachePage } from './pages/Cache'
import { ConfigPage } from './pages/Config'
import { ConfigProviderPage } from './pages/ConfigProvider'
import { AccessPage } from './pages/Access'
import { ProxyPage } from './pages/Proxy'
import { CategoriesPage } from './pages/Categories'
import { DedupesPage } from './pages/Dedupes'
import { GroupsPage } from './pages/Groups'
import { GuidePage } from './pages/Guide'
import { HostsPage } from './pages/Hosts'
import { ProvidersPage } from './pages/Providers'
import { ProviderDetailPage } from './pages/ProviderDetail'
import { StatusPage } from './pages/Status'
import './App.css'

export default function App() {
  return (
    <div className="shell">
      <header className="top">
        <NavLink to="/" className="brand" end>
          GoFAST
        </NavLink>
        <nav className="nav" aria-label="Primary">
          <NavLink to="/status">Status</NavLink>
          <NavLink to="/guide">Guide</NavLink>
          <NavLink to="/providers">Providers</NavLink>
          <NavLink to="/" end>
            Channels
          </NavLink>
          <NavLink to="/groups">Groups</NavLink>
          <NavLink to="/categories">Categories</NavLink>
          <NavLink to="/hosts">Hosts</NavLink>
          <NavLink to="/dedupes">Dedupe</NavLink>
          <NavLink to="/access">Access</NavLink>
          <NavLink to="/cache">Cache</NavLink>
          <NavLink to="/proxy">Proxy</NavLink>
          <NavLink to="/config">Config</NavLink>
        </nav>
      </header>
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
          <Route path="/cache" element={<CachePage />} />
          <Route path="/access" element={<AccessPage />} />
          <Route path="/proxy" element={<ProxyPage />} />
          <Route path="/providers" element={<ProvidersPage />} />
          <Route path="/providers/:id" element={<ProviderDetailPage />} />
          <Route path="/groups" element={<GroupsPage />} />
          <Route path="/categories" element={<CategoriesPage />} />
          <Route path="/dedupes" element={<DedupesPage />} />
          <Route path="/config" element={<ConfigPage />} />
          <Route path="/config/providers/:id" element={<ConfigProviderPage />} />
        </Routes>
      </main>
    </div>
  )
}
