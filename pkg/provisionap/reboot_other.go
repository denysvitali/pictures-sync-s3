//go:build !linux

package provisionap

func rebootSystem() error {
	return nil
}
