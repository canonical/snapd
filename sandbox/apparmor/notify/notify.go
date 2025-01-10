// Package notify implements high-level notify interface to a subset of AppArmor features
package notify

import (
	"errors"
	"fmt"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var SysPath string

// SupportAvailable returns true if SysPath exists, indicating that apparmor
// prompting messages may be received from SysPath.
func SupportAvailable() bool {
	return osutil.FileExists(SysPath)
}

// RegisterFileDescriptor registers a notification socket using the given file
// descriptor. Attempts to use the latest notification protocol version which
// both snapd and the kernel support, and returns that version.
//
// If no protocol version is mutually supported, or some other error occurs,
// returns an error.
func RegisterFileDescriptor(fd uintptr) (Version, error) {
	unsupported := make(map[Version]bool)
	for {
		protocolVersion, ok := supportedProtocolVersion(unsupported)
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
		if _, err = Ioctl(fd, APPARMOR_NOTIF_SET_FILTER, ioctlBuf); err != nil {
			var ioctlErr *IoctlError
			if errors.As(err, &ioctlErr) {
				if ioctlErr.errno == unix.ENOTSUP || ioctlErr.errno == unix.EPROTONOSUPPORT {
					// TODO: only one of these errnos is correct, so limit to
					// correct one once confirming with JJ.
					unsupported[protocolVersion] = true
					continue
				}
			}
			return 0, err
		}
		return protocolVersion, nil
	}
}

func setupSysPath(newrootdir string) {
	SysPath = filepath.Join(newrootdir, "/sys/kernel/security/apparmor/.notify")
}

func init() {
	dirs.AddRootDirCallback(setupSysPath)
	setupSysPath(dirs.GlobalRootDir)
}
