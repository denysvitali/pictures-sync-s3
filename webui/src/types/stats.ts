export interface StatPoint {
  [key: string]: number | undefined
  timestamp: number
  cpu_percent: number
  rss_bytes: number
  total_mem_bytes: number
  load1?: number
  load5?: number
  load15?: number
  swap_used_bytes?: number
  swap_total_bytes?: number
  disk_used_bytes?: number
  disk_total_bytes?: number
  net_rx_bytes_per_sec?: number
  net_tx_bytes_per_sec?: number
}

export interface StatsResponse {
  since: number
  until: number
  interval: number
  resolution: number
  count: number
  points: StatPoint[]
}

export type ResolutionValue = 'auto' | number

export type RangeValue = {
  preset: string
  from?: number
  to?: number
}
