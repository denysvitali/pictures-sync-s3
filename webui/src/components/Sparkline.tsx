import { useId } from 'react'

interface SparklineProps {
  data?: Array<Record<string, number | undefined>>
  valueKey?: string
  color?: string
  width?: number
  height?: number
  strokeWidth?: number
  fill?: boolean
}

const prefersReducedMotion =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches

// Catmull-Rom -> cubic Bezier smoothing for a fluid line.
function smoothPath(points: ReadonlyArray<readonly [number, number]>): string {
  if (points.length < 2) return ''
  if (points.length === 2) {
    return `M ${points[0]![0]} ${points[0]![1]} L ${points[1]![0]} ${points[1]![1]}`
  }
  let d = `M ${points[0]![0].toFixed(2)} ${points[0]![1].toFixed(2)}`
  for (let i = 0; i < points.length - 1; i++) {
    const p0 = points[i - 1] ?? points[i]!
    const p1 = points[i]!
    const p2 = points[i + 1]!
    const p3 = points[i + 2] ?? p2
    const t = 0.18
    const c1x = p1[0] + (p2[0] - p0[0]) * t
    const c1y = p1[1] + (p2[1] - p0[1]) * t
    const c2x = p2[0] - (p3[0] - p1[0]) * t
    const c2y = p2[1] - (p3[1] - p1[1]) * t
    d += ` C ${c1x.toFixed(2)} ${c1y.toFixed(2)}, ${c2x.toFixed(2)} ${c2y.toFixed(2)}, ${p2[0].toFixed(2)} ${p2[1].toFixed(2)}`
  }
  return d
}

export function Sparkline({
  data = [],
  valueKey = 'value',
  color = '#f59e0b',
  width = 120,
  height = 32,
  strokeWidth = 1.75,
  fill = true,
}: SparklineProps) {
  const reactId = useId()
  const gradientId = `spark-grad-${reactId}`

  if (!Array.isArray(data) || data.length < 2) {
    return (
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label="Trend sparkline (insufficient data)"
        preserveAspectRatio="none"
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
  const pad = range * 0.12

  const yMin = min - pad
  const yMax = max + pad
  const yRange = yMax - yMin || 1

  const n = values.length
  const stepX = width / (n - 1)
  // Inset vertically so the stroke + dot are never clipped at the edges.
  const vPad = strokeWidth + 1.5
  const usableH = Math.max(1, height - vPad * 2)

  const points = values.map((v, i) => {
    const x = i * stepX
    const y = vPad + (usableH - ((v - yMin) / yRange) * usableH)
    return [x, y] as const
  })

  const pathD = smoothPath(points)
  const last = points[points.length - 1]!
  const areaD = `${pathD} L ${width.toFixed(2)} ${height.toFixed(2)} L 0 ${height.toFixed(2)} Z`

  // Approximate path length for the draw-in animation.
  let approxLen = 0
  for (let i = 1; i < points.length; i++) {
    approxLen += Math.hypot(points[i]![0] - points[i - 1]![0], points[i]![1] - points[i - 1]![1])
  }

  return (
    <svg
      width={width}
      height={height}
      viewBox={`0 0 ${width} ${height}`}
      role="img"
      aria-label="Trend sparkline"
      preserveAspectRatio="none"
      style={{ overflow: 'visible' }}
    >
      <defs>
        <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.32" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      {fill && <path d={areaD} fill={`url(#${gradientId})`} stroke="none" />}
      <path
        d={pathD}
        fill="none"
        stroke={color}
        strokeWidth={strokeWidth}
        strokeLinecap="round"
        strokeLinejoin="round"
        vectorEffect="non-scaling-stroke"
        style={
          prefersReducedMotion
            ? undefined
            : {
                strokeDasharray: approxLen,
                strokeDashoffset: approxLen,
                animation: 'spark-draw 0.6s ease-out forwards',
              }
        }
      />
      <circle cx={last[0]} cy={last[1]} r={strokeWidth + 0.75} fill={color} />
      <circle cx={last[0]} cy={last[1]} r={strokeWidth + 0.75} fill={color} opacity={0.35}>
        {!prefersReducedMotion && (
          <animate
            attributeName="r"
            values={`${strokeWidth + 0.75};${strokeWidth + 3.5};${strokeWidth + 0.75}`}
            dur="2s"
            repeatCount="indefinite"
          />
        )}
      </circle>
      <style>{`@keyframes spark-draw { to { stroke-dashoffset: 0; } }`}</style>
    </svg>
  )
}

export default Sparkline
