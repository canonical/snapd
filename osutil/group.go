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

// FindUid returns the identifier of the given UNIX user name with no getent
// fallback
func FindUid(username string) (uint64, error) {
	return findUid(username)
}

// FindGid returns the identifier of the given UNIX group name with no getent
// fallback
func FindGid(groupname string) (uint64, error) {
	return findGid(groupname)
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
	if len(parts) < 4 {
		return 0, fmt.Errorf("malformed entry: %q", output)
	}

	return strconv.ParseUint(string(parts[2]), 10, 64)
}

var findUidNoGetentFallback = func(username string) (uint64, error) {
	myuser, err := user.Lookup(username)
	if err != nil {
		return 0, err
	}

	return strconv.ParseUint(myuser.Uid, 10, 64)
}

var findGidNoGetentFallback = func(groupname string) (uint64, error) {
	group, err := user.LookupGroup(groupname)
	if err != nil {
		return 0, err
	}

	return strconv.ParseUint(group.Gid, 10, 64)
}

// findUidWithGetentFallback returns the identifier of the given UNIX user name with
// getent fallback
func findUidWithGetentFallback(username string) (uint64, error) {
	// first do the cheap os/user lookup
	myuser, err := findUidNoGetentFallback(username)
	switch err.(type) {
	case nil:
		// found it!
		return myuser, nil
	case user.UnknownUserError:
		// user unknown, let's try getent
		return getent(username, "passwd")
	default:
		// something weird happened with the lookup, just report it
		return 0, err
	}
}

// findGidWithGetentFallback returns the identifier of the given UNIX group name with
// getent fallback
func findGidWithGetentFallback(groupname string) (uint64, error) {
	// first do the cheap os/user lookup
	group, err := findGidNoGetentFallback(groupname)
	switch err.(type) {
	case nil:
		// found it!
		return group, nil
	case user.UnknownGroupError:
		// group unknown, let's try getent
		return getent(groupname, "group")
	default:
		// something weird happened with the lookup, just report it
		return 0, err
	}
}

func IsUnknownUser(err error) bool {
	_, ok := err.(user.UnknownUserError)
	return ok
}

func IsUnknownGroup(err error) bool {
	_, ok := err.(user.UnknownGroupError)
	return ok
}
