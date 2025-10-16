package syncmanager

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/operations"
)

// uploadToGooglePhotos uploads only JPG files to Google Photos
func (m *Manager) uploadToGooglePhotos(ctx context.Context, sourcePath, cardID string) error {
	// Load rclone config from custom path
	if err := m.loadRcloneConfig(); err != nil {
		return err
	}

	// Create source filesystem
	srcFs, err := fs.NewFs(ctx, sourcePath)
	if err != nil {
		return fmt.Errorf("failed to create source filesystem: %w", err)
	}

	// Destination path for Google Photos: remote:/cardID/
	destPath := m.googlePhotosRemoteName + ":" + cardID

	// Create destination filesystem
	dstFs, err := fs.NewFs(ctx, destPath)
	if err != nil {
		return fmt.Errorf("failed to create Google Photos destination filesystem: %w", err)
	}

	// Set up config with fewer parallel transfers for Google Photos
	ci := fs.GetConfig(ctx)
	ci.Transfers = 1 // Google Photos API is rate-limited, use single transfer
	ci.Checkers = 2

	log.Printf("Uploading JPG files to Google Photos at %s", destPath)

	// List all files in source
	var jpgFiles []fs.Object
	err = operations.ListFn(ctx, srcFs, func(obj fs.Object) {
		name := strings.ToLower(obj.Remote())
		if strings.HasSuffix(name, ".jpg") || strings.HasSuffix(name, ".jpeg") {
			jpgFiles = append(jpgFiles, obj)
		}
	})
	if err != nil {
		return fmt.Errorf("failed to list source files: %w", err)
	}

	if len(jpgFiles) == 0 {
		log.Printf("No JPG files found to upload to Google Photos")
		return nil
	}

	log.Printf("Found %d JPG files to upload to Google Photos", len(jpgFiles))

	// Copy each JPG file individually
	for i, obj := range jpgFiles {
		log.Printf("Uploading JPG %d/%d to Google Photos: %s", i+1, len(jpgFiles), obj.Remote())

		_, err := operations.Copy(ctx, dstFs, nil, obj.Remote(), obj)
		if err != nil {
			log.Printf("Warning: failed to upload %s to Google Photos: %v", obj.Remote(), err)
			// Continue with other files
		}
	}

	return nil
}
