// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package backend

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/snap"
)

// zipMember returns an io.ReadCloser for the 'member' file in the 'f' zip file.
func zipMember(f *os.File, member string) (r io.ReadCloser, sz int64, err error) {
	mylog.Check2(
		// rewind the file
		// (shouldn't be needed, but doesn't hurt too much)
		f.Seek(0, 0))

	fi := mylog.Check2(f.Stat())

	arch := mylog.Check2(zip.NewReader(f, fi.Size()))

	for _, fh := range arch.File {
		if fh.Name == member {
			r = mylog.Check2(fh.Open())
			return r, int64(fh.UncompressedSize64), err
		}
	}

	return nil, -1, fmt.Errorf("missing archive member %q", member)
}

func userArchiveName(usr *user.User) string {
	return filepath.Join(userArchivePrefix, usr.Username+userArchiveSuffix)
}

func isUserArchive(entry string) bool {
	return strings.HasPrefix(entry, userArchivePrefix) && strings.HasSuffix(entry, userArchiveSuffix)
}

func entryUsername(entry string) string {
	// this _will_ panic if !isUserArchive(entry)
	return entry[len(userArchivePrefix) : len(entry)-len(userArchiveSuffix)]
}

type bySnap []*client.Snapshot

func (a bySnap) Len() int           { return len(a) }
func (a bySnap) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a bySnap) Less(i, j int) bool { return a[i].Snap < a[j].Snap }

type byID []client.SnapshotSet

func (a byID) Len() int           { return len(a) }
func (a byID) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byID) Less(i, j int) bool { return a[i].ID < a[j].ID }

var (
	userLookup   = user.Lookup
	userLookupId = user.LookupId
)

func usersForUsernamesImpl(usernames []string, opts *dirs.SnapDirOptions) ([]*user.User, error) {
	if len(usernames) == 0 {
		return snap.AllUsers(opts)
	}
	users := make([]*user.User, 0, len(usernames))
	for _, username := range usernames {
		usr := mylog.Check2(userLookup(username))

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

		// return first error, as it's usually clearer

		users = append(users, usr)

	}
	return users, nil
}

var (
	sysGeteuid   = sys.Geteuid
	execLookPath = exec.LookPath
)

func pickUserWrapper() string {
	// runuser and sudo happen to work the same way in this case.  The main
	// reason to prefer runuser over sudo is that runuser is part of
	// util-linux, which is considered essential, whereas sudo is an addon
	// which could be removed.  However util-linux < 2.23 does not have
	// runuser, and we support some distros that ship things older than that
	// (e.g. Ubuntu 14.04)
	for _, cmd := range []string{"runuser", "sudo"} {
		if lp := mylog.Check2(execLookPath(cmd)); err == nil {
			return lp
		}
	}
	return ""
}

var userWrapper = pickUserWrapper()

// tarAsUser returns an exec.Cmd that will, if the current effective user id is
// 0 and username is not "root", and if either runuser(1) or sudo(8) are found
// on the PATH, run tar as the given user.
//
// If the effective user id is not 0, or username is "root", exec.Command is
// used directly; changing the user id would fail (in the first case) or be a
// no-op (in the second).
//
// If neither runuser nor sudo are found on the path, exec.Command is also used
// directly. This will result in tar running as root in this situation (so it
// will fail if on NFS; I don't think there's an attack vector though).
var tarAsUser = func(username string, args ...string) *exec.Cmd {
	if sysGeteuid() == 0 && username != "root" {
		if userWrapper != "" {
			uwArgs := make([]string, len(args)+5)
			uwArgs[0] = userWrapper
			uwArgs[1] = "-u"
			uwArgs[2] = username
			uwArgs[3] = "--"
			uwArgs[4] = "tar"
			copy(uwArgs[5:], args)
			return &exec.Cmd{
				Path: userWrapper,
				Args: uwArgs,
			}
		}
		// TODO: use warnings instead
		logger.Noticef("No user wrapper found; running tar for user data as root. Please make sure 'sudo' or 'runuser' (from util-linux) is on $PATH to avoid this.")
	}

	return exec.Command("tar", args...)
}

func MockUserLookup(newLookup func(string) (*user.User, error)) func() {
	oldLookup := userLookup
	userLookup = newLookup
	return func() {
		userLookup = oldLookup
	}
}
