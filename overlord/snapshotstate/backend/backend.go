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
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
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

var (
	// Stop is used to ask Iter to stop iteration, without it being an error.
	Stop = errors.New("stop iteration")

	osOpen      = os.Open
	dirNames    = (*os.File).Readdirnames
	backendOpen = Open
)

// Iter loops over all snapshots in the snapshots directory, applying the given
// function to each. The snapshot will be closed after the function returns. If
// the function returns an error, iteration is stopped (and if the error isn't
// Stop, it's returned as the error of the iterator).
func Iter(ctx context.Context, f func(*Reader) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	dir, err := osOpen(dirs.SnapshotsDir)
	if err != nil {
		if osutil.IsDirNotExist(err) {
			// no dir -> no snapshots
			return nil
		}
		return fmt.Errorf("cannot open snapshots directory: %v", err)
	}
	defer dir.Close()

	var names []string
	var readErr error
	for readErr == nil && err == nil {
		names, readErr = dirNames(dir, 100)
		// note os.Readdirnames can return a non-empty names and a non-nil err
		for _, name := range names {
			if err = ctx.Err(); err != nil {
				break
			}

			filename := filepath.Join(dirs.SnapshotsDir, name)
			reader, openError := backendOpen(filename)
			// reader can be non-nil even when openError is not nil (in
			// which case reader.Broken will have a reason). f can
			// check and either ignore or return an error when
			// finding a broken snapshot.
			if reader != nil {
				err = f(reader)
			} else {
				// TODO: use warnings instead
				logger.Noticef("Cannot open snapshot %q: %v.", name, openError)
			}
			if openError == nil {
				// if openError was nil the snapshot was opened and needs closing
				if closeError := reader.Close(); err == nil {
					err = closeError
				}
			}
			if err != nil {
				break
			}
		}
	}

	if readErr != nil && readErr != io.EOF {
		return readErr
	}

	if err == Stop {
		err = nil
	}

	return err
}

// List valid snapshots sets.
func List(ctx context.Context, setID uint64, snapNames []string) ([]client.SnapshotSet, error) {
	setshots := map[uint64][]*client.Snapshot{}
	err := Iter(ctx, func(reader *Reader) error {
		if setID == 0 || reader.SetID == setID {
			if len(snapNames) == 0 || strutil.ListContains(snapNames, reader.Snap) {
				setshots[reader.SetID] = append(setshots[reader.SetID], &reader.Snapshot)
			}
		}
		return nil
	})

	sets := make([]client.SnapshotSet, 0, len(setshots))
	for id, shots := range setshots {
		sort.Sort(bySnap(shots))
		sets = append(sets, client.SnapshotSet{ID: id, Snapshots: shots})
	}

	sort.Sort(byID(sets))

	return sets, err
}

// Filename of the given client.Snapshot in this backend.
func Filename(snapshot *client.Snapshot) string {
	// this _needs_ the snap name and version to be valid
	return filepath.Join(dirs.SnapshotsDir, fmt.Sprintf("%d_%s_%s_%s.zip", snapshot.SetID, snapshot.Snap, snapshot.Version, snapshot.Revision))
}

// Save a snapshot
func Save(ctx context.Context, id uint64, si *snap.Info, cfg map[string]interface{}, usernames []string) (*client.Snapshot, error) {
	if err := os.MkdirAll(dirs.SnapshotsDir, 0700); err != nil {
		return nil, err
	}

	snapshot := &client.Snapshot{
		SetID:    id,
		Snap:     si.InstanceName(),
		Revision: si.Revision,
		Version:  si.Version,
		Time:     time.Now(),
		SHA3_384: make(map[string]string),
		Size:     0,
		Conf:     cfg,
	}

	aw, err := osutil.NewAtomicFile(Filename(snapshot), 0600, 0, osutil.NoChown, osutil.NoChown)
	if err != nil {
		return nil, err
	}
	// if things worked, we'll commit (and Cancel becomes a NOP)
	defer aw.Cancel()

	w := zip.NewWriter(aw)
	defer w.Close() // note this does not close the file descriptor (that's done by hand on the atomic writer, above)
	if err := addDirToZip(ctx, snapshot, w, "root", archiveName, si.DataDir()); err != nil {
		return nil, err
	}

	users, err := usersForUsernames(usernames)
	if err != nil {
		return nil, err
	}

	for _, usr := range users {
		if err := addDirToZip(ctx, snapshot, w, usr.Username, userArchiveName(usr), si.UserDataDir(usr.HomeDir)); err != nil {
			return nil, err
		}
	}

	metaWriter, err := w.Create(metadataName)
	if err != nil {
		return nil, err
	}

	hasher := crypto.SHA3_384.New()
	enc := json.NewEncoder(io.MultiWriter(metaWriter, hasher))
	if err := enc.Encode(snapshot); err != nil {
		return nil, err
	}

	hashWriter, err := w.Create(metaHashName)
	if err != nil {
		return nil, err
	}
	fmt.Fprintf(hashWriter, "%x\n", hasher.Sum(nil))
	if err := w.Close(); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if err := aw.Commit(); err != nil {
		return nil, err
	}

	return snapshot, nil
}

func addDirToZip(ctx context.Context, snapshot *client.Snapshot, w *zip.Writer, username string, entry, dir string) error {
	hasher := crypto.SHA3_384.New()
	if exists, isDir, err := osutil.DirExists(dir); !exists || !isDir || err != nil {
		if exists && !isDir {
			logger.Noticef("Not saving %q in snapshot #%d of %q as it is not a directory.", dir, snapshot.SetID, snapshot.Snap)
		}
		return err
	}
	parent, dir := filepath.Split(dir)

	archiveWriter, err := w.CreateHeader(&zip.FileHeader{Name: entry})
	if err != nil {
		return err
	}

	var sz sizer

	cmd := maybeRunuserCommand(username,
		"tar",
		"--create",
		"--sparse", "--gzip",
		"--directory", parent, dir, "common")
	cmd.Env = []string{"GZIP=-9 -n"}
	cmd.Stdout = io.MultiWriter(archiveWriter, hasher, &sz)
	matchCounter := &strutil.MatchCounter{Regexp: regexp.MustCompile(".*"), N: 1}
	cmd.Stderr = matchCounter
	if err := osutil.RunWithContext(ctx, cmd); err != nil {
		matches, count := matchCounter.Matches()
		if count > 0 {
			return fmt.Errorf("cannot create archive: %s (and %d more)", matches[0], count-1)
		}
		return fmt.Errorf("tar failed: %v", err)
	}

	snapshot.SHA3_384[entry] = fmt.Sprintf("%x", hasher.Sum(nil))
	snapshot.Size += sz.size

	return nil
}
