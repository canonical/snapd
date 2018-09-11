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
	"strconv"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil/sys"
)

func zipMember(f *os.File, member string) (r io.ReadCloser, sz int64, err error) {
	// rewind the file
	// (shouldn't be needed, but doesn't hurt too much)
	if _, err := f.Seek(0, 0); err != nil {
		return nil, -1, err
	}

	fi, err := f.Stat()
	if err != nil {
		return nil, -1, err
	}

	arch, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, -1, err
	}

	for _, fh := range arch.File {
		if fh.Name == member {
			r, err = fh.Open()
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

func usersForUsernames(usernames []string) ([]*user.User, error) {
	if len(usernames) == 0 {
		return allUsers()
	}
	users := make([]*user.User, 0, len(usernames))
	for _, username := range usernames {
		usr, err := userLookup(username)
		if err != nil {
			if _, ok := err.(*user.UnknownUserError); !ok {
				return nil, err
			}
			u, e := userLookupId(username)
			if e != nil {
				// return first error, as it's usually clearer
				return nil, err
			}
			usr = u
		}
		users = append(users, usr)

	}
	return users, nil
}

func allUsers() ([]*user.User, error) {
	ds, err := filepath.Glob(dirs.SnapDataHomeGlob)
	if err != nil {
		// can't happen?
		return nil, err
	}

	users := make([]*user.User, 1, len(ds)+1)
	root, err := user.LookupId("0")
	if err != nil {
		return nil, err
	}
	users[0] = root
	seen := make(map[uint32]bool, len(ds)+1)
	seen[0] = true
	var st syscall.Stat_t
	for _, d := range ds {
		err := syscall.Stat(d, &st)
		if err != nil {
			continue
		}
		if seen[st.Uid] {
			continue
		}
		seen[st.Uid] = true
		usr, err := user.LookupId(strconv.FormatUint(uint64(st.Uid), 10))
		if err != nil {
			return nil, err
		}
		users = append(users, usr)
	}

	return users, nil
}

// maybeRunuserCommand returns an exec.Cmd that will, if the current
// effective user id is 0 and username is not "root", call runuser(1)
// to change to the given username before running the given command.
//
// If username is "root", or the effective user id is 0, the given
// command is passed directly to exec.Command.
//
// TODO: maybe move this to osutil
func maybeRunuserCommand(username string, args ...string) *exec.Cmd {
	if username == "root" || sys.Geteuid() != 0 {
		// runuser only works for euid 0, and doesn't make sense for root
		return exec.Command(args[0], args[1:]...)
	}
	sudoArgs := make([]string, len(args)+3)
	copy(sudoArgs[3:], args)
	sudoArgs[0] = "-u"
	sudoArgs[1] = username
	sudoArgs[2] = "--"

	return exec.Command("runuser", sudoArgs...)
}

func MockUserLookup(newLookup func(string) (*user.User, error)) func() {
	oldLookup := userLookup
	userLookup = newLookup
	return func() {
		userLookup = oldLookup
	}
}
