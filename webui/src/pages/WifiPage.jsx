import { useState, useEffect, useCallback } from 'react'

function describeError(err) {
  if (!err) return 'Unknown error'
  const msg = err.message || String(err)
  if (msg.includes('Failed to fetch') || msg.includes('NetworkError') || msg.includes('ERR_NETWORK')) {
    return 'Device unreachable — is it powered on and connected to the network?'
  }
  if (msg.includes('ERR_CONNECTION_REFUSED')) {
    return 'Connection refused — the web server may not be running'
  }
  if (msg.includes('timeout')) {
    return 'Request timed out — the device may be unreachable'
  }
  return msg
}
import { useDevice } from '../DeviceContext.jsx'
import { useToast } from '../components/Toast.jsx'
import {
  getWifiStatus,
  getWifiNetworks,
  scanWifi,
  connectWifi,
  disconnectWifi,
  reorderWifi,
} from '../api.js'
import { Card, CardHeader, CardTitle } from '../components/Card.jsx'
import { StatusBadge } from '../components/StatusBadge.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { PageLoader } from '../components/LoadingSpinner.jsx'

function signalIcon(strength) {
  if (strength >= 70) return 'signal-strong'
  if (strength >= 40) return 'signal-medium'
  return 'signal-weak'
}

function signalLabel(strength) {
  if (strength >= 70) return 'Strong'
  if (strength >= 40) return 'Medium'
  return 'Weak'
}

function signalVariant(strength) {
  if (strength >= 70) return 'success'
  if (strength >= 40) return 'warning'
  return 'danger'
}

function WifiStatusCard({ status }) {
  if (!status) return null

  return (
    <Card>
      <CardHeader>
        <CardTitle>Connection</CardTitle>
        {status.connected ? (
          <StatusBadge variant="success" pulse>Connected</StatusBadge>
        ) : (
          <StatusBadge variant="neutral">Disconnected</StatusBadge>
        )}
      </CardHeader>

      {status.connected ? (
        <div className="space-y-3">
          <div className="flex items-center gap-3">
            <div className="flex items-center justify-center w-10 h-10 rounded-lg bg-success/15">
              <Icon name="wifi" className="w-5 h-5 text-success" />
            </div>
            <div className="flex-1 min-w-0">
              <p className="text-sm font-medium text-surface-50 truncate">{status.ssid}</p>
              <p className="text-xs text-surface-400">
                {status.frequency || '2.4 GHz'}
                {status.ip_address ? ` · ${status.ip_address}` : ''}
              </p>
            </div>
            {status.signal != null && (
              <StatusBadge variant={signalVariant(status.signal)}>
                <Icon name={signalIcon(status.signal)} className="w-3 h-3" />
                {signalLabel(status.signal)}
              </StatusBadge>
            )}
          </div>
        </div>
      ) : (
        <p className="text-sm text-surface-400">No active WiFi connection. Scan for available networks below.</p>
      )}
    </Card>
  )
}

function PasswordInput({ value, onChange, placeholder }) {
  const [visible, setVisible] = useState(false)

  return (
    <div className="relative">
      <input
        type={visible ? 'text' : 'password'}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder || 'Enter password'}
        className="w-full bg-surface-900/50 border border-surface-600/50 rounded-lg px-3 py-2.5 pr-10 text-sm text-surface-100 placeholder:text-surface-500 focus:outline-none focus:ring-2 focus:ring-brand-500/50 focus:border-brand-500/50"
      />
      <button
        type="button"
        onClick={() => setVisible((v) => !v)}
        className="absolute right-2 top-1/2 -translate-y-1/2 p-1 text-surface-400 hover:text-surface-200 transition-colors"
        tabIndex={-1}
      >
        <Icon name={visible ? 'x' : 'check'} className="w-4 h-4" />
      </button>
    </div>
  )
}

