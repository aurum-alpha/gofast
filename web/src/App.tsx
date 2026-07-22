import { NavLink, Route, Routes } from 'react-router-dom'
import { ChannelDetailPage } from './pages/ChannelDetail'
import { ChannelsPage } from './pages/Channels'
import { ConfigPage } from './pages/Config'
import { GuidePage } from './pages/Guide'
import { ProvidersPage } from './pages/Providers'
import { ProviderDetailPage } from './pages/ProviderDetail'
import './App.css'

export default function App() {
  return (
    <div className="shell">
      <header className="top">
        <NavLink to="/" className="brand" end>
          GoFAST
        </NavLink>
        <nav className="nav" aria-label="Primary">
          <NavLink to="/providers">Providers</NavLink>
          <NavLink to="/" end>
            Channels
          </NavLink>
          <NavLink to="/guide">Guide</NavLink>
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
          <Route path="/providers" element={<ProvidersPage />} />
          <Route path="/providers/:id" element={<ProviderDetailPage />} />
          <Route path="/config" element={<ConfigPage />} />
        </Routes>
      </main>
    </div>
  )
}
