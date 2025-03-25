package notify_test

import (
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) { TestingT(t) }

type notifySuite struct {
	testutil.BaseTest
}

var _ = Suite(&notifySuite{})

func (s *notifySuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })
}

var fakeNotifyVersions = []notify.VersionAndCheck{
	{
		Version: 11,
		Check:   func() bool { return false },
	},
	{
		Version: 7,
		Check:   func() bool { return true },
	},
	{
		Version: 5,
		Check:   func() bool { return false },
	},
	{
		Version: 3,
		Check:   func() bool { return true },
	},
	{
		Version: 2,
		Check:   func() bool { return false },
	},
}

func (s *notifySuite) TestRegisterFileDescriptor(c *C) {
	restoreVersions := notify.MockVersionLikelySupportedChecks(fakeNotifyVersions)
	defer restoreVersions()

	var fakeFD uintptr = 1234

	ioctlCalls := 0
	restoreSyscall := notify.MockIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(fd, Equals, fakeFD)

		ioctlCalls++

		// First expect check for version 7, then for version 3
		switch ioctlCalls {
		case 1:
			// v7 APPARMOR_NOTIF_REGISTER
			c.Check(req, Equals, notify.APPARMOR_NOTIF_REGISTER)
			// Expect listener ID 0, set some arbitrary values
			respBuf := checkIoctlBufferRegister(c, buf, notify.ProtocolVersion(7), 0, 123, 456, 789)
			return respBuf, nil
		case 2:
			// v7 APPARMOR_NOTIF_RESEND
			c.Check(req, Equals, notify.APPARMOR_NOTIF_RESEND)
			respBuf := checkIoctlBufferResend(c, buf, notify.ProtocolVersion(7))
			return respBuf, nil
		case 3:
			// v7 APPARMOR_NOTIF_SET_FILTER
			c.Check(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)
			respBuf := checkIoctlBufferSetFilter(c, buf, notify.ProtocolVersion(7))
			// Here we return error, so that we're forced to try again with v3.
			return respBuf, fmt.Errorf("cannot perform IOCTL request %v: %w (%s)", req, unix.EPROTONOSUPPORT, unix.ErrnoName(unix.EPROTONOSUPPORT))
		case 4:
			// v3 APPARNOR_NOTIF_SET_FILTER (v3 doesn't support reregistration)
			respBuf := checkIoctlBufferSetFilter(c, buf, notify.ProtocolVersion(3))
			return respBuf, nil
		default:
			c.Fatal("called Ioctl more than twice")
			return buf, nil
		}
	})
	defer restoreSyscall()

	receivedVersion, pendingCount, err := notify.RegisterFileDescriptor(fakeFD)
	c.Check(err, IsNil)
	c.Check(receivedVersion, Equals, notify.ProtocolVersion(3))
	// Technically, if the protocol supports re-registration, it should always
	// support setting filter. We registered the notify FD with a listener
	// which set the pending count (though this would only really happen if we
	// were re-registering an existing listener with non-zero ID), and we never
	// retried re-registering it again with protocol version 3, since it
	// doesn't support (re-)registration, so the initial registration is still
	// valid, and the associated pendingCount is valid as well. Check that it
	// was returned correctly, though in practice we're testing an edge case
	// which leaks pendingCount.
	c.Check(pendingCount, Equals, 789)
}

func checkIoctlBufferRegister(c *C, receivedBuf notify.IoctlRequestBuffer, expectedVersion notify.ProtocolVersion, expectedListenerID, setListenerID uint64, ready uint32, pending uint32) []byte {
	expectedMsg := notify.MsgNotificationReclaim{
		MsgHeader: notify.MsgHeader{
			Version: expectedVersion,
		},
		KernelListenerID: expectedListenerID,
	}
	expectedBuf, err := expectedMsg.MarshalBinary()
	c.Assert(err, IsNil)
	ioctlBuf := notify.IoctlRequestBuffer(expectedBuf)

	c.Check(receivedBuf, DeepEquals, ioctlBuf, Commentf("received incorrect buffer on Ioctl call; expected: %+v", expectedMsg))

	responseMsg := notify.MsgNotificationReclaim{
		MsgHeader: notify.MsgHeader{
			Version: expectedVersion,
		},
		KernelListenerID: setListenerID,
		Ready:            ready,
		Pending:          pending,
	}
	responseBuf, err := responseMsg.MarshalBinary()
	c.Assert(err, IsNil)
	return responseBuf
}

