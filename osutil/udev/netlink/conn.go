package netlink

import (
	"fmt"
	"os"
	"syscall"
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
	buf  []byte
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
		Pid:    uint32(os.Getpid()),
	}

	if err = syscall.Bind(c.Fd, &c.Addr); err != nil {
		syscall.Close(c.Fd)
	}

	// this must be rather large for some events, see the reference implementation from systemd:
	// https://github.com/systemd/systemd/blob/0d92a3088a50212f16bf72672832b2b61dfca551/src/udev/udevadm-monitor.c#L73
	c.buf = make([]byte, 128*1024*1024)

	return
}

// Close allow to close file descriptor and socket bound
func (c *UEventConn) Close() error {
	return syscall.Close(c.Fd)
}

// ReadMsg allow to read an entire uevent msg
func (c *UEventConn) ReadMsg() (msg []byte, err error) {
	var n int

	// Read complete data
	n, _, err = syscall.Recvfrom(c.Fd, c.buf, 0)
	if err != nil {
		return
	}

	// Extract only real data from buffer and return that
	msg = c.buf[:n]

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
func (c *UEventConn) Monitor(queue chan UEvent, errors chan error, matcher Matcher) chan struct{} {
	quit := make(chan struct{}, 1)

	if matcher != nil {
		if err := matcher.Compile(); err != nil {
			errors <- fmt.Errorf("Wrong matcher, err: %v", err)
			quit <- struct{}{}
			return quit
		}
	}

	go func() {
		for {
			select {
			case <-quit:
				return
			default:
				uevent, err := c.ReadUEvent()
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
	return quit
}
