// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"reflect"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"gopkg.in/tomb.v2"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/epoll"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/sandbox/apparmor/notify/listener"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
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
}

func (*listenerSuite) TestReply(c *C) {
	rc := make(chan interface{}, 1)
	req := listener.FakeRequestWithClassAndReplyChan(notify.AA_CLASS_FILE, rc)
	reply := true
	req.Reply(reply)
	resp := <-rc
	c.Assert(resp, Equals, reply)
}

func (*listenerSuite) TestBadReply(c *C) {
	rc := make(chan interface{}, 1)
	req := listener.FakeRequestWithClassAndReplyChan(notify.AA_CLASS_FILE, rc)
	reply := 1
	err := req.Reply(reply)
	c.Assert(err, ErrorMatches, "invalid reply: response must be of type bool")
}

func (*listenerSuite) TestReplyTwice(c *C) {
	rc := make(chan interface{}, 1)
	req := listener.FakeRequestWithClassAndReplyChan(notify.AA_CLASS_FILE, rc)
	reply := false
	err := req.Reply(reply)
	c.Assert(err, IsNil)
	resp := <-rc
	c.Assert(resp, Equals, reply)

	reply = true
	err = req.Reply(reply)
	c.Assert(err, Equals, listener.ErrAlreadyReplied)
}

func (*listenerSuite) TestRegisterClose(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		expected := notify.MsgNotificationFilter{ModeSet: notify.APPARMOR_MODESET_USER}
		expectedBytes, err := expected.MarshalBinary()
		c.Assert(err, IsNil)
		expectedBuf := notify.IoctlRequestBuffer(expectedBytes)
		c.Assert(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)
		c.Assert(buf, DeepEquals, expectedBuf)
		return make([]byte, 0), nil
	})
	defer restoreIoctl()

	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		err = l.Close()
		c.Assert(err, IsNil)
	}()
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

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)
		return make([]byte, 0), nil
	})
	defer restoreIoctl()

	l, err := listener.Register()
	c.Assert(err, IsNil)

	c.Assert(outputOverridePath, Equals, notify.SysPath)

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
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot open %q: %v", notify.SysPath, customError))

	restoreOpen = listener.MockOsOpen(func(name string) (*os.File, error) {
		placeholderSocket, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		c.Assert(err, IsNil)
		placeholderFile := os.NewFile(uintptr(placeholderSocket), name)
		return placeholderFile, nil
	})
	defer restoreOpen()

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)
		return nil, customError
	})
	defer restoreIoctl()

	l, err = listener.Register()
	c.Assert(l, IsNil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot notify ioctl to modeset user on %q: %v", notify.SysPath, customError))

	restoreOpen = listener.MockOsOpen(func(name string) (*os.File, error) {
		badFd := ^uintptr(0)
		fakeFile := os.NewFile(badFd, name)
		return fakeFile, nil
	})
	defer restoreOpen()

	restoreIoctl = listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)
		return make([]byte, 0), nil
	})
	defer restoreIoctl()

	l, err = listener.Register()
	c.Assert(l, IsNil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("cannot register epoll on %q: bad file descriptor", notify.SysPath))
}

// An expedient abstraction over notify.MsgNotificationFile to allow defining
// MsgNotificationFile structs using value literals.
type msgNotificationFile struct {
	// MsgHeader
	Length  uint16
	Version uint16
	// MsgNotification
	NotificationType notify.NotificationType
	Signalled        uint8
	NoCache          uint8
	ID               uint64
	Error            int32
	// msgNotificationOpKernel
	Allow uint32
	Deny  uint32
	Pid   uint32
	Label uint32
	Class uint32
	Op    uint32
	// msgNotificationFileKernel
	SUID uint32
	OUID uint32
	Name uint32
}

func (msg *msgNotificationFile) MarshalBinary(c *C) []byte {
	msgBuf := bytes.NewBuffer(make([]byte, 0, msg.Length))
	order := arch.Endian()
	c.Assert(binary.Write(msgBuf, order, msg), IsNil)
	return msgBuf.Bytes()
}

