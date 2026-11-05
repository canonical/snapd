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

package cli_test

import (
	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snapd/cli"
)

type closestSuite struct{}

var _ = Suite(&closestSuite{})

func (s *closestSuite) TestClosestCommand(c *C) {
	// build the real parser so that the command tree matches the one
	// used by the snap command
	parser := snap.Parser(nil)

	for _, tc := range []struct {
		typo string
		name string
	}{
		{"instal", "install"},
		{"isntall", "install"},
		{"lst", "list"},
		{"remov", "remove"},
		// "akc" is one transposition away from "ack"
		{"akc", "ack"},
		// command names are matched case-insensitively
		{"INSTALL", "install"},
		{"Install", "install"},
		{"inSTALL", "install"},
		// command aliases are valid suggestions
		{"chage", "change"},
		{"seach", "search"},
		{"xyzzy", ""},
		// clearly unrelated commands must not produce a suggestion
		{"readme", ""},
		{"", ""},
	} {
		comment := Commentf("typo %q", tc.typo)
		c.Check(snap.ClosestCommand(parser.Command, tc.typo), Equals, tc.name, comment)
	}
}

func (s *closestSuite) TestClosestCommandSubCommands(c *C) {
	parser := snap.Parser(nil)
	debug := parser.Command.Find("debug")
	c.Assert(debug, NotNil)

	c.Check(snap.ClosestCommand(debug, "stat"), Equals, "state")
}

func (s *closestSuite) TestClosestCommandHidden(c *C) {
	parser := flags.NewParser(nil, flags.Default)
	_, err := parser.AddCommand("install", "", "", &struct{}{})
	c.Assert(err, IsNil)
	hidden, err := parser.AddCommand("internal-thing", "", "", &struct{}{})
	c.Assert(err, IsNil)
	hidden.Hidden = true

	// hidden commands are still runnable, so they are suggested too
	c.Check(snap.ClosestCommand(parser.Command, "internal-thin"), Equals, "internal-thing")
}

func (s *closestSuite) TestClosestCommandIneligibleNotPreferred(c *C) {
	parser := flags.NewParser(nil, flags.Default)
	for _, name := range []string{"ack", "tasks"} {
		_, err := parser.AddCommand(name, "", "", &struct{}{})
		c.Assert(err, IsNil)
	}

	// "ack" sorts first and is equally distant, but does not pass the
	// distance cutoff, so "tasks" is suggested instead
	c.Check(snap.ClosestCommand(parser.Command, "aks"), Equals, "tasks")
}

func (s *closestSuite) TestClosestCommandEquidistant(c *C) {
	parser := flags.NewParser(nil, flags.Default)
	for _, name := range []string{"cat", "car"} {
		_, err := parser.AddCommand(name, "", "", &struct{}{})
		c.Assert(err, IsNil)
	}

	// both candidates are one edit away from the typo, the
	// lexicographically first one wins
	c.Check(snap.ClosestCommand(parser.Command, "cap"), Equals, "car")
}

func (s *closestSuite) TestClosestCommandNil(c *C) {
	c.Check(snap.ClosestCommand(nil, "install"), Equals, "")
}
