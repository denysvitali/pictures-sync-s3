package syncmanager

import (
	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

// validateCardID checks if a card ID is safe to use in paths
// Uses the utils package for consistent validation across the codebase
func validateCardID(cardID string) error {
	return utils.ValidateCardID(cardID)
}
