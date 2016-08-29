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

package ctlcmd_test

import (
	"testing"

	"github.com/snapcore/snapd/overlord/hookstate/ctlcmd"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type ctlcmdSuite struct {
	mockHandler *hooktest.MockHandler
}

var _ = Suite(&ctlcmdSuite{})

func (s *ctlcmdSuite) SetUpTest(c *C) {
	s.mockHandler = hooktest.NewMockHandler()
}

func (s *ctlcmdSuite) TestNonExistingCommand(c *C) {
	stdout, stderr, err := ctlcmd.RunCommand(s.mockHandler, []string{"foo", "--bar"})
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
	c.Check(err, ErrorMatches, ".*[Uu]nknown command.*")
}

func (s *ctlcmdSuite) TestCommandOutput(c *C) {
	mockCommand := ctlcmd.NewMockCommand()
	mockCommand.FakeStdout = "test stdout"
	mockCommand.FakeStderr = "test stderr"
	ctlcmd.AddCommand("mock", mockCommand)
	defer ctlcmd.RemoveCommand("mock")

	stdout, stderr, err := ctlcmd.RunCommand(s.mockHandler, []string{"mock", "foo", "--bar"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "test stdout")
	c.Check(string(stderr), Equals, "test stderr")
	c.Check(mockCommand.Args, DeepEquals, []string{"foo", "--bar"})
}

func (s *ctlcmdSuite) TestSetCommand(c *C) {
	stdout, stderr, err := ctlcmd.RunCommand(s.mockHandler, []string{"set", "foo=bar"})
	c.Check(err, IsNil)
	c.Check(string(stdout), Equals, "")
	c.Check(string(stderr), Equals, "")
}
