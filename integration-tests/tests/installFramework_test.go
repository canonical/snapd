// -*- Mode: Go; indent-tabs-mode: t -*-
// +build integration

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

package tests

import (
	"fmt"
	"os"

	"github.com/ubuntu-core/snappy/integration-tests/testutils/build"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/common"
	"github.com/ubuntu-core/snappy/integration-tests/testutils/data"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&installFrameworkSuite{})

type installFrameworkSuite struct {
	common.SnappySuite
}

func (s *installFrameworkSuite) TestInstallFrameworkMustPrintPackageInformation(c *check.C) {
	snapPath, err := build.LocalSnap(c, data.BasicFrameworkSnapName)
	defer os.Remove(snapPath)
	c.Assert(err, check.IsNil, check.Commentf("Error building local snap: %s", err))
	installOutput := common.InstallSnap(c, snapPath)
	defer common.RemoveSnap(c, data.BasicFrameworkSnapName)

	expected := "(?ms)" +
		fmt.Sprintf("Installing %s\n", snapPath) +
		".*Signature check failed, but installing anyway as requested\n" +
		"Name +Date +Version +Developer \n" +
		".*" +
		"^basic-framework +.* +.* +sideload *\n" +
		".*"

	c.Assert(installOutput, check.Matches, expected)
}
