// Package notify implements high-level notify interface to a subset of AppArmor features
package notify

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/arch"
)

var (
	doIoctl = Ioctl

	// TODO: replace with binary.NativeEndian once we're at go 1.21+
	nativeByteOrder = arch.Endian() // ioctl messages are native byte order
)

// RegisterFileDescriptor registers a notification socket using the given file
// descriptor. Attempts to use the latest notification protocol version which
// both snapd and the kernel support, and returns that version.
//
// If no protocol version is mutually supported, or some other error occurs,
// returns an error.
func RegisterFileDescriptor(fd uintptr) (ProtocolVersion, error) {
	unsupported := make(map[ProtocolVersion]bool)
	for {
		protocolVersion, ok := likelySupportedProtocolVersion(unsupported)
		if !ok {
			return 0, fmt.Errorf("cannot register notify socket: no mutually supported protocol versions")
		}
		msg := MsgNotificationFilter{
			MsgHeader: MsgHeader{
				Version: protocolVersion,
			},
			ModeSet: APPARMOR_MODESET_USER,
		}
		data, err := msg.MarshalBinary()
		if err != nil {
			return 0, err
		}
		ioctlBuf := IoctlRequestBuffer(data)
		if _, err = doIoctl(fd, APPARMOR_NOTIF_SET_FILTER, ioctlBuf); err != nil {
			if errors.Is(err, unix.EPROTONOSUPPORT) {
				unsupported[protocolVersion] = true
				continue
			}
			return 0, err
		}
		return protocolVersion, nil
	}
}
