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

function NavItem({ route, active, onNavigate, layout = 'mobile' }) {
  const activeClass = active
    ? 'text-brand-300 bg-brand-400/10 border-brand-400/20'
    : 'text-surface-400 border-transparent hover:text-surface-100 hover:bg-surface-800/70'

  if (layout === 'desktop') {
    return (
      <button
        onClick={() => onNavigate(route)}
        className={`flex w-full items-center gap-3 rounded-lg border px-3 py-2.5 text-sm font-medium transition-colors ${activeClass}`}
        aria-current={active ? 'page' : undefined}
      >
        <Icon name={route.icon} className="w-5 h-5 shrink-0" />
        <span>{route.label}</span>
      </button>
    )
  }

  return (
    <button
      onClick={() => onNavigate(route)}
      className={`flex min-w-0 flex-1 flex-col items-center gap-0.5 rounded-lg border px-1.5 py-2 text-[10px] font-medium transition-colors ${activeClass}`}
      aria-current={active ? 'page' : undefined}
    >
      <Icon name={route.icon} className="w-5 h-5" />
      <span className="max-w-full truncate">{route.label}</span>
    </button>
  )
}

function BottomNav({ current, onNavigate }) {
  return (
    <nav className="fixed bottom-0 inset-x-0 z-40 border-t border-surface-700/60 bg-surface-900/95 px-2 safe-area-bottom backdrop-blur lg:hidden">
      <div className="mx-auto flex max-w-lg items-center justify-around gap-1 py-1.5">
        {routes.map((route) => {
          const active = current === route.id
          return (
            <NavItem
              key={route.id}
              route={route}
              active={active}
              onNavigate={onNavigate}
            />
          )
        })}
      </div>
    </nav>
  )
}

function DesktopSidebar({ current, onNavigate }) {
  return (
    <aside className="fixed inset-y-0 left-0 z-40 hidden w-64 border-r border-surface-700/60 bg-surface-950/90 px-4 py-5 backdrop-blur lg:block">
      <div className="flex h-full flex-col">
        <div className="flex items-center gap-3 px-1">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-brand-500/15 text-xl">
            <span aria-hidden="true">📸</span>
          </div>
          <div className="min-w-0">
            <p className="text-sm font-semibold text-surface-50">Photo Backup</p>
            <p className="text-xs text-surface-500">Station console</p>
          </div>
        </div>

        <nav className="mt-8 space-y-1.5" aria-label="Main navigation">
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

        <div className="mt-auto" />
      </div>
    </aside>
  )
}

function Header({ title, subtitle, currentRoute }) {
  return (
    <header className="sticky top-0 z-30 border-b border-surface-700/60 glass safe-area-top">
      <div className="mx-auto flex min-h-16 max-w-6xl items-center gap-4 px-3 py-3 sm:px-5 lg:px-8">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-brand-500/15 lg:hidden">
            <span className="text-lg" aria-hidden="true">📸</span>
          </div>
          <div className="hidden h-9 w-9 items-center justify-center rounded-lg bg-brand-500/15 text-brand-300 lg:flex">
            <Icon name={currentRoute.icon} className="h-5 w-5" />
          </div>
          <div className="min-w-0">
            <h1 className="truncate text-base font-bold text-surface-50 sm:text-lg">{title}</h1>
            {subtitle && (
              <p className="text-xs text-surface-400 truncate">{subtitle}</p>
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
          <div className="absolute inset-0 -m-8 rounded-full bg-gradient-to-br from-brand-500/20 via-brand-600/10 to-transparent blur-md" />
          <div className="relative flex h-24 w-24 items-center justify-center rounded-2xl bg-gradient-to-br from-brand-500/20 to-brand-700/10 border border-brand-400/15">
            <Icon name="cloud" className="h-12 w-12 text-brand-400 float-animation" />
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

        <h2 className="mb-2 text-xl font-semibold text-surface-200">No Device Connected</h2>
        <p className="mx-auto max-w-sm text-sm text-surface-400 leading-relaxed">
          Enter your Photo Backup Station&apos;s address above to get started. Make sure the device is powered on and connected to your network.
        </p>

        {/* Connection status indicator */}
        <div className="mt-5 inline-flex items-center gap-2 rounded-full bg-surface-800/60 border border-surface-700/50 px-4 py-2">
          <span className="relative flex h-2.5 w-2.5">
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-surface-500 opacity-40" />
            <span className="relative inline-flex h-2.5 w-2.5 rounded-full bg-surface-500" />
          </span>
          <span className="text-xs text-surface-400">Waiting for connection</span>
        </div>

        {/* Troubleshooting steps */}
        <div className="mt-6 mx-auto max-w-sm text-left">
          <p className="text-xs font-medium text-surface-500 uppercase tracking-wider mb-2">Troubleshooting</p>
          <ul className="space-y-1.5">
            {[
              'Check that the Raspberry Pi is powered on',
              'Verify the device is on the same WiFi network',
              'Try the device IP address with port 8080',
              'Ensure the web server service is running',
            ].map((step, i) => (
              <li key={i} className="flex items-start gap-2 text-xs text-surface-400">
                <span className="mt-0.5 flex h-4 w-4 shrink-0 items-center justify-center rounded-full bg-surface-800 text-[10px] font-medium text-surface-500">
                  {i + 1}
                </span>
                {step}
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
                  className="inline-flex items-center gap-1.5 rounded-full bg-surface-800/80 border border-surface-700/50 px-3 py-1.5 text-xs text-surface-300 hover:text-surface-100 hover:border-brand-400/30 hover:bg-surface-700/60 transition-all"
                >
                  <Icon name="cloud" className="w-3 h-3 text-brand-400" />
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
            className="inline-flex items-center gap-1.5 text-xs text-surface-500 hover:text-brand-400 transition-colors"
          >
            <Icon name="play" className="w-3 h-3" />
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
