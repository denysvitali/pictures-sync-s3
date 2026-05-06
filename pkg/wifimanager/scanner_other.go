//go:build !linux

package wifimanager

import (
	"fmt"

	"github.com/mdlayher/wifi"
)

func readAccessPointsNL80211(_ *wifi.Interface) ([]*wifi.BSS, error) {
	return nil, fmt.Errorf("raw nl80211 scanning is only available on Linux")
}
