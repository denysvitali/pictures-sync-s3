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
}

export function StatsSummary({
  data = [],
  valueKey = 'value',
  formatValue = defaultFormat,
  className = '',
}: StatsSummaryProps) {
  const values = (Array.isArray(data) ? data : [])
    .map((d) => Number(d?.[valueKey]))
    .filter((v) => Number.isFinite(v))

  const stats = computeStats(values)

  const items: Array<{ label: string; value: number | undefined }> = [
    { label: 'Min', value: stats?.min },
    { label: 'Max', value: stats?.max },
    { label: 'Avg', value: stats?.avg },
    { label: 'p95', value: stats?.p95 },
    { label: 'Current', value: stats?.current },
  ]

  return (
    <dl
      className={`grid grid-cols-5 gap-2 rounded-md border border-surface-700/60 bg-surface-800/55 p-2 ${className}`}
    >
      {items.map((item) => (
        <div key={item.label} className="flex flex-col items-start min-w-0">
          <dt className="text-xs uppercase tracking-wide text-surface-500">{item.label}</dt>
          <dd className="text-sm font-medium text-surface-200 tabular-nums truncate w-full">
            {stats ? formatValue(item.value) : '—'}
          </dd>
        </div>
      ))}
    </dl>
  )
}

export default StatsSummary
