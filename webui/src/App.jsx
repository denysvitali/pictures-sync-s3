import { useState, useEffect, Suspense, useCallback, useMemo } from 'react'
import { motion, AnimatePresence } from 'framer-motion'
import { useDevice } from './DeviceContext.jsx'
import { routes, getCurrentRoute } from './routes.js'
import { ToastProvider } from './components/Toast.jsx'
import { DeviceSwitcher } from './components/DeviceSwitcher.jsx'
import { Icon } from './components/Icons.jsx'
import { PageLoader } from './components/LoadingSpinner.jsx'
import { KeyboardShortcutsModal, useKeyboardShortcuts } from './components/KeyboardShortcuts.jsx'
import { EmptyState } from './components/EmptyState.jsx'

const RECENT_DEVICES_KEY = 'recentDevices'
const MAX_RECENT_DEVICES = 5

// Mobile bottom nav shows 3 primary destinations + a "More" button that
// reveals the remaining routes. Order here defines the bottom-bar order.
const PRIMARY_NAV_IDS = ['status', 'history', 'gallery']

function NavItem({ route, active, onNavigate, layout = 'mobile' }) {
  if (layout === 'desktop') {
    const activeClass = active
      ? 'text-brand-200'
      : 'text-surface-400 hover:text-surface-100 hover:bg-surface-800/60'
    return (
      <button
        onClick={() => onNavigate(route)}
        className={`group relative flex w-full items-center gap-3 rounded-lg px-3 py-2.5 text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/60 ${activeClass}`}
        aria-current={active ? 'page' : undefined}
      >
        {active && (
          <motion.span
            layoutId="desktop-nav-active"
            className="absolute inset-0 rounded-lg border border-brand-400/20 bg-brand-400/10"
            transition={{ type: 'spring', stiffness: 500, damping: 38 }}
            aria-hidden="true"
          />
        )}
        {active && (
          <span className="absolute left-0 top-1/2 h-5 w-1 -translate-y-1/2 rounded-full bg-brand-400" aria-hidden="true" />
        )}
        <Icon name={route.icon} className="relative z-10 h-5 w-5 shrink-0" />
        <span className="relative z-10">{route.label}</span>
      </button>
    )
  }

  const activeClass = active ? 'text-brand-300' : 'text-surface-500 hover:text-surface-200'
  return (
    <button
      onClick={() => onNavigate(route)}
      className={`group relative flex min-w-0 flex-1 flex-col items-center gap-0.5 rounded-lg px-1.5 py-1.5 text-[10px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/60 ${activeClass}`}
      aria-current={active ? 'page' : undefined}
    >
      <span className="relative flex h-7 w-12 items-center justify-center">
        {active && (
          <motion.span
            layoutId="mobile-nav-active"
            className="absolute inset-0 rounded-full bg-brand-400/15"
            transition={{ type: 'spring', stiffness: 500, damping: 38 }}
            aria-hidden="true"
          />
        )}
        <Icon name={route.icon} className="relative z-10 h-5 w-5" />
      </span>
      <span className="max-w-full truncate">{route.label}</span>
    </button>
  )
}

function MoreNavButton({ active, label, icon, onClick, expanded }) {
  const activeClass = active || expanded ? 'text-brand-300' : 'text-surface-500 hover:text-surface-200'
  return (
    <button
      onClick={onClick}
      className={`group relative flex min-w-0 flex-1 flex-col items-center gap-0.5 rounded-lg px-1.5 py-1.5 text-[10px] font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/60 ${activeClass}`}
      aria-haspopup="menu"
      aria-expanded={expanded}
    >
      <span className="relative flex h-7 w-12 items-center justify-center">
        {(active || expanded) && (
          <span className="absolute inset-0 rounded-full bg-brand-400/15" aria-hidden="true" />
        )}
        <Icon name={icon} className="relative z-10 h-5 w-5" />
      </span>
      <span className="max-w-full truncate">{label}</span>
    </button>
  )
}

