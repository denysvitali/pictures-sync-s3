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
func GetOrCreateCardID(mountPath string, monitor *Monitor) (string, bool, error) {
	idPath := filepath.Join(mountPath, CardIDFile)

	// Try to read existing ID
	if data, err := os.ReadFile(idPath); err == nil {
		cardID := strings.TrimSpace(string(data))
		if cardID != "" {
			log.Printf("Found existing card ID: %s", cardID)
			// Remount read-only now that we've read the ID
			if monitor != nil {
				if err := monitor.RemountReadOnly(); err != nil {
					log.Printf("ERROR: Failed to remount read-only after reading card ID: %v", err)
					// This is critical - SD card remains read-write and could be corrupted
					return cardID, false, fmt.Errorf("failed to remount read-only: %w", err)
				}
			}
			return cardID, false, nil
		}
	}

	// Generate new ID
	newID := generateCardID()
	log.Printf("Generated new card ID: %s", newID)

	// Write to card (filesystem should be read-write at this point)
	if err := os.WriteFile(idPath, []byte(newID+"\n"), 0644); err != nil {
		log.Printf("ERROR: Could not write card ID to %s: %v", idPath, err)
		// This means the card will get a different ID next time
		return newID, true, fmt.Errorf("failed to write card ID: %w", err)
	} else {
		log.Printf("Successfully wrote card ID to %s", idPath)
	}

	// Remount read-only now that we've written the ID
	if monitor != nil {
		if err := monitor.RemountReadOnly(); err != nil {
			log.Printf("ERROR: Failed to remount read-only after writing card ID: %v", err)
			// This is critical - SD card remains read-write and could be corrupted
			return newID, true, fmt.Errorf("failed to remount read-only: %w", err)
		}
	}

	return newID, true, nil
}

// CreateNewCardID forces creation of a new card ID (for reformatted cards)
// The monitor parameter is optional - if provided, it will remount read-only after writing
func CreateNewCardID(mountPath string, monitor *Monitor) (string, error) {
	newID := generateCardID()
	idPath := filepath.Join(mountPath, CardIDFile)

	log.Printf("Creating new card ID: %s", newID)

	// Write to card (filesystem should be read-write at this point)
	if err := os.WriteFile(idPath, []byte(newID+"\n"), 0644); err != nil {
		log.Printf("Warning: Could not write card ID to %s: %v", idPath, err)
		// Continue anyway
	} else {
		log.Printf("Successfully wrote new card ID to %s", idPath)
	}

	// Remount read-only now that we've written the ID
	if monitor != nil {
		if err := monitor.RemountReadOnly(); err != nil {
			log.Printf("Warning: Failed to remount read-only: %v", err)
		}
	}

	return newID, nil
}

// generateCardID generates a unique card ID
func generateCardID() string {
	// Generate 8 random bytes
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("card-%d", time.Now().Unix())
	}
	return fmt.Sprintf("card-%s", hex.EncodeToString(b))
}
