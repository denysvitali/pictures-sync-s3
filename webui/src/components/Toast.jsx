import { useState, useCallback, useRef, useMemo, createContext, useContext, useEffect } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { Icon } from './Icons.jsx'

const ToastContext = createContext(null)

let toastId = 0

const DEDUP_WINDOW_MS = 5000

const variantIcons = {
  success: 'check-circle',
  danger: 'x-circle',
  warning: 'alert-triangle',
  info: 'info-circle',
}

const variantStyles = {
  success: 'border-success/30 bg-success/10 text-success',
  danger: 'border-danger/30 bg-danger/10 text-danger',
  warning: 'border-warning/30 bg-warning/10 text-warning',
  info: 'border-info/30 bg-info/10 text-info',
}

const progressColors = {
  success: 'bg-success',
  danger: 'bg-danger',
  warning: 'bg-warning',
  info: 'bg-info',
}

export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([])
  const recentRef = useRef(new Map())

  const addToast = useCallback((message, variant = 'info', duration = 4000, undo) => {
    const now = Date.now()
    const key = `${variant}:${message}`

    const lastTime = recentRef.current.get(key)
    if (variant !== 'danger' && lastTime && now - lastTime < DEDUP_WINDOW_MS) {
      return null
    }
    recentRef.current.set(key, now)

    const id = ++toastId
    setToasts((prev) => [...prev, { id, message, variant, duration, undo, createdAt: now }])
    if (duration > 0) {
      setTimeout(() => {
        setToasts((prev) => prev.filter((t) => t.id !== id))
      }, duration)
    }
    return id
  }, [])

  const removeToast = useCallback((id) => {
    setToasts((prev) => prev.filter((t) => t.id !== id))
  }, [])

  const toast = useMemo(() => ({
    success: (msg, opts) => addToast(msg, 'success', opts?.duration, opts?.undo),
    error: (msg, opts) => addToast(msg, 'danger', opts?.duration ?? 6000, opts?.undo),
    info: (msg, opts) => addToast(msg, 'info', opts?.duration, opts?.undo),
    warning: (msg, opts) => addToast(msg, 'warning', opts?.duration ?? 5000, opts?.undo),
  }), [addToast])

  return (
    <ToastContext.Provider value={toast}>
      {children}
      <div
        className="fixed toast-bottom-safe left-3 right-3 z-50 flex flex-col gap-2 sm:left-auto sm:right-4 sm:w-full sm:max-w-sm lg:bottom-4"
        aria-label="Notifications"
      >
        <AnimatePresence mode="popLayout">
          {toasts.map((t) => {
            const isError = t.variant === 'danger'
            const isWarning = t.variant === 'warning'
            const assertive = isError || isWarning
            return (
              <ToastItem
                key={t.id}
                toast={t}
                assertive={assertive}
                onDismiss={() => removeToast(t.id)}
              />
            )
          })}
        </AnimatePresence>
      </div>
    </ToastContext.Provider>
  )
}

function ToastItem({ toast, assertive, onDismiss }) {
  const [progress, setProgress] = useState(100)
  const rafRef = useRef(null)
  const startRef = useRef(Date.now())

  useEffect(() => {
    if (toast.duration <= 0) return
    startRef.current = Date.now()

    const tick = () => {
      const elapsed = Date.now() - startRef.current
      const remaining = Math.max(0, toast.duration - elapsed)
      setProgress((remaining / toast.duration) * 100)
      if (remaining > 0) {
        rafRef.current = requestAnimationFrame(tick)
      }
    }
    rafRef.current = requestAnimationFrame(tick)
    return () => {
      if (rafRef.current) cancelAnimationFrame(rafRef.current)
    }
  }, [toast.duration])

  return (
    <motion.div
      role={assertive ? 'alert' : 'status'}
      aria-live={assertive ? 'assertive' : 'polite'}
      aria-atomic="true"
      initial={{ opacity: 0, x: 60, scale: 0.95 }}
      animate={{ opacity: 1, x: 0, scale: 1 }}
      exit={{ opacity: 0, x: 60, scale: 0.95 }}
      transition={{ type: 'spring', stiffness: 400, damping: 30 }}
      layout
      className={`relative flex flex-col gap-2 rounded-lg border text-sm font-medium overflow-hidden ${variantStyles[toast.variant] || variantStyles.info}`}
    >
      <div className="flex items-center gap-2 px-4 pt-3 pb-1">
        <Icon name={variantIcons[toast.variant] || 'info-circle'} className="w-5 h-5 shrink-0" aria-hidden="true" />
        <span className="flex-1 break-words">{toast.message}</span>
        {toast.undo && (
          <button
            onClick={() => {
              toast.undo()
              onDismiss()
            }}
            className="shrink-0 text-xs font-semibold underline underline-offset-2 opacity-80 hover:opacity-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 rounded px-1"
          >
            Undo
          </button>
        )}
        <button
          onClick={onDismiss}
          className="opacity-60 hover:opacity-100 shrink-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 rounded"
          aria-label="Dismiss notification"
        >
          <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor" aria-hidden="true">
            <path strokeLinecap="round" strokeLinejoin="round" d="M6 18 18 6M6 6l12 12" />
          </svg>
        </button>
      </div>
      {toast.duration > 0 && (
        <div className="h-0.5 w-full bg-current/10">
          <div
            className={`h-full ${progressColors[toast.variant] || progressColors.info} transition-none`}
            style={{ width: `${progress}%` }}
            aria-hidden="true"
          />
        </div>
      )}
      {toast.duration <= 0 && <div className="h-0.5" />}
    </motion.div>
  )
}

export function useToast() {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used within ToastProvider')
  return ctx
}
