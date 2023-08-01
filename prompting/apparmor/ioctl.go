package apparmor

import (
	"bytes"
	"fmt"
	"log"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/osutil"
)

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

var dumpIoctl bool = osutil.GetenvBool("SNAPD_DEBUG_DUMP_IOCTL")

// NotifyIoctl performs a ioctl(2) on the apparmor .notify file.
func NotifyIoctl(fd uintptr, req IoctlRequest, msg []byte) (int, error) {
	if dumpIoctl {
		log.Printf(">>> ioctl %v (%d bytes) ...\n", req, len(msg))
		log.Printf("%v\n", hexBuf(msg))
	}
	ret, _, errno := doSyscall(unix.SYS_IOCTL, fd, uintptr(req), uintptr(unsafe.Pointer(&msg[0])))
	if dumpIoctl {
		log.Printf("<<< ioctl %v returns %d, errno: %v\n", req, int(ret), errno)
		if int(ret) != -1 && int(ret) < len(msg) {
			log.Printf("%v\n", hexBuf(msg[:int(ret)]))
		}
	}
	if errno != 0 {
		return 0, fmt.Errorf("cannot perform IOCTL request %v: %v", req, unix.Errno(errno))
	}
	return int(ret), nil
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
