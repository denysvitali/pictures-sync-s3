package systeminfo

import (
	"bufio"
	"bytes"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	defaultInterval  = 10 * time.Second
	defaultRetention = 7 * 24 * time.Hour // 7 days, matches longest UI option
)

var (
	activeCollectorMu sync.RWMutex
	activeCollector   *StatsCollector
)

func setActiveCollector(c *StatsCollector) {
	activeCollectorMu.Lock()
	activeCollector = c
	activeCollectorMu.Unlock()
}

func getActiveCollector() *StatsCollector {
	activeCollectorMu.RLock()
	c := activeCollector
	activeCollectorMu.RUnlock()
	return c
}

// StatsRecord represents a single system stats sample.
type StatsRecord struct {
	Timestamp     int64   // Unix seconds
	CPUPercent    float32 // 0-100
	RSSBytes      uint64
	TotalMemBytes uint64

	// Load averages
	Load1  float32
	Load5  float32
	Load15 float32

	// Swap usage
	SwapUsedBytes  uint64
	SwapTotalBytes uint64

	// Root filesystem usage
	DiskUsedBytes  uint64
	DiskTotalBytes uint64

	// Network throughput (delta-based, bytes/sec)
	NetRxBytesPerSec uint64
	NetTxBytesPerSec uint64
}

// StatsCollector records system CPU and memory stats at regular intervals.
type StatsCollector struct {
	mu        sync.RWMutex
	interval  time.Duration
	retention time.Duration
	stopCh    chan struct{}
	stopped   bool

	// CPU sampling state
	lastCPUTotal uint64
	lastCPUIdle  uint64
	lastCPUTime  time.Time

	// Network sampling state
	lastNetRx   uint64
	lastNetTx   uint64
	lastNetTime time.Time

	records []StatsRecord
}

// NewStatsCollector creates a new stats collector.
func NewStatsCollector() *StatsCollector {
	c := &StatsCollector{
		interval:  defaultInterval,
		retention: defaultRetention,
		stopCh:    make(chan struct{}),
	}
	setActiveCollector(c)
	return c
}

// Start begins recording stats in a background goroutine.
func (c *StatsCollector) Start() {
	go c.run()
}

// Stop stops the background collector.
func (c *StatsCollector) Stop() {
	c.mu.Lock()
	if !c.stopped {
		close(c.stopCh)
		c.stopped = true
	}
	c.mu.Unlock()
}

func (c *StatsCollector) run() {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Run compaction daily
	compactionTicker := time.NewTicker(24 * time.Hour)
	defer compactionTicker.Stop()

	// Do initial CPU reading so first sample has valid delta
	c.sampleCPU()

	for {
		select {
		case <-ticker.C:
			if err := c.collect(); err != nil {
				log.Printf("stats collector: collection failed: %v", err)
			}
		case <-compactionTicker.C:
			if err := c.Compact(); err != nil {
				log.Printf("stats collector: compaction failed: %v", err)
			}
		case <-c.stopCh:
			return
		}
	}
}

func (c *StatsCollector) collect() error {
	cpuPct := c.sampleCPU()
	rss, totalMem, swapUsed, swapTotal := c.sampleMemory()
	load1, load5, load15 := sampleLoadAvg()
	diskUsed, diskTotal := sampleDiskUsage("/")
	netRx, netTx := c.sampleNetwork()

	record := StatsRecord{
		Timestamp:        time.Now().Unix(),
		CPUPercent:       float32(cpuPct),
		RSSBytes:         rss,
		TotalMemBytes:    totalMem,
		Load1:            load1,
		Load5:            load5,
		Load15:           load15,
		SwapUsedBytes:    swapUsed,
		SwapTotalBytes:   swapTotal,
		DiskUsedBytes:    diskUsed,
		DiskTotalBytes:   diskTotal,
		NetRxBytesPerSec: netRx,
		NetTxBytesPerSec: netTx,
	}

	return c.appendRecord(record)
}

func (c *StatsCollector) appendRecord(record StatsRecord) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.records = append(c.records, record)

	// Keep only records within retention and bounded by configured resolution.
	c.truncateLocked()

	return nil
}

func (c *StatsCollector) truncateLocked() {
	if len(c.records) == 0 {
		return
	}

	if c.retention <= 0 {
		c.records = nil
		return
	}

	cutoff := time.Now().Add(-c.retention).Unix()
	oldest := 0
	for oldest < len(c.records) && c.records[oldest].Timestamp < cutoff {
		oldest++
	}
	if oldest > 0 {
		copy(c.records, c.records[oldest:])
		c.records = c.records[:len(c.records)-oldest]
	}

	maxRecords := int(c.retention.Seconds() / c.interval.Seconds())
	if maxRecords <= 0 {
		maxRecords = 1
	}

	if len(c.records) > maxRecords {
		c.records = c.records[len(c.records)-maxRecords:]
	}
}

// sampleCPU reads /proc/stat and returns CPU usage percentage since last call.
// First call returns 0 and establishes baseline.
func (c *StatsCollector) sampleCPU() float64 {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return 0
	}

	// Parse first "cpu" line
	line := string(bytes.SplitN(data, []byte{'\n'}, 2)[0])
	fields := strings.Fields(line)
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0
	}

	var total, idle uint64
	for i := 1; i < len(fields); i++ {
		v, _ := strconv.ParseUint(fields[i], 10, 64)
		total += v
		if i == 4 { // idle is the 4th field (index 4, 0-based from fields[1])
			idle = v
		}
	}

	now := time.Now()
	c.mu.Lock()
	lastTotal := c.lastCPUTotal
	lastIdle := c.lastCPUIdle
	lastTime := c.lastCPUTime
	c.lastCPUTotal = total
	c.lastCPUIdle = idle
	c.lastCPUTime = now
	c.mu.Unlock()

	if lastTime.IsZero() {
		return 0
	}

	totalDelta := total - lastTotal
	idleDelta := idle - lastIdle
	if totalDelta == 0 {
		return 0
	}

	cpuPct := 100.0 * (1.0 - float64(idleDelta)/float64(totalDelta))
	if cpuPct < 0 {
		cpuPct = 0
	}
	if cpuPct > 100 {
		cpuPct = 100
	}
	return cpuPct
}

