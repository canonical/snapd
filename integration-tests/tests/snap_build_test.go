// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration

/*
 * Copyright (C) 2015, 2016 Canonical Ltd
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
	"os"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&snapBuildSuite{})

type snapBuildSuite struct {
	common.SnappySuite
}

func (s *snapBuildSuite) TestBuildBasicSnapOnSnappy(c *check.C) {
	c.Skip("port to snapd")

	// build basic snap and check output
	snapPath, err := build.LocalSnap(c, data.BasicSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))

	// install built snap and check output
	installOutput := common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicSnapName)
	expected := "(?ms)" +
		"Name +Date +Version +Developer\n" +
		".*" +
		data.BasicSnapName + " +.* +.* +sideload\n" +
		".*"

	c.Check(installOutput, check.Matches, expected)
}
