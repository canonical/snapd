// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023-2024 Canonical Ltd
 *
 * This program is free software: you can redistribute it and/or modify
 * it under the terms of the GNU General Public License version 3 as
 * published by the Free Software Foundation.
 *
 * This program is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
 * GNU General Public License for more details.
 *
 * You should have received a copy of the GNU General Public License
 * along with this program.  If not, see <http://www.gnu.org/licenses/>.
 *
 */

package listener_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"gopkg.in/tomb.v2"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/epoll"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/testtime"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeutil"
)

func Test(t *testing.T) { TestingT(t) }

type listenerSuite struct {
	testutil.BaseTest
}

var _ = Suite(&listenerSuite{})

func (s *listenerSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("") })

	restore := notify.MockVersionKnown(func(v notify.ProtocolVersion) bool {
		// treat every non-zero version as known
		return v != 0
	})
	s.AddCleanup(restore)

	restore = listener.MockCgroupProcessPathInTrackingCgroup(func(pid int) (string, error) {
		return "some-cgroup-path", nil
	})
	s.AddCleanup(restore)
}

func (*listenerSuite) TestReply(c *C) {
	var (
		id      = uint64(0xabcd)
		version = notify.ProtocolVersion(43)
		class   = notify.AA_CLASS_FILE
		aaAllow = notify.FilePermission(0b1010)
		aaDeny  = notify.FilePermission(0b0101)

		userAllow = notify.FilePermission(0b0011)
	)

	restore := listener.MockEncodeAndSendResponse(func(l *listener.Listener, resp *notify.MsgNotificationResponse) error {
		c.Check(resp.KernelNotificationID, Equals, id)
		c.Check(resp.Version, Equals, version)
		c.Check(resp.Allow, Equals, uint32(0b1011))
		c.Check(resp.Deny, Equals, uint32(0b0100))
		return nil
	})
	defer restore()

	req := listener.FakeRequestWithIDVersionClassAllowDeny(id, version, class, aaAllow, aaDeny)
	err := req.Reply(userAllow)
	c.Assert(err, IsNil)
}

func (*listenerSuite) TestReplyNil(c *C) {
	var (
		id      = uint64(0xabcd)
		version = notify.ProtocolVersion(43)
		class   = notify.AA_CLASS_FILE
		aaAllow = notify.FilePermission(0b1010)
		aaDeny  = notify.FilePermission(0b0101)

		userAllow notify.AppArmorPermission = nil
	)

	restore := listener.MockEncodeAndSendResponse(func(l *listener.Listener, resp *notify.MsgNotificationResponse) error {
		c.Check(resp.KernelNotificationID, Equals, id)
		c.Check(resp.Version, Equals, version)
		c.Check(resp.Allow, Equals, aaAllow.AsAppArmorOpMask())
		c.Check(resp.Deny, Equals, aaDeny.AsAppArmorOpMask())
		return nil
	})
	defer restore()

	req := listener.FakeRequestWithIDVersionClassAllowDeny(id, version, class, aaAllow, aaDeny)
	err := req.Reply(userAllow)
	c.Assert(err, IsNil)
}

type fakeAaPerm string

func (p fakeAaPerm) AsAppArmorOpMask() uint32 {
	// return something gratuitously meaningless
	return uint32(len(p))
}

func (p fakeAaPerm) String() string {
	return string(p)
}

func (*listenerSuite) TestReplyBad(c *C) {
	var (
		id      = uint64(0xabcd)
		version = notify.ProtocolVersion(43)
		class   = notify.AA_CLASS_FILE
		aaAllow = notify.FilePermission(0b1010)
		aaDeny  = notify.FilePermission(0b0101)

		userAllow = fakeAaPerm("read")
	)

	restore := listener.MockEncodeAndSendResponse(func(l *listener.Listener, resp *notify.MsgNotificationResponse) error {
		c.Fatalf("should not have attempted to encode and send response")
		return nil
	})
	defer restore()

	req := listener.FakeRequestWithIDVersionClassAllowDeny(id, version, class, aaAllow, aaDeny)
	err := req.Reply(userAllow)
	c.Assert(err, ErrorMatches, "invalid reply: response permission must be of type notify.FilePermission")

	class = notify.AA_CLASS_DBUS // unsupported at the moment
	req = listener.FakeRequestWithIDVersionClassAllowDeny(id, version, class, aaAllow, aaDeny)
	err = req.Reply(userAllow)
	c.Assert(err, ErrorMatches, "internal error: unsupported mediation class: AA_CLASS_DBUS")
}

func (*listenerSuite) TestReplyError(c *C) {
	var (
		id      = uint64(0xabcd)
		version = notify.ProtocolVersion(43)
		class   = notify.AA_CLASS_FILE
		aaAllow = notify.FilePermission(0b1010)
		aaDeny  = notify.FilePermission(0b0101)

		userAllow = notify.FilePermission(0b1111)
	)

	restore := listener.MockEncodeAndSendResponse(func(l *listener.Listener, resp *notify.MsgNotificationResponse) error {
		return fmt.Errorf("failed to send response")
	})
	defer restore()

	req := listener.FakeRequestWithIDVersionClassAllowDeny(id, version, class, aaAllow, aaDeny)
	err := req.Reply(userAllow)
	c.Assert(err, ErrorMatches, "failed to send response")
}

