import { useEffect, useRef, useState } from 'react'

export interface PresetSpec {
  key: string
  label: string
  seconds: number
}

const PRESETS: PresetSpec[] = [
  { key: '15m', label: '15m', seconds: 15 * 60 },
  { key: '1h', label: '1h', seconds: 60 * 60 },
  { key: '6h', label: '6h', seconds: 6 * 60 * 60 },
  { key: '12h', label: '12h', seconds: 12 * 60 * 60 },
  { key: '24h', label: '24h', seconds: 24 * 60 * 60 },
  { key: '48h', label: '48h', seconds: 48 * 60 * 60 },
  { key: '7d', label: '7d', seconds: 7 * 86400 },
  { key: '30d', label: '30d', seconds: 30 * 86400 },
]

export interface RangeValue {
  preset: string
  from?: number
  to?: number
}

function presetToRange(preset: string): { from: number; to: number } | null {
  const found = PRESETS.find((p) => p.key === preset)
  if (!found) return null
  const now = Math.floor(Date.now() / 1000)
  return { from: now - found.seconds, to: now }
}

function toLocalInputValue(unixSeconds: number | undefined): string {
  if (!unixSeconds) return ''
  const d = new Date(unixSeconds * 1000)
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function fromLocalInputValue(s: string): number | null {
  if (!s) return null
  const t = new Date(s).getTime()
  if (Number.isNaN(t)) return null
  return Math.floor(t / 1000)
}

interface RangePickerProps {
  value: RangeValue
  onChange: (next: RangeValue) => void
}

const segmentBase =
  'relative rounded-md px-3 py-1.5 text-xs font-medium tabular-nums transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 focus-visible:ring-offset-1 focus-visible:ring-offset-surface-900'

function RangePicker({ value, onChange }: RangePickerProps) {
  const isCustom = value?.preset === 'custom'
  const [open, setOpen] = useState(false)
  const [fromStr, setFromStr] = useState(toLocalInputValue(value?.from))
  const [toStr, setToStr] = useState(toLocalInputValue(value?.to))
  const [err, setErr] = useState<string | null>(null)
  const popoverRef = useRef<HTMLDivElement>(null)

  useEffect(() => {
    if (isCustom) {
      setFromStr(toLocalInputValue(value?.from))
      setToStr(toLocalInputValue(value?.to))
    }
  }, [isCustom, value?.from, value?.to])

  useEffect(() => {
    if (!open) return
    function handleClick(e: MouseEvent) {
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        setOpen(false)
      }
    }
    function handleKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false)
    }
    document.addEventListener('mousedown', handleClick)
    document.addEventListener('keydown', handleKey)
    return () => {
      document.removeEventListener('mousedown', handleClick)
      document.removeEventListener('keydown', handleKey)
    }
  }, [open])

  function selectPreset(key: string) {
    const r = presetToRange(key)
    if (!r) return
    setOpen(false)
    setErr(null)
    onChange({ preset: key, from: r.from, to: r.to })
  }

  function applyCustom() {
    const from = fromLocalInputValue(fromStr)
    const to = fromLocalInputValue(toStr)
    const now = Math.floor(Date.now() / 1000)
    if (from === null || to === null) {
      setErr('Both dates are required')
      return
    }
    if (from >= to) {
      setErr('"From" must be before "To"')
      return
    }
    if (to > now + 60) {
      setErr('"To" cannot be in the future')
      return
    }
    setErr(null)
    setOpen(false)
    onChange({ preset: 'custom', from, to })
  }

  return (
    <div
      className="relative flex max-w-full flex-wrap items-center gap-1 rounded-lg border border-surface-700/60 bg-surface-800/60 p-1 shadow-sm shadow-black/10"
      role="group"
      aria-label="Time range"
    >
      {PRESETS.map((p) => {
        const active = value?.preset === p.key
        return (
          <button
            key={p.key}
            type="button"
            aria-pressed={active}
            onClick={() => selectPreset(p.key)}
            className={`${segmentBase} ${
              active
                ? 'bg-brand-500/20 text-brand-200 ring-1 ring-inset ring-brand-400/40'
                : 'text-surface-400 hover:bg-surface-700/50 hover:text-surface-100'
            }`}
          >
            {p.label}
          </button>
        )
      })}

      <span className="mx-0.5 h-4 w-px bg-surface-700/70" aria-hidden="true" />

      <button
        type="button"
        aria-pressed={isCustom}
        aria-expanded={open}
        aria-haspopup="dialog"
        onClick={() => setOpen((v) => !v)}
        className={`${segmentBase} inline-flex items-center gap-1 ${
          isCustom
            ? 'bg-brand-500/20 text-brand-200 ring-1 ring-inset ring-brand-400/40'
            : 'text-surface-400 hover:bg-surface-700/50 hover:text-surface-100'
        }`}
      >
        <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" fill="none" aria-hidden="true">
          <rect x="2.5" y="3" width="11" height="10.5" rx="1.5" stroke="currentColor" strokeWidth="1.3" />
          <path d="M2.5 6h11M5.5 1.75v2.5M10.5 1.75v2.5" stroke="currentColor" strokeWidth="1.3" strokeLinecap="round" />
        </svg>
        Custom
      </button>

      {open && (
        <div
          ref={popoverRef}
          role="dialog"
          aria-label="Custom time range"
          className="absolute right-0 top-full z-20 mt-2 w-72 rounded-xl border border-surface-700/80 bg-surface-900/95 p-3.5 shadow-2xl shadow-black/40 backdrop-blur"
        >
          <div className="space-y-2.5">
            <p className="text-xs font-semibold text-surface-200">Custom range</p>
            <label className="block text-[11px] font-medium uppercase tracking-wide text-surface-400">
              From
              <input
                type="datetime-local"
                value={fromStr}
                onChange={(e) => setFromStr(e.target.value)}
                className="mt-1 w-full rounded-md border border-surface-700 bg-surface-800 px-2 py-1.5 text-xs text-surface-100 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500/50 [color-scheme:dark]"
              />
            </label>
            <label className="block text-[11px] font-medium uppercase tracking-wide text-surface-400">
              To
              <input
                type="datetime-local"
                value={toStr}
                onChange={(e) => setToStr(e.target.value)}
                className="mt-1 w-full rounded-md border border-surface-700 bg-surface-800 px-2 py-1.5 text-xs text-surface-100 focus:border-brand-500 focus:outline-none focus:ring-1 focus:ring-brand-500/50 [color-scheme:dark]"
              />
            </label>
            {err && (
              <div
                role="alert"
                className="rounded-md border border-danger/25 bg-danger/10 px-2 py-1.5 text-xs text-danger"
              >
                {err}
              </div>
            )}
            <div className="flex justify-end gap-2 pt-1">
              <button
                type="button"
                onClick={() => setOpen(false)}
                className="rounded-md px-3 py-1.5 text-xs font-medium text-surface-400 transition-colors hover:bg-surface-800 hover:text-surface-200 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-surface-400/60"
              >
                Cancel
              </button>
              <button
                type="button"
                onClick={applyCustom}
                className="rounded-md bg-brand-500/20 px-3 py-1.5 text-xs font-medium text-brand-200 ring-1 ring-inset ring-brand-400/40 transition-colors hover:bg-brand-500/30 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
              >
                Apply
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}

export default RangePicker
export { RangePicker, presetToRange, PRESETS }