func checkIoctlBufferResend(c *C, receivedBuf notify.IoctlRequestBuffer, expectedVersion notify.ProtocolVersion) []byte {
	ioctlBuf := notify.NewIoctlRequestBuffer(expectedVersion)

	c.Check(receivedBuf, DeepEquals, ioctlBuf, Commentf("received incorrect buffer on Ioctl call; expected empty buffer with header version %d", expectedVersion))

	return receivedBuf
}

func checkIoctlBufferSetFilter(c *C, receivedBuf notify.IoctlRequestBuffer, expectedVersion notify.ProtocolVersion) []byte {
	expectedMsg := notify.MsgNotificationFilter{
		MsgHeader: notify.MsgHeader{
			Version: expectedVersion,
		},
		ModeSet: notify.APPARMOR_MODESET_USER,
	}
	expectedBuf, err := expectedMsg.MarshalBinary()
	c.Assert(err, IsNil)
	ioctlBuf := notify.IoctlRequestBuffer(expectedBuf)

	c.Check(receivedBuf, DeepEquals, ioctlBuf, Commentf("received incorrect buffer on Ioctl call; expected: %+v", expectedMsg))

	return receivedBuf
}

func (s *notifySuite) TestRegisterFileDescriptorErrors(c *C) {
	restoreVersions := notify.MockVersionLikelySupportedChecks(fakeNotifyVersions)
	defer restoreVersions()

	var fakeFD uintptr = 1234

	ioctlCalls := 0
	restoreSyscall := notify.MockIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(fd, Equals, fakeFD)

		ioctlCalls++

		// First expect check for version 7, then for version 3
		switch ioctlCalls {
		case 1:
			c.Check(req, Equals, notify.APPARMOR_NOTIF_REGISTER)
			// Expect listener ID 0, set arbitrary ID/ready/pending
			respBuf := checkIoctlBufferRegister(c, buf, notify.ProtocolVersion(7), 0, 123, 456, 789)
			// On v7, return an error on the APPARMOR_NOTIF_REGISTER
			return respBuf, fmt.Errorf("cannot perform IOCTL request %v: %w (%s)", req, unix.EINVAL, unix.ErrnoName(unix.EINVAL))
		case 2:
			c.Check(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)
			respBuf := checkIoctlBufferSetFilter(c, buf, notify.ProtocolVersion(3))
			return respBuf, fmt.Errorf("cannot perform IOCTL request %v: %w (%s)", req, unix.EPROTONOSUPPORT, unix.ErrnoName(unix.EPROTONOSUPPORT))
		default:
			c.Fatal("called Ioctl more than twice")
			return buf, fmt.Errorf("called Ioctl more than twice")
		}
	})
	defer restoreSyscall()

	receivedVersion, pendingCount, err := notify.RegisterFileDescriptor(fakeFD)
	c.Check(err, ErrorMatches, "cannot register notify socket: no mutually supported protocol versions")
	c.Check(receivedVersion, Equals, notify.ProtocolVersion(0))
	c.Check(pendingCount, Equals, 0)

	// A non-recoverable error occurs during REGISTER
	calledIoctl := false
	restoreSyscallError := notify.MockIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(fd, Equals, fakeFD)

		c.Assert(calledIoctl, Equals, false, Commentf("called ioctl more than once after first returned error"))
		calledIoctl = true

		c.Assert(req, Equals, notify.APPARMOR_NOTIF_REGISTER)
		// Expect listener ID 0, reply with arbitrary values
		respBuf := checkIoctlBufferRegister(c, buf, notify.ProtocolVersion(7), 0, 123, 456, 789)
		return respBuf, fmt.Errorf("cannot perform IOCTL request %v: %w (%s)", req, unix.EPERM, unix.ErrnoName(unix.EPERM))
	})
	defer restoreSyscallError()

	receivedVersion, pendingCount, err = notify.RegisterFileDescriptor(fakeFD)
	c.Check(err, ErrorMatches, `cannot perform IOCTL request APPARMOR_NOTIF_REGISTER: operation not permitted \(EPERM\)`)
	c.Check(receivedVersion, Equals, notify.ProtocolVersion(0))
	c.Check(pendingCount, Equals, 0)

	// REGISTER succeeds but a non-recoverable error occurs during RESEND
	ioctlCount := 0
	restoreSyscallError = notify.MockIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(fd, Equals, fakeFD)

		ioctlCount++

		switch ioctlCount {
		case 1:
			c.Assert(req, Equals, notify.APPARMOR_NOTIF_REGISTER)
			respBuf := checkIoctlBufferRegister(c, buf, notify.ProtocolVersion(7), 0, 123, 456, 789)
			// Since REGISTER succeeds, we actually save listener ID 123 to disk, so expect it next time
			return respBuf, nil
		case 2:
			// Throw an error on RESEND
			c.Assert(req, Equals, notify.APPARMOR_NOTIF_RESEND)
			respBuf := checkIoctlBufferResend(c, buf, notify.ProtocolVersion(7))
			return respBuf, fmt.Errorf("cannot perform IOCTL request %v: %w (%s)", req, unix.EINVAL, unix.ErrnoName(unix.EINVAL))
		default:
			c.Fatal("called Ioctl more than twice")
			return buf, nil
		}
	})
	defer restoreSyscallError()

	receivedVersion, pendingCount, err = notify.RegisterFileDescriptor(fakeFD)
	c.Check(err, ErrorMatches, `cannot perform IOCTL request APPARMOR_NOTIF_RESEND: invalid argument \(EINVAL\)`)
	c.Check(receivedVersion, Equals, notify.ProtocolVersion(0))
	c.Check(pendingCount, Equals, 0)

	// REGISTER and RESEND succeed but a non-recoverable error occurs during SET_FILTER
	ioctlCount = 0
	restoreSyscallError = notify.MockIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(fd, Equals, fakeFD)

		ioctlCount++

		switch ioctlCount {
		case 1:
			c.Assert(req, Equals, notify.APPARMOR_NOTIF_REGISTER)
			// The previous test successfully finished REGISTER, so listener ID
			// 123 was stored to disk. Expect it, and reply with the same ID.
			respBuf := checkIoctlBufferRegister(c, buf, notify.ProtocolVersion(7), 123, 123, 456, 789)
			return respBuf, nil
		case 2:
			c.Assert(req, Equals, notify.APPARMOR_NOTIF_RESEND)
			respBuf := checkIoctlBufferResend(c, buf, notify.ProtocolVersion(7))
			return respBuf, nil
		case 3:
			// Throw an error on SET_FILTER
			c.Assert(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)
			respBuf := checkIoctlBufferSetFilter(c, buf, notify.ProtocolVersion(7))
			return respBuf, fmt.Errorf("cannot perform IOCTL request %v: %w (%s)", req, unix.EINVAL, unix.ErrnoName(unix.EINVAL))
		default:
			c.Fatal("called Ioctl more than thrice")
			return buf, nil
		}
	})
	defer restoreSyscallError()

	receivedVersion, pendingCount, err = notify.RegisterFileDescriptor(fakeFD)
	c.Check(err, ErrorMatches, `cannot perform IOCTL request APPARMOR_NOTIF_SET_FILTER: invalid argument \(EINVAL\)`)
	c.Check(receivedVersion, Equals, notify.ProtocolVersion(0))
	c.Check(pendingCount, Equals, 0)
}
