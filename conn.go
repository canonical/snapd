package main

import (
	"os"
	"syscall"
)

type NetlinkConn struct {
	Fd   int
	Addr syscall.SockaddrNetlink
}

type UEventConn struct {
	NetlinkConn
}

// see: http://elixir.free-electrons.com/linux/v3.12/source/include/uapi/linux/netlink.h#L23
// and see: http://elixir.free-electrons.com/linux/v3.12/source/include/uapi/linux/socket.h#L11
func (c *UEventConn) Connect() (err error) {

	if c.Fd, err = syscall.Socket(syscall.AF_NETLINK, syscall.SOCK_RAW, syscall.NETLINK_KOBJECT_UEVENT); err != nil {
		return
	}

	c.Addr = syscall.SockaddrNetlink{
		Family: syscall.AF_NETLINK,
		Groups: 1, // TODO: demistify this field because msg receive if Groups != 0
		Pid:    uint32(os.Getpid()),
	}

	if err = syscall.Bind(c.Fd, &c.Addr); err != nil {
		syscall.Close(c.Fd)
	}

	return
}

func (c *UEventConn) ReadMsg() (msg []byte, err error) {
	var n int

	b := make([]byte, os.Getpagesize())
	for {
		// Peek at the buffer to see how many bytes are available.
		if n, _, err = syscall.Recvfrom(c.Fd, b, syscall.MSG_PEEK); err != nil {
			return
		}

		// Break when we can read all messages.
		if n < len(b) {
			break
		}

		// Double in size if not enough bytes.
		b = make([]byte, len(b)*2)
	}

	// Now read complete data
	n, _, _ = syscall.Recvfrom(c.Fd, b, 0)

	// Extract only real data from buffer
	msg = b[:n]

	return
}
