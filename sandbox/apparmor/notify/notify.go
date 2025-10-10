// Package notify implements high-level notify interface to a subset of AppArmor features
package notify

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
)

var (
	doIoctl = Ioctl

	// TODO:GOVERSION: replace with binary.NativeEndian once we're at go 1.21+
	nativeByteOrder = arch.Endian() // ioctl messages are native byte order
)

// RegisterFileDescriptor registers a listener for and sets a filter on the
// given file descriptor, returning the protocol version which is negotiated
// with the kernel and the number of pending previously-sent requests, if any.
//
// Attempts to use the latest notification protocol version which both snapd
// and the kernel support.
//
// If the protocol version supports it, and there is a previously-registered
// listener ID saved, attempt to re-register the listener with that ID,
// otherwise ask the kernel for the ID of the new listener and save it to disk.
//
// Then, register a new filter on the listener.
//
// If no protocol version is mutually supported, or some other error occurs,
// returns an error.
func RegisterFileDescriptor(fd uintptr) (version ProtocolVersion, pendingCount int, err error) {
	unsupported := make(map[ProtocolVersion]bool)
	for {
		protocolVersion, ok := likelySupportedProtocolVersion(unsupported)
		if !ok {
			return 0, 0, fmt.Errorf("cannot register notify socket: no mutually supported protocol versions")
		}

		if protocolVersion >= 5 {
			// Attempt to register the listener ID before setting the filter
			listenerID, err := registerListenerID(fd, protocolVersion)
			if err != nil {
				if errors.Is(err, unix.EINVAL) {
					logger.Debugf("kernel returned EINVAL from APPARMOR_NOTIF_REGISTER with protocol version %d; marking version as unsupported and retrying", protocolVersion)
					unsupported[protocolVersion] = true
					continue
				} else if errors.Is(err, unix.ENOENT) {
					// Listener probably timed out in the kernel, so remove the
					// saved ID and retry registration
					logger.Debug("kernel returned ENOENT from APPARMOR_NOTIF_REGISTER (listener probably timed out); removing saved listener ID and retrying")
					if e := removeSavedListenerID(); e != nil {
						logger.Noticef("cannot remove saved listener ID: %v", e)
					}
					continue
				}
				return 0, 0, err
			}

			// Attempt to resend previously-sent requests
			if pendingCount, err = resendRequests(fd, protocolVersion, listenerID); err != nil {
				// If REGISTER succeeded but RESEND failed, a real error
				// occurred, and it's not a problem with protocol version
				return 0, 0, err
			}
		}

		// XXX: SET_FILTER doesn't create listener, opening the FD does.
		// If a filter is set on a listener before reregistering with a known
		// listener ID, the "new" listener (and filter we just sent) are
		// discarded, and the old listener and its filters are used.
		if err := setFilterForListener(fd, protocolVersion); err != nil {
			if errors.Is(err, unix.EPROTONOSUPPORT) {
				unsupported[protocolVersion] = true
				// XXX: pendingCount may still be set, if the current protocol
				// version supports registration but not setting filter. This
				// should never happen in the real world. If the next protocol
				// version we try supports setting filter but not registration,
				// then we will leak the pendingCount from this previous
				// version, though technically this registration and the
				// pendingCount are still valid, so this is... not incorrect.
				continue
			}
			return 0, 0, err
		}

		return protocolVersion, pendingCount, nil
	}
}

