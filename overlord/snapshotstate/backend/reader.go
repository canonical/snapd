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
	"bytes"
	"crypto"
	"errors"
	"fmt"
	"hash"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sort"
	"syscall"

	"golang.org/x/net/context"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/sys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

// A Reader is a snapshot that's been opened for reading.
type Reader struct {
	*os.File
	client.Snapshot
}

// Open a Snapshot given its full filename.
//
// If the returned error is nil, the caller must close the reader (or
// its file) when done with it.
//
// If the returned error is non-nil, the returned Reader will be nil,
// *or* have a non-empty Broken; in the latter case its file will be
// closed.
func Open(fn string) (reader *Reader, e error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer func() {
		if e != nil && f != nil {
			f.Close()
		}
	}()

	reader = &Reader{
		File: f,
	}

	// first try to load the metadata itself
	var sz sizer
	hasher := crypto.SHA3_384.New()
	metaReader, metaSize, err := zipMember(f, metadataName)
	if err != nil {
		// no metadata file -> nothing to do :-(
		return nil, err
	}

	if err := jsonutil.DecodeWithNumber(io.TeeReader(metaReader, io.MultiWriter(hasher, &sz)), &reader.Snapshot); err != nil {
		return nil, err
	}

	// OK, from here on we have a Snapshot

	if !reader.IsValid() {
		reader.Broken = "invalid snapshot"
		return reader, errors.New(reader.Broken)
	}

	if sz.size != metaSize {
		reader.Broken = fmt.Sprintf("declared metadata size (%d) does not match actual (%d)", metaSize, sz.size)
		return reader, errors.New(reader.Broken)
	}

	actualMetaHash := fmt.Sprintf("%x", hasher.Sum(nil))

	// grab the metadata hash
	sz.Reset()
	metaHashReader, metaHashSize, err := zipMember(f, metaHashName)
	if err != nil {
		reader.Broken = err.Error()
		return reader, err
	}
	metaHashBuf, err := ioutil.ReadAll(io.TeeReader(metaHashReader, &sz))
	if err != nil {
		reader.Broken = err.Error()
		return reader, err
	}
	if sz.size != metaHashSize {
		reader.Broken = fmt.Sprintf("declared hash size (%d) does not match actual (%d)", metaHashSize, sz.size)
		return reader, errors.New(reader.Broken)
	}
	if expectedMetaHash := string(bytes.TrimSpace(metaHashBuf)); actualMetaHash != expectedMetaHash {
		reader.Broken = fmt.Sprintf("declared hash (%.7s…) does not match actual (%.7s…)", expectedMetaHash, actualMetaHash)
		return reader, errors.New(reader.Broken)
	}

	return reader, nil
}

func (r *Reader) checkOne(ctx context.Context, entry string, hasher hash.Hash) error {
	body, reportedSize, err := zipMember(r.File, entry)
	if err != nil {
		return err
	}
	defer body.Close()

	expectedHash := r.SHA3_384[entry]
	readSize, err := io.Copy(io.MultiWriter(osutil.ContextWriter(ctx), hasher), body)
	if err != nil {
		return err
	}

	if readSize != reportedSize {
		return fmt.Errorf("snapshot entry %q size (%d) different from actual (%d)", entry, reportedSize, readSize)
	}

	if actualHash := fmt.Sprintf("%x", hasher.Sum(nil)); actualHash != expectedHash {
		return fmt.Errorf("snapshot entry %q expected hash (%.7s…) does not match actual (%.7s…)", entry, expectedHash, actualHash)
	}
	return nil
}

// Check that the data contained in the snapshot matches its hashsums.
func (r *Reader) Check(ctx context.Context, usernames []string) error {
	sort.Strings(usernames)

	hasher := crypto.SHA3_384.New()
	for entry := range r.SHA3_384 {
		if len(usernames) > 0 && isUserArchive(entry) {
			username := entryUsername(entry)
			if !strutil.SortedListContains(usernames, username) {
				logger.Debugf("In checking snapshot %q, skipping entry %q by user request.", r.Name(), username)
				continue
			}
		}

		if err := r.checkOne(ctx, entry, hasher); err != nil {
			return err
		}
		hasher.Reset()
	}

	return nil
}

// Logf is the type implemented by logging functions.
type Logf func(format string, args ...interface{})

