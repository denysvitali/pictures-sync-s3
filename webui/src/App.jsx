import { useState, useEffect, Suspense } from 'react'
import { useDevice } from './DeviceContext.jsx'
import { routes, getCurrentRoute } from './routes.js'
import { ToastProvider } from './components/Toast.jsx'
import { DeviceSwitcher } from './components/DeviceSwitcher.jsx'
import { Icon } from './components/Icons.jsx'
import { PageLoader } from './components/LoadingSpinner.jsx'

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

export default function App() {
  const { deviceUrl } = useDevice()
  const [current, setCurrent] = useState(() => getCurrentRoute().id)

  useEffect(() => {
    const onHashChange = () => {
      setCurrent(getCurrentRoute().id)
    }
    window.addEventListener('hashchange', onHashChange)
    return () => window.removeEventListener('hashchange', onHashChange)
  }, [])

  const navigate = (route) => {
    window.location.hash = route.path.replace('#', '')
  }

  const currentRoute = routes.find((r) => r.id === current) || routes[0]
  const PageComponent = currentRoute.Component

  const titles = {
    status: 'Overview',
    wifi: 'Wi-Fi',
    history: 'Sync History',
    gallery: 'Gallery',
    config: 'Settings',
  }

  return (
    <ToastProvider>
      <div className="min-h-screen app-background content-bottom-safe lg:pb-0">
        <DeviceSwitcher />
        <DesktopSidebar current={current} onNavigate={navigate} />

        <div className="lg:pl-64">
          <Header title="Photo Backup Station" subtitle={titles[current]} currentRoute={currentRoute} />

          <main className="mx-auto w-full max-w-6xl px-3 py-4 sm:px-5 sm:py-6 lg:px-8">
            {!deviceUrl ? (
              <div className="flex min-h-[60vh] flex-col items-center justify-center px-4 text-center">
                <div className="mb-4 flex h-16 w-16 items-center justify-center rounded-lg bg-brand-600/10">
                  <Icon name="cloud" className="h-8 w-8 text-brand-400" />
                </div>
                <h2 className="mb-2 text-lg font-semibold text-surface-200">No Device Connected</h2>
                <p className="max-w-xs text-sm text-surface-400">
                  Enter your Photo Backup Station's address above to get started.
                </p>
              </div>
            ) : (
              <Suspense fallback={<PageLoader />}>
                <PageComponent />
              </Suspense>
            )}
          </main>
        </div>

        <BottomNav current={current} onNavigate={navigate} />
      </div>
    </ToastProvider>
  )
}
