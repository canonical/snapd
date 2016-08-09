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
	"io/ioutil"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
)

var userLookup = user.Lookup

var sudoersDotD = "/etc/sudoers.d"

var sudoersTemplate = `
# Created by snap create-user

# User rules for %[1]s
%[1]s ALL=(ALL) NOPASSWD:ALL
`

func AddExtraSudoUser(name string, sshKeys []string, gecos string) error {
	// we check the (user)name ourselves, adduser is a bit too
	// strict (i.e. no `.`) - this regexp is in sync with that SSO
	// allows as valid usernames
	validNames := regexp.MustCompile(`^[a-z0-9][-a-z0-9+.-_]*$`)
	if !validNames.MatchString(name) {
		return fmt.Errorf("cannot add user %q: name contains invalid characters", name)
	}

	cmd := exec.Command("adduser",
		"--force-badname",
		"--gecos", gecos,
		"--extrausers",
		"--disabled-password",
		name)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("adduser failed with %s: %s", err, output)
	}

	sudoersFile := filepath.Join(sudoersDotD, "create-user-"+name)
	if err := AtomicWriteFile(sudoersFile, []byte(fmt.Sprintf(sudoersTemplate, name)), 0400, 0); err != nil {
		return fmt.Errorf("creating sudoers fragment failed with %s", err)
	}

	u, err := userLookup(name)
	if err != nil {
		return fmt.Errorf("cannot find user %q: %s", name, err)
	}
	sshDir := filepath.Join(u.HomeDir, ".ssh")
	if err := os.MkdirAll(sshDir, 0700); err != nil {
		return fmt.Errorf("cannot create %s: %s", sshDir, err)
	}
	authKeys := filepath.Join(sshDir, "authorized_keys")
	authKeysContent := strings.Join(sshKeys, "\n")
	if err := ioutil.WriteFile(authKeys, []byte(authKeysContent), 0644); err != nil {
		return fmt.Errorf("cannot write %s: %s", authKeys, err)
	}

	cmd = exec.Command("chown", "-R", u.Uid+":"+u.Gid, sshDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("changing ownser of sshDir failed %s: %s", err, output)
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
	if err != nil {
		return nil, err
	}

	return real, nil
}
