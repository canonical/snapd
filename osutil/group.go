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
	"os/user"
	"strconv"
)

// FindUid returns the identifier of the given UNIX user name. It will
// automatically fallback to use "getent" if needed.
var FindUid = findUid

// FindGid returns the identifier of the given UNIX group name. It will
// automatically fallback to use "getent" if needed.
var FindGid = findGid

func MockFindUid(f func(string) (uint64, error)) (restore func()) {
	old := FindUid
	FindUid = f
	return func() {
		FindUid = old
	}
}

func MockFindGid(f func(string) (uint64, error)) (restore func()) {
	old := FindGid
	FindGid = f
	return func() {
		FindGid = old
	}
}

// getent returns the identifier of the given UNIX user or group name as
// determined by the specified database
func getent(database, name string) (uint64, error) {
	if database != "passwd" && database != "group" {
		return 0, fmt.Errorf(`unsupported getent database "%q"`, database)
	}

	cmdStr := []string{
		"getent",
		database,
		name,
	}
	output, stderr, err := RunSplitOutput(cmdStr[0], cmdStr[1:]...)
	if err != nil {
		// according to getent(1) the exit value of "2" means:
		// "One or more supplied key could not be found in the
		// database."
		exitCode, _ := ExitCode(err)
		if exitCode == 2 {
			if database == "passwd" {
				return 0, user.UnknownUserError(name)
			}
			return 0, user.UnknownGroupError(name)
		}
		return 0, fmt.Errorf("getent failed with: %v", OutputErrCombine(output, stderr, err))
	}

	// passwd has 7 entries and group 4. In both cases, parts[2] is the id
	parts := bytes.Split(output, []byte(":"))
	if len(parts) < 4 {
		return 0, fmt.Errorf("malformed entry: %q", output)
	}

	return strconv.ParseUint(string(parts[2]), 10, 64)
}

// TODO: both findUidNoGetentFallback and findGidNoGetentFallback should return
//       a more qualified default value than uint64, because currently the
//       default return value for findUid is "0" as per Go conventions, which is
//       unfortunately also the uid of root, so if a caller ignored the error
//       from this function and used that to perform access authorization, then
//       the caller would accidentally provide the same access level as root in
//       the error case. This is excaberated by the fact that the error case is
//       very difficult to positively identify correctly as "not found", see the
//       comments inside the functions for more details.
// Note: there is a similar implementation in overlord/snapshotstate which
//       should be similarly adjusted when resolving the above TODO.

var findUidNoGetentFallback = func(username string) (uint64, error) {
	myuser, err := user.Lookup(username)
	if err != nil {
		// Treat all non-nil errors as user.Unknown{User,Group}Error's, as
		// currently Go's handling of returned errno from get{pw,gr}nam_r
		// in the cgo implementation of user.Lookup is lacking, and thus
		// user.Unknown{User,Group}Error is returned only when errno is 0
		// and the list of users/groups is empty, but as per the man page
		// for get{pw,gr}nam_r, there are many other errno's that typical
		// systems could return to indicate that the user/group wasn't
		// found, however unfortunately the POSIX standard does not actually
		// dictate what errno should be used to indicate "user/group not
		// found", and so even if Go is more robust, it may not ever be
		// fully robust. See from the man page:
		//
		// > It [POSIX.1-2001] does not call "not found" an error, hence
		// > does not specify what value errno might have in this situation.
		// > But that makes it impossible to recognize errors.
		//
		// See upstream Go issue: https://github.com/golang/go/issues/40334

		// if there is a real problem finding the user/group then presumably
		// other things will fail upon trying to create the user, etc. which
		// will give more useful and specific errors
		return 0, user.UnknownUserError(username)
	}

	return strconv.ParseUint(myuser.Uid, 10, 64)
}

var findGidNoGetentFallback = func(groupname string) (uint64, error) {
	group, err := user.LookupGroup(groupname)
	if err != nil {
		// Treat all non-nil errors as user.Unknown{User,Group}Error's, as
		// currently Go's handling of returned errno from get{pw,gr}nam_r
		// in the cgo implementation of user.Lookup is lacking, and thus
		// user.Unknown{User,Group}Error is returned only when errno is 0
		// and the list of users/groups is empty, but as per the man page
		// for get{pw,gr}nam_r, there are many other errno's that typical
		// systems could return to indicate that the user/group wasn't
		// found, however unfortunately the POSIX standard does not actually
		// dictate what errno should be used to indicate "user/group not
		// found", and so even if Go is more robust, it may not ever be
		// fully robust. See from the man page:
		//
		// > It [POSIX.1-2001] does not call "not found" an error, hence
		// > does not specify what value errno might have in this situation.
		// > But that makes it impossible to recognize errors.
		//
		// See upstream Go issue: https://github.com/golang/go/issues/40334

		// if there is a real problem finding the user/group then presumably
		// other things will fail upon trying to create the user, etc. which
		// will give more useful and specific errors
		return 0, user.UnknownGroupError(groupname)
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
		return getent("passwd", username)
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
		return getent("group", groupname)
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
