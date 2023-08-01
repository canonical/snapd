package apparmor_test

import (
	"unsafe"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/prompting/apparmor"
	"golang.org/x/sys/unix"
)

type ioctlSuite struct{}

var _ = Suite(&ioctlSuite{})

func (*ioctlSuite) TestIoctlHappy(c *C) {
	fd := uintptr(123)
	req := apparmor.IoctlRequest(456)
	buf := []byte{1, 2, 3}
	restore := apparmor.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
			c.Check(trap, Equals, uintptr(unix.SYS_IOCTL))
			c.Check(a1, Equals, fd)
			c.Check(a2, Equals, uintptr(req))
			c.Check(a3, Equals, uintptr(unsafe.Pointer(&buf[0])))
			return uintptr(len(buf)), 0, 0
		})
	defer restore()
	n, err := apparmor.NotifyIoctl(fd, req, buf)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, len(buf))
}

func (*ioctlSuite) TestIoctlReturnValueSizeMismatch(c *C) {
	fd := uintptr(123)
	req := apparmor.IoctlRequest(456)
	buf := []byte{1, 2, 3}
	restore := apparmor.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
			// Return different size.
			return uintptr(len(buf) * 2), 0, 0
		})
	defer restore()
	n, err := apparmor.NotifyIoctl(fd, req, buf)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, len(buf)*2)
}

func (*ioctlSuite) TestIoctlString(c *C) {
	c.Assert(apparmor.APPARMOR_NOTIF_SET_FILTER.String(), Equals, "APPARMOR_NOTIF_SET_FILTER")
	c.Assert(apparmor.APPARMOR_NOTIF_GET_FILTER.String(), Equals, "APPARMOR_NOTIF_GET_FILTER")
	c.Assert(apparmor.APPARMOR_NOTIF_IS_ID_VALID.String(), Equals, "APPARMOR_NOTIF_IS_ID_VALID")
	c.Assert(apparmor.APPARMOR_NOTIF_RECV.String(), Equals, "APPARMOR_NOTIF_RECV")
	c.Assert(apparmor.APPARMOR_NOTIF_SEND.String(), Equals, "APPARMOR_NOTIF_SEND")

	arbitrary := apparmor.IoctlRequest(0xDEADBEEF)
	c.Assert(arbitrary.String(), Equals, "IoctlRequest(deadbeef)")
}