func (*listenerSuite) TestRunSimple(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	recvChan, sendChan, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl()
	defer restoreEpollIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		c.Check(l.Close(), IsNil)
		c.Check(t.Wait(), Equals, listener.ErrClosed)
	}()

	t.Go(l.Run)

	ids := []uint64{0xdead, 0xbeef}
	requests := make([]*listener.Request, 0, len(ids))

	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	for _, id := range ids {
		msg := newMsgNotificationFile(id, label, path, aBits, dBits)
		buf, err := msg.MarshalBinary()
		c.Assert(err, IsNil)
		recvChan <- buf

		select {
		case req := <-l.Reqs():
			c.Assert(req.PID(), Equals, msg.Pid)
			c.Assert(req.Label(), Equals, label)
			c.Assert(req.SubjectUID(), Equals, msg.SUID)
			c.Assert(req.Path(), Equals, path)
			c.Assert(req.Class(), Equals, notify.AA_CLASS_FILE)
			perm, ok := req.Permission().(notify.FilePermission)
			c.Assert(ok, Equals, true)
			c.Assert(perm, Equals, notify.FilePermission(dBits))
			requests = append(requests, req)
		case <-l.Dying():
			c.Fatalf("listener encountered unexpected error: %v", l.Err())
		}
	}

	for i, id := range ids {
		switch i % 2 {
		case 0:
			err = requests[i].Reply(false)
		case 1:
			err = requests[i].Reply(true)
		}
		c.Assert(err, IsNil)

		allow := aBits | (dBits * uint32(i))
		deny := dBits * uint32(1-i)
		resp := newMsgNotificationResponse(id, allow, deny)
		desiredBuf, err := resp.MarshalBinary()
		c.Assert(err, IsNil)

		received := <-sendChan
		c.Assert(received, DeepEquals, desiredBuf)
	}
}

// Check that if a request is written between when the listener is registered
// and when Run() is called, that request will still be handled correctly.
func (*listenerSuite) TestRegisterWriteRun(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	recvChan, sendChan, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl()
	defer restoreEpollIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		c.Check(l.Close(), IsNil)
		c.Check(t.Wait(), Equals, listener.ErrClosed)
	}()

	id := uint64(1234)
	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	msg := newMsgNotificationFile(id, label, path, aBits, dBits)
	buf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)

	go func() {
		giveUp := time.NewTimer(100 * time.Millisecond)
		select {
		case recvChan <- buf:
			// all good
		case <-giveUp.C:
			c.Fatalf("failed to receive buffer")
		}
	}()

	timer := time.NewTimer(10 * time.Millisecond)
	select {
	case <-l.Reqs():
		c.Fatalf("should not have received request before Run() called")
	case <-l.Dying():
		c.Fatalf("listener encountered an error before Run() called: %v", l.Err())
	case <-timer.C:
	}

	t.Go(l.Run)

	timer.Reset(100 * time.Millisecond)
	select {
	case req, ok := <-l.Reqs():
		c.Assert(ok, Equals, true)
		c.Assert(req.Path(), Equals, path)
	case <-l.Dying():
		c.Fatalf("listener encountered unexpected error: %v", l.Err())
	case <-timer.C:
		c.Fatalf("failed to receive request before timer expired")
	}

	go func() {
		<-sendChan
	}()
}

// Check that if multiple requests are included in a single request buffer from
// the kernel, each will still be handled correctly.
func (*listenerSuite) TestRunMultipleRequestsInBuffer(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	recvChan, sendChan, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl()
	defer restoreEpollIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		c.Assert(t.Wait(), Equals, listener.ErrClosed)
	}()

	t.Go(l.Run)

	label := "snap.foo.bar"
	paths := []string{"/home/Documents/foo", "/path/to/bar", "/baz"}

	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	var megaBuf []byte
	for i, path := range paths {
		msg := newMsgNotificationFile(uint64(i), label, path, aBits, dBits)
		buf, err := msg.MarshalBinary()
		c.Assert(err, IsNil)
		megaBuf = append(megaBuf, buf...)
	}

	recvChan <- megaBuf

	timeout := 100 * time.Millisecond
	timer := time.NewTimer(timeout)

	pathsReceived := make([]string, 0, 3)
	for i := range paths {
		timer.Reset(timeout)
		select {
		case req := <-l.Reqs():
			if !strutil.ListContains(paths, req.Path()) {
				c.Fatalf("received request with unexpected path: %s", req.Path())
			}
			if strutil.ListContains(pathsReceived, req.Path()) {
				c.Fatalf("received request with duplicate path: %s", req.Path())
			}
			pathsReceived = append(pathsReceived, req.Path())
		case <-l.Dying():
			c.Fatalf("listener encountered unexpected error during request %d: %v", i, l.Err())
		case <-timer.C:
			c.Fatalf("failed to receive request %d before timer expired", i)
		}
	}

	go func() {
		c.Check(l.Close(), IsNil)
	}()

	for i := range paths {
		if !timer.Stop() {
			<-timer.C
		}
		timer.Reset(timeout)
		select {
		case resp := <-sendChan:
			c.Assert(resp, Not(HasLen), 0)
		case <-timer.C:
			c.Fatalf("failed to receive response %d before timer expired", i)
		}
	}
}

