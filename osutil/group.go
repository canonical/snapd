// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2019 Canonical Ltd
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
	"bytes"
	"fmt"
	"os/exec"
	"os/user"
	"strconv"
)

var (
	FindUid       = findUid
	FindGid       = findGid
	FindUidGetent = findUidGetent
	FindGidGetent = findGidGetent
)

// The builtin os/user functions only look at /etc/passwd and /etc/group and
// nothing configured via nsswitch.conf, like extrausers. findUid() and
// findGid() continue this behavior where findUidGetent() and findGidGetent()
// will perform a 'getent <database> <name>'

// findUid returns the identifier of the given UNIX user name with no getent
// fallback
func findUid(username string) (uint64, error) {
	myuser, err := user.Lookup(username)
	if err != nil {
		return 0, err
	}

	return strconv.ParseUint(myuser.Uid, 10, 64)
}

// findGid returns the identifier of the given UNIX group name with no getent
// fallback
func findGid(groupname string) (uint64, error) {
	group, err := user.LookupGroup(groupname)
	if err != nil {
		return 0, err
	}

	return strconv.ParseUint(group.Gid, 10, 64)
}

// getent returns the identifier of the given UNIX user or group name as
// determined by the specified database
func getent(name string, database string) (uint64, error) {
	if database != "passwd" && database != "group" {
		return 0, fmt.Errorf(`unsupported getent database "%q"`, database)
	}

	cmdStr := []string{
		"getent",
		database,
		name,
	}
	cmd := exec.Command(cmdStr[0], cmdStr[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// according to getent(1) the exit value of "2" means:
		//    One or more supplied key could not be found in
		//    the database.
		exitCode, _ := ExitCode(err)
		if exitCode == 2 {
			if database == "passwd" {
				return 0, user.UnknownUserError(name)
			}
			return 0, user.UnknownGroupError(name)
		}
		return 0, fmt.Errorf("cannot run getent: %v", err)
	}

	// passwd has 7 entries and group 4. In both cases, parts[2] is the id
	parts := bytes.Split(output, []byte(":"))
	if len(parts) < 3 {
		return 0, fmt.Errorf("malformed entry: %q", output)
	}

	return strconv.ParseUint(string(parts[2]), 10, 64)
}

// findUidGetent returns the identifier of the given UNIX user name with
// getent fallback
func findUidGetent(username string) (uint64, error) {
	// first do the cheap os/user lookup
	myuser, err := FindUid(username)
	if err == nil {
		// found it!
		return myuser, nil
	} else if _, ok := err.(user.UnknownUserError); !ok {
		// something weird happened with the lookup, just report it
		return 0, err
	}

	// user unknown, let's try getent
	return getent(username, "passwd")
}

// findGidGetent returns the identifier of the given UNIX group name with
// getent fallback
func findGidGetent(groupname string) (uint64, error) {
	// first do the cheap os/user lookup
	group, err := FindGid(groupname)
	if err == nil {
		// found it!
		return group, nil
	} else if _, ok := err.(user.UnknownGroupError); !ok {
		// something weird happened with the lookup, just report it
		return 0, err
	}

	// group unknown, let's try getent
	return getent(groupname, "group")
}
