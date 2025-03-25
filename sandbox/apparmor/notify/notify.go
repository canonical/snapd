// Package notify implements high-level notify interface to a subset of AppArmor features
package notify

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

var (
	doIoctl = Ioctl

	// TODO: replace with binary.NativeEndian once we're at go 1.21+
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
			pendingCount, err = registerListenerID(fd, protocolVersion)
			if err != nil {
				if errors.Is(err, unix.EINVAL) {
					unsupported[protocolVersion] = true
					continue
				}
				return 0, 0, err
			}

			// Attempt to resend previously-sent requests
			if err := resendRequests(fd, protocolVersion); err != nil {
				// If REGISTER succeeded but RESEND failed, a real error
				// occurred, and it's not a problem with protocol version
				return 0, 0, err
			}
		}

		// Set filter on the listener
		if err := setFilterForListener(fd, protocolVersion); err != nil {
			if errors.Is(err, unix.EPROTONOSUPPORT) {
				unsupported[protocolVersion] = true
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
// saves it to disk. Returns the number of pending requests which were
// previously sent before the listener was re-registered.
func registerListenerID(fd uintptr, version ProtocolVersion) (pendingCount int, err error) {
	listenerID, ok := retrieveSavedListenerID()
	if !ok {
		// Listener ID not found, so request the new ID by setting the ID to 0.
		listenerID = 0
	}

	msg := MsgNotificationResend{
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

	// Return the number of pending previously-sent requests. This should be 0
	// if there was no saved listener ID, since this is a new listener.
	return int(msg.Pending), nil
}

// retrieveSavedListenerID returns the listener ID which is saved on disk, if
// one exists and can be read, and true if so.
func retrieveSavedListenerID() (id uint64, ok bool) {
	f, err := os.Open(listenerIDFilepath())
	if err != nil {
		return 0, false
	}
	if err = binary.Read(f, nativeByteOrder, &id); err != nil {
		return 0, false
	}
	return id, true
}

// listenerIDFilepath returns the filepath at which the listener ID should be
// saved.
func listenerIDFilepath() string {
	return filepath.Join(dirs.SnapInterfacesRequestsRunDir, "listener-id")
}

// saveListenerID writes the given listener ID to disk.
func saveListenerID(id uint64) error {
	buf := bytes.NewBuffer(make([]byte, 0, binary.Size(id)))
	if err := binary.Write(buf, nativeByteOrder, id); err != nil {
		return err
	}
	if err := os.MkdirAll(dirs.SnapInterfacesRequestsRunDir, 0o755); err != nil {
		return err
	}
	return osutil.AtomicWriteFile(listenerIDFilepath(), buf.Bytes(), 0o600, 0)
}

// resendRequests tells the kernel to resend all pending requests previously
// sent by the listener associated with the given notify file descriptor.
//
// XXX: Is there a race if a new request is sent between when the listener is
// re-registered and when the resend command is issued? That new request could
// not have been read yet, but would it be sent twice, once without the
// NOTIF_RESENT flag and once with?
func resendRequests(fd uintptr, version ProtocolVersion) error {
	// TODO: at the moment, the spec does not specify any input or output
	// messages for the APPARMOR_NOTIF_RESEND command, though it does suggest
	// that apparmor_notif_reclaim may be sent as output in the future.
	ioctlBuf := NewIoctlRequestBuffer(version)
	if _, err := doIoctl(fd, APPARMOR_NOTIF_RESEND, ioctlBuf); err != nil {
		return err
	}
	return nil
}

// setFilterForListener sets a filter on the listener corresponding to the
// given file descriptor, using the given protocol version.
//
// TODO: do we want to avoid re-setting an identical filter with an identical
// protocol version on a reclaimed listener which already had that filter?
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