// Check that the system of epoll event listening works as expected.
func (*listenerSuite) TestRunEpoll(c *C) {
	sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
	c.Assert(err, IsNil)
	notifyFile := os.NewFile(uintptr(sockets[0]), notify.SysPath)
	kernelSocket := sockets[1]

	restoreOpen := listener.MockOsOpen(func(name string) (*os.File, error) {
		c.Assert(name, Equals, notify.SysPath)
		return notifyFile, nil
	})
	defer restoreOpen()

	id := uint64(1234)
	label := "snap.foo.bar"
	path := "/home/Documents/foo/bar"
	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	msg := newMsgNotificationFile(id, label, path, aBits, dBits)
	recvBuf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)

	expectedSend := newMsgNotificationResponse(id, aBits, dBits)
	expectedSendBuf, err := expectedSend.MarshalBinary()
	c.Assert(err, IsNil)

	sendChan := make(chan []byte)

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(fd, Equals, uintptr(notifyFile.Fd()))
		switch req {
		case notify.APPARMOR_NOTIF_SET_FILTER:
			return make([]byte, 0), nil
		case notify.APPARMOR_NOTIF_RECV:
			buf := notify.NewIoctlRequestBuffer()
			n, err := unix.Read(int(fd), buf)
			c.Assert(err, IsNil)
			return buf[:n], nil
		case notify.APPARMOR_NOTIF_SEND:
			sendChan <- buf
			close(sendChan)
			return buf, nil
		}
		return nil, fmt.Errorf("unexpected ioctl request: %v", req)
	})
	defer restoreIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)

	t.Go(l.Run)

	_, err = unix.Write(kernelSocket, recvBuf)
	c.Assert(err, IsNil)

	requestTimer := time.NewTimer(time.Second)
	select {
	case req := <-l.Reqs():
		c.Assert(req.Path(), Equals, path)
	case <-l.Dying():
		c.Fatalf("listener encountered unexpected error: %v", l.Err())
	case <-requestTimer.C:
		c.Fatalf("timed out waiting for listener to send request")
	}

	fakeError := fmt.Errorf("fake error")

	l.Kill(fakeError)

	sendTimer := time.NewTimer(time.Second)
	select {
	case sendBuf := <-sendChan:
		c.Assert(sendBuf, DeepEquals, expectedSendBuf)
	case <-sendTimer.C:
		c.Fatalf("timed out waiting for listener to send response")
	}

	// There is a race between the tomb dying and Close() being called in
	// Run(), so wait for Run() to return before checking closed status.
	c.Assert(t.Wait(), Equals, fakeError)
	c.Assert(l.Close(), Equals, listener.ErrAlreadyClosed)
	// notifyFile should be closed, so closing again should return an error.
	c.Assert(notifyFile.Close(), Not(IsNil))
}

// Check that if no epoll event occurs, listener can still close after an error.
func (*listenerSuite) TestRunNoEpoll(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	restoreEpoll := listener.MockEpollWait(func(l *listener.Listener) ([]epoll.Event, error) {
		for !l.EpollIsClosed() {
		}
		return nil, fmt.Errorf("fake epoll error")
	})
	defer restoreEpoll()

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)
		return make([]byte, 0), nil
	})
	defer restoreIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)

	t.Go(l.Run)

	// Make sure Run() starts before Close()
	time.Sleep(10 * time.Millisecond)

	fakeError := fmt.Errorf("fake error occurred")
	l.Kill(fakeError)

	c.Assert(t.Wait(), Equals, fakeError)
}

// Test that if there is no read from Reqs(), requests are still processed and
// a denial is sent if the listener is closed.
func (*listenerSuite) TestRunNoReceiver(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	recvChan, sendChan, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl()
	defer restoreEpollIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)

	t.Go(l.Run)

	label := "snap.foo.bar"
	paths := []string{"/home/Documents/foo", "/path/to/bar", "/baz"}

	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	for i, path := range paths {
		msg := newMsgNotificationFile(uint64(i), label, path, aBits, dBits)
		buf, err := msg.MarshalBinary()
		c.Assert(err, IsNil)
		recvChan <- buf
	}

	c.Assert(l.Err(), Equals, tomb.ErrStillAlive)
	timer := time.NewTimer(10 * time.Millisecond)
	select {
	case response := <-sendChan:
		c.Fatalf("listener sent unexpected message: %v", response)
	case <-timer.C:
	}

	received := make(chan []byte, 3)
	go func() {
		for range paths {
			timeout := time.NewTimer(100 * time.Millisecond)
			select {
			case resp := <-sendChan:
				received <- resp
			case <-timeout.C:
				c.Fatalf("timed out waiting for listener to send responses")
			}
		}
		close(received)
		unexpected, ok := <-sendChan
		c.Assert(ok, Equals, false, Commentf("listener sent unexpected message: %v", unexpected))
	}()

	c.Check(l.Close(), IsNil)
	c.Check(t.Wait(), Equals, listener.ErrClosed)

	expected := make([][]byte, 0, 3)
	for i := range paths {
		newResp := newMsgNotificationResponse(uint64(i), aBits, dBits)
		respBuf, err := newResp.MarshalBinary()
		c.Assert(err, IsNil)
		expected = append(expected, respBuf)
	}

