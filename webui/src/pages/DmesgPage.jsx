import { useState, useEffect, useRef, useCallback, useMemo } from 'react'
import { useWebSocket } from '../WebSocketContext.jsx'
import { Card, CardHeader, CardTitle } from '../components/Card.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'
import { EmptyState } from '../components/EmptyState.jsx'

// Per-level presentation: text color, severity dot color, short name, and the
// severity "group" used by the quick filter (error / warn / info / debug).
const LEVELS = {
  0: { name: 'EMERG', text: 'text-danger', dot: 'bg-danger', group: 'error' },
  1: { name: 'ALERT', text: 'text-danger', dot: 'bg-danger', group: 'error' },
  2: { name: 'CRIT', text: 'text-danger', dot: 'bg-danger', group: 'error' },
  3: { name: 'ERR', text: 'text-danger', dot: 'bg-danger', group: 'error' },
  4: { name: 'WARN', text: 'text-warning', dot: 'bg-warning', group: 'warn' },
  5: { name: 'NOTICE', text: 'text-brand-300', dot: 'bg-brand-400', group: 'info' },
  6: { name: 'INFO', text: 'text-surface-300', dot: 'bg-surface-400', group: 'info' },
  7: { name: 'DEBUG', text: 'text-surface-500', dot: 'bg-surface-600', group: 'debug' },
}

const DEFAULT_LEVEL = { name: '', text: 'text-surface-300', dot: 'bg-surface-500', group: 'info' }

const SEVERITY_FILTERS = [
  { key: 'all', label: 'All', dot: 'bg-surface-400' },
  { key: 'error', label: 'Errors', dot: 'bg-danger' },
  { key: 'warn', label: 'Warnings', dot: 'bg-warning' },
  { key: 'info', label: 'Info', dot: 'bg-brand-400' },
  { key: 'debug', label: 'Debug', dot: 'bg-surface-600' },
]

function levelInfo(level) {
  return LEVELS[level] || DEFAULT_LEVEL
}

function formatDmesgTime(ts) {
  if (!ts) return '--:--:--'
  const d = new Date(ts)
  return d.toLocaleTimeString('en-US', { hour12: false })
}

