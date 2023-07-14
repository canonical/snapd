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
	_, err := unix.Write(fd, msg)
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

	buf := make([]byte, len(msg))
	_, err = unix.Read(events[0].Fd, buf)
	c.Assert(err, IsNil)
	c.Assert(buf, DeepEquals, msg)

	err = e.Modify(listenerFd, epoll.Readable|epoll.Writable)
	c.Assert(err, IsNil)

	err = e.Deregister(listenerFd)
	c.Assert(err, IsNil)

	err = e.Close()
	c.Assert(err, IsNil)
}

func (*epollSuite) TestWriteBeforeWait(c *C) {
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

	msgs := [][]byte{
		[]byte("foo"),
		[]byte("bar"),
		[]byte("baz"),
	}

	for _, msg := range msgs {
		_, err = unix.Write(senderFd, msg)
		c.Assert(err, IsNil)
	}

	time.Sleep(time.Duration(1) * time.Second)

	for _, msg := range msgs {
		events, err := e.Wait()
		c.Assert(err, IsNil)
		c.Assert(len(events), Equals, 1) // multiple writes to same fd appear as one event per Wait

		c.Assert(events[0].Fd, Equals, listenerFd)
		buf := make([]byte, len(msg))
		_, err = unix.Read(events[0].Fd, buf)
		c.Assert(err, IsNil)
		c.Assert(buf, DeepEquals, msg)
	}

	err = e.Deregister(listenerFd)
	c.Assert(err, IsNil)

	err = e.Close()
	c.Assert(err, IsNil)
}

func (*epollSuite) TestRegisterMultiple(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	numSockets := 20

	socketRxFds := make([]int, 0, numSockets)
	socketTxFds := make([]int, 0, numSockets)

	msg1 := []byte("foo")
	msg2 := []byte("bar")

	for i := 0; i < numSockets; i++ {
		socketFds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		c.Assert(err, IsNil)

		listenerFd := socketFds[0]
		senderFd := socketFds[1]

		err = unix.SetNonblock(listenerFd, true)
		c.Assert(err, IsNil)

		err = e.Register(listenerFd, epoll.Readable)
		c.Assert(err, IsNil)

		_, err = unix.Write(senderFd, msg1)
		c.Assert(err, IsNil)

		socketRxFds = append(socketRxFds, listenerFd)
		socketTxFds = append(socketTxFds, senderFd)
	}

	for _, senderFd := range socketTxFds {
		_, err = unix.Write(senderFd, msg2)
		c.Assert(err, IsNil)
	}

	events, err := e.Wait()
	c.Assert(err, IsNil)
	c.Assert(len(events), Equals, len(socketRxFds))

	for i, listenerFd := range socketRxFds {
		buf := make([]byte, len(msg1))
		c.Assert(events[i].Fd, Equals, listenerFd)
		_, err = unix.Read(events[i].Fd, buf)
		c.Assert(err, IsNil)
		c.Assert(buf, DeepEquals, msg1)
	}

	for i, listenerFd := range socketRxFds {
		buf := make([]byte, len(msg2))
		c.Assert(events[i].Fd, Equals, listenerFd)
		_, err = unix.Read(events[i].Fd, buf)
		c.Assert(err, IsNil)
		c.Assert(buf, DeepEquals, msg2)
	}

	for i := 0; i < len(socketRxFds)/2; i++ {
		err = e.Deregister(socketRxFds[i])
		c.Assert(err, IsNil)
	}

	msg3 := []byte("baz")

	for _, senderFd := range socketTxFds {
		_, err = unix.Write(senderFd, msg3)
		c.Assert(err, IsNil)
	}

	events, err = e.Wait()
	c.Assert(err, IsNil)
	c.Assert(len(events), Equals, len(socketRxFds)/2)

	for i, listenerFd := range socketRxFds[len(socketRxFds)/2:] {
		buf := make([]byte, len(msg3))
		c.Assert(events[i].Fd, Equals, listenerFd)
		_, err = unix.Read(events[i].Fd, buf)
		c.Assert(err, IsNil)
		c.Assert(buf, DeepEquals, msg3)
	}

	err = e.Close()
	c.Assert(err, IsNil)
}
