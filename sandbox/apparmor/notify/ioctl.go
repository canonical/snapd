package notify

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/osutil"
)

var ErrIoctlReturnInvalid = errors.New("IOCTL request returned invalid bufsize")

var doSyscall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
	return unix.Syscall(trap, a1, a2, a3)
}

type hexBuf []byte

// String returns a string representation of a hexBuf.
func (hb hexBuf) String() string {
	var buf bytes.Buffer
	for i, b := range hb {
		if i%16 == 0 && i > 0 {
			fmt.Fprintf(&buf, "\n")
		}
		fmt.Fprintf(&buf, "%#02x", b)
		if i != len(hb)-1 {
			fmt.Fprintf(&buf, ", ")
		}
	}
	return buf.String()
}

// IoctlRequestBuffer is a buffer with a constructor method which prepares a
// buffer to be passed into ioctl(2), along with other useful methods.
type IoctlRequestBuffer []byte

// NewIoctlRequestBuffer returns a new buffer for communication with the kernel.
//
// The buffer contains encoded information about its size and protocol version.
func NewIoctlRequestBuffer() IoctlRequestBuffer {
	bufSize := 0xFFFF
	buf := bytes.NewBuffer(make([]byte, 0, bufSize))
	header := MsgHeader{Version: 3, Length: uint16(bufSize)}
	order := arch.Endian()
	binary.Write(buf, order, &header)
	buf.Write(make([]byte, bufSize-buf.Len()))
	return IoctlRequestBuffer(buf.Bytes())
}

// Pointer returns a pointer to the first element of an IoctlRequestBuffer.
//
// This is intended to be used to simplify passing the buffer into ioctl(2).
func (b IoctlRequestBuffer) Pointer() uintptr {
	return uintptr(unsafe.Pointer(&b[0]))
}

var dumpIoctl bool = osutil.GetenvBool("SNAPD_DEBUG_DUMP_IOCTL")

// SetIoctlDump enables or disables dumping the return value and buffer from ioctl(2).
//
// Returns the previous ioctl dump value.
func SetIoctlDump(value bool) bool {
	prev := dumpIoctl
	dumpIoctl = value
	return prev
}

// Ioctl performs a ioctl(2) on the given file descriptor.
//
// Returns a []byte with the contents of buf.Bytes() after the syscall, set to
// the size returned by the syscall, indicating how many bytes were written.
// The size of buf.Bytes() is left unchanged, so buf may be re-used for future
// Ioctl calls.  If the ioctl syscall returns an error, the buffer contents are
// those filled by the syscall, but the size of the buffer is unchanged, and
// the complete buffer and error are returned.
func Ioctl(fd uintptr, req IoctlRequest, buf IoctlRequestBuffer) ([]byte, error) {
	var err error
	if dumpIoctl {
		log.Printf(">>> ioctl %v (%d bytes) ...\n", req, len(buf))
		log.Printf("%v\n", hexBuf(buf))
	}
	ret, _, errno := doSyscall(unix.SYS_IOCTL, fd, uintptr(req), buf.Pointer())
	size := int(ret)
	if dumpIoctl {
		log.Printf("<<< ioctl %v returns %d, errno: %v\n", req, size, errno)
		if size != -1 && size < len(buf) {
			log.Printf("%v\n", hexBuf(buf[:size]))
		}
	}
	if errno != 0 {
		return nil, fmt.Errorf("cannot perform IOCTL request %v: %v", req, unix.Errno(errno))
	}
	if size >= 0 && size <= len(buf) {
		buf = buf[:size]
	} else {
		err = ErrIoctlReturnInvalid
	}
	return buf, err
}

// IoctlRequest is the type of ioctl(2) request numbers used by apparmor .notify file.
type IoctlRequest uintptr

// Available ioctl(2) requests for .notify file.
// Those are not documented beyond the implementeation in the kernel.
const (
	APPARMOR_NOTIF_SET_FILTER  IoctlRequest = 0x4008F800
	APPARMOR_NOTIF_GET_FILTER  IoctlRequest = 0x8008F801
	APPARMOR_NOTIF_IS_ID_VALID IoctlRequest = 0x8008F803
	APPARMOR_NOTIF_RECV        IoctlRequest = 0xC008F804
	APPARMOR_NOTIF_SEND        IoctlRequest = 0xC008F805
)

// String returns the string representation of an IoctlRequest.
func (req IoctlRequest) String() string {
	switch req {
	case APPARMOR_NOTIF_SET_FILTER:
		return "APPARMOR_NOTIF_SET_FILTER"
	case APPARMOR_NOTIF_GET_FILTER:
		return "APPARMOR_NOTIF_GET_FILTER"
	case APPARMOR_NOTIF_IS_ID_VALID:
		return "APPARMOR_NOTIF_IS_ID_VALID"
	case APPARMOR_NOTIF_RECV:
		return "APPARMOR_NOTIF_RECV"
	case APPARMOR_NOTIF_SEND:
		return "APPARMOR_NOTIF_SEND"
	default:
		return fmt.Sprintf("IoctlRequest(%x)", uintptr(req))
	}
}
