package systeminfo

import (
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

var processStartedAt = time.Now()

type RuntimeInfo struct {
	SystemUptimeSeconds  float64    `json:"system_uptime_seconds,omitempty"`
	ProcessUptimeSeconds float64    `json:"process_uptime_seconds"`
	ProcessStartedAt     time.Time  `json:"process_started_at"`
	Memory               MemoryInfo `json:"memory"`
	Go                   GoInfo     `json:"go"`
	Cgroup               CgroupInfo `json:"cgroup,omitempty"`
}

type MemoryInfo struct {
	AllocBytes        uint64 `json:"alloc_bytes"`
	TotalAllocBytes   uint64 `json:"total_alloc_bytes"`
	SysBytes          uint64 `json:"sys_bytes"`
	HeapAllocBytes    uint64 `json:"heap_alloc_bytes"`
	HeapInuseBytes    uint64 `json:"heap_inuse_bytes"`
	HeapIdleBytes     uint64 `json:"heap_idle_bytes"`
	HeapReleasedBytes uint64 `json:"heap_released_bytes"`
	StackInuseBytes   uint64 `json:"stack_inuse_bytes"`
	NextGCBytes       uint64 `json:"next_gc_bytes"`
	NumGC             uint32 `json:"num_gc"`
	ProcessRSSBytes   uint64 `json:"process_rss_bytes,omitempty"`
	ProcessVMBytes    uint64 `json:"process_vm_bytes,omitempty"`
}

type GoInfo struct {
	Goroutines int    `json:"goroutines"`
	Version    string `json:"version"`
}

type CgroupInfo struct {
	MemoryCurrentBytes uint64 `json:"memory_current_bytes,omitempty"`
	MemoryMaxBytes     uint64 `json:"memory_max_bytes,omitempty"`
	OOMKillCount       uint64 `json:"oom_kill_count,omitempty"`
	OOMCount           uint64 `json:"oom_count,omitempty"`
}

func Snapshot() RuntimeInfo {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	rss, vm := readProcSelfMemory()

	return RuntimeInfo{
		SystemUptimeSeconds:  readSystemUptime(),
		ProcessUptimeSeconds: time.Since(processStartedAt).Seconds(),
		ProcessStartedAt:     processStartedAt.UTC(),
		Memory: MemoryInfo{
			AllocBytes:        mem.Alloc,
			TotalAllocBytes:   mem.TotalAlloc,
			SysBytes:          mem.Sys,
			HeapAllocBytes:    mem.HeapAlloc,
			HeapInuseBytes:    mem.HeapInuse,
			HeapIdleBytes:     mem.HeapIdle,
			HeapReleasedBytes: mem.HeapReleased,
			StackInuseBytes:   mem.StackInuse,
			NextGCBytes:       mem.NextGC,
			NumGC:             mem.NumGC,
			ProcessRSSBytes:   rss,
			ProcessVMBytes:    vm,
		},
		Go: GoInfo{
			Goroutines: runtime.NumGoroutine(),
			Version:    runtime.Version(),
		},
		Cgroup: readCgroupInfo(),
	}
}

func readSystemUptime() float64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	uptime, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0
	}
	return uptime
}

func readProcSelfMemory() (rssBytes, vmBytes uint64) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		switch {
		case strings.HasPrefix(line, "VmRSS:"):
			rssBytes = parseStatusKB(line) * 1024
		case strings.HasPrefix(line, "VmSize:"):
			vmBytes = parseStatusKB(line) * 1024
		}
	}
	return rssBytes, vmBytes
}

func parseStatusKB(line string) uint64 {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return 0
	}
	value, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func readCgroupInfo() CgroupInfo {
	info := CgroupInfo{
		MemoryCurrentBytes: readUintFile("/sys/fs/cgroup/memory.current"),
		MemoryMaxBytes:     readUintOrMax("/sys/fs/cgroup/memory.max"),
	}
	events := readKeyValueFile("/sys/fs/cgroup/memory.events")
	info.OOMKillCount = events["oom_kill"]
	info.OOMCount = events["oom"]
	return info
}

func readUintOrMax(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	raw := strings.TrimSpace(string(data))
	if raw == "max" {
		return 0
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func readUintFile(path string) uint64 {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	value, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64)
	if err != nil {
		return 0
	}
	return value
}

func readKeyValueFile(path string) map[string]uint64 {
	out := map[string]uint64{}
	data, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		value, err := strconv.ParseUint(fields[1], 10, 64)
		if err == nil {
			out[fields[0]] = value
		}
	}
	return out
}
