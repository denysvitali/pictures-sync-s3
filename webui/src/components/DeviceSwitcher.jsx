import { useState } from 'react'
import { useDevice } from '../DeviceContext.jsx'
import { Icon } from './Icons.jsx'

export function DeviceSwitcher() {
  const { deviceUrl, setDeviceUrl, isLocalhost } = useDevice()
  const [input, setInput] = useState(deviceUrl)
  const [editing, setEditing] = useState(false)

  if (isLocalhost) return null

  const handleSave = () => {
    setDeviceUrl(input)
    setEditing(false)
  }

  return (
    <div className="bg-surface-800/80 border-b border-surface-700/50 px-4 py-2 safe-area-top">
      <div className="max-w-5xl mx-auto flex items-center gap-3">
        <Icon name="cloud" className="w-4 h-4 text-brand-400 shrink-0" />
        {editing ? (
          <div className="flex items-center gap-2 flex-1 min-w-0">
            <input
              type="url"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleSave()}
              placeholder="http://192.168.1.100:8080"
              className="flex-1 min-w-0 bg-surface-900 border border-surface-600 rounded-lg px-3 py-1.5 text-sm text-surface-100 placeholder:text-surface-500 focus:outline-none focus:ring-2 focus:ring-brand-500"
              autoFocus
            />
            <button
              onClick={handleSave}
              className="px-3 py-1.5 bg-brand-600 hover:bg-brand-500 text-white text-xs font-medium rounded-lg transition-colors"
            >
              Connect
            </button>
            <button
              onClick={() => { setEditing(false); setInput(deviceUrl) }}
              className="px-3 py-1.5 text-surface-400 hover:text-surface-200 text-xs font-medium rounded-lg transition-colors"
            >
              Cancel
            </button>
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
