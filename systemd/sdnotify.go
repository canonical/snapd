// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2025 Canonical Ltd
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

package systemd

import (
	"fmt"
	"net"
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

var osGetenv = os.Getenv

// SdNotify sends the given state string notification to systemd.
//
// inspired by libsystemd/sd-daemon/sd-daemon.c from the systemd source
var SdNotify = sdNotifyImpl

func sdNotifyImpl(notifyState string) error {
	if notifyState == "" {
		return fmt.Errorf("cannot use empty notify state")
	}

	conn, err := sdNotifyConn()
	if err != nil {
		return err
	}
	defer conn.Close()

	_, err = conn.Write([]byte(notifyState))
	return err
}

// SdNotifyWithFds sends the given state string notification and file
// descriptors to systemd.
//
// inspired by libsystemd/sd-daemon/sd-daemon.c from the systemd source
var SdNotifyWithFds = sdNotifyWithFdsImpl

func sdNotifyWithFdsImpl(notifyState string, fds ...int) error {
	if notifyState == "" {
		return fmt.Errorf("cannot use empty notify state")
	}

	if len(fds) == 0 {
		return fmt.Errorf("at least one file descriptor is required")
	}

	conn, err := sdNotifyConn()
	if err != nil {
		return err
	}
	defer conn.Close()

	rawConn, err := conn.SyscallConn()
	if err != nil {
		return err
	}

	var sendMsgErr error
	err = rawConn.Control(func(sdNotifyFd uintptr) {
		rights := unix.UnixRights(fds...)
		creds := unix.UnixCredentials(&unix.Ucred{
			Pid: int32(os.Getpid()),
			Uid: uint32(os.Getuid()),
			Gid: uint32(os.Getgid()),
		})
		oob := append(creds, rights...)
		sendMsgErr = unix.Sendmsg(int(sdNotifyFd), []byte(notifyState), oob, nil, 0)
	})
	if err != nil {
		return err
	}
	if sendMsgErr != nil {
		return sendMsgErr
	}

	return nil
}

func sdNotifyConn() (*net.UnixConn, error) {
	notifySocket := osGetenv("NOTIFY_SOCKET")
	if notifySocket == "" {
		return nil, fmt.Errorf("cannot find NOTIFY_SOCKET environment")
	}
	if !strings.HasPrefix(notifySocket, "@") && !strings.HasPrefix(notifySocket, "/") {
		return nil, fmt.Errorf("cannot use NOTIFY_SOCKET %q", notifySocket)
	}

	raddr := &net.UnixAddr{
		Name: notifySocket,
		Net:  "unixgram",
	}
	return net.DialUnix("unixgram", nil, raddr)
}
