import { useState, useEffect, useCallback, useMemo } from 'react'
import { motion, AnimatePresence, useReducedMotion } from 'framer-motion'

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

function formatBytes(bytes) {
  const n = Number(bytes)
  if (!Number.isFinite(n) || n <= 0) return null
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.floor(Math.log(n) / Math.log(1024))
  return `${(n / Math.pow(1024, i)).toFixed(i > 0 ? 1 : 0)} ${units[i]}`
}
import { useDevice } from '../DeviceContext.jsx'
import { getHistory, getStatus } from '../api.js'
import { StatusBadge } from '../components/StatusBadge.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { Skeleton } from '../components/Skeleton.jsx'
import { useToast } from '../components/Toast.jsx'
import { EmptyState } from '../components/EmptyState.jsx'
import { ErrorState } from '../components/ErrorState.jsx'

function formatTimestamp(ts) {
  if (!ts) return '--'
  const date = new Date(ts)
  if (isNaN(date.getTime())) return '--'
  const months = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun',
    'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']
  const month = months[date.getMonth()]
  const day = date.getDate()
  const year = date.getFullYear()
  const hours = String(date.getHours()).padStart(2, '0')
  const minutes = String(date.getMinutes()).padStart(2, '0')
  return `${month} ${day}, ${year} ${hours}:${minutes}`
}

function formatDuration(seconds) {
  if (seconds == null || seconds < 0) return '--'
  const totalSeconds = Math.round(seconds)
  if (totalSeconds < 60) return `${totalSeconds}s`
  const h = Math.floor(totalSeconds / 3600)
  const m = Math.floor((totalSeconds % 3600) / 60)
  const s = totalSeconds % 60
  if (h > 0) return `${h}h ${m}m`
  return `${m}m ${s}s`
}

function entryStart(entry) {
  return entry.start_time || entry.timestamp || null
}

function entryDuration(entry) {
  if (entry.duration != null) return entry.duration
  if (!entry.start_time || !entry.end_time) return null
  const start = new Date(entry.start_time).getTime()
  const end = new Date(entry.end_time).getTime()
  if (isNaN(start) || isNaN(end) || end < start) return null
  return (end - start) / 1000
}

function entryFileCount(entry) {
  return entry.files_synced ?? entry.files_total ?? entry.photos_synced ?? null
}

function statusConfig(status) {
  switch (status) {
    case 'success':
      return { variant: 'success', label: 'Success', icon: 'check' }
    case 'failed':
    case 'error':
      return { variant: 'danger', label: 'Failed', icon: 'x' }
    case 'syncing':
    case 'in-progress':
    case 'running':
      return { variant: 'info', label: 'In Progress', icon: 'arrow-path', pulse: true }
    default:
      return { variant: 'neutral', label: status || 'Unknown', icon: 'clock' }
  }
}

const FILTER_TABS = [
  { id: 'all', label: 'All' },
  { id: 'success', label: 'Success' },
  { id: 'failed', label: 'Failed' },
]

const ICON_TONE = {
  success: 'bg-success/15 text-success ring-success/20',
  danger: 'bg-danger/15 text-danger ring-danger/20',
  info: 'bg-info/15 text-info ring-info/20',
  neutral: 'bg-surface-700/50 text-surface-400 ring-surface-600/30',
}

const ACCENT_BORDER = {
  success: 'before:bg-success/60',
  danger: 'before:bg-danger/60',
  info: 'before:bg-info/60',
  neutral: 'before:bg-surface-600/50',
}

function MetaStat({ label, value }) {
  return (
    <div className="text-right">
      <div className="text-sm font-medium text-surface-200 tabular-nums">{value}</div>
      <div className="text-[10px] uppercase tracking-wider text-surface-500">{label}</div>
    </div>
  )
}

