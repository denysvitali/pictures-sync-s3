import { useState, useEffect, useCallback, useRef } from 'react'
import { motion, AnimatePresence, useReducedMotion } from 'framer-motion'
import { useDevice } from '../DeviceContext.jsx'
import { useToast } from '../components/Toast.jsx'
import { Card, CardHeader, CardTitle } from '../components/Card.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { StatusBadge } from '../components/StatusBadge.jsx'
import { LoadingSpinner } from '../components/LoadingSpinner.jsx'
import { EmptyState } from '../components/EmptyState.jsx'
import {
  getGooglePhotosStatus,
  startGooglePhotosAuth,
  disconnectGooglePhotos,
  startGooglePhotosSync,
  cancelGooglePhotosSync,
  getGooglePhotosSyncProgress,
  getGooglePhotosCards,
  getGooglePhotosCardSummary,
  getGooglePhotosAlbums,
  getRemoteThumbnailUrl,
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

  

const TONE_RING = {
  success: 'from-success/25 to-success/5 text-success ring-success/30',
  warning: 'from-warning/25 to-warning/5 text-warning ring-warning/30',
  danger: 'from-danger/25 to-danger/5 text-danger ring-danger/30',
  info: 'from-info/25 to-info/5 text-info ring-info/30',
  neutral: 'from-surface-600/30 to-surface-700/10 text-surface-300 ring-surface-600/40',
}

function StatusPanel({ status, statusError }) {
  const summary = formatStatusSummary(status)
  const hint = formatStatusHint(status)
  const badge = connectionStatusMeta(status, Boolean(statusError))
  const ring = TONE_RING[badge.tone] || TONE_RING.neutral

  return (
    <Card glow>
      <div className="flex items-start gap-4">
        <div className={`relative flex h-14 w-14 shrink-0 items-center justify-center rounded-2xl bg-gradient-to-br ring-1 ${ring}`}>
          {badge.pulse && (
            <span className="pulse-ring absolute inset-0 rounded-2xl bg-current opacity-20" aria-hidden="true" />
          )}
          <Icon name="cloud" className="relative h-7 w-7" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2">
            <h3 className="text-base font-semibold tracking-tight text-surface-100">Google Photos</h3>
            <StatusBadge variant={badge.tone} pulse={badge.pulse}>
              <Icon name={badge.icon} className="h-3.5 w-3.5" />
              {badge.label}
            </StatusBadge>
          </div>
          <p className="mt-0.5 text-sm font-medium text-surface-300">{summary}</p>
          <p className="mt-2 text-sm leading-relaxed text-surface-400">{hint}</p>

          {statusError && (
            <div className="mt-3 rounded-lg border border-amber-500/30 bg-amber-500/10 p-3">
              <p className="flex items-center gap-1.5 text-xs font-medium text-amber-300">
                <Icon name="alert-triangle" className="h-3.5 w-3.5" />
                Status check issue
              </p>
              <p className="mt-1 text-xs text-amber-200">{statusError}</p>
            </div>
          )}
        </div>
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
        <div className="flex items-center gap-2">
          <Icon name="zap" className="h-5 w-5 text-brand-400" />
          <CardTitle>Actions</CardTitle>
        </div>
      </CardHeader>

      <div className="space-y-3">
        <p className="text-sm leading-relaxed text-surface-300">
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
              <Button onClick={() => onSync(false)} loading={syncing} disabled={disablePrimary}>
                <Icon name="arrow-up-tray" className="w-4 h-4" />
                {syncing ? 'Starting…' : primaryMessage}
              </Button>

              <Button variant="secondary" onClick={() => onSync(true)} disabled={disablePrimary}>
                <Icon name="arrow-path" className="w-4 h-4" />
                Force re-sync
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

function StatTile({ icon, label, value, accent = false }) {
  return (
    <div className="rounded-lg border border-surface-700/50 bg-surface-900/50 p-2.5">
      <p className="flex items-center gap-1.5 text-[11px] text-surface-400">
        <Icon name={icon} className="h-3.5 w-3.5 text-surface-500" />
        {label}
      </p>
      <p className={`mt-1 truncate text-sm font-semibold tabular-nums ${accent ? 'text-brand-300' : 'text-surface-100'}`}>
        {value}
      </p>
    </div>
  )
}

function ProgressBar({ percentage, indeterminate = false, tone = 'brand', reduceMotion = false }) {
  const barColor =
    tone === 'danger' ? 'bg-danger' : tone === 'success' ? 'bg-success' : 'bg-gradient-to-r from-brand-500 to-brand-400'
  if (indeterminate) {
    return (
      <div className="h-2 overflow-hidden rounded-full bg-surface-800">
        <div className={`h-full w-1/3 rounded-full ${barColor} animate-pulse`} style={{ marginLeft: '20%' }} />
      </div>
    )
  }
  return (
    <div
      className="h-2 overflow-hidden rounded-full bg-surface-800"
      role="progressbar"
      aria-valuenow={Math.round(percentage)}
      aria-valuemin={0}
      aria-valuemax={100}
    >
      <motion.div
        className={`h-full rounded-full ${barColor} shadow-sm shadow-brand-500/20`}
        initial={false}
        animate={{ width: `${percentage}%` }}
        transition={reduceMotion ? { duration: 0 } : { type: 'spring', stiffness: 120, damping: 24 }}
      />
    </div>
  )
}

function ProgressPanel({ progress, reduceMotion }) {
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
          <Icon name="arrow-up-tray" className="h-5 w-5 text-brand-400" />
          <CardTitle>Sync progress</CardTitle>
          <StatusBadge variant={state.tone} pulse={state.pulse}>
            <Icon name={state.icon} className="h-3.5 w-3.5" />
            {state.label}
          </StatusBadge>
        </div>
        {percentage > 0 ? <span className="text-sm font-semibold tabular-nums text-brand-300">{percentage.toFixed(0)}%</span> : null}
      </CardHeader>

      <div className="space-y-4">
        {hasFiles && (
          <ProgressBar
            percentage={percentage}
            tone={progress.error ? 'danger' : isInProgress ? 'brand' : 'success'}
            reduceMotion={reduceMotion}
          />
        )}

        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3">
          <StatTile icon="image" label="Files" value={`${progress.transferred_files || 0} / ${hasFiles ? progress.total_files : '—'}`} />
          <StatTile icon="cloud" label="Data synced" value={formatBytes(progress.bytes_transferred)} />
          <StatTile icon="zap" label="Speed" value={hasSpeed ? formatSpeed(progress.speed) : '--'} accent={hasSpeed} />
          <StatTile icon="clock" label="ETA" value={formatDuration(progress.eta)} />
          <StatTile icon="activity" label="Phase" value={formatPhase(progress)} />
          <StatTile icon={isInProgress ? 'arrow-path' : 'check'} label="Mode" value={isInProgress ? 'Active' : 'Idle'} />
        </div>

        {progress.current_file && (
          <div className="rounded-lg border border-surface-700/60 bg-surface-900/40 p-3">
            <p className="flex items-center gap-1.5 text-xs text-surface-400">
              <Icon name="arrow-up-tray" className="h-3.5 w-3.5 text-brand-400" />
              Uploading
            </p>
            <p className="mt-1 truncate text-sm text-surface-100">{progress.current_file}</p>
            {progress.current_file_size > 0 && (
              <p className="mt-1 text-xs tabular-nums text-surface-500">{formatBytes(progress.current_file_size)}</p>
            )}
          </div>
        )}

        {progress.error && (
          <div className="rounded-lg border border-rose-500/20 bg-rose-500/10 p-3">
            <p className="flex items-center gap-1.5 text-xs font-medium text-rose-400">
              <Icon name="exclamation-triangle" className="h-3.5 w-3.5" />
              Sync Error
            </p>
            <p className="mt-1 break-words text-xs text-rose-300">{progress.error}</p>
          </div>
        )}
      </div>
    </Card>
  )
}

function SyncStartingPanel({ reduceMotion }) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <Icon name="arrow-path" className="h-5 w-5 text-brand-400 motion-safe:animate-spin" />
          <CardTitle>Sync starting</CardTitle>
        </div>
      </CardHeader>
      <div className="space-y-3">
        <p className="text-sm text-surface-300">
          Starting the Google Photos sync run. Status will update here as soon as the backend reports progress.
        </p>
        <ProgressBar indeterminate reduceMotion={reduceMotion} />
      </div>
    </Card>
  )
}

