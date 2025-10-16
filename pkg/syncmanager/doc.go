// Package syncmanager integrates with rclone to perform photo synchronization operations.
// It spawns rclone as a subprocess, parses progress from JSON logs, manages retries with
// exponential backoff, and supports multiple storage backends (B2, S3, Google Drive, Azure).
//
// Architecture:
//   - manager.go: Core Manager struct and lifecycle methods (New, Cancel, Subscribe, etc.)
//   - sync.go: Main Sync() method and sync operation orchestration
//   - progress.go: Progress monitoring, calculation, and broadcasting to subscribers
//   - remote.go: Remote operations (list files, test connection, get files)
//   - googlephotos.go: Google Photos integration for JPG upload
//   - retry.go: Retry logic with exponential backoff for transient errors
//   - validation.go: Input validation helpers (card ID format, path traversal prevention)
//
// The package is thread-safe and uses channels for progress updates.
package syncmanager
