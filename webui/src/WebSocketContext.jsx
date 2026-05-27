import { createContext, useContext, useEffect, useRef, useState, useCallback } from 'react'
import { useDevice } from './DeviceContext.jsx'
import { getWSToken, getWebSocketUrl } from './api.js'

const WebSocketContext = createContext({
  status: null,
  setStatus: () => {},
  wsConnected: false,
  wsError: null,
  dmesgLines: [],
})

export function useWebSocket() {
  return useContext(WebSocketContext)
}

const MAX_DMESG_LINES = 2000

function isSameSync(prevSync, nextSync) {
  if (!prevSync || !nextSync) return false
  if (prevSync.id && nextSync.id) return prevSync.id === nextSync.id
  if (prevSync.start_time && nextSync.start_time) return prevSync.start_time === nextSync.start_time
  return Boolean(
    prevSync.card_id &&
    nextSync.card_id &&
    prevSync.card_id === nextSync.card_id &&
    prevSync.files_total === nextSync.files_total
  )
}

function mergeStatusProgress(prevStatus, nextStatus) {
  if (!prevStatus || !nextStatus) return nextStatus

  const prevSync = prevStatus.current_sync
  const nextSync = nextStatus.current_sync
  if (nextStatus.status !== 'syncing' || !isSameSync(prevSync, nextSync)) {
    return nextStatus
  }

  const prevFiles = Number(prevSync.files_synced || 0)
  const nextFiles = Number(nextSync.files_synced || 0)
  const prevBytes = Number(prevSync.bytes_synced || 0)
  const nextBytes = Number(nextSync.bytes_synced || 0)

  if (nextFiles < prevFiles || nextBytes < prevBytes) {
    return {
      ...nextStatus,
      current_sync: prevSync,
    }
  }

  return nextStatus
}

export function WebSocketProvider({ children }) {
  const { deviceUrl } = useDevice()
  const [status, setStatus] = useState(null)
  const [wsConnected, setWsConnected] = useState(false)
  const [wsError, setWsError] = useState(null)
  const [dmesgLines, setDmesgLines] = useState([])
  const consecutiveErrorsRef = useRef(0)
  const wsReconnectRef = useRef(null)
  const dmesgPausedRef = useRef(false)

  const addDmesgLine = useCallback((line) => {
    if (dmesgPausedRef.current) return
    setDmesgLines((prev) => {
      if (prev.length >= MAX_DMESG_LINES) {
        return [...prev.slice(1), line]
      }
      return [...prev, line]
    })
  }, [])

  const clearDmesg = useCallback(() => {
    setDmesgLines([])
  }, [])

  const setDmesgPaused = useCallback((paused) => {
    dmesgPausedRef.current = paused
  }, [])

  useEffect(() => {
    if (!deviceUrl) return undefined

    let cancelled = false
    let reconnectAttempts = 0
    let socket = null

    const scheduleReconnect = () => {
      if (cancelled) return
      const delay = Math.min(1000 * Math.pow(2, reconnectAttempts), 15000)
      reconnectAttempts += 1
      wsReconnectRef.current = window.setTimeout(connect, delay)
    }

    const connect = async () => {
      try {
        const tokenData = await getWSToken(deviceUrl)
        if (cancelled) return

        socket = new WebSocket(getWebSocketUrl(deviceUrl))
        socket.onopen = () => {
          reconnectAttempts = 0
          socket.send(JSON.stringify({ type: 'auth', token: tokenData.ws_token }))
          if (!cancelled) {
            setWsConnected(true)
            setWsError(null)
          }
        }
        socket.onmessage = (event) => {
          let message = null
          try {
            message = JSON.parse(event.data)
          } catch {
            return
          }

          if (message.type === 'state' && message.data) {
            if (!cancelled) {
              setStatus((currentStatus) => mergeStatusProgress(currentStatus, message.data))
              setWsError(null)
              consecutiveErrorsRef.current = 0
            }
          } else if (message.type === 'dmesg' && message.data) {
            if (!cancelled) {
              addDmesgLine(message.data)
            }
          }
        }
        socket.onclose = () => {
          if (!cancelled) setWsConnected(false)
          scheduleReconnect()
        }
        socket.onerror = () => socket?.close()
      } catch (err) {
        if (!cancelled) {
          setWsConnected(false)
          setWsError(err?.message || 'WebSocket connection failed')
        }
        scheduleReconnect()
      }
    }

    connect()

    return () => {
      cancelled = true
      if (wsReconnectRef.current) window.clearTimeout(wsReconnectRef.current)
      if (socket) socket.close()
      setWsConnected(false)
    }
  }, [deviceUrl, addDmesgLine])

  const value = {
    status,
    setStatus,
    wsConnected,
    wsError,
    dmesgLines,
    clearDmesg,
    setDmesgPaused,
  }

  return (
    <WebSocketContext.Provider value={value}>
      {!wsConnected && deviceUrl && (
        <div
          role="status"
          aria-live="polite"
          className="fixed top-0 left-0 right-0 z-50 flex items-center justify-center gap-2 px-4 py-2 bg-warning/90 text-surface-900 text-xs font-medium backdrop-blur-sm"
        >
          <span className="inline-block w-2 h-2 rounded-full bg-surface-900 opacity-70 animate-pulse" aria-hidden="true" />
          Connection lost — retrying…
        </div>
      )}
      {children}
    </WebSocketContext.Provider>
  )
}
