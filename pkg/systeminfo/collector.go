package systeminfo

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"math"
	"sync"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

const (
	statsMagic      = "PSS3"
	statsVersion    = 1
	statsRecordSize = 28
	defaultInterval = 5 * time.Second
	defaultRetention = 7 * 24 * time.Hour // 7 days
)

var statsFilePath = defaultStatsFilePath()

func defaultStatsFilePath() string {
	baseDir := "/perm"
	if _, err := os.Stat(baseDir); os.IsNotExist(err) {
		baseDir = os.TempDir()
	}
	if envDir := os.Getenv("PERM_DIR"); envDir != "" {
		baseDir = envDir
	}
	return filepath.Join(baseDir, "pictures-sync", "system-stats.bin")
}

// StatsRecord represents a single system stats sample.
// Stored in little-endian binary format: 28 bytes per record.
type StatsRecord struct {
	Timestamp     int64   // Unix seconds
	CPUPercent    float32 // 0-100
	RSSBytes      uint64
	TotalMemBytes uint64
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
}

// NewStatsCollector creates a new stats collector.
func NewStatsCollector() *StatsCollector {
	return &StatsCollector{
		interval:  defaultInterval,
		retention: defaultRetention,
		stopCh:    make(chan struct{}),
	}
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
	if err := utils.EnsureDir(filepath.Dir(statsFilePath), 0750); err != nil {
		log.Printf("stats collector: failed to create directory: %v", err)
		return
	}

	// Ensure file has valid header
	if err := c.ensureHeader(); err != nil {
		log.Printf("stats collector: failed to initialize stats file: %v", err)
		return
	}

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

func (c *StatsCollector) ensureHeader() error {
	info, err := os.Stat(statsFilePath)
	if err == nil && info.Size() >= 8 {
		// File exists and has header
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	// Write header
	header := make([]byte, 8)
	copy(header[0:4], statsMagic)
	header[4] = statsVersion
	binary.LittleEndian.PutUint16(header[5:7], statsRecordSize)
	header[7] = 0 // reserved

	return os.WriteFile(statsFilePath, header, 0640)
}

func (c *StatsCollector) collect() error {
	cpuPct := c.sampleCPU()
	rss, totalMem := c.sampleMemory()

	record := StatsRecord{
		Timestamp:     time.Now().Unix(),
		CPUPercent:    float32(cpuPct),
		RSSBytes:      rss,
		TotalMemBytes: totalMem,
	}

	return c.appendRecord(record)
}

func (c *StatsCollector) appendRecord(record StatsRecord) error {
	// Use O_APPEND for atomic small writes on Linux
	f, err := os.OpenFile(statsFilePath, os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0640)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, statsRecordSize)
	binary.LittleEndian.PutUint64(buf[0:8], uint64(record.Timestamp))
	binary.LittleEndian.PutUint32(buf[8:12], math.Float32bits(record.CPUPercent))
	binary.LittleEndian.PutUint64(buf[12:20], record.RSSBytes)
	binary.LittleEndian.PutUint64(buf[20:28], record.TotalMemBytes)

	_, err = f.Write(buf)
	return err
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

	if lastTotal == 0 || lastTime.IsZero() {
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

func (c *StatsCollector) sampleMemory() (rssBytes, totalBytes uint64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}

	var memTotal uint64
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			memTotal = parseMeminfoKB(line) * 1024
		}
	}

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

	return rssBytes, memTotal
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
	cutoff := time.Now().Add(-c.retention).Unix()

	records, err := readAllRecords()
	if err != nil {
		return err
	}

	// Filter old records
	var kept []StatsRecord
	for _, r := range records {
		if r.Timestamp >= cutoff {
			kept = append(kept, r)
		}
	}

	// If nothing changed, skip rewrite
	if len(kept) == len(records) {
		return nil
	}

	// Rewrite file atomically
	buf := make([]byte, 8+len(kept)*statsRecordSize)
	copy(buf[0:4], statsMagic)
	buf[4] = statsVersion
	binary.LittleEndian.PutUint16(buf[5:7], statsRecordSize)
	buf[7] = 0

	for i, r := range kept {
		off := 8 + i*statsRecordSize
		binary.LittleEndian.PutUint64(buf[off:off+8], uint64(r.Timestamp))
		binary.LittleEndian.PutUint32(buf[off+8:off+12], math.Float32bits(r.CPUPercent))
		binary.LittleEndian.PutUint64(buf[off+12:off+20], r.RSSBytes)
		binary.LittleEndian.PutUint64(buf[off+20:off+28], r.TotalMemBytes)
	}

	return utils.AtomicWrite(statsFilePath, buf, 0640)
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
	data, err := os.ReadFile(statsFilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	if len(data) < 8 {
		return nil, nil
	}

	// Validate header
	if string(data[0:4]) != statsMagic {
		return nil, fmt.Errorf("invalid stats file magic")
	}
	if data[4] != statsVersion {
		return nil, fmt.Errorf("unsupported stats file version: %d", data[4])
	}

	recordSize := int(binary.LittleEndian.Uint16(data[5:7]))
	if recordSize != statsRecordSize {
		return nil, fmt.Errorf("unexpected record size: %d", recordSize)
	}

	data = data[8:]
	numRecords := len(data) / statsRecordSize

	records := make([]StatsRecord, 0, numRecords)
	for i := 0; i < numRecords; i++ {
		off := i * statsRecordSize
		if off+statsRecordSize > len(data) {
			break // partial record at end, ignore
		}
		records = append(records, StatsRecord{
			Timestamp:     int64(binary.LittleEndian.Uint64(data[off : off+8])),
			CPUPercent:    math.Float32frombits(binary.LittleEndian.Uint32(data[off+8 : off+12])),
			RSSBytes:      binary.LittleEndian.Uint64(data[off+12 : off+20]),
			TotalMemBytes: binary.LittleEndian.Uint64(data[off+20 : off+28]),
		})
	}

	return records, nil
}

// SetStatsFilePath sets the stats file path (for testing).
func SetStatsFilePath(path string) {
	statsFilePath = path
}
