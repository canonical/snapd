package notify_test

import (
	"os"
	"path/filepath"
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

func (*notifySuite) TestSysPathBehavior(c *C) {
	newRoot := c.MkDir()
	newSysPath := filepath.Join(newRoot, "/sys/kernel/security/apparmor/.notify")
	dirs.SetRootDir(newRoot)
	c.Assert(notify.SysPath, Equals, newSysPath)
}

func (*notifySuite) TestSupportAvailable(c *C) {
	newRoot := c.MkDir()
	dirs.SetRootDir(newRoot)
	c.Assert(notify.SupportAvailable(), Equals, false)
	err := os.MkdirAll(filepath.Dir(notify.SysPath), 0755)
	c.Assert(err, IsNil)
	c.Assert(notify.SupportAvailable(), Equals, false)
	_, err = os.Create(notify.SysPath)
	c.Assert(err, IsNil)
	c.Assert(notify.SupportAvailable(), Equals, true)
}

var fakeNotifyVersions = []notify.VersionAndCallback{
	{
		Version:  2,
		Callback: func() bool { return false },
	},
	{
		Version:  3,
		Callback: func() bool { return true },
	},
	{
		Version:  5,
		Callback: func() bool { return false },
	},
	{
		Version:  7,
		Callback: func() bool { return true },
	},
	{
		Version:  11,
		Callback: func() bool { return false },
	},
}

func (s *notifySuite) TestRegisterFileDescriptor(c *C) {
	restoreVersions := notify.MockVersionSupportedCallbacks(fakeNotifyVersions)
	defer restoreVersions()

	var fakeFD uintptr = 1234

	ioctlCalls := 0
	restoreSyscall := notify.MockIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(fd, Equals, fakeFD)
		c.Assert(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)

		ioctlCalls++

		// First expect check for version 3, then for version 7
		switch ioctlCalls {
		case 1:
			checkIoctlBuffer(c, buf, notify.ProtocolVersion(3))
			return buf, &notify.IoctlError{req, unix.EPROTONOSUPPORT}
		case 2:
			checkIoctlBuffer(c, buf, notify.ProtocolVersion(7))
			return buf, nil
		default:
			c.Fatal("called Ioctl more than twice")
			return buf, nil
		}
	})
	defer restoreSyscall()

	receivedVersion, err := notify.RegisterFileDescriptor(fakeFD)
	c.Check(err, IsNil)
	c.Check(receivedVersion, Equals, notify.ProtocolVersion(7))
}

func checkIoctlBuffer(c *C, receivedBuf notify.IoctlRequestBuffer, expectedVersion notify.ProtocolVersion) {
	expectedMsg := notify.MsgNotificationFilter{
		MsgHeader: notify.MsgHeader{
			Version: expectedVersion,
		},
		ModeSet: notify.APPARMOR_MODESET_USER,
	}
	expectedBuf, err := expectedMsg.MarshalBinary()
	c.Assert(err, IsNil)
	ioctlBuf := notify.IoctlRequestBuffer(expectedBuf)

	c.Check(receivedBuf, DeepEquals, ioctlBuf, Commentf("received incorrect buffer on Ioctl call, which expected version %d", expectedVersion))
}

func (s *notifySuite) TestRegisterFileDescriptorErrors(c *C) {
	restoreVersions := notify.MockVersionSupportedCallbacks(fakeNotifyVersions)
	defer restoreVersions()

	var fakeFD uintptr = 1234

	ioctlCalls := 0
	restoreSyscall := notify.MockIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(fd, Equals, fakeFD)
		c.Assert(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)

		ioctlCalls++

		// First expect check for version 3, then for version 7
		switch ioctlCalls {
		case 1:
			checkIoctlBuffer(c, buf, notify.ProtocolVersion(3))
		case 2:
			checkIoctlBuffer(c, buf, notify.ProtocolVersion(7))
		default:
			c.Fatal("called Ioctl more than twice")
		}
		// Always return EPROTONOSUPPORT
		return buf, &notify.IoctlError{req, unix.EPROTONOSUPPORT}
	})
	defer restoreSyscall()

	receivedVersion, err := notify.RegisterFileDescriptor(fakeFD)
	c.Check(err, ErrorMatches, "cannot register notify socket: no mutually supported protocol versions")
	c.Check(receivedVersion, Equals, notify.ProtocolVersion(0))

	calledIoctl := false
	restoreSyscallError := notify.MockIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(fd, Equals, fakeFD)
		c.Assert(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)

		checkIoctlBuffer(c, buf, notify.ProtocolVersion(3))
		c.Assert(calledIoctl, Equals, false, Commentf("called ioctl more than once after first returned error"))
		calledIoctl = true
		return buf, &notify.IoctlError{req, unix.EINVAL}
	})
	defer restoreSyscallError()

	receivedVersion, err = notify.RegisterFileDescriptor(fakeFD)
	c.Check(err, ErrorMatches, "cannot perform IOCTL request APPARMOR_NOTIF_SET_FILTER: EINVAL")
	c.Check(receivedVersion, Equals, notify.ProtocolVersion(0))
}