func (c *StatsCollector) sampleMemory() (rssBytes, totalBytes, swapUsedBytes, swapTotalBytes uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0, 0, 0
	}

	var memTotal, swapTotal, swapFree uint64
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			memTotal = parseMeminfoKB(line) * 1024
		case strings.HasPrefix(line, "SwapTotal:"):
			swapTotal = parseMeminfoKB(line) * 1024
		case strings.HasPrefix(line, "SwapFree:"):
			swapFree = parseMeminfoKB(line) * 1024
		}
	}

	if swapTotal >= swapFree {
		swapUsedBytes = swapTotal - swapFree
	}
	swapTotalBytes = swapTotal

	// Read process RSS from /proc/self/status
	selfData, err := os.ReadFile("/proc/self/status")
	if err == nil {
		for _, line := range strings.Split(string(selfData), "\n") {
			if strings.HasPrefix(line, "VmRSS:") {
				rssBytes = parseMeminfoKB(line) * 1024
				break
			}
		}
	}

	return rssBytes, memTotal, swapUsedBytes, swapTotalBytes
}

// sampleLoadAvg reads /proc/loadavg and returns the 1/5/15 minute load averages.
func sampleLoadAvg() (load1, load5, load15 float32) {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return 0, 0, 0
	}
	fields := strings.Fields(string(data))
	if len(fields) < 3 {
		return 0, 0, 0
	}
	parseF := func(s string) float32 {
		v, err := strconv.ParseFloat(s, 32)
		if err != nil {
			return 0
		}
		return float32(v)
	}
	return parseF(fields[0]), parseF(fields[1]), parseF(fields[2])
}

// sampleDiskUsage returns the used/total bytes for the given mountpoint.
func sampleDiskUsage(path string) (usedBytes, totalBytes uint64) {
	var st unix.Statfs_t
	if err := unix.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bsize := uint64(st.Bsize)
	totalBytes = st.Blocks * bsize
	freeBytes := st.Bavail * bsize
	if totalBytes >= freeBytes {
		usedBytes = totalBytes - freeBytes
	}
	return usedBytes, totalBytes
}

// sampleNetwork reads /proc/net/dev, sums non-loopback rx/tx counters, and
// returns the per-second delta since the last call. First call returns 0
// while establishing the baseline.
func (c *StatsCollector) sampleNetwork() (rxPerSec, txPerSec uint64) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return 0, 0
	}

	var totalRx, totalTx uint64
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		iface := strings.TrimSpace(line[:idx])
		if iface == "" || iface == "lo" {
			continue
		}
		fields := strings.Fields(line[idx+1:])
		if len(fields) < 16 {
			continue
		}
		rx, _ := strconv.ParseUint(fields[0], 10, 64)
		tx, _ := strconv.ParseUint(fields[8], 10, 64)
		totalRx += rx
		totalTx += tx
	}

	now := time.Now()
	c.mu.Lock()
	lastRx := c.lastNetRx
	lastTx := c.lastNetTx
	lastTime := c.lastNetTime
	c.lastNetRx = totalRx
	c.lastNetTx = totalTx
	c.lastNetTime = now
	c.mu.Unlock()

	if lastTime.IsZero() {
		return 0, 0
	}
	dt := now.Sub(lastTime).Seconds()
	if dt <= 0 {
		return 0, 0
	}
	if totalRx >= lastRx {
		rxPerSec = uint64(float64(totalRx-lastRx) / dt)
	}
	if totalTx >= lastTx {
		txPerSec = uint64(float64(totalTx-lastTx) / dt)
	}
	return rxPerSec, txPerSec
}

func parseMeminfoKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	v, _ := strconv.ParseUint(fields[1], 10, 64)
	return v
}

// Compact removes records older than the retention period.
func (c *StatsCollector) Compact() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.truncateLocked()
	return nil
}

// ReadStats returns records within the given time range.
func ReadStats(since, until time.Time) ([]StatsRecord, error) {
	records, err := readAllRecords()
	if err != nil {
		return nil, err
	}

	sinceUnix := since.Unix()
	untilUnix := until.Unix()

	var result []StatsRecord
	for _, r := range records {
		if r.Timestamp >= sinceUnix && r.Timestamp <= untilUnix {
			result = append(result, r)
		}
	}
	return result, nil
}

func readAllRecords() ([]StatsRecord, error) {
	active := getActiveCollector()
	if active == nil {
		return nil, nil
	}

	active.mu.RLock()
	defer active.mu.RUnlock()
	records := make([]StatsRecord, len(active.records))
	copy(records, active.records)
	return records, nil
}

func resetForTests() {
	activeCollectorMu.Lock()
	activeCollector = nil
	activeCollectorMu.Unlock()
}

// AppendRecordForTest exposes appendRecord to other packages for testing.
// It must only be used from tests.
func (c *StatsCollector) AppendRecordForTest(r StatsRecord) error {
	return c.appendRecord(r)
}

func clearActiveRecordsForTest() {
	active := getActiveCollector()
	if active == nil {
		return
	}
	active.mu.Lock()
	active.records = nil
	active.mu.Unlock()
}
