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
	"fmt"

	. "../common"

	. "gopkg.in/check.v1"
)

const (
	baseSnapPath  = "_integration-tests/data/snaps"
	basicSnapName = "basic"
	wrongSnapName = "wrong"
)

var _ = Suite(&buildSuite{})

type buildSuite struct {
	SnappySuite
}

func buildSnap(c *C, snapPath string) string {
	return ExecCommand(c, "snappy", "build", snapPath)
}

func (s *buildSuite) TestBuildBasicSnapOnSnappy(c *C) {
	// build basic snap and check output
	buildOutput := buildSnap(c, fmt.Sprintf("%s/%s", baseSnapPath, basicSnapName))
	expected := ""
	c.Assert(buildOutput, Equals, expected)

	// install built snap
	installOutput := InstallSnap(c, basicSnapName+".snap")

	// check install output
	expected = ""
	c.Assert(installOutput, Equals, expected)

	// teardown, remove snap
	RemoveSnap(c, basicSnapName)
}

func (s *buildSuite) TestBuildWrongSnapOnSnappy(c *C) {
	// build wrong snap and check output
	buildOutput := buildSnap(c, fmt.Sprintf("%s/%s", baseSnapPath, wrongSnapName))
	expected := ""
	c.Assert(buildOutput, Equals, expected)
}
