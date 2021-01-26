package netlink

import (
	"fmt"
	"os"
	"syscall"
	"time"
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

	if c.Fd, err = syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW, syscall.NETLINK_KOBJECT_UEVENT); err != nil {
		return
	}

	c.Addr = syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Groups: uint32(mode),
	}

	if err = syscall.Bind(c.Fd, &c.Addr); err != nil {
		syscall.Close(c.Fd)
	}

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
		if n, _, err = syscall.Recvfrom(c.Fd, buf, syscall.MSG_PEEK); err != nil {
			return
		}

		// If all message could be store inside the buffer : break
		if n < len(buf) {
			break
		}

		// Increase size of buffer if not enough
		buf = make([]byte, len(buf)+os.Getpagesize())
	}

	// Now read complete data
	n, _, err = syscall.Recvfrom(c.Fd, buf, 0)
	if err != nil {
		return
	}

	// Extract only real data from buffer and return that
	msg = buf[:n]

	return
}

// ReadMsg allow to read an entire uevent msg
func (c *UEventConn) ReadUEvent() (*UEvent, error) {
	msg, err := c.ReadMsg()
	if err != nil {
		return nil, err
	}

	return ParseUEvent(msg)
}

// Monitor run in background a worker to read netlink msg in loop and notify
// when msg receive inside a queue using channel.
// To be notified with only relevant message, use Matcher.
func (c *UEventConn) Monitor(queue chan UEvent, errors chan error, matcher Matcher) (stop func(stopTimeout time.Duration) (ok bool)) {
	if matcher != nil {
		if err := matcher.Compile(); err != nil {
			errors <- fmt.Errorf("Wrong matcher, err: %v", err)
			return func(time.Duration) bool {
				return true
			}
		}
	}

	quitting := make(chan struct{})
	quit := make(chan struct{})

	readableOrStop, stop1, err := RawSockStopper(c.Fd)
	if err != nil {
		errors <- fmt.Errorf("Internal error: %v", err)
		return func(time.Duration) bool {
			return true
		}
	}
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
			_, err := readableOrStop()
			if err != nil {
				errors <- fmt.Errorf("Internal error: %v", err)
				return
			}
			select {
			case <-quitting:
				close(quit)
				return
			default:
				uevent, err := c.ReadUEvent()
				// underlying file descriptor is
				// non-blocking here, be paranoid if
				// for some reason we get here after
				// readableOrStop but the read would
				// block anyway
				if errno, ok := err.(syscall.Errno); ok && errno.Temporary() {
					continue EventReading
				}
				if err != nil {
					errors <- fmt.Errorf("Unable to parse uevent, err: %v", err)
					continue
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
