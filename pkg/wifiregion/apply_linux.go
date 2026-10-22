package wifiregion

import (
	"fmt"

	"github.com/mdlayher/genetlink"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// Apply asks the kernel (nl80211) to use the given regulatory region now.
func Apply(country string) error {
	country, err := Normalize(country)
	if err != nil {
		return err
	}

	conn, err := genetlink.Dial(nil)
	if err != nil {
		return fmt.Errorf("genetlink dial: %w", err)
	}
	defer conn.Close()

	family, err := conn.GetFamily(unix.NL80211_GENL_NAME)
	if err != nil {
		return fmt.Errorf("get nl80211 family: %w", err)
	}

	ae := netlink.NewAttributeEncoder()
	ae.String(unix.NL80211_ATTR_REG_ALPHA2, country)
	data, err := ae.Encode()
	if err != nil {
		return fmt.Errorf("encode attrs: %w", err)
	}

	_, err = conn.Execute(genetlink.Message{
		Header: genetlink.Header{
			Command: unix.NL80211_CMD_REQ_SET_REG,
			Version: family.Version,
		},
		Data: data,
	}, family.ID, netlink.Request|netlink.Acknowledge)
	if err != nil {
		return fmt.Errorf("set regulatory domain: %w", err)
	}
	return nil
}
