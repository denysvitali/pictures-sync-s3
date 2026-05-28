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
  getGooglePhotosAlbums,
  clearGooglePhotosAlbum,
  getGooglePhotosAlbumClearProgress,
  sortGooglePhotosAlbum,
  getGooglePhotosAlbumSortProgress,
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
  if (status?.configured) return 'OAuth setup saved; account not connected'
  return 'Not connected'
}

function formatStatusHint(status) {
  if (status?.connected) {
    return 'Your Google Photos account is connected and remote synchronization can run.'
  }
  if (status?.configured) {
    return 'Complete the OAuth connection to create the Google Photos rclone remote before syncing.'
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
  connected,
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
  const primaryMessage = connected
    ? syncing
      ? 'Sync in progress'
      : 'Sync to Google Photos'
    : 'Connect Google Photos'

  const disablePrimary = loading || syncing || connecting

  return (
    <Card>
      <CardHeader>
        <CardTitle>Actions</CardTitle>
      </CardHeader>

      <div className="space-y-3">
        <p className="text-sm text-surface-300">
          {connected
            ? 'Start a sync to upload photos from all detected cards into album folders in Google Photos.'
            : configured
              ? 'Complete the Google Photos OAuth connection before syncing your photos.'
              : 'Connect Google Photos first to start syncing your photos.'}
        </p>

        <div className="flex flex-wrap gap-2">
          {!connected ? (
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

function AlbumsPanel({ albums, loading, onClear, clearingId, clearProgress, onSort, sortingId, sortProgress }) {
  if (loading) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Albums</CardTitle>
        </CardHeader>
        <div className="flex items-center gap-2 text-sm text-surface-400">
          <LoadingSpinner size="sm" />
          Loading albums...
        </div>
      </Card>
    )
  }

  if (!albums || albums.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle>Albums</CardTitle>
        </CardHeader>
        <p className="text-sm text-surface-400">No app-managed albums found yet. Run a sync to create them.</p>
      </Card>
    )
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Albums</CardTitle>
      </CardHeader>
      <div className="space-y-2">
        <p className="text-xs text-surface-500">
          These are the albums created by this app. You can clear an album to remove all its photos from Google Photos.
        </p>
        <ul className="space-y-2">
          {albums.map((album) => {
            const progress = clearProgress?.[album.id]
            const isClearing = clearingId === album.id
            const clearLabel = isClearing && progress?.status === 'clearing' && progress?.total_items > 0
              ? `Removing ${progress.removed_items}/${progress.total_items}...`
              : 'Clear'
            const hasProgressCounts = Number(progress?.total_items) > 0 || Number(progress?.removed_items) > 0
            const isError = progress?.status === 'error'
            const errorMessage = isError ? (progress?.error || 'Failed to clear album') : ''
            const errorLower = errorMessage.toLowerCase()
            const showPermissionHint =
              isError &&
              (errorLower.includes('permission_denied') ||
                errorLower.includes('no permission') ||
                errorLower.includes('permission'))

            const sp = sortProgress?.[album.id]
            const isSorting = sortingId === album.id
            const sortLabel = isSorting && sp?.status !== 'completed' && sp?.status !== 'error'
              ? `Sorting...`
              : 'Sort'
            const sortError = sp?.status === 'error' ? (sp?.error || 'Failed to sort album') : ''

            return (
              <li
                key={album.id}
                className="rounded-lg border border-surface-700/60 p-3"
              >
                <div className="flex items-center justify-between gap-3">
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-surface-100 truncate">{album.title}</p>
                    <p className="text-xs text-surface-500">
                      {album.mediaItemsCount ? `${album.mediaItemsCount} items` : 'Empty'}
                    </p>
                  </div>
                  <div className="flex items-center gap-2">
                    <Button
                      variant="secondary"
                      size="sm"
                      loading={isSorting}
                      disabled={isSorting || isClearing}
                      onClick={() => onSort(album.id, album.title)}
                    >
                      <Icon name="sort" className="w-4 h-4" />
                      {sortLabel}
                    </Button>
                    <Button
                      variant="danger"
                      size="sm"
                      loading={isClearing}
                      disabled={isClearing || isSorting}
                      onClick={() => onClear(album.id, album.title)}
                    >
                      <Icon name="trash" className="w-4 h-4" />
                      {clearLabel}
                    </Button>
                  </div>
                </div>
                {hasProgressCounts && (
                  <p className="mt-2 text-xs text-surface-400">
                    Removed {progress?.removed_items || 0}
                    {Number(progress?.total_items) > 0 ? `/${progress.total_items}` : ''}
                  </p>
                )}
                {sortError && (
                  <div className="mt-2 rounded-lg border border-rose-500/20 bg-rose-500/10 p-3">
                    <p className="text-xs font-medium text-rose-400">Failed to sort album</p>
                    <p className="mt-1 text-xs text-rose-300 break-words whitespace-pre-wrap">{sortError}</p>
                  </div>
                )}
                {isError && (
                  <div className="mt-2 rounded-lg border border-rose-500/20 bg-rose-500/10 p-3">
                    <p className="text-xs font-medium text-rose-400">Failed to clear album</p>
                    <p className="mt-1 text-xs text-rose-300 break-words whitespace-pre-wrap">{errorMessage}</p>
                    {showPermissionHint && (
                      <p className="mt-2 text-xs text-rose-200/80 break-words">
                        The Google Photos v1 API only allows removing items the app uploaded, from albums the app created. Items added through the Google Photos app or website cannot be cleared from here.
                      </p>
                    )}
                  </div>
                )}
              </li>
            )
          })}
        </ul>
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
  const [albums, setAlbums] = useState(null)
  const [albumsLoading, setAlbumsLoading] = useState(false)
  const [clearingAlbumId, setClearingAlbumId] = useState(null)
  const [clearProgress, setClearProgress] = useState({})
  const [sortingAlbumId, setSortingAlbumId] = useState(null)
  const [sortProgress, setSortProgress] = useState({})
  const progressIntervalRef = useRef(null)
  const statusIntervalRef = useRef(null)
  const clearProgressIntervalRef = useRef(null)
  const sortProgressIntervalRef = useRef(null)
  const hasLoadedStatusRef = useRef(false)

  const applySyncProgress = useCallback((data) => {
    if (!data) {
      return
    }

    setProgress(data)
    if (!data.status) {
      return
    }
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

  const loadAlbums = useCallback(async () => {
    if (!deviceUrl) return
    setAlbumsLoading(true)
    try {
      const data = await getGooglePhotosAlbums(deviceUrl)
      setAlbums(data?.albums || [])
    } catch (err) {
      setAlbums([])
    } finally {
      setAlbumsLoading(false)
    }
  }, [deviceUrl])

  const handleClearAlbum = useCallback(
    async (albumId, albumTitle) => {
      if (!deviceUrl) return
      if (!window.confirm(`Clear all photos from "${albumTitle}"? This cannot be undone.`)) return

      setClearingAlbumId(albumId)
      setClearProgress((prev) => ({ ...prev, [albumId]: { status: 'clearing', removed_items: 0, total_items: 0 } }))

      let pollInterval = null
      const stopPolling = () => {
        if (pollInterval) {
          clearInterval(pollInterval)
          pollInterval = null
        }
      }

      try {
        await clearGooglePhotosAlbum(deviceUrl, albumId)

        pollInterval = setInterval(async () => {
          try {
            const data = await getGooglePhotosAlbumClearProgress(deviceUrl, albumId)
            setClearProgress((prev) => ({ ...prev, [albumId]: data }))

            if (data?.status === 'completed' || data?.status === 'error' || data?.status === 'idle') {
              stopPolling()
              setClearingAlbumId(null)
              if (data?.status === 'completed') {
                toast.success(`Cleared ${data?.removed_items || 0} item(s) from "${albumTitle}"`)
                loadAlbums()
              } else if (data?.status === 'error') {
                toast.error(`Failed to clear album: ${data?.error || 'Unknown error'}`)
              }
            }
          } catch (err) {
            stopPolling()
            setClearingAlbumId(null)
            toast.error(`Failed to clear album: ${describeError(err)}`)
          }
        }, 1000)

        clearProgressIntervalRef.current = pollInterval
      } catch (err) {
        stopPolling()
        setClearingAlbumId(null)
        toast.error(`Failed to start clearing: ${describeError(err)}`)
      }
    },
    [deviceUrl, toast, loadAlbums]
  )

  const handleSortAlbum = useCallback(
    async (albumId, albumTitle) => {
      if (!deviceUrl) return

      setSortingAlbumId(albumId)
      setSortProgress((prev) => ({ ...prev, [albumId]: { status: 'listing', total_items: 0, added_items: 0 } }))

      let pollInterval = null
      const stopPolling = () => {
        if (pollInterval) {
          clearInterval(pollInterval)
          pollInterval = null
        }
      }

      try {
        await sortGooglePhotosAlbum(deviceUrl, albumId)

        pollInterval = setInterval(async () => {
          try {
            const data = await getGooglePhotosAlbumSortProgress(deviceUrl, albumId)
            setSortProgress((prev) => ({ ...prev, [albumId]: data }))

            if (data?.status === 'completed' || data?.status === 'error' || data?.status === 'idle') {
              stopPolling()
              setSortingAlbumId(null)
              if (data?.status === 'completed') {
                toast.success(`Sorted "${albumTitle}" — ${data?.total_items || 0} items reordered`)
                loadAlbums()
              } else if (data?.status === 'error') {
                toast.error(`Failed to sort album: ${data?.error || 'Unknown error'}`)
              }
            }
          } catch (err) {
            stopPolling()
            setSortingAlbumId(null)
            toast.error(`Failed to sort album: ${describeError(err)}`)
          }
        }, 1000)

        sortProgressIntervalRef.current = pollInterval
      } catch (err) {
        stopPolling()
        setSortingAlbumId(null)
        toast.error(`Failed to start sorting: ${describeError(err)}`)
      }
    },
    [deviceUrl, toast, loadAlbums]
  )

  useEffect(() => {
    loadAll()
  }, [loadAll])

  useEffect(() => {
    if (status?.connected) {
      loadAlbums()
    }
  }, [status?.connected, loadAlbums])

  useEffect(() => {
    if (!deviceUrl) return
    statusIntervalRef.current = setInterval(loadStatus, 10000)
    return () => {
      clearInterval(statusIntervalRef.current)
      if (clearProgressIntervalRef.current) {
        clearInterval(clearProgressIntervalRef.current)
      }
      if (sortProgressIntervalRef.current) {
        clearInterval(sortProgressIntervalRef.current)
      }
    }
  }, [deviceUrl, loadStatus])

  useEffect(() => {
    if (!deviceUrl) return
    const shouldPollProgress = syncing || isActiveSyncStatus(progress?.status)

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
  }, [deviceUrl, syncing, progress?.status, loadSyncProgress, loadStatus])

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
    if (!status?.connected) {
      toast.error('Connect Google Photos before starting a sync')
      return
    }
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
  }, [deviceUrl, status?.connected, toast, loadSyncProgress])

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

  const isConnected = Boolean(status?.connected)
  const isConfigured = Boolean(status?.configured)
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
        connected={isConnected}
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

      {isConnected && (
        <AlbumsPanel
          albums={albums}
          loading={albumsLoading}
          onClear={handleClearAlbum}
          clearingId={clearingAlbumId}
          clearProgress={clearProgress}
          onSort={handleSortAlbum}
          sortingId={sortingAlbumId}
          sortProgress={sortProgress}
        />
      )}

      <InfoPanel />
    </div>
  )
}
