// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

/*
 * Copyright (C) 2017-2026 Canonical Ltd
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
	"runtime"
	"strings"
	"sync"

	"golang.org/x/sys/unix"
)

var (
	sdNotifyCache sdNotifyConnCache
)

type sdNotifyConnCache struct {
	conn *net.UnixConn
	mu   sync.Mutex
}

// Lock acquires the cache lock.
func (c *sdNotifyConnCache) Lock() {
	c.mu.Lock()
}

// Unlock releases the cache lock.
func (c *sdNotifyConnCache) Unlock() {
	c.mu.Unlock()
}

func (c *sdNotifyConnCache) assertLocked() {
	if c.mu.TryLock() {
		c.mu.Unlock()
		panic("internal error: sdNotifyConnCache lock is not held")
	}
}

// Close should be called with the lock held.
func (c *sdNotifyConnCache) Close() {
	c.assertLocked()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

// Conn should be called with the lock held.
func (c *sdNotifyConnCache) Conn() (*net.UnixConn, error) {
	c.assertLocked()

	if c.conn != nil {
		return c.conn, nil
	}

	notifySocket := NotifySocket()
	if notifySocket == "" {
		return nil, fmt.Errorf("cannot find NOTIFY_SOCKET environment variable")
	}
	if !strings.HasPrefix(notifySocket, "@") && !strings.HasPrefix(notifySocket, "/") {
		return nil, fmt.Errorf("cannot use NOTIFY_SOCKET %q", notifySocket)
	}

	raddr := &net.UnixAddr{
		Name: notifySocket,
		Net:  "unixgram",
	}
	// net.DialUnix opens the socket with SOCK_CLOEXEC.
	conn, err := net.DialUnix("unixgram", nil, raddr)
	if err != nil {
		return nil, err
	}
	c.conn = conn
	return conn, nil
}

// SdNotify sends the given state string notification to systemd.
//
// inspired by libsystemd/sd-daemon/sd-daemon.c from the systemd source
func SdNotify(notifyState string) error {
	if notifyState == "" {
		return fmt.Errorf("invalid empty notify state")
	}

	sdNotifyCache.Lock()
	defer sdNotifyCache.Unlock()

	conn, err := sdNotifyCache.Conn()
	if err != nil {
		return err
	}

	if _, err := conn.Write([]byte(notifyState)); err != nil {
		// UNIXGRAM sockets are connectionless, so an error here likely indicates
		// that the socket is no longer valid. We drop the cached connection to
		// ensure the next call reconnects.
		sdNotifyCache.Close()
		return err
	}
	return nil
}

// SdNotifyWithFds sends the given state string notification and file
// descriptors associated with passed files to systemd.
//
// Caller is responsible for closing the passed files.
//
// inspired by libsystemd/sd-daemon/sd-daemon.c from the systemd source
func SdNotifyWithFds(notifyState string, files ...*os.File) error {
	if notifyState == "" {
		return fmt.Errorf("invalid empty notify state")
	}

	if len(files) == 0 {
		return fmt.Errorf("at least one file is required")
	}

	sdNotifyCache.Lock()
	defer sdNotifyCache.Unlock()

	conn, err := sdNotifyCache.Conn()
	if err != nil {
		return err
	}

	rawConn, err := conn.SyscallConn()
	if err != nil {
		return err
	}

	var sendMsgErr error
	err = rawConn.Control(func(sdNotifyFd uintptr) {
		fds := make([]int, len(files))
		for i := range files {
			fds[i] = int(files[i].Fd())
		}

		rights := unix.UnixRights(fds...)
		creds := unix.UnixCredentials(&unix.Ucred{
			Pid: int32(os.Getpid()),
			Uid: uint32(os.Getuid()),
			Gid: uint32(os.Getgid()),
		})
		oob := append(creds, rights...)
		sendMsgErr = unix.Sendmsg(int(sdNotifyFd), []byte(notifyState), oob, nil, 0)

		// Ensure finalizer is not called for passed files.
		runtime.KeepAlive(files)
	})
	if err != nil {
		return err
	}

	if sendMsgErr != nil {
		// UNIXGRAM sockets are connectionless, so an error here likely indicates
		// that the socket is no longer valid. We drop the cached connection to
		// ensure the next call reconnects.
		sdNotifyCache.Close()
	}
	return sendMsgErr
}
