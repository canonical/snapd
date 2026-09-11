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
	"errors"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/daemon"
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

func (s *seclogSuite) TestSeclogPeerFromUcred(c *C) {
	cases := []struct {
		tag      string
		snap     string
		runnable string
	}{{
		tag:      "snap.firefox.firefox",
		snap:     "firefox",
		runnable: "app.firefox",
	}, {
		tag:      "snap.firefox_foo.firefox",
		snap:     "firefox_foo",
		runnable: "app.firefox",
	}, {
		tag:      "snap.mysnap.hook.install",
		snap:     "mysnap",
		runnable: "hook.install",
	}, {
		tag:      "snap.mysnap+comp.hook.install",
		snap:     "mysnap",
		runnable: "comp.hook.install",
	}, {
		tag:      "snap.mysnap_foo+comp.hook.install",
		snap:     "mysnap_foo",
		runnable: "comp.hook.install",
	}}

	for _, tc := range cases {
		ucred := daemon.NewUcrednet(tc.tag, "/usr/bin/snap", 1000, "/run/snapd.socket")
		ucred.PIDForPolkit = 4242

		peer := daemon.SeclogPeerFromUcred(ucred)
		c.Check(peer, DeepEquals, seclog.Peer{
			Socket:   "/run/snapd.socket",
			UID:      1000,
			PID:      4242,
			Exe:      "/usr/bin/snap",
			Snap:     tc.snap,
			Runnable: tc.runnable,
		}, Commentf("tag %s", tc.tag))
	}
}

func (s *seclogSuite) TestSeclogPeerFromUcredMissingTag(c *C) {
	ucred := daemon.NewUcrednet("", "/usr/bin/snap", 0, "/run/snapd.socket")
	ucred.PIDForPolkit = 10

	peer := daemon.SeclogPeerFromUcred(ucred)

	c.Check(peer, DeepEquals, seclog.Peer{
		Socket: "/run/snapd.socket",
		UID:    0,
		PID:    10,
		Exe:    "/usr/bin/snap",
	})
}

func (s *seclogSuite) TestSeclogPeerFromUcredExeError(c *C) {
	ucred := daemon.NewUcrednet("snap.firefox.firefox", "", 1000, "/run/snapd.socket")
	ucred.PIDForPolkit = 4242
	ucred.SetUntrustedProcessExeNameErr(errors.New("cannot read executable"))

	peer := daemon.SeclogPeerFromUcred(ucred)

	c.Check(peer, DeepEquals, seclog.Peer{
		Socket:   "/run/snapd.socket",
		UID:      1000,
		PID:      4242,
		Snap:     "firefox",
		Runnable: "app.firefox",
	})
}
