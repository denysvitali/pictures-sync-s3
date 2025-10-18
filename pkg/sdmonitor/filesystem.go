package sdmonitor

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Photo file extensions
var photoExtensions = map[string]bool{
	".jpg":  true,
	".jpeg": true,
	".png":  true,
	".gif":  true,
	".raw":  true,
	".cr2":  true,
	".nef":  true,
	".arw":  true,
	".mp4":  true,
	".mov":  true,
	".avi":  true,
	".mkv":  true,
}

// HasDCIM checks if the mounted SD card has a DCIM directory
func HasDCIM(mountPath string) bool {
	dcimPath := filepath.Join(mountPath, "DCIM")
	info, err := os.Stat(dcimPath)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// CountPhotos counts photo files in DCIM directory
func CountPhotos(mountPath string) (int, int64, error) {
	dcimPath := filepath.Join(mountPath, "DCIM")

	var count int
	var totalSize int64

	err := filepath.WalkDir(dcimPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		// Check if it's an image or video file
		ext := strings.ToLower(filepath.Ext(path))
		if photoExtensions[ext] {
			count++
			info, err := d.Info()
			if err == nil {
				totalSize += info.Size()
			}
		}
		return nil
	})

	return count, totalSize, err
}

