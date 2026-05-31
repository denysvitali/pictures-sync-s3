package syncmanager

import (
	"strings"

	"github.com/denysvitali/pictures-sync-s3/pkg/utils"
)

// isRetryableError determines if an error is worth retrying. It covers the
// generic network/HTTP cases plus the transient Google Photos batch-upload
// failures that the generic detector doesn't know about.
func isRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if utils.IsRetryableNetworkError(err) {
		return true
	}
	return isRetryableGooglePhotosError(err)
}

// isRetryableGooglePhotosError reports whether a Google Photos batchCreate
// failure is transient and worth retrying.
//
// Google's BatchCreate can fail a single media item with a gRPC status code
// even when the HTTP call succeeded; rclone surfaces it as
// "upload failed: <message> (<code>)". Code 13 (INTERNAL) and 14 (UNAVAILABLE)
// are server-side hiccups that clear on retry — the common one we see is
// "There was an error while trying to create this media item. (13)". Treating
// these as fatal aborts the whole sync for what is really a momentary blip.
//
// "failed to commit batch" / "failed to create media item" are rclone's
// wrappers around the same transient BatchCreate round-trip.
func isRetryableGooglePhotosError(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "create this media item") ||
		strings.Contains(s, "failed to commit batch") ||
		strings.Contains(s, "failed to create media item") ||
		strings.Contains(s, " (13)") || // INTERNAL
		strings.Contains(s, " (14)") // UNAVAILABLE
}
