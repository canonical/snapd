package notify_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"log"
	"unsafe"

	. "gopkg.in/check.v1"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
)

type ioctlSuite struct{}

var _ = Suite(&ioctlSuite{})

func (*messageSuite) TestIoctlRequestBuffer(c *C) {
	buf := notify.NewIoctlRequestBuffer()
	c.Assert(buf.Bytes(), HasLen, 0xFFFF)
	var header notify.MsgHeader
	err := header.UnmarshalBinary(buf.Bytes())
	c.Assert(err, IsNil)
	c.Check(header, Equals, notify.MsgHeader{
		Length:  0xFFFF,
		Version: 2,
	})
}

func (*ioctlSuite) TestIoctlHappy(c *C) {
	fd := uintptr(123)
	req := notify.IoctlRequest(456)
	buf := notify.NewIoctlRequestBuffer()
	restore := notify.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
			c.Check(trap, Equals, uintptr(unix.SYS_IOCTL))
			c.Check(a1, Equals, fd)
			c.Check(a2, Equals, uintptr(req))
			c.Check(a3, Equals, buf.Pointer())
			return uintptr(buf.Len()), 0, 0
		})
	defer restore()
	n, err := notify.Ioctl(fd, req, buf)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, buf.Len())
}

func (ioctlSuite) TestIoctlBuffer(c *C) {
	fd := uintptr(123)
	req := notify.IoctlRequest(456)
	buf := notify.NewIoctlRequestBuffer()
	bufAddr := uintptr(unsafe.Pointer(&buf.Bytes()[0]))

	contents := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x01, 0x23, 0x45, 0x67, 0x89, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}

	restore := notify.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
			c.Assert(a3, Equals, bufAddr)
			raw := buf.Bytes()

			for i, b := range contents {
				raw[i] = b
			}

			return (uintptr)(len(contents)), 0, 0
		})
	defer restore()

	n, err := notify.Ioctl(fd, req, buf)
	c.Assert(err, Equals, nil)
	c.Assert(n, Equals, len(contents))
	c.Assert(buf.Bytes(), DeepEquals, contents)
}

func (*ioctlSuite) TestReadMessage(c *C) {
	fd := uintptr(123)
	req := notify.APPARMOR_NOTIF_RECV
	restore := notify.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
			c.Check(trap, Equals, uintptr(unix.SYS_IOCTL))
			c.Check(a1, Equals, fd)
			c.Check(a2, Equals, uintptr(req))
			return 0, 0, 0
		})
	defer restore()
	buf, err := notify.ReadMessage(fd)
	c.Assert(err, IsNil)
	preparedBuf := notify.NewIoctlRequestBuffer()
	buf = buf[:preparedBuf.Len()]
	c.Assert(buf, DeepEquals, preparedBuf.Bytes())
}

func (*ioctlSuite) TestIoctlReturnValueSizeMismatch(c *C) {
	fd := uintptr(123)
	req := notify.IoctlRequest(456)
	buf := notify.NewIoctlRequestBuffer()
	restore := notify.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
			// Return different size.
			return uintptr(buf.Len() * 2), 0, 0
		})
	defer restore()
	n, err := notify.Ioctl(fd, req, buf)
	c.Assert(err, Equals, notify.ErrIoctlReturnInvalid)
	c.Assert(n, Equals, buf.Len()*2)
}

func (*ioctlSuite) TestIoctlReturnError(c *C) {
	fd := uintptr(123)
	req := notify.IoctlRequest(456)
	buf := notify.NewIoctlRequestBuffer()
	restore := notify.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
			// return size of -1
			var zero uintptr = 0
			return ^zero, 0, unix.EBADF
		})
	defer restore()
	n, err := notify.Ioctl(fd, req, buf)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot perform IOCTL request .*"))
	c.Assert(n, Equals, -1)
}

