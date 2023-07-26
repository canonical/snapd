package apparmor_test

import (
	"syscall"
	"unsafe"

	. "gopkg.in/check.v1"

	"github.com/snapcore/cerberus/apparmor"
)

type ioctlSuite struct{}

var _ = Suite(&ioctlSuite{})

func (*ioctlSuite) TestIoctlHappy(c *C) {
	fd := uintptr(123)
	req := apparmor.IoctlRequest(456)
	buf := []byte{1, 2, 3}
	restore := apparmor.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno) {
			c.Check(trap, Equals, uintptr(syscall.SYS_IOCTL))
			c.Check(a1, Equals, fd)
			c.Check(a2, Equals, uintptr(req))
			c.Check(a3, Equals, uintptr(unsafe.Pointer(&buf[0])))
			return uintptr(len(buf)), 0, 0
		})
	defer restore()
	_, err := apparmor.NotifyIoctl(fd, req, buf)
	c.Assert(err, IsNil)
}

func (*ioctlSuite) TestIoctlReturnValueSizeMismatch(c *C) {
	fd := uintptr(123)
	req := apparmor.IoctlRequest(456)
	buf := []byte{1, 2, 3}
	restore := apparmor.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err syscall.Errno) {
			// Return different size.
			return uintptr(len(buf) * 2), 0, 0
		})
	defer restore()
	_, err := apparmor.NotifyIoctl(fd, req, buf)
	c.Assert(err, IsNil)
}
