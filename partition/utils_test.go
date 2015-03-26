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
	. "launchpad.net/gocheck"
)

type UtilsTestSuite struct {
}

var _ = Suite(&UtilsTestSuite{})

func (s *UtilsTestSuite) SetUpTest(c *C) {
}

func (s *UtilsTestSuite) TestRunCommand(c *C) {
	err := runCommandImpl("false")
	c.Assert(err, NotNil)

	err = runCommandImpl("no-such-command")
	c.Assert(err, NotNil)
}

func (s *UtilsTestSuite) TestRunCommandWithStdout(c *C) {
	runCommandWithStdout = runCommandWithStdoutImpl
	output, err := runCommandWithStdout("sh", "-c", "printf 'foo\nbar'")
	c.Assert(err, IsNil)
	c.Assert(output, DeepEquals, "foo\nbar")
}
