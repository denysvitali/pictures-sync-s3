package syncmanager

import (
	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

// isRetryableError determines if an error is worth retrying
// Uses the utils package for consistent error handling across the codebase
func isRetryableError(err error) bool {
	return utils.IsRetryableNetworkError(err)
}
