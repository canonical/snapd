package epoll_test

import (
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/osutil/epoll"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type epollSuite struct{}

var _ = Suite(&epollSuite{})

func (*epollSuite) TestString(c *C) {
	c.Check(epoll.Readable.String(), Equals, "Readable")
	c.Check(epoll.Writable.String(), Equals, "Writable")
	c.Check(epoll.Readiness(epoll.Readable|epoll.Writable).String(), Equals, "Readable|Writable")
}

func (*epollSuite) TestOpenClose(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)
	c.Assert(e.Fd() == -1, Equals, false)
	c.Assert(e.Fd() == 0, Equals, false)
	c.Assert(e.Fd() == 1, Equals, false)
	c.Assert(e.Fd() == 2, Equals, false)

	err = e.Close()
	c.Assert(err, IsNil)
	c.Assert(e.Fd(), Equals, -1)
}

func waitNSecondsThenWriteToFile(n int, fd int, msg []byte) error {
	time.Sleep(time.Duration(n) * time.Second)
	data := []byte("foo")
	_, err := unix.Write(fd, data)
	return err
}

func (*epollSuite) TestRegisterWaitModifyDeregister(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	socketFds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	c.Assert(err, IsNil)

	listenerFd := socketFds[0]
	senderFd := socketFds[1]

	err = unix.SetNonblock(listenerFd, true)
	c.Assert(err, IsNil)

	err = e.Register(listenerFd, epoll.Readable)
	c.Assert(err, IsNil)

	msg := []byte("foo")

	go waitNSecondsThenWriteToFile(1, senderFd, msg)

	events, err := e.Wait()
	c.Assert(err, IsNil)
	c.Assert(len(events), Equals, 1)
	c.Assert(events[0].Fd, Equals, listenerFd)

	buf := make([]byte, len("foo"))
	_, err = unix.Read(events[0].Fd, buf)
	c.Assert(err, IsNil)
	c.Assert(buf, DeepEquals, msg)

	err = e.Modify(listenerFd, epoll.Readable|epoll.Writable)
	c.Assert(err, IsNil)

	err = e.Deregister(listenerFd)
	c.Assert(err, IsNil)
}
