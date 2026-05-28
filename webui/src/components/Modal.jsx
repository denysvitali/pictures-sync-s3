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
      className={`fixed inset-0 z-50 flex items-end sm:items-center justify-center p-3 sm:p-4 bg-surface-950/70 ${backdropBlur[blur]}`}
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
        className={`w-full ${sizeClasses[size]} bg-surface-900 border border-surface-700 rounded-2xl shadow-2xl shadow-brand-500/5 p-5 outline-none`}
        initial={{ opacity: 0, scale: 0.92, y: 20 }}
        animate={{ opacity: 1, scale: 1, y: 0 }}
        exit={{ opacity: 0, scale: 0.96, y: 10 }}
        transition={{ type: 'spring', stiffness: 400, damping: 30 }}
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
        <div className="mb-4">
          {children}
        </div>
        {footer && (
          <div className="flex items-center justify-end gap-2 pt-3 border-t border-surface-700/50">
            {footer}
          </div>
        )}
      </motion.div>
    </motion.div>,
    document.body,
  )
}
