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

package daemon_test

import (
	"fmt"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/seclog"
)

type seclogSuite struct{}

var _ = Suite(&seclogSuite{})

func (s *seclogSuite) TestSeclogPeerFromUcredNil(c *C) {
	peer := daemon.SeclogPeerFromUcred(nil)

	c.Check(peer, DeepEquals, seclog.Peer{
		UID: seclog.PeerNobody,
		PID: seclog.PeerNoProcess,
	})
	c.Check(peer.String(), Equals, "<unknown>:<unknown>:<unknown>")
}

func (s *seclogSuite) TestSeclogPeerFromUcredEnrichment(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(path string) (string, error) {
		c.Check(path, Equals, filepath.Join(dirs.GlobalRootDir, "proc/4242/exe"))
		return "/usr/bin/snap", nil
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockApparmorSecurityLabelFromPid(func(pid int) (string, error) {
		c.Check(pid, Equals, 4242)
		return "snap.firefox.firefox", nil
	})
	defer restoreLabels()

	restoreCgroup := daemon.MockCgroupPathFromPid(func(pid int) (string, error) {
		c.Check(pid, Equals, 4242)
		return "/system.slice/snap.firefox.firefox.service", nil
	})
	defer restoreCgroup()

	peer := daemon.SeclogPeerFromUcred(&daemon.Ucrednet{
		Socket: "/run/snapd.socket",
		Uid:    1000,
		Pid:    4242,
	})

	c.Check(peer, DeepEquals, seclog.Peer{
		Socket:   "/run/snapd.socket",
		UID:      1000,
		PID:      4242,
		Exe:      "/usr/bin/snap",
		Snap:     "firefox",
		Runnable: "app.firefox",
	})
}

func (s *seclogSuite) TestSeclogPeerFromUcredUnconfined(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "/usr/bin/curl", nil
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "unconfined", nil
	})
	defer restoreLabels()

	restoreCgroup := daemon.MockCgroupPathFromPid(func(int) (string, error) {
		return "/user.slice/snap.firefox.firefox.scope", nil
	})
	defer restoreCgroup()

	peer := daemon.SeclogPeerFromUcred(&daemon.Ucrednet{
		Socket: "/run/snapd.socket",
		Uid:    1000,
		Pid:    99,
	})

	c.Check(peer.Snap, Equals, "firefox")
	c.Check(peer.Runnable, Equals, "app.firefox")
}

func (s *seclogSuite) TestSeclogPeerFromUcredHook(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "/snap/bin/snap", nil
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "snap.mysnap.hook.install", nil
	})
	defer restoreLabels()

	restoreCgroup := daemon.MockCgroupPathFromPid(func(int) (string, error) {
		return "", fmt.Errorf("no cgroup")
	})
	defer restoreCgroup()

	peer := daemon.SeclogPeerFromUcred(&daemon.Ucrednet{
		Socket: "/run/snapd-snap.socket",
		Uid:    0,
		Pid:    1,
	})

	c.Check(peer.Snap, Equals, "mysnap")
	c.Check(peer.Runnable, Equals, "hook.install")
}

func (s *seclogSuite) TestSeclogPeerFromUcredComponentHook(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "/snap/bin/snap", nil
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "snap.mysnap+comp.hook.install", nil
	})
	defer restoreLabels()

	restoreCgroup := daemon.MockCgroupPathFromPid(func(int) (string, error) {
		return "", fmt.Errorf("no cgroup")
	})
	defer restoreCgroup()

	peer := daemon.SeclogPeerFromUcred(&daemon.Ucrednet{
		Socket: "/run/snapd-snap.socket",
		Uid:    0,
		Pid:    1,
	})

	c.Check(peer.Snap, Equals, "mysnap")
	c.Check(peer.Runnable, Equals, "mysnap+comp.hook.install")
}

func (s *seclogSuite) TestSeclogPeerFromUcredComponentHookParallelInstance(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "/snap/bin/snap", nil
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "snap.mysnap_foo+comp.hook.install", nil
	})
	defer restoreLabels()

	restoreCgroup := daemon.MockCgroupPathFromPid(func(int) (string, error) {
		return "", fmt.Errorf("no cgroup")
	})
	defer restoreCgroup()

	peer := daemon.SeclogPeerFromUcred(&daemon.Ucrednet{
		Socket: "/run/snapd-snap.socket",
		Uid:    0,
		Pid:    1,
	})

	c.Check(peer.Snap, Equals, "mysnap_foo")
	c.Check(peer.Runnable, Equals, "mysnap+comp.hook.install")
}

func (s *seclogSuite) TestSeclogPeerFromUcredCgroupFallback(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "", fmt.Errorf("no exe")
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "", fmt.Errorf("permission denied")
	})
	defer restoreLabels()

	restoreCgroup := daemon.MockCgroupPathFromPid(func(int) (string, error) {
		return "/user.slice/snap.hello-world.hello-world.scope", nil
	})
	defer restoreCgroup()

	peer := daemon.SeclogPeerFromUcred(&daemon.Ucrednet{
		Socket: "/run/snapd-snap.socket",
		Uid:    1000,
		Pid:    55,
	})

	c.Check(peer.Exe, Equals, "")
	c.Check(peer.Snap, Equals, "hello-world")
	c.Check(peer.Runnable, Equals, "app.hello-world")
}

func (s *seclogSuite) TestSeclogPeerFromUcredLabelReadError(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "/usr/bin/snap", nil
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "", fmt.Errorf("permission denied")
	})
	defer restoreLabels()

	restoreCgroup := daemon.MockCgroupPathFromPid(func(int) (string, error) {
		return "", fmt.Errorf("no cgroup")
	})
	defer restoreCgroup()

	peer := daemon.SeclogPeerFromUcred(&daemon.Ucrednet{
		Socket: "/run/snapd.socket",
		Uid:    0,
		Pid:    10,
	})

	c.Check(peer.Snap, Equals, "")
	c.Check(peer.Runnable, Equals, "")
}

func (s *seclogSuite) TestSeclogPeerFromUcredParallelInstance(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "/snap/bin/firefox", nil
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "snap.firefox_foo.firefox", nil
	})
	defer restoreLabels()

	restoreCgroup := daemon.MockCgroupPathFromPid(func(int) (string, error) {
		return "", fmt.Errorf("no cgroup")
	})
	defer restoreCgroup()

	peer := daemon.SeclogPeerFromUcred(&daemon.Ucrednet{
		Socket: "/run/snapd.socket",
		Uid:    1000,
		Pid:    4242,
	})

	c.Check(peer.Snap, Equals, "firefox_foo")
	c.Check(peer.Runnable, Equals, "app.firefox")
}

func (s *seclogSuite) TestSeclogPeerFromUcredCgroupHookFallback(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "", fmt.Errorf("no exe")
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockApparmorSecurityLabelFromPid(func(int) (string, error) {
		return "", fmt.Errorf("permission denied")
	})
	defer restoreLabels()

	restoreCgroup := daemon.MockCgroupPathFromPid(func(int) (string, error) {
		return "/user.slice/snap.mysnap.hook.install-54b38acc-3ba2-4c6d-b284-7ac07e1159e5.scope", nil
	})
	defer restoreCgroup()

	peer := daemon.SeclogPeerFromUcred(&daemon.Ucrednet{
		Socket: "/run/snapd-snap.socket",
		Uid:    0,
		Pid:    1,
	})

	c.Check(peer.Snap, Equals, "mysnap")
	c.Check(peer.Runnable, Equals, "hook.install")
}