func (*listenerSuite) TestReplyPermissions(c *C) {
	var (
		id      = uint64(0xabcd)
		version = notify.ProtocolVersion(43)
		class   = notify.AA_CLASS_FILE
		aaAllow = notify.FilePermission(0b0101)
		aaDeny  = notify.FilePermission(0b0011)
	)

	for _, testCase := range []struct {
		allowedPermission notify.AppArmorPermission
		respAllow         uint32
		respDeny          uint32
	}{
		{
			nil,
			0b0100,
			0b0011,
		},
		{
			notify.FilePermission(0b0000),
			0b0100,
			0b0011,
		},
		{
			notify.FilePermission(0b0001),
			0b0101,
			0b0010,
		},
		{
			notify.FilePermission(0b0010),
			0b0110,
			0b0001,
		},
		{
			notify.FilePermission(0b0011),
			0b0111,
			0b0000,
		},
		{
			notify.FilePermission(0b0100),
			0b0100,
			0b0011,
		},
		{
			notify.FilePermission(0b0101),
			0b0101,
			0b0010,
		},
		{
			notify.FilePermission(0b0110),
			0b0110,
			0b0001,
		},
		{
			notify.FilePermission(0b0111),
			0b0111,
			0b0000,
		},
		{
			notify.FilePermission(0b1000),
			0b1100,
			0b0011,
		},
		{
			notify.FilePermission(0b1001),
			0b1101,
			0b0010,
		},
		{
			notify.FilePermission(0b1010),
			0b1110,
			0b0001,
		},
		{
			notify.FilePermission(0b1011),
			0b1111,
			0b0000,
		},
		{
			notify.FilePermission(0b1100),
			0b1100,
			0b0011,
		},
		{
			notify.FilePermission(0b1101),
			0b1101,
			0b0010,
		},
		{
			notify.FilePermission(0b1110),
			0b1110,
			0b0001,
		},
		{
			notify.FilePermission(0b1111),
			0b1111,
			0b0000,
		},
	} {
		restore := listener.MockEncodeAndSendResponse(func(l *listener.Listener, resp *notify.MsgNotificationResponse) error {
			c.Check(resp.KernelNotificationID, Equals, id)
			c.Check(resp.Version, Equals, version)
			c.Check(resp.Allow, Equals, testCase.respAllow, Commentf("testCase: %+v", testCase))
			c.Check(resp.Deny, Equals, testCase.respDeny, Commentf("testCase: %+v", testCase))
			return nil
		})

		req := listener.FakeRequestWithIDVersionClassAllowDeny(id, version, class, aaAllow, aaDeny)
		err := req.Reply(testCase.allowedPermission)
		c.Assert(err, IsNil)

		restore()
	}
}

func (*listenerSuite) TestRegisterClose(c *C) {
	testRegisterCloseWithPendingCountExpectReady(c, 0, true)
}

func (*listenerSuite) TestRegisterClosePending(c *C) {
	testRegisterCloseWithPendingCountExpectReady(c, 1, false)
	testRegisterCloseWithPendingCountExpectReady(c, 5, false)
}

func testRegisterCloseWithPendingCountExpectReady(c *C, pendingCount int, expectReady bool) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	restoreRegisterFileDescriptor := listener.MockNotifyRegisterFileDescriptor(func(fd uintptr) (notify.ProtocolVersion, int, error) {
		return notify.ProtocolVersion(12345), pendingCount, nil
	})
	defer restoreRegisterFileDescriptor()

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Fatalf("unexpectedly called notifyIoctl directly: req: %v, buf: %v", req, buf)
		return make([]byte, 0), nil
	})
	defer restoreIoctl()

	restoreTimer := listener.MockTimeAfterFunc(func(d time.Duration, f func()) timeutil.Timer {
		c.Fatalf("unexpectedly called timeutil.AfterFunc without calling Run")
		return nil
	})
	defer restoreTimer()

	l, err := listener.Register()
	c.Assert(err, IsNil)

	checkListenerReady(c, l, expectReady)

	err = l.Close()
	c.Assert(err, IsNil)
}

func (*listenerSuite) TestRegisterOverridePath(c *C) {
	var outputOverridePath string
	restoreOpen := listener.MockOsOpen(func(name string) (*os.File, error) {
		outputOverridePath = name
		placeholderSocket, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		c.Assert(err, IsNil)
		placeholderFile := os.NewFile(uintptr(placeholderSocket), name)
		return placeholderFile, nil
	})
	defer restoreOpen()

	restoreRegisterFileDescriptor := listener.MockNotifyRegisterFileDescriptor(func(fd uintptr) (notify.ProtocolVersion, int, error) {
		pendingCount := 1
		return notify.ProtocolVersion(12345), pendingCount, nil
	})
	defer restoreRegisterFileDescriptor()

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Fatalf("unexpectedly called notifyIoctl directly: req: %v, buf: %v", req, buf)
		return make([]byte, 0), nil
	})
	defer restoreIoctl()

	l, err := listener.Register()
	c.Assert(err, IsNil)

	c.Assert(outputOverridePath, Equals, apparmor.NotifySocketPath)

	err = l.Close()
	c.Assert(err, IsNil)

	fakePath := "/a/new/path"
	err = os.Setenv("PROMPT_NOTIFY_PATH", fakePath)
	c.Assert(err, IsNil)
	defer func() {
		err := os.Unsetenv("PROMPT_NOTIFY_PATH")
		c.Assert(err, IsNil)
	}()

	l, err = listener.Register()
	c.Assert(err, IsNil)

	c.Assert(outputOverridePath, Equals, fakePath)

	err = l.Close()
	c.Assert(err, IsNil)
}

func (*listenerSuite) TestRegisterErrors(c *C) {
	restoreOpen := listener.MockOsOpen(func(name string) (*os.File, error) {
		return nil, os.ErrNotExist
	})
	defer restoreOpen()

	l, err := listener.Register()
	c.Assert(l, IsNil)
	c.Assert(err, Equals, listener.ErrNotSupported)

	customError := errors.New("custom error")

	restoreOpen = listener.MockOsOpen(func(name string) (*os.File, error) {
		return nil, customError
	})
	defer restoreOpen()

	l, err = listener.Register()
	c.Assert(l, IsNil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot open %q: %v", apparmor.NotifySocketPath, customError))

	restoreOpen = listener.MockOsOpen(func(name string) (*os.File, error) {
		placeholderSocket, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		c.Assert(err, IsNil)
		placeholderFile := os.NewFile(uintptr(placeholderSocket), name)
		return placeholderFile, nil
	})
	defer restoreOpen()

	restoreRegisterFileDescriptor := listener.MockNotifyRegisterFileDescriptor(func(fd uintptr) (notify.ProtocolVersion, int, error) {
		return 0, 0, customError
	})
	defer restoreRegisterFileDescriptor()

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Fatalf("unexpectedly called notifyIoctl directly: req: %v, buf: %v", req, buf)
		return make([]byte, 0), nil
	})
	defer restoreIoctl()

	l, err = listener.Register()
	c.Assert(l, IsNil)
	c.Assert(err, Equals, customError)

	restoreOpen = listener.MockOsOpen(func(name string) (*os.File, error) {
		badFd := ^uintptr(0)
		fakeFile := os.NewFile(badFd, name)
		return fakeFile, nil
	})
	defer restoreOpen()

	restoreRegisterFileDescriptor = listener.MockNotifyRegisterFileDescriptor(func(fd uintptr) (notify.ProtocolVersion, int, error) {
		pendingCount := 0
		return notify.ProtocolVersion(12345), pendingCount, nil
	})
	defer restoreRegisterFileDescriptor()

	l, err = listener.Register()
	c.Assert(l, IsNil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot register epoll on %q: bad file descriptor", apparmor.NotifySocketPath))
}