OUTER:
	for resp := range received {
		for _, exp := range expected {
			if reflect.DeepEqual(resp, exp) {
				continue OUTER
			}
		}
		c.Fatalf("listener sent response which does not match any expected: %v not in %v", resp, expected)
	}
}

func newMsgNotificationFile(id uint64, label, name string, allow, deny uint32) *notify.MsgNotificationFile {
	msg := notify.MsgNotificationFile{}
	msg.Version = 3
	msg.NotificationType = notify.APPARMOR_NOTIF_OP
	msg.NoCache = 1
	msg.ID = id
	msg.Allow = allow
	msg.Deny = deny
	msg.Pid = 1234
	msg.Label = label
	msg.Class = notify.AA_CLASS_FILE
	msg.SUID = 1000
	msg.Name = name
	return &msg
}

func newMsgNotificationResponse(id uint64, allow, deny uint32) *notify.MsgNotificationResponse {
	msgNotification := notify.MsgNotification{
		NotificationType: notify.APPARMOR_NOTIF_RESP,
		NoCache:          1,
		ID:               id,
		Error:            0,
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
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	recvChan, _, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl()
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
			`cannot extract first message: length in header exceeds data length: 1234 > 56`,
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
				Length:           56,
				Version:          3,
				NotificationType: notify.APPARMOR_NOTIF_CANCEL,
			},
			`unsupported notification type: APPARMOR_NOTIF_CANCEL`,
		},
		{
			msgNotificationFile{
				Length:           56,
				Version:          3,
				NotificationType: notify.APPARMOR_NOTIF_OP,
				Class:            uint32(notify.AA_CLASS_DBUS),
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
		}
		return nil, fmt.Errorf("fake epoll error")
	})
	defer restoreEpoll()

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)
		return make([]byte, 0), nil
	})
	defer restoreIoctl()

	var t tomb.Tomb
	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		c.Check(l.Close(), IsNil)
		c.Check(t.Wait(), Equals, listener.ErrClosed)
	}()

	t.Go(l.Run)

	// Make sure the spawned goroutine starts Run() first
	time.Sleep(10 * time.Millisecond)

	err = l.Run()
	c.Assert(err, Equals, listener.ErrAlreadyRun)
}

