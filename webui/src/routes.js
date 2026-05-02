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
    label: 'Status',
    icon: '📈',
    component: StatusPage,
    requiresDeviceUrl: true,
    description: 'Current sync state and recent run history.'
  },
  {
    key: 'wifi',
    path: 'wifi',
    label: 'Wi-Fi',
    icon: '📶',
    component: WifiPage,
    requiresDeviceUrl: true,
    description: 'Manage saved networks and connection actions.'
  },
  {
    key: 'history',
    path: 'history',
    label: 'History',
    icon: '🗂️',
    component: HistoryPage,
    requiresDeviceUrl: true,
    description: 'Inspect historical sync jobs and status messages.'
  },
  {
    key: 'gallery',
    path: 'gallery',
    label: 'Gallery',
    icon: '🖼️',
    component: GalleryPage,
    requiresDeviceUrl: true,
    description: 'Browse synced images and folders from the device.'
  },
  {
    key: 'config',
    path: 'config',
    label: 'Config',
    icon: '⚙️',
    component: ConfigPage,
    requiresDeviceUrl: true,
    description: 'Edit sync settings and validate rclone config.'
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
