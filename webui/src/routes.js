import { lazy } from 'react'

const StatusPage = lazy(() => import('./pages/StatusPage.jsx'))
const WifiPage = lazy(() => import('./pages/WifiPage.jsx'))
const HistoryPage = lazy(() => import('./pages/HistoryPage.jsx'))
const GalleryPage = lazy(() => import('./pages/GalleryPage.jsx'))
const ConfigPage = lazy(() => import('./pages/ConfigPage.jsx'))
const GooglePhotosPage = lazy(() => import('./pages/GooglePhotosPage.jsx'))
const StatsPage = lazy(() => import('./pages/StatsPage.jsx'))

export const routes = [
  { id: 'status', label: 'Overview', path: '#/status', icon: 'home', Component: StatusPage },
  { id: 'wifi', label: 'Wi-Fi', path: '#/wifi', icon: 'wifi', Component: WifiPage },
  { id: 'history', label: 'Runs', path: '#/history', icon: 'clock', Component: HistoryPage },
  { id: 'gallery', label: 'Gallery', path: '#/gallery', icon: 'image', Component: GalleryPage },
  { id: 'googlephotos', label: 'Google Photos', path: '#/googlephotos', icon: 'cloud', Component: GooglePhotosPage },
  { id: 'stats', label: 'Stats', path: '#/stats', icon: 'chart', Component: StatsPage },
  { id: 'config', label: 'Settings', path: '#/config', icon: 'settings', Component: ConfigPage },
]

export function getCurrentRoute() {
  const hash = window.location.hash || '#/status'
  return routes.find((r) => r.path === hash) || routes[0]
}
