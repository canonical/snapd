package netlink

import (
	"fmt"
	"math/bits"
	"os"
	"syscall"
)

// RawSockStopper returns a pair of functions to manage stopping code
// reading from a raw socket, readableOrStop blocks until
// fd is readable or stop was called. To work properly it sets fd
// to non-blocking mode.
// TODO: with go 1.11+ it should be possible to just switch to setting
// fd to non-blocking and then wrapping the socket via os.NewFile and
// use Close to force a read to stop.
// c.f. https://github.com/golang/go/commit/ea5825b0b64e1a017a76eac0ad734e11ff557c8e
func RawSockStopper(fd int) (readableOrStop func() (bool, error), stop func(), err error) {
	if err := syscall.SetNonblock(fd, true); err != nil {
		return nil, nil, err
	}

	stopR, stopW, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}

	// both stopR and stopW must be kept alive otherwise the corresponding
	// file descriptors will get closed
	readableOrStop = func() (bool, error) {
		return stopperSelectReadable(fd, int(stopR.Fd()))
	}
	stop = func() {
		stopW.Write([]byte{0})
	}
	return readableOrStop, stop, nil
}

func stopperSelectReadable(fd, stopFd int) (bool, error) {
	maxFd := fd
	if maxFd < stopFd {
		maxFd = stopFd
	}
	if maxFd >= 1024 {
		return false, fmt.Errorf("fd too high for syscall.Select")
	}
	fdIdx := fd / bits.UintSize
	fdShift := uint(fd) % bits.UintSize
	stopFdIdx := stopFd / bits.UintSize
	stopFdShift := uint(stopFd) % bits.UintSize
	readable := false
	tout := stopperSelectTimeout()
	for {
		var r syscall.FdSet
		r.Bits[fdIdx] = 1 << fdShift
		r.Bits[stopFdIdx] |= 1 << stopFdShift
		_, err := syscall.Select(maxFd+1, &r, nil, nil, tout)
		if errno, ok := err.(syscall.Errno); ok && errno.Temporary() {
			continue
		}
		if err != nil {
			return false, err
		}
		readable = (r.Bits[fdIdx] & (1 << fdShift)) != 0
		break
	}
	return readable, nil
}