// registerListenerID checks whether there's a saved listener ID, and if so,
// attempts to register the given file descriptor with it. If not, or if a
// listener with the saved ID is not found, requests the new listener ID and
// saves it to disk. Returns the ID of the listener.
func registerListenerID(fd uintptr, version ProtocolVersion) (listenerID uint64, err error) {
	listenerID, ok := retrieveSavedListenerID()
	if !ok {
		// Listener ID not found, so request the new ID by setting the ID to 0.
		listenerID = 0
	}

	msg := MsgNotificationRegister{
		MsgHeader: MsgHeader{
			Version: version,
		},
		// If KernelListenerID is 0, the kernel will populate the struct in the
		// buffer with the ID the kernel has assigned to the new listener. If
		// it's not 0, we attempt to re-register the existing listener with the
		// given ID.
		KernelListenerID: listenerID,
	}
	data, err := msg.MarshalBinary()
	if err != nil {
		return 0, err
	}
	ioctlBuf := IoctlRequestBuffer(data)
	logger.Debugf("performing APPARMOR_NOTIF_REGISTER with listener ID set to %d", listenerID)
	buf, err := doIoctl(fd, APPARMOR_NOTIF_REGISTER, ioctlBuf)
	if err != nil {
		return 0, err
	}

	// Success, now get the listener ID and pending request count, which were
	// populated by the kernel. If we had the ID set to 0 in the register
	// command, the kernel should have populated the field with the listener ID.
	// Otherwise, it should be the same ID that we passed to the kernel.
	// Regardless, we want to save it to disk.
	if err = msg.UnmarshalBinary(buf); err != nil {
		return 0, err
	}

	// Now save the listener ID to disk so we can retrieve it later. It may be
	// the case that the subsequent set filter command fails, but there's no
	// harm in reclaiming a previous listener for which we had yet to set a
	// filter. This way the caller of this function doesn't have to worry about
	// the listener ID file.
	if err = saveListenerID(msg.KernelListenerID); err != nil {
		return 0, err
	}

	return msg.KernelListenerID, nil
}

// retrieveSavedListenerID returns the listener ID which is saved on disk, if
// one exists and can be read, and true if so.
func retrieveSavedListenerID() (id uint64, ok bool) {
	f, err := os.Open(listenerIDFilepath())
	if err != nil {
		return 0, false
	}
	defer f.Close()
	if err = binary.Read(f, nativeByteOrder, &id); err != nil {
		return 0, false
	}
	logger.Debugf("retrieved saved listener ID from disk: %d", id)
	return id, true
}

// listenerIDFilepath returns the filepath at which the listener ID should be
// saved.
func listenerIDFilepath() string {
	return filepath.Join(dirs.SnapInterfacesRequestsRunDir, "listener-id")
}

// saveListenerID writes the given listener ID to disk.
func saveListenerID(id uint64) error {
	var buf [8]byte
	nativeByteOrder.PutUint64(buf[:], id)
	if err := os.MkdirAll(dirs.SnapInterfacesRequestsRunDir, 0o755); err != nil {
		return err
	}
	logger.Debugf("saving listener ID to disk: %d", id)
	return osutil.AtomicWriteFile(listenerIDFilepath(), buf[:], 0o600, 0)
}

// removeSavedListenerID removes the file which stores the listener ID on disk.
func removeSavedListenerID() error {
	return os.Remove(listenerIDFilepath())
}

// resendRequests tells the kernel to resend all pending requests previously
// sent by the listener associated with the given notify file descriptor.
//
// Returns the number of previously-sent messages which were pending a response
// and have now been queued to be re-sent.
func resendRequests(fd uintptr, version ProtocolVersion, listenerID uint64) (pendingCount int, err error) {
	msg := MsgNotificationResend{
		MsgHeader: MsgHeader{
			Version: version,
		},
		KernelListenerID: listenerID,
		// Ready and Pending will be set by the kernel
	}
	data, err := msg.MarshalBinary()
	if err != nil {
		return 0, err
	}
	ioctlBuf := IoctlRequestBuffer(data)
	buf, err := doIoctl(fd, APPARMOR_NOTIF_RESEND, ioctlBuf)
	if err != nil {
		return 0, err
	}
	// Success, now extract the pending request count.
	if err = msg.UnmarshalBinary(buf); err != nil {
		return 0, err
	}
	return int(msg.Pending), nil
}

// setFilterForListener sets a filter on the listener corresponding to the
// given file descriptor, using the given protocol version.
//
// Setting a filter on a re-registered listener which already had a filter set
// should have no ill effects. The kernel supports multiple filters on a
// listener, and requests which maych more than one filter on a listener will
// still only be queued once for that listener.
func setFilterForListener(fd uintptr, version ProtocolVersion) error {
	msg := MsgNotificationFilter{
		MsgHeader: MsgHeader{
			Version: version,
		},
		ModeSet: APPARMOR_MODESET_USER,
	}
	data, err := msg.MarshalBinary()
	if err != nil {
		return err
	}
	ioctlBuf := IoctlRequestBuffer(data)
	if _, err = doIoctl(fd, APPARMOR_NOTIF_SET_FILTER, ioctlBuf); err != nil {
		return err
	}
	return nil
}
