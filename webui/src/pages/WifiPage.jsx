import { useState, useEffect, useCallback, useId } from 'react'
import { AnimatePresence, motion, useReducedMotion } from 'framer-motion'

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

function signalLabel(strength) {
  if (strength >= -50) return 'Strong'
  if (strength >= -70) return 'Medium'
  return 'Weak'
}

function signalVariant(strength) {
  if (strength >= -50) return 'success'
  if (strength >= -70) return 'warning'
  return 'danger'
}

function signalClass(strength) {
  const variant = signalVariant(strength)
  if (variant === 'success') return 'text-success'
  if (variant === 'warning') return 'text-warning'
  return 'text-danger'
}

// A compact, layered "signal bars" indicator that reads at a glance and
// gracefully degrades for screen readers via an aria-label.
function SignalStrength({ strength }) {
  const variant = signalVariant(strength)
  const barColor = variant === 'success' ? 'bg-success' : variant === 'warning' ? 'bg-warning' : 'bg-danger'
  const filled = variant === 'success' ? 3 : variant === 'warning' ? 2 : 1
  const heights = ['h-1.5', 'h-2.5', 'h-3.5']

  return (
    <span
      className="inline-flex items-end gap-0.5"
      role="img"
      aria-label={`Signal ${signalLabel(strength)}${strength != null ? `, ${strength} dBm` : ''}`}
    >
      {heights.map((h, i) => (
        <span
          key={i}
          className={`w-1 rounded-full ${h} ${i < filled ? barColor : 'bg-surface-600/70'}`}
        />
      ))}
    </span>
  )
}

function WifiStatusCard({ status }) {
  if (!status) return null

  return (
    <Card glow>
      <CardHeader>
        <CardTitle>Connection</CardTitle>
        {status.connected ? (
          <StatusBadge variant="success" pulse>Connected</StatusBadge>
        ) : (
          <StatusBadge variant="neutral">Disconnected</StatusBadge>
        )}
      </CardHeader>

      {status.connected ? (
        <div className="flex items-center gap-3">
          <div className="relative flex items-center justify-center w-12 h-12 rounded-xl bg-success/15 ring-1 ring-success/25 shrink-0">
            <Icon name="wifi" className="w-6 h-6 text-success" />
          </div>
          <div className="flex-1 min-w-0">
            <p className="text-sm font-semibold text-surface-50 truncate">{status.ssid}</p>
            <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-surface-400">
              <span className="inline-flex items-center gap-1">
                <Icon name="wifi" className="w-3.5 h-3.5 text-surface-500" aria-hidden="true" />
                {status.frequency || '2.4 GHz'}
              </span>
              {status.ip_address && (
                <span className="inline-flex items-center gap-1">
                  <span className="w-1 h-1 rounded-full bg-surface-600" aria-hidden="true" />
                  <span className="font-mono text-surface-300">{status.ip_address}</span>
                </span>
              )}
            </div>
          </div>
          {status.signal != null && (
            <div className="flex flex-col items-end gap-1.5 shrink-0">
              <SignalStrength strength={status.signal} />
              <span className={`text-[10px] font-medium ${signalClass(status.signal)}`}>
                {signalLabel(status.signal)}
              </span>
            </div>
          )}
        </div>
      ) : (
        <div className="flex items-start gap-3 rounded-lg bg-surface-900/40 border border-surface-700/40 p-3">
          <div className="flex items-center justify-center w-9 h-9 rounded-lg bg-surface-700/50 shrink-0">
            <Icon name="wifi" className="w-5 h-5 text-surface-400" />
          </div>
          <p className="text-sm text-surface-400 leading-relaxed">
            No active Wi-Fi connection. Scan for available networks below to get online.
          </p>
        </div>
      )}
    </Card>
  )
}

function PasswordInput({ value, onChange, placeholder, id, describedBy, autoFocus }) {
  const [visible, setVisible] = useState(false)

  return (
    <div className="relative">
      <span className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-surface-500">
        <Icon name="lock" className="w-4 h-4" aria-hidden="true" />
      </span>
      <input
        id={id}
        type={visible ? 'text' : 'password'}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder || 'Enter password'}
        autoFocus={autoFocus}
        autoComplete="off"
        aria-describedby={describedBy}
        className="w-full min-h-11 bg-surface-900/60 border border-surface-600/60 rounded-lg pl-9 pr-11 py-2.5 text-sm text-surface-100 placeholder:text-surface-500 transition-colors focus:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/60 focus:border-brand-500/60"
      />
      <button
        type="button"
        onClick={() => setVisible((v) => !v)}
        className="absolute right-1.5 top-1/2 -translate-y-1/2 flex items-center justify-center w-8 h-8 text-surface-400 hover:text-surface-100 transition-colors rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
        aria-label={visible ? 'Hide password' : 'Show password'}
        aria-pressed={visible}
      >
        <Icon name={visible ? 'x' : 'check'} className="w-4 h-4" aria-hidden="true" />
      </button>
    </div>
  )
}

