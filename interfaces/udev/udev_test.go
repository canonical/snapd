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

type uDevSuite struct{}

var _ = Suite(&uDevSuite{})

// Tests for ReloadRules()

func (s *uDevSuite) TestReloadUDevRulesRunsUDevAdm(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", 0)
	defer cmd.Restore()
	err := udev.ReloadRules()
	c.Assert(err, IsNil)
	c.Assert(cmd.Calls(), DeepEquals, []string{
		"control --reload-rules",
		"trigger",
	})
}

func (s *uDevSuite) TestReloadUDevRulesReportsErrors(c *C) {
	cmd := testutil.MockCommand(c, "udevadm", 42)
	defer cmd.Restore()
	err := udev.ReloadRules()
	c.Assert(err, ErrorMatches, "exit status 42")
}
