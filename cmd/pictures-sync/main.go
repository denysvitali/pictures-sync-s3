package main

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"

	"github.com/denysvitali/pictures-sync-s3/pkg/daemon"
)

const (
	// logDir is the directory on the persistent /perm partition where the
	// daemon writes app.log. /perm is the only writable location that
	// survives reboots on Gokrazy.
	logDir = "/perm/pictures-sync/log"

	// logFileName is the active log file name.
	logFileName = "app.log"

	// rotatedLogFileName is the single rotated generation.
	rotatedLogFileName = "app.log.1"

	// logRotateThreshold rotates app.log when it exceeds 5 MB at startup.
	// This is a low-volume daemon so a single rotation generation is plenty.
	logRotateThreshold int64 = 5 * 1024 * 1024
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	logFile, err := setupPersistentLog(logDir, logFileName, rotatedLogFileName, logRotateThreshold)
	if err != nil {
		log.Printf("Warning: persistent logging unavailable, falling back to stdout: %v", err)
	} else {
		log.SetOutput(io.MultiWriter(os.Stderr, logFile))
		defer logFile.Close()
	}

	log.Println("Photo Backup Station - Starting...")

	cfg := daemon.DefaultConfig()
	svc, err := daemon.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create daemon: %v", err)
	}
	defer svc.Shutdown()

	if err := svc.Run(); err != nil {
		log.Fatalf("Daemon error: %v", err)
	}

	log.Println("Photo Backup Station - Shutdown complete")
}

// setupPersistentLog ensures dir exists, rotates app.log -> app.log.1 if it
// exceeds threshold, then opens app.log for append. Returns the open file on
// success; the caller is responsible for closing it. On any failure the
// caller should keep the default stdout-only logging.
func setupPersistentLog(dir, name, rotated string, threshold int64) (*os.File, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir %s: %w", dir, err)
	}

	logPath := filepath.Join(dir, name)
	rotatedPath := filepath.Join(dir, rotated)

	if info, err := os.Stat(logPath); err == nil && info.Size() > threshold {
		if err := os.Rename(logPath, rotatedPath); err != nil {
			return nil, fmt.Errorf("rotate %s -> %s: %w", logPath, rotatedPath, err)
		}
	}

	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", logPath, err)
	}
	return f, nil
}
