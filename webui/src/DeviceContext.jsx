import { createContext, useContext, useState, useCallback } from 'react'

const DeviceContext = createContext(null)

function getInitialDeviceUrl() {
  const params = new URLSearchParams(window.location.search)
  const fromUrl = params.get('device')
  if (fromUrl) return fromUrl

  try {
    return localStorage.getItem('deviceUrl') || ''
  } catch {
    return ''
  }
}

export function DeviceProvider({ children }) {
  const [deviceUrl, setDeviceUrlRaw] = useState(getInitialDeviceUrl)

  const setDeviceUrl = useCallback((url) => {
    const normalized = String(url || '').trim().replace(/\/$/, '')
    setDeviceUrlRaw(normalized)
    try {
      if (normalized) {
        localStorage.setItem('deviceUrl', normalized)
      } else {
        localStorage.removeItem('deviceUrl')
      }
    } catch {}
  }, [])

  const isLocalhost = window.location.hostname === 'localhost' ||
    window.location.hostname === '127.0.0.1'

  return (
    <DeviceContext.Provider value={{ deviceUrl, setDeviceUrl, isLocalhost }}>
      {children}
    </DeviceContext.Provider>
  )
}

export function useDevice() {
  const ctx = useContext(DeviceContext)
  if (!ctx) throw new Error('useDevice must be used within DeviceProvider')
  return ctx
}