function ScannedNetworkItem({ network, currentSsid, onConnect }) {
  const [expanded, setExpanded] = useState(false)
  const [password, setPassword] = useState('')
  const [connecting, setConnecting] = useState(false)
  const isCurrent = currentSsid === network.ssid

  async function handleConnect() {
    if (network.encrypted && !expanded) {
      setExpanded(true)
      return
    }
    setConnecting(true)
    try {
      await onConnect(network.ssid, password)
    } finally {
      setConnecting(false)
    }
  }

  return (
    <div className="border border-surface-700/30 rounded-lg overflow-hidden">
      <div className="flex items-center gap-3 p-3">
        <Icon
          name={signalIcon(network.signal)}
          className={`w-5 h-5 text-${signalVariant(network.signal)}`}
        />
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-surface-100 truncate">{network.ssid}</span>
            {network.encrypted && <Icon name="lock" className="w-3.5 h-3.5 text-surface-400 shrink-0" />}
            {isCurrent && (
              <StatusBadge variant="success">
                <Icon name="check" className="w-3 h-3" />
                Connected
              </StatusBadge>
            )}
          </div>
          <span className="text-xs text-surface-400">
            {network.frequency || ''}
          </span>
        </div>
        <Button
          variant={isCurrent ? 'danger' : 'primary'}
          size="sm"
          loading={connecting}
          onClick={handleConnect}
        >
          {isCurrent ? 'Disconnect' : 'Connect'}
        </Button>
      </div>

      {expanded && !isCurrent && network.encrypted && (
        <div className="px-3 pb-3 space-y-2">
          <PasswordInput
            value={password}
            onChange={setPassword}
            placeholder="WiFi password"
          />
          <Button
            variant="primary"
            size="sm"
            className="w-full"
            loading={connecting}
            disabled={!password}
            onClick={handleConnect}
          >
            Connect
          </Button>
        </div>
      )}
    </div>
  )
}

function SavedNetworkItem({ network, index, total, onMoveUp, onMoveDown, onDisconnect, isConnected }) {
  return (
    <div className="flex items-center gap-3 p-3 bg-surface-900/30 rounded-lg">
      <div className="flex flex-col gap-0.5">
        <button
          onClick={() => onMoveUp(index)}
          disabled={index === 0}
          className="p-0.5 text-surface-400 hover:text-surface-200 disabled:opacity-25 disabled:cursor-not-allowed transition-colors"
          title="Move up"
        >
          <Icon name="chevron-left" className="w-3.5 h-3.5 rotate-[-90deg]" />
        </button>
        <button
          onClick={() => onMoveDown(index)}
          disabled={index === total - 1}
          className="p-0.5 text-surface-400 hover:text-surface-200 disabled:opacity-25 disabled:cursor-not-allowed transition-colors"
          title="Move down"
        >
          <Icon name="chevron-left" className="w-3.5 h-3.5 rotate-[90deg]" />
        </button>
      </div>

      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-surface-100 truncate">{network.ssid}</span>
          {network.has_password && <Icon name="lock" className="w-3.5 h-3.5 text-surface-400 shrink-0" />}
          {isConnected && (
            <StatusBadge variant="success">
              <Icon name="check" className="w-3 h-3" />
              Active
            </StatusBadge>
          )}
        </div>
        <span className="text-xs text-surface-400">Priority {index + 1}</span>
      </div>

      <Button
        variant="ghost"
        size="sm"
        onClick={() => onDisconnect(network.ssid)}
      >
        <Icon name="x" className="w-4 h-4" />
      </Button>
    </div>
  )
}

