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

// AtomicWriteFile updates the filename atomically and works otherwise
// like io/ioutil.WriteFile()
//
// Note that it won't follow symlinks and will replace existing symlinks
// with the real file
func AtomicWriteFile(filename string, data []byte, perm os.FileMode, flags AtomicWriteFlags) (err error) {
	return AtomicWriteChown(filename, bytes.NewReader(data), perm, flags, -1, -1)
}

func AtomicWrite(filename string, reader io.Reader, perm os.FileMode, flags AtomicWriteFlags) (err error) {
	return AtomicWriteChown(filename, reader, perm, flags, -1, -1)
}

func AtomicWriteFileChown(filename string, data []byte, perm os.FileMode, flags AtomicWriteFlags, uid, gid int) (err error) {
	return AtomicWriteChown(filename, bytes.NewReader(data), perm, flags, uid, gid)
}

func AtomicWriteChown(filename string, reader io.Reader, perm os.FileMode, flags AtomicWriteFlags, uid, gid int) (err error) {
	aw, err := NewAtomicWriter(filename, perm, flags, uid, gid)
	if err != nil {
		return err
	}

	defer func() {
		if err != nil {
			os.Remove(aw.Name())
		}
	}()

	if _, err := io.Copy(aw, reader); err != nil {
		return err
	}

	return aw.Commit()
}

type AtomicWriter interface {
	io.WriteCloser
	Commit() error
	Name() string
}

type atomicWriter struct {
	*os.File

	target  string
	tmpname string
	uid     int
	gid     int
	renamed bool
}

func NewAtomicWriter(filename string, perm os.FileMode, flags AtomicWriteFlags, uid, gid int) (aw AtomicWriter, err error) {
	if (uid < 0) != (gid < 0) {
		return nil, errors.New("internal error: AtomicWriteChown needs none or both of uid and gid set")
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

	return &atomicWriter{
		File:    fd,
		target:  filename,
		tmpname: tmp,
		uid:     uid,
		gid:     gid,
	}, nil
}

func (aw *atomicWriter) Name() string {
	if aw.renamed {
		return aw.target
	}

	return aw.tmpname
}

func (aw *atomicWriter) Commit() error {
	if aw.uid > -1 && aw.gid > -1 {
		if err := aw.Chown(aw.uid, aw.gid); err != nil {
			aw.Close()
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
	aw.renamed = true

	if !snapdUnsafeIO {
		if err := dir.Sync(); err != nil {
			return err
		}
	}

	return aw.Close()
}
