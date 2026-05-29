import { useState, useEffect, useCallback, useRef } from 'react'
import { motion, useReducedMotion } from 'framer-motion'
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
import { CardHeader, CardTitle } from '../components/Card.jsx'
import { StatusBadge } from '../components/StatusBadge.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { Skeleton } from '../components/Skeleton.jsx'
import { Modal } from '../components/Modal.jsx'
import { ErrorState } from '../components/ErrorState.jsx'

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

/* ─── Animated counter hook (respects prefers-reduced-motion) ─── */
function useAnimatedNumber(target, duration = 600) {
  const reduceMotion = useReducedMotion()
  const [display, setDisplay] = useState(target)
  const startRef = useRef(null)
  const fromRef = useRef(target)

  useEffect(() => {
    if (reduceMotion) {
      setDisplay(target)
      return undefined
    }
    fromRef.current = display
    startRef.current = null
    let raf
    const animate = (ts) => {
      if (startRef.current === null) startRef.current = ts
      const elapsed = ts - startRef.current
      const progress = Math.min(1, elapsed / duration)
      const eased = 1 - Math.pow(1 - progress, 3)
      const current = Math.round(fromRef.current + (target - fromRef.current) * eased)
      setDisplay(current)
      if (progress < 1) raf = requestAnimationFrame(animate)
    }
    raf = requestAnimationFrame(animate)
    return () => cancelAnimationFrame(raf)
  }, [target, duration, reduceMotion])

  return display
}

/* ─── Circular progress ring ─── */
function CircularProgress({ percent, size = 160, stroke = 10, children }) {
  const r = (size - stroke) / 2
  const c = 2 * Math.PI * r
  const offset = c - (Math.min(100, Math.max(0, percent)) / 100) * c
  const gradId = `ring-grad-${size}`

  return (
    <div className="relative inline-flex items-center justify-center" style={{ width: size, height: size }}>
      <svg width={size} height={size} className="-rotate-90" aria-hidden="true">
        <defs>
          <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" stopColor="var(--color-brand-400)" />
            <stop offset="100%" stopColor="var(--color-brand-600)" />
          </linearGradient>
        </defs>
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke="currentColor" strokeWidth={stroke}
          className="text-surface-700/50" />
        <circle cx={size / 2} cy={size / 2} r={r} fill="none" stroke={`url(#${gradId})`} strokeWidth={stroke}
          strokeLinecap="round"
          className="transition-[stroke-dashoffset] duration-500 ease-out"
          style={{ strokeDasharray: c, strokeDashoffset: offset, filter: 'drop-shadow(0 0 4px rgba(34,211,238,0.35))' }} />
      </svg>
      <div className="absolute inset-0 flex flex-col items-center justify-center">
        {children}
      </div>
    </div>
  )
}

/* ─── Hero stat tile ─── */
function HeroStat({ icon, label, children }) {
  return (
    <div className="rounded-xl bg-surface-950/50 border border-surface-700/40 p-3.5 text-center sm:p-4">
      <div className="flex items-center justify-center gap-1.5 mb-1">
        <Icon name={icon} className="w-3.5 h-3.5 text-brand-400" aria-hidden="true" />
        <span className="text-[10px] text-surface-500 uppercase tracking-wider">{label}</span>
      </div>
      <p className="text-lg font-bold text-surface-100 tabular-nums sm:text-xl">{children}</p>
    </div>
  )
}

