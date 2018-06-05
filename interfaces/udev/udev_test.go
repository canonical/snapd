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

	"github.com/snapcore/snapd/interfaces/udev"
	"github.com/snapcore/snapd/testutil"
)

func Test(t *testing.T) {
	TestingT(t)
}

type uDevSuite struct{}

var _ = Suite(&uDevSuite{})

// Tests for ReloadRules()

func (s *uDevSuite) TestReloadUDevRulesRunsUDevAdm(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", "")
	defer cmd.Restore()
	err := udev.ReloadRules()
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger"},
		{"udevadm", "settle", "--timeout=10"},
	})
}

func (s *uDevSuite) TestReloadUDevRulesReportsErrorsFromReloadRules(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", `
if [ "$1" = "control" ]; then
	echo "failure 1"
	exit 1
fi
	`)
	defer cmd.Restore()
	err := udev.ReloadRules()
	c.Assert(err.Error(), Equals, ""+
		"cannot reload udev rules: exit status 1\n"+
		"udev output:\n"+
		"failure 1\n")
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
	})
}

func (s *uDevSuite) TestReloadUDevRulesReportsErrorsFromTrigger(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", `
if [ "$1" = "trigger" ]; then
	echo "failure 2"
	exit 2
fi
	`)
	defer cmd.Restore()
	err := udev.ReloadRules()
	c.Assert(err.Error(), Equals, ""+
		"cannot run udev triggers: exit status 2\n"+
		"udev output:\n"+
		"failure 2\n")
	c.Assert(cmd.Calls(), DeepEquals, [][]string{
		{"udevadm", "control", "--reload-rules"},
		{"udevadm", "trigger"},
	})
}
