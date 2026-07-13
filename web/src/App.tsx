import { NavLink, Route, Routes } from 'react-router-dom'
import { ChannelsPage } from './pages/Channels'
import { ConfigPage } from './pages/Config'
import { ProvidersPage } from './pages/Providers'
import './App.css'

export default function App() {
  return (
    <div className="shell">
      <header className="top">
        <NavLink to="/" className="brand" end>
          GoFAST
        </NavLink>
        <nav className="nav" aria-label="Primary">
          <NavLink to="/" end>
            Channels
          </NavLink>
          <NavLink to="/providers">Providers</NavLink>
          <NavLink to="/config">Config</NavLink>
        </nav>
      </header>
      <main className="main">
        <Routes>
          <Route path="/" element={<ChannelsPage />} />
          <Route path="/channels" element={<ChannelsPage />} />
          <Route path="/providers" element={<ProvidersPage />} />
          <Route path="/config" element={<ConfigPage />} />
        </Routes>
      </main>
    </div>
  )
}