function PreviewThumb({ path, deviceUrl }) {
  const [errored, setErrored] = useState(false)
  const name = String(path || '').split('/').pop()
  return (
    <div
      className="relative aspect-square overflow-hidden rounded-md border border-surface-700/50 bg-surface-900"
      title={name}
    >
      {errored ? (
        <div className="flex h-full w-full items-center justify-center">
          <Icon name="image" className="h-4 w-4 text-surface-600" />
        </div>
      ) : (
        <img
          src={getRemoteThumbnailUrl(deviceUrl, path)}
          alt={name || 'preview'}
          loading="lazy"
          decoding="async"
          className="h-full w-full object-cover"
          onError={() => setErrored(true)}
        />
      )}
    </div>
  )
}

// PreviewGrid shows up to 4 thumbnails sourced from B2 (the sync source) so the
// user can see what each card will sync.
function PreviewGrid({ summary, deviceUrl }) {
  const tiles = (
    <div className="mt-3 grid grid-cols-4 gap-1.5">
      {Array.from({ length: 4 }).map((_, i) => (
        <div key={i} className="aspect-square animate-shimmer rounded-md" />
      ))}
    </div>
  )

  if (!summary || summary.loading) return tiles
  if (summary.error) {
    return <p className="mt-3 text-xs text-surface-500">Preview unavailable</p>
  }
  if (!summary.preview || summary.preview.length === 0) {
    return <p className="mt-3 text-xs text-surface-500">No photos to preview</p>
  }

  return (
    <div className="mt-3 grid grid-cols-4 gap-1.5">
      {summary.preview.slice(0, 4).map((p) => (
        <PreviewThumb key={p} path={p} deviceUrl={deviceUrl} />
      ))}
    </div>
  )
}

