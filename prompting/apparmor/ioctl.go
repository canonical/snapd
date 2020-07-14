package apparmor

import (
	"bytes"
	"fmt"
	"syscall"
	"unsafe"

	"golang.org/x/xerrors"
)

var doSyscall = func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno) {
	return syscall.Syscall(trap, a1, a2, a3)
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

// NotifyIoctl performs a inoctl(2) on the apparmor .notify file.
func NotifyIoctl(fd uintptr, req IoctlRequest, msg []byte) (int, error) {
	// log.Printf(">>> ioctl %#x (%d bytes) ...\n", req, len(msg))
	// log.Printf("%v\n", hexBuf(msg))
	ret, _, errno := doSyscall(syscall.SYS_IOCTL, fd, uintptr(req), uintptr(unsafe.Pointer(&msg[0])))
	// log.Printf("<<< ioctl %#x returns %d, errno: %v\n", req, int(ret), errno)
	// if int(ret) != -1 && int(ret) < len(msg) {
	//	log.Printf("%v\n", hexBuf(msg[:int(ret)]))
	//}
	if errno != 0 {
		return 0, xerrors.Errorf("cannot perform IOCTL request %v: %v", req, syscall.Errno(errno))
	}
	return int(ret), nil
}

// IoctlRequest is the type of ioctl(2) request numbers used by apparmor .notify file.
type IoctlRequest uintptr

// Available ioctl(2) requests for .notify file.
// Those are not documented beyond the implementeation in the kernel.
const (
	// IoctlSetFilter is the ioctl request for APPARMOR_NOTIF_SET_FILTER.
	IoctlSetFilter IoctlRequest = 0x4008F800
	// IoctlGetFilter is the ioctl request for APPARMOR_NOTIF_GET_FILTER.
	IoctlGetFilter IoctlRequest = 0x8008F801
	// IoctlIsIDValid is the ioctl request for APPARMOR_NOTIF_IS_ID_VALID.
	IoctlIsIDValid IoctlRequest = 0x8008F803
	// IoctlReceive is the ioctl request for APPARMOR_NOTIF_RECV.
	IoctlReceive IoctlRequest = 0xC008F804
	// IoctlSend is the ioctl request for APPARMOR_NOTIF_SEND.
	IoctlSend IoctlRequest = 0xC008F805
)

func (req IoctlRequest) String() string {
	switch req {
	case IoctlSetFilter:
		return "IoctlSetFilter"
	case IoctlGetFilter:
		return "IoctlGetFilter"
	case IoctlIsIDValid:
		return "IoctlIsIDValid"
	case IoctlReceive:
		return "IoctlReceive"
	case IoctlSend:
		return "IoctlSend"
	default:
		return fmt.Sprintf("IoctlRequest(%x)", uintptr(req))
	}
}
