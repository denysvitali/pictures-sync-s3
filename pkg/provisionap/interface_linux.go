package provisionap

import (
	"fmt"
	"net"
	"unsafe"

	"golang.org/x/sys/unix"
)

// InterfaceConfigurer configures a network interface for AP mode.
type InterfaceConfigurer interface {
	Configure(name string, ip net.IP, mask net.IPMask) error
}

type linuxInterfaceConfigurer struct{}

func (linuxInterfaceConfigurer) Configure(name string, ip net.IP, mask net.IPMask) error {
	ip = ip.To4()
	if ip == nil {
		return fmt.Errorf("IP must be IPv4")
	}
	if len(mask) != net.IPv4len {
		return fmt.Errorf("netmask must be IPv4")
	}
	if err := setInterfaceDown(name); err != nil {
		return err
	}
	if err := setInterfaceIPv4(name, unix.SIOCSIFADDR, ip); err != nil {
		return err
	}
	if err := setInterfaceIPv4(name, unix.SIOCSIFNETMASK, net.IP(mask)); err != nil {
		return err
	}
	return setInterfaceUp(name)
}

func setInterfaceDown(name string) error {
	return updateInterfaceFlags(name, func(flags uint16) uint16 {
		return flags &^ unix.IFF_UP
	})
}

func setInterfaceUp(name string) error {
	return updateInterfaceFlags(name, func(flags uint16) uint16 {
		return flags | unix.IFF_UP | unix.IFF_RUNNING
	})
}

func updateInterfaceFlags(name string, update func(uint16) uint16) error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	req := ifreqName(name)
	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.SIOCGIFFLAGS), uintptr(unsafe.Pointer(&req[0]))); errno != 0 {
		return fmt.Errorf("get flags for %s: %w", name, errno)
	}

	flags := *(*uint16)(unsafe.Pointer(&req[unix.IFNAMSIZ]))
	flags = update(flags)
	*(*uint16)(unsafe.Pointer(&req[unix.IFNAMSIZ])) = flags

	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.SIOCSIFFLAGS), uintptr(unsafe.Pointer(&req[0]))); errno != 0 {
		return fmt.Errorf("set flags for %s: %w", name, errno)
	}
	return nil
}

func setInterfaceIPv4(name string, request uintptr, ip net.IP) error {
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM, 0)
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	req := ifreqName(name)
	addr := (*unix.RawSockaddrInet4)(unsafe.Pointer(&req[unix.IFNAMSIZ]))
	addr.Family = unix.AF_INET
	copy(addr.Addr[:], ip.To4())

	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), request, uintptr(unsafe.Pointer(&req[0]))); errno != 0 {
		return fmt.Errorf("configure %s: %w", name, errno)
	}
	return nil
}

func ifreqName(name string) [40]byte {
	var req [40]byte
	copy(req[:unix.IFNAMSIZ], []byte(name))
	return req
}
