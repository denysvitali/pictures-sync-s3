package sdmonitor

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// HasDCIM checks if the mounted SD card has a DCIM directory
func HasDCIM(mountPath string) bool {
	dcimPath := filepath.Join(mountPath, "DCIM")
	info, err := os.Stat(dcimPath)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// CountPhotos counts every regular file under the DCIM directory. The count
// must match what rclone will actually upload — rclone syncs the whole DCIM
// tree without an extension filter, so filtering here would let the progress
// counter overshoot the precomputed total (e.g. "Uploading 69 of 64 files"
// when the card has sidecar files like .thm/.xmp/.modd or formats not on a
// hardcoded whitelist such as .heic/.dng/.mts).
func CountPhotos(mountPath string) (int, int64, error) {
	dcimPath := filepath.Join(mountPath, "DCIM")
	info, err := os.Stat(dcimPath)
	if err != nil {
		return 0, 0, err
	}
	if !info.IsDir() {
		return 0, 0, fmt.Errorf("DCIM is not a directory")
	}

	var count int
	var totalSize int64

	err = filepath.WalkDir(dcimPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if d.IsDir() {
			return nil
		}

		count++
		fi, err := d.Info()
		if err == nil {
			totalSize += fi.Size()
		}
		return nil
	})

	return count, totalSize, err
}
