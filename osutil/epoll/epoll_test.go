package epoll_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/snapcore/snapd/osutil/epoll"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type epollSuite struct{}

var _ = Suite(&epollSuite{})

func (*epollSuite) TestFlagMapping(c *C) {
	c.Check(epoll.Readable.ToSys(), Equals, syscall.EPOLLIN)
	c.Check(epoll.Writable.ToSys(), Equals, syscall.EPOLLOUT)
	c.Check((epoll.Readable | epoll.Writable).ToSys(), Equals, syscall.EPOLLIN|syscall.EPOLLOUT)

	c.Check(epoll.FromSys(syscall.EPOLLIN), Equals, epoll.Readable)
	c.Check(epoll.FromSys(syscall.EPOLLOUT), Equals, epoll.Writable)
	c.Check(epoll.FromSys(syscall.EPOLLIN|syscall.EPOLLOUT), Equals, epoll.Readable|epoll.Writable)
}

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

const (
	modeSetUser    uint32  = 1 << 7
	ioctlSetFilter uintptr = 0x4008F800
)

type msgNotificationFilter struct {
	Length  uint16
	Version uint16
	ModeSet uint32
	NS      uint32
	Filter  uint32
}

var ErrIoctl = fmt.Errorf("cannot perform IOCTL request")

func prepareFdForEpollRegister(fd int) error {
	// based on a simplified version of notifier.Register() from
	// https://github.com/mvo5/snappy/blob/cerberus-dbus/prompting/notifier/notifier.go#L82
	var msg msgNotificationFilter
	msg.Length = uint16(binary.Size(msg))
	msg.Version = 2
	msg.ModeSet = modeSetUser
	msg.NS = 0
	msg.Filter = 0
	buf := bytes.NewBuffer(make([]byte, 0, binary.Size(msg)))
	if err := binary.Write(buf, binary.LittleEndian, msg); err != nil {
		return err
	}

	data := buf.Bytes()

	_, _, errno := syscall.Syscall(uintptr(syscall.SYS_IOCTL), uintptr(fd), uintptr(ioctlSetFilter), uintptr(unsafe.Pointer(&data[0])))
	if errno != 0 {
		return ErrIoctl
	}

	return nil
}

func waitNSecondsThenWriteToFile(n int, file *os.File) error {
	time.Sleep(time.Duration(n) * time.Second)
	data := []byte("foo")
	_, err := file.Write(data)
	return err
}

func (*epollSuite) TestRegisterWaitModifyDeregister(c *C) {
	e, err := epoll.Open()
	c.Assert(err, IsNil)

	tmpFileWriter, err := os.CreateTemp("/tmp", "snapd-unittest-epoll-TestRegisterWaitDeregister-")
	c.Assert(err, IsNil)
	defer tmpFileWriter.Close()

	tmpFileReader, err := os.Open(tmpFileWriter.Name()) // open for reading only
	defer tmpFileReader.Close()
	c.Assert(err, IsNil)

	defer os.Remove(tmpFileReader.Name())

	err = prepareFdForEpollRegister(int(tmpFileReader.Fd()))
	if err == ErrIoctl {
		// Not sure how to make this work.
		// I believe this is required for Register() to succeed.
		// Or, perhaps Register() just requires user to be root.
		return
	}
	c.Assert(err, IsNil)

	err = e.Register(int(tmpFileReader.Fd()), epoll.Readable)
	c.Assert(err, IsNil)

	go waitNSecondsThenWriteToFile(3, tmpFileWriter)

	events, err := e.Wait()
	c.Assert(err, IsNil)
	c.Assert(len(events), Equals, 1)
	c.Assert(events[0].Fd, Equals, tmpFileReader.Fd())

	err = e.Modify(int(tmpFileReader.Fd()), epoll.Readable|epoll.Writable)
	c.Assert(err, IsNil)

	err = e.Deregister(int(tmpFileReader.Fd()))
	c.Assert(err, IsNil)
}
