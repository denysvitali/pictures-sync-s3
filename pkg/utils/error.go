package utils

import (
	"errors"
	"fmt"
	"strings"
)

// WrapError wraps an error with additional context.
func WrapError(err error, context string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", context, err)
}

// WrapErrorf wraps an error with formatted context.
func WrapErrorf(err error, format string, args ...interface{}) error {
	if err == nil {
		return nil
	}
	context := fmt.Sprintf(format, args...)
	return fmt.Errorf("%s: %w", context, err)
}

// IsRetryableNetworkError determines if an error is worth retrying based on common network error patterns.
func IsRetryableNetworkError(err error) bool {
	if err == nil {
		return false
	}

	errStr := strings.ToLower(err.Error())

	// Network-related errors
	if strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "temporary failure") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "server misbehaving") ||
		strings.Contains(errStr, "servfail") ||
		strings.Contains(errStr, "no upstream resolvers set") ||
		strings.Contains(errStr, "network is unreachable") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "eof") {
		return true
	}

	// Rate limiting errors
	if strings.Contains(errStr, "rate limit") ||
		strings.Contains(errStr, "too many requests") ||
		strings.Contains(errStr, "429") {
		return true
	}

	// Temporary server errors
	if strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "502") ||
		strings.Contains(errStr, "500") {
		return true
	}

	// Concurrency / serialization conflicts. Google Photos returns
	// "(409 ABORTED) The operation was aborted." when concurrent batch
	// commits race on the same album; the canonical remedy is retry with
	// backoff.
	if strings.Contains(errStr, "409 aborted") ||
		strings.Contains(errStr, "operation was aborted") {
		return true
	}

	return false
}

// JoinErrors combines multiple errors into a single error message.
// Deprecated: Use errors.Join from the standard library instead (Go 1.20+).
func JoinErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}

	// Filter out nil errors for errors.Join
	var nonNilErrs []error
	for _, err := range errs {
		if err != nil {
			nonNilErrs = append(nonNilErrs, err)
		}
	}

	if len(nonNilErrs) == 0 {
		return nil
	}

	return fmt.Errorf("multiple errors: %w", errors.Join(nonNilErrs...))
}
