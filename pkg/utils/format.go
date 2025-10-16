package utils

import (
	"fmt"
	"time"
)

// FormatBytes formats bytes to a human-readable string (e.g., "1.5 MB", "234 KB").
func FormatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}

// FormatBytesFloat formats bytes (as float64) to a human-readable string.
func FormatBytesFloat(bytes float64) string {
	return FormatBytes(int64(bytes))
}

// FormatDuration formats a duration in seconds to a human-readable string (e.g., "1h 30m", "45s").
func FormatDuration(seconds int) string {
	if seconds < 0 {
		seconds = 0
	}

	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}

	minutes := seconds / 60
	if minutes < 60 {
		return fmt.Sprintf("%dm %ds", minutes, seconds%60)
	}

	hours := minutes / 60
	return fmt.Sprintf("%dh %dm", hours, minutes%60)
}

// FormatDurationTime formats a time.Duration to a human-readable string.
func FormatDurationTime(d time.Duration) string {
	return FormatDuration(int(d.Seconds()))
}

// FormatPercentage formats a percentage value (0-100) as a string with 1 decimal place.
func FormatPercentage(value float64) string {
	return fmt.Sprintf("%.1f%%", value)
}

// FormatSpeed formats bytes per second to a human-readable speed string (e.g., "5.2 MB/s").
func FormatSpeed(bytesPerSecond float64) string {
	return FormatBytesFloat(bytesPerSecond) + "/s"
}
