// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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
	"os"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/snapcore/snapd/osutil/sys"
)

// XXX: we need to come back and fix this; this is a hack to unblock us.
// Have a lock so that if one goroutine tries to Mkdir /foo/bar, and
// another tries to Mkdir /foo/baz, they can't both decide they need
// to make /foo and then have one fail.
var mu sync.Mutex

// MkdirOptions holds the options for a call to Mkdir.
type MkdirOptions struct {
	// If false (the default), return an error if the parent directory is missing.
	// If true, any missing parent directories of the given path are created
	// (with the same permissions and owner/group as the leaf directory).
	MakeParents bool

	// If false (the default), return an error if the target directory already exists.
	// If true, don't return an error if the target directory already exists (unless
	// it's not a directory).
	ExistOK bool

	// If false (the default), no explicit chmod is performed. In this case, the permission
	// of the created directories will be affected by umask settings.
	//
	// If true, perform an explicit chmod on any directories created.
	Chmod bool

	// If false (the default), no explicit chown is performed.
	// If true, perform an explicit chown on any directories created, using the UserID
	// and GroupID provided.
	Chown   bool
	UserID  sys.UserID
	GroupID sys.GroupID
}

// Mkdir creates a directory at path with the permissions perm and the options provided.
// If options is nil, it is treated as &MkdirOptions{}.
func Mkdir(path string, perm os.FileMode, options *MkdirOptions) error {
	if options == nil {
		options = &MkdirOptions{}
	}
	mu.Lock()
	defer mu.Unlock()

	path = filepath.Clean(path)

	if s, err := os.Stat(path); err == nil {
		// If path exists but not as a directory, return a "not a directory" error.
		if !s.IsDir() {
			return &os.PathError{
				Op:   "mkdir",
				Path: path,
				Err:  syscall.ENOTDIR,
			}
		}

		// If path exists as a directory, and ExistOK option is set, do nothing.
		if options.ExistOK {
			return nil
		}

		// If path exists but ExistOK option isn't set, return a "file exists" error.
		return &os.PathError{
			Op:   "mkdir",
			Path: path,
			Err:  syscall.EEXIST,
		}
	}

	// If path doesn't exist, create it.
	return mkdirAll(path, perm, options)
}

// create directories recursively
func mkdirAll(path string, perm os.FileMode, options *MkdirOptions) error {
	// if path exists
	if s, err := os.Stat(path); err == nil {
		if s.IsDir() {
			return nil
		}

		// If path exists but not as a directory, return a "not a directory" error.
		return &os.PathError{
			Op:   "mkdir",
			Path: path,
			Err:  syscall.ENOTDIR,
		}
	}

	// If path doesn't exist, and MakeParents is specified in options,
	// create all directories recursively.
	if options.MakeParents {
		parent := filepath.Dir(path)
		if parent != "/" {
			if err := mkdirAll(parent, perm, options); err != nil {
				return err
			}
		}
	}

	// If path doesn't exist, and MakeParents isn't specified in options,
	// create a single directory.
	return mkdir(path, perm, options)
}

// Create a single directory and perform chown/chmod operations according to options.
func mkdir(path string, perm os.FileMode, options *MkdirOptions) error {
	cand := path + ".mkdir-new"

	if err := os.Mkdir(cand, perm); err != nil && !os.IsExist(err) {
		return err
	}

	if options.Chown {
		if err := sys.ChownPath(cand, options.UserID, options.GroupID); err != nil {
			return err
		}
	}

	if options.Chmod {
		if err := os.Chmod(cand, perm); err != nil {
			return err
		}
	}

	if err := os.Rename(cand, path); err != nil {
		return err
	}

	fd, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer fd.Close()

	return fd.Sync()
}
