// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build linux

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
 */

package userbus

import (
	"fmt"
	"net"
	"os"

	"golang.org/x/sys/unix"
)

func socketPair() (*net.UnixConn, *os.File, error) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_STREAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return nil, nil, err
	}
	parent := os.NewFile(uintptr(fds[0]), "userbus-parent")
	child := os.NewFile(uintptr(fds[1]), "userbus-child")
	conn, err := fileConn(parent)
	if err != nil {
		child.Close()
		return nil, nil, err
	}
	return conn, child, nil
}

// fileConn takes ownership of file and returns a separately owned connection.
func fileConn(file *os.File) (*net.UnixConn, error) {
	defer file.Close()
	conn, err := net.FileConn(file)
	if err != nil {
		return nil, err
	}
	uc, ok := conn.(*net.UnixConn)
	if !ok {
		conn.Close()
		return nil, fmt.Errorf("expected a UNIX socket")
	}
	return uc, nil
}