function CardsHeader({ count }) {
  return (
    <CardHeader>
      <div className="flex items-center gap-2">
        <Icon name="folder" className="h-5 w-5 text-brand-400" />
        <CardTitle>Cards to sync</CardTitle>
      </div>
      {count > 0 && (
        <StatusBadge variant="neutral">
          {count} {count === 1 ? 'card' : 'cards'}
        </StatusBadge>
      )}
    </CardHeader>
  )
}

function CardsPanel({
  cards,
  loading,
  summaries,
  deviceUrl,
  onClear,
  clearingId,
  clearProgress,
  onSort,
  sortingId,
  sortProgress,
  reduceMotion,
  selectedNames,
  onToggleCard,
  onToggleAll,
  onSyncSelected,
  syncing,
}) {
  if (loading) {
    return (
      <Card>
        <CardsHeader count={0} />
        <ul className="space-y-2" aria-hidden="true">
          {Array.from({ length: 3 }).map((_, i) => (
            <li key={i} className="flex items-center gap-3 rounded-lg border border-surface-700/50 bg-surface-900/40 p-3">
              <div className="h-10 w-10 shrink-0 animate-shimmer rounded-lg" />
              <div className="flex-1 space-y-2">
                <div className="h-3 w-1/3 animate-shimmer rounded" />
                <div className="h-2 w-1/5 animate-shimmer rounded" />
              </div>
              <div className="h-8 w-16 animate-shimmer rounded" />
            </li>
          ))}
        </ul>
      </Card>
    )
  }

  if (!cards || cards.length === 0) {
    return (
      <Card>
        <CardsHeader count={0} />
        <EmptyState
          compact
          icon="folder"
          title="No cards found"
          description="No card folders were found on your B2 remote. Back up an SD card first, then sync it to Google Photos."
        />
      </Card>
    )
  }

  const selectedCount = cards.reduce((n, c) => (selectedNames?.has(c.name) ? n + 1 : n), 0)
  const allSelected = cards.length > 0 && selectedCount === cards.length

  return (
    <Card>
      <CardsHeader count={cards.length} />
      <div className="space-y-3">
        <p className="text-xs text-surface-500">
          These are the card folders on your B2 remote — the source of the sync. Tick the cards you want to upload to
          Google Photos, preview their photos, and track how many files have been transferred so far.
        </p>

        <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg border border-surface-700/50 bg-surface-900/40 p-2.5">
          <label className="flex cursor-pointer select-none items-center gap-2 text-sm text-surface-200">
            <input
              type="checkbox"
              className="h-4 w-4 cursor-pointer rounded border-surface-600 bg-surface-800 accent-brand-500"
              checked={allSelected}
              ref={(el) => {
                if (el) el.indeterminate = selectedCount > 0 && !allSelected
              }}
              onChange={(e) => onToggleAll(e.target.checked)}
            />
            <span>
              {selectedCount > 0 ? `${selectedCount} selected` : 'Select all'}
            </span>
          </label>
          <Button
            size="sm"
            loading={syncing}
            disabled={syncing || selectedCount === 0}
            onClick={onSyncSelected}
          >
            <Icon name="arrow-up-tray" className="h-4 w-4" />
            {syncing ? 'Starting…' : `Sync selected${selectedCount > 0 ? ` (${selectedCount})` : ''}`}
          </Button>
        </div>

        <ul className="space-y-2">
          {cards.map((card) => {
            const albumId = card.album_id || ''
            const hasAlbum = albumId !== ''
            const isSelected = Boolean(selectedNames?.has(card.name))
            const summary = summaries?.[card.name]
            const totalFiles = Number(summary?.total_files || 0)
            const transferredFiles = Number(summary?.transferred_files || 0)
            const transferPct = totalFiles > 0 ? clampPercent((transferredFiles / totalFiles) * 100) : 0
            const fullyTransferred = totalFiles > 0 && transferredFiles >= totalFiles
            const summaryLoading = !summary || summary.loading

            const progress = clearProgress?.[albumId]
            const isClearing = clearingId === albumId && hasAlbum
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

            const sp = sortProgress?.[albumId]
            const isSorting = sortingId === albumId && hasAlbum
            const sortActive = isSorting && sp?.status !== 'completed' && sp?.status !== 'error'
            const sortStatusLabel = (() => {
              switch (sp?.status) {
                case 'listing':
                  return 'Listing...'
                case 'sorting':
                  return 'Sorting...'
                case 'creating-album':
                  return 'Creating album...'
                case 'adding':
                  return Number(sp?.total_items) > 0
                    ? `Adding ${sp?.added_items || 0}/${sp.total_items}...`
                    : 'Adding...'
                case 'deleting-old':
                  return 'Finishing...'
                default:
                  return 'Sorting...'
              }
            })()
            const sortLabel = sortActive ? sortStatusLabel : 'Sort'
            const hasSortProgress = sortActive && Number(sp?.total_items) > 0
            const sortError = sp?.status === 'error' ? (sp?.error || 'Failed to sort album') : ''
            const sortInaccessible = sp?.status === 'error' ? Number(sp?.inaccessible || 0) : 0
            const sortAccessible = Number(sp?.total_items || 0)
            const sortReported = sortAccessible + sortInaccessible

            const clearPct = Number(progress?.total_items) > 0
              ? clampPercent((Number(progress?.removed_items || 0) / Number(progress.total_items)) * 100)
              : 0
            const sortPct = hasSortProgress
              ? clampPercent((Number(sp?.added_items || 0) / Number(sp.total_items)) * 100)
              : 0
            const busy = isSorting || isClearing
            const initial = (card.name || '?').trim().charAt(0).toUpperCase() || '?'

            return (
              <li
                key={card.name}
                className={`overflow-hidden rounded-lg border bg-surface-900/40 p-3 transition-colors ${
                  busy
                    ? 'border-brand-400/40'
                    : isSelected
                      ? 'border-brand-500/40 bg-brand-500/[0.04]'
                      : 'border-surface-700/60'
                }`}
              >
                <div className="flex items-center justify-between gap-3">
                  <div className="flex min-w-0 items-center gap-3">
                    <input
                      type="checkbox"
                      className="h-4 w-4 shrink-0 cursor-pointer rounded border-surface-600 bg-surface-800 accent-brand-500"
                      checked={isSelected}
                      onChange={(e) => onToggleCard(card.name, e.target.checked)}
                      aria-label={`Select card ${card.name}`}
                    />
                    <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-lg border border-brand-400/20 bg-gradient-to-br from-brand-500/20 to-brand-700/10 text-sm font-bold text-brand-300">
                      {initial}
                    </div>
                    <div className="min-w-0">
                      <p className="truncate text-sm font-medium text-surface-100">{card.name}</p>
                      <p className="flex items-center gap-1 text-xs text-surface-500">
                        <Icon name="image" className="h-3 w-3" />
                        {summaryLoading
                          ? 'Counting…'
                          : totalFiles > 0
                            ? `${totalFiles} file${totalFiles === 1 ? '' : 's'} · ${formatBytes(summary?.total_bytes)}`
                            : 'No photos'}
                      </p>
                    </div>
                  </div>
                  <div className="flex shrink-0 items-center gap-2">
                    <Button
                      variant="secondary"
                      size="sm"
                      loading={isSorting}
                      disabled={!hasAlbum || isSorting || isClearing}
                      title={hasAlbum ? 'Sort the Google Photos album by shoot time' : 'Sync this card first to enable sorting'}
                      onClick={() => onSort(albumId, card.name)}
                    >
                      <Icon name="sort" className="h-4 w-4" />
                      {sortLabel}
                    </Button>
                    <Button
                      variant="danger"
                      size="sm"
                      loading={isClearing}
                      disabled={!hasAlbum || isClearing || isSorting}
                      title={hasAlbum ? 'Remove all photos this app uploaded to the album' : 'Sync this card first to enable clearing'}
                      onClick={() => onClear(albumId, card.name)}
                    >
                      <Icon name="trash" className="h-4 w-4" />
                      {clearLabel}
                    </Button>
                  </div>
                </div>

                {!summaryLoading && totalFiles > 0 && (
                  <div className="mt-3 space-y-1.5">
                    <div className="flex items-center justify-between text-xs text-surface-400">
                      <span className="flex items-center gap-1">
                        <Icon name={fullyTransferred ? 'check' : 'cloud'} className="h-3 w-3" />
                        {fullyTransferred ? 'Transferred to Google Photos' : 'Transferred so far'}
                      </span>
                      <span className="tabular-nums">{transferredFiles}/{totalFiles}</span>
                    </div>
                    <ProgressBar
                      percentage={transferPct}
                      tone={fullyTransferred ? 'success' : 'brand'}
                      reduceMotion={reduceMotion}
                    />
                  </div>
                )}

                <PreviewGrid summary={summary} deviceUrl={deviceUrl} />

                <AnimatePresence initial={false}>
                  {hasProgressCounts && (
                    <motion.div
                      key="clear-progress"
                      className="mt-3 space-y-1.5"
                      initial={reduceMotion ? false : { opacity: 0, height: 0 }}
                      animate={{ opacity: 1, height: 'auto' }}
                      exit={reduceMotion ? { opacity: 0 } : { opacity: 0, height: 0 }}
                    >
                      <div className="flex items-center justify-between text-xs text-surface-400">
                        <span>Removing photos</span>
                        <span className="tabular-nums">
                          {progress?.removed_items || 0}
                          {Number(progress?.total_items) > 0 ? `/${progress.total_items}` : ''}
                        </span>
                      </div>
                      {Number(progress?.total_items) > 0 && (
                        <ProgressBar percentage={clearPct} tone="danger" reduceMotion={reduceMotion} />
                      )}
                    </motion.div>
                  )}
                  {hasSortProgress && (
                    <motion.div
                      key="sort-progress"
                      className="mt-3 space-y-1.5"
                      initial={reduceMotion ? false : { opacity: 0, height: 0 }}
                      animate={{ opacity: 1, height: 'auto' }}
                      exit={reduceMotion ? { opacity: 0 } : { opacity: 0, height: 0 }}
                    >
                      <div className="flex items-center justify-between text-xs text-surface-400">
                        <span>{sp?.status === 'adding' ? 'Adding to album' : sortStatusLabel}</span>
                        {sp?.status === 'adding' && (
                          <span className="tabular-nums">{sp?.added_items || 0}/{sp.total_items}</span>
                        )}
                      </div>
                      <ProgressBar percentage={sortPct} tone="brand" reduceMotion={reduceMotion} />
                    </motion.div>
                  )}
                </AnimatePresence>

                {sortError && sortInaccessible > 0 && (
                  <div className="mt-3 rounded-lg border border-amber-500/20 bg-amber-500/10 p-3">
                    <p className="flex items-center gap-1.5 text-xs font-medium text-amber-300">
                      <Icon name="exclamation-triangle" className="h-3.5 w-3.5" />
                      Sort skipped — album left untouched
                    </p>
                    <p className="mt-1 text-xs text-amber-200">
                      Only {sortAccessible} of {sortReported} items are visible to this app.
                      The other {sortInaccessible} were uploaded by a different account or client
                      (e.g. an rclone remote) and can&apos;t be sorted from here. Re-upload them
                      through this app, then try again.
                    </p>
                  </div>
                )}
                {sortError && sortInaccessible === 0 && (
                  <div className="mt-3 rounded-lg border border-rose-500/20 bg-rose-500/10 p-3">
                    <p className="flex items-center gap-1.5 text-xs font-medium text-rose-400">
                      <Icon name="exclamation-triangle" className="h-3.5 w-3.5" />
                      Failed to sort album
                    </p>
                    <p className="mt-1 whitespace-pre-wrap break-words text-xs text-rose-300">{sortError}</p>
                  </div>
                )}
                {isError && (
                  <div className="mt-3 rounded-lg border border-rose-500/20 bg-rose-500/10 p-3">
                    <p className="flex items-center gap-1.5 text-xs font-medium text-rose-400">
                      <Icon name="exclamation-triangle" className="h-3.5 w-3.5" />
                      Failed to clear album
                    </p>
                    <p className="mt-1 whitespace-pre-wrap break-words text-xs text-rose-300">{errorMessage}</p>
                    {showPermissionHint && (
                      <p className="mt-2 break-words text-xs text-rose-200/80">
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

const WORKFLOW_STEPS = [
  <>Configure <strong className="text-surface-200">OAuth client ID</strong> and <strong className="text-surface-200">client secret</strong> in Settings.</>,
  <>Click <strong className="text-surface-200">Connect Google Photos</strong> and complete the authorization flow.</>,
  <>Start a sync. Each card folder is uploaded under album-style destinations created by rclone.</>,
  <>Monitor live progress and cancel if needed.</>,
]

function InfoPanel() {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center gap-2">
          <Icon name="sparkles" className="h-5 w-5 text-brand-400" />
          <CardTitle>How it works</CardTitle>
        </div>
      </CardHeader>
      <ol className="space-y-2.5">
        {WORKFLOW_STEPS.map((step, i) => (
          <li key={i} className="flex items-start gap-3">
            <span className="flex h-6 w-6 shrink-0 items-center justify-center rounded-full bg-brand-500/15 text-xs font-semibold text-brand-300">
              {i + 1}
            </span>
            <p className="pt-0.5 text-sm leading-relaxed text-surface-300">{step}</p>
          </li>
        ))}
      </ol>
      <p className="mt-3 border-t border-surface-700/50 pt-3 text-xs text-surface-500">
        Supported formats are filtered by the backend sync engine before upload.
      </p>
    </Card>
  )
}

export default function GooglePhotosPage() {
  const { deviceUrl } = useDevice()
  const toast = useToast()
  const reduceMotion = useReducedMotion() ?? false

  const [status, setStatus] = useState(null)
  const [loading, setLoading] = useState(true)
  const [connecting, setConnecting] = useState(false)
  const [syncing, setSyncing] = useState(false)
  const [disconnecting, setDisconnecting] = useState(false)
  const [progress, setProgress] = useState(null)
  const [statusError, setStatusError] = useState(null)
  const [cards, setCards] = useState(null)
  const [cardsLoading, setCardsLoading] = useState(false)
  const [cardSummaries, setCardSummaries] = useState({})
  const [clearingAlbumId, setClearingAlbumId] = useState(null)
  const [clearProgress, setClearProgress] = useState({})
  const [sortingAlbumId, setSortingAlbumId] = useState(null)
  const [sortProgress, setSortProgress] = useState({})
  const [selectedCardNames, setSelectedCardNames] = useState(() => new Set())
  const knownCardNamesRef = useRef(new Set())
  const summaryFetchedRef = useRef(new Set())
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

  // List the B2 source cards (the sync source). Each carries its matched Google
  // Photos album id (when already synced) so sort/clear can target the album.
  const loadCards = useCallback(async () => {
    if (!deviceUrl) return
    setCardsLoading(true)
    try {
      const data = await getGooglePhotosCards(deviceUrl)
      setCards(data?.cards || [])
    } catch (err) {
      setCards([])
    } finally {
      setCardsLoading(false)
    }

    // Resolve Google Photos album matches separately so paginating the album
    // library never blocks the card list. Match albums to cards by title
    // (albums are named after the card) and merge album_id / item count in when
    // they arrive — the sort/clear controls light up progressively.
    try {
      const albumData = await getGooglePhotosAlbums(deviceUrl)
      const albums = albumData?.albums || []
      if (albums.length > 0) {
        const byTitle = new Map(albums.map((a) => [a.title, a]))
        setCards((prev) =>
          prev
            ? prev.map((c) => {
                const a = byTitle.get(c.name)
                if (!a) return c
                return {
                  ...c,
                  album_id: a.id,
                  album_item_count: Number.parseInt(a.mediaItemsCount, 10) || 0,
                }
              })
            : prev
        )
      }
    } catch {
      // Best-effort: cards remain usable without album info.
    }
  }, [deviceUrl])

  // Lazily fetch each card's summary (file count, bytes, transferred count, and
  // up to 4 preview paths) so the card list renders immediately and fills in.
  const loadCardSummaries = useCallback(
    async (cardList) => {
      if (!deviceUrl || !cardList) return
      // Only fetch summaries we haven't already requested.
      const pending = cardList.filter((card) => !summaryFetchedRef.current.has(card.name))
      if (pending.length === 0) return
      for (const card of pending) {
        summaryFetchedRef.current.add(card.name)
        setCardSummaries((prev) => ({ ...prev, [card.name]: { loading: true } }))
      }

      // Each summary triggers a recursive B2 walk on the device, so fetch them
      // concurrently (bounded) instead of one-at-a-time — sequential awaits made
      // the page take ~30s with several cards. The bound keeps us from hammering
      // the device with one request per card all at once.
      const CONCURRENCY = 6
      const fetchOne = async (card) => {
        try {
          const data = await getGooglePhotosCardSummary(deviceUrl, card.name)
          setCardSummaries((prev) => ({ ...prev, [card.name]: { loading: false, ...data } }))
        } catch (err) {
          setCardSummaries((prev) => ({
            ...prev,
            [card.name]: { loading: false, error: describeError(err) },
          }))
        }
      }
      let next = 0
      const worker = async () => {
        while (next < pending.length) {
          const card = pending[next++]
          await fetchOne(card)
        }
      }
      await Promise.all(
        Array.from({ length: Math.min(CONCURRENCY, pending.length) }, worker)
      )
    },
    [deviceUrl]
  )

  const handleToggleCard = useCallback((cardName, checked) => {
    setSelectedCardNames((prev) => {
      const next = new Set(prev)
      if (checked) next.add(cardName)
      else next.delete(cardName)
      return next
    })
  }, [])

  const handleToggleAll = useCallback(
    (checked) => {
      setSelectedCardNames(() => (checked && cards ? new Set(cards.map((c) => c.name)) : new Set()))
    },
    [cards]
  )

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
                // Clearing wipes the album and its local upload ledger, so the
                // card's transferred count and preview must be recomputed.
                summaryFetchedRef.current.delete(albumTitle)
                loadCards()
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
    [deviceUrl, toast, loadCards]
  )

  // Poll the backend for an album sort that is running server-side. Safe to
  // call to (re)attach to a sort started in a previous page visit — the sort
  // runs in the background on the device regardless of this page being open.
  const pollSortProgress = useCallback(
    (albumId, albumTitle) => {
      if (!deviceUrl) return
      if (sortProgressIntervalRef.current) {
        clearInterval(sortProgressIntervalRef.current)
        sortProgressIntervalRef.current = null
      }

      const interval = setInterval(async () => {
        try {
          const data = await getGooglePhotosAlbumSortProgress(deviceUrl, albumId)
          setSortProgress((prev) => ({ ...prev, [albumId]: data }))

          if (data?.status === 'completed' || data?.status === 'error' || data?.status === 'idle') {
            clearInterval(interval)
            sortProgressIntervalRef.current = null
            setSortingAlbumId(null)
            if (data?.status === 'completed') {
              toast.success(`Sorted "${albumTitle}" — ${data?.total_items || 0} items reordered`)
              // The sort recreates the album with a new id; refresh the matched
              // album ids so subsequent sort/clear target the right album.
              loadCards()
            } else if (data?.status === 'error') {
              if (Number(data?.inaccessible || 0) > 0) {
                toast.error(`Sort skipped: ${data.inaccessible} item(s) aren't visible to this app — album left untouched`)
              } else {
                toast.error(`Failed to sort album: ${data?.error || 'Unknown error'}`)
              }
            }
          }
        } catch (err) {
          clearInterval(interval)
          sortProgressIntervalRef.current = null
          setSortingAlbumId(null)
          toast.error(`Failed to sort album: ${describeError(err)}`)
        }
      }, 1000)

      sortProgressIntervalRef.current = interval
    },
    [deviceUrl, toast, loadCards]
  )

  const handleSortAlbum = useCallback(
    async (albumId, albumTitle) => {
      if (!deviceUrl) return

      setSortingAlbumId(albumId)
      setSortProgress((prev) => ({ ...prev, [albumId]: { status: 'listing', total_items: 0, added_items: 0 } }))

      try {
        await sortGooglePhotosAlbum(deviceUrl, albumId)
        pollSortProgress(albumId, albumTitle)
      } catch (err) {
        setSortingAlbumId(null)
        toast.error(`Failed to start sorting: ${describeError(err)}`)
      }
    },
    [deviceUrl, toast, pollSortProgress]
  )

  useEffect(() => {
    loadAll()
  }, [loadAll])

  useEffect(() => {
    if (status?.connected) {
      loadCards()
    }
  }, [status?.connected, loadCards])

  // Reconcile card selection whenever the card list changes: newly appeared
  // cards default to selected, user toggles for still-present cards are kept,
  // and removed cards are dropped. Also kick off lazy summary loading.
  useEffect(() => {
    if (!cards) return
    setSelectedCardNames((prev) => {
      const next = new Set()
      for (const card of cards) {
        const wasKnown = knownCardNamesRef.current.has(card.name)
        if (!wasKnown || prev.has(card.name)) next.add(card.name)
      }
      return next
    })
    knownCardNamesRef.current = new Set(cards.map((c) => c.name))
    if (cards.length > 0) {
      loadCardSummaries(cards)
    }
  }, [cards, loadCardSummaries])

  // Reconcile with sorts still running on the device. A sort runs in the
  // background server-side, so if the user navigated away and came back we
  // re-attach to it instead of showing an idle "Sort" button.
  useEffect(() => {
    if (!deviceUrl || sortingAlbumId || !cards || cards.length === 0) return
    let cancelled = false
    ;(async () => {
      // Probe every card's album in parallel — these are cheap in-memory reads
      // on the device, but issuing them one-at-a-time made reconciliation scale
      // with the card count. Re-attach to the first card found mid-sort.
      const withAlbums = cards.filter((c) => c.album_id)
      const results = await Promise.all(
        withAlbums.map(async (card) => {
          try {
            const data = await getGooglePhotosAlbumSortProgress(deviceUrl, card.album_id)
            return { card, data }
          } catch {
            return null
          }
        })
      )
      if (cancelled) return
      for (const r of results) {
        if (!r) continue
        const st = r.data?.status
        if (st && st !== 'idle' && st !== 'completed' && st !== 'error') {
          setSortProgress((prev) => ({ ...prev, [r.card.album_id]: r.data }))
          setSortingAlbumId(r.card.album_id)
          pollSortProgress(r.card.album_id, r.card.name)
          return
        }
      }
    })()
    return () => {
      cancelled = true
    }
  }, [deviceUrl, cards, sortingAlbumId, pollSortProgress])

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

  const handleSync = useCallback(async (force = false, cards = null) => {
    if (!deviceUrl) return
    if (!status?.connected) {
      toast.error('Connect Google Photos before starting a sync')
      return
    }
    if (
      force &&
      !window.confirm(
        'Force re-sync ignores the local upload tracking and re-uploads every file not already confirmed in its album. Use this only when photos are missing from Google Photos despite a successful sync. Continue?',
      )
    ) {
      return
    }
    const selection = Array.isArray(cards) && cards.length ? cards : null
    try {
      await startGooglePhotosSync(deviceUrl, force, selection)
      setSyncing(true)
      const scope = selection ? `${selection.length} selected album(s)` : 'all albums'
      toast.success(force ? `Force re-sync of ${scope} started` : `Sync of ${scope} started`)
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

  // Sync only the cards the user ticked. The backend filters the B2 → Google
  // Photos sync to these card names.
  const handleSyncSelected = useCallback(() => {
    if (!cards) return
    const names = cards.filter((c) => selectedCardNames.has(c.name)).map((c) => c.name)
    if (names.length === 0) {
      toast.error('Select at least one card to sync')
      return
    }
    handleSync(false, names)
  }, [cards, selectedCardNames, handleSync, toast])

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

  const itemVariants = reduceMotion
    ? undefined
    : { hidden: { opacity: 0, y: 10 }, show: { opacity: 1, y: 0, transition: { duration: 0.25 } } }

  return (
    <motion.div
      className="space-y-4"
      initial={reduceMotion ? false : 'hidden'}
      animate="show"
      variants={{ show: { transition: { staggerChildren: 0.06 } } }}
    >
      <motion.div variants={itemVariants}>
        <StatusPanel status={status} statusError={statusError} />
      </motion.div>

      <motion.div variants={itemVariants}>
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
      </motion.div>

      <AnimatePresence initial={false}>
        {showProgress ? (
          <motion.div
            key="progress"
            initial={reduceMotion ? false : { opacity: 0, y: 10 }}
            animate={{ opacity: 1, y: 0 }}
            exit={reduceMotion ? { opacity: 0 } : { opacity: 0, y: -8 }}
          >
            {progress ? <ProgressPanel progress={progress} reduceMotion={reduceMotion} /> : <SyncStartingPanel reduceMotion={reduceMotion} />}
          </motion.div>
        ) : null}
      </AnimatePresence>

      {isConnected && (
        <motion.div variants={itemVariants}>
          <CardsPanel
            cards={cards}
            loading={cardsLoading}
            summaries={cardSummaries}
            deviceUrl={deviceUrl}
            onClear={handleClearAlbum}
            clearingId={clearingAlbumId}
            clearProgress={clearProgress}
            onSort={handleSortAlbum}
            sortingId={sortingAlbumId}
            sortProgress={sortProgress}
            reduceMotion={reduceMotion}
            selectedNames={selectedCardNames}
            onToggleCard={handleToggleCard}
            onToggleAll={handleToggleAll}
            onSyncSelected={handleSyncSelected}
            syncing={syncing}
          />
        </motion.div>
      )}

      <motion.div variants={itemVariants}>
        <InfoPanel />
      </motion.div>
    </motion.div>
  )
}
