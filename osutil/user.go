// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package osutil

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/snapcore/snapd/osutil/sys"
)

var sudoersTemplate = `
# Created by snap create-user

# User rules for %[1]s
%[1]s ALL=(ALL) NOPASSWD:ALL
`

type AddUserOptions struct {
	Sudoer     bool
	ExtraUsers bool
	Gecos      string
	SSHKeys    []string
	// crypt(3) compatible password of the form $id$salt$hash
	Password string
	// force a password change by the user on login
	ForcePasswordChange bool
}

// we check the (user)name ourselves, adduser is a bit too
// strict (i.e. no `.`) - this regexp is in sync with that SSO
// allows as valid usernames
var IsValidUsername = regexp.MustCompile(`^[a-z0-9][-a-z0-9+._]*$`).MatchString

// EnsureUserGroup uses the standard shadow utilities' 'useradd' and 'groupadd'
// commands for creating non-login system users and groups that is portable
// cross-distro. It will create the group with groupname 'name' and gid 'id' as
// well as the user with username 'name' and uid 'id'. Importantly, 'useradd'
// and 'groupadd' will use NSS to determine if a uid/gid is already assigned
// (so LDAP, etc are consulted), but will themselves only add to local files,
// which is exactly what we want since we don't want snaps to be blocked on
// LDAP, etc when performing lookups.
func EnsureUserGroup(name string, id uint32, extraUsers bool) error {
	if !IsValidUsername(name) {
		return fmt.Errorf(`cannot add user/group %q: name contains invalid characters`, name)
	}

	// Perform uid and gid lookups
	uid, uidErr := FindUid(name)
	if uidErr != nil {
		if _, ok := uidErr.(user.UnknownUserError); !ok {
			return uidErr
		}
	}

	gid, gidErr := FindGid(name)
	if gidErr != nil {
		if _, ok := gidErr.(user.UnknownGroupError); !ok {
			return gidErr
		}
	}

	if uidErr == nil && gidErr == nil {
		if uid != uint64(id) {
			return fmt.Errorf(`found unexpected uid for user %q: %d`, name, uid)
		} else if gid != uint64(id) {
			return fmt.Errorf(`found unexpected gid for group %q: %d`, name, gid)
		}
		// found the user and group with expected values
		return nil
	}

	// If the user and group do not exist, snapd will create both, so if
	// the admin removed one of them, error and don't assume we can just
	// add the missing one
	if uidErr != nil && gidErr == nil {
		return fmt.Errorf(`cannot add user/group %q: group exists and user does not`, name)
	} else if uidErr == nil && gidErr != nil {
		return fmt.Errorf(`cannot add user/group %q: user exists and group does not`, name)
	}

	// At this point, we know that the user and group don't exist, so
	// create them.

	// First create the group. useradd --user-group will choose a gid from
	// the range defined in login.defs, so first call groupadd and use
	// --gid with useradd.
	groupCmdStr := []string{
		"groupadd",
		"--system",
		"--gid", strconv.FormatUint(uint64(id), 10),
	}

	if extraUsers {
		groupCmdStr = append(groupCmdStr, "--extrausers")
	}
	groupCmdStr = append(groupCmdStr, name)

	cmd := exec.Command(groupCmdStr[0], groupCmdStr[1:]...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("groupadd failed with: %s", OutputErr(output, err))
	}

	// Now call useradd with the group we just created. As a non-login
	// system user, we choose:
	// - no password or aging (use --system without --password)
	// - a non-existent home directory (--home-dir /nonexistent and
	//   --no-create-home)
	// - a non-functional shell (--shell .../nologin)
	// - use the above group (--gid with --no-user-group)
	userCmdStr := []string{
		"useradd",
		"--system",
		"--home-dir", "/nonexistent", "--no-create-home",
		"--shell", LookPathDefault("false", "/bin/false"),
		"--gid", strconv.FormatUint(uint64(id), 10), "--no-user-group",
		"--uid", strconv.FormatUint(uint64(id), 10),
	}

	if extraUsers {
		userCmdStr = append(userCmdStr, "--extrausers")
	}
	userCmdStr = append(userCmdStr, name)

	cmd = exec.Command(userCmdStr[0], userCmdStr[1:]...)
	if output, err := cmd.CombinedOutput(); err != nil {
		useraddErrStr := fmt.Sprintf("useradd failed with: %s", OutputErr(output, err))

		delCmdStr := []string{"groupdel"}
		if extraUsers {
			delCmdStr = append(delCmdStr, "--extrausers")
		}

		// TODO: groupdel doesn't currently support --extrausers, so
		// don't try to clean up when it is specified (LP: #1840375)
		if !extraUsers {
			delCmdStr = append(delCmdStr, name)
			cmd = exec.Command(delCmdStr[0], delCmdStr[1:]...)
			if output2, err2 := cmd.CombinedOutput(); err2 != nil {
				return fmt.Errorf("groupdel failed with: %s (after %s)", OutputErr(output2, err2), useraddErrStr)
			}
		}
		return fmt.Errorf(useraddErrStr)
	}

	return nil
}