// An expedient abstraction over notify.MsgNotificationFile to allow defining
// MsgNotificationFile structs using value literals.
type msgNotificationFile struct {
	// MsgHeader
	Length  uint16
	Version uint16
	// MsgNotification
	NotificationType     notify.NotificationType
	Signalled            uint8
	Flags                uint8
	KernelNotificationID uint64
	Error                int32
	// msgNotificationOpKernel
	Allow uint32
	Deny  uint32
	Pid   int32
	Label uint32
	Class uint16
	Op    uint16
	// msgNotificationFileKernel
	SUID     uint32
	OUID     uint32
	Filename uint32
	// msgNotificationFileKernel version 5+
	Tags         uint32
	TagsetsCount uint16
}

func (msg *msgNotificationFile) MarshalBinary(c *C) []byte {
	// Check that all the variable-length fields are 0, since we're not packing
	// strings at the end of the message.
	c.Assert(msg.Label, Equals, uint32(0))
	c.Assert(msg.Filename, Equals, uint32(0))
	c.Assert(msg.Tags, Equals, uint32(0))

	msgBuf := bytes.NewBuffer(make([]byte, 0, msg.Length))
	order := arch.Endian()
	c.Assert(binary.Write(msgBuf, order, msg), IsNil)
	length := msgBuf.Len()
	if msg.Version < 5 {
		length -= 6 // cut off Tags and TagsetsCount
	}
	return msgBuf.Bytes()[:length]
}

func (*listenerSuite) TestRunSimple(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	protoVersion := notify.ProtocolVersion(12345)
	pendingCount := 0

	recvChan, sendChan, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl(protoVersion, pendingCount)
	defer restoreEpollIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		c.Check(l.Close(), IsNil)
		c.Check(t.Wait(), IsNil)
	}()

	// since pendingCount == 0, should be immediately ready
	checkListenerReady(c, l, true)

	t.Go(l.Run)

	ids := []uint64{0xdead, 0xbeef}
	requests := make([]*listener.Request, 0, len(ids))

	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)
	tagsets := notify.TagsetMap{
		notify.FilePermission(0b1100): notify.MetadataTags{"tag1", "tag2"},
		notify.FilePermission(0b0010): notify.MetadataTags{"tag3"},
		notify.FilePermission(0b0001): notify.MetadataTags{"tag4"},
	}
	// only expect the tagsets associated with denied permissions
	expectedTagsets := notify.TagsetMap{
		notify.FilePermission(0b0100): notify.MetadataTags{"tag1", "tag2"},
		notify.FilePermission(0b0001): notify.MetadataTags{"tag4"},
	}

	// simulate user only explicitly giving permission for final two bits
	respBits := dBits & 0b0011

	for _, id := range ids {
		msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, tagsets)
		buf, err := msg.MarshalBinary()
		c.Assert(err, IsNil)
		recvChan <- buf

		select {
		case req := <-l.Reqs():
			c.Check(req.PID, Equals, msg.Pid)
			c.Check(req.Label, Equals, label)
			c.Check(req.SubjectUID, Equals, msg.SUID)
			c.Check(req.Path, Equals, path)
			c.Check(req.Class, Equals, notify.AA_CLASS_FILE)
			perm, ok := req.Permission.(notify.FilePermission)
			c.Check(ok, Equals, true)
			c.Check(perm, Equals, notify.FilePermission(dBits))
			c.Check(req.Tagsets, DeepEquals, expectedTagsets)
			requests = append(requests, req)
		case <-t.Dying():
			c.Fatalf("listener encountered unexpected error: %v", t.Err())
		}
	}

	for i, id := range ids {
		response := notify.FilePermission(respBits)

		var desiredBuf []byte
		allow := aBits | (respBits & dBits)
		deny := (^respBits) & dBits
		resp := newMsgNotificationResponse(protoVersion, id, allow, deny)
		desiredBuf, err = resp.MarshalBinary()
		c.Assert(err, IsNil)
		err = requests[i].Reply(response)
		c.Assert(err, IsNil)

		select {
		case received := <-sendChan:
			// all good
			c.Check(received, DeepEquals, desiredBuf)
		case <-time.NewTimer(time.Second).C:
			c.Errorf("failed to receive response in time")
		}
	}
}

func checkListenerReady(c *C, l *listener.Listener, ready bool) {
	if ready {
		select {
		case <-l.Ready():
			// all good
		default:
			c.Error("listener not ready")
		}
	} else {
		select {
		case <-l.Ready():
			c.Error("listener unexpectedly ready")
		default:
			// all good
		}
	}
}

func checkListenerReadyWithTimeout(c *C, l *listener.Listener, ready bool, timeout time.Duration) {
	c.Assert(timeout, Not(Equals), time.Duration(0))
	if ready {
		select {
		case <-l.Ready():
			// all good
		case <-time.NewTimer(timeout).C:
			c.Error("listener not ready")
		}
	} else {
		select {
		case <-l.Ready():
			c.Error("listener unexpectedly ready")
		case <-time.NewTimer(timeout).C:
			// all good
		}
	}
}

