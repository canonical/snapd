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
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/testutil"
)

type createUserSuite struct {
	testutil.BaseTest

	mockHome string
	restorer func()

	mockAddUser *testutil.MockCmd
	mockUserAdd *testutil.MockCmd
	mockUserMod *testutil.MockCmd
	mockPasswd  *testutil.MockCmd
}

var _ = check.Suite(&createUserSuite{})

func (s *createUserSuite) SetUpTest(c *check.C) {
	s.mockHome = c.MkDir()
	s.restorer = osutil.MockUserLookup(func(string) (*user.User, error) {
		current, err := user.Current()
		if err != nil {
			c.Fatalf("user.Current() failed with %s", err)
		}
		return &user.User{
			HomeDir: s.mockHome,
			Gid:     current.Gid,
			Uid:     current.Uid,
		}, nil
	})
	s.mockAddUser = testutil.MockCommand(c, "adduser", "")
	s.mockUserAdd = testutil.MockCommand(c, "useradd", "")
	s.mockUserMod = testutil.MockCommand(c, "usermod", "")
	s.mockPasswd = testutil.MockCommand(c, "passwd", "")
}

func (s *createUserSuite) TearDownTest(c *check.C) {
	s.restorer()
	s.mockAddUser.Restore()
	s.mockUserAdd.Restore()
	s.mockUserMod.Restore()
	s.mockPasswd.Restore()
}

func (s *createUserSuite) TestAddUserExtraUsersFalse(c *check.C) {
	r := osutil.MockhasAddUserExecutable(func() bool { return true })
	defer r()

	err := osutil.AddUser("lakatos", &osutil.AddUserOptions{
		Gecos:      "my gecos",
		ExtraUsers: false,
	})
	c.Assert(err, check.IsNil)

	c.Check(s.mockAddUser.Calls(), check.DeepEquals, [][]string{
		{"adduser", "--force-badname", "--gecos", "my gecos", "--disabled-password", "lakatos"},
	})
}

func (s *createUserSuite) TestUserAddExtraUsersFalse(c *check.C) {
	r := osutil.MockhasAddUserExecutable(func() bool { return false })
	defer r()

	err := osutil.AddUser("lakatos", &osutil.AddUserOptions{
		Gecos:      "my gecos",
		ExtraUsers: false,
	})
	c.Assert(err, check.IsNil)

	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string{
		{"useradd", "--comment", "my gecos", "--create-home", "--shell", "/bin/bash", "lakatos"},
	})
}

func (s *createUserSuite) TestAddUserExtraUsersTrue(c *check.C) {
	r := osutil.MockhasAddUserExecutable(func() bool { return true })
	defer r()

	err := osutil.AddUser("lakatos", &osutil.AddUserOptions{
		Gecos:      "my gecos",
		ExtraUsers: true,
	})
	c.Assert(err, check.IsNil)

	c.Check(s.mockAddUser.Calls(), check.DeepEquals, [][]string{
		{"adduser", "--force-badname", "--gecos", "my gecos", "--disabled-password", "--extrausers", "lakatos"},
	})
}

func (s *createUserSuite) TestUserAddExtraUsersTrue(c *check.C) {
	r := osutil.MockhasAddUserExecutable(func() bool { return false })
	defer r()

	err := osutil.AddUser("lakatos", &osutil.AddUserOptions{
		Gecos:      "my gecos",
		ExtraUsers: true,
	})
	c.Assert(err, check.IsNil)

	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string{
		{"useradd", "--comment", "my gecos", "--create-home", "--shell", "/bin/bash", "--extrausers", "lakatos"},
	})
}