// AddUser uses the Debian/Ubuntu/derivative 'adduser' command for creating
// regular login users on Ubuntu Core. 'adduser' is not portable cross-distro
// but is convenient for creating regular login users.
func AddUser(name string, opts *AddUserOptions) error {
	if opts == nil {
		opts = &AddUserOptions{}
	}

	if !IsValidUsername(name) {
		return fmt.Errorf("cannot add user %q: name contains invalid characters", name)
	}

	cmdStr := []string{
		"adduser",
		"--force-badname",
		"--gecos", opts.Gecos,
		"--disabled-password",
	}
	if opts.ExtraUsers {
		cmdStr = append(cmdStr, "--extrausers")
	}
	cmdStr = append(cmdStr, name)

	cmd := exec.Command(cmdStr[0], cmdStr[1:]...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adduser failed with: %s", OutputErr(output, err))
	}

	if opts.Sudoer {
		// Must escape "." as files containing it are ignored in sudoers.d.
		sudoersFile := filepath.Join(sudoersDotD, "create-user-"+strings.Replace(name, ".", "%2E", -1))
		if err := AtomicWriteFile(sudoersFile, []byte(fmt.Sprintf(sudoersTemplate, name)), 0400, 0); err != nil {
			return fmt.Errorf("cannot create file under sudoers.d: %s", err)
		}
	}

	if opts.Password != "" {
		cmdStr := []string{
			"usermod",
			"--password", opts.Password,
			// no --extrauser required, see LP: #1562872
			name,
		}
		if output, err := exec.Command(cmdStr[0], cmdStr[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("setting password failed: %s", OutputErr(output, err))
		}
	}
	if opts.ForcePasswordChange {
		if opts.Password == "" {
			return fmt.Errorf("cannot force password change when no password is provided")
		}
		cmdStr := []string{
			"passwd",
			"--expire",
			// no --extrauser required, see LP: #1562872
			name,
		}
		if output, err := exec.Command(cmdStr[0], cmdStr[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("cannot force password change: %s", OutputErr(output, err))
		}
	}

	u, err := userLookup(name)
	if err != nil {
		return fmt.Errorf("cannot find user %q: %s", name, err)
	}

	uid, gid, err := UidGid(u)
	if err != nil {
		return err
	}

	sshDir := filepath.Join(u.HomeDir, ".ssh")
	if err := MkdirAllChown(sshDir, 0700, uid, gid); err != nil {
		return fmt.Errorf("cannot create %s: %s", sshDir, err)
	}
	authKeys := filepath.Join(sshDir, "authorized_keys")
	authKeysContent := strings.Join(opts.SSHKeys, "\n")
	if err := AtomicWriteFileChown(authKeys, []byte(authKeysContent), 0600, 0, uid, gid); err != nil {
		return fmt.Errorf("cannot write %s: %s", authKeys, err)
	}

	return nil
}

// RealUser finds the user behind a sudo invocation when root, if applicable
// and possible.
//
// Don't check SUDO_USER when not root and simply return the current uid
// to properly support sudo'ing from root to a non-root user
func RealUser() (*user.User, error) {
	cur, err := userCurrent()
	if err != nil {
		return nil, err
	}

	// not root, so no sudo invocation we care about
	if cur.Uid != "0" {
		return cur, nil
	}

	realName := os.Getenv("SUDO_USER")
	if realName == "" {
		// not sudo; current is correct
		return cur, nil
	}

	real, err := user.Lookup(realName)
	// can happen when sudo is used to enter a chroot (e.g. pbuilder)
	if _, ok := err.(user.UnknownUserError); ok {
		return cur, nil
	}
	if err != nil {
		return nil, err
	}

	return real, nil
}

// UidGid returns the uid and gid of the given user, as uint32s
//
// XXX this should go away soon
func UidGid(u *user.User) (sys.UserID, sys.GroupID, error) {
	// XXX this will be wrong for high uids on 32-bit arches (for now)
	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return sys.FlagID, sys.FlagID, fmt.Errorf("cannot parse user id %s: %s", u.Uid, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return sys.FlagID, sys.FlagID, fmt.Errorf("cannot parse group id %s: %s", u.Gid, err)
	}

	return sys.UserID(uid), sys.GroupID(gid), nil
}
