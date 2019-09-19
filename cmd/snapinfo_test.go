// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package cmd_test

import (
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/snap"
)

func (*cmdSuite) TestC2S(c *check.C) {
	// TODO: add moar fields!
	si := &snap.Info{
		Website: "http://example.com/xyzzy",
	}
	ci, err := cmd.ClientSnapFromSnapInfo(si)
	c.Check(err, check.IsNil)
	c.Check(ci.Website, check.Equals, si.Website)
}

func (*cmdSuite) TestAppStatusNotes(c *check.C) {
	ai := client.AppInfo{}
	c.Check(cmd.ClientAppInfoNotes(&ai), check.Equals, "-")

	ai = client.AppInfo{
		Daemon: "oneshot",
	}
	c.Check(cmd.ClientAppInfoNotes(&ai), check.Equals, "-")

	ai = client.AppInfo{
		Daemon: "oneshot",
		Activators: []client.AppActivator{
			{Type: "timer"},
		},
	}
	c.Check(cmd.ClientAppInfoNotes(&ai), check.Equals, "timer-activated")

	ai = client.AppInfo{
		Daemon: "oneshot",
		Activators: []client.AppActivator{
			{Type: "socket"},
		},
	}
	c.Check(cmd.ClientAppInfoNotes(&ai), check.Equals, "socket-activated")

	// check that the output is stable regardless of the order of activators
	ai = client.AppInfo{
		Daemon: "oneshot",
		Activators: []client.AppActivator{
			{Type: "timer"},
			{Type: "socket"},
		},
	}
	c.Check(cmd.ClientAppInfoNotes(&ai), check.Equals, "timer-activated,socket-activated")
	ai = client.AppInfo{
		Daemon: "oneshot",
		Activators: []client.AppActivator{
			{Type: "socket"},
			{Type: "timer"},
		},
	}
	c.Check(cmd.ClientAppInfoNotes(&ai), check.Equals, "timer-activated,socket-activated")
}
