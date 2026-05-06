import { useState, useEffect, useCallback, useMemo } from 'react'

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
import { getHistory, getStatus } from '../api.js'
import { Card, CardHeader, CardTitle } from '../components/Card.jsx'
import { StatusBadge } from '../components/StatusBadge.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { LoadingSpinner, PageLoader } from '../components/LoadingSpinner.jsx'
import { useToast } from '../components/Toast.jsx'

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

function HistoryEntry({ entry }) {
  const cfg = statusConfig(entry.status)
  const hasError = entry.status === 'failed' || entry.status === 'error'

  return (
    <Card className="transition-all duration-200 hover:bg-surface-800/80">
      {/* Desktop layout */}
      <div className="hidden sm:flex items-center gap-4">
        <div className="flex items-center gap-3 min-w-0 flex-1">
          <div className={`w-9 h-9 rounded-lg flex items-center justify-center shrink-0
            ${cfg.variant === 'success' ? 'bg-success/15 text-success' :
              cfg.variant === 'danger' ? 'bg-danger/15 text-danger' :
              cfg.variant === 'info' ? 'bg-info/15 text-info' :
              'bg-surface-700/50 text-surface-400'}`}
          >
            <Icon name={cfg.icon} className="w-5 h-5" />
          </div>
          <div className="min-w-0">
            <div className="text-sm font-medium text-surface-100 truncate">
              {formatTimestamp(entryStart(entry))}
            </div>
            {entry.card_id && (
              <div className="text-xs text-surface-500 truncate">
                Card: {entry.card_id}
              </div>
            )}
          </div>
        </div>

        <div className="flex items-center gap-6 shrink-0">
          <div className="text-right min-w-[60px]">
            <div className="text-sm font-medium text-surface-200">
              {entryFileCount(entry) != null ? entryFileCount(entry).toLocaleString() : '--'}
            </div>
            <div className="text-[10px] text-surface-500 uppercase tracking-wider">files</div>
          </div>

          <div className="text-right min-w-[50px]">
            <div className="text-sm text-surface-300">
              {formatDuration(entryDuration(entry))}
            </div>
            <div className="text-[10px] text-surface-500 uppercase tracking-wider">time</div>
          </div>

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
          <div className={`w-9 h-9 rounded-lg flex items-center justify-center shrink-0 mt-0.5
            ${cfg.variant === 'success' ? 'bg-success/15 text-success' :
              cfg.variant === 'danger' ? 'bg-danger/15 text-danger' :
              cfg.variant === 'info' ? 'bg-info/15 text-info' :
              'bg-surface-700/50 text-surface-400'}`}
          >
            <Icon name={cfg.icon} className="w-5 h-5" />
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center justify-between gap-2">
              <span className="text-sm font-medium text-surface-100 truncate">
                {formatTimestamp(entryStart(entry))}
              </span>
              <StatusBadge variant={cfg.variant} pulse={cfg.pulse}>
                {cfg.label}
              </StatusBadge>
            </div>
            <div className="flex items-center gap-4 mt-1.5 text-xs text-surface-400">
              <span>
                {entryFileCount(entry) != null ? `${entryFileCount(entry).toLocaleString()} files` : '-- files'}
              </span>
              <span className="text-surface-600">|</span>
              <span>{formatDuration(entryDuration(entry))}</span>
              {entry.card_id && (
                <>
                  <span className="text-surface-600">|</span>
                  <span className="truncate">Card {entry.card_id}</span>
                </>
              )}
            </div>
          </div>
        </div>
      </div>

      {hasError && entry.error && (
        <div className="mt-3 px-3 py-2 rounded-lg bg-danger/10 border border-danger/20">
          <p className="text-xs text-danger leading-relaxed">{entry.error}</p>
        </div>
      )}
    </Card>
  )
}