func (*listenerSuite) TestRunWithPendingReady(c *C) {
	// Usual case:
	// Pending count 3, send 3 RESENT messages, then one non-RESENT message.
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	logbuf, restore := logger.MockLogger()
	defer restore()

	protoVersion := notify.ProtocolVersion(12345)
	pendingCount := 3

	recvChan, _, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl(protoVersion, pendingCount)
	defer restoreEpollIoctl()

	var timer *testtime.TestTimer
	restoreTimer := listener.MockTimeAfterFunc(func(d time.Duration, f func()) timeutil.Timer {
		if timer != nil {
			c.Fatalf("created more than one timer")
		}
		timer = testtime.AfterFunc(d, func() {
			f()
			c.Fatalf("should not have timed out; receiving final pending RESENT message should have explicitly triggered ready")
		})
		return timer
	})
	defer restoreTimer()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		c.Check(l.Close(), IsNil)
		c.Check(t.Wait(), IsNil)
	}()

	// The timer isn't created until Run is called
	c.Check(timer, IsNil)
	checkListenerReady(c, l, false) // not ready

	t.Go(l.Run)

	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	id := uint64(0xabc0)

	for i := 0; i < 3; i++ {
		id++
		msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, nil)
		msg.Flags = notify.UNOTIF_RESENT
		buf, err := msg.MarshalBinary()
		c.Assert(err, IsNil)
		recvChan <- buf

		// Check that we're not ready yet. Even if this is the last pending
		// message, the listener doesn't ready until the message has been
		// received.
		checkListenerReady(c, l, false)
		c.Check(timer.Active(), Equals, true)

		select {
		case req := <-l.Reqs():
			c.Assert(req.ID, Equals, msg.KernelNotificationID)
		case <-time.NewTimer(time.Second).C:
			c.Fatalf("failed to receive request 0x%x", id)
		}
	}

	// We received the final RESENT message, so should be ready now.
	checkListenerReadyWithTimeout(c, l, true, time.Second)
	c.Check(timer.Active(), Equals, false)

	// Send one more message for good measure, without UNOTIF_RESENT
	id++
	msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, nil)
	buf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	recvChan <- buf

	select {
	case req := <-l.Reqs():
		c.Assert(req.ID, Equals, msg.KernelNotificationID)
	case <-time.NewTimer(time.Second).C:
		c.Fatalf("failed to receive request 0x%x", id)
	}

	checkListenerReady(c, l, true)
	c.Check(timer.Active(), Equals, false)

	c.Check(logbuf.String(), Equals, "")
}

func (*listenerSuite) TestRunWithPendingReadyDropped(c *C) {
	// Rare case:
	// Pending count 3, send 2 RESENT messages, then one non-RESENT message.
	// This should only occur if the kernel times out/drops a pending message.
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	logbuf, restore := logger.MockLogger()
	defer restore()

	protoVersion := notify.ProtocolVersion(12345)
	pendingCount := 3

	recvChan, _, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl(protoVersion, pendingCount)
	defer restoreEpollIoctl()

	var timer *testtime.TestTimer
	restoreTimer := listener.MockTimeAfterFunc(func(d time.Duration, f func()) timeutil.Timer {
		if timer != nil {
			c.Fatalf("created more than one timer")
		}
		timer = testtime.AfterFunc(d, func() {
			f()
			c.Fatalf("should not have timed out; receiving non-RESENT message should have explicitly triggered ready")
		})
		return timer
	})
	defer restoreTimer()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		c.Check(l.Close(), IsNil)
		c.Check(t.Wait(), IsNil)
	}()

	// The timer isn't created until Run is called
	c.Check(timer, IsNil)
	checkListenerReady(c, l, false) // not ready

	t.Go(l.Run)

	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	id := uint64(0xabc0)

	for i := 0; i < 2; i++ {
		id++
		msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, nil)
		msg.Flags = notify.UNOTIF_RESENT
		buf, err := msg.MarshalBinary()
		c.Assert(err, IsNil)
		recvChan <- buf

		// Check that we're not ready yet. Even if this is the last pending
		// message, the listener doesn't ready until the message has been
		// received.
		checkListenerReady(c, l, false)
		c.Check(timer.Active(), Equals, true)

		select {
		case req := <-l.Reqs():
			c.Assert(req.ID, Equals, msg.KernelNotificationID)
		case <-time.NewTimer(time.Second).C:
			c.Fatalf("failed to receive request 0x%x", id)
		}
	}

	// We have still not received the final RESENT message, so should not be ready.
	checkListenerReady(c, l, false)
	c.Check(timer.Active(), Equals, true)

	// Send a message without UNOTIF_RESENT
	id++
	msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, nil)
	buf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	recvChan <- buf

	// The listener even seeing a message without UNOTIF_RESENT should be enough
	// for it to ready up, since this indicates the kernel is done resending
	// previously-sent requests. We don't need to have received it yet.
	checkListenerReadyWithTimeout(c, l, true, time.Second)
	// Readiness stops the timer
	c.Check(timer.Active(), Equals, false)

	// Now receive it
	select {
	case req := <-l.Reqs():
		c.Assert(req.ID, Equals, msg.KernelNotificationID)
	case <-time.NewTimer(time.Second).C:
		c.Fatalf("failed to receive request 0x%x", id)
	}

	c.Check(logbuf.String(), testutil.Contains, "received non-resent message when pending count was 1")

	// We're still ready, of course
	checkListenerReady(c, l, true)
}

func (*listenerSuite) TestRunWithPendingReadyTimeout(c *C) {
	// Somewhat rare case:
	// Pending count 3, send 1 RESENT message, then time out.
	// This should only occur if snapd or the kernel is exceptionally slow, or
	// if the kernel times out/drops a pending message but then never sends any
	// new messages (at least until after the timeout).
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	logbuf, restore := logger.MockLogger()
	defer restore()

	protoVersion := notify.ProtocolVersion(12345)
	pendingCount := 3

	recvChan, _, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl(protoVersion, pendingCount)
	defer restoreEpollIoctl()

	var timer *testtime.TestTimer
	callbackDone := make(chan struct{})
	restoreTimer := listener.MockTimeAfterFunc(func(d time.Duration, f func()) timeutil.Timer {
		if timer != nil {
			c.Fatalf("created more than one timer")
		}
		timer = testtime.AfterFunc(d, func() {
			f()
			close(callbackDone)
		})
		return timer
	})
	defer restoreTimer()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		c.Check(l.Close(), IsNil)
		c.Check(t.Wait(), IsNil)
	}()

	// The timer isn't created until Run is called
	c.Check(timer, IsNil)
	checkListenerReady(c, l, false) // not ready

	t.Go(l.Run)

	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	id := uint64(0xabc0)

	id++
	msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, nil)
	msg.Flags = notify.UNOTIF_RESENT
	buf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	recvChan <- buf

	checkListenerReady(c, l, false)
	c.Check(timer.Active(), Equals, true)

	select {
	case req := <-l.Reqs():
		c.Assert(req.ID, Equals, msg.KernelNotificationID)
	case <-time.NewTimer(time.Second).C:
		c.Fatalf("failed to receive request 0x%x", id)
	}

	// We have still not received the final RESENT message, so should not be ready.
	checkListenerReady(c, l, false)
	c.Check(timer.Active(), Equals, true)

	// Time out the ready timer
	timer.Elapse(listener.ReadyTimeout)
	c.Assert(timer.Active(), Equals, false)
	c.Assert(timer.FireCount(), Equals, 1)

	// Wait for callback to finish readying and recording logger.Notice
	<-callbackDone

	// Expect the callback to mark the listener as ready
	checkListenerReady(c, l, true)

	// Now, for good measure, send a message with UNOTIF_RESENT
	id++
	msg = newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, nil)
	msg.Flags = notify.UNOTIF_RESENT
	buf, err = msg.MarshalBinary()
	c.Assert(err, IsNil)
	recvChan <- buf

	// We're still ready
	checkListenerReady(c, l, true)

	// Now receive it
	select {
	case req := <-l.Reqs():
		c.Assert(req.ID, Equals, msg.KernelNotificationID)
	case <-time.NewTimer(time.Second).C:
		c.Fatalf("failed to receive request 0x%x", id)
	}

	c.Check(logbuf.String(), testutil.Contains, "timeout waiting for resent messages from apparmor: still expected 2 more resent messages")

	// We're still ready
	checkListenerReady(c, l, true)
}

