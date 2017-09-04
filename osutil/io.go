// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/strutil"
)

// AtomicWriteFlags are a bitfield of flags for AtomicWriteFile
type AtomicWriteFlags uint

const (
	// AtomicWriteFollow makes AtomicWriteFile follow symlinks
	AtomicWriteFollow AtomicWriteFlags = 1 << iota
)

// Allow disabling sync for testing. This brings massive improvements on
// certain filesystems (like btrfs) and very much noticeable improvements in
// all unit tests in genreal.
var snapdUnsafeIO bool = len(os.Args) > 0 && strings.HasSuffix(os.Args[0], ".test") && GetenvBool("SNAPD_UNSAFE_IO")

// An AtomicWriter is an io.WriteCloser that has a Finalize() method that does
// whatever needs to be done so the edition is "atomic": an AtomicWriter will
// do its best to leave either the previous content or the new content in
// permanent storage. It also has a Cancel() method to abort and clean up.
type AtomicWriter interface {
	io.WriteCloser

	// Finalize the writing operation and make it permanent.
	//
	// If Finalize succeeds, the file is closed and further attempts to write will
	// fail. If Finalize fails, Cancel() needs to be called to clean up.
	Finalize() error

	// Cancel closes the AtomicWriter, and cleans up any artifacts. Cancel
	// can fail if Finalize() was (even partially) successful.
	//
	// It's an error to call Cancel once Close has been called.
	Cancel() error
}

type atomicFile struct {
	*os.File

	target  string
	tmpname string
	uid     int
	gid     int
	renamed bool
}

// NewAtomicFile builds an AtomicWriter backed by an *os.File that will have
// the given filename, permissions and uid/gid when Finalized.
//
//   It _might_ be implemented using O_TMPFILE (see open(2)).
//
// It is the caller's responsibility to clean up on error, by calling Cancel().
//
// Note that it won't follow symlinks and will replace existing symlinks with
// the real file, unless the AtomicWriteFollow flag is specified.
func NewAtomicFile(filename string, perm os.FileMode, flags AtomicWriteFlags, uid, gid int) (aw AtomicWriter, err error) {
	if (uid < 0) != (gid < 0) {
		return nil, errors.New("internal error: AtomicFile needs none or both of uid and gid set")
	}

	if flags&AtomicWriteFollow != 0 {
		if fn, err := os.Readlink(filename); err == nil || (fn != "" && os.IsNotExist(err)) {
			if filepath.IsAbs(fn) {
				filename = fn
			} else {
				filename = filepath.Join(filepath.Dir(filename), fn)
			}
		}
	}
	tmp := filename + "." + strutil.MakeRandomString(12)

	fd, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL, perm)
	if err != nil {
		return nil, err
	}

	return &atomicFile{
		File:    fd,
		target:  filename,
		tmpname: tmp,
		uid:     uid,
		gid:     gid,
	}, nil
}

// ErrCannotCancel means the Finalize operation failed at the last step, and
// your luck has run out.
var ErrCannotCancel = errors.New("cannot cancel: file has already been renamed")

func (aw *atomicFile) Cancel() error {
	if aw.renamed {
		return ErrCannotCancel
	}
	if err := aw.Close(); err != nil {
		return err
	}
	if aw.tmpname != "" {
		return os.Remove(aw.tmpname)
	}

	return nil
}

var chown = (*os.File).Chown

func (aw *atomicFile) Finalize() error {
	if aw.uid > -1 && aw.gid > -1 {
		if err := chown(aw.File, aw.uid, aw.gid); err != nil {
			return err
		}
	}

	var dir *os.File
	if !snapdUnsafeIO {
		// XXX: if go switches to use aio_fsync, we need to open the dir for writing
		d, err := os.Open(filepath.Dir(aw.target))
		if err != nil {
			return err
		}
		dir = d
		defer dir.Close()

		if err := aw.Sync(); err != nil {
			return err
		}
	}

	if err := os.Rename(aw.tmpname, aw.target); err != nil {
		return err
	}
	aw.renamed = true // it is now too late to Cancel()

	if !snapdUnsafeIO {
		if err := dir.Sync(); err != nil {
			return err
		}
	}

	// given we called Sync before, Close _shouldn't_ be able to
	// fail. Still, stuff happens.
	return aw.Close()
}

// The AtomicWrite* family of functions work like ioutil.WriteFile(), but the
// file created is an AtomicWriter, which is Finalized before returning.
//
// AtomicWriteChown and AtomicWriteFileChown take an uid and a gid that can be
// used to specify the ownership of the created file. They must be both
// non-negative (in which case chown is called), or both negative (in which
// case it isn't).
//
// AtomicWriteFile and AtomicWriteFileChown take the content to be written as a
// []byte, and so work exactly like io.WriteFile(); AtomicWrite and
// AtomicWriteChown take an io.Reader which is copied into the file instead,
// and so are more amenable to streaming.
func AtomicWrite(filename string, reader io.Reader, perm os.FileMode, flags AtomicWriteFlags) (err error) {
	return AtomicWriteChown(filename, reader, perm, flags, -1, -1)
}

func AtomicWriteFile(filename string, data []byte, perm os.FileMode, flags AtomicWriteFlags) (err error) {
	return AtomicWriteChown(filename, bytes.NewReader(data), perm, flags, -1, -1)
}

func AtomicWriteFileChown(filename string, data []byte, perm os.FileMode, flags AtomicWriteFlags, uid, gid int) (err error) {
	return AtomicWriteChown(filename, bytes.NewReader(data), perm, flags, uid, gid)
}

func AtomicWriteChown(filename string, reader io.Reader, perm os.FileMode, flags AtomicWriteFlags, uid, gid int) (err error) {
	aw, err := NewAtomicFile(filename, perm, flags, uid, gid)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			// XXX there's a small window where Finalize can fail in the last two steps
			// (syncing the containing directory, or closing the file), and this quietly
			// ignores that -- not that there'd be much we could do!
			aw.Cancel()
		}
	}()

	if _, err := io.Copy(aw, reader); err != nil {
		return err
	}

	return aw.Finalize()
}
