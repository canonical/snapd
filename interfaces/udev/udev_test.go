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

package udev_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/interfaces/udev"
	"github.com/ubuntu-core/snappy/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type uDevSuite struct {
	cmd *testutil.MockCmd
}

var _ = Suite(&uDevSuite{})

func (s *uDevSuite) SetUpTest(c *C) {
	s.cmd = testutil.MockCommand(c, "udevadm", 0)
}

func (s *uDevSuite) TearDownTest(c *C) {
	s.cmd.Restore()
}

// Tests for ReloadRules()

func (s *uDevSuite) TestReloadUDevRulesRunsUDevAdm(c *C) {
	err := udev.ReloadRules()
	c.Assert(err, IsNil)
	c.Assert(s.cmd.Calls(), DeepEquals, []string{
		"control --reload-rules",
		"trigger",
	})
}

func (s *uDevSuite) TestReloadUDevRulesReportsErrorsFromReloadRules(c *C) {
	s.cmd.SetDynamicBehavior(1, func(n int) (string, int) {
		switch n {
		case 0:
			return "failure 1", 1
		default:
			panic(n)
		}
	})
	err := udev.ReloadRules()
	c.Assert(err.Error(), Equals, ""+
		"Cannot reload udev rules: exit status 1\n"+
		"udev output:\n"+
		"failure 1\n")
	c.Assert(s.cmd.Calls(), DeepEquals, []string{"control --reload-rules"})
}

func (s *uDevSuite) TestReloadUDevRulesReportsErrorsFromTrigger(c *C) {
	s.cmd.SetDynamicBehavior(2, func(n int) (string, int) {
		switch n {
		case 0:
			return "", 0
		case 1:
			return "failure 2", 2
		default:
			panic(n)
		}
	})
	err := udev.ReloadRules()
	c.Assert(err.Error(), Equals, ""+
		"Cannot run udev triggers: exit status 2\n"+
		"udev output:\n"+
		"failure 2\n")
	c.Assert(s.cmd.Calls(), DeepEquals, []string{
		"control --reload-rules",
		"trigger",
	})
}
