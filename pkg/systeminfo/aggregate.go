package systeminfo

// AggregatePoint is a downsampled stats point. It mirrors StatsRecord but
// fields are computed via aggregation (averages for gauges, last-value for
// totals/snapshots) over a fixed-width bucket.
type AggregatePoint struct {
	Timestamp      int64
	CPUPercent     float32
	RSSBytes       uint64
	TotalMemBytes  uint64
	Load1          float32
	Load5          float32
	Load15         float32
	SwapUsedBytes  uint64
	SwapTotalBytes uint64
	DiskUsedBytes  uint64
	DiskTotalBytes uint64
	NetRxBytesPS   uint64
	NetTxBytesPS   uint64
}

// AutoResolution picks a bucket size in seconds such that at most maxPoints
// buckets cover the [since, until] range. If the raw 10s cadence already fits
// (or is finer than the picked candidate), 10s is returned.
//
// Candidates are common round numbers so the UI x-axis stays predictable.
func AutoResolution(sinceUnix, untilUnix int64, maxPoints int) int {
	if maxPoints <= 0 {
		maxPoints = 500
	}
	span := untilUnix - sinceUnix
	if span <= 0 {
		return 10
	}

	// Common bucket sizes in ascending order.
	candidates := []int{10, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 14400, 21600, 43200, 86400}
	for _, b := range candidates {
		if int(span)/b <= maxPoints {
			return b
		}
	}
	// Fall back to the coarsest candidate.
	return candidates[len(candidates)-1]
}

// Downsample groups records into fixed-width time buckets of resolutionSec
// seconds (bucket key = floor(ts/res)*res) and aggregates each bucket:
//   - gauges (CPU%, mem%/RSS, load, net throughput) use the arithmetic mean.
//   - snapshots/totals (mem total, swap total/used, disk used/total) use the
//     last value within the bucket.
//
// Buckets are emitted in ascending time order. If resolutionSec <= 10 (raw
// cadence), records are returned 1:1 as AggregatePoints with no aggregation.
func Downsample(records []StatsRecord, resolutionSec int) []AggregatePoint {
	if len(records) == 0 {
		return nil
	}
	if resolutionSec <= 10 {
		out := make([]AggregatePoint, len(records))
		for i, r := range records {
			out[i] = AggregatePoint{
				Timestamp:      r.Timestamp,
				CPUPercent:     r.CPUPercent,
				RSSBytes:       r.RSSBytes,
				TotalMemBytes:  r.TotalMemBytes,
				Load1:          r.Load1,
				Load5:          r.Load5,
				Load15:         r.Load15,
				SwapUsedBytes:  r.SwapUsedBytes,
				SwapTotalBytes: r.SwapTotalBytes,
				DiskUsedBytes:  r.DiskUsedBytes,
				DiskTotalBytes: r.DiskTotalBytes,
				NetRxBytesPS:   r.NetRxBytesPerSec,
				NetTxBytesPS:   r.NetTxBytesPerSec,
			}
		}
		return out
	}

	res := int64(resolutionSec)

	type acc struct {
		bucketStart int64
		count       int

		sumCPU                            float64
		sumLoad1, sumLoad5, sumLoad15     float64
		sumRSS                            float64
		sumNetRx, sumNetTx                float64
		lastTotalMem                      uint64
		lastSwapUsed, lastSwapTotal       uint64
		lastDiskUsed, lastDiskTotal       uint64
	}

	var (
		out     []AggregatePoint
		current *acc
	)

	flush := func() {
		if current == nil || current.count == 0 {
			return
		}
		n := float64(current.count)
		out = append(out, AggregatePoint{
			Timestamp:      current.bucketStart,
			CPUPercent:     float32(current.sumCPU / n),
			RSSBytes:       uint64(current.sumRSS / n),
			TotalMemBytes:  current.lastTotalMem,
			Load1:          float32(current.sumLoad1 / n),
			Load5:          float32(current.sumLoad5 / n),
			Load15:         float32(current.sumLoad15 / n),
			SwapUsedBytes:  current.lastSwapUsed,
			SwapTotalBytes: current.lastSwapTotal,
			DiskUsedBytes:  current.lastDiskUsed,
			DiskTotalBytes: current.lastDiskTotal,
			NetRxBytesPS:   uint64(current.sumNetRx / n),
			NetTxBytesPS:   uint64(current.sumNetTx / n),
		})
	}

	for _, r := range records {
		bucket := (r.Timestamp / res) * res
		if current == nil || bucket != current.bucketStart {
			flush()
			current = &acc{bucketStart: bucket}
		}
		current.count++
		current.sumCPU += float64(r.CPUPercent)
		current.sumLoad1 += float64(r.Load1)
		current.sumLoad5 += float64(r.Load5)
		current.sumLoad15 += float64(r.Load15)
		current.sumRSS += float64(r.RSSBytes)
		current.sumNetRx += float64(r.NetRxBytesPerSec)
		current.sumNetTx += float64(r.NetTxBytesPerSec)
		current.lastTotalMem = r.TotalMemBytes
		current.lastSwapUsed = r.SwapUsedBytes
		current.lastSwapTotal = r.SwapTotalBytes
		current.lastDiskUsed = r.DiskUsedBytes
		current.lastDiskTotal = r.DiskTotalBytes
	}
	flush()

	return out
}
