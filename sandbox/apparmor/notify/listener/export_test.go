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

package listener

import (
	"os"
	"time"

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/osutil/epoll"
	"github.com/snapcore/snapd/sandbox/apparmor"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timeutil"
)

var (
	ReadyTimeout = readyTimeout
	NewRequest   = (*Listener).newRequest
)

func ExitOnError() (restore func()) {
	restore = testutil.Backup(&exitOnError)
	exitOnError = true
	return restore
}

func FakeRequestWithIDVersionAllowDenyIfacePerms(id uint64, version notify.ProtocolVersion, aaAllow, aaDeny notify.FilePermission, iface string, perms []string) *Request {
	l := &Listener{
		protocolVersion: version,
	}
	return &Request{
		id:             id,
		aaRequested:    aaDeny,
		aaAllowed:      aaAllow,
		iface:          iface,
		requestedPerms: perms,
		listener:       l,
	}
}

func MockOsOpen(f func(name string) (*os.File, error)) (restore func()) {
	return testutil.Mock(&osOpen, f)
}

// Mocks os.Open to instead create a socket, wrap it in a os.File, and return
// it to the caller.
func MockOsOpenWithSocket() (restore func()) {
	f := func(name string) (*os.File, error) {
		socket, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		if err != nil {
			return nil, err
		}
		notifyFile := os.NewFile(uintptr(socket), apparmor.NotifySocketPath)
		return notifyFile, nil
	}
	restore = MockOsOpen(f)
	return restore
}

func MockEpollWait(f func(l *Listener) ([]epoll.Event, error)) (restore func()) {
	restore = testutil.Backup(&listenerEpollWait)
	listenerEpollWait = f
	return restore
}

func MockNotifyRegisterFileDescriptor(f func(fd uintptr) (notify.ProtocolVersion, int, error)) (restore func()) {
	restore = testutil.Backup(&notifyRegisterFileDescriptor)
	notifyRegisterFileDescriptor = f
	return restore
}

func MockNotifyIoctl(f func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error)) (restore func()) {
	restore = testutil.Backup(&notifyIoctl)
	notifyIoctl = f
	return restore
}

// Mocks epoll.Wait, notify.Ioctl, and notify.RegisterFileDescriptor calls by
// sending data over channels, using the given version as the protocol version
// for the listener.
//
// When data is sent over the recv channel (to be consumed by a mocked ioctl
// call), it triggers an epoll event with the listener's notify socket fd, and
// then passes the data on to the next ioctl RECV call. When the listener makes
// a SEND call via ioctl, the data is instead written to the send channel.
func MockEpollWaitNotifyIoctl(protoVersion notify.ProtocolVersion, pendingCount int) (recvChan chan<- []byte, sendChan <-chan []byte, restore func()) {
	recvChanRW := make(chan []byte)
	sendChanRW := make(chan []byte, 1) // need to have buffer size 1 since reply does not run in a goroutine and the test would otherwise block
	internalRecvChan := make(chan []byte, 1)
	epollF := func(l *Listener) ([]epoll.Event, error) {
		// In the real listener, the epoll instance has its own FD and we don't
		// need to get the notify FD, but here, we get the notify FD directly,
		// so we need to get it with the socket mutex held to avoid a race.
		l.socketMu.Lock()
		socketFd := int(l.notifyFile.Fd())
		l.socketMu.Unlock()
		for {
			select {
			case request := <-recvChanRW:
				internalRecvChan <- request
				events := []epoll.Event{
					{
						Fd:        socketFd,
						Readiness: epoll.Readable,
					},
				}
				return events, nil
			default:
				if l.poll.IsClosed() {
					return nil, epoll.ErrEpollClosed
				}
			}
		}
	}
	ioctlF := func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		switch req {
		case notify.APPARMOR_NOTIF_RECV:
			request := <-internalRecvChan
			return request, nil
		case notify.APPARMOR_NOTIF_SEND:
			sendChanRW <- buf
		default:
			// ignore other IoctlRequest types
		}
		return buf, nil
	}
	rfdF := func(fd uintptr) (notify.ProtocolVersion, int, error) {
		return protoVersion, pendingCount, nil
	}
	restoreEpoll := testutil.Mock(&listenerEpollWait, epollF)
	restoreIoctl := testutil.Mock(&notifyIoctl, ioctlF)
	restoreRegisterFileDescriptor := testutil.Mock(&notifyRegisterFileDescriptor, rfdF)

	restore = func() {
		restoreEpoll()
		restoreIoctl()
		restoreRegisterFileDescriptor()
		close(recvChanRW)
		close(sendChanRW)
	}
	return recvChanRW, sendChanRW, restore
}

// Return a blocking channel over which a IoctlRequest type will be sent
// whenever notifyIoctl returns.
func SynchronizeNotifyIoctl() (ioctlDone <-chan notify.IoctlRequest, restore func()) {
	ioctlDoneRW := make(chan notify.IoctlRequest)
	realIoctl := notifyIoctl
	restore = testutil.Mock(&notifyIoctl, func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		ret, err := realIoctl(fd, req, buf)
		ioctlDoneRW <- req // synchronize
		return ret, err
	})
	return ioctlDoneRW, restore
}

func MockCgroupProcessPathInTrackingCgroup(f func(pid int) (string, error)) (restore func()) {
	return testutil.Mock(&cgroupProcessPathInTrackingCgroup, f)
}

func MockPromptingInterfaceFromTagsets(f func(tm notify.TagsetMap) (string, error)) (restore func()) {
	return testutil.Mock(&promptingInterfaceFromTagsets, f)
}

func MockEncodeAndSendResponse(f func(l *Listener, resp *notify.MsgNotificationResponse) error) (restore func()) {
	return testutil.Mock(&encodeAndSendResponse, f)
}

func (l *Listener) EpollIsClosed() bool {
	return l.poll.IsClosed()
}

func MockTimeAfterFunc(f func(d time.Duration, callback func()) timeutil.Timer) (restore func()) {
	return testutil.Mock(&timeAfterFunc, f)
}