// Check that if a request is written between when the listener is registered
// and when Run() is called, that request will still be handled correctly.
func (*listenerSuite) TestRegisterWriteRun(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	protoVersion := notify.ProtocolVersion(0xabc)
	pendingCount := 0

	recvChan, _, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl(protoVersion, pendingCount)
	defer restoreEpollIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		c.Check(l.Close(), IsNil)
		c.Check(t.Wait(), IsNil)
	}()

	// since pendingCount == 0, should be immediately ready
	checkListenerReady(c, l, true)

	id := uint64(0x1234)
	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)
	tagsets := notify.TagsetMap{}

	msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, tagsets)
	buf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)

	go func() {
		select {
		case recvChan <- buf:
			// all good
		case <-time.NewTimer(time.Second).C:
			c.Fatalf("failed to receive buffer")
		}
	}()

	select {
	case <-l.Reqs():
		c.Fatalf("should not have received request before Run() called")
	case <-t.Dying():
		c.Fatalf("tomb encountered an error before Run() called: %v", t.Err())
	case <-time.NewTimer(10 * time.Millisecond).C:
	}

	t.Go(l.Run)

	select {
	case req, ok := <-l.Reqs():
		c.Assert(ok, Equals, true)
		c.Assert(req.Path, Equals, path)
	case <-t.Dying():
		c.Fatalf("listener encountered unexpected error: %v", t.Err())
	case <-time.NewTimer(time.Second).C:
		c.Fatalf("failed to receive request before timer expired")
	}
}

// Check that if multiple requests are included in a single request buffer from
// the kernel, each will still be handled correctly.
func (*listenerSuite) TestRunMultipleRequestsInBuffer(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	protoVersion := notify.ProtocolVersion(0x43)
	pendingCount := 0

	recvChan, _, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl(protoVersion, pendingCount)
	defer restoreEpollIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		c.Check(l.Close(), IsNil)
		c.Check(t.Wait(), IsNil)
	}()

	// since pendingCount == 0, should be immediately ready
	checkListenerReady(c, l, true)

	t.Go(l.Run)

	label := "snap.foo.bar"
	paths := []string{"/home/Documents/foo", "/path/to/bar", "/baz"}

	aBits := uint32(0b1010)
	dBits := uint32(0b0101)
	tagsets := notify.TagsetMap{}

	var megaBuf []byte
	for i, path := range paths {
		msg := newMsgNotificationFile(protoVersion, uint64(i), label, path, aBits, dBits, tagsets)
		buf, err := msg.MarshalBinary()
		c.Assert(err, IsNil)
		megaBuf = append(megaBuf, buf...)
	}

	recvChan <- megaBuf

	for i, path := range paths {
		select {
		case req := <-l.Reqs():
			c.Assert(req.Path, DeepEquals, path)
		case <-t.Dying():
			c.Fatalf("listener encountered unexpected error during request %d: %v", i, t.Err())
		case <-time.NewTimer(time.Second).C:
			c.Fatalf("failed to receive request %d before timer expired", i)
		}
	}
}

// Check that the system of epoll event listening works as expected.
func (*listenerSuite) TestRunEpoll(c *C) {
	listener.ExitOnError()

	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	c.Assert(err, IsNil)
	notifyFile := os.NewFile(uintptr(sockets[0]), apparmor.NotifySocketPath)
	kernelSocket := sockets[1]

	restoreOpen := listener.MockOsOpen(func(name string) (*os.File, error) {
		c.Assert(name, Equals, apparmor.NotifySocketPath)
		return notifyFile, nil
	})
	defer restoreOpen()

	protoVersion := notify.ProtocolVersion(12345)

	restoreRegisterFileDescriptor := listener.MockNotifyRegisterFileDescriptor(func(fd uintptr) (notify.ProtocolVersion, int, error) {
		pendingCount := 0
		return protoVersion, pendingCount, nil
	})
	defer restoreRegisterFileDescriptor()

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(fd, Equals, uintptr(notifyFile.Fd()))
		switch req {
		case notify.APPARMOR_NOTIF_SET_FILTER:
			c.Fatalf("unexpectedly called notifyIoctl directly: req: %v, buf: %v", req, buf)
		case notify.APPARMOR_NOTIF_RECV:
			buf := notify.NewIoctlRequestBuffer(protoVersion)
			n, err := unix.Read(int(fd), buf)
			c.Assert(err, IsNil)
			return buf[:n], nil
		case notify.APPARMOR_NOTIF_SEND:
			c.Fatalf("listener should not have tried to SEND")
		}
		return nil, fmt.Errorf("unexpected ioctl request: %v", req)
	})
	defer restoreIoctl()

	id := uint64(0x1234)
	label := "snap.foo.bar"
	path := "/home/Documents/foo/bar"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, nil)
	recvBuf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		c.Check(l.Close(), IsNil)
		c.Check(t.Wait(), IsNil)
	}()

	t.Go(l.Run)

	_, err = unix.Write(kernelSocket, recvBuf)
	c.Check(err, IsNil)

	requestTimer := time.NewTimer(time.Second)
	select {
	case req := <-l.Reqs():
		c.Check(req.Path, Equals, path)
	case <-t.Dying():
		c.Errorf("listener encountered unexpected error: %v", t.Err())
	case <-requestTimer.C:
		c.Errorf("timed out waiting for listener to send request")
	}
}