func (s *createUserSuite) TestAddSudoUser(c *check.C) {
	r := osutil.MockhasAddUserExecutable(func() bool { return true })
	defer r()
	mockSudoers := c.MkDir()
	restorer := osutil.MockSudoersDotD(mockSudoers)
	defer restorer()

	err := osutil.AddUser("karl.sagan", &osutil.AddUserOptions{
		Gecos:      "my gecos",
		Sudoer:     true,
		ExtraUsers: true,
	})
	c.Assert(err, check.IsNil)

	c.Check(s.mockAddUser.Calls(), check.DeepEquals, [][]string{
		{"adduser", "--force-badname", "--gecos", "my gecos", "--disabled-password", "--extrausers", "karl.sagan"},
	})

	fs, _ := filepath.Glob(filepath.Join(mockSudoers, "*"))
	c.Assert(fs, check.HasLen, 1)
	c.Assert(filepath.Base(fs[0]), check.Equals, "create-user-karl%2Esagan")
	c.Check(fs[0], testutil.FileEquals, `
# Created by snap create-user

# User rules for karl.sagan
karl.sagan ALL=(ALL) NOPASSWD:ALL
`)
}

func (s *createUserSuite) TestAddUserSSHKeys(c *check.C) {
	r := osutil.MockhasAddUserExecutable(func() bool { return true })
	defer r()
	err := osutil.AddUser("karl.sagan", &osutil.AddUserOptions{
		SSHKeys: []string{"ssh-key1", "ssh-key2"},
	})
	c.Assert(err, check.IsNil)
	c.Check(filepath.Join(s.mockHome, ".ssh", "authorized_keys"), testutil.FileEquals, "ssh-key1\nssh-key2")

}

func (s *createUserSuite) TestAddUserInvalidUsername(c *check.C) {
	r := osutil.MockhasAddUserExecutable(func() bool { return true })
	defer r()
	err := osutil.AddUser("k!", nil)
	c.Assert(err, check.ErrorMatches, `cannot add user "k!": name contains invalid characters`)
}

func (s *createUserSuite) TestAddUserWithPassword(c *check.C) {
	r := osutil.MockhasAddUserExecutable(func() bool { return true })
	defer r()

	mockSudoers := c.MkDir()
	restorer := osutil.MockSudoersDotD(mockSudoers)
	defer restorer()

	err := osutil.AddUser("karl.sagan", &osutil.AddUserOptions{
		Gecos:    "my gecos",
		Password: "$6$salt$hash",
	})
	c.Assert(err, check.IsNil)

	c.Check(s.mockAddUser.Calls(), check.DeepEquals, [][]string{
		{"adduser", "--force-badname", "--gecos", "my gecos", "--disabled-password", "karl.sagan"},
	})
	c.Check(s.mockUserMod.Calls(), check.DeepEquals, [][]string{
		{"usermod", "--password", "$6$salt$hash", "karl.sagan"},
	})
}

func (s *createUserSuite) TestAddUserWithPasswordForceChange(c *check.C) {
	r := osutil.MockhasAddUserExecutable(func() bool { return false })
	defer r()

	mockSudoers := c.MkDir()
	restorer := osutil.MockSudoersDotD(mockSudoers)
	defer restorer()

	err := osutil.AddUser("karl.popper", &osutil.AddUserOptions{
		Gecos:               "my gecos",
		Password:            "$6$salt$hash",
		ForcePasswordChange: true,
	})
	c.Assert(err, check.IsNil)

	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string{
		{"useradd", "--comment", "my gecos", "--create-home", "--shell", "/bin/bash", "karl.popper"},
	})
	c.Check(s.mockUserMod.Calls(), check.DeepEquals, [][]string{
		{"usermod", "--password", "$6$salt$hash", "karl.popper"},
	})
	c.Check(s.mockPasswd.Calls(), check.DeepEquals, [][]string{
		{"passwd", "--expire", "karl.popper"},
	})
}

func (s *createUserSuite) TestAddUserPasswordForceChangeUnhappy(c *check.C) {
	r := osutil.MockhasAddUserExecutable(func() bool { return true })
	defer r()
	mockSudoers := c.MkDir()
	restorer := osutil.MockSudoersDotD(mockSudoers)
	defer restorer()

	err := osutil.AddUser("karl.popper", &osutil.AddUserOptions{
		Gecos:               "my gecos",
		ForcePasswordChange: true,
	})
	c.Assert(err, check.ErrorMatches, `cannot force password change when no password is provided`)
}

