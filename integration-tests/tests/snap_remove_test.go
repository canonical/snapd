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

	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&removeSuite{})

type removeSuite struct {
	common.SnappySuite
}

// SNAP_REMOVE_001: --help prints the detailed help test for the command
func (s *removeSuite) TestRemoveShowsHelp(c *check.C) {
	expected := "(?ms)" +
		"^Usage:\n" +
		`  snap \[OPTIONS\] remove.*\n` +
		"\n^The remove command .*\n" +
		"^Help Options:\n" +
		"^  -h, --help +Show this help message\n" +
		".*"

	actual := cli.ExecCommand(c, "snap", "remove", "--help")

	c.Assert(actual, check.Matches, expected)
}

// SNAP_REMOVE_002: - invalid pkg name
func (s *removeSuite) TestRemoveInvalidPackageShowsError(c *check.C) {
	invalidPkg := "invalid-package-name"

	expected := fmt.Sprintf(`error: cannot remove "%s": cannot find snap "%[1]s"\n`, invalidPkg)

	actual, err := cli.ExecCommandErr("sudo", "snap", "remove", invalidPkg)

	c.Assert(err, check.NotNil)
	c.Assert(actual, check.Matches, expected)
}

// SNAP_REMOVE_007: - ubuntu-core
func (s *removeSuite) TestRemoveUbuntuCoreShowsError(c *check.C) {
	expected := `error: cannot remove "ubuntu-core": snap "ubuntu-core" is not removable\n`

	actual, err := cli.ExecCommandErr("sudo", "snap", "remove", "ubuntu-core")

	c.Assert(err, check.NotNil)
	c.Assert(actual, check.Matches, expected)
}