// Check that if no epoll event occurs, listener can still close.
func (*listenerSuite) TestRunNoEpoll(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	restoreEpoll := listener.MockEpollWait(func(l *listener.Listener) ([]epoll.Event, error) {
		for !l.EpollIsClosed() {
			// do nothing until epoll is closed
		}
		return nil, fmt.Errorf("fake epoll error")
	})
	defer restoreEpoll()

	restoreRegisterFileDescriptor := listener.MockNotifyRegisterFileDescriptor(func(fd uintptr) (notify.ProtocolVersion, int, error) {
		pendingCount := 1
		return notify.ProtocolVersion(12345), pendingCount, nil
	})
	defer restoreRegisterFileDescriptor()

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Fatalf("unexpectedly called notifyIoctl directly: req: %v, buf: %v", req, buf)
		return make([]byte, 0), nil
	})
	defer restoreIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)

	runAboutToStart := make(chan struct{})
	t.Go(func() error {
		close(runAboutToStart)
		return l.Run()
	})

	// Make sure Run() starts before error triggers Close()
	<-runAboutToStart
	time.Sleep(10 * time.Millisecond)

	err = l.Close()
	c.Check(err, IsNil)

	c.Check(t.Wait(), IsNil)
}

// Test that if there is no read from Reqs(), listener can still close.
func (*listenerSuite) TestRunNoReceiver(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	protoVersion := notify.ProtocolVersion(5)
	pendingCount := 0

	recvChan, _, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl(protoVersion, pendingCount)
	defer restoreEpollIoctl()

	ioctlDone, restoreIoctl := listener.SynchronizeNotifyIoctl()
	defer restoreIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)

	checkListenerReady(c, l, true)

	t.Go(l.Run)

	id := uint64(0x1234)
	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, notify.TagsetMap{})
	buf, err := msg.MarshalBinary()
	c.Check(err, IsNil)
	recvChan <- buf

	// wait for the ioctl to finish before closing, so that the listener will
	// be waiting, trying to send the request, when the close occurs
	select {
	case req := <-ioctlDone:
		c.Check(req, Equals, notify.APPARMOR_NOTIF_RECV)
	case <-time.NewTimer(100 * time.Millisecond).C:
		c.Errorf("failed to synchronize on ioctl call")
	}

	c.Check(l.Close(), IsNil)
	c.Check(t.Wait(), IsNil)
}

// Test that if there is no read from Reqs(), listener can still close, even
// if there are pending unreceived requests which are expected to be re-sent.
func (*listenerSuite) TestRunNoReceiverWithPending(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	protoVersion := notify.ProtocolVersion(5)
	pendingCount := 1

	recvChan, _, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl(protoVersion, pendingCount)
	defer restoreEpollIoctl()

	ioctlDone, restoreIoctl := listener.SynchronizeNotifyIoctl()
	defer restoreIoctl()

	var timer *testtime.TestTimer
	restoreTimer := listener.MockTimeAfterFunc(func(d time.Duration, f func()) timeutil.Timer {
		if timer != nil {
			c.Fatalf("created more than one timer")
		}
		timer = testtime.AfterFunc(d, f)
		return timer
	})
	defer restoreTimer()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)

	// Timer hasn't been created yet
	c.Check(timer, IsNil)

	t.Go(l.Run)

	checkListenerReady(c, l, false)

	id := uint64(0x1234)
	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, nil)
	// Set UNOTIF_RESENT so this message doesn't trigger ready as soon as it
	// is seen by the listener. Since there's no reader, it should not ready.
	msg.Flags = notify.UNOTIF_RESENT
	buf, err := msg.MarshalBinary()
	c.Check(err, IsNil)
	recvChan <- buf

	// wait for the ioctl to finish before closing, so that the listener will
	// be waiting, trying to send the request, when the close occurs
	select {
	case req := <-ioctlDone:
		c.Check(req, Equals, notify.APPARMOR_NOTIF_RECV)
	case <-time.NewTimer(100 * time.Millisecond).C:
		c.Errorf("failed to synchronize on ioctl call")
	}

	// No receiver read this request, so we're still not ready
	checkListenerReadyWithTimeout(c, l, false, 10*time.Millisecond)

	c.Check(timer.Active(), Equals, true)

	c.Check(l.Close(), IsNil)

	// Close() doesn't cause the listener to signal that it's ready
	checkListenerReadyWithTimeout(c, l, false, 10*time.Millisecond)

	c.Check(t.Wait(), IsNil)

	// Closing the listener should have stopped the timer without firing it
	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 0)
}

// Test that if there is no read from Reqs(), listener closing does not cause
// a panic if it races with the ready timeout expiring.
func (*listenerSuite) TestRunNoReceiverWithPendingTimeout(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	protoVersion := notify.ProtocolVersion(5)
	pendingCount := 1

	recvChan, _, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl(protoVersion, pendingCount)
	defer restoreEpollIoctl()

	ioctlDone, restoreIoctl := listener.SynchronizeNotifyIoctl()
	defer restoreIoctl()

	var timer *testtime.TestTimer
	startCallback := make(chan struct{})
	callbackDone := make(chan struct{})
	restoreTimer := listener.MockTimeAfterFunc(func(d time.Duration, f func()) timeutil.Timer {
		if timer != nil {
			c.Fatalf("created more than one timer")
		}
		timer = testtime.AfterFunc(d, func() {
			// timer fired, but don't run callback until we get the signal
			<-startCallback
			f()
			close(callbackDone)
		})
		return timer
	})
	defer restoreTimer()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)

	// Timer hasn't been created yet
	c.Check(timer, IsNil)

	t.Go(l.Run)

	checkListenerReady(c, l, false)

	id := uint64(0x1234)
	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, nil)
	// Set UNOTIF_RESENT so this message doesn't trigger ready as soon as it
	// is seen by the listener. Since there's no reader, it should not ready.
	msg.Flags = notify.UNOTIF_RESENT
	buf, err := msg.MarshalBinary()
	c.Check(err, IsNil)
	recvChan <- buf

	// wait for the ioctl to finish before closing, so that the listener will
	// be waiting, trying to send the request, when the close occurs
	select {
	case req := <-ioctlDone:
		c.Check(req, Equals, notify.APPARMOR_NOTIF_RECV)
	case <-time.NewTimer(100 * time.Millisecond).C:
		c.Errorf("failed to synchronize on ioctl call")
	}

	// No receiver read this request, so we're still not ready
	checkListenerReadyWithTimeout(c, l, false, 10*time.Millisecond)

	c.Check(timer.Active(), Equals, true)
	// Time out the ready timer
	timer.Elapse(listener.ReadyTimeout)
	c.Check(timer.Active(), Equals, false)
	c.Check(timer.FireCount(), Equals, 1)
	// Callback is now running, but waiting
	checkListenerReadyWithTimeout(c, l, false, 10*time.Millisecond)

	c.Check(l.Close(), IsNil)

	// Close() doesn't cause the listener to signal that it's ready
	checkListenerReadyWithTimeout(c, l, false, 10*time.Millisecond)

	// Run loop stops the timer (which has already fired) and then immediately
	// closes reqs
	select {
	case <-l.Reqs():
		// all good
	case <-time.NewTimer(10 * time.Millisecond).C:
		c.Fatalf("reqs failed to close once listener closed")
	}

	// Let the callback run and wait for it to finish
	close(startCallback)
	<-callbackDone
	// The callback marks the listener as ready
	checkListenerReady(c, l, true)

	c.Check(t.Wait(), IsNil)
}

