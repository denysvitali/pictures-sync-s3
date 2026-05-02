//go:build linux

package provisionap

import "golang.org/x/sys/unix"

func rebootSystem() error {
	return unix.Reboot(unix.LINUX_REBOOT_CMD_RESTART)
}
