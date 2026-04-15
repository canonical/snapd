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

	"github.com/coreos/go-systemd/activation"

	"github.com/snapcore/snapd/logger"
)

// GetListener tries to get a listener for the given socket path from the
// listener map, and if it fails it tries to set it up directly. The socket, if
// needed to be created, is owned by the current user and its mode is set to
// 0666. Upon returning, the caller can change to mode using os.Chmod() if
// required.
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

	listener, err := net.ListenUnix("unix", address)
	if err != nil {
		return nil, err
	}
	// if we reached here, the socket was clearly not in the set passed as
	// activation sockets. It is owned by current user, but its mode is 0777 &
	// ~umask. Update the mode to same value as systemd's default (0666). The
	// caller knows the path and can adjust the mode to their liking.
	if err := os.Chmod(socketPath, 0666); err != nil {
		listener.Close()
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
