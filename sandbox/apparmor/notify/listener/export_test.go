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
	"fmt"
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
)

func ListenerWithProtocolVersion[R any](version notify.ProtocolVersion) *Listener[R] {
	return &Listener[R]{
		protocolVersion: version,
	}
}

func ExitOnError() (restore func()) {
	restore = testutil.Backup(&exitOnError)
	exitOnError = true
	return restore
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

type EpollWaiter = epollWaiter

func MockEpollWaitForClose() (restore func()) {
	closeChan := make(chan struct{})
	restoreWait := testutil.Mock(&listenerEpollWait, func(l epollWaiter) ([]epoll.Event, error) {
		<-closeChan
		// The listenerEpollWait() error will cause handleRequests() to check
		// whether the listener has been closed, and if so, return ErrClosed.
		// Thus, return an arbitrary error here to make sure that ErrClosed is
		// returned instead.
		return nil, fmt.Errorf("fake epoll error")
	})
	restoreClose := testutil.Mock(&listenerEpollClose, func(l epollWaiter) error {
		close(closeChan)
		return nil
	})
	return func() {
		restoreWait()
		restoreClose()
	}
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

// Mocks epoll.Wait, epoll.Close, notify.Ioctl, and notify.RegisterFileDescriptor
// calls by sending data over channels, using the given version as the protocol
// version for the listener.
//
// When data is sent over the recv channel (to be consumed by a mocked ioctl
// call), it triggers an epoll event with the listener's notify socket fd, and
// then passes the data on to the next ioctl RECV call. When the listener makes
// a SEND call via ioctl, the data is instead written to the send channel.
func MockEpollWaitNotifyIoctl(protoVersion notify.ProtocolVersion, pendingCount int) (recvChan chan<- []byte, sendChan <-chan []byte, restore func()) {
	recvChanRW := make(chan []byte)
	sendChanRW := make(chan []byte, 1) // need to have buffer size 1 since reply does not run in a goroutine and the test would otherwise block
	internalRecvChan := make(chan []byte, 1)
	closeChan := make(chan struct{})
	epollCloseF := func(l epollWaiter) error {
		close(closeChan)
		return nil
	}
	epollWaitF := func(l epollWaiter) ([]epoll.Event, error) {
		// In the real listener, the epoll instance has its own FD and we don't
		// need to get the notify FD, but here, we need to get the notify FD
		// directly from the listener.
		socketFd := l.socketFD()

		select {
		case request := <-recvChanRW:
			select {
			case internalRecvChan <- request:
				// all good
			case <-time.NewTimer(5 * time.Second).C:
				panic("timed out trying to send request to internalRecvChan")
			}
			events := []epoll.Event{
				{
					Fd:        socketFd,
					Readiness: epoll.Readable,
				},
			}
			return events, nil
		case <-closeChan:
			return nil, epoll.ErrEpollClosed
		}
	}
	ioctlF := func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		switch req {
		case notify.APPARMOR_NOTIF_RECV:
			select {
			case request := <-internalRecvChan:
				return request, nil
			case <-time.NewTimer(5 * time.Second).C:
				panic("timed out waiting for request from internalRecvChan")
			}
		case notify.APPARMOR_NOTIF_SEND:
			select {
			case sendChanRW <- buf:
				// all good
			case <-time.NewTimer(5 * time.Second).C:
				panic("timed out trying to send buf over sendChanRW")
			}
		default:
			// ignore other IoctlRequest types
		}
		return buf, nil
	}
	rfdF := func(fd uintptr) (notify.ProtocolVersion, int, error) {
		return protoVersion, pendingCount, nil
	}
	restoreEpollClose := testutil.Mock(&listenerEpollClose, epollCloseF)
	restoreEpollWait := testutil.Mock(&listenerEpollWait, epollWaitF)
	restoreIoctl := testutil.Mock(&notifyIoctl, ioctlF)
	restoreRegisterFileDescriptor := testutil.Mock(&notifyRegisterFileDescriptor, rfdF)

	restore = func() {
		restoreEpollClose()
		restoreEpollWait()
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

func MockTimeAfterFunc(f func(d time.Duration, callback func()) timeutil.Timer) (restore func()) {
	return testutil.Mock(&timeAfterFunc, f)
}
