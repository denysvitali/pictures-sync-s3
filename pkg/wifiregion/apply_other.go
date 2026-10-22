//go:build !linux

package wifiregion

import "fmt"

// Apply is only supported on Linux.
func Apply(country string) error {
	return fmt.Errorf("setting the regulatory domain is only available on Linux")
}
