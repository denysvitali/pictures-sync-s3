import { useState } from 'react'
import { useDevice } from '../DeviceContext.jsx'
import { Icon } from './Icons.jsx'

export function DeviceSwitcher() {
  const { deviceUrl, setDeviceUrl } = useDevice()
  const [input, setInput] = useState(deviceUrl)
  const [editing, setEditing] = useState(false)

  const handleSave = () => {
    const trimmed = input.trim().replace(/\/+$/, '')
    if (!trimmed) return
    const nextUrl = /^https?:\/\//i.test(trimmed) ? trimmed : `http://${trimmed}`
    setDeviceUrl(nextUrl)
    setInput(nextUrl)
    setEditing(false)
  }

  return (
    <div className="bg-surface-900/90 border-b border-surface-700/50 px-3 sm:px-5 lg:pl-72 lg:pr-8 py-2 safe-area-top">
      <div className="max-w-6xl mx-auto flex items-center gap-3">
        <Icon name="cloud" className="w-4 h-4 text-brand-400 shrink-0" />
        {editing ? (
          <div className="flex flex-1 min-w-0 flex-col gap-2 sm:flex-row sm:items-center">
            <input
              type="url"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleSave()}
              placeholder="http://192.168.1.100:8080"
              className="flex-1 min-w-0 bg-surface-900 border border-surface-600 rounded-lg px-3 py-1.5 text-sm text-surface-100 placeholder:text-surface-500 focus:outline-none focus:ring-2 focus:ring-brand-500"
              autoFocus
            />
            <div className="flex items-center gap-2">
              <button
                onClick={handleSave}
                className="min-h-9 px-3 py-1.5 bg-brand-600 hover:bg-brand-500 text-white text-xs font-medium rounded-lg transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
              >
                Connect
              </button>
              <button
                onClick={() => { setEditing(false); setInput(deviceUrl) }}
                className="min-h-9 px-3 py-1.5 text-surface-400 hover:text-surface-200 text-xs font-medium rounded-lg transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
              >
                Cancel
              </button>
            </div>
          </div>
        ) : (
          <button
            onClick={() => setEditing(true)}
            className="flex items-center gap-2 min-w-0 text-sm text-surface-300 hover:text-surface-100 transition-colors"
          >
            <span className="truncate">
              {deviceUrl || 'No device configured'}
            </span>
            <Icon name="settings" className="w-3.5 h-3.5 shrink-0 opacity-50" />
          </button>
        )}
      </div>
    </div>
  )
}
