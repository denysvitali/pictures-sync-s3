package syncmanager

import (
	"context"
	"log"
	"time"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
	"github.com/rclone/rclone/fs/accounting"
)

// monitorProgress monitors sync progress and updates state
func (m *Manager) monitorProgress(ctx context.Context, stats *accounting.StatsInfo, totalFiles int, totalBytes int64, alreadySyncedBytes int64, done chan struct{}) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		case <-ticker.C:
			progress := m.calculateProgress(stats, totalFiles, totalBytes, alreadySyncedBytes)
			m.broadcastProgress(progress)
			m.logProgress(progress, totalFiles)
		}
	}
}

// calculateProgress calculates the current sync progress
func (m *Manager) calculateProgress(stats *accounting.StatsInfo, totalFiles int, totalBytes int64, alreadySyncedBytes int64) Progress {
	// Get current stats (bytes transferred in this session only)
	sessionTransferred := stats.GetBytes()
	transfers := int(stats.GetTransfers())

	// Calculate total bytes including what was already synced
	totalTransferred := alreadySyncedBytes + sessionTransferred

	// Get current file being transferred from RemoteStats
	var currentFile string
	var currentFileSize int64

	// Try to get remote stats which includes current transfers
	if remoteStats, err := stats.RemoteStats(true); err == nil {
		if transferring, ok := remoteStats["transferring"].([]interface{}); ok && len(transferring) > 0 {
			if transfer, ok := transferring[0].(map[string]interface{}); ok {
				if name, ok := transfer["name"].(string); ok {
					currentFile = name
				}
				if size, ok := transfer["size"].(int64); ok {
					currentFileSize = size
				}
			}
		}
	}

	// Calculate speed from elapsed time and bytes transferred in this session
	elapsed := time.Since(m.startTime)
	var speed float64
	if elapsed > 0 {
		speed = float64(sessionTransferred) / elapsed.Seconds()
	}

	// Calculate percentage based on total progress (including already synced)
	var percentage int
	if totalBytes > 0 {
		percentage = int((float64(totalTransferred) / float64(totalBytes)) * 100)
	}

	// Calculate ETA and format it (based on remaining bytes and current speed)
	var etaSeconds int
	var etaStr string
	if speed > 0 {
		remaining := totalBytes - totalTransferred
		etaSeconds = int(float64(remaining) / speed)
		etaStr = formatDuration(etaSeconds)
	}

	// Update state manager with current file info including speed and ETA
	// Note: We report totalTransferred (including already synced) for accurate percentage
	m.stateMgr.UpdateSyncProgress(int64(transfers), totalTransferred, currentFile, currentFileSize, speed, etaStr)

	return Progress{
		BytesTransferred: totalTransferred, // Use total including already synced
		Percentage:       percentage,
		TransferredFiles: transfers,
		TotalFiles:       totalFiles,
		Speed:            speed,
		ETA:              etaSeconds,
		CurrentFile:      currentFile,
		CurrentFileSize:  currentFileSize,
	}
}

// broadcastProgress sends progress updates to all subscribers
func (m *Manager) broadcastProgress(progress Progress) {
	m.mu.Lock()
	chans := m.progressChans
	m.mu.Unlock()

	for _, ch := range chans {
		select {
		case ch <- progress:
		default:
			// Skip if channel is full
		}
	}
}

// logProgress logs the current progress
func (m *Manager) logProgress(progress Progress, totalFiles int) {
	log.Printf("Progress: %d/%d files, %d%%, %.2f MB/s",
		progress.TransferredFiles, totalFiles, progress.Percentage, progress.Speed/(1024*1024))
}

// formatDuration formats seconds into a human-readable duration string
// Uses the utils package for consistent formatting across the codebase
func formatDuration(seconds int) string {
	return utils.FormatDuration(seconds)
}
