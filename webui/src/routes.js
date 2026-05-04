import { ConfigPage } from './pages/ConfigPage'
import { GalleryPage } from './pages/GalleryPage'
import { HistoryPage } from './pages/HistoryPage'
import { StatusPage } from './pages/StatusPage'
import { WifiPage } from './pages/WifiPage'

/**
 * @typedef {Object} PageDefinition
 * @property {string} key
 * @property {string} path
 * @property {string} label
 * @property {string} icon
 * @property {string} description
 * @property {React.ComponentType<any>} component
 * @property {boolean} requiresDeviceUrl
 */

/** @type {PageDefinition[]} */
export const pageRegistry = [
  {
    key: 'status',
    path: 'status',
    label: 'Overview',
    icon: '📈',
    component: StatusPage,
    requiresDeviceUrl: true,
    description: 'See live backup health and recent activity.'
  },
  {
    key: 'wifi',
    path: 'wifi',
    label: 'Wi‑Fi',
    icon: '📶',
    component: WifiPage,
    requiresDeviceUrl: true,
    description: 'Connect the device to your home network.'
  },
  {
    key: 'history',
    path: 'history',
    label: 'Runs',
    icon: '🗂️',
    component: HistoryPage,
    requiresDeviceUrl: true,
    description: 'Review completed and failed backup runs.'
  },
  {
    key: 'gallery',
    path: 'gallery',
    label: 'Gallery',
    icon: '🖼️',
    component: GalleryPage,
    requiresDeviceUrl: true,
    description: 'Quickly check what synced files look like.'
  },
  {
    key: 'config',
    path: 'config',
    label: 'Settings',
    icon: '⚙️',
    component: ConfigPage,
    requiresDeviceUrl: true,
    description: 'Set destination, tuning, and maintenance options.'
  }
]

export const pageByPath = (path) => {
  const normalized = parseHashRoute(path)
  return pageRegistry.find((route) => route.path === normalized) || pageRegistry[0]
}

export const parseHashRoute = (hash = '') => {
  const raw = hash || (typeof window !== 'undefined' ? window.location.hash : '')
  const normalized = String(raw || '')
    .split('?')[0]
    .replace(/^#\//, '')
    .replace(/^#/, '')
    .trim()
    .toLowerCase()

  return normalized || pageRegistry[0].path
}

export const setHashRoute = (path) => {
  const normalized = String(path || pageRegistry[0].path).replace(/^\//, '')
  window.location.hash = `#/${normalized}`
}

export const navigateRoute = (path) => {
  setHashRoute(path)
}