/* ─── Hero sync progress section ─── */
function SyncHero({ sync }) {
  const reduceMotion = useReducedMotion()
  const percent = getProgressPercent(sync)
  const animatedPercent = useAnimatedNumber(percent, 400)
  const animatedFiles = useAnimatedNumber(sync?.files_synced || 0, 400)
  const totalFiles = sync?.files_total || 0
  const speed = formatSpeed(sync?.transfer_speed)
  const eta = sync?.eta || '--'
  const currentFile = sync?.current_file

  return (
    <motion.div
      initial={reduceMotion ? false : { opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.4, ease: 'easeOut' }}
      className="relative overflow-hidden rounded-2xl border border-brand-500/25 bg-gradient-to-br from-surface-800/80 to-surface-900/90 p-5 shadow-lg shadow-brand-500/5 sm:p-6 md:p-8"
      role="status"
      aria-live="polite"
      aria-label={`Sync ${percent}% complete`}
    >
      {/* Ambient glow */}
      <div className="pointer-events-none absolute -top-16 -right-16 h-48 w-48 rounded-full bg-brand-500/10 blur-3xl" aria-hidden="true" />

      {/* Animated pulse rings (suppressed when reduced motion is preferred) */}
      {!reduceMotion && (
        <div className="absolute inset-0 flex items-center justify-center pointer-events-none" aria-hidden="true">
          <div className="w-40 h-40 rounded-full border border-brand-500/10 animate-ping" style={{ animationDuration: '3s' }} />
          <div className="absolute w-56 h-56 rounded-full border border-brand-500/5 animate-ping" style={{ animationDuration: '4s', animationDelay: '1s' }} />
        </div>
      )}

      <div className="relative flex flex-col items-center gap-6 md:flex-row md:gap-10">
        {/* Circular progress */}
        <div className="shrink-0">
          <CircularProgress percent={percent} size={160} stroke={10}>
            <span className="text-3xl font-bold text-surface-100 tabular-nums">{animatedPercent}%</span>
            <span className="mt-1 text-[11px] text-surface-500">{getProgressLabel(sync)}</span>
          </CircularProgress>
        </div>

        {/* Stats grid */}
        <div className="w-full flex-1">
          <div className="mb-4 flex items-center gap-2">
            <span className="relative flex h-2.5 w-2.5" aria-hidden="true">
              {!reduceMotion && (
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-brand-400 opacity-60" style={{ animationDuration: '1.6s' }} />
              )}
              <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-brand-400" />
            </span>
            <span className="text-xs font-semibold uppercase tracking-wider text-brand-300">Sync in progress</span>
          </div>

          <div className="grid grid-cols-2 gap-3 sm:gap-4">
            <HeroStat icon="arrow-up-tray" label="Files">
              {animatedFiles.toLocaleString()}
              <span className="text-sm font-normal text-surface-500"> / {totalFiles.toLocaleString()}</span>
            </HeroStat>
            <HeroStat icon="activity" label="Speed">{speed}</HeroStat>
            <HeroStat icon="clock" label="ETA">{eta}</HeroStat>
            <HeroStat icon="image" label="Size">{formatBytes(sync?.bytes_synced || 0)}</HeroStat>
          </div>

          {currentFile && (
            <p className="mt-4 truncate text-center font-mono text-[11px] text-surface-500 md:text-left" title={currentFile}>
              {currentFile}
            </p>
          )}
        </div>
      </div>
    </motion.div>
  )
}

/* ─── Gradient border card wrapper ─── */
function GradientCard({ children, className = '', delay = 0 }) {
  const reduceMotion = useReducedMotion()
  return (
    <motion.div
      initial={reduceMotion ? false : { opacity: 0, y: 10 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.45, delay: delay / 1000, ease: 'easeOut' }}
      className={`group relative rounded-lg p-[1px] bg-gradient-to-br from-brand-500/20 via-surface-700/40 to-brand-500/10 ${className}`}
    >
      <div className="relative h-full rounded-lg bg-surface-800/55 p-4 shadow-sm shadow-black/10 transition-colors duration-300 group-hover:bg-surface-800/70">
        {children}
      </div>
    </motion.div>
  )
}

/* ─── Icon background circle ─── */
function IconCircle({ icon, colorClass = 'text-brand-400', bgClass = 'bg-brand-500/10' }) {
  return (
    <div className={`w-9 h-9 rounded-xl flex items-center justify-center shrink-0 ${bgClass}`}>
      <Icon name={icon} className={`w-5 h-5 ${colorClass}`} />
    </div>
  )
}