function MoreSheet({ open, onClose, overflowRoutes, current, onNavigate }) {
  useEffect(() => {
    if (!open) return
    const onKey = (e) => {
      if (e.key === 'Escape') onClose()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [open, onClose])

  return (
    <AnimatePresence>
      {open && (
        <>
          <motion.div
            key="more-backdrop"
            initial={{ opacity: 0 }}
            animate={{ opacity: 1 }}
            exit={{ opacity: 0 }}
            transition={{ duration: 0.15 }}
            className="fixed inset-0 z-40 bg-surface-950/60 backdrop-blur-sm lg:hidden"
            onClick={onClose}
            aria-hidden="true"
          />
          <motion.div
            key="more-sheet"
            role="menu"
            aria-label="More navigation"
            initial={{ y: '100%' }}
            animate={{ y: 0 }}
            exit={{ y: '100%' }}
            transition={{ type: 'spring', stiffness: 320, damping: 32 }}
            style={{ bottom: 'calc(3.5rem + env(safe-area-inset-bottom))' }}
            className="glass fixed inset-x-0 z-50 rounded-t-2xl border border-b-0 border-surface-700/60 shadow-elevated lg:hidden"
          >
            <div className="mx-auto max-w-lg px-4 pb-4 pt-3">
              <div className="mx-auto mb-3 h-1.5 w-10 rounded-full bg-surface-600/80" aria-hidden="true" />
              <p className="mb-2 px-1 text-[10px] font-medium uppercase tracking-wider text-surface-500">
                More destinations
              </p>
              <ul className="grid grid-cols-2 gap-2">
                {overflowRoutes.map((route) => {
                  const active = current === route.id
                  const activeClass = active
                    ? 'text-brand-300 bg-brand-400/10 border-brand-400/30'
                    : 'text-surface-300 border-surface-700/50 hover:border-surface-600 hover:text-surface-100 hover:bg-surface-800/70'
                  return (
                    <li key={route.id}>
                      <button
                        role="menuitem"
                        onClick={() => {
                          onNavigate(route)
                          onClose()
                        }}
                        className={`flex w-full items-center gap-2.5 rounded-lg border px-3 py-2.5 text-sm font-medium transition-colors ${activeClass}`}
                        aria-current={active ? 'page' : undefined}
                      >
                        <Icon name={route.icon} className="h-5 w-5 shrink-0" />
                        <span className="truncate">{route.label}</span>
                      </button>
                    </li>
                  )
                })}
              </ul>
            </div>
          </motion.div>
        </>
      )}
    </AnimatePresence>
  )
}

function BottomNav({ current, onNavigate }) {
  const [menuOpen, setMenuOpen] = useState(false)

  const primaryRoutes = useMemo(
    () =>
      PRIMARY_NAV_IDS
        .map((id) => routes.find((r) => r.id === id))
        .filter(Boolean),
    []
  )
  const overflowRoutes = useMemo(
    () => routes.filter((r) => !PRIMARY_NAV_IDS.includes(r.id)),
    []
  )

  // Close the sheet when navigating to a primary destination.
  useEffect(() => {
    setMenuOpen(false)
  }, [current])

  const activeOverflow = overflowRoutes.find((r) => r.id === current)
  const moreActive = Boolean(activeOverflow)
  const moreLabel = activeOverflow ? activeOverflow.label : 'More'
  const moreIcon = activeOverflow ? activeOverflow.icon : 'dots-horizontal'

  return (
    <>
      <MoreSheet
        open={menuOpen}
        onClose={() => setMenuOpen(false)}
        overflowRoutes={overflowRoutes}
        current={current}
        onNavigate={onNavigate}
      />
      <nav className="glass fixed inset-x-0 bottom-0 z-40 border-t border-surface-700/60 px-2 safe-area-bottom lg:hidden">
        <div className="mx-auto flex max-w-lg items-center justify-around gap-1 py-1.5">
          {primaryRoutes.map((route) => (
            <NavItem
              key={route.id}
              route={route}
              active={current === route.id}
              onNavigate={onNavigate}
            />
          ))}
          <MoreNavButton
            active={moreActive}
            label={moreLabel}
            icon={moreIcon}
            expanded={menuOpen}
            onClick={() => setMenuOpen((v) => !v)}
          />
        </div>
      </nav>
    </>
  )
}

function DesktopSidebar({ current, onNavigate }) {
  return (
    <aside className="fixed inset-y-0 left-0 z-40 hidden w-64 border-r border-surface-700/60 bg-surface-950/80 px-4 py-5 backdrop-blur-xl lg:block">
      {/* Faint brand glow at top of sidebar */}
      <div className="pointer-events-none absolute -left-16 -top-16 h-48 w-48 rounded-full bg-brand-500/10 blur-3xl" aria-hidden="true" />
      <div className="relative flex h-full flex-col">
        <div className="flex items-center gap-3 px-1">
          <div className="flex h-10 w-10 items-center justify-center rounded-xl bg-gradient-to-br from-brand-500/25 to-brand-700/10 ring-1 ring-inset ring-brand-400/20 shadow-card">
            <Icon name="logo" className="h-6 w-6 text-brand-300" />
          </div>
          <div className="min-w-0">
            <p className="text-sm font-semibold tracking-tight text-surface-50">Photo Backup</p>
            <p className="text-xs text-surface-500">Station console</p>
          </div>
        </div>

        <nav className="mt-8 space-y-1" aria-label="Main navigation">
          {routes.map((route) => (
            <NavItem
              key={route.id}
              route={route}
              active={current === route.id}
              onNavigate={onNavigate}
              layout="desktop"
            />
          ))}
        </nav>

        <div className="mt-auto pt-4">
          <div className="flex items-center justify-between gap-2 rounded-lg border border-surface-700/50 bg-surface-900/40 px-3 py-2.5">
            <span className="flex items-center gap-2 text-xs text-surface-500">
              <Icon name="zap" className="h-3.5 w-3.5 text-surface-500" />
              Shortcuts
            </span>
            <kbd className="inline-flex h-5 min-w-[20px] items-center justify-center rounded border border-surface-600/80 border-b-2 bg-surface-800 px-1 font-mono text-[10px] font-medium text-surface-300">
              ?
            </kbd>
          </div>
        </div>
      </div>
    </aside>
  )
}

function Header({ title, subtitle, currentRoute }) {
  return (
    <header className="sticky top-0 z-30 border-b border-surface-700/60 glass safe-area-top">
      <div className="mx-auto flex min-h-16 max-w-6xl items-center gap-4 px-3 py-3 sm:px-5 lg:px-8">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-brand-500/25 to-brand-700/10 ring-1 ring-inset ring-brand-400/20 lg:hidden">
            <Icon name="logo" className="h-5 w-5 text-brand-300" />
          </div>
          <div className="hidden h-9 w-9 items-center justify-center rounded-xl bg-gradient-to-br from-brand-500/25 to-brand-700/10 text-brand-300 ring-1 ring-inset ring-brand-400/20 lg:flex">
            <Icon name={currentRoute.icon} className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <h1 className="truncate text-base font-bold tracking-tight text-surface-50 sm:text-lg">{title}</h1>
            {subtitle && (
              <p className="truncate text-xs text-surface-400">{subtitle}</p>
            )}
          </div>
        </div>
      </div>
    </header>
  )
}

function FloatingShapes() {
  return (
    <div className="absolute inset-0 overflow-hidden pointer-events-none" aria-hidden="true">
      <div className="absolute top-[10%] left-[15%] w-32 h-32 rounded-full bg-brand-500/10 blur-2xl pulse-ring" />
      <div className="absolute top-[30%] right-[10%] w-24 h-24 rounded-full bg-brand-400/8 blur-xl pulse-ring" style={{ animationDelay: '0.5s' }} />
      <div className="absolute bottom-[20%] left-[25%] w-40 h-40 rounded-full bg-brand-600/8 blur-2xl pulse-ring" style={{ animationDelay: '1s' }} />
      <div className="absolute top-[50%] left-[50%] w-16 h-16 rounded-lg bg-brand-300/5 rotate-45 blur-lg" />
      <div className="absolute bottom-[35%] right-[20%] w-20 h-20 rounded-full bg-brand-500/6 blur-xl" />
    </div>
  )
}

function NoDeviceConnected({ onDemoMode }) {
  const [recentDevices, setRecentDevices] = useState(() => {
    try {
      const saved = localStorage.getItem(RECENT_DEVICES_KEY)
      return saved ? JSON.parse(saved) : []
    } catch {
      return []
    }
  })

  const { setDeviceUrl } = useDevice()

  const handleConnectRecent = useCallback((url) => {
    setDeviceUrl(url)
  }, [setDeviceUrl])

  const handleDemoMode = useCallback(() => {
    onDemoMode?.()
  }, [onDemoMode])

  const clearRecent = useCallback(() => {
    localStorage.removeItem(RECENT_DEVICES_KEY)
    setRecentDevices([])
  }, [])

  return (
    <div className="relative flex min-h-[60vh] flex-col items-center justify-center px-4 text-center">
      <FloatingShapes />

      <div className="relative z-10">
        {/* Main illustration */}
        <div className="relative mb-6 flex items-center justify-center">
          <div className="absolute inset-0 -m-10 rounded-full bg-gradient-to-br from-brand-500/25 via-brand-600/10 to-transparent blur-2xl" aria-hidden="true" />
          <div className="absolute h-32 w-32 rounded-full border border-brand-400/10 pulse-ring" aria-hidden="true" />
          <div className="relative flex h-24 w-24 items-center justify-center rounded-2xl border border-brand-400/20 bg-gradient-to-br from-surface-800/90 to-surface-900/90 shadow-elevated ring-1 ring-inset ring-white/5">
            <Icon name="cloud" className="h-12 w-12 text-brand-300 float-animation" />
          </div>
          {/* Connection dots */}
          <div className="absolute -right-8 top-1/2 -translate-y-1/2 flex items-center gap-1">
            <div className="w-1.5 h-1.5 rounded-full bg-brand-400/40 animate-pulse" />
            <div className="w-1.5 h-1.5 rounded-full bg-brand-400/30 animate-pulse" style={{ animationDelay: '0.2s' }} />
            <div className="w-1.5 h-1.5 rounded-full bg-brand-400/20 animate-pulse" style={{ animationDelay: '0.4s' }} />
          </div>
          <div className="absolute -left-8 top-1/2 -translate-y-1/2 flex items-center gap-1">
            <div className="w-1.5 h-1.5 rounded-full bg-brand-400/20 animate-pulse" style={{ animationDelay: '0.4s' }} />
            <div className="w-1.5 h-1.5 rounded-full bg-brand-400/30 animate-pulse" style={{ animationDelay: '0.2s' }} />
            <div className="w-1.5 h-1.5 rounded-full bg-brand-400/40 animate-pulse" />
          </div>
        </div>

        <h2 className="mb-2 text-2xl font-semibold tracking-tight text-surface-100">No Device Connected</h2>
        <p className="mx-auto max-w-sm text-sm leading-relaxed text-surface-400">
          Enter your Photo Backup Station&apos;s address above to get started. Make sure the device is powered on and connected to your network.
        </p>

        {/* Connection status indicator */}
        <div className="mt-5 inline-flex items-center gap-2 rounded-full border border-surface-700/50 bg-surface-800/60 px-4 py-2 shadow-card">
          <span className="relative flex h-2.5 w-2.5">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-warning opacity-50" />
            <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-warning/80" />
          </span>
          <span className="text-xs font-medium text-surface-300">Waiting for connection</span>
        </div>

        {/* Troubleshooting steps */}
        <div className="mx-auto mt-7 max-w-sm rounded-xl border border-surface-700/50 bg-surface-900/40 p-4 text-left shadow-card">
          <p className="mb-3 text-xs font-medium uppercase tracking-wider text-surface-500">Troubleshooting</p>
          <ul className="space-y-2.5">
            {[
              'Check that the Raspberry Pi is powered on',
              'Verify the device is on the same WiFi network',
              'Try the device IP address with port 8080',
              'Ensure the web server service is running',
            ].map((step, i) => (
              <li key={i} className="flex items-start gap-2.5 text-xs text-surface-400">
                <span className="flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-brand-500/10 text-[10px] font-semibold text-brand-300 ring-1 ring-inset ring-brand-400/20">
                  {i + 1}
                </span>
                <span className="pt-0.5">{step}</span>
              </li>
            ))}
          </ul>
        </div>

        {/* Recent devices */}
        {recentDevices.length > 0 && (
          <div className="mt-6">
            <div className="flex items-center justify-center gap-2 mb-2">
              <p className="text-xs font-medium text-surface-500 uppercase tracking-wider">Recently connected</p>
              <button
                onClick={clearRecent}
                className="text-[10px] text-surface-600 hover:text-surface-400 transition-colors"
              >
                Clear
              </button>
            </div>
            <div className="flex flex-wrap items-center justify-center gap-2">
              {recentDevices.map((url) => (
                <button
                  key={url}
                  onClick={() => handleConnectRecent(url)}
                  className="inline-flex items-center gap-1.5 rounded-full border border-surface-700/50 bg-surface-800/80 px-3 py-1.5 text-xs text-surface-300 shadow-card transition-all hover:border-brand-400/40 hover:bg-surface-700/60 hover:text-surface-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/60"
                >
                  <Icon name="cloud" className="h-3 w-3 text-brand-400" />
                  <span className="max-w-[180px] truncate">{url.replace(/^https?:\/\//, '')}</span>
                </button>
              ))}
            </div>
          </div>
        )}

        {/* Demo mode hint */}
        <div className="mt-6">
          <button
            onClick={handleDemoMode}
            className="inline-flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-surface-500 transition-colors hover:text-brand-400 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/60"
          >
            <Icon name="play" className="h-3 w-3" />
            Try demo mode for UI testing
          </button>
        </div>
      </div>
    </div>
  )
}

export default function App() {
  const { deviceUrl, setDeviceUrl } = useDevice()
  const [current, setCurrent] = useState(() => getCurrentRoute().id)
  const [demoMode, setDemoMode] = useState(false)

  useEffect(() => {
    const onHashChange = () => {
      setCurrent(getCurrentRoute().id)
    }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])

  const navigate = useCallback((route) => {
    window.location.hash = route.path.replace('#', '')
  }, [])

  const handleRefresh = useCallback(() => {
    window.dispatchEvent(new Event('app-refresh'))
  }, [])

  const handleSyncStart = useCallback(() => {
    window.dispatchEvent(new Event('app-sync-start'))
  }, [])

  const handleDemoMode = useCallback(() => {
    setDemoMode(true)
    // Set a mock device URL for demo purposes
    setDeviceUrl('http://demo.local:8080')
  }, [setDeviceUrl])

  const { modalOpen, closeModal } = useKeyboardShortcuts({
    onRefresh: handleRefresh,
    onSyncStart: handleSyncStart,
  })

  const currentRoute = routes.find((r) => r.id === current) || routes[0]
  const PageComponent = currentRoute.Component

  const titles = {
    status: 'Overview',
    wifi: 'Wi-Fi',
    history: 'Sync History',
    gallery: 'Gallery',
    googlephotos: 'Google Photos',
    stats: 'System Stats',
    dmesg: 'Kernel Log',
    config: 'Settings',
  }

  // Track recent devices
  useEffect(() => {
    if (!deviceUrl || demoMode) return
    try {
      const saved = localStorage.getItem(RECENT_DEVICES_KEY)
      const existing = saved ? JSON.parse(saved) : []
      const updated = [deviceUrl, ...existing.filter((u) => u !== deviceUrl)].slice(0, MAX_RECENT_DEVICES)
      localStorage.setItem(RECENT_DEVICES_KEY, JSON.stringify(updated))
    } catch {}
  }, [deviceUrl, demoMode])

  const showDeviceUrl = useMemo(() => {
    if (demoMode) return null
    return deviceUrl
  }, [demoMode, deviceUrl])

  return (
    <ToastProvider>
      <div className="min-h-screen app-background content-bottom-safe lg:pb-0">
        <DeviceSwitcher />
        <DesktopSidebar current={current} onNavigate={navigate} />

        <div className="lg:pl-64">
          <Header title="Photo Backup Station" subtitle={titles[current]} currentRoute={currentRoute} />

          <main className="mx-auto w-full max-w-6xl px-3 py-4 sm:px-5 sm:py-6 lg:px-8">
            <AnimatePresence mode="wait">
              {!showDeviceUrl ? (
                <motion.div
                  key="no-device"
                  initial={{ opacity: 0, y: 12 }}
                  animate={{ opacity: 1, y: 0 }}
                  exit={{ opacity: 0, y: -8 }}
                  transition={{ duration: 0.2 }}
                >
                  <NoDeviceConnected onDemoMode={handleDemoMode} />
                </motion.div>
              ) : (
                <motion.div
                  key={current}
                  initial={{ opacity: 0, y: 12 }}
                  animate={{ opacity: 1, y: 0 }}
                  exit={{ opacity: 0, y: -8 }}
                  transition={{ duration: 0.2 }}
                >
                  <Suspense fallback={<PageLoader />}>
                    <PageComponent />
                  </Suspense>
                </motion.div>
              )}
            </AnimatePresence>
          </main>
        </div>

        <BottomNav current={current} onNavigate={navigate} />

        <KeyboardShortcutsModal open={modalOpen} onClose={closeModal} />
      </div>
    </ToastProvider>
  )
}
