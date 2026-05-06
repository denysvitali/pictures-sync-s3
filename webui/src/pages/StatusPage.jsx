import { useState, useEffect, useCallback, useRef } from 'react'
import { useDevice } from '../DeviceContext.jsx'
import { useToast } from '../components/Toast.jsx'
import {
  getStatus,
  getHistory,
  getDevices,
  startSync,
  cancelSync,
  selectDevice,
  formatSDCard,
} from '../api.js'
import { Card, CardHeader, CardTitle } from '../components/Card.jsx'
import { StatusBadge } from '../components/StatusBadge.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { PageLoader } from '../components/LoadingSpinner.jsx'

const SYNC_STATUS_CONFIG = {
  idle: { variant: 'neutral', label: 'Idle', pulse: false },
  detected: { variant: 'info', label: 'Card Detected', pulse: false },
  syncing: { variant: 'success', label: 'Syncing', pulse: true },
  success: { variant: 'success', label: 'Completed', pulse: false },
  error: { variant: 'danger', label: 'Error', pulse: false },
}

function formatBytes(bytes) {
  if (!bytes || bytes === 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(bytes) / Math.log(1024))
  return `${(bytes / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}

function formatSpeed(bytesPerSecond) {
  const speed = Number(bytesPerSecond)
  if (!Number.isFinite(speed) || speed <= 0) return '--'
  return `${formatBytes(speed)}/s`
}

function getDeviceDisplayName(device) {
  const base = device?.volume_label || device?.device_name || device?.device_path || 'Unknown device'
  const details = []

  if (device?.size_human) details.push(device.size_human)
  if (device?.is_usb) details.push('USB')
  if (device?.mount_path) details.push(device.mount_path)

  if (details.length === 0) return base
  return `${base} (${details.join(' · ')})`
}

function describeError(err) {
  if (!err) return 'Unknown error'
  const msg = err.message || String(err)
  if (msg.includes('Failed to fetch') || msg.includes('NetworkError') || msg.includes('ERR_NETWORK')) {
    return 'Device unreachable — is it powered on and connected to the network?'
  }
  if (msg.includes('ERR_CONNECTION_REFUSED')) {
    return 'Connection refused — the web server may not be running on this device'
  }
  if (msg.includes('ERR_CONNECTION_TIMED_OUT') || msg.includes('timeout')) {
    return 'Request timed out — the device may be slow or unreachable'
  }
  if (msg.includes('401') || msg.includes('403') || msg.toLowerCase().includes('unauthorized')) {
    return 'Authentication failed — check the device credentials'
  }
  return msg
}

function formatDuration(seconds) {
  if (!seconds || seconds <= 0) return '--'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

function formatTimeAgo(dateStr) {
  if (!dateStr) return '--'
  const date = new Date(dateStr)
  const now = new Date()
  const diffMs = now - date
  const diffSec = Math.floor(diffMs / 1000)
  const diffMin = Math.floor(diffSec / 60)
  const diffHr = Math.floor(diffMin / 60)
  const diffDay = Math.floor(diffHr / 24)

  if (diffSec < 60) return 'just now'
  if (diffMin < 60) return `${diffMin}m ago`
  if (diffHr < 24) return `${diffHr}h ago`
  if (diffDay < 7) return `${diffDay}d ago`
  return date.toLocaleDateString('en-US', { month: 'short', day: 'numeric' })
}

function getProgressPercent(sync) {
  if (!sync || !sync.files_total) return 0
  return Math.min(100, Math.round((sync.files_synced / sync.files_total) * 100))
}

function SystemStatusCard({ status }) {
  const statusConf = SYNC_STATUS_CONFIG[status.status] || SYNC_STATUS_CONFIG.idle

  return (
    <Card>
      <CardHeader>
        <CardTitle>System Status</CardTitle>
        <StatusBadge variant={statusConf.variant} pulse={statusConf.pulse}>
          {statusConf.label}
        </StatusBadge>
      </CardHeader>

      <div className="space-y-3">
        {/* Active sync progress */}
        {status.current_sync && (
          <div className="bg-surface-900/50 rounded-lg p-3">
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs text-surface-400">
                {status.current_sync.files_synced || 0} of {status.current_sync.files_total || 0} files
              </span>
              <span className="text-xs font-medium text-brand-400">
                {getProgressPercent(status.current_sync)}%
              </span>
            </div>
            <div className="w-full h-1.5 bg-surface-700 rounded-full overflow-hidden">
              <div
                className="h-full bg-brand-500 rounded-full transition-all duration-500"
                style={{ width: `${getProgressPercent(status.current_sync)}%` }}
              />
            </div>
            {status.current_sync.current_file && (
              <p className="text-xs text-surface-500 mt-2 truncate">
                {status.current_sync.current_file}
              </p>
            )}
            <div className="grid grid-cols-2 gap-2 mt-2 text-xs text-surface-500">
              <p>
                Upload: {formatSpeed(status.current_sync.transfer_speed)}
              </p>
              <p className="text-right">
                ETA: {status.current_sync.eta || '--'}
              </p>
            </div>
          </div>
        )}

        {/* Status indicators */}
        <div className="grid grid-cols-2 gap-3">
          <StatusRow
            icon="image"
            label="SD Card"
            value={status.sdcard_mounted ? 'Inserted' : 'None'}
            ok={status.sdcard_mounted}
          />
          <StatusRow
            icon="wifi"
            label="WiFi"
            value={status.status !== 'idle' || status.sdcard_mounted ? 'Connected' : 'Unknown'}
            ok={status.status !== 'idle' || status.sdcard_mounted}
          />
        </div>

        {/* Current remote */}
        {status.last_sync && (
          <div className="flex items-center gap-2 text-sm text-surface-300">
            <Icon name="cloud" className="w-4 h-4 text-surface-400" />
            <span className="truncate">
              {status.last_sync.card_id ? `Card ${status.last_sync.card_id}` : 'No card data'}
            </span>
          </div>
        )}
      </div>
    </Card>
  )
}

function StatusRow({ icon, label, value, ok }) {
  return (
    <div className="flex items-center gap-2.5">
      <div className={`w-8 h-8 rounded-lg flex items-center justify-center shrink-0 ${ok ? 'bg-success/10' : 'bg-surface-700/50'}`}>
        <Icon name={icon} className={`w-4 h-4 ${ok ? 'text-success' : 'text-surface-500'}`} />
      </div>
      <div className="min-w-0">
        <p className="text-xs text-surface-500">{label}</p>
        <p className="text-sm font-medium text-surface-200 truncate">{value}</p>
      </div>
    </div>
  )
}

function DeviceInfoCard({ status, devices, onSelectDevice, onFormatDevice, isSelecting, isFormatting }) {
  const deviceList = devices || []
  const canSelect = (status?.needs_device_select || deviceList.length > 1) && deviceList.length > 0
  const device = deviceList.find((d) => d.is_mounted) || deviceList[0]
  const selectedDevice = deviceList.find((d) => d.device_path === status.sdcard_path) || device
  const selectedDevicePath = canSelect
    ? (selectedDevice?.device_path || deviceList[0]?.device_path || '')
    : ''
  const hasCard = status.sdcard_mounted
  const photoCount = status.current_sync?.files_total || status.last_sync?.files_total || 0

  const deviceName = device?.volume_label || device?.device_name || (hasCard ? 'SD Card' : null)
  const deviceSize = device?.size_human || (device?.size ? formatBytes(device.size) : null)
  const devicePath = device?.device_path || status.sdcard_path || null
  const canFormat = hasCard && devicePath && status.status !== 'syncing'

  return (
    <Card>
      <CardHeader>
        <CardTitle>Device Info</CardTitle>
      </CardHeader>

      {device || hasCard ? (
        <div className="space-y-3">
          <div className="flex items-center gap-3">
            <div className="w-10 h-10 rounded-lg bg-info/10 flex items-center justify-center shrink-0">
              <Icon name="folder" className="w-5 h-5 text-info" />
            </div>
            <div className="min-w-0">
              <p className="text-sm font-medium text-surface-200 truncate">
                {deviceName}
              </p>
              <p className="text-xs text-surface-500">
                {deviceSize || (hasCard ? 'Mounted' : '')}
                {device?.is_usb && ' (USB)'}
              </p>
            </div>
          </div>

          {device?.size > 0 && (
            <div>
              <div className="flex justify-between text-xs mb-1">
                <span className="text-surface-400">Storage used</span>
                <span className="text-surface-300">{formatBytes(device.size)}</span>
              </div>
            </div>
          )}

          {photoCount > 0 && (
            <div className="flex items-center justify-between py-2 border-t border-surface-700/50">
              <div className="flex items-center gap-2">
                <Icon name="image" className="w-4 h-4 text-surface-400" />
                <span className="text-sm text-surface-300">Photos</span>
              </div>
              <span className="text-sm font-semibold text-surface-100">
                {photoCount.toLocaleString()}
              </span>
            </div>
          )}

          {canSelect && (
            <div className="pt-2 border-t border-surface-700/50 space-y-2">
              <label className="block text-xs text-surface-500">Active storage device</label>
              <select
                className="w-full border border-surface-700 rounded-lg bg-surface-800 px-3 py-2 text-sm text-surface-100"
                value={selectedDevicePath}
                disabled={isSelecting}
                onChange={(e) => onSelectDevice?.(e.target.value)}
              >
                {deviceList.map((candidate) => (
                  <option
                    key={candidate.device_path || candidate.device_name}
                    value={candidate.device_path}
                  >
                    {getDeviceDisplayName(candidate)}
                  </option>
                ))}
              </select>
              <p className="text-xs text-surface-500">
                Multiple cards detected. Select the device you want to sync from.
              </p>
            </div>
          )}

          <div className="pt-2 border-t border-surface-700/50">
            <Button
              variant="danger"
              size="md"
              onClick={() => onFormatDevice?.(devicePath)}
              loading={isFormatting}
              disabled={!canFormat}
              className="w-full"
            >
              <Icon name="trash" className="w-4 h-4" />
              Format SD Card
            </Button>
          </div>
        </div>
      ) : (
        <div className="text-center py-6">
          <Icon name="image" className="w-8 h-8 text-surface-600 mx-auto mb-2" />
          <p className="text-sm text-surface-400">No storage device detected</p>
          <p className="text-xs text-surface-500 mt-1">Insert an SD card to begin</p>
        </div>
      )}
    </Card>
  )
}

function SyncControls({ status, onSync, onCancel, loading }) {
  const syncState = status.status
  const isSyncing = syncState === 'syncing'
  const canStart = syncState === 'idle' || syncState === 'detected' || syncState === 'success' || syncState === 'error'
  const hasCard = status.sdcard_mounted

  return (
    <div className="flex gap-3">
      {isSyncing ? (
        <Button
          variant="danger"
          size="lg"
          onClick={onCancel}
          loading={loading}
          className="flex-1"
        >
          <Icon name="stop" className="w-5 h-5" />
          Cancel Sync
        </Button>
      ) : (
        <Button
          variant="primary"
          size="lg"
          onClick={onSync}
          loading={loading}
          disabled={!canStart || !hasCard}
          className="flex-1"
        >
          <Icon name="play" className="w-5 h-5" />
          Start Sync
        </Button>
      )}
    </div>
  )
}

function SyncHistoryCard({ history }) {
  if (!history || history.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Recent Syncs</CardTitle>
        </CardHeader>
        <div className="text-center py-6">
          <Icon name="clock" className="w-8 h-8 text-surface-600 mx-auto mb-2" />
          <p className="text-sm text-surface-400">No sync history yet</p>
        </div>
      </Card>
    )
  }

  const recent = history.slice(-5).reverse()

  return (
    <Card>
      <CardHeader>
        <CardTitle>Recent Syncs</CardTitle>
        <span className="text-xs text-surface-500">{history.length} total</span>
      </CardHeader>

      <div className="space-y-2">
        {recent.map((entry, i) => {
          const entryStatus = SYNC_STATUS_CONFIG[entry.status] || SYNC_STATUS_CONFIG.idle
          const duration = entry.end_time && entry.start_time
            ? (new Date(entry.end_time) - new Date(entry.start_time)) / 1000
            : null

          return (
            <div
              key={entry.id || i}
              className="flex items-center gap-3 py-2.5 px-3 rounded-lg bg-surface-900/30"
            >
              <div className="flex-1 min-w-0">
                <div className="flex items-center gap-2 mb-0.5">
                  <StatusBadge variant={entryStatus.variant}>
                    {entry.status}
                  </StatusBadge>
                  <span className="text-xs text-surface-500">
                    {formatTimeAgo(entry.start_time)}
                  </span>
                </div>
                <p className="text-sm text-surface-300">
                  {entry.files_synced || 0} files synced
                  {duration != null && (
                    <span className="text-surface-500"> in {formatDuration(duration)}</span>
                  )}
                </p>
                {entry.error && (
                  <p className="text-xs text-danger mt-1 truncate">{entry.error}</p>
                )}
              </div>
              <Icon name="chevron-right" className="w-4 h-4 text-surface-600 shrink-0" />
            </div>
          )
        })}
      </div>
    </Card>
  )
}

export default function StatusPage() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [status, setStatus] = useState(null)
  const [history, setHistory] = useState([])
  const [devices, setDevices] = useState([])
  const [loading, setLoading] = useState(true)
  const [actionLoading, setActionLoading] = useState(false)
  const [selectionLoading, setSelectionLoading] = useState(false)
  const [formatLoading, setFormatLoading] = useState(false)
  const [error, setError] = useState(null)
  const consecutiveErrorsRef = useRef(0)
  const timerRef = useRef(null)

  const fetchData = useCallback(async (isAutoRefresh = false) => {
    if (!deviceUrl) return
    if (!isAutoRefresh) setLoading(true)
    setError(null)
    try {
      const [statusData, historyData] = await Promise.all([
        getStatus(deviceUrl),
        getHistory(deviceUrl),
      ])
      setStatus(statusData)
      setHistory(Array.isArray(historyData) ? historyData : [])
      try {
        const devicesData = await getDevices(deviceUrl)
        setDevices(Array.isArray(devicesData) ? devicesData : [])
      } catch {
        setDevices([])
      }
      consecutiveErrorsRef.current = 0
    } catch (err) {
      const detailed = describeError(err)
      setDevices([])
      setError(detailed)
      consecutiveErrorsRef.current++
      if (!isAutoRefresh) {
        toast.error(`Could not reach device: ${detailed}`)
      }
    } finally {
      setLoading(false)
    }
  }, [deviceUrl, toast])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  // Auto-refresh while syncing with exponential backoff on errors
  useEffect(() => {
    if (!status || status.status !== 'syncing') return

    const scheduleNext = () => {
      const errors = consecutiveErrorsRef.current
      const delay = errors === 0 ? 3000 : Math.min(3000 * Math.pow(2, errors - 1), 30000)
      timerRef.current = setTimeout(async () => {
        await fetchData(true)
        if (consecutiveErrorsRef.current < 5) {
          scheduleNext()
        }
      }, delay)
    }

    scheduleNext()
    return () => clearTimeout(timerRef.current)
  }, [status?.status, fetchData])

  const handleStartSync = async () => {
    setActionLoading(true)
    try {
      await startSync(deviceUrl)
      toast.success('Sync started')
      await fetchData()
    } catch (err) {
      toast.error(`Failed to start sync: ${describeError(err)}`)
    } finally {
      setActionLoading(false)
    }
  }

  const handleCancelSync = async () => {
    setActionLoading(true)
    try {
      await cancelSync(deviceUrl)
      toast.info('Sync cancelled')
      await fetchData()
    } catch (err) {
      toast.error(`Failed to cancel sync: ${describeError(err)}`)
    } finally {
      setActionLoading(false)
    }
  }

  const handleSelectDevice = useCallback(async (devicePath) => {
    if (!deviceUrl || !devicePath) return
    setSelectionLoading(true)
    try {
      await selectDevice(deviceUrl, devicePath)
      toast.success('Storage device selected')
      await fetchData()
    } catch (err) {
      toast.error(`Failed to select device: ${describeError(err)}`)
    } finally {
      setSelectionLoading(false)
    }
  }, [deviceUrl, toast, fetchData])

  const handleFormatDevice = useCallback(async (devicePath) => {
    if (!deviceUrl || !devicePath) return

    const confirmation = window.prompt(
      `Formatting ${devicePath} will erase all files on the SD card. Type FORMAT to continue.`
    )
    if (confirmation !== 'FORMAT') {
      toast.info('Format cancelled')
      return
    }
    const rawLabel = window.prompt('Optional volume label (leave blank for no label):', '')
    if (rawLabel === null) {
      toast.info('Format cancelled')
      return
    }
    const label = rawLabel.trim()

    setFormatLoading(true)
    try {
      await formatSDCard(deviceUrl, devicePath, confirmation, label)
      toast.success('SD card formatted')
      await fetchData()
    } catch (err) {
      toast.error(`Failed to format SD card: ${describeError(err)}`)
    } finally {
      setFormatLoading(false)
    }
  }, [deviceUrl, toast, fetchData])

  if (loading && !status) {
    return <PageLoader />
  }

  if (error && !status) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[50vh] text-center px-4">
        <Icon name="exclamation-triangle" className="w-12 h-12 text-danger mb-4" />
        <h2 className="text-lg font-semibold text-surface-200 mb-2">Connection Error</h2>
        <p className="text-sm text-surface-400 mb-2 max-w-xs">{error}</p>
        <p className="text-xs text-surface-500 mb-4 max-w-xs">
          Make sure the device is powered on, connected to the network, and the web server is running.
        </p>
        <Button variant="secondary" onClick={() => fetchData()}>
          <Icon name="arrow-path" className="w-4 h-4" />
          Try Again
        </Button>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* Refresh button */}
      <div className="flex items-center justify-between">
        <h2 className="text-lg font-semibold text-surface-100">Overview</h2>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => fetchData()}
          loading={loading}
        >
          <Icon name="arrow-path" className="w-4 h-4" />
          Refresh
        </Button>
      </div>

      {/* Warning when auto-refresh keeps failing */}
      {error && consecutiveErrorsRef.current > 0 && (
        <div className="flex items-start gap-3 px-4 py-3 rounded-lg bg-warning/10 border border-warning/20">
          <Icon name="exclamation-triangle" className="w-5 h-5 text-warning shrink-0 mt-0.5" />
          <div className="min-w-0">
            <p className="text-sm font-medium text-warning">Connection issues detected</p>
            <p className="text-xs text-surface-400 mt-1">{error}</p>
          </div>
        </div>
      )}

      {/* Sync controls */}
      {status && (
        <SyncControls
          status={status}
          onSync={handleStartSync}
          onCancel={handleCancelSync}
          loading={actionLoading}
        />
      )}

      {/* Status cards */}
      {status && (
        <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
          <SystemStatusCard status={status} />
          <DeviceInfoCard
            status={status}
            devices={devices}
            onSelectDevice={handleSelectDevice}
            onFormatDevice={handleFormatDevice}
            isSelecting={selectionLoading}
            isFormatting={formatLoading}
          />
        </div>
      )}

      {/* Sync history */}
      <SyncHistoryCard history={history} />
    </div>
  )
}
