import { useState, useEffect, useMemo, useCallback, useRef } from 'react'
import { useDevice } from '../DeviceContext.jsx'
import { getSystemStats } from '../api.js'
import { Card } from '../components/Card.jsx'
import { LoadingSpinner } from '../components/LoadingSpinner.jsx'
import { TimeSeriesChart } from '../components/TimeSeriesChart'
import { RangePicker, presetToRange, type RangeValue } from '../components/RangePicker'
import { ResolutionPicker, autoResolution, type ResolutionValue } from '../components/ResolutionPicker'
import { MetricCard } from '../components/MetricCard'
import { StatsSummary } from '../components/StatsSummary'
import type { StatPoint, StatsResponse } from '../types/stats'

function formatBytes(bytes: number | undefined | null): string {
  if (!Number.isFinite(bytes) || (bytes as number) <= 0) return '0 B'
  const k = 1024
  const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
  const b = bytes as number
  const i = Math.min(sizes.length - 1, Math.floor(Math.log(b) / Math.log(k)))
  return `${(b / Math.pow(k, i)).toFixed(1)} ${sizes[i]}`
}

function formatBytesPerSec(bytes: number | undefined | null): string {
  return `${formatBytes(bytes)}/s`
}

function formatPercent(v: number | undefined | null): string {
  if (!Number.isFinite(v)) return '—'
  return `${(v as number).toFixed(1)}%`
}

function formatLoad(v: number | undefined | null): string {
  if (!Number.isFinite(v)) return '—'
  return (v as number).toFixed(2)
}

const COLORS = {
  cpu: '#f59e0b',
  ram: '#10b981',
  ramPct: '#3b82f6',
  load1: '#8b5cf6',
  load5: '#a78bfa',
  load15: '#c4b5fd',
  disk: '#ec4899',
  swap: '#f43f5e',
  netRx: '#06b6d4',
  netTx: '#22d3ee',
} as const

const defaultRange: RangeValue = (() => {
  const r = presetToRange('24h')
  return { preset: '24h', from: r?.from, to: r?.to }
})()

interface Meta {
  since?: number
  until?: number
  resolution?: number
  count?: number
}

