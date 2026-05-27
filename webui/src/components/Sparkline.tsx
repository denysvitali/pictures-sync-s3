interface SparklineProps {
  data?: Array<Record<string, number | undefined>>
  valueKey?: string
  color?: string
  width?: number
  height?: number
  strokeWidth?: number
  fill?: boolean
}

export function Sparkline({
  data = [],
  valueKey = 'value',
  color = '#f59e0b',
  width = 120,
  height = 32,
  strokeWidth = 1.5,
  fill = true,
}: SparklineProps) {
  if (!Array.isArray(data) || data.length < 2) {
    return (
      <svg
        width={width}
        height={height}
        role="img"
        aria-label="sparkline (insufficient data)"
        className="opacity-40"
      >
        <line
          x1="0"
          y1={height / 2}
          x2={width}
          y2={height / 2}
          stroke="currentColor"
          strokeWidth="1"
          strokeDasharray="2 3"
          className="text-surface-500"
        />
      </svg>
    )
  }

  const values = data.map((d) => Number(d?.[valueKey]) || 0)
  const min = Math.min(...values)
  const max = Math.max(...values)
  const range = max - min || 1
  const pad = range * 0.08

  const yMin = min - pad
  const yMax = max + pad
  const yRange = yMax - yMin || 1

  const n = values.length
  const stepX = width / (n - 1)

  const points = values.map((v, i) => {
    const x = i * stepX
    const y = height - ((v - yMin) / yRange) * height
    return [x, y] as const
  })

  const pathD = points
    .map(([x, y], i) => (i === 0 ? `M ${x.toFixed(2)} ${y.toFixed(2)}` : `L ${x.toFixed(2)} ${y.toFixed(2)}`))
    .join(' ')

  const areaD = `${pathD} L ${width.toFixed(2)} ${height.toFixed(2)} L 0 ${height.toFixed(2)} Z`
  const gradientId = `spark-grad-${Math.abs(
    (color + width + height + n).split('').reduce((a, c) => a + c.charCodeAt(0), 0)
  )}`

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label="trend sparkline"
      preserveAspectRatio="none"
    >
      {fill && (
        <>
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity="0.35" />
              <stop offset="100%" stopColor={color} stopOpacity="0" />
            </linearGradient>
          </defs>
          <path d={areaD} fill={`url(#${gradientId})`} stroke="none" />
        </>
      )}
      <path
        d={pathD}
        fill="none"
        stroke={color}
        strokeWidth={strokeWidth}
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
  )
}

export default Sparkline
