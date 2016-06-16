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
	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

const nothingPendingErrorTpl = "error: cannot abort change %s with nothing pending\n"

var _ = check.Suite(&abortSuite{})

type abortSuite struct {
	common.SnappySuite
}

// SNAP_ABORT_001: --help - print detailed help text for the abort command
func (s *abortSuite) TestAbortShowHelp(c *check.C) {
	expected := "(?ms)" +
		"^Usage:\n" +
		`  snap \[OPTIONS\] abort.*\n` +
		"\n^The abort command .*\n" +
		"^Help Options:\n" +
		"^  -h, --help +Show this help message\n" +
		".*"

	actual := cli.ExecCommand(c, "snap", "abort", "--help")

	c.Assert(actual, check.Matches, expected)
}
