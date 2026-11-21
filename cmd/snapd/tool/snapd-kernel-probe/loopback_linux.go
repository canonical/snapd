//go:build linux

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

package snapd_kernel_probe

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// For mocking in tests.
var raiseLoopback = raiseLoopbackInOwnNetns

// raiseLoopbackInOwnNetns sets IFF_UP on the "lo" interface of the current
// network namespace.
//
// A new network namespace holds a single, down, address-less "lo". Bringing it
// up is all that is needed: the kernel then adds 127.0.0.1/8 and ::1/128 and
// the matching local routes synchronously, so the address is usable as soon
// as the ioctl returns.
func raiseLoopbackInOwnNetns() error {
	if err := ensureOwnNetns("/proc/self/ns/net", fmt.Sprintf("/proc/%d/ns/net", os.Getppid())); err != nil {
		return err
	}

	// Any AF_INET socket is a valid handle for the SIOC*IFFLAGS ioctls, which
	// apply to the network namespace of the socket.
	fd, err := unix.Socket(unix.AF_INET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		return fmt.Errorf("cannot bring up loopback: %v", err)
	}
	defer unix.Close(fd)

	ifr, err := unix.NewIfreq("lo")
	if err != nil {
		return fmt.Errorf("cannot bring up loopback: %v", err)
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, ifr); err != nil {
		return fmt.Errorf("cannot bring up loopback: cannot get flags: %v", err)
	}
	ifr.SetUint16(ifr.Uint16() | unix.IFF_UP)
	if err := unix.IoctlIfreq(fd, unix.SIOCSIFFLAGS, ifr); err != nil {
		return fmt.Errorf("cannot bring up loopback: cannot set flags: %v", err)
	}
	if err := unix.IoctlIfreq(fd, unix.SIOCGIFFLAGS, ifr); err != nil {
		return fmt.Errorf("cannot bring up loopback: cannot get flags: %v", err)
	}
	if ifr.Uint16()&unix.IFF_UP == 0 {
		return fmt.Errorf("cannot bring up loopback: interface is not up")
	}
	return nil
}
