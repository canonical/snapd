// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package latest

import (
	. "../common"

	. "gopkg.in/check.v1"
)

const (
	baseSnapPath  = "_integration-tests/tests/latest/fixtures/snaps"
	basicSnapName = "basic-snap"
)

var _ = Suite(&buildSuite{})

type buildSuite struct {
	CommonSuite
}

func buildSnap(c *C, snapPath string) {
	ExecCommand(c, "snappy", "build", snapPath)
}

func (s *buildSuite) TestBuildBasicSnapOnSnappy(c *C) {
	// build basic snap

	// check build output

	// install built snap

	// check install output

	// teardown, remove snap
	removeSnap(c, basicSnapName)
}

func (s *buildSuite) TestBuildWrongSnapOnSnappy(c *C) {
	// build wrong snap

	// check build output with error
}
