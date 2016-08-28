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
		current, err := user.Current()
		if err != nil {
			c.Fatalf("user.Current() failed with %s", err)
		}
		return &user.User{
			HomeDir: mockHome,
			Gid:     current.Gid,
			Uid:     current.Uid,
		}, nil
	})
	defer restorer()
	mockSudoers := c.MkDir()
	restorer = osutil.MockSudoersDotD(mockSudoers)
	defer restorer()

	mockAddUser := testutil.MockCommand(c, "adduser", "true")
	defer mockAddUser.Restore()

	err := osutil.AddExtraUser("karl.sagan", []string{"ssh-key1", "ssh-key2"}, "my gecos", false)
	c.Assert(err, check.IsNil)

	c.Check(mockAddUser.Calls(), check.DeepEquals, [][]string{
		{"adduser", "--force-badname", "--gecos", "my gecos", "--extrausers", "--disabled-password", "karl.sagan"},
	})
	fs, _ := filepath.Glob(filepath.Join(mockSudoers, "*"))
	c.Assert(fs, check.HasLen, 0)

	mockAddUser.ForgetCalls()

	err = osutil.AddExtraUser("karl.sagan", []string{"ssh-key1", "ssh-key2"}, "my gecos", true)
	c.Assert(err, check.IsNil)

	c.Check(mockAddUser.Calls(), check.DeepEquals, [][]string{
		{"adduser", "--force-badname", "--gecos", "my gecos", "--extrausers", "--disabled-password", "karl.sagan"},
	})

	sshKeys, err := ioutil.ReadFile(filepath.Join(mockHome, ".ssh", "authorized_keys"))
	c.Assert(err, check.IsNil)
	c.Check(string(sshKeys), check.Equals, "ssh-key1\nssh-key2")

	fs, _ = filepath.Glob(filepath.Join(mockSudoers, "*"))
	c.Assert(fs, check.HasLen, 1)
	c.Assert(filepath.Base(fs[0]), check.Equals, "create-user-karl%2Esagan")
	bs, err := ioutil.ReadFile(fs[0])
	c.Assert(err, check.IsNil)
	c.Check(string(bs), check.Equals, `
# Created by snap create-user

# User rules for karl.sagan
karl.sagan ALL=(ALL) NOPASSWD:ALL
`)
}

func (s *createUserSuite) TestAddExtraSudoUserInvalid(c *check.C) {
	err := osutil.AddExtraUser("k!", nil, "my gecos", false)
	c.Assert(err, check.ErrorMatches, `cannot add user "k!": name contains invalid characters`)
}
