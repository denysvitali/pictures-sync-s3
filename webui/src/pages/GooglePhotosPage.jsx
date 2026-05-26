import { useState, useEffect, useCallback, useRef } from 'react'
import { useDevice } from '../DeviceContext.jsx'
import { useToast } from '../components/Toast.jsx'
import { Card, CardHeader, CardTitle } from '../components/Card.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { LoadingSpinner } from '../components/LoadingSpinner.jsx'
import {
  getGooglePhotosStatus,
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
    return 'Connection refused — the web server may not be running'
  }
  if (msg.includes('timeout')) {
    return 'Request timed out — the device may be unreachable'
  }
  return msg
}

const activeSyncStatuses = new Set(['listing_cards', 'syncing'])
const terminalSyncStatuses = new Set(['completed', 'error', 'cancelled', 'idle'])

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

function clampPercent(value) {
  if (!Number.isFinite(value)) return 0
  return Math.min(100, Math.max(0, value))
}

function formatPhase(progress) {
  return progress?.status?.replace(/_/g, ' ') || 'Syncing'
}

export default function GooglePhotosPage() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [status, setStatus] = useState(null)
  const [loading, setLoading] = useState(true)
  const [syncing, setSyncing] = useState(false)
  const [progress, setProgress] = useState(null)
  const [statusError, setStatusError] = useState(null)
  const progressIntervalRef = useRef(null)
  const statusIntervalRef = useRef(null)
  const hasLoadedStatusRef = useRef(false)

  const applySyncProgress = useCallback((data) => {
    const syncStatus = data?.status

    if (activeSyncStatuses.has(syncStatus)) {
      setSyncing(true)
      setProgress(data)
      return
    }

    if (terminalSyncStatuses.has(syncStatus)) {
      setSyncing(false)
      setProgress(data)
      return
    }

    setSyncing(false)
    setProgress(data)
  }, [])

  const loadSyncProgress = useCallback(async () => {
    if (!deviceUrl) return
    try {
      const data = await getGooglePhotosSyncProgress(deviceUrl)
      applySyncProgress(data)
      return data
    } catch {
      // Ignore progress errors; status loading will surface connection issues.
    }
    return null
  }, [deviceUrl, applySyncProgress])

  const loadStatus = useCallback(async () => {
    if (!deviceUrl) return
    try {
      const data = await getGooglePhotosStatus(deviceUrl)
      setStatus(data)
      setStatusError(null)
      hasLoadedStatusRef.current = true
    } catch (err) {
      // Silently fail on background status checks
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
    if (!syncing) {
      if (progressIntervalRef.current) {
        clearInterval(progressIntervalRef.current)
        progressIntervalRef.current = null
      }
      return
    }
    progressIntervalRef.current = setInterval(async () => {
      try {
        const data = await loadSyncProgress()
        if (terminalSyncStatuses.has(data?.status)) {
          loadStatus()
        }
      } catch {
        // Ignore progress errors
      }
    }, 2000)
    return () => clearInterval(progressIntervalRef.current)
  }, [syncing, loadSyncProgress, loadStatus])

  const handleSync = useCallback(async () => {
    if (!deviceUrl) return
    try {
      await startGooglePhotosSync(deviceUrl)
      setSyncing(true)
      loadSyncProgress()
      toast.success('Sync to Google Photos started')
    } catch (err) {
      const msg = describeError(err)
      if (msg.includes('already in progress')) {
        toast.info('Sync is already running')
        setSyncing(true)
        loadSyncProgress()
      } else {
        toast.error(`Failed to start sync: ${msg}`)
      }
    }
  }, [deviceUrl, loadSyncProgress, toast])

  const handleCancelSync = useCallback(async () => {
    if (!deviceUrl) return
    try {
      await cancelGooglePhotosSync(deviceUrl)
      toast.info('Cancel requested')
      loadSyncProgress()
    } catch (err) {
      toast.error(`Failed to cancel sync: ${describeError(err)}`)
    }
  }, [deviceUrl, loadSyncProgress, toast])

  if (loading) {
    return (
      <div className="flex items-center justify-center py-20">
        <LoadingSpinner size="lg" />
      </div>
    )
  }

  const isConfigured = status?.configured
  const percentage = clampPercent(progress?.percentage)

  return (
    <div className="space-y-6">
      {/* Status Card */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Icon name="cloud" className="w-5 h-5 text-brand-400" />
            <CardTitle>Google Photos Sync</CardTitle>
          </div>
          <div className="flex items-center gap-2">
            <span
              className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${
                isConfigured
                  ? 'bg-emerald-500/15 text-emerald-400'
                  : 'bg-surface-600/30 text-surface-400'
              }`}
            >
              {isConfigured ? 'Configured' : 'Not configured'}
            </span>
          </div>
        </CardHeader>

        <div className="space-y-4">
          <p className="text-sm text-surface-300">
            {isConfigured
              ? 'Google Photos is configured via rclone. Click Sync to copy photos from cloud storage to Google Photos.'
              : 'Google Photos is not configured. Set up a googlephotos remote in rclone and enable it in Settings.'}
          </p>
          {statusError && (
            <div className="rounded-lg border border-amber-500/20 bg-amber-500/10 p-3">
              <p className="text-xs font-medium text-amber-300">Status check failed</p>
              <p className="mt-1 text-xs text-amber-200">{statusError}</p>
            </div>
          )}

          <div className="flex flex-wrap gap-2">
            {isConfigured && (
              <>
                <Button onClick={handleSync} loading={syncing} disabled={syncing}>
                  <Icon name="arrow-up-tray" className="w-4 h-4" />
                  {syncing ? 'Syncing...' : 'Sync to Google Photos'}
                </Button>
                {syncing && (
                  <Button variant="secondary" onClick={handleCancelSync}>
                    <Icon name="x" className="w-4 h-4" />
                    Cancel sync
                  </Button>
                )}
              </>
            )}
          </div>
        </div>
      </Card>

      {/* Sync Progress */}
      {(syncing || (progress && progress.status !== 'idle')) && (
        <Card>
          <CardHeader>
            <CardTitle>Sync Progress</CardTitle>
            {progress?.total_files > 0 && (
              <span className="text-xs font-medium text-surface-300">
                {percentage.toFixed(0)}%
              </span>
            )}
          </CardHeader>
          <div className="space-y-3">
            <div className="flex items-center justify-between text-sm">
              <span className="text-surface-300">Status</span>
              <span className="font-medium text-surface-100">{formatPhase(progress)}</span>
            </div>
            {progress?.total_files > 0 && (
              <div className="flex items-center justify-between text-sm">
                <span className="text-surface-300">Files</span>
                <span className="text-surface-100">
                  {progress.transferred_files || 0} / {progress.total_files}
                </span>
              </div>
            )}
            {progress?.total_files > 0 && progress?.bytes_transferred > 0 && (
              <div className="flex items-center justify-between text-sm">
                <span className="text-surface-300">Data</span>
                <span className="text-surface-100">
                  {formatBytes(progress.bytes_transferred)}
                </span>
              </div>
            )}
            {progress?.speed && (
              <div className="flex items-center justify-between text-sm">
                <span className="text-surface-300">Speed</span>
                <span className="text-surface-100">{progress.speed}</span>
              </div>
            )}
            {progress?.eta && (
              <div className="flex items-center justify-between text-sm">
                <span className="text-surface-300">ETA</span>
                <span className="text-surface-100">{progress.eta}</span>
              </div>
            )}
            {progress?.current_file && (
              <div className="rounded-lg bg-surface-900/50 p-3">
                <div className="flex items-start justify-between gap-3 text-xs">
                  <div className="min-w-0">
                    <p className="font-medium text-surface-200 truncate">{progress.current_file}</p>
                  </div>
                  {progress.current_file_size > 0 && (
                    <span className="shrink-0 text-surface-400">{formatBytes(progress.current_file_size)}</span>
                  )}
                </div>
              </div>
            )}
            {progress?.total_files > 0 && (
              <div className="h-2 w-full rounded-full bg-surface-700 overflow-hidden">
                <div
                  className="h-full rounded-full bg-brand-500 transition-all duration-300"
                  style={{ width: `${percentage}%` }}
                />
              </div>
            )}
            {progress?.error && (
              <div className="rounded-lg border border-rose-500/20 bg-rose-500/10 p-3">
                <p className="text-xs font-medium text-rose-400">Sync Error</p>
                <p className="mt-1 text-xs text-rose-300">{progress.error}</p>
              </div>
            )}
          </div>
        </Card>
      )}

      {/* How it works */}
      {!isConfigured && (
        <Card>
          <CardHeader>
            <CardTitle>How it works</CardTitle>
          </CardHeader>
          <div className="space-y-2 text-sm text-surface-300">
            <p>
              1. Configure a <code>googlephotos</code> remote in rclone via the Settings page.
            </p>
            <p>
              2. Enable Google Photos sync and set the remote name in Settings.
            </p>
            <p>
              3. Click <strong>Sync to Google Photos</strong> to copy photos from your cloud storage to Google Photos.
            </p>
            <p className="text-surface-500 text-xs">
              Photos are uploaded to the main Google Photos stream. Albums are not created automatically.
            </p>
          </div>
        </Card>
      )}
    </div>
  )
}
