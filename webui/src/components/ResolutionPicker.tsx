import { motion, useReducedMotion } from 'framer-motion'

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

interface SegmentProps {
  label: string
  active: boolean
  disabled?: boolean
  reduceMotion: boolean
  onSelect: () => void
}

function Segment({ label, active, disabled = false, reduceMotion, onSelect }: SegmentProps) {
  return (
    <button
      type="button"
      aria-pressed={active}
      disabled={disabled}
      onClick={() => !disabled && onSelect()}
      className={`relative rounded-md px-3 py-1 text-xs font-medium transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70 focus-visible:ring-offset-1 focus-visible:ring-offset-surface-900 ${
        active
          ? 'text-brand-200'
          : disabled
            ? 'text-surface-500 cursor-not-allowed'
            : 'text-surface-400 hover:text-surface-100'
      } ${disabled ? 'opacity-40' : ''}`}
    >
      {active && (
        <motion.span
          layoutId="resolution-active-pill"
          className="absolute inset-0 rounded-md border border-brand-400/30 bg-brand-500/20 shadow-sm shadow-brand-500/10"
          transition={
            reduceMotion
              ? { duration: 0 }
              : { type: 'spring', stiffness: 500, damping: 36 }
          }
        />
      )}
      <span className="relative z-10">{label}</span>
    </button>
  )
}

function ResolutionPicker({ value, rangeSeconds, onChange }: ResolutionPickerProps) {
  const reduceMotion = useReducedMotion() ?? false
  const effectiveSeconds: number =
    value === 'auto' ? autoResolution(rangeSeconds) : value
  const estimatedPoints = rangeSeconds && effectiveSeconds
    ? Math.max(1, Math.round(rangeSeconds / effectiveSeconds))
    : 0
  const autoLabel = RESOLUTIONS.find((r) => r.seconds === effectiveSeconds)?.label || `${effectiveSeconds}s`

  return (
    <div className="flex max-w-full flex-col gap-1.5">
      <div className="flex max-w-full flex-wrap items-center gap-0.5 rounded-lg border border-surface-700/60 bg-surface-900/50 p-1">
        <Segment
          label="Auto"
          active={value === 'auto'}
          reduceMotion={reduceMotion}
          onSelect={() => onChange('auto')}
        />
        <span className="mx-0.5 h-4 w-px shrink-0 bg-surface-700/70" aria-hidden="true" />
        {RESOLUTIONS.map((r) => {
          const active = value === r.seconds
          const tooFine = Boolean(rangeSeconds && rangeSeconds / r.seconds > MAX_POINTS)
          return (
            <Segment
              key={r.key}
              label={r.label}
              active={active}
              disabled={tooFine}
              reduceMotion={reduceMotion}
              onSelect={() => onChange(r.seconds)}
            />
          )
        })}
      </div>
      <div className="flex items-center gap-1.5 px-1 text-[10px] text-surface-500">
        <span className="inline-block h-1 w-1 rounded-full bg-brand-400/60" aria-hidden="true" />
        <span className="tabular-nums text-surface-400">≈ {formatPoints(estimatedPoints)}</span>
        <span>data points</span>
        {value === 'auto' && effectiveSeconds && (
          <span className="rounded bg-surface-800/80 px-1.5 py-0.5 font-medium text-surface-400">
            {autoLabel}
          </span>
        )}
      </div>
    </div>
  )
}

export default ResolutionPicker
export { ResolutionPicker, autoResolution, RESOLUTIONS }
