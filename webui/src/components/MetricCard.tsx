import { Card } from './Card.jsx'
import { Sparkline } from './Sparkline'

type Direction = 'up' | 'down' | 'flat'

function computeTrend(
  data: Array<Record<string, number | undefined>>,
  valueKey: string
): { direction: Direction; pct: number } {
  if (!Array.isArray(data) || data.length < 2) return { direction: 'flat', pct: 0 }
  const first = Number(data[0]?.[valueKey])
  const last = Number(data[data.length - 1]?.[valueKey])
  if (!Number.isFinite(first) || !Number.isFinite(last)) return { direction: 'flat', pct: 0 }
  if (first === 0 && last === 0) return { direction: 'flat', pct: 0 }
  const base = first === 0 ? 1 : Math.abs(first)
  const pct = ((last - first) / base) * 100
  const direction: Direction = Math.abs(pct) < 0.5 ? 'flat' : pct > 0 ? 'up' : 'down'
  return { direction, pct }
}

const pillClasses: Record<Direction, string> = {
  up: 'bg-emerald-500/15 text-emerald-300 ring-1 ring-inset ring-emerald-500/30',
  down: 'bg-rose-500/15 text-rose-300 ring-1 ring-inset ring-rose-500/30',
  flat: 'bg-surface-700/40 text-surface-400 ring-1 ring-inset ring-surface-600/50',
}

const trendWord: Record<Direction, string> = { up: 'increased', down: 'decreased', flat: 'steady' }

interface MetricCardProps {
  title: string
  value?: number | null
  unit?: string
  formatValue?: (v: number | undefined | null) => string
  data?: Array<Record<string, number | undefined>>
  valueKey?: string
  color?: string
  trend?: Direction
  hint?: string
  className?: string
}

function TrendArrow({ direction }: { direction: Direction }) {
  if (direction === 'flat') {
    return (
      <svg viewBox="0 0 12 12" className="h-3 w-3" fill="none" aria-hidden="true">
        <path d="M2.5 6h7" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
      </svg>
    )
  }
  const up = direction === 'up'
  return (
    <svg viewBox="0 0 12 12" className="h-3 w-3" fill="none" aria-hidden="true">
      <path
        d={up ? 'M6 9.5V2.5M6 2.5 3 5.5M6 2.5 9 5.5' : 'M6 2.5v7M6 9.5 3 6.5M6 9.5 9 6.5'}
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

export function MetricCard({
  title,
  value,
  unit = '',
  formatValue,
  data = [],
  valueKey = 'value',
  color = '#f59e0b',
  trend,
  hint,
  className = '',
}: MetricCardProps) {
  const computed = computeTrend(data, valueKey)
  const direction: Direction = trend || computed.direction
  const pct = computed.pct

  const displayValue =
    typeof formatValue === 'function'
      ? formatValue(value)
      : value === null || value === undefined || Number.isNaN(value)
        ? '—'
        : String(value)

  const pctLabel =
    direction === 'flat'
      ? '0%'
      : `${pct > 0 ? '+' : ''}${pct.toFixed(pct >= 10 || pct <= -10 ? 0 : 1)}%`

  return (
    <Card glow className={`relative overflow-hidden ${className}`}>
      {/* Accent edge tinted by the series color. */}
      <span
        className="pointer-events-none absolute inset-y-0 left-0 w-1"
        style={{ background: `linear-gradient(to bottom, ${color}, transparent)` }}
        aria-hidden="true"
      />

      <div className="flex items-center justify-between gap-2">
        <span className="flex items-center gap-1.5 text-xs font-medium uppercase tracking-wider text-surface-400">
          <span
            className="inline-block h-2 w-2 rounded-full"
            style={{ backgroundColor: color }}
            aria-hidden="true"
          />
          {title}
        </span>
        {hint && (
          <span className="truncate text-[10px] text-surface-500" title={hint}>
            {hint}
          </span>
        )}
      </div>

      <div className="mt-3 flex items-end justify-between gap-3">
        <div className="flex min-w-0 flex-col gap-1.5">
          <div className="flex items-baseline gap-1">
            <span className="text-3xl font-semibold leading-none tracking-tight text-surface-100 tabular-nums">
              {displayValue}
            </span>
            {!formatValue && unit && <span className="text-sm text-surface-500">{unit}</span>}
          </div>
          <span
            className={`inline-flex w-fit items-center gap-1 rounded-md px-1.5 py-0.5 text-[11px] font-medium tabular-nums ${pillClasses[direction]}`}
          >
            <TrendArrow direction={direction} />
            <span>{pctLabel}</span>
            <span className="sr-only"> {trendWord[direction]} over range</span>
          </span>
        </div>

        <div className="flex shrink-0 basis-[44%] justify-end pb-0.5" style={{ color }}>
          <Sparkline data={data} valueKey={valueKey} color={color} width={130} height={40} />
        </div>
      </div>
    </Card>
  )
}

export default MetricCard
