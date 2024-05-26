// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package snap

import (
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap/naming"
)

var userLookupId = user.LookupId

func IsSnapd(snapID string) bool {
	return snapID == naming.WellKnownSnapID("snapd")
}

// AllUsers returns a list of users, including the root user and all users that
// can be found under /home with a snap directory.
func AllUsers(opts *dirs.SnapDirOptions) ([]*user.User, error) {
	var ds []string

	for _, entry := range dirs.DataHomeGlobs(opts) {
		entryPaths := mylog.Check2(filepath.Glob(entry))

		ds = append(ds, entryPaths...)
	}

	users := make([]*user.User, 1, len(ds)+1)
	root := mylog.Check2(user.LookupId("0"))

	users[0] = root
	seen := make(map[uint32]bool, len(ds)+1)
	seen[0] = true
	var st syscall.Stat_t
	for _, d := range ds {
		mylog.Check(syscall.Stat(d, &st))

		if seen[st.Uid] {
			continue
		}
		seen[st.Uid] = true
		usr := mylog.Check2(userLookupId(strconv.FormatUint(uint64(st.Uid), 10)))

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

	}

	return users, nil
}
