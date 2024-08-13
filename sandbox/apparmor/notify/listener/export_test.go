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

	"golang.org/x/sys/unix"

	"github.com/snapcore/snapd/osutil/epoll"
	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/testutil"
)

func ExitOnError() (restore func()) {
	restore = testutil.Backup(&exitOnError)
	exitOnError = true
	return restore
}

func FakeRequestWithClassAndReplyChan(class notify.MediationClass, replyChan chan *Response) *Request {
	return &Request{
		Class:     class,
		replyChan: replyChan,
	}
}

func MockOsOpen(f func(name string) (*os.File, error)) (restore func()) {
	restore = testutil.Backup(&osOpen)
	osOpen = f
	return restore
}

// Mocks os.Open to instead create a socket, wrap it in a os.File, and return
// it to the caller.
func MockOsOpenWithSocket() (restore func()) {
	f := func(name string) (*os.File, error) {
		socket, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		if err != nil {
			return nil, err
		}
		notifyFile := os.NewFile(uintptr(socket), notify.SysPath)
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

func MockNotifyIoctl(f func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error)) (restore func()) {
	restore = testutil.Backup(&notifyIoctl)
	notifyIoctl = f
	return restore
}

// Mocks epoll.Wait and notify.Ioctl calls by sending data over channels.
// When data is sent over the recv channel (to be consumed by a mocked ioctl
// call), it triggers an epoll event with the listener's notify socket fd, and
// then passes the data on to the next ioctl RECV call. When the listener makes
// a SEND call via ioctl, the data is instead written to the send channel.
func MockEpollWaitNotifyIoctl() (recvChan chan<- []byte, sendChan <-chan []byte, restore func()) {
	recvChanRW := make(chan []byte)
	sendChanRW := make(chan []byte)
	internalRecvChan := make(chan []byte, 1)
	ef := func(l *Listener) ([]epoll.Event, error) {
		for {
			select {
			case request := <-recvChanRW:
				internalRecvChan <- request
				events := []epoll.Event{
					{
						Fd:        int(l.notifyFile.Fd()),
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
	nf := func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
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
	restoreEpoll := testutil.Backup(&listenerEpollWait)
	listenerEpollWait = ef
	restoreIoctl := testutil.Backup(&notifyIoctl)
	notifyIoctl = nf
	restore = func() {
		restoreEpoll()
		restoreIoctl()
		close(recvChanRW)
		close(sendChanRW)
	}
	return recvChanRW, sendChanRW, restore
}

func (l *Listener) Dead() <-chan struct{} {
	return l.tomb.Dead()
}

func (l *Listener) Dying() <-chan struct{} {
	return l.tomb.Dying()
}

func (l *Listener) Err() error {
	return l.tomb.Err()
}

func (l *Listener) Kill(err error) {
	l.tomb.Kill(err)
}

func (l *Listener) EpollIsClosed() bool {
	return l.poll.IsClosed()
}
