// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"syscall"
)

// MkdirAllChown is like os.MkdirAll but it calls os.Chown on any
// directories it creates.
func MkdirAllChown(path string, perm os.FileMode, uid, gid int) error {
	if s, err := os.Stat(path); err == nil {
		if s.IsDir() {
			return nil
		}

		// emulate os.MkdirAll
		return &os.PathError{
			Op:   "mkdir",
			Path: path,
			Err:  syscall.ENOTDIR,
		}
	}

	dir := filepath.Dir(path)
	if dir != "/" {
		if err := MkdirAllChown(dir, perm, uid, gid); err != nil {
			return err
		}
	}

	cand := path + ".mkdir-new"

	if err := os.Mkdir(cand, perm); err != nil && !os.IsExist(err) {
		return err
	}

	if err := os.Chown(cand, uid, gid); err != nil {
		return err
	}

	if err := os.Rename(cand, path); err != nil {
		return err
	}

	fd, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer fd.Close()

	return fd.Sync()
}
