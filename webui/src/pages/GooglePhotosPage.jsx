import { useState, useEffect, useCallback, useRef } from 'react'
import { useDevice } from '../DeviceContext.jsx'
import { useToast } from '../components/Toast.jsx'
import { Card, CardHeader, CardTitle } from '../components/Card.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { StatusBadge } from '../components/StatusBadge.jsx'
import { LoadingSpinner } from '../components/LoadingSpinner.jsx'
import {
  getGooglePhotosStatus,
  startGooglePhotosAuth,
  disconnectGooglePhotos,
  startGooglePhotosSync,
  cancelGooglePhotosSync,
  getGooglePhotosSyncProgress,
} from '../api.js'

function describeError(err) {
  if (!err) return 'Unknown error'
  const msg = err.message || String(err)
  if (msg.includes('Failed to fetch') || msg.includes('NetworkError') || msg.includes('ERR_NETWORK')) {
    return 'Device unreachable — is it powered on and connected to the network?'
  }
  if (msg.includes('ERR_CONNECTION_REFUSED')) {
    return 'Connection refused — the web server may not be running on the device'
  }
  if (msg.includes('timeout')) {
    return 'Request timed out — the device may be unreachable'
  }
  return msg
}

const ACTIVE_SYNC_STATUSES = new Set(['syncing', 'checking', 'uploading'])
const TERMINAL_SYNC_STATUSES = new Set(['completed', 'error', 'cancelled', 'idle', 'not_initialized'])

function isActiveSyncStatus(status) {
  if (!status) return false
  if (ACTIVE_SYNC_STATUSES.has(status)) return true
  return !TERMINAL_SYNC_STATUSES.has(status)
}

function syncStatusMeta(status) {
  const normalized = String(status || '').trim().toLowerCase()

  if (!normalized || normalized === 'not_initialized') {
    return {
      label: 'Idle',
      tone: 'neutral',
      icon: 'clock',
      pulse: false,
    }
  }

  if (normalized === 'syncing') {
    return {
      label: 'Syncing',
      tone: 'info',
      icon: 'arrow-path',
      pulse: true,
    }
  }

  if (normalized === 'completed') {
    return {
      label: 'Completed',
      tone: 'success',
      icon: 'check',
      pulse: false,
    }
  }

  if (normalized === 'cancelled') {
    return {
      label: 'Cancelled',
      tone: 'warning',
      icon: 'stop',
      pulse: false,
    }
  }

  if (normalized === 'error') {
    return {
      label: 'Error',
      tone: 'danger',
      icon: 'exclamation-triangle',
      pulse: false,
    }
  }

  return {
    label: normalized.replace(/_/g, ' ').replace(/\b\w/g, (c) => c.toUpperCase()),
    tone: 'neutral',
    icon: 'clock',
    pulse: false,
  }
}

function formatBytes(bytes) {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let value = bytes
  let unitIndex = 0
  while (value >= 1024 && unitIndex < units.length - 1) {
    value /= 1024
    unitIndex += 1
  }
  const decimals = value >= 10 || unitIndex === 0 ? 0 : 1
  return `${value.toFixed(decimals)} ${units[unitIndex]}`
}

function formatSpeed(speedBytesPerSecond) {
  const speed = Number(speedBytesPerSecond)
  if (!Number.isFinite(speed) || speed <= 0) return '--'
  return `${formatBytes(speed)}/s`
}

