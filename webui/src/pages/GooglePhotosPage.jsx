import { useState, useEffect, useCallback, useRef } from 'react'
import { useDevice } from '../DeviceContext.jsx'
import { useToast } from '../components/Toast.jsx'
import { Card, CardHeader, CardTitle } from '../components/Card.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { LoadingSpinner } from '../components/LoadingSpinner.jsx'
import {
  getGooglePhotosStatus,
  startGooglePhotosAuth,
  disconnectGooglePhotos,
  startGooglePhotosSync,
  cancelGooglePhotosSync,
  getGooglePhotosSyncProgress,
  getGooglePhotosSyncHistoryExportUrl,
  getGooglePhotosAlbums,
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

function CopyButton({ text }) {
  const [copied, setCopied] = useState(false)
  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(text)
      setCopied(true)
      setTimeout(() => setCopied(false), 2000)
    } catch {
      // Ignore clipboard errors
    }
  }
  return (
    <button
      onClick={handleCopy}
      className="text-xs text-surface-400 hover:text-surface-200 underline"
    >
      {copied ? 'Copied!' : 'Copy'}
    </button>
  )
}

const activeSyncStatuses = new Set(['listing_cards', 'syncing'])
const terminalSyncStatuses = new Set(['completed', 'error', 'cancelled'])

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

function getOverallPercent(progress) {
  if (progress?.total_bytes > 0) {
    return clampPercent((progress.processed_bytes / progress.total_bytes) * 100)
  }
  if (progress?.total_files > 0) {
    return clampPercent((progress.processed_files / progress.total_files) * 100)
  }
  return 0
}

function formatPhase(progress) {
  return progress?.current_phase || progress?.status?.replace('_', ' ') || 'Syncing'
}

function formatDuration(value) {
  if (!value) return 'n/a'
  return value
}

function formatTimestamp(value) {
  if (!value) return 'n/a'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return 'n/a'
  return date.toLocaleString()
}

