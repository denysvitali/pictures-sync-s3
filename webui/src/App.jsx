import { useEffect, useMemo, useState } from 'react'

const TABS = ['status', 'wifi', 'history', 'gallery', 'config']
const STORAGE_KEY = 'photo-backup-device-url'

const defaultDeviceUrl = () => {
  const saved = localStorage.getItem(STORAGE_KEY)
  if (saved && !isHostedPagesUrl(saved)) {
    return saved
  }

  const host = window.location.hostname.toLowerCase()
  if (host.endsWith('.github.io') || host.endsWith('.github.com')) {
    return ''
  }

  return `${window.location.origin}`
}

function isHostedPagesUrl(raw) {
  try {
    const url = new URL(raw)
    const host = url.hostname.toLowerCase()
    return host.endsWith('.github.io') || host.endsWith('.github.com')
  } catch {
    return false
  }
}

function useDeviceUrl() {
  const [deviceUrl, setDeviceUrlState] = useState(defaultDeviceUrl)

  const setDeviceUrl = (value) => {
    const next = String(value || '').trim()
    localStorage.setItem(STORAGE_KEY, next)
    setDeviceUrlState(next)
  }

  return [deviceUrl, setDeviceUrl]
}

function toApiUrl(base, path) {
  const trimmed = String(base || '').trim().replace(/\/$/, '')
  if (!trimmed) return ''
  return `${trimmed}${path}`
}

function JsonBlock({ value }) {
  return <pre>{JSON.stringify(value, null, 2)}</pre>
}

function PlaceholderPanel({ title }) {
  return (
    <section className="card">
      <h2>{title}</h2>
      <p>This section is reachable through the on-device API and will be filled as the backend adds data.</p>
    </section>
  )
}

function DeviceSwitcher({ value, onChange }) {
  const [raw, setRaw] = useState(value)

  const save = (event) => {
    event.preventDefault()
    onChange(raw)
  }

  return (
    <form className="control-card" onSubmit={save}>
      <label htmlFor="device-input">Device base URL</label>
      <div className="row">
        <input
          id="device-input"
          value={raw}
          onChange={(event) => setRaw(event.target.value)}
          placeholder="http://192.168.1.10:8080"
          spellCheck="false"
        />
        <button type="submit">Use device</button>
      </div>
    </form>
  )
}

async function jsonOrText(response) {
  const text = await response.text()
  if (!response.ok) {
    throw new Error(text || `Request failed (${response.status})`)
  }
  if (response.headers.get('content-type')?.includes('application/json')) {
    return JSON.parse(text)
  }
  return text
}

function ApiStatusPanel({ deviceUrl }) {
  const [state, setState] = useState(null)
  const [history, setHistory] = useState([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const load = async () => {
    if (!deviceUrl) return
    setLoading(true)
    setError('')
    try {
      const [statusResponse, historyResponse] = await Promise.all([
        fetch(toApiUrl(deviceUrl, '/api/status'), { credentials: 'include' }).then(jsonOrText),
        fetch(toApiUrl(deviceUrl, '/api/history'), { credentials: 'include' }).then(jsonOrText)
      ])
      setState(statusResponse)
      setHistory(Array.isArray(historyResponse?.history) ? historyResponse.history : [])
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [deviceUrl])

  return (
    <section className="card">
      <h2>Status</h2>
      {loading ? <p className="muted">Loading status...</p> : null}
      {error ? <p className="error">{error}</p> : null}
      {!deviceUrl ? (
        <p className="error">Set your device base URL (for example <code>http://192.168.1.10:8080</code>) to load live status.</p>
      ) : null}
      <div className="row two-cols">
        <div className="stat">
          <h3>Current status</h3>
          <JsonBlock value={state || {}} />
        </div>
        <div className="stat">
          <h3>Recent history</h3>
          <p>Items: {Array.isArray(history) ? history.length : 0}</p>
          <JsonBlock value={history} />
        </div>
      </div>
      <div className="actions">
        <button onClick={load}>Refresh</button>
      </div>
    </section>
  )
}

function ApiWifiPanel({ deviceUrl }) {
  const [status, setStatus] = useState(null)
  const [networks, setNetworks] = useState([])
  const [error, setError] = useState('')
  const [loading, setLoading] = useState(false)

  const load = async () => {
    if (!deviceUrl) return
    setLoading(true)
    setError('')
    try {
      const [statusResponse, networksResponse] = await Promise.all([
        fetch(toApiUrl(deviceUrl, '/api/wifi/status'), { credentials: 'include' }).then(jsonOrText),
        fetch(toApiUrl(deviceUrl, '/api/wifi/networks'), { credentials: 'include' }).then(jsonOrText)
      ])
      setStatus(statusResponse)
      setNetworks(Array.isArray(networksResponse?.networks) ? networksResponse.networks : [])
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => {
    load()
  }, [deviceUrl])

  return (
    <section className="card">
      <h2>Wi-Fi</h2>
      {loading ? <p className="muted">Loading Wi-Fi info...</p> : null}
      {!deviceUrl ? <p className="error">Set your device base URL to load Wi-Fi data.</p> : null}
      {error ? <p className="error">{error}</p> : null}
      <div className="row two-cols">
        <div className="stat">
          <h3>Status</h3>
          <JsonBlock value={status || {}} />
        </div>
        <div className="stat">
          <h3>Saved networks</h3>
          <JsonBlock value={networks} />
        </div>
      </div>
      <div className="actions">
        <button onClick={load}>Refresh</button>
      </div>
    </section>
  )
}

function App() {
  const [deviceUrl, setDeviceUrl] = useDeviceUrl()
  const [tab, setTab] = useState('status')

  useEffect(() => {
    const onHash = () => {
      const candidate = window.location.hash.replace(/^#\//, '')
      if (TABS.includes(candidate)) {
        setTab(candidate)
      }
    }
    onHash()
    window.addEventListener('hashchange', onHash)
    return () => window.removeEventListener('hashchange', onHash)
  }, [])

  const navigate = (next) => {
    setTab(next)
    window.location.hash = `#/${next}`
  }

  const activeTab = useMemo(() => (TABS.includes(tab) ? tab : 'status'), [tab])

  return (
    <div className="app-shell">
      <header className="app-header">
        <div>
          <h1>Photo Backup Station</h1>
          <p className="muted">React frontend built from source in webui/</p>
        </div>
      </header>

      <section className="control-card">
        <p className="muted">If you are opening this page from GitHub Pages, the Device base URL must point to your device.</p>
        <DeviceSwitcher value={deviceUrl} onChange={setDeviceUrl} />
      </section>

      <nav className="tabs" aria-label="Main navigation">
        {TABS.map((item) => (
          <button
            key={item}
            type="button"
            className={item === activeTab ? 'active' : ''}
            onClick={() => navigate(item)}
          >
            {item.toUpperCase()}
          </button>
        ))}
      </nav>

      {activeTab === 'status' && <ApiStatusPanel deviceUrl={deviceUrl} />}
      {activeTab === 'wifi' && <ApiWifiPanel deviceUrl={deviceUrl} />}
      {activeTab === 'history' && <PlaceholderPanel title="History" />}
      {activeTab === 'gallery' && <PlaceholderPanel title="Gallery" />}
      {activeTab === 'config' && <PlaceholderPanel title="Config" />}
    </div>
  )
}

export default App
