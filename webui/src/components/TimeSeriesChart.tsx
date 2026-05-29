import {
  useState,
  useMemo,
  useCallback,
  useRef,
  useEffect,
  useId,
  type MouseEvent as ReactMouseEvent,
  type KeyboardEvent as ReactKeyboardEvent,
} from 'react'

export interface NiceScale {
  min: number
  max: number
  step: number
  ticks: number[]
}

export function niceScale(min: number, max: number, tickCount = 5): NiceScale {
  if (!isFinite(min) || !isFinite(max)) {
    return { min: 0, max: 1, step: 0.25, ticks: [0, 0.25, 0.5, 0.75, 1] }
  }
  if (min === max) {
    const pad = Math.abs(min) > 0 ? Math.abs(min) * 0.5 : 1
    min -= pad
    max += pad
  }
  const range = max - min
  const roughStep = range / Math.max(1, tickCount)
  const pow = Math.pow(10, Math.floor(Math.log10(roughStep)))
  const norm = roughStep / pow
  let step: number
  if (norm < 1.5) step = 1 * pow
  else if (norm < 3) step = 2 * pow
  else if (norm < 7) step = 5 * pow
  else step = 10 * pow

  const niceMin = Math.floor(min / step) * step
  const niceMax = Math.ceil(max / step) * step
  const ticks: number[] = []
  for (let v = niceMin; v <= niceMax + step / 2; v += step) {
    ticks.push(Number(v.toFixed(12)))
  }
  return { min: niceMin, max: niceMax, step, ticks }
}

const MONTHS = ['Jan', 'Feb', 'Mar', 'Apr', 'May', 'Jun', 'Jul', 'Aug', 'Sep', 'Oct', 'Nov', 'Dec']

function pad2(n: number): string {
  return n < 10 ? `0${n}` : `${n}`
}

export function formatTimestamp(ts: number, rangeSeconds = 0): string {
  const d = new Date(ts * 1000)
  if (rangeSeconds >= 7 * 86400) {
    return `${MONTHS[d.getMonth()]} ${pad2(d.getDate())}`
  }
  if (rangeSeconds >= 86400) {
    return `${MONTHS[d.getMonth()]} ${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}`
  }
  return `${pad2(d.getHours())}:${pad2(d.getMinutes())}`
}

function formatTooltipTimestamp(ts: number): string {
  const d = new Date(ts * 1000)
  return `${MONTHS[d.getMonth()]} ${pad2(d.getDate())} ${pad2(d.getHours())}:${pad2(d.getMinutes())}:${pad2(d.getSeconds())}`
}

function defaultFormat(v: number | null | undefined, unit = ''): string {
  if (v === null || v === undefined || !isFinite(v)) return '—'
  const abs = Math.abs(v)
  let s: string
  if (abs >= 1000) s = v.toFixed(0)
  else if (abs >= 10) s = v.toFixed(1)
  else s = v.toFixed(2)
  return `${s}${unit}`
}

const prefersReducedMotion =
  typeof window !== 'undefined' &&
  typeof window.matchMedia === 'function' &&
  window.matchMedia('(prefers-reduced-motion: reduce)').matches

const VIEW_W = 800
const PAD_LEFT_BASE = 52
const PAD_RIGHT_BASE = 18
const PAD_TOP = 14
const PAD_BOTTOM = 34

export type ChartDatum = { timestamp: number } & Record<string, number | undefined>

export interface SeriesSpec {
  key: string
  label: string
  color: string
  fillColor?: string
  unit?: string
  formatValue?: (v: number) => string
  yAxis?: 'left' | 'right'
  yMax?: number
}

export interface TimeSeriesChartProps {
  data?: ChartDatum[]
  series?: SeriesSpec[]
  height?: number
  yMin?: number
  yMax?: number
  formatValue?: (v: number) => string
  emptyMessage?: string
  onRangeSelect?: (range: { from: number; to: number } | null) => void
}

interface DragState {
  startX: number
  currentX: number
}

interface SeriesRender {
  key: string
  label: string
  color: string
  fillColor: string
  unit: string
  formatValue?: (v: number) => string
  pathD: string
  areaD: string
  points: Array<{ x: number; y: number; value: number; timestamp: number } | null>
  axisSide: 'left' | 'right'
}

