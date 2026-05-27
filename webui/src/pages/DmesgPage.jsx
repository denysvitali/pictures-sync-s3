import { useState, useEffect, useRef, useCallback } from 'react'
import { useWebSocket } from '../WebSocketContext.jsx'
import { Card, CardHeader, CardTitle } from '../components/Card.jsx'
import { Button } from '../components/Button.jsx'
import { Icon } from '../components/Icons.jsx'

const LEVEL_COLORS = {
  0: 'text-danger',      // EMERG
  1: 'text-danger',      // ALERT
  2: 'text-danger',      // CRIT
  3: 'text-danger',      // ERR
  4: 'text-warning',     // WARN
  5: 'text-brand-300',   // NOTICE
  6: 'text-surface-300', // INFO
  7: 'text-surface-500', // DEBUG
}

const LEVEL_NAMES = {
  0: 'EMERG',
  1: 'ALERT',
  2: 'CRIT',
  3: 'ERR',
  4: 'WARN',
  5: 'NOTICE',
  6: 'INFO',
  7: 'DEBUG',
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
  const scrollRef = useRef(null)
  const wasNearBottomRef = useRef(true)

  const filteredLines = filter.trim()
    ? dmesgLines.filter((l) => l.text?.toLowerCase().includes(filter.toLowerCase()))
    : dmesgLines

  // Auto-scroll to bottom when new lines arrive (unless paused or user scrolled up)
  useEffect(() => {
    if (paused || !follow || !scrollRef.current) return
    const el = scrollRef.current
    el.scrollTop = el.scrollHeight
  }, [dmesgLines, paused, follow])

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

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <h2 className="text-lg font-semibold text-surface-100">Kernel Log</h2>
        <div className="flex items-center gap-2">
          {!wsConnected && (
            <span className="text-xs text-warning">Reconnecting…</span>
          )}
          <Button
            variant="ghost"
            size="sm"
            onClick={handleTogglePause}
            aria-label={paused ? 'Resume' : 'Pause'}
          >
            <Icon name={paused ? 'play' : 'stop'} className="w-4 h-4" aria-hidden="true" />
            {paused ? 'Resume' : 'Pause'}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={handleToggleFollow}
            aria-label={follow ? 'Unfollow' : 'Follow'}
          >
            <Icon name={follow ? 'arrow-down' : 'arrow-down'} className="w-4 h-4" aria-hidden="true" />
            {follow ? 'Following' : 'Follow'}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={handleClear}
            aria-label="Clear log"
          >
            <Icon name="trash" className="w-4 h-4" aria-hidden="true" />
            Clear
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>dmesg / kmsg</CardTitle>
          <span className="text-xs text-surface-500">
            {dmesgLines.length.toLocaleString()} lines
            {paused && ' · paused'}
          </span>
        </CardHeader>

        <div className="space-y-3">
          <input
            type="text"
            value={filter}
            onChange={(e) => setFilter(e.target.value)}
            placeholder="Filter lines…"
            className="w-full bg-surface-950 border border-surface-700 rounded-lg px-3 py-2 text-sm text-surface-100 placeholder:text-surface-500 focus:outline-none focus:ring-2 focus:ring-brand-400/50 focus:border-brand-400/50"
          />

          <div
            ref={scrollRef}
            onScroll={handleScroll}
            className="h-[60vh] overflow-auto rounded-lg border border-surface-700 bg-surface-950 p-3 font-mono text-xs leading-relaxed"
          >
            {filteredLines.length === 0 ? (
              <p className="text-surface-600 text-center py-8">
                {dmesgLines.length === 0
                  ? wsConnected
                    ? 'Waiting for kernel messages…'
                    : 'Connecting to WebSocket…'
                  : 'No lines match the filter.'}
              </p>
            ) : (
              <div className="space-y-0.5">
                {filteredLines.map((line, index) => {
                  const levelColor = LEVEL_COLORS[line.level] || 'text-surface-300'
                  const levelName = LEVEL_NAMES[line.level] || ''
                  const time = formatDmesgTime(line.timestamp)
                  return (
                    <div key={`${line.timestamp}-${index}`} className="flex gap-2">
                      <span className="shrink-0 text-surface-600 select-none">
                        {time}
                      </span>
                      {levelName && (
                        <span className={`shrink-0 w-14 text-right select-none ${levelColor}`}>
                          {levelName}
                        </span>
                      )}
                      <span className={`break-all ${levelColor}`}>
                        {line.text}
                      </span>
                    </div>
                  )
                })}
              </div>
            )}
          </div>
        </div>
      </Card>
    </div>
  )
}
