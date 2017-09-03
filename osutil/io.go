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
	if (uid < 0) != (gid < 0) {
		return errors.New("internal error: AtomicWriteChown needs none or both of uid and gid set")
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

	// XXX: if go switches to use aio_fsync, we need to open the dir for writing
	dir, err := os.Open(filepath.Dir(filename))
	if err != nil {
		return err
	}
	defer dir.Close()

	fd, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|os.O_EXCL, perm)
	if err != nil {
		return err
	}
	defer func() {
		e := fd.Close()
		if err == nil {
			err = e
		}
		if err != nil {
			os.Remove(tmp)
		}
	}()

	if _, err := io.Copy(fd, reader); err != nil {
		return err
	}

	if uid > -1 && gid > -1 {
		if err := fd.Chown(uid, gid); err != nil {
			return err
		}
	}

	if !snapdUnsafeIO {
		if err := fd.Sync(); err != nil {
			return err
		}
	}

	if err := os.Rename(tmp, filename); err != nil {
		return err
	}

	if !snapdUnsafeIO {
		return dir.Sync()
	}
	return nil
}
