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

  const connected = Boolean(deviceUrl)

  return (
    <div className="relative z-20 border-b border-surface-700/50 glass px-3 py-2 safe-area-top sm:px-5 lg:pl-72 lg:pr-8">
      <div className="mx-auto flex max-w-6xl items-center gap-3">
        <span className="relative flex h-2.5 w-2.5 shrink-0" aria-hidden="true">
          {connected && (
            <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-success opacity-60" />
          )}
          <span
            className={`relative inline-flex h-2.5 w-2.5 rounded-full ${connected ? 'bg-success' : 'bg-surface-500'}`}
          />
        </span>
        {editing ? (
          <div className="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:items-center">
            <input
              type="url"
              value={input}
              onChange={(e) => setInput(e.target.value)}
              onKeyDown={(e) => e.key === 'Enter' && handleSave()}
              placeholder="http://192.168.1.100:8080"
              className="min-w-0 flex-1 rounded-lg border border-surface-600/80 bg-surface-900/80 px-3 py-1.5 text-sm text-surface-100 placeholder:text-surface-500 focus:border-brand-500 focus:outline-none focus:ring-2 focus:ring-brand-500/40"
              autoFocus
            />
            <div className="flex items-center gap-2">
              <button
                onClick={handleSave}
                className="inline-flex min-h-9 items-center gap-1.5 rounded-lg bg-gradient-to-b from-brand-500 to-brand-600 px-3 py-1.5 text-xs font-medium text-white shadow-sm shadow-brand-900/40 ring-1 ring-inset ring-white/10 transition-colors hover:from-brand-400 hover:to-brand-500 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
              >
                <Icon name="cloud" className="h-3.5 w-3.5" />
                Connect
              </button>
              <button
                onClick={() => { setEditing(false); setInput(deviceUrl) }}
                className="min-h-9 rounded-lg px-3 py-1.5 text-xs font-medium text-surface-400 transition-colors hover:bg-surface-700/50 hover:text-surface-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
              >
                Cancel
              </button>
            </div>
          </div>
        ) : (
          <button
            onClick={() => setEditing(true)}
            className="group flex min-w-0 items-center gap-2 rounded-md px-1 text-sm text-surface-300 transition-colors hover:text-surface-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
          >
            <span className="text-xs uppercase tracking-wider text-surface-500">Device</span>
            <span className="truncate font-medium">
              {deviceUrl ? deviceUrl.replace(/^https?:\/\//, '') : 'No device configured'}
            </span>
            <Icon name="pencil" className="h-3.5 w-3.5 shrink-0 text-surface-500 transition-colors group-hover:text-brand-400" />
          </button>
        )}
      </div>
    </div>
  )
}