func (s *createUserSuite) TestUserMaybeSudoUser(c *check.C) {
	oldUser := os.Getenv("SUDO_USER")
	defer func() { os.Setenv("SUDO_USER", oldUser) }()

	for _, t := range []struct {
		SudoUsername    string
		CurrentUsername string
		CurrentUid      int
	}{
		// simulate regular "root", no SUDO_USER set
		{"", os.Getenv("USER"), 0},
		// simulate a normal sudo invocation
		{"guy", "guy", 0},
		// simulate running "sudo -u some-user -i" as root
		// (LP: #1638656)
		{"root", os.Getenv("USER"), 1000},
	} {
		restore := osutil.MockUserCurrent(func() (*user.User, error) {
			return &user.User{
				Username: t.CurrentUsername,
				Uid:      strconv.Itoa(t.CurrentUid),
			}, nil
		})
		defer restore()

		os.Setenv("SUDO_USER", t.SudoUsername)
		cur, err := osutil.UserMaybeSudoUser()
		c.Assert(err, check.IsNil)
		c.Check(cur.Username, check.Equals, t.CurrentUsername)
	}
}

func (s *createUserSuite) TestUidGid(c *check.C) {
	for k, t := range map[string]struct {
		User *user.User
		Uid  sys.UserID
		Gid  sys.GroupID
		Err  string
	}{
		"happy":   {&user.User{Uid: "10", Gid: "10"}, 10, 10, ""},
		"bad uid": {&user.User{Uid: "x", Gid: "10"}, sys.FlagID, sys.FlagID, "cannot parse user id x"},
		"bad gid": {&user.User{Uid: "10", Gid: "x"}, sys.FlagID, sys.FlagID, "cannot parse group id x"},
	} {
		uid, gid, err := osutil.UidGid(t.User)
		c.Check(uid, check.Equals, t.Uid, check.Commentf(k))
		c.Check(gid, check.Equals, t.Gid, check.Commentf(k))
		if t.Err == "" {
			c.Check(err, check.IsNil, check.Commentf(k))
		} else {
			c.Check(err, check.ErrorMatches, ".*"+t.Err+".*", check.Commentf(k))
		}
	}
}

func (s *createUserSuite) TestAddUserUnhappy(c *check.C) {
	r := osutil.MockhasAddUserExecutable(func() bool { return true })
	defer r()

	mockAddUser := testutil.MockCommand(c, "adduser", "echo some error; exit 1")
	defer mockAddUser.Restore()

	err := osutil.AddUser("lakatos", nil)
	c.Assert(err, check.ErrorMatches, "adduser failed with: some error")

}

func (s *createUserSuite) TestUserAddUnhappy(c *check.C) {
	r := osutil.MockhasAddUserExecutable(func() bool { return false })
	defer r()

	mockUserAdd := testutil.MockCommand(c, "useradd", "echo some error; exit 1")
	defer mockUserAdd.Restore()
	err := osutil.AddUser("lakatos", nil)
	c.Assert(err, check.ErrorMatches, "useradd failed with: some error")
}

var usernameTestCases = map[string]bool{
	"a":       true,
	"a-b":     true,
	"a+b":     false,
	"a.b":     true,
	"a_b":     true,
	"1":       true,
	"1+":      false,
	"1.":      true,
	"1_":      true,
	"-":       false,
	"+":       false,
	".":       false,
	"_":       false,
	"-a":      false,
	"+a":      false,
	".a":      false,
	"_a":      false,
	"a:b":     false,
	"inval!d": false,
}

func (s *createUserSuite) TestIsValidUsername(c *check.C) {
	for k, v := range usernameTestCases {
		c.Check(osutil.IsValidUsername(k), check.Equals, v)
	}
}

func (s *createUserSuite) TestIsValidSnapSystemUsername(c *check.C) {
	systemUsernameTestCases := map[string]bool{
		"_daemon_":    true,
		"snap_daemon": true,
		"_a_":         true,
	}
	for k, v := range usernameTestCases {
		systemUsernameTestCases[k] = v
	}

	for k, v := range systemUsernameTestCases {
		c.Check(osutil.IsValidSnapSystemUsername(k), check.Equals, v, check.Commentf("%v not %v", k, v))
	}
}

