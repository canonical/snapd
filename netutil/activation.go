// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2019 Canonical Ltd
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

package netutil

import (
	"fmt"
	"net"
	"os"
	"runtime"
	unix "syscall"

	"github.com/coreos/go-systemd/activation"

	"github.com/snapcore/snapd/logger"
)

// GetListener tries to get a listener for the given socket path from
// the listener map, and if it fails it tries to set it up directly.
func GetListener(socketPath string, listenerMap map[string]net.Listener) (net.Listener, error) {
	if listener, ok := listenerMap[socketPath]; ok {
		return listener, nil
	}

	if c, err := net.Dial("unix", socketPath); err == nil {
		c.Close()
		return nil, fmt.Errorf("socket %q already in use", socketPath)
	}

	if err := os.Remove(socketPath); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	address, err := net.ResolveUnixAddr("unix", socketPath)
	if err != nil {
		return nil, err
	}

	runtime.LockOSThread()
	oldmask := unix.Umask(0111)
	listener, err := net.ListenUnix("unix", address)
	unix.Umask(oldmask)
	runtime.UnlockOSThread()
	if err != nil {
		return nil, err
	}

	logger.Debugf("socket %q was not activated; listening", socketPath)

	return listener, nil
}

// ActivationListeners builds a map of addresses to listeners that were passed
// during systemd activation
func ActivationListeners() (lns map[string]net.Listener, err error) {
	// pass false to keep LISTEN_* environment variables passed by systemd
	files := activation.Files(false)
	lns = make(map[string]net.Listener, len(files))

	for _, f := range files {
		ln, err := net.FileListener(f)
		if err != nil {
			return nil, err
		}
		addr := ln.Addr().String()
		lns[addr] = ln
	}
	return lns, nil
}