function ScannedNetworkItem({ network, currentSsid, onConnect, onDisconnect, reduceMotion }) {
  const [expanded, setExpanded] = useState(false)
  const [password, setPassword] = useState('')
  const [connecting, setConnecting] = useState(false)
  const isCurrent = currentSsid === network.ssid
  const pwId = useId()
  const hintId = useId()

  async function handleConnect() {
    if (isCurrent) {
      setConnecting(true)
      try {
        await onDisconnect(network.ssid)
      } finally {
        setConnecting(false)
      }
      return
    }
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
    <div
      className={`rounded-lg border transition-colors ${
        isCurrent
          ? 'border-success/30 bg-success/5'
          : expanded
          ? 'border-brand-500/40 bg-surface-900/40'
          : 'border-surface-700/40 bg-surface-900/20 hover:border-surface-600/60'
      }`}
    >
      <div className="flex flex-wrap items-center gap-3 p-3">
        <span className={`shrink-0 ${signalClass(network.signal)}`}>
          <SignalStrength strength={network.signal} />
        </span>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-surface-100 truncate">{network.ssid}</span>
            {network.encrypted && (
              <Icon name="lock" className="w-3.5 h-3.5 text-surface-500 shrink-0" aria-label="Secured network" />
            )}
            {isCurrent && (
              <StatusBadge variant="success" size="sm">
                <Icon name="check" className="w-3 h-3" aria-hidden="true" />
                Connected
              </StatusBadge>
            )}
          </div>
          <span className="text-xs text-surface-400">
            {[signalLabel(network.signal), network.frequency, network.encrypted ? 'Secured' : 'Open']
              .filter(Boolean)
              .join(' · ')}
          </span>
        </div>
        <Button
          variant={isCurrent ? 'danger' : 'primary'}
          size="sm"
          loading={connecting}
          onClick={handleConnect}
          className="ml-auto"
          aria-expanded={network.encrypted && !isCurrent ? expanded : undefined}
        >
          {isCurrent ? 'Disconnect' : 'Connect'}
        </Button>
      </div>

      <AnimatePresence initial={false}>
        {expanded && !isCurrent && network.encrypted && (
          <motion.div
            initial={reduceMotion ? false : { height: 0, opacity: 0 }}
            animate={{ height: 'auto', opacity: 1 }}
            exit={reduceMotion ? { opacity: 0 } : { height: 0, opacity: 0 }}
            transition={{ duration: 0.2, ease: 'easeOut' }}
            className="overflow-hidden"
          >
            <div className="px-3 pb-3 pt-1 space-y-2 border-t border-surface-700/40">
              <label htmlFor={pwId} className="block text-xs font-medium text-surface-300 pt-2">
                Wi-Fi password
              </label>
              <PasswordInput
                id={pwId}
                value={password}
                onChange={setPassword}
                placeholder={`Password for ${network.ssid}`}
                describedBy={hintId}
                autoFocus
              />
              <p id={hintId} className="text-xs text-surface-500">
                Enter the network password, then connect.
              </p>
              <div className="flex items-center justify-end gap-2 pt-1">
                <Button variant="ghost" size="sm" onClick={() => setExpanded(false)} disabled={connecting}>
                  Cancel
                </Button>
                <Button
                  variant="primary"
                  size="sm"
                  loading={connecting}
                  disabled={!password}
                  onClick={handleConnect}
                >
                  <Icon name="wifi" className="w-4 h-4" aria-hidden="true" />
                  Connect
                </Button>
              </div>
            </div>
          </motion.div>
        )}
      </AnimatePresence>
    </div>
  )
}

