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
  up: 'bg-emerald-500/15 text-emerald-300 border border-emerald-500/30',
  down: 'bg-rose-500/15 text-rose-300 border border-rose-500/30',
  flat: 'bg-surface-700/40 text-surface-400 border border-surface-700/60',
}

const arrows: Record<Direction, string> = { up: '▲', down: '▼', flat: '–' }

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
    <Card className={className}>
      <div className="flex items-start justify-between gap-2">
        <span className="text-xs uppercase tracking-wide text-surface-500">{title}</span>
        {hint && <span className="text-[10px] text-surface-500">{hint}</span>}
      </div>

      <div className="mt-2 flex items-end justify-between gap-3">
        <div className="flex flex-col min-w-0">
          <div className="flex items-baseline gap-1.5 flex-wrap">
            <span className="text-2xl font-semibold text-surface-200 tabular-nums leading-none">
              {displayValue}
            </span>
            {!formatValue && unit && (
              <span className="text-sm text-surface-500">{unit}</span>
            )}
            <span
              className={`inline-flex items-center gap-0.5 text-xs rounded-md px-1.5 py-0.5 ${pillClasses[direction]}`}
              aria-label={`change ${pctLabel}`}
            >
              <span aria-hidden="true">{arrows[direction]}</span>
              <span>{pctLabel}</span>
            </span>
          </div>
        </div>

        <div className="shrink-0 w-2/5 flex justify-end" style={{ color }}>
          <Sparkline data={data} valueKey={valueKey} color={color} width={120} height={32} />
        </div>
      </div>
    </Card>
  )
}

export default MetricCard
