package main

import (
	"log"

	"github.com/denysvitali/pictures-sync-s3/pkg/daemon"
)

func main() {
	// Enable caller reporting in logs (file:line)
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	log.Println("Photo Backup Station - Starting...")

	// Create daemon with default configuration
	cfg := daemon.DefaultConfig()
	svc, err := daemon.New(cfg)
	if err != nil {
		log.Fatalf("Failed to create daemon: %v", err)
	}
	defer svc.Shutdown()

	// Run the daemon (blocks until shutdown signal)
	if err := svc.Run(); err != nil {
		log.Fatalf("Daemon error: %v", err)
	}

	log.Println("Photo Backup Station - Shutdown complete")
}