// Restore the data from the snapshot.
//
// If successful this will replace the existing data (for the revision in the
// snapshot) with that contained in the snapshot.  It keeps track of the old
// data in the task so it can be undone (or cleaned up).
func (r *Reader) Restore(ctx context.Context, usernames []string, logf Logf) (rs *RestoreState, e error) {
	rs = &RestoreState{}
	defer func() {
		if e != nil {
			logger.Noticef("Restore of snapshot %q failed (%v); undoing.", r.Name(), e)
			rs.Revert()
			rs = nil
		}
	}()

	sort.Strings(usernames)
	isRoot := sys.Geteuid() == 0
	si := snap.MinimalPlaceInfo(r.Snap, r.Revision)
	hasher := crypto.SHA3_384.New()
	var sz sizer

	for entry := range r.SHA3_384 {
		if err := ctx.Err(); err != nil {
			return rs, err
		}

		var dest string
		isUser := isUserArchive(entry)
		username := "root"
		uid := sys.UserID(osutil.NoChown)
		gid := sys.GroupID(osutil.NoChown)

		if !isUser {
			if entry != archiveName {
				// hmmm
				logf("Skipping restore of unknown entry %q.", entry)
				continue
			}
			dest = si.DataDir()
		} else {
			username = entryUsername(entry)
			if len(usernames) > 0 && !strutil.SortedListContains(usernames, username) {
				logger.Debugf("In restoring snapshot %q, skipping entry %q by user request.", r.Name(), username)
				continue
			}
			usr, err := userLookup(username)
			if err != nil {
				logf("Skipping restore of user %q: %v.", username, err)
				continue
			}

			dest = si.UserDataDir(usr.HomeDir)
			fi, err := os.Stat(usr.HomeDir)
			if err != nil {
				if osutil.IsDirNotExist(err) {
					logf("Skipping restore of %q as %q doesn't exist.", dest, usr.HomeDir)
				} else {
					logf("Skipping restore of %q: %v.", dest, err)
				}
				continue
			}

			if !fi.IsDir() {
				logf("Skipping restore of %q as %q is not a directory.", dest, usr.HomeDir)
				continue
			}

			if st, ok := fi.Sys().(*syscall.Stat_t); ok && isRoot {
				// the mkdir below will use the uid/gid of usr.HomeDir
				if st.Uid > 0 {
					uid = sys.UserID(st.Uid)
				}
				if st.Gid > 0 {
					gid = sys.GroupID(st.Gid)
				}
			}
		}
		parent, revdir := filepath.Split(dest)

		exists, isDir, err := osutil.DirExists(parent)
		if err != nil {
			return rs, err
		}
		if !exists {
			// NOTE that the chown won't happen (it'll be NoChown)
			// for the system path, and we won't be creating the
			// user's home (as we skip restore in that case).
			// Also no chown happens for root/root.
			if err := osutil.MkdirAllChown(parent, 0755, uid, gid); err != nil {
				return rs, err
			}
			rs.Created = append(rs.Created, parent)
		} else if !isDir {
			return rs, fmt.Errorf("Cannot restore snapshot into %q: not a directory.", parent)
		}

		tempdir, err := ioutil.TempDir(parent, ".snapshot")
		if err != nil {
			return rs, err
		}
		// one way or another we want tempdir gone
		defer func() {
			if err := os.RemoveAll(tempdir); err != nil {
				logf("Cannot clean up temporary directory %q: %v.", tempdir, err)
			}
		}()

		logger.Debugf("Restoring %q from %q into %q.", entry, r.Name(), tempdir)

		body, expectedSize, err := zipMember(r.File, entry)
		if err != nil {
			return rs, err
		}

		expectedHash := r.SHA3_384[entry]

		tr := io.TeeReader(body, io.MultiWriter(hasher, &sz))

		// resist the temptation of using archive/tar unless it's proven
		// that calling out to tar has issues -- there are a lot of
		// special cases we'd need to consider otherwise
		cmd := maybeRunuserCommand(username,
			"tar",
			"--extract",
			"--preserve-permissions", "--preserve-order", "--gunzip",
			"--directory", tempdir)
		cmd.Env = []string{}
		cmd.Stdin = tr
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stderr

		if err = osutil.RunWithContext(ctx, cmd); err != nil {
			return rs, err
		}

		if sz.size != expectedSize {
			return rs, fmt.Errorf("snapshot %q entry %q expected size (%d) does not match actual (%d)",
				r.Name(), entry, expectedSize, sz.size)
		}

		if actualHash := fmt.Sprintf("%x", hasher.Sum(nil)); actualHash != expectedHash {
			return rs, fmt.Errorf("snapshot %q entry %q expected hash (%.7s…) does not match actual (%.7s…)",
				r.Name(), entry, expectedHash, actualHash)
		}

		// TODO: something with Config

		for _, dir := range []string{"common", revdir} {
			source := filepath.Join(tempdir, dir)
			if exists, _, err := osutil.DirExists(source); err != nil {
				return rs, err
			} else if !exists {
				continue
			}
			target := filepath.Join(parent, dir)
			exists, _, err := osutil.DirExists(target)
			if err != nil {
				return rs, err
			}
			if exists {
				rsfn := restoreStateFilename(target)
				if err := os.Rename(target, rsfn); err != nil {
					return rs, err
				}
				rs.Moved = append(rs.Moved, rsfn)
			}

			if err := os.Rename(source, target); err != nil {
				return rs, err
			}
			rs.Created = append(rs.Created, target)
		}

		sz.Reset()
		hasher.Reset()
	}

	return rs, nil
}
