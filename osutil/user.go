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
)

var userLookup = user.Lookup

var sudoersDotD = "/etc/sudoers.d"

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
}

func AddUser(name string, opts *AddUserOptions) error {
	if opts == nil {
		opts = &AddUserOptions{}
	}

	// we check the (user)name ourselves, adduser is a bit too
	// strict (i.e. no `.`) - this regexp is in sync with that SSO
	// allows as valid usernames
	validNames := regexp.MustCompile(`^[a-z0-9][-a-z0-9+.-_]*$`)
	if !validNames.MatchString(name) {
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
		return fmt.Errorf("adduser failed with %s: %s", err, output)
	}

	if opts.Sudoer {
		// Must escape "." as files containing it are ignored in sudoers.d.
		sudoersFile := filepath.Join(sudoersDotD, "create-user-"+strings.Replace(name, ".", "%2E", -1))
		if err := AtomicWriteFile(sudoersFile, []byte(fmt.Sprintf(sudoersTemplate, name)), 0400, 0); err != nil {
			return fmt.Errorf("cannot create file under sudoers.d: %s", err)
		}
	}

	u, err := userLookup(name)
	if err != nil {
		return fmt.Errorf("cannot find user %q: %s", name, err)
	}

	uid, err := strconv.Atoi(u.Uid)
	if err != nil {
		return fmt.Errorf("cannot parse user id %s: %s", u.Uid, err)
	}
	gid, err := strconv.Atoi(u.Gid)
	if err != nil {
		return fmt.Errorf("cannot parse group id %s: %s", u.Gid, err)
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

// RealUser finds the user behind a sudo invocation, if applicable and possible.
func RealUser() (*user.User, error) {
	cur, err := user.Current()
	if err != nil {
		return nil, err
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