// Test that if there is no reply to a request, listener can still close, and
// subsequent reply does not block.
func (*listenerSuite) TestRunNoReply(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	protoVersion := notify.ProtocolVersion(0x1234)
	pendingCount := 0

	recvChan, _, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl(protoVersion, pendingCount)
	defer restoreEpollIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)

	t.Go(l.Run)

	id := uint64(0x1234)
	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	msg := newMsgNotificationFile(protoVersion, id, label, path, aBits, dBits, notify.TagsetMap{})
	buf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	recvChan <- buf

	req := <-l.Reqs()

	c.Check(l.Close(), IsNil)

	response := notify.FilePermission(1234)
	err = req.Reply(response)
	c.Check(err, Equals, listener.ErrClosed)

	c.Check(t.Wait(), IsNil)
}

func newMsgNotificationFile(protocolVersion notify.ProtocolVersion, id uint64, label, name string, allow, deny uint32, tagsets notify.TagsetMap) *notify.MsgNotificationFile {
	msg := notify.MsgNotificationFile{}
	msg.Version = protocolVersion
	msg.NotificationType = notify.APPARMOR_NOTIF_OP
	msg.KernelNotificationID = id
	msg.Allow = allow
	msg.Deny = deny
	msg.Pid = 1234
	msg.Label = label
	msg.Class = notify.AA_CLASS_FILE
	msg.SUID = 1000
	msg.Filename = name
	msg.Tagsets = tagsets
	return &msg
}

func newMsgNotificationResponse(protocolVersion notify.ProtocolVersion, id uint64, allow, deny uint32) *notify.MsgNotificationResponse {
	msgHeader := notify.MsgHeader{
		Version: protocolVersion,
	}
	msgNotification := notify.MsgNotification{
		MsgHeader:            msgHeader,
		NotificationType:     notify.APPARMOR_NOTIF_RESP,
		Flags:                notify.URESPONSE_NO_CACHE,
		KernelNotificationID: id,
		Error:                0,
	}
	resp := notify.MsgNotificationResponse{
		MsgNotification: msgNotification,
		Error:           0,
		Allow:           allow,
		Deny:            deny,
	}
	return &resp
}

func (*listenerSuite) TestRunErrors(c *C) {
	listener.ExitOnError()

	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	protoVersion := notify.ProtocolVersion(1123)
	pendingCount := 0

	recvChan, _, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl(protoVersion, pendingCount)
	defer restoreEpollIoctl()

	for _, testCase := range []struct {
		msg msgNotificationFile
		err string
	}{
		{
			msgNotificationFile{},
			`cannot extract first message: cannot parse message header: invalid length \(must be >= 4\): 0`,
		},
		{
			msgNotificationFile{
				Length: 1234,
			},
			`cannot extract first message: length in header exceeds data length: 1234 > 52`,
		},
		{
			msgNotificationFile{
				Length:  1234,
				Version: 1123,
			},
			`cannot extract first message: length in header exceeds data length: 1234 > 58`,
		},
		{
			msgNotificationFile{
				Length: 13,
			},
			`cannot unmarshal apparmor message header: unsupported version: 0`,
		},
		{
			msgNotificationFile{
				Version: 3,
				Length:  13,
			},
			`cannot unmarshal apparmor notification message: cannot unpack: unexpected EOF`,
		},
		{
			msgNotificationFile{
				Version: 99,
				Length:  52,
			},
			`unexpected protocol version: listener registered with 1123, but received 99`,
		},
		{
			msgNotificationFile{
				Length:           52,
				Version:          1123,
				NotificationType: notify.APPARMOR_NOTIF_CANCEL,
			},
			`unsupported notification type: APPARMOR_NOTIF_CANCEL`,
		},
		{
			msgNotificationFile{
				Length:           52,
				Version:          1123,
				NotificationType: notify.APPARMOR_NOTIF_OP,
				Class:            uint16(notify.AA_CLASS_DBUS),
			},
			`unsupported mediation class: AA_CLASS_DBUS`,
		},
	} {
		l, err := listener.Register()
		c.Assert(err, IsNil)

		var t tomb.Tomb
		t.Go(l.Run)

		buf := testCase.msg.MarshalBinary(c)
		recvChan <- buf

		select {
		case r := <-l.Reqs():
			c.Check(r, IsNil, Commentf("should not have received non-nil request; expected error: %v", testCase.err))
		case <-time.NewTimer(time.Second).C:
			c.Error("done waiting for expected error", testCase.err)
		case <-t.Dying():
		}
		err = t.Wait()
		c.Check(err, ErrorMatches, testCase.err)

		err = l.Close()
		c.Check(err, Equals, listener.ErrAlreadyClosed)
	}
}