type delUserSuite struct {
	mockUserDel *testutil.MockCmd
	opts        *osutil.DelUserOptions
	sudoersd    string
	restore     func()
}

var _ = check.Suite(&delUserSuite{opts: nil})
var _ = check.Suite(&delUserSuite{opts: &osutil.DelUserOptions{ExtraUsers: true}})

func (s *delUserSuite) SetUpTest(c *check.C) {
	s.mockUserDel = testutil.MockCommand(c, "userdel", "")
	s.sudoersd = c.MkDir()
	s.restore = osutil.MockSudoersDotD(s.sudoersd)
}

func (s *delUserSuite) TearDownTest(c *check.C) {
	s.mockUserDel.Restore()
	s.restore()
}

func (s *delUserSuite) expectedCmd(u string) []string {
	if s.opts != nil && s.opts.ExtraUsers {
		return []string{"userdel", "--remove", "--extrausers", u}
	}
	return []string{"userdel", "--remove", u}
}

func (s *delUserSuite) TestDelUser(c *check.C) {
	c.Assert(osutil.DelUser("u1", s.opts), check.IsNil)
	c.Assert(s.mockUserDel.Calls(), check.DeepEquals, [][]string{s.expectedCmd("u1")})
}

func (s *delUserSuite) TestDelUserForce(c *check.C) {
	c.Assert(osutil.DelUser("u1", &osutil.DelUserOptions{Force: false}), check.IsNil)
	c.Assert(osutil.DelUser("u2", &osutil.DelUserOptions{Force: true}), check.IsNil)

	// validity check
	c.Check(s.mockUserDel.Calls(), check.DeepEquals, [][]string{
		{"userdel", "--remove", "u1"},
		{"userdel", "--remove", "--force", "u2"},
	})
}

func (s *delUserSuite) TestDelUserRemovesSudoersIfPresent(c *check.C) {
	f1 := osutil.SudoersFile("u1")

	// only create u1's sudoers file
	c.Assert(os.WriteFile(f1, nil, 0600), check.IsNil)

	// neither of the delusers fail
	c.Assert(osutil.DelUser("u1", s.opts), check.IsNil)
	c.Assert(osutil.DelUser("u2", s.opts), check.IsNil)

	// but u1's sudoers file is no more
	c.Check(f1, testutil.FileAbsent)

	// validity check
	c.Check(s.mockUserDel.Calls(), check.DeepEquals, [][]string{
		s.expectedCmd("u1"),
		s.expectedCmd("u2"),
	})
}

func (s *delUserSuite) TestDelUserSudoersRemovalFailure(c *check.C) {
	f1 := osutil.SudoersFile("u1")

	// create a directory that'll mess with the removal
	c.Assert(os.MkdirAll(filepath.Join(f1, "ook", "ook"), 0700), check.IsNil)

	// delusers fails
	c.Assert(osutil.DelUser("u1", s.opts), check.ErrorMatches, `cannot remove sudoers file for user "u1": .*`)

	// validity check
	c.Check(s.mockUserDel.Calls(), check.DeepEquals, [][]string{
		s.expectedCmd("u1"),
	})
}

func (s *delUserSuite) TestDelUserFails(c *check.C) {
	mockUserDel := testutil.MockCommand(c, "userdel", "exit 99")
	defer mockUserDel.Restore()

	c.Assert(osutil.DelUser("u1", s.opts), check.ErrorMatches, `cannot delete user "u1": exit status 99`)
	c.Check(mockUserDel.Calls(), check.DeepEquals, [][]string{s.expectedCmd("u1")})
}

type ensureUserSuite struct {
	mockUserAdd  *testutil.MockCmd
	mockGroupAdd *testutil.MockCmd
	mockGroupDel *testutil.MockCmd
}

var _ = check.Suite(&ensureUserSuite{})

func (s *ensureUserSuite) SetUpTest(c *check.C) {
	s.mockUserAdd = testutil.MockCommand(c, "useradd", "")
	s.mockGroupAdd = testutil.MockCommand(c, "groupadd", "")
	s.mockGroupDel = testutil.MockCommand(c, "groupdel", "")
}

