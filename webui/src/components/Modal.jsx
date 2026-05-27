import { useEffect, useRef, useCallback, useId } from 'react'
import { createPortal } from 'react-dom'
import { Icon } from './Icons.jsx'

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

/**
 * Accessible modal dialog.
 *
 * - role="alertdialog" / aria-modal="true"
 * - Esc closes (via onClose)
 * - Focus is moved into the dialog on open and trapped on Tab/Shift+Tab
 * - Previous focus is restored on close
 * - Clicking the backdrop calls onClose
 */
export function Modal({
  open,
  onClose,
  title,
  children,
  initialFocusRef,
  labelledBy,
  describedBy,
}) {
  const dialogRef = useRef(null)
  const previousFocusRef = useRef(null)
  const generatedId = useId()
  const titleId = labelledBy || `modal-title-${generatedId}`

  const handleKeyDown = useCallback((e) => {
    if (e.key === 'Escape') {
      e.stopPropagation()
      onClose?.()
      return
    }
    if (e.key !== 'Tab') return
    const root = dialogRef.current
    if (!root) return
    const focusables = Array.from(root.querySelectorAll(FOCUSABLE_SELECTOR))
      .filter((el) => !el.hasAttribute('disabled') && el.offsetParent !== null)
    if (focusables.length === 0) {
      e.preventDefault()
      root.focus()
      return
    }
    const first = focusables[0]
    const last = focusables[focusables.length - 1]
    const active = document.activeElement
    if (e.shiftKey) {
      if (active === first || !root.contains(active)) {
        e.preventDefault()
        last.focus()
      }
    } else {
      if (active === last) {
        e.preventDefault()
        first.focus()
      }
    }
  }, [onClose])

  useEffect(() => {
    if (!open) return undefined
    previousFocusRef.current = document.activeElement

    // Move focus into the dialog after it mounts
    const id = window.setTimeout(() => {
      if (initialFocusRef?.current) {
        initialFocusRef.current.focus()
      } else if (dialogRef.current) {
        const focusables = dialogRef.current.querySelectorAll(FOCUSABLE_SELECTOR)
        if (focusables.length > 0) {
          focusables[0].focus()
        } else {
          dialogRef.current.focus()
        }
      }
    }, 0)

    const prevOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'

    return () => {
      window.clearTimeout(id)
      document.body.style.overflow = prevOverflow
      const prev = previousFocusRef.current
      queueMicrotask(() => {
        if (prev && typeof prev.focus === 'function') {
          prev.focus()
        }
      })
    }
  }, [open, initialFocusRef])

  if (!open) return null

  return createPortal(
    <div
      className="fixed inset-0 z-50 flex items-end sm:items-center justify-center p-3 sm:p-4 bg-surface-950/70 backdrop-blur-sm"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose?.()
      }}
    >
      <div
        ref={dialogRef}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={describedBy}
        tabIndex={-1}
        onKeyDown={handleKeyDown}
        className="w-full sm:max-w-md bg-surface-900 border border-surface-700 rounded-2xl shadow-2xl p-5 outline-none animate-in fade-in"
      >
        <div className="flex items-start justify-between gap-3 mb-3">
          <h2
            id={titleId}
            className="text-base font-semibold text-surface-50"
          >
            {title}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close dialog"
            className="p-1 text-surface-400 hover:text-surface-200 rounded focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
          >
            <Icon name="x" className="w-5 h-5" />
          </button>
        </div>
        {children}
      </div>
    </div>,
    document.body,
  )
}
