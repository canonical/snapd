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

package partition

import (
	. "gopkg.in/check.v1"
)

type UtilsTestSuite struct {
}

var _ = Suite(&UtilsTestSuite{})

func (s *UtilsTestSuite) TestRunCommandSimple(c *C) {
	output, err := runCommandImpl("sh", "-c", "printf 'foo\nbar'")
	c.Assert(err, IsNil)
	c.Assert(output, DeepEquals, "foo\nbar")
}

func (s *UtilsTestSuite) TestRunCommandWithStdoutReturnsFalse(c *C) {
	_, err := runCommandImpl("false")
	c.Assert(err, ErrorMatches, `failed to run command \"false\": \"\" \(exit status 1\)`)
}

func (s *UtilsTestSuite) TestRunCommandWithStdoutNoSuchCommand(c *C) {
	_, err := runCommandImpl("no-such-command")
	c.Assert(err, ErrorMatches, `failed to run command \"no-such-command\": \"\" \(exec: \"no-such-command\": executable file not found in \$PATH\)`)
}

func (s *UtilsTestSuite) TestRunCommandWithStdoutReturnsStdout(c *C) {
	output, err := runCommandImpl("sh", "-c", "printf stdout ; printf 'stderr' >&2; false")
	c.Assert(output, Matches, "stdout")
	c.Assert(err, ErrorMatches, `failed to run command \".*\": \"stderr\" \(exit status 1\)`)
}
