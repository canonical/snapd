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
	"os/exec"
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
func Open(fn string) (rsh *Reader, e error) {
	f, err := os.Open(fn)
	if err != nil {
		return nil, err
	}
	defer func() {
		if e != nil && f != nil {
			f.Close()
		}
	}()

	rsh = &Reader{
		File: f,
	}

	// first try to load the metadata itself
	var sz sizer
	hasher := crypto.SHA3_384.New()
	metaReader, metaSize, err := member(f, metadataName)
	if err != nil {
		// no metadata file -> nothing to do :-(
		return nil, err
	}

	if err := jsonutil.DecodeWithNumber(io.TeeReader(metaReader, io.MultiWriter(hasher, &sz)), &rsh.Snapshot); err != nil {
		return nil, err
	}

	// OK, from here on we have a Snapshot

	if !rsh.IsValid() {
		rsh.Broken = "invalid snapshot"
		return rsh, errors.New(rsh.Broken)
	}

	if sz.size != metaSize {
		rsh.Broken = fmt.Sprintf("declared metadata size (%d) does not match actual (%d)", metaSize, sz.size)
		return rsh, errors.New(rsh.Broken)
	}

	actualMetaHash := fmt.Sprintf("%x", hasher.Sum(nil))

	// grab the metadata hash
	sz.Reset()
	metaHashReader, metaHashSize, err := member(f, metaHashName)
	if err != nil {
		rsh.Broken = err.Error()
		return rsh, err
	}
	metaHashBuf, err := ioutil.ReadAll(io.TeeReader(metaHashReader, &sz))
	if err != nil {
		rsh.Broken = err.Error()
		return rsh, err
	}
	if sz.size != metaHashSize {
		rsh.Broken = fmt.Sprintf("declared hash size (%d) does not match actual (%d)", metaHashSize, sz.size)
		return rsh, errors.New(rsh.Broken)
	}
	if expectedMetaHash := string(bytes.TrimSpace(metaHashBuf)); actualMetaHash != expectedMetaHash {
		logger.Noticef("Declared metadata hash (%s) does not match actual (%s).", expectedMetaHash, actualMetaHash)
		rsh.Broken = "metadata hash mismatch"
		return rsh, errors.New(rsh.Broken)
	}

	return rsh, nil
}

func (r *Reader) checkOne(ctx context.Context, entry string, hasher hash.Hash) error {
	body, reportedSize, err := member(r.File, entry)
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
		logger.Noticef("Snapshot entry %q expected hash (%s) does not match actual (%s).", entry, expectedHash, actualHash)
		return fmt.Errorf("snapshot entry %q expected hash different from actual", entry)
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

// A Logf writes somewhere the user will see it
//
// (typically this is task.Logf when running via the overlord, or fmt.Printf
// when running direct)
type Logf func(format string, args ...interface{})

// Restore the data from the snapshot.
//
// If successful this will replace the existing data (for the revision
// in the snapshot) with that contained in the snapshot.
func (r *Reader) Restore(ctx context.Context, usernames []string, logf Logf) error {
	_, err := r.restore(ctx, false, usernames, logf)
	return err
}

// RestoreLeavingTrash is like Restore, but it leaves the old data in
// a trash directory.
func (r *Reader) RestoreLeavingTrash(ctx context.Context, usernames []string, logf Logf) (*Trash, error) {
	return r.restore(ctx, true, usernames, logf)
}

func (r *Reader) restore(ctx context.Context, leaveTrash bool, usernames []string, logf Logf) (b *Trash, e error) {
	b = &Trash{}
	defer func() {
		if e != nil {
			logger.Noticef("Restore of snapshot %q failed (%v); undoing.", r.Name(), e)
			b.Revert()
			b = nil
		} else if !leaveTrash {
			logger.Debugf("Restore of snapshot succeeded and no trash requested; cleaning up.")
			b.Cleanup()
			b = nil
		}
	}()

	sort.Strings(usernames)
	isRoot := sys.Geteuid() == 0
	si := snap.MinimalPlaceInfo(r.Snap, r.Revision)
	hasher := crypto.SHA3_384.New()
	var sz sizer

	for entry := range r.SHA3_384 {
		if err := ctx.Err(); err != nil {
			return b, err
		}

		var dest string
		isUser := isUserArchive(entry)
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
			username := entryUsername(entry)
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
			return b, err
		}
		if !exists {
			// NOTE that the chown won't happen (it'll be NoChown)
			// for the system path, and we won't be creating the
			// user's home (as we skip restore in that case).
			// Also no chown happens for root/root.
			if err := osutil.MkdirAllChown(parent, 0755, uid, gid); err != nil {
				return b, err
			}
			b.Created = append(b.Created, parent)
		} else if !isDir {
			return b, fmt.Errorf("Cannot restore snapshot into %q: not a directory.", parent)
		}

		tempdir, err := ioutil.TempDir(parent, ".snapshot")
		if err != nil {
			return b, err
		}
		// one way or another we want tempdir gone
		defer func() {
			if err := os.RemoveAll(tempdir); err != nil {
				logf("Cannot clean up temporary directory %q: %v.", tempdir, err)
			}
		}()

		logger.Debugf("Restoring %q from %q into %q.", entry, r.Name(), tempdir)

		body, expectedSize, err := member(r.File, entry)
		if err != nil {
			return b, err
		}

		expectedHash := r.SHA3_384[entry]

		tr := io.TeeReader(body, io.MultiWriter(hasher, &sz))

		// resist the temptation of using archive/tar unless it's proven
		// that calling out to tar has issues -- there are a lot of
		// special cases we'd need to consider otherwise
		cmd := exec.Command("tar",
			"--extract",
			"--preserve-permissions", "--preserve-order", "--gunzip",
			"--directory", tempdir)
		cmd.Env = []string{}
		cmd.Stdin = tr
		cmd.Stderr = os.Stderr
		cmd.Stdout = os.Stderr

		if err = osutil.RunWithContext(ctx, cmd); err != nil {
			return b, err
		}

		if sz.size != expectedSize {
			return b, fmt.Errorf("snapshot %q entry %q expected size (%d) does not match actual (%d)", r.Name(), entry, expectedSize, sz.size)
		}

		if actualHash := fmt.Sprintf("%x", hasher.Sum(nil)); actualHash != expectedHash {
			logger.Noticef("Snapshot %q entry %q expected hash (%s) does not match actual (%s).",
				r.Name(), entry, expectedHash, actualHash)
			return b, fmt.Errorf("snapshot %q entry %q expected hash does not match actual", r.Name(), entry)
		}

		// TODO: something with Config

		for _, dir := range []string{"common", revdir} {
			source := filepath.Join(tempdir, dir)
			if exists, _, err := osutil.DirExists(source); err != nil {
				return b, err
			} else if !exists {
				continue
			}
			target := filepath.Join(parent, dir)
			exists, _, err := osutil.DirExists(target)
			if err != nil {
				return b, err
			}
			if exists {
				trash, err := nextTrash(target)
				if err != nil {
					return b, err
				}
				if err := os.Rename(target, trash); err != nil {
					return b, err
				}
				b.Moved = append(b.Moved, trash)
			}

			if err := os.Rename(source, target); err != nil {
				return b, err
			}
			b.Created = append(b.Created, target)
		}

		sz.Reset()
		hasher.Reset()
	}

	return b, nil
}
