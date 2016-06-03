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
	"github.com/snapcore/snapd/osutil"

	"gopkg.in/check.v1"
)

var _ = check.Suite(&createUserSuite{})

type createUserSuite struct {
	common.SnappySuite
}

func (s *createUserSuite) TestCreateUserCreatesUser(c *check.C) {
	createOutput := cli.ExecCommand(c, "sudo", "snap", "create-user", "mvo@ubuntu.com")

	expected := "Created user 'mvo'"
	c.Assert(createOutput, check.Matches, expected)
	c.Assert(osutil.FileExists("/home/mvo/.ssh/authorized_keys"), check.Equals, true)
}
