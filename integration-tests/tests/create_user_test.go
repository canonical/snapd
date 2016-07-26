// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !excludeintegration,!classic

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

var _ = check.Suite(&createUserSuite{})

type createUserSuite struct {
	common.SnappySuite
}

func (s *createUserSuite) TestCreateUserCreatesUser(c *check.C) {
	createOutput := cli.ExecCommand(c, "sudo", "snap", "create-user", "mvo@ubuntu.com")

	expected := `Created user "mvo"\n`
	c.Assert(createOutput, check.Matches, expected)

	// file exists and has a size greater than zero
	cli.ExecCommand(c, "sudo", "test", "-s", "/home/mvo/.ssh/authorized_keys")
	// content looks sane
	cli.ExecCommand(c, "sudo", "grep", "ssh-rsa", "/home/mvo/.ssh/authorized_keys")
}

func (s *createUserSuite) TestCreateUserFails(c *check.C) {
	createOutput := cli.ExecCommand(c, "sudo", "snap", "create-user", "nosuchuser@example.com")

	expected := `error: bad user result: cannot create user "nosuchuser@example.com.*"`
	c.Assert(createOutput, check.Matches, expected)
}