// Smooth a sequence of points using Catmull-Rom -> cubic Bezier.
function smoothSegment(pts: Array<{ x: number; y: number }>): string {
  if (pts.length === 0) return ''
  if (pts.length === 1) return `M ${pts[0]!.x} ${pts[0]!.y}`
  if (pts.length === 2) return `M ${pts[0]!.x} ${pts[0]!.y} L ${pts[1]!.x} ${pts[1]!.y}`
  let d = `M ${pts[0]!.x.toFixed(2)} ${pts[0]!.y.toFixed(2)}`
  const t = 0.16
  for (let i = 0; i < pts.length - 1; i++) {
    const p0 = pts[i - 1] ?? pts[i]!
    const p1 = pts[i]!
    const p2 = pts[i + 1]!
    const p3 = pts[i + 2] ?? p2
    const c1x = p1.x + (p2.x - p0.x) * t
    const c1y = p1.y + (p2.y - p0.y) * t
    const c2x = p2.x - (p3.x - p1.x) * t
    const c2y = p2.y - (p3.y - p1.y) * t
    d += ` C ${c1x.toFixed(2)} ${c1y.toFixed(2)}, ${c2x.toFixed(2)} ${c2y.toFixed(2)}, ${p2.x.toFixed(2)} ${p2.y.toFixed(2)}`
  }
  return d
}