export default function GooglePhotosPage() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [status, setStatus] = useState(null)
  const [albums, setAlbums] = useState([])
  const [loading, setLoading] = useState(true)
  const [connecting, setConnecting] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [skipDuplicates, setSkipDuplicates] = useState(true)
  const [progress, setProgress] = useState(null)
  const [lastSyncResult, setLastSyncResult] = useState(null)
  const [albumsLoading, setAlbumsLoading] = useState(false)
  const [statusError, setStatusError] = useState(null)
  const [albumsError, setAlbumsError] = useState(null)
  const progressIntervalRef = useRef(null)
  const statusIntervalRef = useRef(null)
  const hasLoadedStatusRef = useRef(false)

  const applySyncProgress = useCallback((data, { preserveSyncing = false } = {}) => {
    const syncStatus = data?.status

    if (activeSyncStatuses.has(syncStatus)) {
      setSyncing(true)
      setProgress(data)
      return
    }

    if (terminalSyncStatuses.has(syncStatus)) {
      setSyncing(false)
      setLastSyncResult(data)
      setProgress(null)
      return
    }

    if (preserveSyncing) {
      return
    }

    setSyncing(false)
    setProgress(null)
  }, [])

  const loadSyncProgress = useCallback(async (options) => {
    if (!deviceUrl) return
    try {
      const data = await getGooglePhotosSyncProgress(deviceUrl)
      applySyncProgress(data, options)
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
        setStatus({ connected: false, configured: false, albums_count: 0 })
        setStatusError(describeError(err))
        hasLoadedStatusRef.current = true
      }
    }
  }, [deviceUrl])

  const loadAlbums = useCallback(async () => {
    if (!deviceUrl) return
    setAlbumsLoading(true)
    setAlbumsError(null)
    try {
      const data = await getGooglePhotosAlbums(deviceUrl)
      setAlbums(data?.albums || [])
    } catch (err) {
      setAlbumsError(describeError(err))
    } finally {
      setAlbumsLoading(false)
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
    // Poll status every 10 seconds
    statusIntervalRef.current = setInterval(loadStatus, 10000)
    return () => clearInterval(statusIntervalRef.current)
  }, [deviceUrl, loadStatus])

  useEffect(() => {
    if (status?.connected) {
      loadAlbums()
    }
  }, [status?.connected, loadAlbums])

  useEffect(() => {
    if (!syncing) {
      if (progressIntervalRef.current) {
        clearInterval(progressIntervalRef.current)
        progressIntervalRef.current = null
      }
      return
    }
    // Poll progress every 2 seconds while syncing
    progressIntervalRef.current = setInterval(async () => {
      try {
        const data = await loadSyncProgress()
        if (terminalSyncStatuses.has(data?.status)) {
          loadStatus()
          loadAlbums()
        }
      } catch {
        // Ignore progress errors
      }
    }, 2000)
    return () => clearInterval(progressIntervalRef.current)
  }, [syncing, loadSyncProgress, loadStatus, loadAlbums])

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

      // Open popup for OAuth
      const width = 500
      const height = 600
      const left = window.screenX + (window.outerWidth - width) / 2
      const top = window.screenY + (window.outerHeight - height) / 2
      const popup = window.open(
        authUrl,
        'google-photos-auth',
        `width=${width},height=${height},left=${left},top=${top},popup=1`
      )

      if (!popup) {
        toast.error('Popup blocked — please allow popups for this site')
        return
      }

      // Listen for postMessage from popup
      const onMessage = (event) => {
        if (event.data?.type === 'google-photos-connected') {
          window.removeEventListener('message', onMessage)
          toast.success('Google Photos connected!')
          loadStatus()
          loadAlbums()
        }
      }
      window.addEventListener('message', onMessage)

      // Poll for popup close as fallback
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
  }, [deviceUrl, toast, loadStatus, loadAlbums])

  const handleDisconnect = useCallback(async () => {
    if (!deviceUrl) return
    if (!window.confirm('Disconnect Google Photos? This will remove the stored authorization.')) return
    try {
      await disconnectGooglePhotos(deviceUrl)
      toast.success('Google Photos disconnected')
      setStatus({ connected: false, configured: false, albums_count: 0 })
      setAlbums([])
      setAlbumsError(null)
      setStatusError(null)
    } catch (err) {
      toast.error(`Failed to disconnect: ${describeError(err)}`)
    }
  }, [deviceUrl, toast])

  const handleSync = useCallback(async () => {
    if (!deviceUrl) return
    try {
      await startGooglePhotosSync(deviceUrl, { skip_duplicates: skipDuplicates })
      setSyncing(true)
      loadSyncProgress({ preserveSyncing: true })
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
  }, [deviceUrl, loadSyncProgress, skipDuplicates, toast])

  const handleCancelSync = useCallback(async () => {
    if (!deviceUrl) return
    try {
      await cancelGooglePhotosSync(deviceUrl)
      toast.info('Cancel requested')
      loadSyncProgress({ preserveSyncing: true })
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

  const isConnected = status?.connected
  const isConfigured = status?.configured
  const overallPercent = getOverallPercent(progress)
  const currentFilePercent = clampPercent(progress?.current_file_percent)

  return (
    <div className="space-y-6">
      {/* Status Card */}
      <Card>
        <CardHeader>
          <div className="flex items-center gap-2">
            <Icon name="cloud" className="w-5 h-5 text-brand-400" />
            <CardTitle>Google Photos Connection</CardTitle>
          </div>
          <div className="flex items-center gap-2">
            <span
              className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${
                isConnected
                  ? 'bg-emerald-500/15 text-emerald-400'
                  : isConfigured
                    ? 'bg-amber-500/15 text-amber-400'
                    : 'bg-surface-600/30 text-surface-400'
              }`}
            >
              {isConnected ? 'Connected' : isConfigured ? 'Configured' : 'Not connected'}
            </span>
          </div>
        </CardHeader>

        <div className="space-y-4">
          <p className="text-sm text-surface-300">
            {isConnected
              ? `Your Google Photos account is connected. ${albums.length > 0 ? `${albums.length} album(s) found.` : ''}`
              : isConfigured
                ? 'Your Google Photos account is configured but the connection could not be verified. You can still try to sync.'
                : 'Connect your Google Photos account to sync photos from cloud storage.'}
          </p>
          {statusError && (
            <div className="rounded-lg border border-amber-500/20 bg-amber-500/10 p-3">
              <p className="text-xs font-medium text-amber-300">Status check failed</p>
              <p className="mt-1 text-xs text-amber-200">{statusError}</p>
            </div>
          )}
          {(progress?.warning || lastSyncResult?.warning) && (
            <div className="rounded-lg border border-amber-500/20 bg-amber-500/10 p-3">
              <p className="text-xs font-medium text-amber-300">Backend behavior note</p>
              <p className="mt-1 text-xs text-amber-200">{progress?.warning || lastSyncResult?.warning}</p>
            </div>
          )}

          <div className="flex flex-wrap gap-2">
            {!isConfigured ? (
              <Button onClick={handleConnect} loading={connecting} disabled={connecting}>
                <Icon name="lock" className="w-4 h-4" />
                Connect Google Photos
              </Button>
            ) : (
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
                <label className="inline-flex items-center gap-2 rounded-md border border-surface-700 px-3 py-2 text-sm text-surface-200">
                  <input
                    type="checkbox"
                    checked={skipDuplicates}
                    onChange={(event) => setSkipDuplicates(event.target.checked)}
                    disabled={syncing}
                    className="h-4 w-4 accent-brand-500"
                  />
                  Skip duplicates
                </label>
                <Button variant="danger" onClick={handleDisconnect}>
                  <Icon name="x" className="w-4 h-4" />
                  Disconnect
                </Button>
              </>
            )}
          </div>
        </div>
      </Card>

      {/* Sync Progress (live during sync) */}
      {syncing && progress && (
        <Card>
          <CardHeader>
            <CardTitle>Sync Progress</CardTitle>
            {progress.total_files > 0 && (
              <span className="text-xs font-medium text-surface-300">
                {overallPercent.toFixed(0)}%
              </span>
            )}
          </CardHeader>
          <div className="space-y-3">
            <div className="flex items-center justify-between text-sm">
              <span className="text-surface-300">Phase</span>
              <span className="font-medium text-surface-100">{formatPhase(progress)}</span>
            </div>
            {progress.sort_description && (
              <div className="flex items-center justify-between text-sm">
                <span className="text-surface-300">Order</span>
                <span className="text-surface-100">{progress.sort_description}</span>
              </div>
            )}
            {progress.stage_timeline?.length > 0 && (
              <div className="grid gap-2 sm:grid-cols-5">
                {progress.stage_timeline.map((stage) => (
                  <div
                    key={stage.name}
                    className={`rounded-md border px-2.5 py-2 text-xs ${
                      stage.status === 'completed'
                        ? 'border-emerald-500/25 bg-emerald-500/10 text-emerald-300'
                        : stage.status === 'active'
                          ? 'border-brand-500/30 bg-brand-500/10 text-brand-200'
                          : 'border-surface-700 bg-surface-900/40 text-surface-400'
                    }`}
                  >
                    <p className="font-medium">{stage.name.replace('_', ' ')}</p>
                    <p className="mt-0.5">{stage.status}</p>
                  </div>
                ))}
              </div>
            )}
            {progress.total_cards > 0 && (
              <div className="flex items-center justify-between text-sm">
                <span className="text-surface-300">Cards</span>
                <span className="text-surface-100">
                  {progress.current_card} / {progress.total_cards}
                  {progress.current_card_id && ` (${progress.current_card_id})`}
                </span>
              </div>
            )}
            {progress.total_files > 0 && (
              <div className="flex items-center justify-between text-sm">
                <span className="text-surface-300">Files</span>
                <span className="text-surface-100">
                  {progress.processed_files} / {progress.total_files}
                  {progress.current_card_files > 0 && progress.current_file_index > 0
                    ? ` (${progress.current_file_index}/${progress.current_card_files} on this card)`
                    : ''}
                </span>
              </div>
            )}
            {progress.total_bytes > 0 && (
              <div className="flex items-center justify-between text-sm">
                <span className="text-surface-300">Data</span>
                <span className="text-surface-100">
                  {formatBytes(progress.processed_bytes)} / {formatBytes(progress.total_bytes)}
                </span>
              </div>
            )}
            {progress.current_file && (
              <div className="rounded-lg bg-surface-900/50 p-3">
                <div className="flex items-start justify-between gap-3 text-xs">
                  <div className="min-w-0">
                    <p className="font-medium text-surface-200 truncate">{progress.current_file}</p>
                    {progress.current_file_path && (
                      <p className="mt-0.5 text-surface-500 truncate">{progress.current_file_path}</p>
                    )}
                  </div>
                  {progress.current_file_size > 0 && (
                    <span className="shrink-0 text-surface-400">{formatBytes(progress.current_file_size)}</span>
                  )}
                </div>
                {progress.current_file_size > 0 && (
                  <div className="mt-2">
                    <div className="mb-1 flex items-center justify-between text-xs text-surface-500">
                      <span>{formatBytes(progress.current_file_bytes_uploaded || 0)} uploaded</span>
                      <span>{currentFilePercent.toFixed(0)}%</span>
                    </div>
                    <div className="h-1.5 w-full rounded-full bg-surface-700 overflow-hidden">
                      <div
                        className="h-full rounded-full bg-emerald-500 transition-all duration-300"
                        style={{ width: `${currentFilePercent}%` }}
                      />
                    </div>
                  </div>
                )}
              </div>
            )}
            {(progress.uploaded_files > 0 || progress.skipped_files > 0 || progress.failed_files > 0) && (
              <div className="flex flex-wrap items-center gap-4 text-xs text-surface-400">
                <span className="text-emerald-400">{progress.uploaded_files} uploaded</span>
                {progress.skipped_files > 0 && <span className="text-amber-400">{progress.skipped_files} skipped</span>}
                {progress.duplicates_skipped > 0 && <span className="text-amber-300">{progress.duplicates_skipped} duplicates skipped</span>}
                {progress.failed_files > 0 && <span className="text-rose-400">{progress.failed_files} failed</span>}
                {progress.batch_pending_files > 0 && <span>{progress.batch_pending_files} waiting for album</span>}
              </div>
            )}
            {progress.card_progress?.length > 0 && (
              <div className="space-y-2">
                {progress.card_progress.map((card) => (
                  <div key={card.card_id} className="rounded-md bg-surface-900/50 px-3 py-2 text-xs text-surface-300">
                    Card {card.card_id}: {card.processed}/{card.total_files} files, {card.failed} failed
                    {card.queue_depth > 0 ? `, ${card.queue_depth} queued` : ''}
                  </div>
                ))}
              </div>
            )}
            {progress.retry_status && (
              <div className="rounded-lg border border-amber-500/20 bg-amber-500/10 p-3 text-xs text-amber-200">
                Retry {progress.retry_status.count}; next attempt at {formatTimestamp(progress.retry_status.next_retry_at)}.
                {progress.retry_status.reason ? ` ${progress.retry_status.reason}` : ''}
              </div>
            )}
            {progress.backend_metrics && (
              <div className="grid gap-2 text-xs text-surface-400 sm:grid-cols-3">
                <span>{progress.backend_metrics.upload_workers || 0} upload workers</span>
                <span>{progress.backend_metrics.queue_depth || 0} queued</span>
                <span>{formatBytes(progress.backend_metrics.upload_bytes_per_sec || 0)}/s</span>
              </div>
            )}
            {progress.total_files > 0 && (
              <div className="h-2 w-full rounded-full bg-surface-700 overflow-hidden">
                <div
                  className="h-full rounded-full bg-brand-500 transition-all duration-300"
                  style={{ width: `${overallPercent}%` }}
                />
              </div>
            )}
            {progress.error && (
              <div className="rounded-lg border border-rose-500/20 bg-rose-500/10 p-3">
                <div className="flex items-center justify-between">
                  <p className="text-xs font-medium text-rose-400">Sync Error</p>
                  <CopyButton text={progress.error} />
                </div>
                <p className="mt-1 text-xs text-rose-300">{progress.error}</p>
              </div>
            )}
            {progress.debug_details?.length > 0 && (
              <details className="rounded-lg border border-surface-700 bg-surface-900/40 p-3">
                <summary className="cursor-pointer text-xs font-medium text-surface-300">Debug details</summary>
                <pre className="mt-2 whitespace-pre-wrap text-xs text-surface-400">{progress.debug_details.join('\n')}</pre>
              </details>
            )}
          </div>
        </Card>
      )}

      {/* Last Sync Result (shown after sync ends, especially for errors) */}
      {lastSyncResult && !syncing && (
        <Card>
          <CardHeader>
            <CardTitle>Last Sync Result</CardTitle>
            <span
              className={`inline-flex items-center rounded-full px-2.5 py-0.5 text-xs font-medium ${
                lastSyncResult.status === 'completed'
                  ? 'bg-emerald-500/15 text-emerald-400'
                  : lastSyncResult.status === 'error'
                    ? 'bg-rose-500/15 text-rose-400'
                    : 'bg-amber-500/15 text-amber-400'
              }`}
            >
              {lastSyncResult.status?.replace('_', ' ') || 'unknown'}
            </span>
          </CardHeader>
          <div className="space-y-3">
            <div className="flex items-center gap-4 text-sm">
              <span className="text-emerald-400">{lastSyncResult.uploaded_files || 0} uploaded</span>
              {lastSyncResult.skipped_files > 0 && <span className="text-amber-400">{lastSyncResult.skipped_files} skipped</span>}
              {lastSyncResult.failed_files > 0 && <span className="text-rose-400">{lastSyncResult.failed_files} failed</span>}
            </div>
            {lastSyncResult.total_cards > 0 && (
              <div className="text-xs text-surface-400">
                Processed {lastSyncResult.total_cards} card{lastSyncResult.total_cards !== 1 ? 's' : ''}
              </div>
            )}
            {lastSyncResult.last_successful_sync && (
              <div className="text-xs text-surface-400">
                Last successful sync: {formatTimestamp(lastSyncResult.last_successful_sync.completed_at)}
                {' '}({formatDuration(lastSyncResult.last_successful_sync.duration)})
              </div>
            )}
            {lastSyncResult.cancellation_summary && (
              <div className="text-xs text-amber-300">{lastSyncResult.cancellation_summary}</div>
            )}
            {lastSyncResult.error && (
              <div className="rounded-lg border border-rose-500/20 bg-rose-500/10 p-3">
                <div className="flex items-center justify-between">
                  <p className="text-xs font-medium text-rose-400">Error</p>
                  <CopyButton text={lastSyncResult.error} />
                </div>
                <p className="mt-1 text-xs text-rose-300">{lastSyncResult.error}</p>
              </div>
            )}
            {lastSyncResult.card_errors && lastSyncResult.card_errors.length > 0 && (
              <div className="space-y-2">
                <p className="text-xs font-medium text-surface-300">Failed Cards</p>
                {lastSyncResult.card_errors.map((ce) => (
                  <div
                    key={ce.card_id}
                    className="rounded-lg border border-rose-500/15 bg-rose-500/5 p-2.5"
                  >
                    <div className="flex items-center justify-between">
                      <p className="text-xs font-medium text-surface-200">Card {ce.card_id}</p>
                      <CopyButton text={`Card ${ce.card_id}: ${ce.error}`} />
                    </div>
                    <p className="mt-0.5 text-xs text-rose-400">{ce.error}</p>
                  </div>
                ))}
              </div>
            )}
            <Button variant="secondary" size="sm" onClick={() => setLastSyncResult(null)}>
              Dismiss
            </Button>
          </div>
        </Card>
      )}

      {(progress?.history?.length > 0 || lastSyncResult?.history?.length > 0) && (
        <Card>
          <CardHeader>
            <CardTitle>Sync History</CardTitle>
            <a
              href={getGooglePhotosSyncHistoryExportUrl(deviceUrl)}
              className="text-xs text-brand-400 hover:text-brand-300"
            >
              Export
            </a>
          </CardHeader>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-xs">
              <thead className="text-surface-400">
                <tr>
                  <th className="py-2 pr-3 font-medium">Time</th>
                  <th className="py-2 pr-3 font-medium">Card</th>
                  <th className="py-2 pr-3 font-medium">Uploaded</th>
                  <th className="py-2 pr-3 font-medium">Skipped</th>
                  <th className="py-2 pr-3 font-medium">Failed</th>
                  <th className="py-2 pr-3 font-medium">Bytes</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-surface-800 text-surface-200">
                {(progress?.history || lastSyncResult?.history || []).slice(0, 8).map((entry) => (
                  <tr key={`${entry.started_at}-${entry.status}`}>
                    <td className="py-2 pr-3">{formatTimestamp(entry.completed_at || entry.started_at)}</td>
                    <td className="py-2 pr-3">{entry.card_id || 'all'}</td>
                    <td className="py-2 pr-3 text-emerald-400">{entry.uploaded_files || 0}</td>
                    <td className="py-2 pr-3 text-amber-400">{entry.skipped_files || 0}</td>
                    <td className="py-2 pr-3 text-rose-400">{entry.failed_files || 0}</td>
                    <td className="py-2 pr-3">{formatBytes(entry.processed_bytes || 0)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </Card>
      )}

      {/* Albums */}
      {isConnected && (
        <Card>
          <CardHeader>
            <div className="flex items-center gap-2">
              <Icon name="image" className="w-5 h-5 text-brand-400" />
              <CardTitle>Albums</CardTitle>
            </div>
            {albumsLoading && <LoadingSpinner size="sm" />}
          </CardHeader>
          {albums.length === 0 ? (
            albumsError ? (
              <div className="rounded-lg border border-rose-500/20 bg-rose-500/10 p-3">
                <div className="flex items-center justify-between">
                  <p className="text-xs font-medium text-rose-400">Album list failed</p>
                  <CopyButton text={albumsError} />
                </div>
                <p className="mt-1 text-xs text-rose-300">{albumsError}</p>
              </div>
            ) : (
              <p className="text-sm text-surface-400">
                {albumsLoading ? 'Loading albums...' : 'No albums yet. Run a sync to create albums.'}
              </p>
            )
          ) : (
            <div className="space-y-2">
              {albums.map((album) => (
                <div
                  key={album.id}
                  className="flex items-center justify-between rounded-lg bg-surface-900/50 px-3 py-2.5"
                >
                  <div className="min-w-0">
                    <p className="text-sm font-medium text-surface-100 truncate">{album.title}</p>
                    <p className="text-xs text-surface-500">
                      {album.mediaItemsCount || '0'} items
                    </p>
                  </div>
                  {album.productUrl && (
                    <a
                      href={album.productUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="shrink-0 text-xs text-brand-400 hover:text-brand-300"
                    >
                      Open
                    </a>
                  )}
                </div>
              ))}
            </div>
          )}
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
              1. Click <strong>Connect Google Photos</strong> to authorize this device.
            </p>
            <p>
              2. Once connected, click <strong>Sync to Google Photos</strong> to upload all existing photos from your cloud storage (B2).
            </p>
            <p>
              3. Photos are organized into albums by card ID (e.g., &quot;Card abc123&quot;).
            </p>
            <p className="text-surface-500 text-xs">
              RAW files are automatically filtered out. Only JPG, PNG, HEIC, MP4, MOV, and other common photo/video formats are uploaded.
            </p>
          </div>
        </Card>
      )}
    </div>
  )
}