func (s *ensureUserSuite) TearDownTest(c *check.C) {
	s.mockUserAdd.Restore()
	s.mockGroupAdd.Restore()
	s.mockGroupDel.Restore()
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupExtraUsersFalse(c *check.C) {
	falsePath = osutil.LookPathDefault("false", "/bin/false")
	err := osutil.EnsureSnapUserGroup("lakatos", 123456, false)
	c.Assert(err, check.IsNil)

	c.Check(s.mockGroupAdd.Calls(), check.DeepEquals, [][]string{
		{"groupadd", "--system", "--gid", "123456", "lakatos"},
	})
	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string{
		{"useradd", "--system", "--home-dir", "/nonexistent", "--no-create-home", "--shell", falsePath, "--gid", "123456", "--no-user-group", "--uid", "123456", "lakatos"},
	})
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupExtraUsersTrue(c *check.C) {
	falsePath = osutil.LookPathDefault("false", "/bin/false")
	err := osutil.EnsureSnapUserGroup("lakatos", 123456, true)
	c.Assert(err, check.IsNil)

	c.Check(s.mockGroupAdd.Calls(), check.DeepEquals, [][]string{
		{"groupadd", "--system", "--gid", "123456", "--extrausers", "lakatos"},
	})
	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string{
		{"useradd", "--system", "--home-dir", "/nonexistent", "--no-create-home", "--shell", falsePath, "--gid", "123456", "--no-user-group", "--uid", "123456", "--extrausers", "lakatos"},
	})
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupBadUser(c *check.C) {
	err := osutil.EnsureSnapUserGroup("k!", 123456, false)
	c.Assert(err, check.ErrorMatches, `cannot add user/group "k!": name contains invalid characters`)

	// shouldn't run these on error
	c.Check(s.mockGroupAdd.Calls(), check.DeepEquals, [][]string(nil))
	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupUnexpectedFindUidError(c *check.C) {
	restore := osutil.MockFindUid(func(string) (uint64, error) {
		return 0, fmt.Errorf("some odd FindUid error")
	})
	defer restore()

	err := osutil.EnsureSnapUserGroup("lakatos", 1234, false)
	c.Assert(err, check.ErrorMatches, `some odd FindUid error`)

	// shouldn't run these on error
	c.Check(s.mockGroupAdd.Calls(), check.DeepEquals, [][]string(nil))
	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupUnexpectedFindGidError(c *check.C) {
	restore := osutil.MockFindGid(func(string) (uint64, error) {
		return 0, fmt.Errorf("some odd FindGid error")
	})
	defer restore()

	err := osutil.EnsureSnapUserGroup("lakatos", 1234, false)
	c.Assert(err, check.ErrorMatches, `some odd FindGid error`)

	// shouldn't run these on error
	c.Check(s.mockGroupAdd.Calls(), check.DeepEquals, [][]string(nil))
	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupUnexpectedUid(c *check.C) {
	restore := osutil.MockFindUid(func(string) (uint64, error) {
		return uint64(5432), nil
	})
	defer restore()
	restore = osutil.MockFindGid(func(string) (uint64, error) {
		return uint64(1234), nil
	})
	defer restore()

	err := osutil.EnsureSnapUserGroup("lakatos", 1234, false)
	c.Assert(err, check.ErrorMatches, `found unexpected uid for user "lakatos": 5432`)

	// shouldn't run these on error
	c.Check(s.mockGroupAdd.Calls(), check.DeepEquals, [][]string(nil))
	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupUnexpectedGid(c *check.C) {
	restore := osutil.MockFindUid(func(string) (uint64, error) {
		return uint64(1234), nil
	})
	defer restore()
	restore = osutil.MockFindGid(func(string) (uint64, error) {
		return uint64(5432), nil
	})
	defer restore()

	err := osutil.EnsureSnapUserGroup("lakatos", 1234, false)
	c.Assert(err, check.ErrorMatches, `found unexpected gid for group "lakatos": 5432`)

	// shouldn't run these on error
	c.Check(s.mockGroupAdd.Calls(), check.DeepEquals, [][]string(nil))
	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupFoundBoth(c *check.C) {
	restore := osutil.MockFindUid(func(string) (uint64, error) {
		return uint64(1234), nil
	})
	defer restore()
	restore = osutil.MockFindGid(func(string) (uint64, error) {
		return uint64(1234), nil
	})
	defer restore()

	err := osutil.EnsureSnapUserGroup("lakatos", 1234, false)
	c.Assert(err, check.IsNil)

	// we found both with expected values, shouldn't run these
	c.Check(s.mockGroupAdd.Calls(), check.DeepEquals, [][]string(nil))
	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupUnexpectedGroupMissing(c *check.C) {
	restore := osutil.MockFindUid(func(string) (uint64, error) {
		return uint64(1234), nil
	})
	defer restore()

	err := osutil.EnsureSnapUserGroup("lakatos", 1234, false)
	c.Assert(err, check.ErrorMatches, `cannot add user/group "lakatos": user exists and group does not`)

	// shouldn't run these on error
	c.Check(s.mockGroupAdd.Calls(), check.DeepEquals, [][]string(nil))
	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupUnexpectedUserMissing(c *check.C) {
	restore := osutil.MockFindGid(func(string) (uint64, error) {
		return uint64(1234), nil
	})
	defer restore()

	err := osutil.EnsureSnapUserGroup("lakatos", 1234, false)
	c.Assert(err, check.ErrorMatches, `cannot add user/group "lakatos": group exists and user does not`)

	// shouldn't run these on error
	c.Check(s.mockGroupAdd.Calls(), check.DeepEquals, [][]string(nil))
	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupFailedGroupadd(c *check.C) {
	mockGroupAdd := testutil.MockCommand(c, "groupadd", "echo some error; exit 1")
	defer mockGroupAdd.Restore()

	err := osutil.EnsureSnapUserGroup("lakatos", 123456, false)
	c.Assert(err, check.ErrorMatches, "groupadd failed with: some error")

	// shouldn't run this on error
	c.Check(s.mockUserAdd.Calls(), check.DeepEquals, [][]string(nil))
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupFailedUseraddClassic(c *check.C) {
	mockUserAdd := testutil.MockCommand(c, "useradd", "echo some error; exit 1")
	defer mockUserAdd.Restore()

	err := osutil.EnsureSnapUserGroup("lakatos", 123456, false)
	c.Assert(err, check.ErrorMatches, "useradd failed with: some error")

	c.Check(s.mockGroupDel.Calls(), check.DeepEquals, [][]string{
		{"groupdel", "lakatos"},
	})
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupFailedUseraddCore(c *check.C) {
	mockUserAdd := testutil.MockCommand(c, "useradd", "echo some error; exit 1")
	defer mockUserAdd.Restore()

	err := osutil.EnsureSnapUserGroup("lakatos", 123456, true)
	c.Assert(err, check.ErrorMatches, "useradd failed with: some error")

	c.Check(s.mockGroupDel.Calls(), check.DeepEquals, [][]string{
		{"groupdel", "--extrausers", "lakatos"},
	})
}

func (s *ensureUserSuite) TestEnsureSnapUserGroupFailedUseraddCoreNoExtra(c *check.C) {
	mockUserAdd := testutil.MockCommand(c, "useradd", "echo some error; exit 1")
	defer mockUserAdd.Restore()

	mockGroupDel := testutil.MockCommand(c, "groupdel",
		`echo "groupdel: unrecognized option '--extrauser'" > /dev/stderr; exit 1`)
	defer mockGroupDel.Restore()

	err := osutil.EnsureSnapUserGroup("lakatos", 123456, true)
	c.Assert(err, check.ErrorMatches, `errors encountered ensuring user lakatos exists:
- useradd failed with: some error
- groupdel: unrecognized option '--extrauser'`)

	c.Check(mockGroupDel.Calls(), check.DeepEquals, [][]string{
		{"groupdel", "--extrausers", "lakatos"},
	})
}
