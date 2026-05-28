import { useEffect, useState, useCallback } from 'react'
import { Icon } from './Icons.jsx'
import { routes } from '../routes.js'

const SHORTCUTS = [
  {
    category: 'Navigation',
    items: routes.slice(0, 8).map((route, i) => ({
      keys: [`${i + 1}`],
      description: `Go to ${route.label}`,
    })),
  },
  {
    category: 'Actions',
    items: [
      { keys: ['R'], description: 'Refresh current page' },
      { keys: ['S'], description: 'Start sync (on Overview)' },
      { keys: ['/'], description: 'Focus search field' },
    ],
  },
  {
    category: 'General',
    items: [
      { keys: ['?'], description: 'Show this help' },
      { keys: ['Esc'], description: 'Close modals / overlays' },
    ],
  },
]

export function KeyboardShortcutsModal({ open, onClose }) {
  useEffect(() => {
    if (!open) return
    const handleKeyDown = (e) => {
      if (e.key === 'Escape') {
        onClose()
      }
    }
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [open, onClose])

  if (!open) return null

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-surface-950/70 backdrop-blur-sm p-4"
      onMouseDown={(e) => {
        if (e.target === e.currentTarget) onClose()
      }}
      role="dialog"
      aria-modal="true"
      aria-label="Keyboard shortcuts"
    >
      <div className="w-full max-w-lg bg-surface-900 border border-surface-700 rounded-2xl shadow-2xl p-6 animate-in fade-in">
        <div className="flex items-center justify-between mb-6">
          <h2 className="text-lg font-semibold text-surface-100">Keyboard Shortcuts</h2>
          <button
            onClick={onClose}
            className="p-1 text-surface-400 hover:text-surface-200 rounded focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
            aria-label="Close"
          >
            <Icon name="x" className="w-5 h-5" />
          </button>
        </div>

        <div className="grid gap-6 sm:grid-cols-2">
          {SHORTCUTS.map((section) => (
            <div key={section.category}>
              <h3 className="text-xs font-semibold text-surface-500 uppercase tracking-wider mb-3">
                {section.category}
              </h3>
              <div className="space-y-2">
                {section.items.map((item, i) => (
                  <div key={i} className="flex items-center justify-between gap-3">
                    <span className="text-sm text-surface-300">{item.description}</span>
                    <div className="flex items-center gap-1 shrink-0">
                      {item.keys.map((key, ki) => (
                        <kbd
                          key={ki}
                          className="inline-flex items-center justify-center min-w-[28px] h-6 px-1.5 rounded-md bg-surface-800 border border-surface-600 text-xs font-mono font-medium text-surface-200"
                        >
                          {key}
                        </kbd>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>

        <p className="mt-6 text-xs text-surface-500 text-center">
          Press <kbd className="inline-flex items-center justify-center min-w-[28px] h-6 px-1.5 rounded-md bg-surface-800 border border-surface-600 text-xs font-mono font-medium text-surface-200">?</kbd> anytime to show this help
        </p>
      </div>
    </div>
  )
}

export function useKeyboardShortcuts({ onRefresh, onSyncStart }) {
  const [modalOpen, setModalOpen] = useState(false)

  const openModal = useCallback(() => setModalOpen(true), [])
  const closeModal = useCallback(() => setModalOpen(false), [])

  useEffect(() => {
    const handleKeyDown = (e) => {
      // Don't trigger shortcuts when typing in inputs
      const tag = e.target?.tagName?.toLowerCase()
      const isEditable =
        tag === 'input' ||
        tag === 'textarea' ||
        tag === 'select' ||
        e.target?.isContentEditable

      if (isEditable) {
        if (e.key === 'Escape') {
          e.target.blur()
        }
        return
      }

      // Open shortcuts modal
      if (e.key === '?' && !e.ctrlKey && !e.metaKey && !e.altKey) {
        e.preventDefault()
        setModalOpen((open) => !open)
        return
      }

      if (modalOpen) {
        if (e.key === 'Escape') {
          e.preventDefault()
          setModalOpen(false)
        }
        return
      }

      // Navigation shortcuts
      const num = parseInt(e.key, 10)
      if (!isNaN(num) && num >= 1 && num <= routes.length) {
        e.preventDefault()
        window.location.hash = routes[num - 1].path.replace('#', '')
        return
      }

      // Refresh
      if ((e.key === 'r' || e.key === 'R') && !e.ctrlKey && !e.metaKey) {
        e.preventDefault()
        onRefresh?.()
        return
      }

      // Sync start
      if ((e.key === 's' || e.key === 'S') && !e.ctrlKey && !e.metaKey) {
        e.preventDefault()
        onSyncStart?.()
        return
      }

      // Focus search
      if (e.key === '/' && !e.ctrlKey && !e.metaKey) {
        e.preventDefault()
        const searchInput = document.querySelector('input[type="text"][placeholder*="Search"]')
        if (searchInput) {
          searchInput.focus()
        }
        return
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [modalOpen, onRefresh, onSyncStart])

  return { modalOpen, openModal, closeModal }
}
