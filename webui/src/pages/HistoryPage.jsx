import { useState, useEffect, useCallback, useMemo } from 'react'
import { useDevice } from '../DeviceContext.jsx'
import { getHistory } from '../api.js'
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
              {formatTimestamp(entry.timestamp)}
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
              {entry.photos_synced != null ? entry.photos_synced.toLocaleString() : '--'}
            </div>
            <div className="text-[10px] text-surface-500 uppercase tracking-wider">photos</div>
          </div>

          <div className="text-right min-w-[50px]">
            <div className="text-sm text-surface-300">
              {formatDuration(entry.duration)}
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
                {formatTimestamp(entry.timestamp)}
              </span>
              <StatusBadge variant={cfg.variant} pulse={cfg.pulse}>
                {cfg.label}
              </StatusBadge>
            </div>
            <div className="flex items-center gap-4 mt-1.5 text-xs text-surface-400">
              <span>
                {entry.photos_synced != null ? `${entry.photos_synced.toLocaleString()} photos` : '-- photos'}
              </span>
              <span className="text-surface-600">|</span>
              <span>{formatDuration(entry.duration)}</span>
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

  const fetchHistory = useCallback(async (isRefresh = false) => {
    if (!deviceUrl) return
    if (isRefresh) setRefreshing(true)
    else setLoading(true)

    try {
      const data = await getHistory(deviceUrl)
      const entries = Array.isArray(data) ? data : []
      // Sort newest first
      entries.sort((a, b) => {
        const ta = new Date(a.timestamp || 0).getTime()
        const tb = new Date(b.timestamp || 0).getTime()
        return tb - ta
      })
      setHistory(entries)
    } catch (err) {
      toast.error(`Failed to load history: ${err.message}`)
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [deviceUrl, toast])

  useEffect(() => {
    fetchHistory()
  }, [fetchHistory])

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
          formatTimestamp(e.timestamp),
          e.status,
          e.card_id,
          e.remote,
          e.error,
          String(e.photos_synced),
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
      <div className="sticky top-[57px] z-20 -mx-4 px-4 pb-3 bg-surface-950/95 backdrop-blur-sm border-b border-surface-700/30">
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