function SavedNetworkItem({ network, index, total, onMoveUp, onMoveDown, onDisconnect, isConnected }) {
  const canMoveUp = index > 0
  const canMoveDown = index < total - 1

  function handleRowKeyDown(e) {
    // Only handle Arrow keys when the row itself is focused (not when a button inside is focused).
    if (e.target !== e.currentTarget) return
    if (e.key === 'ArrowUp' && canMoveUp) {
      e.preventDefault()
      onMoveUp(index)
    } else if (e.key === 'ArrowDown' && canMoveDown) {
      e.preventDefault()
      onMoveDown(index)
    }
  }

  return (
    <div
      className={`flex items-center gap-3 p-3 rounded-lg border transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 ${
        isConnected
          ? 'border-success/30 bg-success/5'
          : 'border-surface-700/40 bg-surface-900/30 hover:border-surface-600/60'
      }`}
      role="listitem"
      tabIndex={0}
      aria-label={`${network.ssid}, priority ${index + 1} of ${total}. Use Arrow Up and Arrow Down to reorder.`}
      onKeyDown={handleRowKeyDown}
    >
      <span className="flex items-center justify-center w-6 h-6 rounded-md bg-surface-800 text-[11px] font-semibold text-surface-400 shrink-0 tabular-nums" aria-hidden="true">
        {index + 1}
      </span>

      <div className="flex-1 min-w-0">
        <div className="flex items-center gap-2">
          <span className="text-sm font-medium text-surface-100 truncate">{network.ssid}</span>
          {network.has_password && <Icon name="lock" className="w-3.5 h-3.5 text-surface-500 shrink-0" aria-label="Password protected" />}
          {isConnected && (
            <StatusBadge variant="success" size="sm">
              <Icon name="check" className="w-3 h-3" aria-hidden="true" />
              Active
            </StatusBadge>
          )}
        </div>
        <span className="text-xs text-surface-400">Priority {index + 1} of {total}</span>
      </div>

      <div className="flex items-center gap-0.5 shrink-0">
        <button
          onClick={() => onMoveUp(index)}
          onKeyDown={(e) => {
            if (e.key === 'ArrowUp' && canMoveUp) {
              e.preventDefault()
              onMoveUp(index)
            } else if (e.key === 'ArrowDown' && canMoveDown) {
              e.preventDefault()
              onMoveDown(index)
            }
          }}
          disabled={!canMoveUp}
          className="flex items-center justify-center w-9 h-9 text-surface-400 hover:text-surface-100 hover:bg-surface-700/50 disabled:opacity-25 disabled:cursor-not-allowed disabled:hover:bg-transparent transition-colors rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
          title="Move up"
          aria-label={`Move ${network.ssid} up in priority`}
        >
          <Icon name="chevron-left" className="w-4 h-4 rotate-[90deg]" aria-hidden="true" />
        </button>
        <button
          onClick={() => onMoveDown(index)}
          onKeyDown={(e) => {
            if (e.key === 'ArrowUp' && canMoveUp) {
              e.preventDefault()
              onMoveUp(index)
            } else if (e.key === 'ArrowDown' && canMoveDown) {
              e.preventDefault()
              onMoveDown(index)
            }
          }}
          disabled={!canMoveDown}
          className="flex items-center justify-center w-9 h-9 text-surface-400 hover:text-surface-100 hover:bg-surface-700/50 disabled:opacity-25 disabled:cursor-not-allowed disabled:hover:bg-transparent transition-colors rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
          title="Move down"
          aria-label={`Move ${network.ssid} down in priority`}
        >
          <Icon name="chevron-left" className="w-4 h-4 rotate-[-90deg]" aria-hidden="true" />
        </button>
        <span className="w-px h-6 bg-surface-700/60 mx-1" aria-hidden="true" />
        <Button
          variant="ghost"
          size="sm"
          icon
          onClick={() => onDisconnect(network.ssid)}
          aria-label={`Forget ${network.ssid}`}
          title="Forget network"
        >
          <Icon name="trash" className="w-4 h-4 text-surface-400" aria-hidden="true" />
        </Button>
      </div>
    </div>
  )
}

