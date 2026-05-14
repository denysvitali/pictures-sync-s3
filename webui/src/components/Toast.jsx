import { useState, useCallback, useRef, useMemo, createContext, useContext } from 'react'

const ToastContext = createContext(null)

let toastId = 0

const DEDUP_WINDOW_MS = 5000

export function ToastProvider({ children }) {
  const [toasts, setToasts] = useState([])
  const recentRef = useRef(new Map())

  const addToast = useCallback((message, variant = 'info', duration = 4000) => {
    const now = Date.now()
    const key = `${variant}:${message}`

    const lastTime = recentRef.current.get(key)
    if (lastTime && now - lastTime < DEDUP_WINDOW_MS) {
      return null
    }
    recentRef.current.set(key, now)

    const id = ++toastId
    setToasts((prev) => [...prev, { id, message, variant }])
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
    success: (msg) => addToast(msg, 'success'),
    error: (msg) => addToast(msg, 'danger', 6000),
    info: (msg) => addToast(msg, 'info'),
    warning: (msg) => addToast(msg, 'warning', 5000),
  }), [addToast])

  const variantStyles = {
    success: 'border-success/30 bg-success/10 text-success',
    danger: 'border-danger/30 bg-danger/10 text-danger',
    warning: 'border-warning/30 bg-warning/10 text-warning',
    info: 'border-info/30 bg-info/10 text-info',
  }

  return (
    <ToastContext.Provider value={toast}>
      {children}
      <div
        className="fixed toast-bottom-safe left-3 right-3 z-50 flex flex-col gap-2 sm:left-auto sm:right-4 sm:w-full sm:max-w-sm lg:bottom-4"
        aria-label="Notifications"
      >
        {toasts.map((t) => {
          const isError = t.variant === 'danger'
          const isWarning = t.variant === 'warning'
          const assertive = isError || isWarning
          return (
            <div
              key={t.id}
              role={assertive ? 'alert' : 'status'}
              aria-live={assertive ? 'assertive' : 'polite'}
              aria-atomic="true"
              className={`flex items-center gap-2 px-4 py-3 rounded-lg border text-sm font-medium animate-in slide-in-from-right backdrop-blur-sm ${variantStyles[t.variant] || variantStyles.info}`}
            >
              <span className="flex-1 break-words">{t.message}</span>
              <button
                onClick={() => removeToast(t.id)}
                className="opacity-60 hover:opacity-100 shrink-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 rounded"
                aria-label="Dismiss notification"
              >
                <svg className="w-4 h-4" fill="none" viewBox="0 0 24 24" strokeWidth={2} stroke="currentColor" aria-hidden="true">
                  <path strokeLinecap="round" strokeLinejoin="round" d="M6 18 18 6M6 6l12 12" />
                </svg>
              </button>
            </div>
          )
        })}
      </div>
    </ToastContext.Provider>
  )
}

export function useToast() {
  const ctx = useContext(ToastContext)
  if (!ctx) throw new Error('useToast must be used within ToastProvider')
  return ctx
}
