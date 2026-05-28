interface ResolutionSpec {
  key: string
  label: string
  seconds: number
}

const RESOLUTIONS: ResolutionSpec[] = [
  { key: '10s', label: '10s', seconds: 10 },
  { key: '1m', label: '1m', seconds: 60 },
  { key: '5m', label: '5m', seconds: 300 },
  { key: '15m', label: '15m', seconds: 900 },
  { key: '1h', label: '1h', seconds: 3600 },
]

const MAX_POINTS = 2000
const AUTO_TARGET_POINTS = 300

export type ResolutionValue = 'auto' | number

function autoResolution(rangeSeconds: number): number {
  if (!rangeSeconds || rangeSeconds <= 0) return RESOLUTIONS[0]!.seconds
  const ideal = rangeSeconds / AUTO_TARGET_POINTS
  let chosen = RESOLUTIONS[RESOLUTIONS.length - 1]!.seconds
  for (const r of RESOLUTIONS) {
    if (r.seconds >= ideal) {
      chosen = r.seconds
      break
    }
  }
  while (rangeSeconds / chosen > MAX_POINTS) {
    const next = RESOLUTIONS.find((r) => r.seconds > chosen)
    if (!next) break
    chosen = next.seconds
  }
  return chosen
}

function formatPoints(n: number): string {
  if (n >= 1000) return `${(n / 1000).toFixed(1)}k`
  return String(Math.round(n))
}

interface ResolutionPickerProps {
  value: ResolutionValue
  rangeSeconds: number
  onChange: (next: ResolutionValue) => void
}

function ResolutionPicker({ value, rangeSeconds, onChange }: ResolutionPickerProps) {
  const effectiveSeconds: number =
    value === 'auto' ? autoResolution(rangeSeconds) : value
  const estimatedPoints = rangeSeconds && effectiveSeconds
    ? Math.max(1, Math.round(rangeSeconds / effectiveSeconds))
    : 0

  return (
    <div className="flex max-w-full flex-col gap-1">
      <div className="flex max-w-full flex-wrap items-center gap-1.5 rounded-lg bg-surface-800/60 p-1">
        <button
          type="button"
          aria-pressed={value === 'auto'}
          onClick={() => onChange('auto')}
          className={`rounded-md px-3 py-1 text-xs font-medium transition-colors ${
            value === 'auto'
              ? 'bg-brand-500/20 text-brand-300'
              : 'text-surface-400 hover:text-surface-200'
          }`}
        >
          Auto
        </button>
        {RESOLUTIONS.map((r) => {
          const active = value === r.seconds
          const tooFine = Boolean(rangeSeconds && rangeSeconds / r.seconds > MAX_POINTS)
          return (
            <button
              key={r.key}
              type="button"
              aria-pressed={active}
              disabled={tooFine}
              onClick={() => !tooFine && onChange(r.seconds)}
              className={`rounded-md px-3 py-1 text-xs font-medium transition-colors ${
                active
                  ? 'bg-brand-500/20 text-brand-300'
                  : 'text-surface-400 hover:text-surface-200'
              } ${tooFine ? 'opacity-40 cursor-not-allowed hover:text-surface-400' : ''}`}
            >
              {r.label}
            </button>
          )
        })}
      </div>
      <div className="px-1 text-[10px] text-surface-500">
        ≈ {formatPoints(estimatedPoints)} points
        {value === 'auto' && effectiveSeconds && (
          <span className="ml-1 text-surface-600">
            ({RESOLUTIONS.find((r) => r.seconds === effectiveSeconds)?.label || `${effectiveSeconds}s`})
          </span>
        )}
      </div>
    </div>
  )
}

export default ResolutionPicker
export { ResolutionPicker, autoResolution, RESOLUTIONS }
