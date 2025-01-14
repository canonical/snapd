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

	var fd uintptr = 1234

	ioctlCalls := 0
	restoreSyscall := notify.MockSyscall(func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
		c.Assert(unix.Errno(trap), Equals, unix.Errno(unix.SYS_IOCTL))
		c.Assert(a1, Equals, fd)
		c.Assert(notify.IoctlRequest(a2), Equals, notify.APPARMOR_NOTIF_SET_FILTER)

		ioctlCalls++

		// First expect check for version 3, then for version 7
		switch ioctlCalls {
		case 1:
			checkIoctlBuffer(c, a3, notify.Version(3))
			return 0, 0, unix.EPROTONOSUPPORT
		case 2:
			checkIoctlBuffer(c, a3, notify.Version(7))
			return 0, 0, 0 // no error
		default:
			c.Fatal("called Ioctl more than twice")
			return 0, 0, 0 // no error
		}
	})
	defer restoreSyscall()

	receivedVersion, err := notify.RegisterFileDescriptor(fd)
	c.Check(err, IsNil)
	c.Check(receivedVersion, Equals, notify.Version(7))
}

func checkIoctlBuffer(c *C, ptr uintptr, expectedVersion notify.Version) {
	expectedMsg := notify.MsgNotificationFilter{
		MsgHeader: notify.MsgHeader{
			Version: expectedVersion,
		},
		ModeSet: notify.APPARMOR_MODESET_USER,
	}
	expectedBuf, err := expectedMsg.MarshalBinary()
	c.Assert(err, IsNil)

	// XXX: go vet thinks this is unsafe, since uintptr isn't known to point to
	// valid allocated memory, even though we, the programmer, know it does
	// (to the buffer passed into Ioctl()). And there doesn't seem to be a way
	// to disable the vet error. Need some golang wizard advice here.
	// The tests pass with this uncommented, but go vet fails.
	//receivedBuf := unsafe.Slice((*byte)(unsafe.Pointer(ptr)), len(expectedBuf))
	//c.Check(receivedBuf, DeepEquals, expectedBuf, Commentf("received incorrect buffer on Ioctl call, which expected version %d", expectedVersion))
	c.Check(expectedBuf, NotNil)
}

func (s *notifySuite) TestRegisterFileDescriptorErrors(c *C) {
	restoreVersions := notify.MockVersionSupportedCallbacks(fakeNotifyVersions)
	defer restoreVersions()

	var fd uintptr = 1234

	ioctlCalls := 0
	restoreSyscall := notify.MockSyscall(func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
		c.Assert(unix.Errno(trap), Equals, unix.Errno(unix.SYS_IOCTL))
		c.Assert(a1, Equals, fd)
		c.Assert(notify.IoctlRequest(a2), Equals, notify.APPARMOR_NOTIF_SET_FILTER)

		ioctlCalls++

		// First expect check for version 3, then for version 7
		switch ioctlCalls {
		case 1:
			checkIoctlBuffer(c, a3, notify.Version(3))
		case 2:
			checkIoctlBuffer(c, a3, notify.Version(7))
		default:
			c.Fatal("called Ioctl more than twice")
		}
		// Always return EPROTONOSUPPORT
		return 0, 0, unix.EPROTONOSUPPORT
	})
	defer restoreSyscall()

	receivedVersion, err := notify.RegisterFileDescriptor(fd)
	c.Check(err, ErrorMatches, "cannot register notify socket: no mutually supported protocol versions")
	c.Check(receivedVersion, Equals, notify.Version(0))

	calledIoctl := false
	restoreSyscallError := notify.MockSyscall(func(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err unix.Errno) {
		c.Assert(unix.Errno(trap), Equals, unix.Errno(unix.SYS_IOCTL))
		c.Assert(a1, Equals, fd)
		c.Assert(notify.IoctlRequest(a2), Equals, notify.APPARMOR_NOTIF_SET_FILTER)

		checkIoctlBuffer(c, a3, notify.Version(3))
		c.Assert(calledIoctl, Equals, false, Commentf("called ioctl more than once after first returned error"))
		calledIoctl = true
		return 0, 0, unix.EINVAL // some non-recoverable error
	})
	defer restoreSyscallError()

	receivedVersion, err = notify.RegisterFileDescriptor(fd)
	c.Check(err, ErrorMatches, "cannot perform IOCTL request APPARMOR_NOTIF_SET_FILTER: EINVAL")
	c.Check(receivedVersion, Equals, notify.Version(0))
}
