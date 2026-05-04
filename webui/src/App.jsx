import { useState, useEffect, Suspense } from 'react'
import { useDevice } from './DeviceContext.jsx'
import { routes, getCurrentRoute } from './routes.js'
import { ToastProvider } from './components/Toast.jsx'
import { DeviceSwitcher } from './components/DeviceSwitcher.jsx'
import { Icon } from './components/Icons.jsx'
import { PageLoader } from './components/LoadingSpinner.jsx'

function BottomNav({ current, onNavigate }) {
  return (
    <nav className="fixed bottom-0 inset-x-0 z-40 glass border-t border-surface-700/50 safe-area-bottom">
      <div className="flex items-center justify-around max-w-lg mx-auto">
        {routes.map((route) => {
          const active = current === route.id
          return (
            <button
              key={route.id}
              onClick={() => onNavigate(route)}
              className={`
                flex flex-col items-center gap-0.5 py-2 px-3 min-w-0 transition-colors
                ${active ? 'text-brand-400' : 'text-surface-500 hover:text-surface-300'}
              `}
            >
              <Icon name={route.icon} className="w-5 h-5" />
              <span className="text-[10px] font-medium">{route.label}</span>
            </button>
          )
        })}
      </div>
    </nav>
  )
}

function Header({ title, subtitle }) {
  return (
    <header className="sticky top-0 z-30 glass border-b border-surface-700/50 safe-area-top">
      <div className="max-w-5xl mx-auto px-4 py-3">
        <div className="flex items-center gap-3">
          <div className="w-8 h-8 rounded-lg bg-brand-600/20 flex items-center justify-center">
            <span className="text-lg">📸</span>
          </div>
          <div className="min-w-0">
            <h1 className="text-base font-bold text-surface-50 truncate">{title}</h1>
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
      <div className="min-h-screen bg-surface-950 pb-20">
        <DeviceSwitcher />
        <Header title="Photo Backup Station" subtitle={titles[current]} />

        <main className="max-w-5xl mx-auto px-4 py-4">
          {!deviceUrl ? (
            <div className="flex flex-col items-center justify-center min-h-[60vh] text-center px-4">
              <div className="w-16 h-16 rounded-2xl bg-brand-600/10 flex items-center justify-center mb-4">
                <Icon name="cloud" className="w-8 h-8 text-brand-400" />
              </div>
              <h2 className="text-lg font-semibold text-surface-200 mb-2">No Device Connected</h2>
              <p className="text-sm text-surface-400 max-w-xs">
                Enter your Photo Backup Station's address above to get started.
              </p>
            </div>
          ) : (
            <Suspense fallback={<PageLoader />}>
              <PageComponent />
            </Suspense>
          )}
        </main>

        <BottomNav current={current} onNavigate={navigate} />
      </div>
    </ToastProvider>
  )
}
