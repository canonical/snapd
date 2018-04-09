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
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
)

func nextTrash(fn string) (string, error) {
	for n := 1; n < 100; n++ {
		cand := fmt.Sprintf("%s.~%d~", fn, n)
		if exists, _, _ := osutil.DirExists(cand); !exists {
			return cand, nil
		}
	}
	return "", fmt.Errorf("cannot find a trash name for %q", fn)
}

var trashRx = regexp.MustCompile(`\.~\d+~$`)

func trash2orig(fn string) string {
	if idx := trashRx.FindStringIndex(fn); len(idx) > 0 {
		return fn[:idx[0]]
	}
	return ""
}

func member(f *os.File, member string) (r io.ReadCloser, sz int64, err error) {
	if f == nil {
		// maybe "not open"?
		return nil, -1, io.EOF
	}

	// rewind the file
	// (shouldn't be needed, but doesn't hurt too much)
	if _, err := f.Seek(0, 0); err != nil {
		return nil, -1, err
	}

	fi, err := f.Stat()

	arch, err := zip.NewReader(f, fi.Size())
	if err != nil {
		return nil, -1, err
	}

	for _, f := range arch.File {
		if f.Name == member {
			rc, err := f.Open()
			return rc, int64(f.UncompressedSize64), err
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
			usr, err = userLookupId(username)
			if err != nil {
				return nil, err
			}
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
	seen := make(map[uint32]struct{}, len(ds)+1)
	var yes struct{}
	var st syscall.Stat_t
	for _, d := range ds {
		err := syscall.Stat(d, &st)
		if err != nil {
			continue
		}
		if _, ok := seen[st.Uid]; ok {
			continue
		}
		seen[st.Uid] = yes
		usr, err := user.LookupId(strconv.FormatUint(uint64(st.Uid), 10))
		if err != nil {
			return nil, err
		}
		users = append(users, usr)
	}

	return users, nil
}
