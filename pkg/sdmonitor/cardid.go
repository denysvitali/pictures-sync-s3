package sdmonitor

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// CardIDFile is the name of the file storing the card's unique ID
	CardIDFile = ".pictures-sync-id"
)

// GetOrCreateCardID reads or creates a unique ID for the SD card
// Returns: (cardID, isNewCard, error)
// The monitor parameter is optional - if provided, it will remount read-only after writing
// NOTE: This function assumes the filesystem is already mounted read-write
func GetOrCreateCardID(mountPath string, monitor *Monitor) (string, bool, error) {
	idPath := filepath.Join(mountPath, CardIDFile)

	log.Printf("CardID: Attempting to read or create card ID at %s", idPath)

	// Try to read existing ID
	// #nosec G304 -- idPath is constructed from mount path + well-known CardIDFile constant
	if data, err := os.ReadFile(idPath); err == nil {
		cardID := strings.TrimSpace(string(data))
		if cardID != "" {
			log.Printf("CardID: Found existing card ID: %s", cardID)
			// Remount read-only now that we've read the ID
			if monitor != nil {
				// Note: RemountReadOnly does not need mountMu because it's a different syscall
				// that doesn't conflict with mount/unmount operations
				if err := monitor.RemountReadOnly(); err != nil {
					log.Printf("CardID ERROR: Failed to remount read-only after reading card ID: %v", err)
					// This is critical - SD card remains read-write and could be corrupted
					return cardID, false, fmt.Errorf("failed to remount read-only: %w", err)
				}
			}
			return cardID, false, nil
		}
		log.Printf("CardID: Card ID file exists but is empty, generating new ID")
	} else {
		log.Printf("CardID: No existing card ID file found (%v), generating new ID", err)
	}

	// Generate new ID
	newID := generateCardID()
	log.Printf("CardID: Generated new card ID: %s", newID)

	// Write to card (filesystem should be read-write at this point)
	// #nosec G306 -- Card ID on SD card must be readable by other processes
	if err := os.WriteFile(idPath, []byte(newID+"\n"), 0644); err != nil {
		log.Printf("CardID ERROR: Could not write card ID to %s: %v", idPath, err)
		// This means the card will get a different ID next time
		return newID, true, fmt.Errorf("failed to write card ID: %w", err)
	}
	log.Printf("CardID: Successfully wrote card ID to %s", idPath)

	// Remount read-only now that we've written the ID
	if monitor != nil {
		if err := monitor.RemountReadOnly(); err != nil {
			log.Printf("CardID ERROR: Failed to remount read-only after writing card ID: %v", err)
			// This is critical - SD card remains read-write and could be corrupted
			return newID, true, fmt.Errorf("failed to remount read-only: %w", err)
		}
		log.Printf("CardID: Successfully remounted read-only after writing card ID")
	}

	return newID, true, nil
}

// CreateNewCardID forces creation of a new card ID (for reformatted cards)
// The monitor parameter is optional - if provided, it will remount read-only after writing
// NOTE: This function assumes the filesystem is already mounted read-write
func CreateNewCardID(mountPath string, monitor *Monitor) (string, error) {
	newID := generateCardID()
	idPath := filepath.Join(mountPath, CardIDFile)

	log.Printf("CardID: Creating new card ID: %s", newID)

	// Write to card (filesystem should be read-write at this point)
	// #nosec G306 -- Card ID on SD card must be readable by other processes
	if err := os.WriteFile(idPath, []byte(newID+"\n"), 0644); err != nil {
		log.Printf("CardID ERROR: Could not write card ID to %s: %v", idPath, err)
		// This is a critical error - return it instead of ignoring
		return newID, fmt.Errorf("failed to write new card ID: %w", err)
	}
	log.Printf("CardID: Successfully wrote new card ID to %s", idPath)

	// Remount read-only now that we've written the ID
	if monitor != nil {
		if err := monitor.RemountReadOnly(); err != nil {
			log.Printf("CardID ERROR: Failed to remount read-only after creating new card ID: %v", err)
			// This is critical - SD card remains read-write and could be corrupted
			return newID, fmt.Errorf("failed to remount read-only: %w", err)
		}
		log.Printf("CardID: Successfully remounted read-only after creating new card ID")
	}

	return newID, nil
}

// generateCardID generates a unique card ID
func generateCardID() string {
	// Generate 8 random bytes using crypto/rand
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to high-resolution timestamp with process ID if crypto/rand fails
		// This is extremely unlikely but provides collision resistance even in failure case
		// Format: card-<nanoseconds>-<pid>
		// Example: card-1729180000123456789-1234
		now := time.Now().UnixNano()
		pid := os.Getpid()
		return fmt.Sprintf("card-%d-%d", now, pid)
	}
	// Format: card-<16 hex chars>
	// Example: card-a1b2c3d4e5f6a7b8
	return fmt.Sprintf("card-%s", hex.EncodeToString(b))
}