export default function WifiPage() {
  const { deviceUrl } = useDevice()
  const toast = useToast()
  const reduceMotion = useReducedMotion()

  const [loading, setLoading] = useState(true)
  const [status, setStatus] = useState(null)
  const [savedNetworks, setSavedNetworks] = useState([])
  const [scannedNetworks, setScannedNetworks] = useState([])
  const [scanning, setScanning] = useState(false)
  const [connecting, setConnecting] = useState(false)
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
    if (connecting) return
    setConnecting(true)
    try {
      const res = await connectWifi(deviceUrl, ssid, password)
      if (res?.success === false) {
        throw new Error(res.error || 'Connection failed')
      }
      toast.success(`Connected to ${ssid}`)
      await fetchStatus()
      await fetchSavedNetworks()
    } catch (err) {
      toast.error(`Connection failed: ${describeError(err)}`)
    } finally {
      setConnecting(false)
    }
  }

  async function handleDisconnect(ssid) {
    if (connecting) return
    setConnecting(true)
    try {
      const res = await disconnectWifi(deviceUrl, ssid)
      if (res?.success === false) {
        throw new Error(res.error || 'Disconnect failed')
      }
      toast.success(`Disconnected from ${ssid}`)
      await fetchStatus()
      await fetchSavedNetworks()
    } catch (err) {
      toast.error(`Disconnect failed: ${describeError(err)}`)
    } finally {
      setConnecting(false)
    }
  }

  async function handleReorder(newNetworks) {
    setSavedNetworks(newNetworks)
    try {
      const ssids = newNetworks.map((n) => n.ssid)
      const res = await reorderWifi(deviceUrl, ssids)
      if (res?.success === false) {
        throw new Error(res.error || 'Reorder failed')
      }
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
    <div className="mx-auto max-w-3xl space-y-4">
      <div className="mb-1">
        <h1 className="text-lg font-bold text-surface-50">Wi-Fi</h1>
        <p className="text-sm text-surface-400 mt-1">
          Connect to a network and manage saved connection priority
        </p>
      </div>

      {/* WiFi Status */}
      <WifiStatusCard status={status} />

      {/* Scan Button */}
      <Card>
        <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
          <div className="flex items-start gap-3 min-w-0">
            <div className="flex items-center justify-center w-9 h-9 rounded-lg bg-brand-500/10 ring-1 ring-brand-500/20 shrink-0">
              <Icon name="magnifying" className="w-5 h-5 text-brand-400" aria-hidden="true" />
            </div>
            <div className="min-w-0">
              <h3 className="text-sm font-semibold text-surface-100">Network Scan</h3>
              <p className="text-xs text-surface-400 mt-0.5">Search for nearby Wi-Fi networks</p>
            </div>
          </div>
          <Button
            variant="secondary"
            size="sm"
            loading={scanning}
            disabled={connecting}
            onClick={handleScan}
            className="sm:ml-auto"
          >
            <Icon name="arrow-path" className={`w-4 h-4 ${scanning && !reduceMotion ? 'animate-spin' : ''}`} aria-hidden="true" />
            {scanning ? 'Scanning…' : 'Scan'}
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
          <fieldset disabled={connecting} className="contents">
            <div className="space-y-2">
              {scannedNetworks.map((network) => (
                <ScannedNetworkItem
                  key={network.ssid}
                  network={network}
                  currentSsid={status?.connected ? status.ssid : null}
                  onConnect={handleConnect}
                  onDisconnect={handleDisconnect}
                  reduceMotion={reduceMotion}
                />
              ))}
            </div>
          </fieldset>
        </Card>
      )}

      {/* Saved Networks */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <CardTitle>Saved Networks</CardTitle>
            {savedNetworks.length > 0 && (
              <StatusBadge variant="neutral" size="sm">{savedNetworks.length}</StatusBadge>
            )}
          </div>
          <button
            onClick={() => setShowSaved((v) => !v)}
            className="flex items-center justify-center w-8 h-8 text-surface-400 hover:text-surface-100 hover:bg-surface-700/50 transition-colors rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
            aria-label={showSaved ? 'Collapse saved networks' : 'Expand saved networks'}
            aria-expanded={showSaved}
          >
            <Icon
              name="chevron-right"
              className={`w-4 h-4 transition-transform ${showSaved ? 'rotate-90' : ''}`}
              aria-hidden="true"
            />
          </button>
        </CardHeader>

        {showSaved && (
          <>
            {savedNetworks.length === 0 ? (
              <div className="flex flex-col items-center text-center gap-2 py-6">
                <div className="flex items-center justify-center w-11 h-11 rounded-full bg-surface-800 text-surface-500">
                  <Icon name="wifi" className="w-5 h-5" aria-hidden="true" />
                </div>
                <p className="text-sm text-surface-400">No saved networks yet</p>
                <p className="text-xs text-surface-500 max-w-xs">
                  Scan above to find and connect to a network. Saved networks reconnect automatically in priority order.
                </p>
              </div>
            ) : (
              <>
                <p className="text-xs text-surface-500 mb-2">
                  Networks are tried top to bottom. Use the arrows to reorder priority.
                </p>
                <fieldset disabled={connecting} className="contents">
                  <div className="space-y-2" role="list" aria-label="Saved WiFi networks ordered by priority">
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
                </fieldset>
              </>
            )}
          </>
        )}
      </Card>
    </div>
  )
}
