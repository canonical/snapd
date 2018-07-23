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

package testutil

import (
	"os/exec"

	"gopkg.in/check.v1"
)

type mockCommandSuite struct{}

var _ = check.Suite(&mockCommandSuite{})

const (
	UmountNoFollow = umountNoFollow
)

func (s *mockCommandSuite) TestMockCommand(c *check.C) {
	mock := MockCommand(c, "cmd", "true")
	defer mock.Restore()
	err := exec.Command("cmd", "first-run", "--arg1", "arg2", "a space").Run()
	c.Assert(err, check.IsNil)
	err = exec.Command("cmd", "second-run", "--arg1", "arg2", "a %s").Run()
	c.Assert(err, check.IsNil)
	c.Assert(mock.Calls(), check.DeepEquals, [][]string{
		{"cmd", "first-run", "--arg1", "arg2", "a space"},
		{"cmd", "second-run", "--arg1", "arg2", "a %s"},
	})
}

func (s *mockCommandSuite) TestMockCommandAlso(c *check.C) {
	mock := MockCommand(c, "fst", "")
	also := mock.Also("snd", "")
	defer mock.Restore()

	c.Assert(exec.Command("fst").Run(), check.IsNil)
	c.Assert(exec.Command("snd").Run(), check.IsNil)
	c.Check(mock.Calls(), check.DeepEquals, [][]string{{"fst"}, {"snd"}})
	c.Check(mock.Calls(), check.DeepEquals, also.Calls())
}

func (s *mockCommandSuite) TestMockCommandConflictEcho(c *check.C) {
	mock := MockCommand(c, "do-not-swallow-echo-args", "")
	defer mock.Restore()

	c.Assert(exec.Command("do-not-swallow-echo-args", "-E", "-n", "-e").Run(), check.IsNil)
	c.Assert(mock.Calls(), check.DeepEquals, [][]string{
		{"do-not-swallow-echo-args", "-E", "-n", "-e"},
	})
}