/* ─── System status dot ─── */
function StatusDot({ variant = 'success', pulse = false }) {
  const colorMap = {
    success: 'bg-success',
    warning: 'bg-warning',
    danger: 'bg-danger',
    info: 'bg-info',
    neutral: 'bg-surface-500',
  }
  return (
    <span className="relative flex h-2.5 w-2.5">
      {pulse && <span className={`absolute inline-flex h-full w-full rounded-full opacity-60 motion-safe:animate-ping ${colorMap[variant]}`} style={{ animationDuration: '2s' }} />}
      <span className={`relative inline-flex rounded-full h-2.5 w-2.5 ${colorMap[variant]}`} />
    </span>
  )
}

/* ─── Sparkline bar for history entries ─── */
function SparklineBar({ percent, variant = 'success' }) {
  const colorMap = {
    success: 'bg-success',
    warning: 'bg-warning',
    danger: 'bg-danger',
    info: 'bg-info',
    neutral: 'bg-surface-500',
  }
  return (
    <div className="w-16 h-1.5 bg-surface-700/60 rounded-full overflow-hidden shrink-0">
      <div className={`h-full rounded-full ${colorMap[variant]}`} style={{ width: `${percent}%` }} />
    </div>
  )
}

/* ─── Stat mini-card ─── */
function StatMiniCard({ icon, label, value, delay = 0 }) {
  const reduceMotion = useReducedMotion()
  return (
    <motion.div
      initial={reduceMotion ? false : { opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      transition={{ duration: 0.35, delay: delay / 1000, ease: 'easeOut' }}
      className="group relative overflow-hidden rounded-xl border border-surface-700/40 bg-surface-800/50 p-3.5 transition-colors duration-300 hover:border-brand-500/30 sm:p-4"
    >
      <div className="pointer-events-none absolute -right-6 -top-6 h-16 w-16 rounded-full bg-brand-500/5 blur-xl transition-opacity duration-300 group-hover:opacity-100 opacity-0" aria-hidden="true" />
      <div className="relative flex items-center gap-3">
        <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-brand-500/10 text-brand-400 transition-colors duration-300 group-hover:bg-brand-500/20">
          <Icon name={icon} className="w-5 h-5" aria-hidden="true" />
        </div>
        <div className="min-w-0">
          <p className="text-[10px] uppercase tracking-wider text-surface-500">{label}</p>
          <p className="truncate text-sm font-bold text-surface-100 tabular-nums">{value}</p>
        </div>
      </div>
    </motion.div>
  )
}

/* ─── System Status Card ─── */
function SystemStatusCard({ status }) {
  const statusConf = SYNC_STATUS_CONFIG[status.status] || SYNC_STATUS_CONFIG.idle
  const cardInfo = getOverviewCardID(status)
  const runtime = status.runtime || null
  const memory = runtime?.memory || {}
  const cgroup = runtime?.cgroup || {}

  return (
    <GradientCard delay={100}>
      <CardHeader>
        <div className="flex items-center gap-2.5">
          <IconCircle icon="activity" />
          <CardTitle>System Status</CardTitle>
        </div>
        <div className="flex items-center gap-2">
          <StatusDot variant={statusConf.variant} pulse={statusConf.pulse} />
          <StatusBadge variant={statusConf.variant} pulse={statusConf.pulse}>
            {statusConf.label}
          </StatusBadge>
        </div>
      </CardHeader>

      <div className="space-y-3">
        {/* Error state */}
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
    </GradientCard>
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

/* ─── SD Card visual graphic ─── */
function SDCardGraphic({ usedPercent, label }) {
  return (
    <div className="relative">
      <div className="flex items-stretch gap-0 rounded-lg overflow-hidden border border-surface-600/50 bg-surface-900/50 h-10">
        {/* SD card shape left side */}
        <div className="w-3 bg-surface-600/40 shrink-0 relative">
          <div className="absolute top-1 left-0.5 w-1.5 h-1.5 bg-surface-500/50 rounded-sm" />
          <div className="absolute top-3 left-0.5 w-1.5 h-1.5 bg-surface-500/50 rounded-sm" />
          <div className="absolute top-5 left-0.5 w-1.5 h-1.5 bg-surface-500/50 rounded-sm" />
        </div>
        <div className="flex-1 flex items-center px-3">
          <div className="w-full">
            <div className="flex justify-between text-[11px] text-surface-500 mb-1">
              <span>{label || 'SD Card'}</span>
              <span>{usedPercent}% used</span>
            </div>
            <div className="w-full h-1.5 bg-surface-700/60 rounded-full overflow-hidden">
              <div
                className={`h-full rounded-full transition-all duration-500 ${usedPercent > 90 ? 'bg-danger' : usedPercent > 70 ? 'bg-warning' : 'bg-success'}`}
                style={{ width: `${usedPercent}%` }}
              />
            </div>
          </div>
        </div>
      </div>
    </div>
  )
}

/* ─── File type breakdown ─── */
function FileTypeBreakdown({ photos = 0, videos = 0, raw = 0 }) {
  const total = photos + videos + raw
  if (total === 0) return null

  return (
    <div className="flex items-center gap-3 pt-2">
      {photos > 0 && (
        <div className="flex items-center gap-1.5 rounded-full bg-surface-700/40 px-2.5 py-1">
          <Icon name="image" className="w-3.5 h-3.5 text-brand-400" />
          <span className="text-xs text-surface-300">{photos.toLocaleString()}</span>
        </div>
      )}
      {videos > 0 && (
        <div className="flex items-center gap-1.5 rounded-full bg-surface-700/40 px-2.5 py-1">
          <Icon name="play" className="w-3.5 h-3.5 text-info" />
          <span className="text-xs text-surface-300">{videos.toLocaleString()}</span>
        </div>
      )}
      {raw > 0 && (
        <div className="flex items-center gap-1.5 rounded-full bg-surface-700/40 px-2.5 py-1">
          <Icon name="folder" className="w-3.5 h-3.5 text-warning" />
          <span className="text-xs text-surface-300">{raw.toLocaleString()}</span>
        </div>
      )}
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

  // Estimate used space (we don't have exact used bytes, so approximate from photo count)
  const usedPercent = selectedDevice?.size > 0 && selectedDevice?.used_bytes > 0
    ? Math.min(100, Math.round((selectedDevice.used_bytes / selectedDevice.size) * 100))
    : 0

  return (
    <GradientCard delay={150}>
      <CardHeader>
        <div className="flex items-center gap-2.5">
          <IconCircle icon="sd-card" />
          <CardTitle>Device Info</CardTitle>
        </div>
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

          {/* SD Card visual */}
          {hasCard && selectedDevice?.size > 0 && (
            <SDCardGraphic usedPercent={usedPercent || Math.min(100, Math.round((photoCount / 5000) * 100))} label={deviceSize} />
          )}

          {/* Free/used segmented bar */}
          {hasCard && selectedDevice?.size > 0 && selectedDevice?.used_bytes > 0 && (
            <div>
              <div className="flex justify-between text-[11px] mb-1.5">
                <span className="text-surface-500">
                  <span className="text-success">{formatBytes(selectedDevice.size - selectedDevice.used_bytes)}</span> free
                </span>
                <span className="text-surface-500">
                  <span className="text-brand-400">{formatBytes(selectedDevice.used_bytes)}</span> used
                </span>
              </div>
              <div className="flex h-2 rounded-full overflow-hidden gap-0.5">
                <div
                  className="bg-brand-500/70 rounded-l-full transition-all duration-500"
                  style={{ width: `${usedPercent}%` }}
                />
                <div
                  className="bg-surface-700/60 rounded-r-full flex-1"
                />
              </div>
            </div>
          )}

          {/* File type breakdown */}
          {photoCount > 0 && (
            <div className="py-2 border-t border-surface-700/50">
              <div className="flex items-center justify-between mb-2">
                <div className="flex items-center gap-2">
                  <Icon name="image" className="w-4 h-4 text-surface-400" />
                  <span className="text-sm text-surface-300">Photos</span>
                </div>
                <span className="text-sm font-semibold text-surface-100">
                  {photoCount.toLocaleString()}
                </span>
              </div>
              <FileTypeBreakdown
                photos={photoCount}
                videos={status.current_sync?.video_count || 0}
                raw={status.current_sync?.raw_count || 0}
              />
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
    </GradientCard>
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

/* ─── Timeline-style Sync History ─── */
function SyncHistoryCard({ history }) {
  if (!history || history.length === 0) {
    return (
      <GradientCard delay={300}>
        <CardHeader>
          <div className="flex items-center gap-2.5">
            <IconCircle icon="clock" />
            <CardTitle>Recent Syncs</CardTitle>
          </div>
        </CardHeader>
        <div className="flex flex-col items-center justify-center py-8 text-center" role="status">
          <div className="mb-3 flex h-14 w-14 items-center justify-center rounded-2xl bg-gradient-to-br from-brand-500/15 to-brand-700/5 border border-brand-400/10">
            <Icon name="clock" className="h-7 w-7 text-brand-400 float-animation" aria-hidden="true" />
          </div>
          <p className="text-sm font-medium text-surface-300">No syncs yet</p>
          <p className="mt-1 text-xs text-surface-500">Completed syncs will appear here</p>
        </div>
      </GradientCard>
    )
  }

  const recent = history.slice(-5).reverse()

  return (
    <GradientCard delay={300}>
      <CardHeader>
        <div className="flex items-center gap-2.5">
          <IconCircle icon="clock" />
          <CardTitle>Recent Syncs</CardTitle>
        </div>
        <span className="text-xs text-surface-500">{history.length} total</span>
      </CardHeader>

      <div className="relative">
        {/* Vertical timeline line */}
        <div className="absolute left-[11px] top-2 bottom-2 w-px bg-surface-700/50" />

        <div className="space-y-1">
          {recent.map((entry, i) => {
            const entryStatus = SYNC_STATUS_CONFIG[entry.status] || SYNC_STATUS_CONFIG.idle
            const duration = entry.end_time && entry.start_time
              ? (new Date(entry.end_time) - new Date(entry.start_time)) / 1000
              : null
            const completion = entry.files_total > 0
              ? Math.min(100, Math.round((entry.files_synced / entry.files_total) * 100))
              : entry.status === 'success' ? 100 : 0
            const exactTime = entry.start_time
              ? new Date(entry.start_time).toLocaleString('en-US', {
                  month: 'short', day: 'numeric', hour: '2-digit', minute: '2-digit'
                })
              : '--'

            return (
              <TimelineEntry
                key={entry.id || i}
                index={i}
                entryStatus={entryStatus}
                exactTime={exactTime}
                entry={entry}
                completion={completion}
                duration={duration}
              />
            )
          })}
        </div>
      </div>
    </GradientCard>
  )
}

/* ─── Single timeline entry with staggered entrance ─── */
const TIMELINE_DOT = {
  success: 'border-success bg-success/20',
  danger: 'border-danger bg-danger/20',
  warning: 'border-warning bg-warning/20',
}
const TIMELINE_CORE = {
  success: 'bg-success',
  danger: 'bg-danger',
  warning: 'bg-warning',
}

function TimelineEntry({ index, entryStatus, exactTime, entry, completion, duration }) {
  const reduceMotion = useReducedMotion()
  const dotClass = TIMELINE_DOT[entryStatus.variant] || 'border-surface-500 bg-surface-800'
  const coreClass = TIMELINE_CORE[entryStatus.variant] || 'bg-surface-500'

  return (
    <motion.div
      initial={reduceMotion ? false : { opacity: 0, x: -6 }}
      animate={{ opacity: 1, x: 0 }}
      transition={{ duration: 0.3, delay: Math.min(index * 0.05, 0.3), ease: 'easeOut' }}
      className="relative flex items-start gap-3 px-1 py-1.5"
    >
      {/* Timeline dot */}
      <div className="relative z-10 mt-3 shrink-0">
        <div className={`flex h-5 w-5 items-center justify-center rounded-full border-2 ${dotClass}`}>
          <div className={`h-1.5 w-1.5 rounded-full ${coreClass}`} />
        </div>
      </div>

      <div className="min-w-0 flex-1 rounded-lg border border-transparent bg-surface-900/30 px-3 py-2.5 transition-colors duration-200 hover:border-surface-700/60 hover:bg-surface-900/50">
        <div className="mb-1 flex flex-wrap items-center gap-2">
          <StatusBadge variant={entryStatus.variant} size="sm">
            {entry.status}
          </StatusBadge>
          <span className="text-xs text-surface-500" title={exactTime}>
            {formatTimeAgo(entry.start_time)}
          </span>
          <SparklineBar percent={completion} variant={entryStatus.variant} />
        </div>
        <p className="text-sm text-surface-300">
          <span className="font-medium text-surface-100 tabular-nums">{(entry.files_synced || 0).toLocaleString()}</span> files synced
          {duration != null && (
            <span className="text-surface-500"> in {formatDuration(duration)}</span>
          )}
          {entry.bytes_synced > 0 && (
            <span className="text-surface-500"> · {formatBytes(entry.bytes_synced)}</span>
          )}
        </p>
        {entry.error && (
          <p className="mt-1 truncate text-xs text-danger" title={entry.error}>{entry.error}</p>
        )}
      </div>
    </motion.div>
  )
}

/* ─── Panic card with collapsible stack traces ─── */
function PanicInfoCard({ panicInfo, onClear, clearing }) {
  const records = Array.isArray(panicInfo?.panics)
    ? panicInfo.panics
    : (panicInfo?.panic ? [panicInfo.panic] : [])
  if (!panicInfo?.exists || records.length === 0) return null

  const title = records.length === 1 ? 'Saved Panic' : 'Saved Panics'

  return (
    <div className="relative overflow-hidden rounded-lg border-y border-r border-l-4 border-surface-700/60 border-l-danger bg-surface-800/55 shadow-sm shadow-black/10">
      <div className="pointer-events-none absolute -left-10 top-0 h-full w-24 bg-danger/5 blur-2xl" aria-hidden="true" />
      <div className="relative p-4">
        <div className="flex items-center justify-between gap-3 mb-3">
          <div className="flex items-center gap-2">
            <div className="relative">
              <Icon name="exclamation-triangle" className="w-5 h-5 text-danger" aria-hidden="true" />
              <span className="absolute -top-0.5 -right-0.5 h-2 w-2 rounded-full bg-danger motion-safe:animate-ping" style={{ animationDuration: '2s' }} />
            </div>
            <h3 className="text-sm font-semibold text-surface-200 uppercase tracking-wide">{title}</h3>
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
        </div>

        <div className="space-y-4">
          {records.map((record, index) => (
            <PanicRecord key={`${record.time || 'unknown'}-${record.source || 'panic'}-${index}`} record={record} />
          ))}
        </div>
      </div>
    </div>
  )
}

function PanicRecord({ record }) {
  const [expanded, setExpanded] = useState(false)
  const occurredAt = record.time ? new Date(record.time).toLocaleString() : 'Unknown time'

  return (
    <div className="space-y-3 border-t border-surface-700/60 pt-4 first:border-t-0 first:pt-0">
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
        <div>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => setExpanded((v) => !v)}
            className="text-xs"
            aria-expanded={expanded}
          >
            <Icon
              name="chevron-right"
              className={`w-3 h-3 transition-transform duration-200 ${expanded ? 'rotate-90' : ''}`}
              aria-hidden="true"
            />
            {expanded ? 'Hide stack trace' : 'Show stack trace'}
          </Button>
          {expanded && (
            <pre className="mt-2 max-h-64 overflow-auto rounded-lg border border-surface-700 bg-surface-950 p-3 text-xs leading-relaxed text-surface-300">
              {record.stack}
            </pre>
          )}
        </div>
      )}
    </div>
  )
}

/* ─── Stats mini-cards row ─── */
function StatsRow({ history }) {
  const stats = computeStats(history)

  return (
    <div className="grid grid-cols-2 md:grid-cols-4 gap-3">
      <StatMiniCard icon="image" label="Photos Synced" value={stats.totalPhotos.toLocaleString()} delay={50} />
      <StatMiniCard icon="arrow-up-tray" label="Data Transferred" value={formatBytes(stats.totalBytes)} delay={100} />
      <StatMiniCard icon="activity" label="Avg Speed" value={stats.avgSpeed} delay={150} />
      <StatMiniCard icon="clock" label="Uptime" value={stats.uptime} delay={200} />
    </div>
  )
}

function computeStats(history) {
  if (!history || history.length === 0) {
    return { totalPhotos: 0, totalBytes: 0, avgSpeed: '--', uptime: '--' }
  }

  const completed = history.filter((h) => h.status === 'success')
  const totalPhotos = completed.reduce((sum, h) => sum + (h.files_synced || 0), 0)
  const totalBytes = completed.reduce((sum, h) => sum + (h.bytes_synced || 0), 0)

  // Average speed from entries that have speed data
  const speeds = history
    .filter((h) => h.transfer_speed > 0)
    .map((h) => h.transfer_speed)
  const avgSpeed = speeds.length > 0
    ? formatSpeed(speeds.reduce((a, b) => a + b, 0) / speeds.length)
    : '--'

  // Uptime: time since first sync
  const firstStart = history
    .map((h) => h.start_time)
    .filter(Boolean)
    .sort()[0]
  const uptime = firstStart
    ? formatDuration(Math.floor((Date.now() - new Date(firstStart).getTime()) / 1000))
    : '--'

  return { totalPhotos, totalBytes, avgSpeed, uptime }
}

/* ─── Connection banner with retry countdown ─── */
function ConnectionBanner({ error, consecutiveErrors, onRetry, wsConnected, wsError }) {
  const [countdown, setCountdown] = useState(5)
  const timerRef = useRef(null)

  useEffect(() => {
    if (!error || wsConnected) return
    setCountdown(5)
    timerRef.current = window.setInterval(() => {
      setCountdown((c) => {
        if (c <= 1) {
          onRetry()
          return 5
        }
        return c - 1
      })
    }, 1000)
    return () => {
      if (timerRef.current) window.clearInterval(timerRef.current)
    }
  }, [error, wsConnected, onRetry])

  if (!error) return null

  return (
    <div className="flex items-start gap-3 px-4 py-3 rounded-xl bg-warning/10 border border-warning/20 animate-in fade-in">
      <Icon name="exclamation-triangle" className="w-5 h-5 text-warning shrink-0 mt-0.5" />
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-warning">Connection issues detected</p>
        <p className="text-xs text-surface-400 mt-1">{error}</p>
        {!wsConnected && (
          <p className="text-xs text-surface-500 mt-1">
            Retrying in {countdown}s…
          </p>
        )}
      </div>
      <Button variant="ghost" size="sm" onClick={onRetry}>
        <Icon name="arrow-path" className="w-4 h-4" />
        Retry
      </Button>
    </div>
  )
}

/* ─── Connection status indicator (header) ─── */
function ConnectionIndicator({ wsConnected, wsError }) {
  if (wsConnected) {
    return (
      <div className="flex items-center gap-1.5 rounded-full bg-success/10 border border-success/30 px-2.5 py-1">
        <span className="relative flex h-2 w-2">
          <span className="absolute inline-flex h-full w-full rounded-full bg-success opacity-60 motion-safe:animate-ping" style={{ animationDuration: '2s' }} />
          <span className="relative inline-flex rounded-full h-2 w-2 bg-success" />
        </span>
        <span className="text-[11px] font-medium text-success uppercase tracking-wider">Live</span>
      </div>
    )
  }
  if (wsError) {
    return (
      <div className="flex items-center gap-1.5 rounded-full bg-danger/10 border border-danger/30 px-2.5 py-1">
        <span className="relative flex h-2 w-2">
          <span className="absolute inline-flex h-full w-full rounded-full bg-danger opacity-60 motion-safe:animate-ping" style={{ animationDuration: '2s' }} />
          <span className="relative inline-flex rounded-full h-2 w-2 bg-danger" />
        </span>
        <span className="text-[11px] font-medium text-danger uppercase tracking-wider">Offline</span>
      </div>
    )
  }
  return (
    <div className="flex items-center gap-1.5 rounded-full bg-surface-700/50 border border-surface-600/50 px-2.5 py-1">
      <span className="w-2 h-2 rounded-full bg-surface-500" />
      <span className="text-[11px] font-medium text-surface-500 uppercase tracking-wider">Connecting</span>
    </div>
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

/* ─── Layout-matching loading skeleton ─── */
function SkeletonGradientCard() {
  return (
    <div className="rounded-lg p-[1px] bg-gradient-to-br from-brand-500/15 via-surface-700/40 to-brand-500/5">
      <div className="space-y-4 rounded-lg bg-surface-800/55 p-4 shadow-sm shadow-black/10">
        <div className="flex items-center gap-2.5">
          <Skeleton className="h-9 w-9 rounded-xl" />
          <Skeleton className="h-4 w-32" />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <Skeleton className="h-12 w-full rounded-lg" />
          <Skeleton className="h-12 w-full rounded-lg" />
          <Skeleton className="h-12 w-full rounded-lg" />
          <Skeleton className="h-12 w-full rounded-lg" />
        </div>
      </div>
    </div>
  )
}

function StatusSkeleton() {
  return (
    <div className="space-y-4" aria-busy="true" aria-live="polite">
      <span className="sr-only">Loading device status…</span>
      <div className="flex items-center justify-between">
        <Skeleton className="h-6 w-40" />
        <Skeleton className="h-8 w-24 rounded-lg" />
      </div>
      <Skeleton className="h-12 w-full rounded-lg" />
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        {Array.from({ length: 4 }).map((_, i) => (
          <Skeleton key={i} className="h-16 w-full rounded-xl" />
        ))}
      </div>
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
        <SkeletonGradientCard />
        <SkeletonGradientCard />
      </div>
    </div>
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
    return <StatusSkeleton />
  }

  if (error && !status) {
    return (
      <div className="flex min-h-[50vh] items-center justify-center">
        <ErrorState
          error={error}
          title="Can't reach the device"
          onRetry={() => fetchData()}
        />
      </div>
    )
  }

  const isSyncing = status?.status === 'syncing'

  return (
    <div className="space-y-4">
      {/* Header with connection indicator */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <h2 className="text-xl font-bold tracking-tight text-surface-100 sm:text-2xl">Overview</h2>
          <ConnectionIndicator wsConnected={wsConnected} wsError={wsError} />
        </div>
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

      {/* Connection banner */}
      <ConnectionBanner
        error={error}
        consecutiveErrors={consecutiveErrorsRef.current}
        onRetry={() => fetchData()}
        wsConnected={wsConnected}
        wsError={wsError}
      />

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

      {/* Hero sync progress */}
      {isSyncing && status?.current_sync && (
        <SyncHero sync={status.current_sync} />
      )}

      {/* Stats mini-cards */}
      {history.length > 0 && <StatsRow history={history} />}

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