export default function DmesgPage() {
  const { dmesgLines, wsConnected, clearDmesg, setDmesgPaused } = useWebSocket()
  const [paused, setPaused] = useState(false)
  const [follow, setFollow] = useState(true)
  const [filter, setFilter] = useState('')
  const [severity, setSeverity] = useState('all')
  const scrollRef = useRef(null)
  const wasNearBottomRef = useRef(true)

  const text = filter.trim().toLowerCase()
  const filteredLines = useMemo(() => {
    return dmesgLines.filter((l) => {
      if (severity !== 'all' && levelInfo(l.level).group !== severity) return false
      if (text && !l.text?.toLowerCase().includes(text)) return false
      return true
    })
  }, [dmesgLines, text, severity])

  // Count lines per severity group for the filter badges.
  const counts = useMemo(() => {
    const c = { all: dmesgLines.length, error: 0, warn: 0, info: 0, debug: 0 }
    for (const l of dmesgLines) c[levelInfo(l.level).group]++
    return c
  }, [dmesgLines])

  // Auto-scroll to bottom when new lines arrive (unless paused or user scrolled up)
  useEffect(() => {
    if (paused || !follow || !scrollRef.current) return
    const el = scrollRef.current
    el.scrollTop = el.scrollHeight
  }, [filteredLines, paused, follow])

  // Track whether user is near bottom
  const handleScroll = useCallback(() => {
    const el = scrollRef.current
    if (!el) return
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 50
    wasNearBottomRef.current = nearBottom
    if (nearBottom && !follow) setFollow(true)
  }, [follow])

  const handleTogglePause = useCallback(() => {
    setPaused((p) => {
      const next = !p
      setDmesgPaused(next)
      return next
    })
  }, [setDmesgPaused])

  const handleToggleFollow = useCallback(() => {
    setFollow((f) => {
      const next = !f
      if (next && scrollRef.current) {
        scrollRef.current.scrollTop = scrollRef.current.scrollHeight
      }
      return next
    })
  }, [])

  const handleClear = useCallback(() => {
    clearDmesg()
  }, [clearDmesg])

  const hasActiveFilter = text.length > 0 || severity !== 'all'

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2.5">
          <span className="flex h-9 w-9 items-center justify-center rounded-lg bg-brand-500/15 text-brand-300 ring-1 ring-inset ring-brand-400/20">
            <Icon name="terminal" className="h-5 w-5" aria-hidden="true" />
          </span>
          <div>
            <h2 className="text-lg font-semibold leading-tight text-surface-100">Kernel Log</h2>
            <p className="text-xs text-surface-500">Live kernel ring buffer (dmesg / kmsg)</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <span
            className={`inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs font-medium ring-1 ring-inset ${
              wsConnected
                ? 'bg-success/10 text-success ring-success/25'
                : 'bg-warning/10 text-warning ring-warning/25'
            }`}
          >
            <span className="relative flex h-2 w-2" aria-hidden="true">
              {wsConnected && (
                <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-75" />
              )}
              <span className={`relative inline-flex h-2 w-2 rounded-full ${wsConnected ? 'bg-success' : 'bg-warning'}`} />
            </span>
            {wsConnected ? 'Connected' : 'Reconnecting…'}
          </span>
          <Button
            variant={paused ? 'secondary' : 'ghost'}
            size="sm"
            onClick={handleTogglePause}
            aria-pressed={paused}
            aria-label={paused ? 'Resume' : 'Pause'}
          >
            <Icon name={paused ? 'play' : 'stop'} className="h-4 w-4" aria-hidden="true" />
            {paused ? 'Resume' : 'Pause'}
          </Button>
          <Button
            variant={follow ? 'secondary' : 'ghost'}
            size="sm"
            onClick={handleToggleFollow}
            aria-pressed={follow}
            aria-label={follow ? 'Following output' : 'Follow output'}
          >
            <Icon name="arrow-down" className="h-4 w-4" aria-hidden="true" />
            {follow ? 'Following' : 'Follow'}
          </Button>
          <Button variant="ghost" size="sm" onClick={handleClear} aria-label="Clear log">
            <Icon name="trash" className="h-4 w-4" aria-hidden="true" />
            Clear
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Output</CardTitle>
          <span className="text-xs tabular-nums text-surface-500">
            <span className="text-surface-300">{filteredLines.length.toLocaleString()}</span>
            {filteredLines.length !== dmesgLines.length && (
              <span> / {dmesgLines.length.toLocaleString()}</span>
            )}{' '}
            lines
            {paused && <span className="ml-1 text-amber-400">· paused</span>}
          </span>
        </CardHeader>

        <div className="space-y-3">
          <div className="flex flex-wrap items-center gap-2">
            {/* Severity quick filter */}
            <div
              className="flex flex-wrap items-center gap-1 rounded-lg border border-surface-700/60 bg-surface-800/60 p-1"
              role="group"
              aria-label="Filter by severity"
            >
              {SEVERITY_FILTERS.map((f) => {
                const active = severity === f.key
                return (
                  <button
                    key={f.key}
                    type="button"
                    aria-pressed={active}
                    onClick={() => setSeverity(f.key)}
                    className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1 text-xs font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 ${
                      active
                        ? 'bg-brand-500/20 text-brand-200 ring-1 ring-inset ring-brand-400/40'
                        : 'text-surface-400 hover:bg-surface-700/50 hover:text-surface-100'
                    }`}
                  >
                    <span className={`h-1.5 w-1.5 rounded-full ${f.dot}`} aria-hidden="true" />
                    {f.label}
                    <span className="tabular-nums text-surface-500">{counts[f.key] ?? 0}</span>
                  </button>
                )
              })}
            </div>

            {/* Text filter */}
            <div className="relative min-w-[12rem] flex-1">
              <span
                className="pointer-events-none absolute left-3 top-1/2 -translate-y-1/2 text-surface-500"
                aria-hidden="true"
              >
                <Icon name="magnifying" className="h-4 w-4" />
              </span>
              <input
                type="text"
                value={filter}
                onChange={(e) => setFilter(e.target.value)}
                placeholder="Filter lines…"
                aria-label="Filter log lines by text"
                className="w-full rounded-lg border border-surface-700 bg-surface-950 py-2 pl-9 pr-9 text-sm text-surface-100 placeholder:text-surface-500 focus:border-brand-400/50 focus:outline-none focus:ring-2 focus:ring-brand-400/50"
              />
              {filter && (
                <button
                  type="button"
                  onClick={() => setFilter('')}
                  aria-label="Clear filter"
                  className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-surface-500 transition-colors hover:text-surface-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
                >
                  <Icon name="x" className="h-3.5 w-3.5" />
                </button>
              )}
            </div>
          </div>

          <div className="relative">
            <div
              ref={scrollRef}
              onScroll={handleScroll}
              tabIndex={0}
              role="log"
              aria-label="Kernel log output"
              aria-live={paused ? 'off' : 'polite'}
              className="h-[60vh] overflow-auto rounded-lg border border-surface-700/80 bg-surface-950 p-3 font-mono text-xs leading-relaxed focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/40"
            >
              {filteredLines.length === 0 ? (
                <div className="flex h-full items-center justify-center">
                  {dmesgLines.length === 0 ? (
                    <EmptyState
                      icon="terminal"
                      title={wsConnected ? 'Waiting for kernel messages' : 'Connecting…'}
                      description={
                        wsConnected
                          ? 'New kernel log lines will stream in here as they arrive.'
                          : 'Establishing the live WebSocket connection to the device.'
                      }
                    />
                  ) : (
                    <EmptyState
                      icon="magnifying"
                      title="No matching lines"
                      description="No log lines match the current filter. Try a different term or severity."
                      action={
                        hasActiveFilter
                          ? {
                              label: 'Clear filters',
                              onClick: () => {
                                setFilter('')
                                setSeverity('all')
                              },
                            }
                          : undefined
                      }
                    />
                  )}
                </div>
              ) : (
                <div role="presentation">
                  {filteredLines.map((line, index) => {
                    const info = levelInfo(line.level)
                    const time = formatDmesgTime(line.timestamp)
                    return (
                      <div
                        key={`${line.timestamp}-${index}`}
                        className="group flex items-start gap-2.5 rounded px-1.5 py-0.5 hover:bg-surface-800/60"
                      >
                        <span
                          className={`mt-1.5 h-1.5 w-1.5 shrink-0 rounded-full ${info.dot}`}
                          aria-hidden="true"
                        />
                        <span className="shrink-0 select-none tabular-nums text-surface-600">{time}</span>
                        {info.name && (
                          <span className={`w-14 shrink-0 select-none text-right font-semibold ${info.text}`}>
                            {info.name}
                          </span>
                        )}
                        <span className={`whitespace-pre-wrap break-all ${info.text}`}>{line.text}</span>
                      </div>
                    )
                  })}
                </div>
              )}
            </div>

            {!follow && filteredLines.length > 0 && (
              <button
                type="button"
                onClick={handleToggleFollow}
                className="absolute bottom-3 right-3 inline-flex items-center gap-1.5 rounded-full bg-brand-500/90 px-3 py-1.5 text-xs font-medium text-white shadow-lg shadow-brand-900/40 ring-1 ring-inset ring-white/15 backdrop-blur transition-colors hover:bg-brand-400 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-300"
              >
                <Icon name="arrow-down" className="h-3.5 w-3.5" aria-hidden="true" />
                Jump to latest
              </button>
            )}
          </div>
        </div>
      </Card>
    </div>
  )
}
