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

	. "gopkg.in/check.v1"
)

type mockCommandSuite struct{}

var _ = Suite(&mockCommandSuite{})

func (s *mockCommandSuite) TestMockCommand(c *C) {
	mock := MockCommand(c, "cmd", "true")
	err := exec.Command("cmd", "first-run", "--arg1", "arg2", "a space").Run()
	c.Assert(err, IsNil)
	err = exec.Command("cmd", "second-run", "--arg1", "arg2", "a %s").Run()
	c.Assert(err, IsNil)
	c.Assert(mock.Calls(), DeepEquals, [][]string{
		{"cmd", "first-run", "--arg1", "arg2", "a space"},
		{"cmd", "second-run", "--arg1", "arg2", "a %s"},
	})
}
