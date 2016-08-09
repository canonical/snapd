// -*- Mode: Go; indent-tabs-mode: t -*-

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

package osutil_test

import (
	"io/ioutil"
	"os/user"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type createUserSuite struct {
	testutil.BaseTest
}

var _ = check.Suite(&createUserSuite{})

func (s *createUserSuite) TestAddExtraSudoUser(c *check.C) {
	mockHome := c.MkDir()
	restorer := osutil.MockUserLookup(func(string) (*user.User, error) {
		return &user.User{
			HomeDir: mockHome,
			Gid:     "new-group",
			Uid:     "new-user",
		}, nil
	})
	defer restorer()

	mockAddUser := testutil.MockCommand(c, "adduser", "true")
	defer mockAddUser.Restore()
	mockChown := testutil.MockCommand(c, "chown", "true")
	defer mockChown.Restore()

	err := osutil.AddExtraSudoUser("karl", []string{"ssh-key1", "ssh-key2"}, "my gecos")
	c.Assert(err, check.IsNil)

	c.Check(mockAddUser.Calls(), check.DeepEquals, [][]string{
		{"adduser", "--force-badname", "--gecos", "my gecos", "--extrausers", "--disabled-password", "karl"},
		{"adduser", "karl", "sudo"},
	})

	c.Check(mockChown.Calls(), check.DeepEquals, [][]string{
		{"chown", "-R", "new-user:new-group", filepath.Join(mockHome, ".ssh")},
	})

	sshKeys, err := ioutil.ReadFile(filepath.Join(mockHome, ".ssh", "authorized_keys"))
	c.Assert(err, check.IsNil)
	c.Check(string(sshKeys), check.Equals, "ssh-key1\nssh-key2")
}

func (s *createUserSuite) TestAddExtraSudoUserInvalid(c *check.C) {
	err := osutil.AddExtraSudoUser("k!", nil, "my gecos")
	c.Assert(err, check.ErrorMatches, `cannot add user "k!": name contains invalid characters`)
}
