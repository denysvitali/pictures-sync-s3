package googlephotos

import (
	"sync"
	"time"
)

// syncMetrics accumulates per-run upload and API call statistics. It is
// embedded in SyncManager and its own mu protects concurrent updates.
type syncMetrics struct {
	mu            sync.Mutex
	uploadCount   int
	uploadLatency time.Duration
	apiCount      int
	apiLatency    time.Duration
	bytesUploaded int64
	startedAt     time.Time
}

func (sm *SyncManager) recordUploadLatency(latency time.Duration, bytes int64) {
	sm.metrics.mu.Lock()
	sm.metrics.uploadCount++
	sm.metrics.uploadLatency += latency
	sm.metrics.bytesUploaded += bytes
	sm.metrics.mu.Unlock()
	sm.updateBackendMetrics(0, 0)
}

func (sm *SyncManager) recordAPILatency(latency time.Duration) {
	sm.metrics.mu.Lock()
	sm.metrics.apiCount++
	sm.metrics.apiLatency += latency
	sm.metrics.mu.Unlock()
	sm.updateBackendMetrics(0, 0)
}

func (sm *SyncManager) updateBackendMetrics(queueDepth, inFlightDelta int) {
	sm.metrics.mu.Lock()
	defer sm.metrics.mu.Unlock()
	sm.mu.Lock()
	defer sm.mu.Unlock()
	if sm.progress == nil {
		return
	}
	if queueDepth >= 0 {
		sm.progress.BackendMetrics.QueueDepth = queueDepth
	}
	sm.progress.BackendMetrics.InFlightJobs += inFlightDelta
	if sm.progress.BackendMetrics.InFlightJobs < 0 {
		sm.progress.BackendMetrics.InFlightJobs = 0
	}
	if sm.metrics.uploadCount > 0 {
		sm.progress.BackendMetrics.AverageUploadLatency = (sm.metrics.uploadLatency / time.Duration(sm.metrics.uploadCount)).Round(time.Millisecond).String()
	}
	if sm.metrics.apiCount > 0 {
		sm.progress.BackendMetrics.AverageAPILatency = (sm.metrics.apiLatency / time.Duration(sm.metrics.apiCount)).Round(time.Millisecond).String()
	}
	if elapsed := time.Since(sm.metrics.startedAt).Seconds(); elapsed > 0 {
		sm.progress.BackendMetrics.UploadBytesPerSec = float64(sm.metrics.bytesUploaded) / elapsed
	}
	if sm.progress.BackendMetrics.UploadBytesPerSec > 0 && sm.progress.TotalBytes > sm.progress.ProcessedBytes {
		eta := time.Duration(float64(sm.progress.TotalBytes-sm.progress.ProcessedBytes)/sm.progress.BackendMetrics.UploadBytesPerSec) * time.Second
		for i := range sm.progress.StageTimeline {
			if sm.progress.StageTimeline[i].Status == "active" {
				sm.progress.StageTimeline[i].ETA = eta.Round(time.Second).String()
				sm.progress.StageTimeline[i].BytesPerSec = sm.progress.BackendMetrics.UploadBytesPerSec
			}
		}
	}
	sm.progress.UpdatedAt = timePtr(time.Now())
}
