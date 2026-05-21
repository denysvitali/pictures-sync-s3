package netiface

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CarrierRoot is the sysfs root used for Ethernet carrier checks.
var CarrierRoot = "/sys/class/net"

// HasCarrier reports whether an interface has physical carrier according to
// Linux sysfs. Missing interfaces are treated as no carrier.
func HasCarrier(name string) (bool, error) {
	if strings.TrimSpace(name) == "" {
		return false, nil
	}
	path := filepath.Join(CarrierRoot, name, "carrier")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	value := strings.TrimSpace(string(data))
	switch value {
	case "1":
		return true, nil
	case "0", "":
		return false, nil
	default:
		return false, fmt.Errorf("unexpected carrier value %q", value)
	}
}
