package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/denysvitali/pictures-sync-s3/pkg/provisionap"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Println("Photo Backup Station Provisioning AP - Starting...")

	cfg, err := provisionap.ConfigFromEnv()
	if err != nil {
		log.Fatalf("invalid provisioning AP config: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	manager := provisionap.NewManager(cfg)
	if err := manager.Run(ctx); err != nil {
		log.Fatalf("provisioning AP failed: %v", err)
	}

	log.Println("Photo Backup Station Provisioning AP - Stopped")
}