function EmptyState() {
  return (
    <div className="flex flex-col items-center justify-center py-16 px-4">
      <div className="w-16 h-16 rounded-2xl bg-surface-800/80 border border-surface-700/50 flex items-center justify-center mb-4">
        <Icon name="clock" className="w-8 h-8 text-surface-500" />
      </div>
      <h3 className="text-base font-semibold text-surface-300 mb-1">No Sync History</h3>
      <p className="text-sm text-surface-500 text-center max-w-xs">
        Sync runs will appear here once your first backup completes.
      </p>
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

  return (
    <div className="space-y-4">
      {/* Toolbar */}
      <div className="sticky sticky-under-header z-20 -mx-3 px-3 pb-3 bg-surface-950/95 backdrop-blur-sm border-b border-surface-700/30 sm:-mx-5 sm:px-5 lg:-mx-8 lg:px-8">
        {/* Search + Refresh */}
        <div className="flex items-center gap-2 mb-3">
          <div className="relative flex-1">
            <Icon
              name="magnifying"
              className="absolute left-3 top-1/2 -translate-y-1/2 w-4 h-4 text-surface-500 pointer-events-none"
            />
            <input
              type="text"
              placeholder="Search history..."
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              className="w-full pl-9 pr-3 py-2 text-sm bg-surface-800/50 border border-surface-700/50 rounded-lg
                text-surface-200 placeholder-surface-500 outline-none focus:border-brand-500/50
                focus:ring-1 focus:ring-brand-500/20 transition-colors"
            />
            {search && (
              <button
                onClick={() => setSearch('')}
                className="absolute right-2 top-1/2 -translate-y-1/2 text-surface-500 hover:text-surface-300 transition-colors"
              >
                <Icon name="x" className="w-4 h-4" />
              </button>
            )}
          </div>
          <Button
            variant="secondary"
            size="sm"
            onClick={() => fetchHistory(true)}
            loading={refreshing}
            className="shrink-0"
          >
            <Icon name="arrow-path" className={`w-4 h-4 ${refreshing ? 'animate-spin' : ''}`} />
            <span className="hidden sm:inline">Refresh</span>
          </Button>
        </div>

        {/* Filter tabs */}
        <div className="flex gap-1 p-1 bg-surface-800/50 rounded-lg">
          {FILTER_TABS.map((tab) => (
            <button
              key={tab.id}
              onClick={() => setFilter(tab.id)}
              className={`flex-1 flex items-center justify-center gap-1.5 px-3 py-1.5 text-xs font-medium rounded-md
                transition-all duration-150
                ${filter === tab.id
                  ? 'bg-brand-600/20 text-brand-400 shadow-sm'
                  : 'text-surface-400 hover:text-surface-200 hover:bg-surface-700/50'
                }`}
            >
              {tab.label}
              <span className={`text-[10px] px-1 py-0 rounded-full min-w-[18px]
                ${filter === tab.id ? 'bg-brand-600/20' : 'bg-surface-700/50'}`}
              >
                {counts[tab.id]}
              </span>
            </button>
          ))}
        </div>
      </div>

      {/* Content */}
      {loading ? (
        <PageLoader />
      ) : error && history.length === 0 ? (
        <Card className="text-center py-12">
          <Icon name="exclamation-triangle" className="w-12 h-12 text-danger mx-auto mb-3" />
          <p className="text-surface-200 text-sm font-medium">Could not load history</p>
          <p className="mx-auto mt-2 max-w-md text-xs text-surface-500">{error}</p>
          <Button variant="secondary" size="sm" className="mt-4" onClick={() => fetchHistory()}>
            <Icon name="arrow-path" className="w-4 h-4" />
            Retry
          </Button>
        </Card>
      ) : filtered.length === 0 ? (
        history.length === 0 ? (
          <EmptyState />
        ) : (
          <div className="flex flex-col items-center justify-center py-16 px-4">
            <div className="w-12 h-12 rounded-xl bg-surface-800/80 border border-surface-700/50 flex items-center justify-center mb-3">
              <Icon name="magnifying" className="w-6 h-6 text-surface-500" />
            </div>
            <h3 className="text-sm font-semibold text-surface-300 mb-1">No Matching Results</h3>
            <p className="text-xs text-surface-500 text-center">
              Try adjusting your filter or search term.
            </p>
          </div>
        )
      ) : (
        <div className="space-y-2 transition-all duration-200">
          {filtered.map((entry) => (
            <HistoryEntry key={entry.id || entry.timestamp} entry={entry} />
          ))}
        </div>
      )}
    </div>
  )
}
