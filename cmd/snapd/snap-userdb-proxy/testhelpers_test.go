// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package main_test

import (
	"net"
	"os"
	"syscall"

	. "gopkg.in/check.v1"
)

// unixSocketpair returns a pair of connected AF_UNIX SOCK_STREAM file
// descriptors, mirroring the socketpair(2) syscall.
func unixSocketpair() ([2]int, error) {
	return syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
}

// netConnFromFd wraps a raw file descriptor as a net.Conn (specifically
// a *net.UnixConn) so tests can hand it to peer-identity checks.
func netConnFromFd(c *C, fd int, name string) net.Conn {
	f := os.NewFile(uintptr(fd), name)
	c.Assert(f, NotNil)
	conn, err := net.FileConn(f)
	c.Assert(err, IsNil)
	// FileConn dups the descriptor; close ours to avoid leaks.
	c.Assert(f.Close(), IsNil)
	return conn
}