func (ioctlSuite) TestIoctlDump(c *C) {
	var logBuf bytes.Buffer
	origLog := log.Writer()
	log.SetOutput(&logBuf)
	defer log.SetOutput(origLog)

	origDump := notify.SetIoctlDump(true)
	defer notify.SetIoctlDump(origDump)

	fd := uintptr(123)
	req := notify.IoctlRequest(456)
	buf := notify.NewIoctlRequestBuffer()
	bufAddr := uintptr(unsafe.Pointer(&buf.Bytes()[0]))

	contents := []byte{0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x01, 0x23, 0x45, 0x67, 0x89, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff}
	contentsString := "0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x01, 0x23, 0x45, 0x67, 0x89, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, \n0xff"

	restore := notify.MockSyscall(
		func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
			c.Assert(a3, Equals, bufAddr)
			raw := buf.Bytes()

			for i, b := range contents {
				raw[i] = b
			}

			return (uintptr)(len(contents)), 0, 0
		})
	defer restore()

	sendHeader := fmt.Sprintf(">>> ioctl %v (%d bytes) ...\n", req, buf.Len())
	sendDataStr := "0xff, 0xff, 0x02, 0x00, "
	if arch.Endian() == binary.BigEndian {
		sendDataStr = "0xff, 0xff, 0x00, 0x02, "
	}

	n, err := notify.Ioctl(fd, req, buf)
	c.Assert(err, Equals, nil)
	c.Assert(n, Equals, len(contents))
	c.Assert(buf.Bytes(), HasLen, len(contents))
	c.Assert(buf.Bytes(), DeepEquals, contents)

	recvHeader := fmt.Sprintf("<<< ioctl %v returns %d, errno: %v\n", req, n, unix.Errno(0))

	logBufStr := logBuf.String()

	logTsLen := 20
	logBufStr = logBufStr[logTsLen:]

	// Check that each log component occurs in the log, in order.
	// Since there are timestamps between each message (and 0xFFFB arbitrary
	// bytes after the initial message header), can't construct and search for
	// a complete string.
	l := len(sendHeader)
	c.Assert(logBufStr[:l], Equals, sendHeader, Commentf("Next %d chars of buffer: `%s`", l, logBufStr[:l]))
	logBufStr = logBufStr[l+logTsLen:]
	l = len(sendDataStr)
	c.Assert(logBufStr[:l], Equals, sendDataStr, Commentf("Next %d chars of buffer: `%s`", l, logBufStr[:l]))
	// There should then be 0xFFFB bytes formatted via hexBuf.String().
	// Each byte is of the form "0xnn, ", with newlines every 16 bytes.
	// So, 0xFFF newlines, and 6 bytes per char otherwise, though the final
	// byte is lacking the trailing ", ", and there is a trailing "\n".
	otherBytesLen := 0xFFFB*6 + 0xFFF - 2 + 1
	logBufStr = logBufStr[l+otherBytesLen+logTsLen:]
	l = len(recvHeader)
	c.Assert(logBufStr[:l], Equals, recvHeader, Commentf("Next %d chars of buffer: `%s`", l, logBufStr[:l]))
	logBufStr = logBufStr[l+logTsLen:]
	l = len(contentsString)
	c.Assert(logBufStr[:l], Equals, contentsString, Commentf("Next %d chars of buffer: `%s`", l, logBufStr[:l]))
}

func (*ioctlSuite) TestIoctlString(c *C) {
	c.Assert(notify.APPARMOR_NOTIF_SET_FILTER.String(), Equals, "APPARMOR_NOTIF_SET_FILTER")
	c.Assert(notify.APPARMOR_NOTIF_GET_FILTER.String(), Equals, "APPARMOR_NOTIF_GET_FILTER")
	c.Assert(notify.APPARMOR_NOTIF_IS_ID_VALID.String(), Equals, "APPARMOR_NOTIF_IS_ID_VALID")
	c.Assert(notify.APPARMOR_NOTIF_RECV.String(), Equals, "APPARMOR_NOTIF_RECV")
	c.Assert(notify.APPARMOR_NOTIF_SEND.String(), Equals, "APPARMOR_NOTIF_SEND")

	arbitrary := notify.IoctlRequest(0xDEADBEEF)
	c.Assert(arbitrary.String(), Equals, "IoctlRequest(deadbeef)")
}