function HistoryEntry({ entry, index = 0 }) {
  const reduceMotion = useReducedMotion()
  const cfg = statusConfig(entry.status)
  const hasError = entry.status === 'failed' || entry.status === 'error'
  const tone = ICON_TONE[cfg.variant] || ICON_TONE.neutral
  const accent = ACCENT_BORDER[cfg.variant] || ACCENT_BORDER.neutral
  const files = entryFileCount(entry)
  const bytes = formatBytes(entry.bytes_synced)

  return (
    <motion.div
      layout={!reduceMotion}
      initial={reduceMotion ? false : { opacity: 0, y: 8 }}
      animate={{ opacity: 1, y: 0 }}
      exit={reduceMotion ? undefined : { opacity: 0, scale: 0.98 }}
      transition={{ duration: 0.28, delay: Math.min(index * 0.03, 0.25), ease: 'easeOut' }}
      className={`group relative min-w-0 overflow-hidden rounded-lg border border-surface-700/60 bg-surface-800/55 p-4 shadow-sm shadow-black/10
        transition-colors duration-200 hover:border-surface-600/70 hover:bg-surface-800/80
        before:absolute before:inset-y-0 before:left-0 before:w-1 before:rounded-r ${accent}`}
    >
      {/* Desktop layout */}
      <div className="hidden items-center gap-4 sm:flex">
        <div className="flex min-w-0 flex-1 items-center gap-3">
          <div className={`flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ring-1 ${tone}`}>
            <Icon name={cfg.icon} className={`w-5 h-5 ${cfg.pulse ? 'motion-safe:animate-spin' : ''}`} aria-hidden="true" />
          </div>
          <div className="min-w-0">
            <div className="truncate text-sm font-medium text-surface-100">
              {formatTimestamp(entryStart(entry))}
            </div>
            {entry.card_id && (
              <div className="truncate font-mono text-xs text-surface-500">
                Card {entry.card_id}
              </div>
            )}
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-6">
          <MetaStat label="files" value={files != null ? files.toLocaleString() : '--'} />
          {bytes && <MetaStat label="size" value={bytes} />}
          <MetaStat label="time" value={formatDuration(entryDuration(entry))} />
          <div className="min-w-[100px]">
            <StatusBadge variant={cfg.variant} pulse={cfg.pulse}>
              {cfg.label}
            </StatusBadge>
          </div>
        </div>
      </div>

      {/* Mobile layout */}
      <div className="sm:hidden">
        <div className="flex items-start gap-3">
          <div className={`mt-0.5 flex h-9 w-9 shrink-0 items-center justify-center rounded-lg ring-1 ${tone}`}>
            <Icon name={cfg.icon} className={`w-5 h-5 ${cfg.pulse ? 'motion-safe:animate-spin' : ''}`} aria-hidden="true" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex min-w-0 items-center justify-between gap-2">
              <span className="min-w-0 truncate text-sm font-medium text-surface-100">
                {formatTimestamp(entryStart(entry))}
              </span>
              <StatusBadge variant={cfg.variant} pulse={cfg.pulse}>
                {cfg.label}
              </StatusBadge>
            </div>
            <div className="mt-1.5 flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 text-xs text-surface-400">
              <span className="shrink-0 tabular-nums">
                {files != null ? `${files.toLocaleString()} files` : '-- files'}
              </span>
              <span className="text-surface-600" aria-hidden="true">•</span>
              <span className="shrink-0 tabular-nums">{formatDuration(entryDuration(entry))}</span>
              {bytes && (
                <>
                  <span className="text-surface-600" aria-hidden="true">•</span>
                  <span className="shrink-0 tabular-nums">{bytes}</span>
                </>
              )}
              {entry.card_id && (
                <>
                  <span className="text-surface-600" aria-hidden="true">•</span>
                  <span className="min-w-0 max-w-full truncate font-mono">Card {entry.card_id}</span>
                </>
              )}
            </div>
          </div>
        </div>
      </div>

      {hasError && entry.error && (
        <div className="mt-3 rounded-lg border border-danger/20 bg-danger/10 px-3 py-2">
          <p className="break-words text-xs leading-relaxed text-danger">{entry.error}</p>
        </div>
      )}
    </motion.div>
  )
}

function SummaryStat({ icon, label, value }) {
  return (
    <div className="rounded-xl border border-surface-700/50 bg-surface-800/50 px-3 py-2.5">
      <div className="mb-1 flex items-center gap-1.5">
        <Icon name={icon} className="h-3.5 w-3.5 text-brand-400" aria-hidden="true" />
        <span className="truncate text-[10px] uppercase tracking-wider text-surface-500">{label}</span>
      </div>
      <p className="truncate text-base font-bold text-surface-100 tabular-nums sm:text-lg">{value}</p>
    </div>
  )
}

function HistorySkeleton() {
  return (
    <div className="space-y-2" aria-busy="true" aria-live="polite">
      <span className="sr-only">Loading sync history…</span>
      {Array.from({ length: 5 }).map((_, i) => (
        <div key={i} className="rounded-lg border border-surface-700/60 bg-surface-800/55 p-4">
          <div className="flex items-center gap-3">
            <Skeleton className="h-9 w-9 rounded-lg" />
            <div className="flex-1 space-y-2">
              <Skeleton className="h-3.5 w-40" />
              <Skeleton className="h-2.5 w-24" />
            </div>
            <Skeleton className="hidden h-6 w-20 rounded-full sm:block" />
          </div>
        </div>
      ))}
    </div>
  )
}

