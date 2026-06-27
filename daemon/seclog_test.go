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
		UID: ^uint32(0),
		PID: 0,
	})
	c.Check(peer.String(), Equals, "<unknown>:<unknown>:<unknown>")
}

func (s *seclogSuite) TestSeclogPeerFromUcredEnrichment(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(path string) (string, error) {
		c.Check(path, Equals, filepath.Join(dirs.GlobalRootDir, "proc/4242/exe"))
		return "/usr/bin/snap", nil
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockSecurityLabelsFromPid(func(pid int) (map[string]string, error) {
		c.Check(pid, Equals, 4242)
		return map[string]string{
			seclog.PeerSecurityLabelAppArmor: "snap.firefox.firefox",
		}, nil
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
		Socket: "/run/snapd.socket",
		UID:    1000,
		PID:    4242,
		Exe:    "/usr/bin/snap",
		SecurityLabels: map[string]string{
			seclog.PeerSecurityLabelAppArmor: "snap.firefox.firefox",
		},
		CgroupLabel: "snap.firefox.firefox",
		Snap:        "firefox",
		App:         "firefox",
	})
}

func (s *seclogSuite) TestSeclogPeerFromUcredUnconfined(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "/usr/bin/curl", nil
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockSecurityLabelsFromPid(func(int) (map[string]string, error) {
		return map[string]string{
			seclog.PeerSecurityLabelAppArmor: "unconfined",
		}, nil
	})
	defer restoreLabels()

	restoreCgroup := daemon.MockCgroupPathFromPid(func(int) (string, error) {
		return "", fmt.Errorf("no cgroup")
	})
	defer restoreCgroup()

	peer := daemon.SeclogPeerFromUcred(&daemon.Ucrednet{
		Socket: "/run/snapd.socket",
		Uid:    1000,
		Pid:    99,
	})

	c.Check(peer.SecurityLabels, DeepEquals, map[string]string{
		seclog.PeerSecurityLabelAppArmor: "unconfined",
	})
	c.Check(peer.CgroupLabel, Equals, "")
	c.Check(peer.Snap, Equals, "")
	c.Check(peer.App, Equals, "")
}

func (s *seclogSuite) TestSeclogPeerFromUcredSELinux(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "/snap/bin/firefox", nil
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockSecurityLabelsFromPid(func(int) (map[string]string, error) {
		return map[string]string{
			seclog.PeerSecurityLabelSELinux: "unconfined_u:unconfined_r:unconfined_service_t:s0",
		}, nil
	})
	defer restoreLabels()

	restoreCgroup := daemon.MockCgroupPathFromPid(func(int) (string, error) {
		return "/user.slice/snap.firefox.firefox.scope", nil
	})
	defer restoreCgroup()

	peer := daemon.SeclogPeerFromUcred(&daemon.Ucrednet{
		Socket: "/run/snapd-snap.socket",
		Uid:    1000,
		Pid:    4242,
	})

	c.Check(peer.SecurityLabels, DeepEquals, map[string]string{
		seclog.PeerSecurityLabelSELinux: "unconfined_u:unconfined_r:unconfined_service_t:s0",
	})
	c.Check(peer.CgroupLabel, Equals, "snap.firefox.firefox")
	c.Check(peer.Snap, Equals, "firefox")
	c.Check(peer.App, Equals, "firefox")
}

func (s *seclogSuite) TestSeclogPeerFromUcredHook(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "/snap/bin/snap", nil
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockSecurityLabelsFromPid(func(int) (map[string]string, error) {
		return map[string]string{
			seclog.PeerSecurityLabelAppArmor: "snap.mysnap.hook.install",
		}, nil
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
	c.Check(peer.App, Equals, "install")
}

func (s *seclogSuite) TestSeclogPeerFromUcredCgroupFallback(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "", fmt.Errorf("no exe")
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockSecurityLabelsFromPid(func(int) (map[string]string, error) {
		return nil, fmt.Errorf("permission denied")
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
	c.Check(peer.SecurityLabels, IsNil)
	c.Check(peer.CgroupLabel, Equals, "snap.hello-world.hello-world")
	c.Check(peer.Snap, Equals, "hello-world")
	c.Check(peer.App, Equals, "hello-world")
}

func (s *seclogSuite) TestSeclogPeerFromUcredLabelReadError(c *C) {
	restoreReadlink := daemon.MockOsReadlink(func(string) (string, error) {
		return "/usr/bin/snap", nil
	})
	defer restoreReadlink()

	restoreLabels := daemon.MockSecurityLabelsFromPid(func(int) (map[string]string, error) {
		return nil, fmt.Errorf("permission denied")
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

	c.Check(peer.SecurityLabels, IsNil)
	c.Check(peer.Snap, Equals, "")
	c.Check(peer.App, Equals, "")
}
