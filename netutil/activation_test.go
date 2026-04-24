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

package netutil_test

import (
	"net"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/netutil"
)

func Test(t *testing.T) { TestingT(t) }

type activationSuite struct{}

var _ = Suite(&activationSuite{})

func (s *activationSuite) TestGetListenerSocketMode(c *C) {
	socketPath := filepath.Join(c.MkDir(), "test.sock")

	// create a socket at the path and set its mode to 0644. The
	// listener is closed via SetUnlinkOnClose(false) to keep the
	// stale socket file on disk so we can later verify that
	// GetListener recreates it with mode 0666.
	addr, err := net.ResolveUnixAddr("unix", socketPath)
	c.Assert(err, IsNil)
	stale, err := net.ListenUnix("unix", addr)
	c.Assert(err, IsNil)
	stale.SetUnlinkOnClose(false)
	c.Assert(stale.Close(), IsNil)

	err = os.Chmod(socketPath, 0644)
	c.Assert(err, IsNil)

	// GetListener should remove the stale socket, create a new one
	// and chmod it to 0666.
	listener, err := netutil.GetListener(socketPath, nil)
	c.Assert(err, IsNil)
	defer listener.Close()

	c.Check(listener.Addr().String(), Equals, socketPath)

	fi, err := os.Stat(socketPath)
	c.Assert(err, IsNil)
	c.Check(fi.Mode().Perm(), Equals, os.FileMode(0666))
}

func (s *activationSuite) TestGetListenerSocketAlreadyInUse(c *C) {
	socketPath := filepath.Join(c.MkDir(), "test.sock")

	// Create a listener and keep it open so the socket is actively
	// in use.
	addr, err := net.ResolveUnixAddr("unix", socketPath)
	c.Assert(err, IsNil)
	active, err := net.ListenUnix("unix", addr)
	c.Assert(err, IsNil)
	defer active.Close()

	// GetListener should detect the active socket via Dial and
	// return an error.
	listener, err := netutil.GetListener(socketPath, nil)
	c.Assert(err, ErrorMatches, `socket ".*" already in use`)
	c.Assert(listener, IsNil)
}

func (s *activationSuite) TestGetListenerFromListenerMap(c *C) {
	socketPath := filepath.Join(c.MkDir(), "test.sock")

	// Create a listener and register it in the listener map, as
	// systemd activation would.
	addr, err := net.ResolveUnixAddr("unix", socketPath)
	c.Assert(err, IsNil)
	existing, err := net.ListenUnix("unix", addr)
	c.Assert(err, IsNil)
	defer existing.Close()

	listenerMap := map[string]net.Listener{
		socketPath: existing,
	}

	// GetListener should return the listener from the map directly.
	listener, err := netutil.GetListener(socketPath, listenerMap)
	c.Assert(err, IsNil)
	c.Assert(listener, Equals, existing)
}
