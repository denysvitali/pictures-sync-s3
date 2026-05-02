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

  const clearDeviceUrl = () => {
    localStorage.removeItem(STORAGE_KEY)
    setDeviceUrlState('')
  }

  return [deviceUrl, setDeviceUrl, clearDeviceUrl]
}

function toApiUrl(base, path) {
  const trimmed = String(base || '').trim().replace(/\/$/, '')
  if (!trimmed) return ''
  return `${trimmed}${path}`
}

function toJson(value) {
  try {
    return JSON.stringify(value, null, 2)
  } catch {
    return String(value)
  }
}

async function jsonOrText(response) {
  const text = await response.text()
  if (!response.ok) {
    throw new Error(text || `Request failed (${response.status})`)
  }
  const contentType = response.headers.get('content-type') || ''
  if (contentType.includes('application/json')) {
    return JSON.parse(text)
  }
  return text
}

function StatusAlert({ deviceUrl }) {
  if (deviceUrl) {
    return null
  }

  return (
    <div className="alert alert-warning d-flex gap-2 align-items-start" role="alert">
      <i className="fa-solid fa-circle-info mt-1"></i>
      <div>
        <strong>Set the device URL first.</strong>
        <p className="mb-0">
          You are likely viewing this on GitHub Pages.
          Enter your device address (for example http://192.168.1.10:8080) to load live data.
        </p>
      </div>
    </div>
  )
}

function DeviceSwitcher({ value, onChange, onClear }) {
  const [raw, setRaw] = useState(value)

  useEffect(() => {
    setRaw(value)
  }, [value])

  const save = (event) => {
    event.preventDefault()
    onChange(raw)
  }

  const useCurrentOrigin = () => {
    onChange(window.location.origin)
  }

  const quickExamples = ['http://192.168.1.10:8080', 'http://localhost:8080']

  return (
    <section className="card shadow-sm border-0">
      <div className="card-header bg-transparent border-0">
        <h2 className="h5 mb-0">Device endpoint</h2>
      </div>
      <div className="card-body pt-0">
        <form onSubmit={save} className="row g-2 align-items-end">
          <div className="col-md-8">
            <label htmlFor="device-input" className="form-label">Base URL</label>
            <input
              id="device-input"
              className="form-control"
              value={raw}
              onChange={(event) => setRaw(event.target.value)}
              placeholder="http://192.168.1.10:8080"
              spellCheck="false"
            />
          </div>
          <div className="col-md-4 d-flex gap-2">
            <button type="submit" className="btn btn-primary">Save</button>
            <button type="button" className="btn btn-outline-secondary" onClick={onClear}>Clear</button>
            <button type="button" className="btn btn-outline-primary" onClick={useCurrentOrigin}>Use this host</button>
          </div>
        </form>
        <p className="text-muted small mt-3 mb-0">
          Active target:
          <code className="ms-2 user-select-all">{value || '(not set)'}</code>
        </p>
        <div className="d-flex flex-wrap gap-2 mt-3">
          {quickExamples.map((item) => (
            <button
              key={item}
              type="button"
              className="btn btn-sm btn-outline-light"
              onClick={() => onChange(item)}
            >
              {item}
            </button>
          ))}
        </div>
      </div>
    </section>
  )
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

  const syncedCount = Array.isArray(history) ? history.length : 0

  return (
    <section className="panel-section">
      <StatusAlert deviceUrl={deviceUrl} />
      <div className="card shadow-sm border-0">
        <div className="card-header bg-transparent border-0 d-flex justify-content-between align-items-center">
          <h2 className="h5 mb-0">Status</h2>
          <button className="btn btn-sm btn-outline-light" onClick={load} disabled={loading || !deviceUrl}>
            {loading ? <span className="spinner-border spinner-border-sm me-2" role="status" aria-hidden="true"></span> : null}
            Refresh
          </button>
        </div>
        <div className="card-body pt-0">
          {error ? <div className="alert alert-danger">{error}</div> : null}
          {!deviceUrl ? <p className="text-muted">No device configured.</p> : null}
          <div className="row g-3">
            <div className="col-md-6">
              <div className="border rounded p-3 h-100">
                <h3 className="h6 text-uppercase text-primary">Current status</h3>
                <pre className="api-json">{toJson(state || {})}</pre>
              </div>
            </div>
            <div className="col-md-6">
              <div className="border rounded p-3 h-100">
                <h3 className="h6 text-uppercase text-primary">Recent history</h3>
                <p className="mb-2 small text-muted">Items: <strong>{syncedCount}</strong></p>
                <pre className="api-json">{toJson(history)}</pre>
              </div>
            </div>
          </div>
        </div>
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
    <section className="panel-section">
      <StatusAlert deviceUrl={deviceUrl} />
      <div className="card shadow-sm border-0">
        <div className="card-header bg-transparent border-0 d-flex justify-content-between align-items-center">
          <h2 className="h5 mb-0">Wi-Fi</h2>
          <button className="btn btn-sm btn-outline-light" onClick={load} disabled={loading || !deviceUrl}>
            {loading ? <span className="spinner-border spinner-border-sm me-2" role="status" aria-hidden="true"></span> : null}
            Refresh
          </button>
        </div>
        <div className="card-body pt-0">
          {error ? <div className="alert alert-danger">{error}</div> : null}
          <div className="row g-3">
            <div className="col-md-6">
              <div className="border rounded p-3 h-100">
                <h3 className="h6 text-uppercase text-primary">Wi-Fi status</h3>
                <pre className="api-json">{toJson(status || {})}</pre>
              </div>
            </div>
            <div className="col-md-6">
              <div className="border rounded p-3 h-100">
                <h3 className="h6 text-uppercase text-primary">Saved networks</h3>
                {networks.length === 0 ? (
                  <p className="text-muted small">No networks available.</p>
                ) : null}
                <ul className="list-group list-group-flush api-list">
                  {networks.map((item, index) => (
                    <li className="list-group-item bg-transparent" key={item.ssid || item.SSID || item.id || item.networkId || index}>
                      {item.ssid || item.SSID || JSON.stringify(item)}
                    </li>
                  ))}
                </ul>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  )
}

function PlaceholderPanel({ title }) {
  return (
    <section className="panel-section">
      <div className="card shadow-sm border-0">
        <div className="card-header bg-transparent border-0">
          <h2 className="h5 mb-0">{title}</h2>
        </div>
        <div className="card-body pt-0">
          <p className="text-muted">This screen will be available when backend handlers are added.</p>
        </div>
      </div>
    </section>
  )
}

function App() {
  const [deviceUrl, setDeviceUrl, clearDeviceUrl] = useDeviceUrl()
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
      <header className="hero-card">
        <h1>Photo Backup Station</h1>
        <p className="mb-0 text-muted">Management panel for your on-device sync service.</p>
      </header>

      <DeviceSwitcher value={deviceUrl} onChange={setDeviceUrl} onClear={clearDeviceUrl} />

      <ul className="nav nav-pills mb-3" role="tablist">
        {TABS.map((item) => (
          <li className="nav-item" key={item}>
            <button
              type="button"
              className={`nav-link ${item === activeTab ? 'active' : ''}`}
              onClick={() => navigate(item)}
            >
              {item}
            </button>
          </li>
        ))}
      </ul>

      {activeTab === 'status' && <ApiStatusPanel deviceUrl={deviceUrl} />}
      {activeTab === 'wifi' && <ApiWifiPanel deviceUrl={deviceUrl} />}
      {activeTab === 'history' && <PlaceholderPanel title="History" />}
      {activeTab === 'gallery' && <PlaceholderPanel title="Gallery" />}
      {activeTab === 'config' && <PlaceholderPanel title="Config" />}
    </div>
  )
}

export default App
