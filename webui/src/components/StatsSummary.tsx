interface Stats {
  min: number
  max: number
  avg: number
  p95: number
  current: number
}

function computeStats(values: number[]): Stats | null {
  if (!values.length) return null
  const sorted = [...values].sort((a, b) => a - b)
  const n = sorted.length
  const min = sorted[0]!
  const max = sorted[n - 1]!
  const sum = sorted.reduce((a, b) => a + b, 0)
  const avg = sum / n
  const p95 = sorted[Math.floor(0.95 * (n - 1))]!
  const current = values[values.length - 1]!
  return { min, max, avg, p95, current }
}

const defaultFormat = (v: number | undefined): string =>
  v === null || v === undefined || Number.isNaN(v) ? '—' : Number(v).toFixed(2)

interface StatsSummaryProps {
  data?: Array<Record<string, number | undefined>>
  valueKey?: string
  formatValue?: (v: number | undefined) => string
  className?: string
  accent?: string
}

interface Item {
  label: string
  value: number | undefined
  highlight?: boolean
}

export function StatsSummary({
  data = [],
  valueKey = 'value',
  formatValue = defaultFormat,
  className = '',
  accent,
}: StatsSummaryProps) {
  const values = (Array.isArray(data) ? data : [])
    .map((d) => Number(d?.[valueKey]))
    .filter((v) => Number.isFinite(v))

  const stats = computeStats(values)

  const items: Item[] = [
    { label: 'Min', value: stats?.min },
    { label: 'Avg', value: stats?.avg },
    { label: 'p95', value: stats?.p95 },
    { label: 'Max', value: stats?.max },
    { label: 'Current', value: stats?.current, highlight: true },
  ]

  return (
    <dl
      className={`grid grid-cols-2 gap-px overflow-hidden rounded-lg border border-surface-700/60 bg-surface-700/40 sm:grid-cols-5 ${className}`}
    >
      {items.map((item) => (
        <div
          key={item.label}
          className={`flex flex-col gap-1 px-3 py-2 ${
            item.highlight ? 'bg-surface-800/90' : 'bg-surface-800/55'
          }`}
        >
          <dt className="flex items-center gap-1.5 text-[10px] font-medium uppercase tracking-wider text-surface-500">
            {item.highlight && accent && (
              <span
                className="inline-block h-1.5 w-1.5 rounded-full"
                style={{ backgroundColor: accent }}
                aria-hidden="true"
              />
            )}
            {item.label}
          </dt>
          <dd
            className={`w-full truncate tabular-nums ${
              item.highlight
                ? 'text-sm font-semibold text-surface-100'
                : 'text-sm font-medium text-surface-300'
            }`}
            title={stats ? formatValue(item.value) : undefined}
          >
            {stats ? formatValue(item.value) : '—'}
          </dd>
        </div>
      ))}
    </dl>
  )
}

export default StatsSummary
