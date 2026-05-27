import { useState, useEffect, useCallback, useRef } from 'react'
import { useDevice } from '../DeviceContext.jsx'
import { useWebSocket } from '../WebSocketContext.jsx'
import { useToast } from '../components/Toast.jsx'
import {
  getStatus,
  getHistory,
  getDevices,
  startSync,
  cancelSync,
  selectDevice,
  formatSDCard,
  redetectSDCard,
  getSystemPanic,
  clearSystemPanic,
} from '../api.js'
import { Card, CardHeader, CardTitle } from '../components/Card.jsx'
import { StatusBadge } from '../components/StatusBadge.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { PageLoader } from '../components/LoadingSpinner.jsx'
import { Modal } from '../components/Modal.jsx'

const SYNC_STATUS_CONFIG = {
  idle: { variant: 'neutral', label: 'Idle', pulse: false },
  detected: { variant: 'info', label: 'Card Detected', pulse: false },
  syncing: { variant: 'success', label: 'Syncing', pulse: true },
  cancelling: { variant: 'warning', label: 'Cancelling…', pulse: true },
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

function partitionTitle(partition) {
  return partition?.volume_label || partition?.device_name || partition?.device_path || 'Partition'
}

function partitionDetails(partition) {
  const details = []
  if (partition?.size_human) details.push(partition.size_human)
  else if (partition?.size) details.push(formatBytes(partition.size))
  if (partition?.file_system) details.push(partition.file_system)
  if (partition?.is_mounted) details.push('mounted')
  if (partition?.has_dcim) details.push('DCIM')
  return details.join(' · ')
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

function getProgressLabel(sync) {
  if (!sync) return ''
  const phase = sync.progress_phase || 'preparing'
  const current = sync.files_synced || 0
  const total = sync.files_total || 0
  if (phase === 'checking') return `Checking ${current} of ${total} files`
  if (phase === 'uploading') return `Uploading ${current} of ${total} files`
  return `${current} of ${total} files`
}

function getOverviewCardID(status) {
  if (status?.current_sync?.card_id) {
    return {
      label: 'Current card ID',
      value: status.current_sync.card_id,
    }
  }

  if (status?.last_sync?.card_id) {
    return {
      label: 'Last synced card ID',
      value: status.last_sync.card_id,
    }
  }

  return null
}

function SystemStatusCard({ status }) {
  const statusConf = SYNC_STATUS_CONFIG[status.status] || SYNC_STATUS_CONFIG.idle
  const cardInfo = getOverviewCardID(status)
  const runtime = status.runtime || null
  const memory = runtime?.memory || {}
  const cgroup = runtime?.cgroup || {}

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
          <div
            className="bg-surface-900/50 rounded-lg p-3"
            role="region"
            aria-label="Sync progress"
            aria-live="polite"
            aria-atomic="true"
          >
            <div className="flex items-center justify-between mb-2">
              <span id="sync-progress-label" className="text-xs text-surface-400">
                {getProgressLabel(status.current_sync)}
              </span>
              <span className="text-xs font-medium text-brand-400">
                {getProgressPercent(status.current_sync)}%
              </span>
            </div>
            <div
              className="w-full h-1.5 bg-surface-700 rounded-full overflow-hidden"
              role="progressbar"
              aria-valuemin={0}
              aria-valuemax={100}
              aria-valuenow={getProgressPercent(status.current_sync)}
              aria-labelledby={status.current_sync.current_file ? 'sync-current-file sync-progress-label' : 'sync-progress-label'}
            >
              <div
                className="h-full bg-brand-500 rounded-full transition-all duration-150"
                style={{ width: `${getProgressPercent(status.current_sync)}%` }}
              />
            </div>
            {status.current_sync.current_file && (
              <p id="sync-current-file" className="text-xs text-surface-500 mt-2 truncate">
                {status.current_sync.current_file}
              </p>
            )}
            <div className="grid grid-cols-2 gap-2 mt-2 text-xs text-surface-500">
              <p>
                Speed: {formatSpeed(status.current_sync.transfer_speed)}
              </p>
              <p className="text-right">
                ETA: {status.current_sync.eta || '--'}
              </p>
            </div>
          </div>
        )}

        {status.status === 'error' && status.error && (
          <div className="bg-danger/10 border border-danger/30 rounded-lg p-3">
            <div className="flex items-start gap-2">
              <Icon name="exclamation-triangle" className="w-4 h-4 text-danger shrink-0 mt-0.5" />
              <p className="text-xs text-danger leading-relaxed">{status.error}</p>
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
            icon="cloud"
            label="Device"
            value="Reachable"
            ok
          />
        </div>

        {cardInfo && (
          <div className="pt-2 border-t border-surface-700/50 mt-2">
            <StatusRow
              icon="sd-card"
              label={cardInfo.label}
              value={cardInfo.value}
              ok
            />
          </div>
        )}

        {runtime && (
          <div className="pt-2 border-t border-surface-700/50 mt-2 space-y-3">
            <div className="grid grid-cols-2 gap-3">
              <StatusRow
                icon="clock"
                label="System uptime"
                value={formatDuration(runtime.system_uptime_seconds)}
                ok
              />
              <StatusRow
                icon="clock"
                label="Web UI uptime"
                value={formatDuration(runtime.process_uptime_seconds)}
                ok
              />
              <StatusRow
                icon="activity"
                label="Web UI RSS"
                value={formatBytes(memory.process_rss_bytes || memory.heap_alloc_bytes || 0)}
                ok
              />
              <StatusRow
                icon="activity"
                label="Goroutines"
                value={String(runtime.go?.goroutines ?? '--')}
                ok
              />
            </div>
            {cgroup.memory_current_bytes > 0 && (
              <p className="text-xs text-surface-500">
                Cgroup memory {formatBytes(cgroup.memory_current_bytes)}
                {cgroup.memory_max_bytes > 0 ? ` / ${formatBytes(cgroup.memory_max_bytes)}` : ''}
                {cgroup.oom_kill_count > 0 ? ` · OOM kills: ${cgroup.oom_kill_count}` : ''}
              </p>
            )}
          </div>
        )}

        {status.last_sync && !status.sdcard_mounted && (
          <p className="text-xs text-surface-500">
            Last sync {formatTimeAgo(status.last_sync.end_time || status.last_sync.start_time)}
          </p>
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

function DeviceInfoCard({
  status,
  devices,
  onSelectDevice,
  onFormatDevice,
  onRedetectCard,
  isSelecting,
  isFormatting,
  isRedetecting,
}) {
  const deviceList = devices || []
  const canSelect = (status?.needs_device_select || deviceList.length > 1) && deviceList.length > 0
  const hasCard = status.sdcard_mounted
  const device = hasCard
    ? (deviceList.find((d) => d.is_mounted && d.mount_path === status.sdcard_path) ||
      deviceList.find((d) => d.is_mounted) ||
      deviceList[0])
    : null
  const selectedDevice = device
  const selectedDevicePath = canSelect
    ? (selectedDevice?.device_path || deviceList[0]?.device_path || '')
    : ''
  const photoCount = hasCard
    ? (status.current_sync?.files_total || status.sdcard_photo_count || 0)
    : 0

  const deviceName = selectedDevice?.volume_label || selectedDevice?.device_name || (hasCard ? 'SD Card' : null)
  const deviceSize = selectedDevice?.size_human || (selectedDevice?.size ? formatBytes(selectedDevice.size) : null)
  const devicePath = selectedDevice?.device_path || status.sdcard_device_path || null
  const canFormat = hasCard && devicePath && status.status !== 'syncing' && status.status !== 'cancelling'
  const partitions = selectedDevice?.partitions || []

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
                {selectedDevice?.is_usb && ' (USB)'}
              </p>
            </div>
          </div>

          {hasCard && selectedDevice?.size > 0 && (
            <div>
              <div className="flex justify-between text-xs mb-1">
                <span className="text-surface-400">Card capacity</span>
                <span className="text-surface-300">{formatBytes(selectedDevice.size)}</span>
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

          {partitions.length > 0 && (
            <div className="pt-2 border-t border-surface-700/50">
              <div className="mb-2 flex items-center justify-between gap-2">
                <span className="text-xs text-surface-500">Partitions</span>
                <span className="text-xs text-surface-500">{partitions.length}</span>
              </div>
              <div className="space-y-2">
                {partitions.map((partition) => (
                  <div
                    key={partition.device_path || partition.device_name}
                    className="rounded-md border border-surface-700/60 bg-surface-900/40 px-3 py-2"
                  >
                    <div className="flex min-w-0 items-center justify-between gap-2">
                      <span className="truncate text-sm font-medium text-surface-200">
                        {partitionTitle(partition)}
                      </span>
                      <span className="max-w-[48%] shrink-0 truncate text-xs text-surface-500">
                        {partition.device_path}
                      </span>
                    </div>
                    <div className="mt-1 text-xs text-surface-500">
                      {partitionDetails(partition) || 'No filesystem metadata'}
                    </div>
                    {partition.mount_path && (
                      <div className="mt-1 truncate text-[11px] text-surface-600">
                        {partition.mount_path}
                      </div>
                    )}
                  </div>
                ))}
              </div>
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
          <p className="text-sm text-surface-400">No active SD card detected</p>
          <p className="text-xs text-surface-500 mt-1">
            {deviceList.length > 0 ? 'Re-detect the inserted card or reinsert it' : 'Insert an SD card to begin'}
          </p>
          {deviceList.length > 0 && (
            <Button
              variant="secondary"
              size="md"
              onClick={() => onRedetectCard?.()}
              loading={isRedetecting}
              disabled={status.status === 'syncing' || status.status === 'cancelling'}
              className="mt-4 w-full"
            >
              <Icon name="arrow-path" className="w-4 h-4" />
              Re-detect SD Card
            </Button>
          )}
        </div>
      )}
    </Card>
  )
}

function SyncControls({ status, onSync, onCancel, loading }) {
  const syncState = status.status
  const isSyncing = syncState === 'syncing'
  const isCancelling = syncState === 'cancelling'
  const canStart = syncState === 'idle' || syncState === 'detected' || syncState === 'success' || syncState === 'error'
  const hasCard = status.sdcard_mounted

  return (
    <div className="flex gap-3">
      {isSyncing || isCancelling ? (
        <Button
          variant="danger"
          size="lg"
          onClick={onCancel}
          loading={loading || isCancelling}
          disabled={isCancelling}
          className="flex-1"
          aria-label="Cancel running sync"
        >
          <Icon name="stop" className="w-5 h-5" aria-hidden="true" />
          {isCancelling ? 'Cancelling…' : 'Cancel Sync'}
        </Button>
      ) : (
        <Button
          variant="primary"
          size="lg"
          onClick={onSync}
          loading={loading}
          disabled={!canStart || !hasCard}
          className="flex-1"
          aria-label="Start sync"
        >
          <Icon name="play" className="w-5 h-5" aria-hidden="true" />
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

function PanicInfoCard({ panicInfo, onClear, clearing }) {
  const records = Array.isArray(panicInfo?.panics)
    ? panicInfo.panics
    : (panicInfo?.panic ? [panicInfo.panic] : [])
  if (!panicInfo?.exists || records.length === 0) return null

  const title = records.length === 1 ? 'Saved Panic' : 'Saved Panics'

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <Icon name="exclamation-triangle" className="w-5 h-5 text-danger" />
          <CardTitle>{title}</CardTitle>
          <span className="rounded-full bg-danger/10 px-2 py-0.5 text-xs font-medium text-danger">
            {records.length}
          </span>
        </div>
        <Button
          variant="secondary"
          size="sm"
          onClick={onClear}
          loading={clearing}
          aria-label="Clear saved panic information"
        >
          <Icon name="trash" className="w-4 h-4" aria-hidden="true" />
          Clear
        </Button>
      </CardHeader>

      <div className="space-y-4">
        {records.map((record, index) => {
          const occurredAt = record.time ? new Date(record.time).toLocaleString() : 'Unknown time'
          return (
            <div
              key={`${record.time || 'unknown'}-${record.source || 'panic'}-${index}`}
              className="space-y-3 border-t border-surface-700/60 pt-4 first:border-t-0 first:pt-0"
            >
              <div className="grid gap-2 text-xs text-surface-400 sm:grid-cols-2">
                <div>
                  <p className="text-surface-500">Time</p>
                  <p className="font-medium text-surface-200">{occurredAt}</p>
                </div>
                <div>
                  <p className="text-surface-500">Source</p>
                  <p className="font-medium text-surface-200">{record.source || '--'}</p>
                </div>
              </div>

              <div className="rounded-lg border border-danger/25 bg-danger/10 p-3">
                <p className="text-sm font-medium text-danger break-words">{record.message || 'panic'}</p>
              </div>

              {record.stack && (
                <pre className="max-h-64 overflow-auto rounded-lg border border-surface-700 bg-surface-950 p-3 text-xs leading-relaxed text-surface-300">
                  {record.stack}
                </pre>
              )}
            </div>
          )
        })}
      </div>
    </Card>
  )
}

function FormatSDCardModal({ open, onClose, devicePath, onConfirm, loading }) {
  const [confirmation, setConfirmation] = useState('')
  const [label, setLabel] = useState('')
  const inputRef = useRef(null)

  useEffect(() => {
    if (open) {
      setConfirmation('')
      setLabel('')
    }
  }, [open])

  const canSubmit = confirmation === 'FORMAT' && !loading

  function handleSubmit(e) {
    e?.preventDefault?.()
    if (!canSubmit) return
    onConfirm(confirmation, label.trim())
  }

  return (
    <Modal
      open={open}
      onClose={loading ? () => {} : onClose}
      title="Format SD Card"
      initialFocusRef={inputRef}
    >
      <form onSubmit={handleSubmit} className="space-y-3">
        <p className="text-sm text-surface-300">
          Formatting <span className="font-mono text-surface-100">{devicePath}</span>{' '}
          will erase all files on the SD card. This action cannot be undone.
        </p>
        <div className="space-y-1">
          <label htmlFor="format-confirm" className="block text-xs font-medium text-surface-400">
            Type <span className="font-mono text-danger">FORMAT</span> to continue
          </label>
          <input
            id="format-confirm"
            ref={inputRef}
            type="text"
            autoComplete="off"
            value={confirmation}
            onChange={(e) => setConfirmation(e.target.value)}
            disabled={loading}
            className="w-full bg-surface-950 border border-surface-700 rounded-lg px-3 py-2 text-sm text-surface-100 placeholder:text-surface-500 focus:outline-none focus:ring-2 focus:ring-danger/50 focus:border-danger/50"
            placeholder="FORMAT"
            aria-required="true"
          />
        </div>
        <div className="space-y-1">
          <label htmlFor="format-label" className="block text-xs font-medium text-surface-400">
            Optional volume label
          </label>
          <input
            id="format-label"
            type="text"
            autoComplete="off"
            value={label}
            onChange={(e) => setLabel(e.target.value)}
            disabled={loading}
            className="w-full bg-surface-950 border border-surface-700 rounded-lg px-3 py-2 text-sm text-surface-100 placeholder:text-surface-500 focus:outline-none focus:ring-2 focus:ring-brand-400/50 focus:border-brand-400/50"
            placeholder="Leave blank for no label"
          />
        </div>
        <div className="flex justify-end gap-2 pt-2">
          <Button
            type="button"
            variant="secondary"
            size="md"
            onClick={onClose}
            disabled={loading}
          >
            Cancel
          </Button>
          <Button
            type="submit"
            variant="danger"
            size="md"
            disabled={!canSubmit}
            loading={loading}
          >
            <Icon name="trash" className="w-4 h-4" aria-hidden="true" />
            Format
          </Button>
        </div>
      </form>
    </Modal>
  )
}

export default function StatusPage() {
  const { deviceUrl } = useDevice()
  const { status, wsConnected, wsError, setStatus } = useWebSocket()
  const toast = useToast()

  const [history, setHistory] = useState([])
  const [devices, setDevices] = useState([])
  const [loading, setLoading] = useState(true)
  const [actionLoading, setActionLoading] = useState(false)
  const [selectionLoading, setSelectionLoading] = useState(false)
  const [formatLoading, setFormatLoading] = useState(false)
  const [redetectLoading, setRedetectLoading] = useState(false)
  const [panicInfo, setPanicInfo] = useState(null)
  const [panicClearLoading, setPanicClearLoading] = useState(false)
  const [error, setError] = useState(null)
  const [formatModal, setFormatModal] = useState({ open: false, devicePath: '' })
  const consecutiveErrorsRef = useRef(0)
  const timerRef = useRef(null)
  const lastSyncIdRef = useRef(null)
  const lastMountedRef = useRef(null)

  const fetchData = useCallback(async (isAutoRefresh = false) => {
    if (!deviceUrl) return
    if (!isAutoRefresh) setLoading(true)
    setError(null)
    try {
      const [statusData, historyData] = await Promise.all([
        getStatus(deviceUrl),
        getHistory(deviceUrl),
      ])
      setStatus((currentStatus) => {
        // Simple merge: prefer fetched data but preserve progress during sync
        if (!currentStatus || !statusData) return statusData
        if (currentStatus.status === 'syncing' && statusData.status === 'syncing') {
          const prevFiles = Number(currentStatus.current_sync?.files_synced || 0)
          const nextFiles = Number(statusData.current_sync?.files_synced || 0)
          const prevBytes = Number(currentStatus.current_sync?.bytes_synced || 0)
          const nextBytes = Number(statusData.current_sync?.bytes_synced || 0)
          if (nextFiles < prevFiles || nextBytes < prevBytes) {
            return { ...statusData, current_sync: currentStatus.current_sync }
          }
        }
        return statusData
      })
      setHistory(Array.isArray(historyData) ? historyData : [])
      try {
        const devicesData = await getDevices(deviceUrl)
        setDevices(Array.isArray(devicesData) ? devicesData : [])
      } catch {
        setDevices([])
      }
      try {
        setPanicInfo(await getSystemPanic(deviceUrl))
      } catch {
        setPanicInfo(null)
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
  }, [deviceUrl, toast, setStatus])

  useEffect(() => {
    fetchData()
  }, [fetchData])

  // Refresh history and devices when sync completes or card mounts
  useEffect(() => {
    if (!status || !deviceUrl) return

    const syncID = status.last_sync?.id
    if (syncID && lastSyncIdRef.current !== syncID) {
      lastSyncIdRef.current = syncID
      getHistory(deviceUrl)
        .then((data) => setHistory(Array.isArray(data) ? data : []))
        .catch(() => {})
    }

    const mounted = Boolean(status.sdcard_mounted)
    if (lastMountedRef.current !== mounted) {
      lastMountedRef.current = mounted
      getDevices(deviceUrl)
        .then((data) => setDevices(Array.isArray(data) ? data : []))
        .catch(() => setDevices([]))
    }
  }, [status, deviceUrl])

  // Fallback polling: only while the WebSocket is disconnected.
  useEffect(() => {
    if (!deviceUrl || wsConnected) return undefined

    const scheduleNext = () => {
      const errors = consecutiveErrorsRef.current
      const delay = errors === 0 ? 5000 : Math.min(5000 * Math.pow(2, errors - 1), 30000)
      timerRef.current = window.setTimeout(async () => {
        await fetchData(true)
        scheduleNext()
      }, delay)
    }

    scheduleNext()
    return () => {
      if (timerRef.current) window.clearTimeout(timerRef.current)
    }
  }, [deviceUrl, wsConnected, fetchData])

  // Sync wsError into local error state when we have no status yet
  useEffect(() => {
    if (wsError && !status) {
      setError(wsError)
    }
  }, [wsError, status])

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

  const handleFormatDevice = useCallback((devicePath) => {
    if (!deviceUrl || !devicePath) return
    setFormatModal({ open: true, devicePath })
  }, [deviceUrl])

  const handleFormatConfirm = useCallback(async (confirmation, label) => {
    const devicePath = formatModal.devicePath
    if (!deviceUrl || !devicePath) return
    setFormatLoading(true)
    try {
      await formatSDCard(deviceUrl, devicePath, confirmation, label)
      toast.success('SD card formatted')
      setFormatModal({ open: false, devicePath: '' })
      await fetchData()
    } catch (err) {
      toast.error(`Failed to format SD card: ${describeError(err)}`)
    } finally {
      setFormatLoading(false)
    }
  }, [deviceUrl, formatModal.devicePath, toast, fetchData])

  const handleFormatCancel = useCallback(() => {
    setFormatModal({ open: false, devicePath: '' })
  }, [])

  const handleRedetectCard = useCallback(async () => {
    if (!deviceUrl) return
    setRedetectLoading(true)
    try {
      await redetectSDCard(deviceUrl)
      toast.success('SD card re-detected')
      await fetchData()
    } catch (err) {
      toast.error(`Failed to re-detect SD card: ${describeError(err)}`)
    } finally {
      setRedetectLoading(false)
    }
  }, [deviceUrl, toast, fetchData])

  const handleClearPanic = useCallback(async () => {
    if (!deviceUrl) return
    setPanicClearLoading(true)
    try {
      await clearSystemPanic(deviceUrl)
      setPanicInfo({ exists: false, panics: [] })
      toast.success('Saved panic information cleared')
    } catch (err) {
      toast.error(`Failed to clear panic information: ${describeError(err)}`)
    } finally {
      setPanicClearLoading(false)
    }
  }, [deviceUrl, toast])

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
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-semibold text-surface-100">Overview</h2>
        <Button
          variant="ghost"
          size="sm"
          onClick={() => fetchData()}
          loading={loading}
          aria-label="Refresh status"
        >
          <Icon name="arrow-path" className="w-4 h-4" aria-hidden="true" />
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

      <PanicInfoCard
        panicInfo={panicInfo}
        onClear={handleClearPanic}
        clearing={panicClearLoading}
      />

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
            onRedetectCard={handleRedetectCard}
            isSelecting={selectionLoading}
            isFormatting={formatLoading}
            isRedetecting={redetectLoading}
          />
        </div>
      )}

      {/* Sync history */}
      <SyncHistoryCard history={history} />

      <FormatSDCardModal
        open={formatModal.open}
        devicePath={formatModal.devicePath}
        onClose={handleFormatCancel}
        onConfirm={handleFormatConfirm}
        loading={formatLoading}
      />
    </div>
  )
}
