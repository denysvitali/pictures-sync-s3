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
	checks := int(stats.GetChecks())

	// Calculate total bytes including what was already synced
	totalTransferred := alreadySyncedBytes + sessionTransferred

	// Get current file being transferred or checked from RemoteStats
	var currentFile string
	var currentFileSize int64

	// Try to get remote stats which includes current transfers and checks
	if remoteStats, err := stats.RemoteStats(false); err == nil {
		// First try transferring files (higher priority)
		if transferring, ok := remoteStats["transferring"].([]any); ok && len(transferring) > 0 {
			if transfer, ok := transferring[0].(map[string]any); ok {
				if name, ok := transfer["name"].(string); ok {
					currentFile = name
				}
				if size, ok := transfer["size"].(int64); ok {
					currentFileSize = size
				}
			}
		} else if checking, ok := remoteStats["checking"].([]any); ok && len(checking) > 0 {
			// If nothing transferring, show what's being checked
			if check, ok := checking[0].(map[string]any); ok {
				if name, ok := check["name"].(string); ok {
					currentFile = "[Checking] " + name
				}
				if size, ok := check["size"].(int64); ok {
					currentFileSize = size
				}
			}
		}
	}

	// Calculate speed from elapsed time and bytes transferred in this session
	m.mu.Lock()
	startTime := m.startTime
	m.mu.Unlock()
	elapsed := time.Since(startTime)
	var speed float64
	if elapsed > 0 {
		speed = float64(sessionTransferred) / elapsed.Seconds()
	}

	// Calculate percentage based on:
	// - During checking phase (transfers=0): use checks/totalFiles
	// - During transfer phase: use bytes transferred
	var percentage int
	var filesProcessed int
	if transfers > 0 && totalBytes > 0 {
		// Transfer phase - use bytes for accurate percentage
		percentage = int((float64(totalTransferred) / float64(totalBytes)) * 100)
		filesProcessed = transfers
	} else if checks > 0 && totalFiles > 0 {
		// Checking phase - use file count
		percentage = int((float64(checks) / float64(totalFiles)) * 100)
		filesProcessed = checks
	}

	// Calculate ETA and format it (based on remaining bytes and current speed)
	var etaSeconds int
	var etaStr string
	if speed > 0 && totalBytes > 0 {
		remaining := totalBytes - totalTransferred
		etaSeconds = int(float64(remaining) / speed)
		etaStr = formatDuration(etaSeconds)
	} else if checks > 0 && transfers == 0 && totalFiles > 0 {
		// During checking phase, estimate based on check rate
		if elapsed > 0 {
			checksPerSec := float64(checks) / elapsed.Seconds()
			if checksPerSec > 0 {
				remainingChecks := totalFiles - checks
				etaSeconds = int(float64(remainingChecks) / checksPerSec)
				etaStr = formatDuration(etaSeconds)
			}
		}
	}

	// Update state manager with current file info including speed and ETA
	// Use filesProcessed (either checks or transfers) for accurate progress
	m.stateMgr.UpdateSyncProgress(int64(filesProcessed), totalTransferred, currentFile, currentFileSize, speed, etaStr)

	return Progress{
		BytesTransferred: totalTransferred, // Use total including already synced
		Percentage:       percentage,
		TransferredFiles: filesProcessed, // Either checks or transfers
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
	subs := make([]*progressSubscriber, len(m.progressChans))
	copy(subs, m.progressChans)
	m.mu.Unlock()

	for _, s := range subs {
		s.send(progress)
	}
}

// logProgress logs the current progress
func (m *Manager) logProgress(progress Progress, totalFiles int) {
	// Show different messages for checking vs transferring phase
	if progress.Speed > 0 {
		log.Printf("Progress: %d/%d files, %d%%, %.2f MB/s",
			progress.TransferredFiles, totalFiles, progress.Percentage, progress.Speed/(1024*1024))
	} else {
		// During checking phase (no transfer speed yet)
		log.Printf("Checking: %d/%d files, %d%%",
			progress.TransferredFiles, totalFiles, progress.Percentage)
	}
}

// formatDuration formats seconds into a human-readable duration string
// Uses the utils package for consistent formatting across the codebase
func formatDuration(seconds int) string {
	return utils.FormatDuration(seconds)
}
