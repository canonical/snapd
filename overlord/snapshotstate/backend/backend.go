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
	"crypto"
	"encoding/json"
	"fmt"
	"hash"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

const (
	archiveName  = "archive.tgz"
	metadataName = "meta.json"
	metaHashName = "meta.sha3_384"

	userArchivePrefix = "user/"
	userArchiveSuffix = ".tgz"
)

// Iter loops over all valid snapshots in the snapshots directory, applying
// the given function to each. The snapshot will be closed after the function
// returns. If the function returns error, iteration is stopped (and if the
// error isn't EOF, it's returned as the error of the iterator).
func Iter(ctx context.Context, f func(*Reader) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dir, err := os.Open(dirs.SnapshotDir)
	if osutil.IsDirNotExist(err) {
		// no dir -> no snapshots
		return nil
	}

	var names []string
	for err == nil {
		names, err = dir.Readdirnames(100)
		for _, name := range names {
			if err = ctx.Err(); err != nil {
				break
			}

			var rsh *Reader
			// wish dirs had Open() that did openat
			rsh, err = Open(filepath.Join(dirs.SnapshotDir, name))
			if err != nil {
				err = nil
				continue
			}
			err = f(rsh)
			if closeError := rsh.Close(); err == nil {
				err = closeError
			}
			if err != nil {
				break
			}
		}
	}

	if err == io.EOF {
		err = nil
	}

	return err
}

// List valid snapshots
//
// The snapshots are closed before returning.
func List(ctx context.Context, snapshotID uint64, snapNames []string) ([]client.SnapshotSet, error) {
	shotsmap := map[uint64][]*client.Snapshot{}
	err := Iter(ctx, func(sh *Reader) error {
		if snapshotID == 0 || sh.SetID == snapshotID {
			if len(snapNames) == 0 || strutil.ListContains(snapNames, sh.Snap) {
				shotsmap[sh.SetID] = append(shotsmap[sh.SetID], &sh.Snapshot)
			}
		}
		return nil
	})

	sets := make([]client.SnapshotSet, 0, len(shotsmap))
	for id, shots := range shotsmap {
		sort.Sort(bySnap(shots))
		sets = append(sets, client.SnapshotSet{ID: id, Snapshots: shots})
	}

	sort.Sort(byID(sets))

	return sets, err
}

var isValidVersion = regexp.MustCompile("^[a-zA-Z0-9.+~-]{1,32}$").MatchString

// Filename of the given client.Snapshot in this backend.
func Filename(sh *client.Snapshot) string {
	var basename string
	if snap.ValidateName(sh.Snap) == nil {
		if isValidVersion(sh.Version) {
			basename = fmt.Sprintf("%s_%s_%d.zip", sh.Snap, sh.Version, sh.SetID)
		} else {
			// AFAIK snapcraft would fail these, but we're more generous
			basename = fmt.Sprintf("%s_rev%s_%d.zip", sh.Snap, sh.Revision, sh.SetID)
		}
	} else {
		// just give up
		basename = fmt.Sprintf("%d.zip", sh.SetID)
	}
	return filepath.Join(dirs.SnapshotDir, basename)
}

// Save a snapshot
func Save(ctx context.Context, id uint64, si *snap.Info, cfg *json.RawMessage, usernames []string) (sh *client.Snapshot, e error) {
	if err := os.MkdirAll(dirs.SnapshotDir, 0700); err != nil {
		return nil, err
	}

	sh = &client.Snapshot{
		SetID:    id,
		Snap:     si.Name(),
		Revision: si.Revision,
		Version:  si.Version,
		Time:     time.Now(),
		SHA3_384: make(map[string]string),
		Size:     0,
		Config:   cfg,
	}

	aw, err := osutil.NewAtomicFile(Filename(sh), 0600, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return nil, err
	}
	// if things worked, we'll commit (and Cancel becomes a NOP)
	defer aw.Cancel()

	w := zip.NewWriter(aw)
	// Note we don't “defer w.Close()” because Close on zip.Writers:
	// 1. doesn't close the file descriptor (that's done by Cancel, above)
	// 2. writes out the central directory of the zip
	// (yes they're weird in this way: why is that called Close, even)
	hasher := crypto.SHA3_384.New()
	if err := addToZip(ctx, sh, w, archiveName, si.DataDir(), hasher); err != nil {
		return nil, err
	}
	hasher.Reset()

	users, err := usersForUsernames(usernames)
	if err != nil {
		return nil, err
	}

	for _, usr := range users {
		if err := addToZip(ctx, sh, w, userArchiveName(usr), si.UserDataDir(usr.HomeDir), hasher); err != nil {
			return nil, err
		}
		hasher.Reset()
	}

	metaWriter, err := w.Create(metadataName)
	if err != nil {
		return nil, err
	}

	enc := json.NewEncoder(io.MultiWriter(metaWriter, hasher))
	if err := enc.Encode(sh); err != nil {
		return nil, err
	}

	hashWriter, err := w.Create(metaHashName)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(hashWriter, "%x\n", hasher.Sum(nil))
	w.Close()

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := aw.Commit(); err != nil {
		return nil, err
	}

	return sh, nil
}

func addToZip(ctx context.Context, sh *client.Snapshot, w *zip.Writer, entry, dir string, hasher hash.Hash) error {
	if exists, isDir, err := osutil.DirExists(dir); !exists || !isDir || err != nil {
		return err
	}
	parent, dir := filepath.Split(dir)

	archiveWriter, err := w.CreateHeader(&zip.FileHeader{Name: entry})
	if err != nil {
		return err
	}

	var sz sizer

	cmd := exec.Command("tar",
		"--create",
		"--sparse", "--gzip",
		"--directory", parent, dir, "common")
	cmd.Env = []string{"GZIP=-9 -n"}
	cmd.Stdout = io.MultiWriter(archiveWriter, hasher, &sz)
	cmd.Stderr = os.Stderr
	osutil.RunWithContext(ctx, cmd)

	sh.SHA3_384[entry] = fmt.Sprintf("%x", hasher.Sum(nil))
	sh.Size += sz.size

	return nil
}
