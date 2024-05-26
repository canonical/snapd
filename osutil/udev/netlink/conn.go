package netlink

import (
	"os"
	"syscall"
	"time"

	"github.com/ddkwork/golibrary/mylog"
)

type Mode int

// Mode determines event source: kernel events or udev-processed events.
// See libudev/libudev-monitor.c.
const (
	KernelEvent Mode = 1
	// Events that are processed by udev - much richer, with more attributes (such as vendor info, serial numbers and more).
	UdevEvent Mode = 2
)

// Generic connection
type NetlinkConn struct {
	Fd   int
	Addr syscall.SockaddrNetlink
}

type UEventConn struct {
	NetlinkConn
}

// Connect allow to connect to system socket AF_NETLINK with family NETLINK_KOBJECT_UEVENT to
// catch events about block/char device
// see:
// - http://elixir.free-electrons.com/linux/v3.12/source/include/uapi/linux/netlink.h#L23
// - http://elixir.free-electrons.com/linux/v3.12/source/include/uapi/linux/socket.h#L11
func (c *UEventConn) Connect(mode Mode) (err error) {
	c.Fd := mylog.Check2(syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW, syscall.NETLINK_KOBJECT_UEVENT))

	c.Addr = syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Groups: uint32(mode),
	}
	mylog.Check(syscall.Bind(c.Fd, &c.Addr))

	return
}

// Close allow to close file descriptor and socket bound
func (c *UEventConn) Close() error {
	return syscall.Close(c.Fd)
}

// ReadMsg allow to read an entire uevent msg
func (c *UEventConn) ReadMsg() (msg []byte, err error) {
	var n int

	buf := make([]byte, os.Getpagesize())
	for {
		// Just read how many bytes are available in the socket
		n, _ := mylog.Check3(syscall.Recvfrom(c.Fd, buf, syscall.MSG_PEEK))

		// If all message could be store inside the buffer : break
		if n < len(buf) {
			break
		}

		// Increase size of buffer if not enough
		buf = make([]byte, len(buf)+os.Getpagesize())
	}

	// Now read complete data
	n, _ = mylog.Check3(syscall.Recvfrom(c.Fd, buf, 0))

	// Extract only real data from buffer and return that
	msg = buf[:n]

	return
}

// ReadMsg allow to read an entire uevent msg
func (c *UEventConn) ReadUEvent() (*UEvent, error) {
	msg := mylog.Check2(c.ReadMsg())

	return ParseUEvent(msg)
}

// Monitor run in background a worker to read netlink msg in loop and notify
// when msg receive inside a queue using channel.
// To be notified with only relevant message, use Matcher.
func (c *UEventConn) Monitor(queue chan UEvent, errors chan error, matcher Matcher) (stop func(stopTimeout time.Duration) (ok bool)) {
	if matcher != nil {
		mylog.Check(matcher.Compile())
	}

	quitting := make(chan struct{})
	quit := make(chan struct{})

	readableOrStop, stop1 := mylog.Check3(RawSockStopper(c.Fd))

	// c.Fd is set to non-blocking at this point

	stop = func(stopTimeout time.Duration) bool {
		close(quitting)
		stop1()
		select {
		case <-quit:
			return true
		case <-time.After(stopTimeout):
		}
		return false
	}

	go func() {
	EventReading:
		for {
			_ := mylog.Check2(readableOrStop())

			select {
			case <-quitting:
				close(quit)
				return
			default:
				uevent := mylog.Check2(c.ReadUEvent())
				// underlying file descriptor is
				// non-blocking here, be paranoid if
				// for some reason we get here after
				// readableOrStop but the read would
				// block anyway
				var errno syscall.Errno
				if errors.As(err, &errno) && errno.Temporary() {
					continue EventReading
				}

				if matcher != nil {
					if !matcher.Evaluate(*uevent) {
						continue // Drop uevent if not match
					}
				}

				queue <- *uevent
			}
		}
	}()
	return stop
}
