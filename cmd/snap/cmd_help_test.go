// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"os"

	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

func (s *SnapSuite) TestHelpPrintsHelp(c *check.C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	for _, cmdLine := range [][]string{
		{"snap", "help"},
		{"snap", "--help"},
		{"snap", "-h"},
	} {
		os.Args = cmdLine

		err := snap.RunMain()
		c.Assert(err, check.IsNil)
		c.Check(s.Stdout(), check.Matches, `(?smU)Usage:
 +snap <command>

Install, configure, refresh and remove snap packages. Snaps are
'universal' packages that work across many different Linux systems,
enabling secure distribution of the latest apps and utilities for
cloud, servers, desktops and the internet of things.

This is the CLI for snapd, a background service that takes care of
snaps on the system. Start with 'snap list' to see installed snaps.

Available commands:
 +abort.*
`)
		c.Check(s.Stderr(), check.Equals, "")
	}
}

func (s *SnapSuite) TestSubCommandHelpPrintsHelp(c *check.C) {
	origArgs := os.Args
	defer func() { os.Args = origArgs }()

	os.Args = []string{"snap", "install", "--help"}

	err := snap.RunMain()
	c.Assert(err, check.IsNil)
	c.Check(s.Stdout(), check.Matches, `(?smU)Usage:
 +snap install \[install-OPTIONS\] <snap>...
.*
`)
	c.Check(s.Stderr(), check.Equals, "")
}
