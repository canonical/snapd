// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!classic

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
	"github.com/snapcore/snapd/integration-tests/testutils/cli"
	"github.com/snapcore/snapd/integration-tests/testutils/common"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&aptSuite{})

type aptSuite struct {
	common.SnappySuite
}

func (s *aptSuite) TestAptGetMustPrintError(c *check.C) {
	aptOutput := cli.ExecCommand(c, "apt-get", "update")

	expected := "Ubuntu Core does not use apt-get, see 'snappy --help'!\n"
	c.Assert(aptOutput, check.Equals, expected, check.Commentf("Wrong apt-get output"))
}
