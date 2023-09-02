// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

	"github.com/snapcore/snapd/sandbox/apparmor/notify"
	"github.com/snapcore/snapd/testutil"
)

func MockOsOpen(f func(name string) (*os.File, error)) (restore func()) {
	restore = testutil.Backup(&osOpen)
	osOpen = f
	return restore
}

// Mocks os.Open to instead create a socket pair, return one wrapped in a
// os.File (in place of the opened file), and send the other along the
// sockFdChan which is returned by this function.
func MockOsOpenWithSockets() (sockFdChan chan int, restore func()) {
	sockFdChan = make(chan int, 1)
	f := func(name string) (*os.File, error) {
		sockets, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM, 0)
		if err != nil {
			return nil, err
		}
		senderSocket := sockets[0]
		receiverSocket := sockets[1]
		notifyFile := os.NewFile(uintptr(receiverSocket), notify.SysPath)
		sockFdChan <- senderSocket
		return notifyFile, nil
	}
	restore = MockOsOpen(f)
	return sockFdChan, restore
}

func MockNotifyIoctl(f func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error)) (restore func()) {
	restore = testutil.Backup(&notifyIoctl)
	notifyIoctl = f
	return restore
}

// Mocks notify.Ioctl calls by performing Read in place of RECV and Write in
// place of SEND, and ignoring other IoctlRequest types.
func MockNotifyIoctlWithReadWrite() (restore func()) {
	f := func(fd uintptr, req notify.IoctlRequest, buf notify.IoctlRequestBuffer) ([]byte, error) {
		size := 0
		var err error = nil
		switch req {
		case notify.APPARMOR_NOTIF_RECV:
			size, err = unix.Read(int(fd), buf)
		case notify.APPARMOR_NOTIF_SEND:
			size, err = unix.Write(int(fd), buf)
		default:
			// ignore other IoctlRequest types
		}
		if size >= 0 && size <= len(buf) {
			buf = buf[:size]
		}
		return buf, err
	}
	return MockNotifyIoctl(f)
}