export default function WifiPage() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [loading, setLoading] = useState(true)
  const [status, setStatus] = useState(null)
  const [savedNetworks, setSavedNetworks] = useState([])
  const [scannedNetworks, setScannedNetworks] = useState([])
  const [scanning, setScanning] = useState(false)
  const [showSaved, setShowSaved] = useState(true)
  const [showScanned, setShowScanned] = useState(false)

  const fetchStatus = useCallback(async () => {
    try {
      const data = await getWifiStatus(deviceUrl)
      setStatus(data)
    } catch (err) {
      toast.error(`Could not load WiFi status: ${describeError(err)}`)
    }
  }, [deviceUrl, toast])

  const fetchSavedNetworks = useCallback(async () => {
    try {
      const data = await getWifiNetworks(deviceUrl)
      setSavedNetworks(data?.networks ?? [])
    } catch (err) {
      toast.error(`Could not load saved networks: ${describeError(err)}`)
    }
  }, [deviceUrl, toast])

  useEffect(() => {
    if (!deviceUrl) return
    setLoading(true)
    Promise.all([fetchStatus(), fetchSavedNetworks()])
      .finally(() => setLoading(false))
  }, [deviceUrl, fetchStatus, fetchSavedNetworks])

  async function handleScan() {
    setScanning(true)
    try {
      const data = await scanWifi(deviceUrl, 'signal_strength')
      setScannedNetworks(data?.networks ?? [])
      setShowScanned(true)
      toast.info(`Found ${(data?.networks ?? []).length} networks`)
    } catch (err) {
      toast.error(`Scan failed: ${describeError(err)}`)
    } finally {
      setScanning(false)
    }
  }

  async function handleConnect(ssid, password) {
    try {
      await connectWifi(deviceUrl, ssid, password)
      toast.success(`Connected to ${ssid}`)
      await fetchStatus()
      await fetchSavedNetworks()
    } catch (err) {
      toast.error(`Connection failed: ${describeError(err)}`)
    }
  }

  async function handleDisconnect(ssid) {
    try {
      await disconnectWifi(deviceUrl, ssid)
      toast.success(`Disconnected from ${ssid}`)
      await fetchStatus()
      await fetchSavedNetworks()
    } catch (err) {
      toast.error(`Disconnect failed: ${describeError(err)}`)
    }
  }

  async function handleReorder(newNetworks) {
    setSavedNetworks(newNetworks)
    try {
      const ssids = newNetworks.map((n) => n.ssid)
      await reorderWifi(deviceUrl, ssids)
    } catch (err) {
      toast.error(`Reorder failed: ${describeError(err)}`)
      await fetchSavedNetworks()
    }
  }

  function handleMoveUp(index) {
    if (index === 0) return
    const next = [...savedNetworks]
    ;[next[index - 1], next[index]] = [next[index], next[index - 1]]
    handleReorder(next)
  }

  function handleMoveDown(index) {
    if (index >= savedNetworks.length - 1) return
    const next = [...savedNetworks]
    ;[next[index], next[index + 1]] = [next[index + 1], next[index]]
    handleReorder(next)
  }

  if (loading) return <PageLoader />

  return (
    <div className="space-y-4">
      {/* WiFi Status */}
      <WifiStatusCard status={status} />

      {/* Scan Button */}
      <Card>
        <div className="flex items-center justify-between">
          <div>
            <h3 className="text-sm font-semibold text-surface-200">Network Scan</h3>
            <p className="text-xs text-surface-400 mt-0.5">Search for nearby WiFi networks</p>
          </div>
          <Button
            variant="secondary"
            size="sm"
            loading={scanning}
            onClick={handleScan}
          >
            <Icon name="arrow-path" className="w-4 h-4" />
            {scanning ? 'Scanning...' : 'Scan'}
          </Button>
        </div>
      </Card>

      {/* Scanned Networks */}
      {showScanned && scannedNetworks.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Available Networks</CardTitle>
            <StatusBadge variant="info">{scannedNetworks.length} found</StatusBadge>
          </CardHeader>
          <div className="space-y-2">
            {scannedNetworks.map((network) => (
              <ScannedNetworkItem
                key={network.ssid}
                network={network}
                currentSsid={status?.connected ? status.ssid : null}
                onConnect={handleConnect}
              />
            ))}
          </div>
        </Card>
      )}

      {/* Saved Networks */}
      <Card>
        <CardHeader>
          <CardTitle>Saved Networks</CardTitle>
          <button
            onClick={() => setShowSaved((v) => !v)}
            className="text-surface-400 hover:text-surface-200 transition-colors p-1"
          >
            <Icon
              name="chevron-right"
              className={`w-4 h-4 transition-transform ${showSaved ? 'rotate-90' : ''}`}
            />
          </button>
        </CardHeader>

        {showSaved && (
          <>
            {savedNetworks.length === 0 ? (
              <p className="text-sm text-surface-400 py-2">No saved networks. Scan to find available networks.</p>
            ) : (
              <div className="space-y-2">
                {savedNetworks.map((network, idx) => (
                  <SavedNetworkItem
                    key={network.ssid}
                    network={network}
                    index={idx}
                    total={savedNetworks.length}
                    onMoveUp={handleMoveUp}
                    onMoveDown={handleMoveDown}
                    onDisconnect={handleDisconnect}
                    isConnected={status?.connected && status.ssid === network.ssid}
                  />
                ))}
              </div>
            )}
          </>
        )}
      </Card>
    </div>
  )
}
