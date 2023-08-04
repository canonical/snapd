package prompting_test

import (
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/sandbox/apparmor/prompting"
	"golang.org/x/sys/unix"
)

type ioctlSuite struct{}

var _ = Suite(&ioctlSuite{})

func (*messageSuite) TestIoctlRequestBuffer(c *C) {
	buf := prompting.NewIoctlRequestBuffer()
	c.Assert(buf.Bytes(), HasLen, 0xFFFF)
	var header prompting.MsgHeader
	err := header.UnmarshalBinary(buf.Bytes())
	c.Assert(err, IsNil)
	c.Check(header, Equals, prompting.MsgHeader{
		Length:  0xFFFF,
		Version: 2,
	})
}

func (*ioctlSuite) TestIoctlHappy(c *C) {
	fd := uintptr(123)
	req := prompting.IoctlRequest(456)
	buf := prompting.NewIoctlRequestBuffer()
	restore := prompting.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
			c.Check(trap, Equals, uintptr(unix.SYS_IOCTL))
			c.Check(a1, Equals, fd)
			c.Check(a2, Equals, uintptr(req))
			c.Check(a3, Equals, buf.Pointer())
			return uintptr(buf.Len()), 0, 0
		})
	defer restore()
	n, err := prompting.NotifyIoctl(fd, req, buf)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, buf.Len())
}

func (*ioctlSuite) TestReceiveApparmorMessage(c *C) {
	fd := uintptr(123)
	req := prompting.APPARMOR_NOTIF_RECV
	restore := prompting.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
			c.Check(trap, Equals, uintptr(unix.SYS_IOCTL))
			c.Check(a1, Equals, fd)
			c.Check(a2, Equals, uintptr(req))
			return 0, 0, 0
		})
	defer restore()
	buf, err := prompting.ReceiveApparmorMessage(fd)
	c.Assert(err, IsNil)
	preparedBuf := prompting.NewIoctlRequestBuffer()
	buf = buf[:preparedBuf.Len()]
	c.Assert(buf, DeepEquals, preparedBuf.Bytes())
}

func (*ioctlSuite) TestIoctlReturnValueSizeMismatch(c *C) {
	fd := uintptr(123)
	req := prompting.IoctlRequest(456)
	buf := prompting.NewIoctlRequestBuffer()
	restore := prompting.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
			// Return different size.
			return uintptr(buf.Len() * 2), 0, 0
		})
	defer restore()
	n, err := prompting.NotifyIoctl(fd, req, buf)
	c.Assert(err, Equals, prompting.ErrIoctlReturnInvalid)
	c.Assert(n, Equals, buf.Len()*2)
}

func (*ioctlSuite) TestIoctlString(c *C) {
	c.Assert(prompting.APPARMOR_NOTIF_SET_FILTER.String(), Equals, "APPARMOR_NOTIF_SET_FILTER")
	c.Assert(prompting.APPARMOR_NOTIF_GET_FILTER.String(), Equals, "APPARMOR_NOTIF_GET_FILTER")
	c.Assert(prompting.APPARMOR_NOTIF_IS_ID_VALID.String(), Equals, "APPARMOR_NOTIF_IS_ID_VALID")
	c.Assert(prompting.APPARMOR_NOTIF_RECV.String(), Equals, "APPARMOR_NOTIF_RECV")
	c.Assert(prompting.APPARMOR_NOTIF_SEND.String(), Equals, "APPARMOR_NOTIF_SEND")

	arbitrary := prompting.IoctlRequest(0xDEADBEEF)
	c.Assert(arbitrary.String(), Equals, "IoctlRequest(deadbeef)")
}