export default function StatsPage() {
  const { deviceUrl } = useDevice() as { deviceUrl: string }
  const [range, setRange] = useState<RangeValue>(defaultRange)
  const [resolution, setResolution] = useState<ResolutionValue>('auto')
  const [points, setPoints] = useState<StatPoint[]>([])
  const [meta, setMeta] = useState<Meta | null>(null)
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [paused, setPaused] = useState(false)
  const isLiveRef = useRef(true)

  const isLive = range.preset !== 'custom'
  isLiveRef.current = isLive

  const rangeSeconds = useMemo(() => {
    if (!range.from || !range.to) return 86400
    return Math.max(60, range.to - range.from)
  }, [range])

  const fetchStats = useCallback(
    async ({ silent = false }: { silent?: boolean } = {}) => {
      if (!deviceUrl) return
      if (!silent) setLoading(true)
      try {
        let from = range.from
        let to = range.to
        if (isLiveRef.current) {
          const r = presetToRange(range.preset)
          if (r) {
            from = r.from
            to = r.to
          }
        }
        const data = (await getSystemStats(deviceUrl, {
          since: from,
          until: to,
          resolution: resolution === 'auto' ? undefined : resolution,
        })) as StatsResponse
        setPoints(Array.isArray(data?.points) ? data.points : [])
        setMeta({
          since: data?.since,
          until: data?.until,
          resolution: data?.resolution,
          count: data?.count,
        })
        setError(null)
      } catch (err) {
        setError((err as Error).message)
      } finally {
        setLoading(false)
      }
    },
    [deviceUrl, range.from, range.to, range.preset, resolution]
  )

  useEffect(() => {
    if (!deviceUrl) return
    let cancelled = false
    let timer: ReturnType<typeof setInterval> | null = null

    const tick = async () => {
      if (cancelled) return
      await fetchStats({ silent: points.length > 0 })
    }

    fetchStats({ silent: false })

    if (isLive && !paused) {
      timer = setInterval(() => {
        if (!document.hidden) tick()
      }, 5000)
    }

    return () => {
      cancelled = true
      if (timer) clearInterval(timer)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [deviceUrl, range.preset, range.from, range.to, resolution, paused])

  const latest = points[points.length - 1]
  const memPctData = useMemo(
    () =>
      points.map((p) => ({
        timestamp: p.timestamp,
        mem_pct: p.total_mem_bytes > 0 ? (p.rss_bytes / p.total_mem_bytes) * 100 : 0,
      })),
    [points]
  )
  const diskPctData = useMemo(
    () =>
      points.map((p) => ({
        timestamp: p.timestamp,
        disk_pct:
          (p.disk_total_bytes ?? 0) > 0
            ? ((p.disk_used_bytes ?? 0) / (p.disk_total_bytes ?? 1)) * 100
            : 0,
      })),
    [points]
  )
  const swapPctData = useMemo(
    () =>
      points.map((p) => ({
        timestamp: p.timestamp,
        swap_pct:
          (p.swap_total_bytes ?? 0) > 0
            ? ((p.swap_used_bytes ?? 0) / (p.swap_total_bytes ?? 1)) * 100
            : 0,
      })),
    [points]
  )

  const hasSwap = Boolean(latest && (latest.swap_total_bytes ?? 0) > 0)
  const hasNet = points.some(
    (p) => (p.net_rx_bytes_per_sec ?? 0) > 0 || (p.net_tx_bytes_per_sec ?? 0) > 0
  )
  const hasLoad = points.some((p) => Number.isFinite(p.load1) && (p.load1 ?? 0) > 0)

  const rangeLabel = useMemo(() => {
    if (range.preset !== 'custom') return range.preset
    if (!range.from || !range.to) return 'custom'
    const from = new Date(range.from * 1000)
    const to = new Date(range.to * 1000)
    const opts: Intl.DateTimeFormatOptions = { month: 'short', day: '2-digit', hour: '2-digit', minute: '2-digit' }
    return `${from.toLocaleString([], opts)} → ${to.toLocaleString([], opts)}`
  }, [range])

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-wrap items-start gap-3">
          <RangePicker value={range} onChange={setRange} />
          <ResolutionPicker
            value={resolution}
            rangeSeconds={rangeSeconds}
            onChange={setResolution}
          />
        </div>

        <div className="flex items-center gap-3">
          {loading && <LoadingSpinner size="sm" />}
          {isLive && (
            <button
              type="button"
              onClick={() => setPaused((p) => !p)}
              className={`rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
                paused
                  ? 'bg-amber-500/15 text-amber-300 border border-amber-500/30'
                  : 'bg-emerald-500/15 text-emerald-300 border border-emerald-500/30'
              }`}
              title={paused ? 'Resume live updates' : 'Pause live updates'}
            >
              {paused ? '⏸ Paused' : '● Live'}
            </button>
          )}
          <button
            type="button"
            onClick={() => fetchStats({ silent: false })}
            className="rounded-md border border-surface-700/60 bg-surface-800/60 px-2.5 py-1 text-xs font-medium text-surface-300 hover:text-surface-100"
            title="Refresh now"
          >
            ⟳ Refresh
          </button>
        </div>
      </div>

      {meta && (
        <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-surface-500">
          <span>Range: <span className="text-surface-300">{rangeLabel}</span></span>
          <span>Resolution: <span className="text-surface-300">{meta.resolution}s</span></span>
          <span>Points: <span className="text-surface-300">{meta.count}</span></span>
        </div>
      )}

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

      {points.length > 0 && (
        <>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            <MetricCard
              title="CPU"
              value={latest?.cpu_percent}
              formatValue={formatPercent}
              data={points}
              valueKey="cpu_percent"
              color={COLORS.cpu}
              hint={rangeLabel}
            />
            <MetricCard
              title="Memory"
              value={
                latest && latest.total_mem_bytes > 0
                  ? (latest.rss_bytes / latest.total_mem_bytes) * 100
                  : 0
              }
              formatValue={formatPercent}
              data={memPctData}
              valueKey="mem_pct"
              color={COLORS.ramPct}
              hint={latest ? formatBytes(latest.rss_bytes) : ''}
            />
            <MetricCard
              title="Disk"
              value={
                latest && (latest.disk_total_bytes ?? 0) > 0
                  ? ((latest.disk_used_bytes ?? 0) / (latest.disk_total_bytes ?? 1)) * 100
                  : 0
              }
              formatValue={formatPercent}
              data={diskPctData}
              valueKey="disk_pct"
              color={COLORS.disk}
              hint={
                latest
                  ? `${formatBytes(latest.disk_used_bytes)} / ${formatBytes(latest.disk_total_bytes)}`
                  : ''
              }
            />
            <MetricCard
              title="Load (1m)"
              value={latest?.load1}
              formatValue={formatLoad}
              data={points}
              valueKey="load1"
              color={COLORS.load1}
              hint={
                latest && Number.isFinite(latest.load5)
                  ? `5m ${formatLoad(latest.load5)} · 15m ${formatLoad(latest.load15)}`
                  : ''
              }
            />
          </div>

          <Card>
            <div className="mb-3 flex items-center justify-between">
              <h3 className="text-sm font-semibold text-surface-200">CPU Usage</h3>
              {latest && (
                <span className="text-xs text-surface-500">{formatPercent(latest.cpu_percent)}</span>
              )}
            </div>
            <TimeSeriesChart
              data={points}
              series={[
                {
                  key: 'cpu_percent',
                  label: 'CPU',
                  color: COLORS.cpu,
                  unit: '%',
                  yMax: 100,
                  formatValue: formatPercent,
                },
              ]}
              yMin={0}
            />
            <StatsSummary
              className="mt-3"
              data={points}
              valueKey="cpu_percent"
              formatValue={formatPercent}
            />
          </Card>

          <Card>
            <div className="mb-3 flex items-center justify-between">
              <h3 className="text-sm font-semibold text-surface-200">Memory</h3>
              {latest && (
                <span className="text-xs text-surface-500">
                  {formatBytes(latest.rss_bytes)} / {formatBytes(latest.total_mem_bytes)}
                </span>
              )}
            </div>
            <TimeSeriesChart
              data={points}
              series={[
                {
                  key: 'rss_bytes',
                  label: 'RSS',
                  color: COLORS.ram,
                  formatValue: formatBytes,
                },
              ]}
            />
            <StatsSummary
              className="mt-3"
              data={points}
              valueKey="rss_bytes"
              formatValue={formatBytes}
            />
          </Card>

          {hasLoad && (
            <Card>
              <div className="mb-3 flex items-center justify-between">
                <h3 className="text-sm font-semibold text-surface-200">Load Average</h3>
                {latest && (
                  <span className="text-xs text-surface-500">
                    {formatLoad(latest.load1)} · {formatLoad(latest.load5)} · {formatLoad(latest.load15)}
                  </span>
                )}
              </div>
              <TimeSeriesChart
                data={points}
                series={[
                  { key: 'load1', label: '1m', color: COLORS.load1, formatValue: formatLoad },
                  { key: 'load5', label: '5m', color: COLORS.load5, formatValue: formatLoad },
                  { key: 'load15', label: '15m', color: COLORS.load15, formatValue: formatLoad },
                ]}
              />
            </Card>
          )}

          <Card>
            <div className="mb-3 flex items-center justify-between">
              <h3 className="text-sm font-semibold text-surface-200">Disk Usage</h3>
              {latest && (latest.disk_total_bytes ?? 0) > 0 && (
                <span className="text-xs text-surface-500">
                  {formatPercent(((latest.disk_used_bytes ?? 0) / (latest.disk_total_bytes ?? 1)) * 100)}
                </span>
              )}
            </div>
            <TimeSeriesChart
              data={diskPctData}
              series={[
                {
                  key: 'disk_pct',
                  label: 'Disk',
                  color: COLORS.disk,
                  unit: '%',
                  yMax: 100,
                  formatValue: formatPercent,
                },
              ]}
            />
          </Card>

          {hasSwap && (
            <Card>
              <div className="mb-3 flex items-center justify-between">
                <h3 className="text-sm font-semibold text-surface-200">Swap</h3>
                {latest && (
                  <span className="text-xs text-surface-500">
                    {formatBytes(latest.swap_used_bytes)} / {formatBytes(latest.swap_total_bytes)}
                  </span>
                )}
              </div>
              <TimeSeriesChart
                data={swapPctData}
                series={[
                  {
                    key: 'swap_pct',
                    label: 'Swap',
                    color: COLORS.swap,
                    unit: '%',
                    yMax: 100,
                    formatValue: formatPercent,
                  },
                ]}
              />
            </Card>
          )}

          {hasNet && (
            <Card>
              <div className="mb-3 flex items-center justify-between">
                <h3 className="text-sm font-semibold text-surface-200">Network</h3>
                {latest && (
                  <span className="text-xs text-surface-500">
                    ↓ {formatBytesPerSec(latest.net_rx_bytes_per_sec ?? 0)} · ↑{' '}
                    {formatBytesPerSec(latest.net_tx_bytes_per_sec ?? 0)}
                  </span>
                )}
              </div>
              <TimeSeriesChart
                data={points}
                series={[
                  {
                    key: 'net_rx_bytes_per_sec',
                    label: 'RX',
                    color: COLORS.netRx,
                    formatValue: formatBytesPerSec,
                  },
                  {
                    key: 'net_tx_bytes_per_sec',
                    label: 'TX',
                    color: COLORS.netTx,
                    formatValue: formatBytesPerSec,
                  },
                ]}
              />
            </Card>
          )}
        </>
      )}
    </div>
  )
}
