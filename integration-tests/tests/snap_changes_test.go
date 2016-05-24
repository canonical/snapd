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

var _ = check.Suite(&changesSuite{})

type changesSuite struct {
	common.SnappySuite
}

// SNAP_CHANGES_001: --help prints the detailed help test for the command
func (s *changesSuite) TestChangesShowsHelp(c *check.C) {
	expected := "(?ms)" +
		"^Usage:\n" +
		`  snap \[OPTIONS\] changes.*\n` +
		"\n^The changes command .*\n" +
		"^Help Options:\n" +
		"^  -h, --help +Show this help message\n" +
		".*"

	actual := cli.ExecCommand(c, "snap", "changes", "--help")

	c.Assert(actual, check.Matches, expected)
}

// SNAP_CHANGES_004: with invalid id
func (s *changesSuite) TestChangesWithInvalidIdShowsError(c *check.C) {
	invalidID := "10000000"

	expected := fmt.Sprintf(`error: cannot find change with id "%s"\n`, invalidID)

	actual, err := cli.ExecCommandErr("snap", "change", invalidID)

	c.Assert(err, check.NotNil)
	c.Assert(actual, check.Matches, expected)
}