export function TimeSeriesChart({
  data = [],
  series = [],
  height = 240,
  yMin = 0,
  yMax,
  formatValue,
  emptyMessage = 'No data available',
  onRangeSelect,
}: TimeSeriesChartProps) {
  const svgRef = useRef<SVGSVGElement | null>(null)
  const uid = useId().replace(/[^a-zA-Z0-9_-]/g, '')
  const [hoverIdx, setHoverIdx] = useState<number | null>(null)
  const [zoom, setZoom] = useState<{ from: number; to: number } | null>(null)
  const [drag, setDrag] = useState<DragState | null>(null)

  const hasRightAxis = useMemo(() => series.some((s) => s.yAxis === 'right'), [series])
  const padLeft = PAD_LEFT_BASE
  const padRight = hasRightAxis ? PAD_LEFT_BASE : PAD_RIGHT_BASE
  const chartW = VIEW_W - padLeft - padRight
  const chartH = height - PAD_TOP - PAD_BOTTOM

  const visibleData = useMemo(() => {
    if (!Array.isArray(data) || data.length === 0) return []
    if (!zoom) return data
    return data.filter((d) => d.timestamp >= zoom.from && d.timestamp <= zoom.to)
  }, [data, zoom])

  const scales = useMemo(() => {
    if (visibleData.length === 0) return null
    const first = visibleData[0]!
    const last = visibleData[visibleData.length - 1]!
    const minTs = first.timestamp
    const maxTs = last.timestamp
    const tsRange = Math.max(maxTs - minTs, 1)

    function buildYScale(axisSide: 'left' | 'right'): { min: number; max: number; ticks: number[] } | null {
      const axisSeries = series.filter((s) => (s.yAxis ?? 'left') === axisSide)
      if (axisSeries.length === 0) return null
      let allMin = Infinity
      let allMax = -Infinity
      let overrideMax: number | undefined
      for (const s of axisSeries) {
        if (s.yMax !== undefined) {
          overrideMax = overrideMax === undefined ? s.yMax : Math.max(overrideMax, s.yMax)
        }
        for (const d of visibleData) {
          const v = d[s.key]
          if (typeof v === 'number' && isFinite(v)) {
            if (v < allMin) allMin = v
            if (v > allMax) allMax = v
          }
        }
      }
      if (!isFinite(allMin) || !isFinite(allMax)) {
        allMin = 0
        allMax = 1
      }
      const lo = yMin !== undefined ? Math.min(yMin, allMin) : allMin
      const hi = overrideMax ?? yMax ?? allMax
      const { min, max, ticks } = niceScale(lo, hi <= lo ? lo + 1 : hi, 5)
      return { min, max, ticks }
    }

    const left = buildYScale('left')
    const right = hasRightAxis ? buildYScale('right') : null

    const toX = (ts: number) => padLeft + ((ts - minTs) / tsRange) * chartW
    const toYFor = (scale: { min: number; max: number }) => (v: number) =>
      PAD_TOP + chartH - ((v - scale.min) / Math.max(scale.max - scale.min, 1e-9)) * chartH

    return { minTs, maxTs, tsRange, left, right, toX, toYFor }
  }, [visibleData, series, yMin, yMax, hasRightAxis, padLeft, chartW, chartH])

  const seriesRender: SeriesRender[] = useMemo(() => {
    if (!scales || visibleData.length === 0) return []
    return series
      .map((s): SeriesRender | null => {
        const axisSide: 'left' | 'right' = s.yAxis ?? 'left'
        const scale = axisSide === 'right' ? scales.right : scales.left
        if (!scale) return null
        const toY = scales.toYFor(scale)
        const pts: SeriesRender['points'] = []
        // Build a smooth path that breaks at gaps (null values).
        let pathD = ''
        let segment: Array<{ x: number; y: number }> = []
        const flush = () => {
          if (segment.length) {
            pathD += `${smoothSegment(segment)} `
            segment = []
          }
        }
        for (let i = 0; i < visibleData.length; i++) {
          const d = visibleData[i]!
          const v = d[s.key]
          if (typeof v !== 'number' || !isFinite(v)) {
            flush()
            pts.push(null)
            continue
          }
          const x = scales.toX(d.timestamp)
          const y = toY(v)
          pts.push({ x, y, value: v, timestamp: d.timestamp })
          segment.push({ x, y })
        }
        flush()
        const validPts = pts.filter((p): p is NonNullable<typeof p> => p !== null)
        let areaD = ''
        if (validPts.length > 1) {
          const baselineY = PAD_TOP + chartH
          const top = smoothSegment(validPts.map((p) => ({ x: p.x, y: p.y })))
          areaD = `${top} L ${validPts[validPts.length - 1]!.x} ${baselineY} L ${validPts[0]!.x} ${baselineY} Z`
        }
        return {
          key: s.key,
          label: s.label,
          color: s.color,
          fillColor: s.fillColor ?? s.color,
          unit: s.unit ?? '',
          formatValue: s.formatValue,
          pathD: pathD.trim(),
          areaD,
          points: pts,
          axisSide,
        }
      })
      .filter((s): s is SeriesRender => s !== null)
  }, [scales, visibleData, series, chartH])

  const xTicks = useMemo(() => {
    if (!scales || visibleData.length === 0) return []
    const count = Math.min(5, visibleData.length)
    const rangeSeconds = scales.maxTs - scales.minTs
    const out: Array<{ x: number; label: string; anchor: 'start' | 'middle' | 'end' }> = []
    for (let i = 0; i < count; i++) {
      const t = scales.minTs + ((scales.maxTs - scales.minTs) * i) / Math.max(count - 1, 1)
      out.push({
        x: scales.toX(t),
        label: formatTimestamp(t, rangeSeconds),
        anchor: i === 0 ? 'start' : i === count - 1 ? 'end' : 'middle',
      })
    }
    return out
  }, [scales, visibleData])

  const clientToSvgX = useCallback((clientX: number): number => {
    if (!svgRef.current) return 0
    const rect = svgRef.current.getBoundingClientRect()
    return ((clientX - rect.left) / rect.width) * VIEW_W
  }, [])

  const nearestIdx = useCallback(
    (svgX: number): number | null => {
      if (!scales || visibleData.length === 0) return null
      let bestI = 0
      let bestD = Infinity
      for (let i = 0; i < visibleData.length; i++) {
        const x = scales.toX(visibleData[i]!.timestamp)
        const dist = Math.abs(x - svgX)
        if (dist < bestD) {
          bestD = dist
          bestI = i
        }
      }
      return bestI
    },
    [scales, visibleData]
  )

  const handleMove = useCallback(
    (e: ReactMouseEvent<SVGSVGElement>) => {
      const svgX = clientToSvgX(e.clientX)
      setHoverIdx(nearestIdx(svgX))
      if (drag) {
        setDrag({ ...drag, currentX: svgX })
      }
    },
    [clientToSvgX, nearestIdx, drag]
  )

  const handleLeave = useCallback(() => {
    setHoverIdx(null)
    setDrag(null)
  }, [])

  const handleDown = useCallback(
    (e: ReactMouseEvent<SVGSVGElement>) => {
      if (e.button !== 0) return
      const svgX = clientToSvgX(e.clientX)
      setDrag({ startX: svgX, currentX: svgX })
    },
    [clientToSvgX]
  )

  const handleUp = useCallback(
    (e: ReactMouseEvent<SVGSVGElement>) => {
      if (!drag || !scales) {
        setDrag(null)
        return
      }
      const endX = clientToSvgX(e.clientX)
      const x1 = Math.min(drag.startX, endX)
      const x2 = Math.max(drag.startX, endX)
      setDrag(null)
      if (x2 - x1 < 8) return
      const clampToChart = (x: number) => Math.max(padLeft, Math.min(padLeft + chartW, x))
      const xa = clampToChart(x1)
      const xb = clampToChart(x2)
      const tsFrom = scales.minTs + ((xa - padLeft) / chartW) * scales.tsRange
      const tsTo = scales.minTs + ((xb - padLeft) / chartW) * scales.tsRange
      const from = Math.round(tsFrom)
      const to = Math.round(tsTo)
      setZoom({ from, to })
      if (onRangeSelect) onRangeSelect({ from, to })
    },
    [drag, scales, clientToSvgX, padLeft, chartW, onRangeSelect]
  )

  const handleKeyDown = useCallback(
    (e: ReactKeyboardEvent<SVGSVGElement>) => {
      if (visibleData.length === 0) return
      if (e.key === 'ArrowRight' || e.key === 'ArrowLeft') {
        e.preventDefault()
        setHoverIdx((prev) => {
          const start = prev ?? (e.key === 'ArrowRight' ? -1 : visibleData.length)
          const next = e.key === 'ArrowRight' ? start + 1 : start - 1
          return Math.max(0, Math.min(visibleData.length - 1, next))
        })
      } else if (e.key === 'Home') {
        e.preventDefault()
        setHoverIdx(0)
      } else if (e.key === 'End') {
        e.preventDefault()
        setHoverIdx(visibleData.length - 1)
      } else if (e.key === 'Escape' && zoom) {
        e.preventDefault()
        setZoom(null)
        if (onRangeSelect) onRangeSelect(null)
      }
    },
    [visibleData, zoom, onRangeSelect]
  )

  const resetZoom = useCallback(() => {
    setZoom(null)
    if (onRangeSelect) onRangeSelect(null)
  }, [onRangeSelect])

  useEffect(() => {
    if (!zoom || !data || data.length === 0) return
    const first = data[0]!.timestamp
    const last = data[data.length - 1]!.timestamp
    if (zoom.from > last || zoom.to < first) {
      setZoom(null)
    }
  }, [data, zoom])

  const ariaLabel = useMemo(() => {
    if (series.length === 0) return 'Time series chart'
    return `Time series chart showing ${series.map((s) => s.label).join(', ')}. Use arrow keys to inspect data points.`
  }, [series])

  if (!Array.isArray(data) || data.length === 0 || series.length === 0) {
    return (
      <div
        className="flex flex-col items-center justify-center gap-2 rounded-lg border border-dashed border-surface-700/60 bg-surface-800/30 text-sm text-surface-500"
        style={{ height }}
        role="img"
        aria-label={emptyMessage}
      >
        <svg viewBox="0 0 24 24" className="h-6 w-6 text-surface-600" fill="none" aria-hidden="true">
          <path d="M3 17l5-5 4 4 8-9" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" strokeLinejoin="round" />
          <path d="M3 21h18" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round" />
        </svg>
        {emptyMessage}
      </div>
    )
  }

  const leftScale = scales?.left
  const rightScale = scales?.right
  const hoverPoint = hoverIdx !== null && visibleData[hoverIdx] ? visibleData[hoverIdx] : null

  let tooltipX = 0
  let tooltipFlip = false
  if (hoverPoint && scales) {
    tooltipX = scales.toX(hoverPoint.timestamp)
    if (tooltipX + 188 > padLeft + chartW) tooltipFlip = true
  }

  const tooltipW = 184
  const tooltipH = 30 + series.length * 17

  const areaAnim = prefersReducedMotion
    ? undefined
    : ({ animation: 'tsc-fade 0.7s ease-out forwards' } as const)

  return (
    <div className="relative text-surface-200">
      <svg
        ref={svgRef}
        viewBox={`0 0 ${VIEW_W} ${height}`}
        className="w-full cursor-crosshair select-none rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/60"
        style={{ height }}
        role="img"
        aria-label={ariaLabel}
        tabIndex={0}
        onMouseMove={handleMove}
        onMouseLeave={handleLeave}
        onMouseDown={handleDown}
        onMouseUp={handleUp}
        onKeyDown={handleKeyDown}
        onBlur={() => setHoverIdx(null)}
      >
        <defs>
          {seriesRender.map((s) => (
            <linearGradient key={`grad-${s.key}`} id={`tsc-area-${uid}-${s.key}`} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={s.fillColor} stopOpacity="0.28" />
              <stop offset="100%" stopColor={s.fillColor} stopOpacity="0.02" />
            </linearGradient>
          ))}
        </defs>

        {/* Vertical gridlines aligned with x ticks */}
        {scales &&
          xTicks.map((t, i) => (
            <line
              key={`xg-${i}`}
              x1={t.x}
              y1={PAD_TOP}
              x2={t.x}
              y2={PAD_TOP + chartH}
              stroke="currentColor"
              strokeOpacity={0.05}
              strokeWidth={1}
            />
          ))}

        {/* Horizontal gridlines + left axis labels */}
        {leftScale && scales &&
          leftScale.ticks.map((v, i) => {
            const y = scales.toYFor(leftScale)(v)
            const leftSeries = series.find((s) => (s.yAxis ?? 'left') === 'left')
            const fmt = leftSeries?.formatValue ?? formatValue ?? ((x: number) => defaultFormat(x, leftSeries?.unit ?? ''))
            return (
              <g key={`yl-${i}`}>
                <line
                  x1={padLeft}
                  y1={y}
                  x2={padLeft + chartW}
                  y2={y}
                  stroke="currentColor"
                  strokeOpacity={i === 0 ? 0.18 : 0.08}
                  strokeWidth={1}
                />
                <text
                  x={padLeft - 8}
                  y={y + 3.5}
                  textAnchor="end"
                  fontSize={10}
                  fill="currentColor"
                  fillOpacity={0.55}
                  className="tabular-nums"
                >
                  {fmt(v)}
                </text>
              </g>
            )
          })}

        {rightScale && scales &&
          rightScale.ticks.map((v, i) => {
            const y = scales.toYFor(rightScale)(v)
            const rightSeries = series.find((s) => s.yAxis === 'right')
            const fmt = rightSeries?.formatValue ?? formatValue ?? ((x: number) => defaultFormat(x, rightSeries?.unit ?? ''))
            return (
              <text
                key={`yr-${i}`}
                x={padLeft + chartW + 8}
                y={y + 3.5}
                textAnchor="start"
                fontSize={10}
                fill="currentColor"
                fillOpacity={0.55}
                className="tabular-nums"
              >
                {fmt(v)}
              </text>
            )
          })}

        {seriesRender.map((s) => (
          <g key={s.key}>
            {s.areaD && <path d={s.areaD} fill={`url(#tsc-area-${uid}-${s.key})`} style={areaAnim} />}
            {s.pathD && (
              <path
                d={s.pathD}
                fill="none"
                stroke={s.color}
                strokeWidth={2}
                strokeLinejoin="round"
                strokeLinecap="round"
                style={
                  prefersReducedMotion
                    ? undefined
                    : { strokeDasharray: 2400, strokeDashoffset: 2400, animation: 'tsc-line 0.8s ease-out forwards' }
                }
              />
            )}
          </g>
        ))}

        {drag && Math.abs(drag.currentX - drag.startX) > 2 && (
          <rect
            x={Math.min(drag.startX, drag.currentX)}
            y={PAD_TOP}
            width={Math.abs(drag.currentX - drag.startX)}
            height={chartH}
            fill="#22d3ee"
            fillOpacity={0.12}
            stroke="#22d3ee"
            strokeOpacity={0.45}
            strokeWidth={1}
          />
        )}

        {xTicks.map((t, i) => (
          <text
            key={`xt-${i}`}
            x={t.x}
            y={PAD_TOP + chartH + 18}
            textAnchor={t.anchor}
            fontSize={10}
            fill="currentColor"
            fillOpacity={0.55}
            className="tabular-nums"
          >
            {t.label}
          </text>
        ))}

        {hoverPoint && scales && hoverIdx !== null && (
          <g pointerEvents="none">
            <line
              x1={scales.toX(hoverPoint.timestamp)}
              y1={PAD_TOP}
              x2={scales.toX(hoverPoint.timestamp)}
              y2={PAD_TOP + chartH}
              stroke="currentColor"
              strokeOpacity={0.4}
              strokeDasharray="4 4"
              strokeWidth={1}
            />
            {seriesRender.map((s) => {
              const p = s.points[hoverIdx]
              if (!p) return null
              return (
                <g key={`hp-${s.key}`}>
                  <circle cx={p.x} cy={p.y} r={7} fill={s.color} fillOpacity={0.18} />
                  <circle cx={p.x} cy={p.y} r={4} fill={s.color} stroke="#09090b" strokeWidth={1.75} />
                </g>
              )
            })}

            <g
              transform={`translate(${
                tooltipFlip ? tooltipX - tooltipW - 12 : tooltipX + 12
              }, ${PAD_TOP + 6})`}
            >
              <rect
                x={0}
                y={0}
                width={tooltipW}
                height={tooltipH}
                rx={8}
                fill="rgba(9, 9, 11, 0.94)"
                stroke="rgba(255,255,255,0.12)"
              />
              <text x={11} y={17} fontSize={10} fill="rgba(255,255,255,0.6)" className="tabular-nums">
                {formatTooltipTimestamp(hoverPoint.timestamp)}
              </text>
              <line x1={11} y1={23} x2={tooltipW - 11} y2={23} stroke="rgba(255,255,255,0.08)" strokeWidth={1} />
              {seriesRender.map((s, i) => {
                const p = s.points[hoverIdx]
                const fmt = s.formatValue ?? formatValue ?? ((x: number) => defaultFormat(x, s.unit))
                return (
                  <g key={`tt-${s.key}`} transform={`translate(11, ${37 + i * 17})`}>
                    <rect x={0} y={-8} width={8} height={8} fill={s.color} rx={2} />
                    <text x={15} y={0} fontSize={11} fill="rgba(255,255,255,0.85)">
                      {s.label}
                    </text>
                    <text
                      x={tooltipW - 22}
                      y={0}
                      fontSize={11}
                      textAnchor="end"
                      fill="white"
                      fontWeight={600}
                      className="tabular-nums"
                    >
                      {p ? fmt(p.value) : '—'}
                    </text>
                  </g>
                )
              })}
            </g>
          </g>
        )}

        {!prefersReducedMotion && (
          <style>{`
            @keyframes tsc-fade { from { opacity: 0; } to { opacity: 1; } }
            @keyframes tsc-line { to { stroke-dashoffset: 0; } }
          `}</style>
        )}
      </svg>

      {zoom && (
        <button
          type="button"
          onClick={resetZoom}
          className="absolute right-2 top-2 inline-flex items-center gap-1 rounded-md bg-surface-900/85 px-2 py-1 text-xs font-medium text-surface-200 ring-1 ring-white/10 backdrop-blur transition-colors hover:bg-surface-800 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
        >
          <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" fill="none" aria-hidden="true">
            <path d="M2.5 8a5.5 5.5 0 1 1 1.6 3.9M2.5 12V8.5H6" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" strokeLinejoin="round" />
          </svg>
          Reset zoom
        </button>
      )}

      {series.length > 1 && (
        <div className="mt-2.5 flex flex-wrap gap-x-4 gap-y-1.5 text-xs text-surface-300">
          {series.map((s) => (
            <div key={s.key} className="flex items-center gap-1.5">
              <span
                className="inline-block h-2.5 w-2.5 rounded-full ring-2 ring-inset ring-black/20"
                style={{ backgroundColor: s.color }}
                aria-hidden
              />
              <span>{s.label}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  )
}

export default TimeSeriesChart
