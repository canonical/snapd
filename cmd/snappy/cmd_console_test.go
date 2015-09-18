// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package main

import (
	"bytes"

	. "gopkg.in/check.v1"
)

func (s *CmdTestSuite) TestCmdConsoleCompleter(c *C) {
	// setup
	x := cmdConsole{}

	err := x.initConsole()
	c.Assert(err, IsNil)

	// from cmdline parser
	c.Check(x.snappyCompleter("hw-"), DeepEquals, []string{"hw-assign", "hw-info", "hw-unassign"})

	// extra consoleCommand
	c.Check(x.snappyCompleter("he"), DeepEquals, []string{"help"})
	c.Check(x.snappyCompleter("help"), DeepEquals, []string{"help"})
}

func (s *CmdTestSuite) TestDoHelpGeneric(c *C) {
	stdout = bytes.NewBuffer(nil)

	x := cmdConsole{}
	x.doHelp("")
	c.Assert(stdout.(*bytes.Buffer).String(), Matches, `(?sm).*Available commands:`)
}

func (s *CmdTestSuite) TestDoHelpSet(c *C) {
	stdout = bytes.NewBuffer(nil)

	x := cmdConsole{}
	x.doHelp("set")
	c.Assert(stdout.(*bytes.Buffer).String(), Matches, `(?sm).*Set properties of system or package`)
}
