// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

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

package tests

import (
	"fmt"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/common"
)

var _ = check.Suite(&interfacesCliTest{})

type interfacesCliTest struct {
	common.SnappySuite
}

// SNAP_INTERFACES_001: --help - print detailed help text for the interfaces command
func (s *interfacesCliTest) TestInstallShowHelp(c *check.C) {
	expected := "(?ms)" +
		"^Usage:\n" +
		`  snap \[OPTIONS\] interfaces.*\n` +
		"\n^The interfaces command .*\n" +
		"^Help Options:\n" +
		"^  -h, --help +Show this help message\n" +
		".*"

	actual := cli.ExecCommand(c, "snap", "interfaces", "--help")

	c.Assert(actual, check.Matches, expected)
}

// SNAP_INTERFACES_006: snap interfaces -i=<slot>
func (s *interfacesCliTest) TestFilterBySlot(c *check.C) {
	plug := "network"

	expected := fmt.Sprintf("Slot +Plug\n:%s +-\n", plug)

	actual := cli.ExecCommand(c, "snap", "interfaces", "-i", plug)

	c.Assert(actual, check.Matches, expected)
}