func (*listenerSuite) TestRunMultipleTimes(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	restoreEpoll := listener.MockEpollWait(func(l *listener.Listener) ([]epoll.Event, error) {
		for !l.EpollIsClosed() {
			// do nothing until epoll is closed
		}
		return nil, fmt.Errorf("fake epoll error")
	})
	defer restoreEpoll()

	restoreRegisterFileDescriptor := listener.MockNotifyRegisterFileDescriptor(func(fd uintptr) (notify.ProtocolVersion, int, error) {
		pendingCount := 0
		return notify.ProtocolVersion(12345), pendingCount, nil
	})
	defer restoreRegisterFileDescriptor()

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Fatalf("unexpectedly called notifyIoctl directly: req: %v, buf: %v", req, buf)
		return make([]byte, 0), nil
	})
	defer restoreIoctl()

	l, err := listener.Register()
	c.Assert(err, IsNil)

	count := 3
	var wg sync.WaitGroup
	returnChan := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			wg.Done() // mark that Run has started
			returnChan <- l.Run()
		}()
	}

	// Wait for all Run calls to start
	wg.Wait()

	// Check that no Run calls returned yet
	select {
	case err := <-returnChan:
		c.Fatalf("received unexpected return before listener closed: %v", err)
	case <-time.NewTimer(10 * time.Millisecond).C:
		// no errors yet
	}

	l.Close()

	for i := 0; i < count; i++ {
		select {
		case err := <-returnChan:
			// Run returns nil if the listener was deliberately closed.
			c.Check(err, IsNil)
		case <-time.NewTimer(time.Second).C:
			c.Fatalf("failed to receive error from listener.Run")
		}
	}
}

// Test that calling Run() after Close() is fine.
func (*listenerSuite) TestCloseThenRun(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	restoreRegisterFileDescriptor := listener.MockNotifyRegisterFileDescriptor(func(fd uintptr) (notify.ProtocolVersion, int, error) {
		pendingCount := 3
		return notify.ProtocolVersion(12345), pendingCount, nil
	})
	defer restoreRegisterFileDescriptor()

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Fatalf("unexpectedly called notifyIoctl directly: req: %v, buf: %v", req, buf)
		return make([]byte, 0), nil
	})
	defer restoreIoctl()

	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		c.Assert(l.Close(), Equals, listener.ErrAlreadyClosed)
	}()

	err = l.Close()
	c.Assert(err, IsNil)

	err = l.Run()
	c.Assert(err, Equals, nil)
}

func (*listenerSuite) TestRunConcurrency(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	protoVersion := notify.ProtocolVersion(0xaaaa)
	pendingCount := 0

	recvChan, sendChan, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl(protoVersion, pendingCount)
	epollIoctlRestored := false
	defer func() {
		if !epollIoctlRestored {
			restoreEpollIoctl()
		}
	}()

	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		err = l.Close()
		c.Check(err, Equals, listener.ErrAlreadyClosed)
	}()

	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	reqAllow := uint32(0b1010)
	reqDeny := uint32(0b0101)

	msg := newMsgNotificationFile(protoVersion, 0, label, path, reqAllow, reqDeny, nil)

	respAllow := uint32(0b1111)
	respDeny := uint32(0b0000)
	resp := newMsgNotificationResponse(protoVersion, 0, respAllow, respDeny)

	templateBuf, err := resp.MarshalBinary()
	c.Assert(err, IsNil)
	expectedLen := len(templateBuf)

	var t tomb.Tomb

	// Creator
	requestsSent := 0
	creator := func() error {
		// create requests until the listener is dead
		id := uint64(0)
		for {
			id += 1
			msg.KernelNotificationID = id
			buf, err := msg.MarshalBinary()
			c.Assert(err, IsNil)
			select {
			case <-t.Dying():
			case recvChan <- buf:
				requestsSent += 1
				continue
			}
			break
		}
		c.Logf("total requests sent: %d", id)
		return nil
	}

	// Replier
	replyCount := 0
	replier := func() error {
		// reply to all requests as they are received, until l.Reqs() closes
		response := notify.FilePermission(1234)
		for req := range l.Reqs() {
			err := req.Reply(response)
			if err == listener.ErrClosed {
				break
			}
			c.Check(err, IsNil)
			replyCount += 1
		}
		c.Logf("total replies sent: %d", replyCount)
		return nil
	}

	// Start all the tomb-tracked goroutines
	t.Go(func() error {
		t.Go(l.Run)
		t.Go(creator)
		t.Go(replier)
		return nil
	})

	slowTimer := time.NewTimer(10 * time.Second)
	minResponsesReceived := 10
	hitMinimum := make(chan struct{})
	// Receiver
	doneReceivingResponses := make(chan struct{})
	responseCount := 0
	go func() {
		// Consume a minimum (>1) number of responses from the sendChan.
		// No guarantee of order, so just check that the length is correct,
		// rather than picking apart the buffer to throw out the ID and
		// compare the rest.
		for i := 0; i < minResponsesReceived; i++ {
			response := <-sendChan
			c.Check(response, HasLen, expectedLen)
			responseCount += 1
		}
		close(hitMinimum)
		// Consume any remaining responses
		for response := range sendChan {
			c.Check(response, HasLen, expectedLen)
			responseCount += 1
		}
		close(doneReceivingResponses)
		c.Logf("total responses received: %d", responseCount)
	}()

	// Wait until we can tell that the system is (or is not) working
	select {
	case <-hitMinimum:
	case <-slowTimer.C:
	}

	// Check that no error has yet occurred
	c.Check(t.Err(), Equals, tomb.ErrStillAlive)

	// Check that closing the listener while creating and replying to requests
	// does not cause a panic (e.g. by writing to a closed channel).
	// This also stops the replier since l.Close() closes l.Reqs().
	c.Check(l.Close(), IsNil)
	// Explitly kill the tomb so that the creator finishes creating
	killedErr := fmt.Errorf("killed the tomb")
	t.Kill(killedErr)
	c.Check(t.Wait(), Equals, killedErr)

	// restoreEpollIoctl() closes sendChan
	restoreEpollIoctl()
	epollIoctlRestored = true
	// Now the goroutine reading from sendChan can close doneReceivingResponses
	<-doneReceivingResponses

	c.Check(requestsSent > 1, Equals, true, Commentf("should have sent more than one request"))
	c.Check(replyCount > 1, Equals, true, Commentf("should have replied to more than one request"))
	c.Check(responseCount > 1, Equals, true, Commentf("should have received more than one response"))
}
