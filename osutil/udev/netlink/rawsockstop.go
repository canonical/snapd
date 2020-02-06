package netlink

import (
	"fmt"
	"os"
	"syscall"
)

// RawSockStopper returns a pair of functions to manage stopping code
// reading from a raw socket, readableOrStop blocks until
// fd is readable or stop was called.
// TODO: with go 1.11+ it should be possible to just switch to setting
// fd to non-blocking and then wrapping the socket via os.NewFile and
// use Closeq to force a read to stop.
// c.f. https://github.com/golang/go/commit/ea5825b0b64e1a017a76eac0ad734e11ff557c8e
func RawSockStopper(fd int) (readableOrStop func() (bool, error), stop func(), err error) {
	stopR, stopW, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	stopFd := int(stopR.Fd())

	readableOrStop = func() (bool, error) {
		return stopperSelectReadable(fd, stopFd)
	}
	stop = func() {
		stopW.Write([]byte{0})
	}
	return readableOrStop, stop, nil
}

var stopperSelectTimeout *syscall.Timeval

func stopperSelectReadable(fd, stopFd int) (bool, error) {
	maxFd := fd
	if maxFd < stopFd {
		maxFd = stopFd
	}
	if maxFd >= 1024 {
		return false, fmt.Errorf("fd too high for syscall.Select")
	}
	fdIdx := fd / 64
	fdBits := int64(1 << (uint(fd) % 64))
	readable := false
	for {
		var r syscall.FdSet
		r.Bits[fdIdx] = fdBits
		r.Bits[stopFd/64] |= 1 << (uint(stopFd) % 64)
		_, err := syscall.Select(maxFd+1, &r, nil, nil, stopperSelectTimeout)
		if err == syscall.EINTR {
			continue
		}
		if err != nil {
			return false, err
		}
		readable = (r.Bits[fdIdx] & fdBits) != 0
		break
	}
	return readable, nil
}
