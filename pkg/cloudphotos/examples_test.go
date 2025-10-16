package cloudphotos_test

import (
	"fmt"
	"log"

	"github.com/denysvitali/pictures-sync-s3/pkg/cloudphotos"
	"github.com/denysvitali/pictures-sync-s3/pkg/state"
	"github.com/denysvitali/pictures-sync-s3/pkg/syncmanager"
)

// Example_listCards demonstrates how to list all card IDs with their photo counts
func Example_listCards() {
	// Initialize managers
	stateMgr, _ := state.NewManager()
	syncMgr := syncmanager.NewManager(
		"/perm/pictures-sync/rclone.conf",
		"remote",
		"/photos",
		stateMgr,
		4, // transfers
		8, // checkers
	)

	photoMgr, err := cloudphotos.NewManager(
		"/perm/pictures-sync/rclone.conf",
		"remote",
		"/photos",
		syncMgr,
	)
	if err != nil {
		log.Fatal(err)
	}

	// List all cards
	cards, err := photoMgr.ListCards()
	if err != nil {
		log.Fatal(err)
	}

	// Display card information
	for _, card := range cards {
		fmt.Printf("Card: %s\n", card.CardID)
		fmt.Printf("  Photos: %d\n", card.PhotoCount)
		fmt.Printf("  Size: %s\n", card.TotalSizeHuman)
		fmt.Printf("  Last Modified: %s\n\n", card.LastModified.Format("2006-01-02 15:04:05"))
	}
}

// Example_listPhotos demonstrates how to list photos for a specific card
func Example_listPhotos() {
	// Initialize managers (same as above)
	stateMgr, _ := state.NewManager()
	syncMgr := syncmanager.NewManager(
		"/perm/pictures-sync/rclone.conf",
		"remote",
		"/photos",
		stateMgr,
		4, 8,
	)

	photoMgr, _ := cloudphotos.NewManager(
		"/perm/pictures-sync/rclone.conf",
		"remote",
		"/photos",
		syncMgr,
	)

	// List photos for a specific card
	cardID := "card-a1b2c3d4"
	photos, err := photoMgr.ListPhotos(cardID)
	if err != nil {
		log.Fatal(err)
	}

	// Display photo information
	for _, photo := range photos {
		fmt.Printf("Photo: %s\n", photo.Name)
		fmt.Printf("  Size: %s\n", photo.SizeHuman)
		fmt.Printf("  Date: %s\n", photo.ModTime.Format("2006-01-02 15:04:05"))
		fmt.Printf("  Cached: %v\n\n", photo.IsCached)
	}
}

// Example_downloadPhoto demonstrates how to download a photo from cloud storage
func Example_downloadPhoto() {
	// Initialize managers
	stateMgr, _ := state.NewManager()
	syncMgr := syncmanager.NewManager(
		"/perm/pictures-sync/rclone.conf",
		"remote",
		"/photos",
		stateMgr,
		4, 8,
	)

	photoMgr, _ := cloudphotos.NewManager(
		"/perm/pictures-sync/rclone.conf",
		"remote",
		"/photos",
		syncMgr,
	)

	// Download a photo
	photoPath := "card-a1b2c3d4/DCIM/IMG_1234.JPG"
	cachedPath, err := photoMgr.DownloadPhoto(photoPath)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Photo downloaded to: %s\n", cachedPath)

	// Check if photo is already cached (second call will be instant)
	if photoMgr.GetCachedPhoto(photoPath) != "" {
		fmt.Println("Photo is now cached")
	}
}

// Example_cacheManagement demonstrates cache management operations
func Example_cacheManagement() {
	// Initialize managers
	stateMgr, _ := state.NewManager()
	syncMgr := syncmanager.NewManager(
		"/perm/pictures-sync/rclone.conf",
		"remote",
		"/photos",
		stateMgr,
		4, 8,
	)

	photoMgr, _ := cloudphotos.NewManager(
		"/perm/pictures-sync/rclone.conf",
		"remote",
		"/photos",
		syncMgr,
	)

	// Get cache statistics
	stats, err := photoMgr.GetCacheStats()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Cache Stats:\n")
	fmt.Printf("  Files: %d\n", stats["file_count"])
	fmt.Printf("  Size: %s\n", stats["total_size_human"])
	fmt.Printf("  Usage: %.1f%%\n", stats["usage_percent"])
	fmt.Printf("  Max Size: %s\n", stats["max_size_human"])
	fmt.Printf("  TTL: %.0f hours\n", stats["ttl_hours"])

	// Clear cache if needed
	if stats["usage_percent"].(float64) > 90 {
		if err := photoMgr.ClearCache(); err != nil {
			log.Fatal(err)
		}
		fmt.Println("Cache cleared")
	}
}

// Example_integrationWithSyncHistory shows how to access photos from sync history
func Example_integrationWithSyncHistory() {
	// Initialize managers
	stateMgr, _ := state.NewManager()
	syncMgr := syncmanager.NewManager(
		"/perm/pictures-sync/rclone.conf",
		"remote",
		"/photos",
		stateMgr,
		4, 8,
	)

	photoMgr, _ := cloudphotos.NewManager(
		"/perm/pictures-sync/rclone.conf",
		"remote",
		"/photos",
		syncMgr,
	)

	// Get sync history
	history := stateMgr.GetHistory()

	// For each synced card, get photos
	for _, record := range history {
		if record.Status != "success" {
			continue
		}

		fmt.Printf("Card: %s (synced %s)\n", record.CardID, record.EndTime.Format("2006-01-02"))

		photos, err := photoMgr.ListPhotos(record.CardID)
		if err != nil {
			log.Printf("  Error listing photos: %v\n", err)
			continue
		}

		fmt.Printf("  Photos: %d (%s total)\n\n", len(photos), record.CardID)
	}
}
