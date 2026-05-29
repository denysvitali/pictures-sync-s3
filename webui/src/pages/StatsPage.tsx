import { useState, useEffect, useMemo, useCallback, useRef, type ReactNode } from 'react'
import { useDevice } from '../DeviceContext.jsx'
import { getSystemStats } from '../api.js'
import { Card } from '../components/Card.jsx'
import { LoadingSpinner } from '../components/LoadingSpinner.jsx'
import { Skeleton } from '../components/Skeleton.jsx'
import { EmptyState } from '../components/EmptyState.jsx'
import { TimeSeriesChart } from '../components/TimeSeriesChart'
import { RangePicker, presetToRange, type RangeValue } from '../components/RangePicker'
import { ResolutionPicker, type ResolutionValue } from '../components/ResolutionPicker'
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
  ramPct: '#22d3ee',
  load1: '#8b5cf6',
  load5: '#a78bfa',
  load15: '#c4b5fd',
  disk: '#ec4899',
  swap: '#f43f5e',
  netRx: '#06b6d4',
  netTx: '#67e8f9',
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

function ChartCard({
  title,
  color,
  badge,
  children,
}: {
  title: string
  color: string
  badge?: ReactNode
  children: ReactNode
}) {
  return (
    <Card glow>
      <div
        className="mb-3 -ml-3 flex items-center justify-between gap-3 border-l-2 pl-3"
        style={{ borderColor: color }}
      >
        <h3 className="text-sm font-semibold tracking-tight text-surface-200">{title}</h3>
        {badge && <span className="text-xs tabular-nums text-surface-400">{badge}</span>}
      </div>
      {children}
    </Card>
  )
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
    const opts: Intl.DateTimeFormatOptions = {
      month: 'short',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
    }
    return `${from.toLocaleString([], opts)} → ${to.toLocaleString([], opts)}`
  }, [range])

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="flex flex-wrap items-start gap-3">
          <RangePicker value={range} onChange={setRange} />
          <ResolutionPicker value={resolution} rangeSeconds={rangeSeconds} onChange={setResolution} />
        </div>

        <div className="flex items-center gap-2">
          {loading && points.length > 0 && <LoadingSpinner size="sm" />}
          {isLive && (
            <button
              type="button"
              onClick={() => setPaused((p) => !p)}
              aria-pressed={!paused}
              className={`inline-flex items-center gap-1.5 rounded-md px-2.5 py-1.5 text-xs font-medium ring-1 ring-inset transition-colors focus-visible:outline-none focus-visible:ring-2 ${
                paused
                  ? 'bg-amber-500/15 text-amber-300 ring-amber-500/30 focus-visible:ring-amber-400/70'
                  : 'bg-emerald-500/15 text-emerald-300 ring-emerald-500/30 focus-visible:ring-emerald-400/70'
              }`}
              title={paused ? 'Resume live updates' : 'Pause live updates'}
            >
              {paused ? (
                <>
                  <svg viewBox="0 0 12 12" className="h-3 w-3" fill="currentColor" aria-hidden="true">
                    <rect x="2.5" y="2" width="2.5" height="8" rx="0.6" />
                    <rect x="7" y="2" width="2.5" height="8" rx="0.6" />
                  </svg>
                  Paused
                </>
              ) : (
                <>
                  <span className="relative flex h-2 w-2" aria-hidden="true">
                    <span className="absolute inline-flex h-full w-full animate-ping rounded-full bg-emerald-400 opacity-75" />
                    <span className="relative inline-flex h-2 w-2 rounded-full bg-emerald-400" />
                  </span>
                  Live
                </>
              )}
            </button>
          )}
          <button
            type="button"
            onClick={() => fetchStats({ silent: false })}
            className="inline-flex items-center gap-1.5 rounded-md border border-surface-700/60 bg-surface-800/60 px-2.5 py-1.5 text-xs font-medium text-surface-300 transition-colors hover:bg-surface-700/60 hover:text-surface-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-400/70"
            title="Refresh now"
          >
            <svg viewBox="0 0 16 16" className="h-3.5 w-3.5" fill="none" aria-hidden="true">
              <path
                d="M13.5 8a5.5 5.5 0 1 1-1.6-3.9M13.5 3.5V6H11"
                stroke="currentColor"
                strokeWidth="1.4"
                strokeLinecap="round"
                strokeLinejoin="round"
              />
            </svg>
            Refresh
          </button>
        </div>
      </div>

      {meta && (
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span className="inline-flex items-center gap-1.5 rounded-md border border-surface-700/50 bg-surface-800/50 px-2 py-1 text-surface-400">
            Range <span className="font-medium text-surface-200">{rangeLabel}</span>
          </span>
          <span className="inline-flex items-center gap-1.5 rounded-md border border-surface-700/50 bg-surface-800/50 px-2 py-1 text-surface-400">
            Resolution <span className="font-medium tabular-nums text-surface-200">{meta.resolution}s</span>
          </span>
          <span className="inline-flex items-center gap-1.5 rounded-md border border-surface-700/50 bg-surface-800/50 px-2 py-1 text-surface-400">
            Points <span className="font-medium tabular-nums text-surface-200">{meta.count}</span>
          </span>
        </div>
      )}

      {error && (
        <div
          role="alert"
          className="flex items-start gap-2 rounded-lg border border-danger/25 bg-danger/10 px-4 py-3 text-sm text-danger"
        >
          <svg viewBox="0 0 20 20" className="mt-0.5 h-4 w-4 shrink-0" fill="currentColor" aria-hidden="true">
            <path
              fillRule="evenodd"
              d="M8.485 2.495c.673-1.167 2.357-1.167 3.03 0l6.28 10.875c.673 1.167-.17 2.625-1.516 2.625H3.72c-1.347 0-2.189-1.458-1.515-2.625L8.485 2.495ZM10 6a.75.75 0 0 1 .75.75v3.5a.75.75 0 0 1-1.5 0v-3.5A.75.75 0 0 1 10 6Zm0 8a1 1 0 1 0 0-2 1 1 0 0 0 0 2Z"
              clipRule="evenodd"
            />
          </svg>
          <span>{error}</span>
        </div>
      )}

      {loading && points.length === 0 && !error && (
        <div className="space-y-4">
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-2 lg:grid-cols-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Card key={i} className="space-y-3">
                <Skeleton className="h-3 w-20" />
                <Skeleton className="h-8 w-24" />
                <Skeleton className="h-10 w-full" />
              </Card>
            ))}
          </div>
          {Array.from({ length: 2 }).map((_, i) => (
            <Card key={i} className="space-y-3">
              <Skeleton className="h-4 w-32" />
              <Skeleton className="h-56 w-full rounded-lg" />
            </Card>
          ))}
        </div>
      )}

      {!loading && points.length === 0 && !error && (
        <Card>
          <EmptyState
            icon="chart"
            title="No metrics yet"
            description="The device hasn't reported any system statistics for this time range. Try a wider range or refresh."
            action={{ label: 'Refresh', icon: 'arrow-path', onClick: () => fetchStats({ silent: false }) }}
          >
            {null}
          </EmptyState>
        </Card>
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

          <ChartCard
            title="CPU Usage"
            color={COLORS.cpu}
            badge={latest ? formatPercent(latest.cpu_percent) : undefined}
          >
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
              accent={COLORS.cpu}
            />
          </ChartCard>

          <ChartCard
            title="Memory"
            color={COLORS.ram}
            badge={
              latest
                ? `${formatBytes(latest.rss_bytes)} / ${formatBytes(latest.total_mem_bytes)}`
                : undefined
            }
          >
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
              accent={COLORS.ram}
            />
          </ChartCard>

          {hasLoad && (
            <ChartCard
              title="Load Average"
              color={COLORS.load1}
              badge={
                latest
                  ? `${formatLoad(latest.load1)} · ${formatLoad(latest.load5)} · ${formatLoad(latest.load15)}`
                  : undefined
              }
            >
              <TimeSeriesChart
                data={points}
                series={[
                  { key: 'load1', label: '1m', color: COLORS.load1, formatValue: formatLoad },
                  { key: 'load5', label: '5m', color: COLORS.load5, formatValue: formatLoad },
                  { key: 'load15', label: '15m', color: COLORS.load15, formatValue: formatLoad },
                ]}
              />
            </ChartCard>
          )}

          <ChartCard
            title="Disk Usage"
            color={COLORS.disk}
            badge={
              latest && (latest.disk_total_bytes ?? 0) > 0
                ? formatPercent(((latest.disk_used_bytes ?? 0) / (latest.disk_total_bytes ?? 1)) * 100)
                : undefined
            }
          >
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
          </ChartCard>

          {hasSwap && (
            <ChartCard
              title="Swap"
              color={COLORS.swap}
              badge={
                latest
                  ? `${formatBytes(latest.swap_used_bytes)} / ${formatBytes(latest.swap_total_bytes)}`
                  : undefined
              }
            >
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
            </ChartCard>
          )}

          {hasNet && (
            <ChartCard
              title="Network"
              color={COLORS.netRx}
              badge={
                latest
                  ? `↓ ${formatBytesPerSec(latest.net_rx_bytes_per_sec ?? 0)} · ↑ ${formatBytesPerSec(latest.net_tx_bytes_per_sec ?? 0)}`
                  : undefined
              }
            >
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
            </ChartCard>
          )}
        </>
      )}
    </div>
  )
}
