package apparmor

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

func (hb hexBuf) String() string {
	var buf bytes.Buffer
	for i, b := range hb {
		fmt.Fprintf(&buf, "%#02x", b)
		if i != len(hb)-1 {
			fmt.Fprintf(&buf, ", ")
		}
		if i > 0 && (i+1)%16 == 0 {
			fmt.Fprintf(&buf, "\n")
		}
	}
	return buf.String()
}

type IoctlRequestBuffer struct {
	bytes []byte
}

// NewIoctlRequestBuffer returns a new buffer for communication with the kernel.
// The buffer contains encoded information about its size and protocol version.
func NewIoctlRequestBuffer() *IoctlRequestBuffer {
	bufSize := 0xFFFF
	buf := bytes.NewBuffer(make([]byte, 0, bufSize))
	header := MsgHeader{Version: 2, Length: uint16(bufSize)}
	order := arch.Endian()
	binary.Write(buf, order, &header)
	buf.Write(make([]byte, bufSize-buf.Len()))
	return &IoctlRequestBuffer{
		bytes: buf.Bytes(),
	}
}

func (b *IoctlRequestBuffer) Bytes() []byte {
	return b.bytes
}

func (b *IoctlRequestBuffer) Len() int {
	return len(b.bytes)
}

func (b *IoctlRequestBuffer) Pointer() uintptr {
	return uintptr(unsafe.Pointer(&b.bytes[0]))
}

var dumpIoctl bool = osutil.GetenvBool("SNAPD_DEBUG_DUMP_IOCTL")

// NotifyIoctl performs a ioctl(2) on the given file descriptor.
// Sets the length of buf.Bytes() to be equal to the return value of the
// syscall, indicating how many bytes were written to the buffer.
func NotifyIoctl(fd uintptr, req IoctlRequest, buf *IoctlRequestBuffer) (int, error) {
	var err error
	if dumpIoctl {
		log.Printf(">>> ioctl %v (%d bytes) ...\n", req, buf.Len())
		log.Printf("%v\n", hexBuf(buf.Bytes()))
	}
	ret, _, errno := doSyscall(unix.SYS_IOCTL, fd, uintptr(req), buf.Pointer())
	size := int(ret)
	if size >= 0 && size <= buf.Len() {
		buf.bytes = buf.bytes[:size]
	} else {
		err = ErrIoctlReturnInvalid
	}
	if dumpIoctl {
		log.Printf("<<< ioctl %v returns %d, errno: %v\n", req, size, errno)
		if size != -1 && size < buf.Len() {
			log.Printf("%v\n", hexBuf(buf.Bytes()))
		}
	}
	if errno != 0 {
		return 0, fmt.Errorf("cannot perform IOCTL request %v: %v", req, unix.Errno(errno))
	}
	return size, err
}

// ReceiveApparmorMessage uses ioctl(2) to receive a message from apparmor.
// The ioctl(2) syscall is performed on the given file descriptor.
// Returns a buffer containing the received message.
func ReceiveApparmorMessage(fd uintptr) ([]byte, error) {
	buf := NewIoctlRequestBuffer()
	_, err := NotifyIoctl(fd, APPARMOR_NOTIF_RECV, buf)
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
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
