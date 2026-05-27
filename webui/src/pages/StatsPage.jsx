import { useState, useEffect, useMemo, useCallback } from 'react'
import { useDevice } from '../DeviceContext.jsx'
import { getSystemStats } from '../api.js'
import { Card } from '../components/Card.jsx'
import { LoadingSpinner } from '../components/LoadingSpinner.jsx'

function formatBytes(bytes) {
  if (bytes === 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB']
  const i = Math.floor(Math.log(bytes) / Math.log(k))
  return `${(bytes / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`
}

function formatTime(ts) {
  const d = new Date(ts * 1000)
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' })
}

function TimeSeriesChart({ data, valueKey, color, fillColor, label, unit = '', yMax, formatValue }) {
  const [hoverIdx, setHoverIdx] = useState(null)

  const { path, areaPath, points, viewBox, padLeft, padTop, chartW, chartH, toY, maxVal } = useMemo(() => {
    if (data.length === 0) {
      return { path: '', points: [], viewBox: '0 0 100 100', padLeft: 0, padTop: 0, chartW: 0, chartH: 0, toY: () => 0, maxVal: 1 }
    }

    const width = 800
    const height = 240
    const padLeft = 50
    const padRight = 16
    const padTop = 12
    const padBottom = 32

    const chartW = width - padLeft - padRight
    const chartH = height - padTop - padBottom

    const values = data.map((d) => d[valueKey])
    const minVal = 0
    const maxVal = yMax ?? Math.max(...values, 1) * 1.05

    const minTs = data[0].timestamp
    const maxTs = data[data.length - 1].timestamp
    const tsRange = Math.max(maxTs - minTs, 1)

    const toX = (ts) => padLeft + ((ts - minTs) / tsRange) * chartW
    const toY = (v) => padTop + chartH - (v / maxVal) * chartH

    let d = `M ${toX(data[0].timestamp)} ${toY(values[0])}`
    for (let i = 1; i < data.length; i++) {
      d += ` L ${toX(data[i].timestamp)} ${toY(values[i])}`
    }
    const areaD = `${d} L ${toX(data[data.length - 1].timestamp)} ${padTop + chartH} L ${toX(data[0].timestamp)} ${padTop + chartH} Z`

    const pts = data.map((p, i) => ({
      x: toX(p.timestamp),
      y: toY(p[valueKey]),
      idx: i,
      value: p[valueKey],
      timestamp: p.timestamp,
    }))

    return {
      path: d,
      areaPath: areaD,
      points: pts,
      viewBox: `0 0 ${width} ${height}`,
      padLeft,
      padTop,
      chartW,
      chartH,
      minVal,
      maxVal,
      toY,
    }
  }, [data, valueKey, yMax])

  const handleMove = useCallback(
    (e) => {
      if (data.length === 0) return
      const rect = e.currentTarget.getBoundingClientRect()
      const x = e.clientX - rect.left
      const svgX = (x / rect.width) * 800
      let closest = 0
      let minDist = Infinity
      for (let i = 0; i < points.length; i++) {
        const dist = Math.abs(points[i].x - svgX)
        if (dist < minDist) {
          minDist = dist
          closest = i
        }
      }
      setHoverIdx(closest)
    },
    [data.length, points]
  )

  if (data.length === 0) {
    return (
      <div className="flex h-48 items-center justify-center text-sm text-surface-500">
        No data available
      </div>
    )
  }

  const yTicks = 4
  const yTickVals = Array.from({ length: yTicks + 1 }, (_, i) => (maxVal / yTicks) * i)

  return (
    <div className="relative">
      <svg
        viewBox={viewBox}
        className="w-full"
        onMouseMove={handleMove}
        onMouseLeave={() => setHoverIdx(null)}
      >
        {yTickVals.map((v, i) => (
          <line
            key={i}
            x1={padLeft}
            y1={toY(v)}
            x2={padLeft + chartW}
            y2={toY(v)}
            stroke="currentColor"
            strokeOpacity={0.1}
            strokeWidth={1}
          />
        ))}

        <path d={areaPath} fill={fillColor} fillOpacity={0.15} />
        <path d={path} fill="none" stroke={color} strokeWidth={2} strokeLinejoin="round" />

        {yTickVals.map((v, i) => (
          <text
            key={i}
            x={padLeft - 8}
            y={toY(v) + 4}
            textAnchor="end"
            fontSize={10}
            fill="currentColor"
            fillOpacity={0.5}
          >
            {formatValue ? formatValue(v) : `${v.toFixed(0)}${unit}`}
          </text>
        ))}

        {(() => {
          const indices = [0, Math.floor(data.length / 2), data.length - 1]
          return indices.map((i) => (
            <text
              key={i}
              x={points[i].x}
              y={padTop + chartH + 18}
              textAnchor={i === 0 ? 'start' : i === data.length - 1 ? 'end' : 'middle'}
              fontSize={10}
              fill="currentColor"
              fillOpacity={0.5}
            >
              {formatTime(data[i].timestamp)}
            </text>
          ))
        })()}

        {hoverIdx !== null && points[hoverIdx] && (
          <g>
            <line
              x1={points[hoverIdx].x}
              y1={padTop}
              x2={points[hoverIdx].x}
              y2={padTop + chartH}
              stroke="currentColor"
              strokeOpacity={0.3}
              strokeDasharray="4 4"
              strokeWidth={1}
            />
            <circle cx={points[hoverIdx].x} cy={points[hoverIdx].y} r={4} fill={color} />
            <g transform={`translate(${points[hoverIdx].x + 8}, ${points[hoverIdx].y - 8})`}>
              <rect x={0} y={-20} width={140} height={36} rx={4} fill="rgba(15, 23, 42, 0.9)" stroke="rgba(255,255,255,0.1)" />
              <text x={8} y={-4} fontSize={10} fill="rgba(255,255,255,0.7)">
                {formatTime(points[hoverIdx].timestamp)}
              </text>
              <text x={8} y={10} fontSize={11} fill="white" fontWeight={600}>
                {label}: {formatValue ? formatValue(points[hoverIdx].value) : `${points[hoverIdx].value.toFixed(1)}${unit}`}
              </text>
            </g>
          </g>
        )}
      </svg>
    </div>
  )
}

const HOUR_OPTIONS = [
  { label: '1h', value: 1 },
  { label: '6h', value: 6 },
  { label: '12h', value: 12 },
  { label: '24h', value: 24 },
  { label: '48h', value: 48 },
  { label: '7d', value: 168 },
]

export default function StatsPage() {
  const { deviceUrl } = useDevice()
  const [points, setPoints] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)
  const [hours, setHours] = useState(24)

  useEffect(() => {
    if (!deviceUrl) return
    let cancelled = false

    async function load() {
      setLoading(true)
      setError(null)
      try {
        const data = await getSystemStats(deviceUrl, hours)
        if (!cancelled) {
          setPoints(data.points || [])
        }
      } catch (err) {
        if (!cancelled) setError(err.message)
      } finally {
        if (!cancelled) setLoading(false)
      }
    }

    load()
    const interval = setInterval(load, 5000)
    return () => {
      cancelled = true
      clearInterval(interval)
    }
  }, [deviceUrl, hours])

  const latest = points[points.length - 1]

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-1.5 rounded-lg bg-surface-800/60 p-1">
          {HOUR_OPTIONS.map((opt) => (
            <button
              key={opt.value}
              onClick={() => setHours(opt.value)}
              className={`rounded-md px-3 py-1 text-xs font-medium transition-colors ${
                hours === opt.value
                  ? 'bg-brand-500/20 text-brand-300'
                  : 'text-surface-400 hover:text-surface-200'
              }`}
            >
              {opt.label}
            </button>
          ))}
        </div>

        <div className="flex items-center gap-4">
          {loading && points.length > 0 && (
            <LoadingSpinner size="sm" />
          )}
          {latest && (
            <div className="flex items-center gap-4 text-sm">
              <span className="text-surface-400">
                CPU: <span className="font-semibold text-surface-200">{latest.cpu_percent.toFixed(1)}%</span>
              </span>
              <span className="text-surface-400">
                RAM: <span className="font-semibold text-surface-200">{formatBytes(latest.rss_bytes)}</span>
                {' / '}
                <span className="text-surface-500">{formatBytes(latest.total_mem_bytes)}</span>
              </span>
            </div>
          )}
        </div>
      </div>

      {error && (
        <div className="rounded-lg border border-red-500/20 bg-red-500/10 px-4 py-3 text-sm text-red-400">
          {error}
        </div>
      )}

      {loading && points.length === 0 && (
        <div className="flex h-48 items-center justify-center">
          <LoadingSpinner />
        </div>
      )}

      <Card>
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-semibold text-surface-200">CPU Usage</h3>
          {latest && (
            <span className="text-xs text-surface-500">{latest.cpu_percent.toFixed(1)}%</span>
          )}
        </div>
        <TimeSeriesChart
          data={points}
          valueKey="cpu_percent"
          color="#f59e0b"
          fillColor="#f59e0b"
          label="CPU"
          unit="%"
          yMax={100}
        />
      </Card>

      <Card>
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-semibold text-surface-200">Memory (RSS)</h3>
          {latest && (
            <span className="text-xs text-surface-500">{formatBytes(latest.rss_bytes)}</span>
          )}
        </div>
        <TimeSeriesChart
          data={points}
          valueKey="rss_bytes"
          color="#10b981"
          fillColor="#10b981"
          label="RAM"
          formatValue={formatBytes}
        />
      </Card>

      <Card>
        <div className="mb-3 flex items-center justify-between">
          <h3 className="text-sm font-semibold text-surface-200">Memory Usage %</h3>
          {latest && latest.total_mem_bytes > 0 && (
            <span className="text-xs text-surface-500">
              {((latest.rss_bytes / latest.total_mem_bytes) * 100).toFixed(1)}%
            </span>
          )}
        </div>
        <TimeSeriesChart
          data={points}
          valueKey="rss_bytes"
          color="#3b82f6"
          fillColor="#3b82f6"
          label="RAM %"
          unit="%"
          yMax={latest?.total_mem_bytes || undefined}
          formatValue={(v) =>
            latest?.total_mem_bytes
              ? `${((v / latest.total_mem_bytes) * 100).toFixed(1)}%`
              : formatBytes(v)
          }
        />
      </Card>
    </div>
  )
}
