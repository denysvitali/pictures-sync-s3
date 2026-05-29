import { useEffect, useRef, useCallback, useId } from 'react'
import { createPortal } from 'react-dom'
import { motion, AnimatePresence } from 'framer-motion'
import { Icon } from './Icons.jsx'

const FOCUSABLE_SELECTOR = [
  'a[href]',
  'button:not([disabled])',
  'textarea:not([disabled])',
  'input:not([disabled])',
  'select:not([disabled])',
  '[tabindex]:not([tabindex="-1"])',
].join(',')

const sizeClasses = {
  sm: 'sm:max-w-sm',
  md: 'sm:max-w-md',
  lg: 'sm:max-w-lg',
  xl: 'sm:max-w-xl',
}

const backdropBlur = {
  sm: 'backdrop-blur-sm',
  md: 'backdrop-blur-md',
  lg: 'backdrop-blur-lg',
  xl: 'backdrop-blur-xl',
}

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
  size = 'md',
  backdropBlur: blur = 'sm',
  footer,
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

  return (
    <AnimatePresence>
      {open && (
        <ModalContent
          dialogRef={dialogRef}
          titleId={titleId}
          describedBy={describedBy}
          title={title}
          children={children}
          footer={footer}
          size={size}
          blur={blur}
          onClose={onClose}
          handleKeyDown={handleKeyDown}
        />
      )}
    </AnimatePresence>
  )
}

function ModalContent({ dialogRef, titleId, describedBy, title, children, footer, size, blur, onClose, handleKeyDown }) {
  return createPortal(
    <motion.div
      className={`fixed inset-0 z-50 flex items-end sm:items-center justify-center p-3 sm:p-4 bg-surface-950/80 ${backdropBlur[blur]}`}
      initial={{ opacity: 0 }}
      animate={{ opacity: 1 }}
      exit={{ opacity: 0 }}
      transition={{ duration: 0.2 }}
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose?.()
      }}
    >
      <motion.div
        ref={dialogRef}
        role="alertdialog"
        aria-modal="true"
        aria-labelledby={titleId}
        aria-describedby={describedBy}
        tabIndex={-1}
        onKeyDown={handleKeyDown}
        className={`relative w-full overflow-hidden ${sizeClasses[size]} glass rounded-2xl border border-surface-700/70 p-5 shadow-elevated outline-none ring-1 ring-white/5`}
        initial={{ opacity: 0, scale: 0.94, y: 24 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={{ opacity: 0, scale: 0.96, y: 12 }}
        transition={{ type: 'spring', stiffness: 380, damping: 28 }}
      >
        <div
          className="pointer-events-none absolute inset-x-0 top-0 h-px bg-gradient-to-r from-transparent via-white/15 to-transparent"
          aria-hidden="true"
        />
        <div className="mb-3 flex items-start justify-between gap-3">
          <h2 id={titleId} className="text-lg font-semibold tracking-tight text-surface-50">
            {title}
          </h2>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close dialog"
            className="-mr-1 -mt-1 rounded-lg p-1.5 text-surface-400 transition-colors hover:bg-surface-700/60 hover:text-surface-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
          >
            <Icon name="x" className="h-5 w-5" />
          </button>
        </div>
        <div className="mb-4 text-sm text-surface-300">
          {children}
        </div>
        {footer && (
          <div className="flex items-center justify-end gap-2 border-t border-surface-700/50 pt-3">
            {footer}
          </div>
        )}
      </motion.div>
    </motion.div>,
    document.body,
  )
}