// Test that calling Run() after Close() does not cause a panic, as Close()
// kills the internal tomb
func (*listenerSuite) TestCloseThenRun(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	restoreIoctl := listener.MockNotifyIoctl(func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		c.Assert(req, Equals, notify.APPARMOR_NOTIF_SET_FILTER)
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
	c.Assert(err, Equals, listener.ErrAlreadyClosed)
}

// Test that waiters send back a deny message if the tomb dies as a result of
// an error, and that that message makes it through the notify socket before it
// is closed.
func (*listenerSuite) TestRunTombDying(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	recvChan, sendChan, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl()
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

	msg := newMsgNotificationFile(id, label, path, aBits, dBits)
	buf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	recvChan <- buf

	select {
	case <-l.Reqs():
	case <-l.Dying():
		c.Fatalf("listener encountered unexpected error: %v", l.Err())
	}

	// Build desired message (denial)
	resp := newMsgNotificationResponse(id, aBits, dBits)
	desiredBuf, err := resp.MarshalBinary()
	c.Assert(err, IsNil)

	// Cause an internal error
	fakeError := fmt.Errorf("fake error occurred")
	l.Kill(fakeError)

	received := <-sendChan
	c.Assert(received, DeepEquals, desiredBuf)

	err = t.Wait()
	c.Assert(err, Equals, fakeError)
}

func (*listenerSuite) TestRunListenerClosed(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	recvChan, sendChan, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl()
	defer restoreEpollIoctl()

	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		err = l.Close()
		c.Assert(err, Equals, listener.ErrAlreadyClosed)
	}()

	var t tomb.Tomb
	t.Go(l.Run)

	id := uint64(1234)
	label := "snap.foo.bar"
	path := "/home/Documents/foo"

	aBits := uint32(0b1010)
	dBits := uint32(0b0101)

	msg := newMsgNotificationFile(id, label, path, aBits, dBits)
	buf, err := msg.MarshalBinary()
	c.Assert(err, IsNil)
	recvChan <- buf

	var req *listener.Request
	select {
	case req = <-l.Reqs():
		c.Assert(req.PID(), Equals, msg.Pid)
		c.Assert(req.Label(), Equals, label)
		c.Assert(req.SubjectUID(), Equals, msg.SUID)
		c.Assert(req.Path(), Equals, path)
		c.Assert(req.Class(), Equals, notify.AA_CLASS_FILE)
		perm, ok := req.Permission().(notify.FilePermission)
		c.Assert(ok, Equals, true)
		c.Assert(perm, Equals, notify.FilePermission(dBits))
	case <-l.Dying():
		c.Fatalf("listener encountered unexpected error: %v", l.Err())
	}

	go func() {
		// Check that listener sends deny response as a result of being closed
		resp := newMsgNotificationResponse(id, aBits, dBits)
		desiredBuf, err := resp.MarshalBinary()
		c.Assert(err, IsNil)

		received := <-sendChan
		c.Assert(received, DeepEquals, desiredBuf)
	}()

	// Close listener before sending back allow response
	err = l.Close()
	c.Assert(err, IsNil)

	err = req.Reply(true)
	c.Assert(err, IsNil)

	err = t.Wait()
	c.Assert(err, Equals, listener.ErrClosed)
}

func (*listenerSuite) TestRunConcurrency(c *C) {
	restoreOpen := listener.MockOsOpenWithSocket()
	defer restoreOpen()

	recvChan, sendChan, restoreEpollIoctl := listener.MockEpollWaitNotifyIoctl()
	defer restoreEpollIoctl()

	l, err := listener.Register()
	c.Assert(err, IsNil)
	defer func() {
		err = l.Close()
		c.Check(err, Equals, listener.ErrAlreadyClosed)
	}()

	var t tomb.Tomb
	t.Go(l.Run)

	label := "snap.foo.bar"
	path := "/home/Documents/foo"
	reqAllow := uint32(0b1010)
	reqDeny := uint32(0b0101)

	msg := newMsgNotificationFile(0, label, path, reqAllow, reqDeny)

	respAllow := uint32(0b1111)
	respDeny := uint32(0b0000)
	resp := newMsgNotificationResponse(0, respAllow, respDeny)

	templateBuf, err := resp.MarshalBinary()
	c.Assert(err, IsNil)
	expectedLen := len(templateBuf)

	closeTimeout := 50 * time.Millisecond
	closeTimer := time.NewTimer(closeTimeout)
	createTimer := time.NewTimer(2 * closeTimeout)

	safeToReturn := make(chan struct{})
	go func() {
		// create requests until createTimer expires
		id := uint64(0)
		for {
			id += 1
			msg.ID = id
			buf, err := msg.MarshalBinary()
			c.Assert(err, IsNil)
			select {
			case <-createTimer.C:
			case recvChan <- buf:
				continue
			}
			break
		}
		close(safeToReturn)
		c.Logf("total requests sent: %d", id)
	}()

	go func() {
		// reply to all requests as they are received, until l.Reqs() closes
		replyCount := 0
		for req := range l.Reqs() {
			err := req.Reply(true)
			c.Check(err, IsNil)
			replyCount += 1
		}
		c.Logf("total replies sent: %d", replyCount)
	}()

	go func() {
		// Consume responses from the sendChan (in place of reading from the
		// actual FD). No guarantee of order, so just check that the length is
		// correct, rather than picking apart the buffer to throw out the ID and
		// compare the rest.
		responseCount := 0
		for response := range sendChan {
			c.Check(response, HasLen, expectedLen)
			responseCount += 1
		}
		c.Logf("total responses received: %d", responseCount)
	}()

	<-closeTimer.C

	// Check that no error has yet occurred
	c.Check(l.Err(), Equals, tomb.ErrStillAlive)

	// Check that closing the listener while creating and replying to requests
	// does not cause a panic (e.g. by writing to a closed channel)
	c.Check(l.Close(), IsNil)
	c.Check(t.Wait(), Equals, listener.ErrClosed)

	// Restoring functions closes channels, so make sure to wait until done
	// creating requests before closing the channels.
	<-safeToReturn
}