export default function HistoryPage() {
  const { deviceUrl } = useDevice()
  const toast = useToast()

  const [history, setHistory] = useState([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [filter, setFilter] = useState('all')
  const [search, setSearch] = useState('')
  const [error, setError] = useState(null)

  const fetchHistory = useCallback(async (isRefresh = false) => {
    if (!deviceUrl) return
    if (isRefresh) setRefreshing(true)
    else setLoading(true)

    try {
      setError(null)
      const [historyData, statusData] = await Promise.all([
        getHistory(deviceUrl),
        getStatus(deviceUrl),
      ])
      const entries = Array.isArray(historyData) ? [...historyData] : []
      if (statusData?.current_sync) {
        entries.unshift({
          ...statusData.current_sync,
          status: statusData.current_sync.status || statusData.status || 'syncing',
        })
      }
      // Sort newest first
      entries.sort((a, b) => {
        const ta = new Date(entryStart(a) || 0).getTime()
        const tb = new Date(entryStart(b) || 0).getTime()
        return tb - ta
      })
      setHistory(entries)
    } catch (err) {
      const detailed = describeError(err)
      setError(detailed)
      toast.error(`Could not load history: ${detailed}`)
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [deviceUrl, toast])

  useEffect(() => {
    fetchHistory()
  }, [fetchHistory])

  const hasActiveSync = useMemo(
    () => history.some((entry) => ['syncing', 'in-progress', 'running'].includes(entry.status)),
    [history]
  )

  useEffect(() => {
    if (!hasActiveSync) return
    const timer = window.setTimeout(() => fetchHistory(true), 4000)
    return () => window.clearTimeout(timer)
  }, [hasActiveSync, fetchHistory, history])

  const filtered = useMemo(() => {
    let result = history

    if (filter === 'success') {
      result = result.filter((e) => e.status === 'success')
    } else if (filter === 'failed') {
      result = result.filter((e) => e.status === 'failed' || e.status === 'error')
    }

    if (search.trim()) {
      const q = search.trim().toLowerCase()
      result = result.filter((e) => {
        const searchable = [
          formatTimestamp(entryStart(e)),
          e.status,
          e.card_id,
          e.remote,
          e.error,
          String(entryFileCount(e)),
        ].filter(Boolean).join(' ').toLowerCase()
        return searchable.includes(q)
      })
    }

    return result
  }, [history, filter, search])

  const counts = useMemo(() => ({
    all: history.length,
    success: history.filter((e) => e.status === 'success').length,
    failed: history.filter((e) => e.status === 'failed' || e.status === 'error').length,
  }), [history])

  const summary = useMemo(() => {
    const succeeded = history.filter((e) => e.status === 'success')
    const totalFiles = succeeded.reduce((sum, e) => sum + (entryFileCount(e) || 0), 0)
    const totalBytes = history.reduce((sum, e) => sum + (Number(e.bytes_synced) || 0), 0)
    const successRate = history.length
      ? Math.round((counts.success / history.length) * 100)
      : 0
    return { totalFiles, totalBytes, successRate }
  }, [history, counts.success])

  return (
    <div className="min-w-0 space-y-4">
      {/* Header */}
      <div className="flex flex-wrap items-end justify-between gap-2">
        <div>
          <h2 className="text-xl font-bold tracking-tight text-surface-100 sm:text-2xl">Runs</h2>
          <p className="mt-0.5 text-xs text-surface-500">Every sync the device has performed</p>
        </div>
      </div>

      {/* Aggregate summary */}
      {history.length > 0 && (
        <div className="grid grid-cols-3 gap-2 sm:gap-3">
          <SummaryStat icon="image" label="Files synced" value={summary.totalFiles.toLocaleString()} />
          <SummaryStat icon="arrow-up-tray" label="Data" value={formatBytes(summary.totalBytes) || '0 B'} />
          <SummaryStat icon="check-circle" label="Success rate" value={`${summary.successRate}%`} />
        </div>
      )}
      {/* Toolbar */}
      <div className="sticky sticky-under-header z-20 -mx-3 min-w-0 overflow-x-clip px-3 pb-3 bg-surface-950/95 backdrop-blur-sm border-b border-surface-700/30 sm:-mx-5 sm:px-5 lg:-mx-8 lg:px-8">
        {/* Search + Refresh */}
        <div className="mb-3 flex min-w-0 items-center gap-2">
          <div className="relative min-w-0 flex-1">
            <Icon
              name="magnifying"
              className="pointer-events-none absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-surface-500"
              aria-hidden="true"
            />
            <input
              type="text"
              placeholder="Search runs…"
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              aria-label="Search sync runs"
              className="w-full rounded-lg border border-surface-700/50 bg-surface-800/50 py-2 pl-9 pr-9 text-sm
                text-surface-200 outline-none transition-colors placeholder-surface-500
                focus:border-brand-500/50 focus:ring-2 focus:ring-brand-500/25"
            />
            {search && (
              <button
                type="button"
                onClick={() => setSearch('')}
                aria-label="Clear search"
                className="absolute right-2 top-1/2 -translate-y-1/2 rounded text-surface-500 transition-colors hover:text-surface-300 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/60"
              >
                <Icon name="x" className="w-4 h-4" aria-hidden="true" />
              </button>
            )}
          </div>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => fetchHistory(true)}
            loading={refreshing}
            className="shrink-0"
            aria-label="Refresh history"
          >
            <Icon name="arrow-path" className="w-4 h-4" aria-hidden="true" />
            <span className="hidden sm:inline">Refresh</span>
          </Button>
        </div>

        {/* Filter tabs */}
        <div className="flex min-w-0 gap-1 rounded-lg bg-surface-800/50 p-1" role="tablist" aria-label="Filter runs">
          {FILTER_TABS.map((tab) => {
            const active = filter === tab.id
            return (
              <button
                key={tab.id}
                type="button"
                role="tab"
                aria-selected={active}
                onClick={() => setFilter(tab.id)}
                className={`flex min-w-0 flex-1 items-center justify-center gap-1.5 rounded-md px-2 py-1.5 text-xs font-medium transition-all duration-150 sm:px-3
                  focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/60 focus-visible:ring-offset-2 focus-visible:ring-offset-surface-950
                  ${active
                    ? 'bg-brand-600/20 text-brand-300 shadow-sm'
                    : 'text-surface-400 hover:bg-surface-700/50 hover:text-surface-200'
                  }`}
              >
                <span className="min-w-0 truncate">{tab.label}</span>
                <span className={`min-w-[18px] rounded-full px-1 text-[10px] tabular-nums
                  ${active ? 'bg-brand-500/25 text-brand-200' : 'bg-surface-700/50 text-surface-400'}`}
                >
                  {counts[tab.id]}
                </span>
              </button>
            )
          })}
        </div>
      </div>

      {/* Content */}
      {loading ? (
        <HistorySkeleton />
      ) : error && history.length === 0 ? (
        <div className="py-10">
          <ErrorState
            error={error}
            title="Could not load history"
            onRetry={() => fetchHistory()}
          />
        </div>
      ) : filtered.length === 0 ? (
        history.length === 0 ? (
          <div className="py-10">
            <EmptyState
              icon="clock"
              title="No sync history yet"
              description="Once the device finishes a sync, every run will be listed here with its files, size, and duration."
              action={{ label: 'Refresh', icon: 'arrow-path', onClick: () => fetchHistory(true) }}
            />
          </div>
        ) : (
          <div className="flex flex-col items-center justify-center px-4 py-16 text-center">
            <div className="mb-3 flex h-12 w-12 items-center justify-center rounded-xl border border-surface-700/50 bg-surface-800/80">
              <Icon name="magnifying" className="h-6 w-6 text-surface-500" aria-hidden="true" />
            </div>
            <h3 className="mb-1 text-sm font-semibold text-surface-300">No matching results</h3>
            <p className="text-xs text-surface-500">Try adjusting your filter or search term.</p>
            {(search || filter !== 'all') && (
              <Button
                variant="ghost"
                size="sm"
                className="mt-3"
                onClick={() => { setSearch(''); setFilter('all') }}
              >
                <Icon name="x" className="w-4 h-4" aria-hidden="true" />
                Clear filters
              </Button>
            )}
          </div>
        )
      ) : (
        <motion.div layout className="min-w-0 space-y-2">
          <AnimatePresence initial={false} mode="popLayout">
            {filtered.map((entry, i) => (
              <HistoryEntry key={entry.id || entry.timestamp || entryStart(entry) || i} entry={entry} index={i} />
            ))}
          </AnimatePresence>
        </motion.div>
      )}
    </div>
  )
}