function formatDuration(seconds) {
  if (!seconds || !Number.isFinite(seconds) || seconds <= 0) return '--'

  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  const s = Math.floor(seconds % 60)

  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m ${s}s`
  return `${s}s`
}

function clampPercent(value) {
  if (!Number.isFinite(value)) return 0
  return Math.min(100, Math.max(0, value))
}

function formatPhase(progress) {
  return syncStatusMeta(progress?.status).label
}

function formatStatusSummary(status) {
  if (status?.connected) return 'Connected and ready'
  if (status?.configured) return 'Configured, waiting for account verification'
  return 'Not connected'
}

function formatStatusHint(status) {
  if (status?.connected) {
    return 'Your Google Photos account is connected and remote synchronization can run.'
  }
  if (status?.configured) {
    return 'Google Photos remote exists, but the account could not be verified in this check. You can still try syncing.'
  }
  return 'Connect your Google Photos account to sync from your B2 storage. Configure OAuth credentials in Settings first.'
}

function connectionStatusMeta(status, hasStatusError = false) {
  if (!status) {
    return {
      label: 'Unknown',
      tone: 'neutral',
      icon: 'cloud',
      pulse: false,
    }
  }

  if (status.connected) {
    return {
      label: 'Connected',
      tone: 'success',
      icon: 'check',
      pulse: false,
    }
  }

  if (status.configured) {
    return {
      label: 'Configured',
      tone: 'warning',
      icon: 'clock',
      pulse: true,
    }
  }

  if (!status.configured && hasStatusError) {
    return {
      label: 'Issue',
      tone: 'danger',
      icon: 'exclamation-triangle',
      pulse: true,
    }
  }

  const state = syncStatusMeta('idle')
  return {
    label: state.label,
    tone: state.tone,
    icon: state.icon,
    pulse: state.pulse,
  }
}

  

function StatusPanel({ status, statusError }) {
  const summary = formatStatusSummary(status)
  const hint = formatStatusHint(status)
  const badge = connectionStatusMeta(status, Boolean(statusError))

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <Icon name="cloud" className="w-5 h-5 text-brand-400" />
          <CardTitle>Connection</CardTitle>
        </div>
      </CardHeader>

      <div className="space-y-3">
        <div className="flex items-start justify-between gap-3">
          <div>
            <p className="text-sm text-surface-100">Google Photos OAuth</p>
            <p className="text-sm text-surface-400">{summary}</p>
          </div>
          <StatusBadge variant={badge.tone} pulse={badge.pulse}>
            <Icon name={badge.icon} className="w-3.5 h-3.5" />
            {badge.label}
          </StatusBadge>
        </div>

        <p className="text-sm text-surface-300">{hint}</p>

        {statusError && (
          <div className="rounded-lg border border-amber-500/30 bg-amber-500/10 p-3">
            <p className="text-xs font-medium text-amber-300">Status check issue</p>
            <p className="mt-1 text-xs text-amber-200">{statusError}</p>
          </div>
        )}
      </div>
    </Card>
  )
}

function ActionPanel({
  configured,
  loading,
  syncing,
  connecting,
  disconnecting,
  onConnect,
  onSync,
  onCancel,
  onDisconnect,
  onOpenSettings,
}) {
  const primaryMessage = configured
    ? syncing
      ? 'Sync in progress'
      : 'Sync to Google Photos'
    : 'Connect your account'

  const disablePrimary = loading || syncing || connecting

  return (
    <Card>
      <CardHeader>
        <CardTitle>Actions</CardTitle>
      </CardHeader>

      <div className="space-y-3">
        <p className="text-sm text-surface-300">
          {configured
            ? 'Start a sync to upload photos from all detected cards into album folders in Google Photos.'
            : 'Connect Google Photos first to start syncing your photos.'}
        </p>

        <div className="flex flex-wrap gap-2">
          {!configured ? (
            <>
              <Button onClick={onConnect} loading={connecting} disabled={connecting || loading}>
                <Icon name="lock" className="w-4 h-4" />
                Connect Google Photos
              </Button>
              <Button variant="secondary" onClick={onOpenSettings}>
                <Icon name="settings" className="w-4 h-4" />
                Open Settings
              </Button>
            </>
          ) : (
            <>
              <Button onClick={onSync} loading={syncing} disabled={disablePrimary}>
                <Icon name="arrow-up-tray" className="w-4 h-4" />
                {syncing ? 'Starting…' : primaryMessage}
              </Button>

              <Button variant="secondary" onClick={onCancel} disabled={!syncing || disconnecting}>
                <Icon name="stop" className="w-4 h-4" />
                Cancel sync
              </Button>

              <Button variant="danger" onClick={onDisconnect} loading={disconnecting} disabled={disconnecting || syncing}>
                <Icon name="x" className="w-4 h-4" />
                Disconnect
              </Button>
            </>
          )}
        </div>
      </div>
    </Card>
  )
}

function ProgressPanel({ progress }) {
  if (!progress) return null

  const state = syncStatusMeta(progress.status)
  const percentage = clampPercent(progress?.percentage)
  const hasFiles = Number(progress?.total_files) > 0
  const hasSpeed = Number(progress?.speed) > 0
  const isInProgress = isActiveSyncStatus(progress?.status)

  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <CardTitle>Sync progress</CardTitle>
          <StatusBadge variant={state.tone} pulse={state.pulse}>
            <Icon name={state.icon} className="w-3.5 h-3.5" />
            {state.label}
          </StatusBadge>
        </div>
        {percentage > 0 ? <span className="text-xs text-surface-300">{percentage.toFixed(0)}%</span> : null}
      </CardHeader>

      <div className="space-y-3">
        <div className="grid gap-2 sm:grid-cols-2 text-xs">
          <div className="rounded-lg bg-surface-900/50 p-2">
            <p className="text-surface-400">Status</p>
            <p className="mt-1 text-surface-100">{formatPhase(progress)}</p>
          </div>
          <div className="rounded-lg bg-surface-900/50 p-2">
            <p className="text-surface-400">Files</p>
            <p className="mt-1 text-surface-100">
              {progress.transferred_files || 0} / {hasFiles ? progress.total_files : 'unknown'}
            </p>
          </div>
          <div className="rounded-lg bg-surface-900/50 p-2">
            <p className="text-surface-400">Data synced</p>
            <p className="mt-1 text-surface-100">{formatBytes(progress.bytes_transferred)}</p>
          </div>
          <div className="rounded-lg bg-surface-900/50 p-2">
            <p className="text-surface-400">Speed</p>
            <p className="mt-1 text-surface-100">{hasSpeed ? formatSpeed(progress.speed) : '--'}</p>
          </div>
          <div className="rounded-lg bg-surface-900/50 p-2">
            <p className="text-surface-400">ETA</p>
            <p className="mt-1 text-surface-100">{formatDuration(progress.eta)}</p>
          </div>
          <div className="rounded-lg bg-surface-900/50 p-2">
            <p className="text-surface-400">Mode</p>
            <p className="mt-1 text-surface-100">{isInProgress ? 'Active' : 'Idle'}</p>
          </div>
        </div>

        {progress.current_file && (
          <div className="rounded-lg border border-surface-700/60 p-3">
            <p className="text-xs text-surface-400">Current file</p>
            <p className="mt-1 text-xs text-surface-100 truncate">{progress.current_file}</p>
            {progress.current_file_size > 0 && (
              <p className="mt-1 text-xs text-surface-500">{formatBytes(progress.current_file_size)}</p>
            )}
          </div>
        )}

        {hasFiles && (
          <div className="h-2 rounded-full bg-surface-700 overflow-hidden">
            <div
              className="h-full rounded-full bg-brand-500 transition-all duration-300"
              style={{ width: `${percentage}%` }}
            />
          </div>
        )}

        {progress.error && (
          <div className="rounded-lg border border-rose-500/20 bg-rose-500/10 p-3">
            <p className="text-xs font-medium text-rose-400">Sync Error</p>
            <p className="mt-1 text-xs text-rose-300">{progress.error}</p>
          </div>
        )}
      </div>
    </Card>
  )
}

function SyncStartingPanel() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>Sync starting</CardTitle>
      </CardHeader>
      <div className="space-y-2">
        <p className="text-sm text-surface-300">
          Starting the Google Photos sync run. Status will update here as soon as the backend reports progress.
        </p>
        <div className="h-2 rounded-full bg-surface-700 overflow-hidden">
          <div className="h-full rounded-full bg-brand-500 animate-pulse" style={{ width: '35%' }} />
        </div>
      </div>
    </Card>
  )
}

function InfoPanel() {
  return (
    <Card>
      <CardHeader>
        <CardTitle>How it works</CardTitle>
      </CardHeader>
      <div className="space-y-2 text-sm text-surface-300">
        <p className="text-xs uppercase tracking-wide text-surface-400">Workflow</p>
        <ol className="ml-4 list-decimal space-y-1.5 text-sm text-surface-300">
          <li>
            Configure <strong>Google Photos OAuth client ID</strong> and <strong>client secret</strong> in Settings.
          </li>
          <li>
            Click <strong>Connect Google Photos</strong> and complete the authorization flow.
          </li>
          <li>
            Start a sync. Each card folder is uploaded under album-style destinations created by rclone.
          </li>
          <li>
            Monitor live progress and cancel if needed.
          </li>
        </ol>
        <p className="text-xs text-surface-500">
          Supported formats are filtered by the backend sync engine before upload.
        </p>
      </div>
    </Card>
  )
}

export default function GooglePhotosPage() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [status, setStatus] = useState(null)
  const [loading, setLoading] = useState(true)
  const [connecting, setConnecting] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [disconnecting, setDisconnecting] = useState(false)
  const [progress, setProgress] = useState(null)
  const [statusError, setStatusError] = useState(null)
  const progressIntervalRef = useRef(null)
  const statusIntervalRef = useRef(null)
  const hasLoadedStatusRef = useRef(false)

  const applySyncProgress = useCallback((data) => {
    if (!data) {
      return
    }

    setProgress(data)
    if (isActiveSyncStatus(data.status)) {
      setSyncing(true)
      return
    }
    setSyncing(false)
  }, [])

  const loadSyncProgress = useCallback(async () => {
    if (!deviceUrl) return null
    try {
      const data = await getGooglePhotosSyncProgress(deviceUrl)
      applySyncProgress(data)
      return data
    } catch {
      return null
    }
  }, [deviceUrl, applySyncProgress])

  const loadStatus = useCallback(async () => {
    if (!deviceUrl) return
    try {
      const data = await getGooglePhotosStatus(deviceUrl)
      setStatus(data)
      setStatusError(null)
      hasLoadedStatusRef.current = true
    } catch (err) {
      if (!hasLoadedStatusRef.current) {
        setStatus({ connected: false, configured: false })
        setStatusError(describeError(err))
        hasLoadedStatusRef.current = true
      }
    }
  }, [deviceUrl])

  const loadAll = useCallback(async () => {
    setLoading(true)
    await Promise.all([loadStatus(), loadSyncProgress()])
    setLoading(false)
  }, [loadStatus, loadSyncProgress])

  useEffect(() => {
    loadAll()
  }, [loadAll])

  useEffect(() => {
    if (!deviceUrl) return
    statusIntervalRef.current = setInterval(loadStatus, 10000)
    return () => clearInterval(statusIntervalRef.current)
  }, [deviceUrl, loadStatus])

  useEffect(() => {
    if (!deviceUrl) return
    const shouldPollProgress = isActiveSyncStatus(progress?.status)

    if (!shouldPollProgress) {
      if (progressIntervalRef.current) {
        clearInterval(progressIntervalRef.current)
        progressIntervalRef.current = null
      }
      return
    }

    if (!progressIntervalRef.current) {
      progressIntervalRef.current = setInterval(async () => {
        try {
          const data = await loadSyncProgress()
          if (!isActiveSyncStatus(data?.status)) {
            loadStatus()
          }
        } catch {
          // Ignore transient progress errors.
        }
      }, 2000)
    }

    return () => {
      if (progressIntervalRef.current) {
        clearInterval(progressIntervalRef.current)
        progressIntervalRef.current = null
      }
    }
  }, [deviceUrl, progress?.status, loadSyncProgress, loadStatus])

  const openSettings = useCallback(() => {
    window.location.hash = '#/config'
  }, [])

  const handleConnect = useCallback(async () => {
    if (!deviceUrl) return
    setConnecting(true)

    try {
      const redirectUri = `${deviceUrl}/api/googlephotos/auth/callback`
      const data = await startGooglePhotosAuth(deviceUrl, redirectUri)
      const authUrl = data?.auth_url
      if (!authUrl) {
        toast.error('No authorization URL received')
        return
      }

      const width = 520
      const height = 660
      const left = window.screenX + (window.outerWidth - width) / 2
      const top = window.screenY + (window.outerHeight - height) / 2
      const popup = window.open(
        authUrl,
        'google-photos-auth',
        `width=${width},height=${height},left=${left},top=${top},popup=1`
      )

      if (!popup) {
        toast.error('Popup blocked — allow popups for this page')
        return
      }

      const onMessage = (event) => {
        const isAppEvent = event?.data?.type === 'google-photos-connected'
        if (!isAppEvent) return
        window.removeEventListener('message', onMessage)
        if (progressIntervalRef.current) {
          clearInterval(progressIntervalRef.current)
          progressIntervalRef.current = null
        }
        toast.success('Google Photos connected')
        loadStatus()
      }

      window.addEventListener('message', onMessage)

      const checkClosed = setInterval(() => {
        if (popup.closed) {
          clearInterval(checkClosed)
          window.removeEventListener('message', onMessage)
          loadStatus()
        }
      }, 500)
    } catch (err) {
      toast.error(`Failed to start OAuth: ${describeError(err)}`)
    } finally {
      setConnecting(false)
    }
  }, [deviceUrl, toast, loadStatus])

  const handleDisconnect = useCallback(async () => {
    if (!deviceUrl) return
    if (!window.confirm('Disconnect Google Photos?')) return

    setDisconnecting(true)
    try {
      await disconnectGooglePhotos(deviceUrl)
      toast.success('Google Photos disconnected')
      setStatus({ connected: false, configured: false })
      setStatusError(null)
      setProgress(null)
    } catch (err) {
      toast.error(`Failed to disconnect: ${describeError(err)}`)
    } finally {
      setDisconnecting(false)
    }
  }, [deviceUrl, toast])

  const handleSync = useCallback(async () => {
    if (!deviceUrl) return
    try {
      await startGooglePhotosSync(deviceUrl)
      setSyncing(true)
      toast.success('Sync to Google Photos started')
      await loadSyncProgress()
    } catch (err) {
      const msg = describeError(err)
      if (msg.includes('already in progress')) {
        toast.info('Sync is already running')
        setSyncing(true)
        loadSyncProgress()
        return
      }
      toast.error(`Failed to start sync: ${msg}`)
    }
  }, [deviceUrl, toast, loadSyncProgress])

  const handleCancelSync = useCallback(async () => {
    if (!deviceUrl) return
    if (!window.confirm('Cancel the active sync?')) return

    try {
      await cancelGooglePhotosSync(deviceUrl)
      toast.info('Sync cancellation requested')
      setSyncing(false)
      loadSyncProgress()
    } catch (err) {
      toast.error(`Failed to cancel sync: ${describeError(err)}`)
    }
  }, [deviceUrl, toast, loadSyncProgress])

  const isConfigured = status?.configured
  const showProgress =
    syncing ||
    (progress && ['completed', 'error', 'cancelled'].includes(progress.status || ''))

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <LoadingSpinner size="lg" />
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <StatusPanel status={status} statusError={statusError} />

      <ActionPanel
        configured={isConfigured}
        loading={loading}
        syncing={syncing}
        connecting={connecting}
        disconnecting={disconnecting}
        onConnect={handleConnect}
        onSync={handleSync}
        onCancel={handleCancelSync}
        onDisconnect={handleDisconnect}
        onOpenSettings={openSettings}
      />

      {showProgress ? (progress ? <ProgressPanel progress={progress} /> : <SyncStartingPanel />) : null}

      <InfoPanel />
    </div>
  )
}
