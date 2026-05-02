import { createContext, useContext, useEffect, useState } from 'react'

const STORAGE_KEY = 'photo-backup-device-url'

const defaultDeviceUrl = () => {
  if (typeof window === 'undefined') return ''

  const saved = window.localStorage.getItem(STORAGE_KEY)
  if (saved && !isHostedPagesUrl(saved)) {
    return saved
  }

  const host = window.location.hostname.toLowerCase()
  if (host.endsWith('.github.io') || host.endsWith('.github.com')) {
    return ''
  }

  return `${window.location.origin}`
}

export function isHostedPagesUrl(raw) {
  try {
    const url = new URL(raw)
    const host = url.hostname.toLowerCase()
    return host.endsWith('.github.io') || host.endsWith('.github.com')
  } catch {
    return false
  }
}

const DeviceUrlContext = createContext({
  deviceUrl: '',
  setDeviceUrl: () => {},
  clearDeviceUrl: () => {}
})

export function DeviceUrlProvider({ children }) {
  const [deviceUrl, setDeviceUrlState] = useState(defaultDeviceUrl)

  useEffect(() => {
    setDeviceUrlState(defaultDeviceUrl())
  }, [])

  const setDeviceUrl = (value) => {
    const next = String(value || '').trim()
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(STORAGE_KEY, next)
    }
    setDeviceUrlState(next)
  }

  const clearDeviceUrl = () => {
    if (typeof window !== 'undefined') {
      window.localStorage.removeItem(STORAGE_KEY)
    }
    setDeviceUrlState('')
  }

  return (
    <DeviceUrlContext.Provider value={{ deviceUrl, setDeviceUrl, clearDeviceUrl }}>
      {children}
    </DeviceUrlContext.Provider>
  )
}

export function useDeviceUrl() {
  return useContext(DeviceUrlContext)
}
