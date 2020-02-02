// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package wrappers

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"syscall"
)

func AddSnapManpages(s *snap.Info) error {
	snapManRoot := filepath.Join(s.MountDir(), "meta", "man")
	inSnapPrefix := s.SnapName() + "."
	onDiskPrefix := s.InstanceName() + "."
	return filepath.Walk(snapManRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.Mode().IsRegular() {
			// TODO: support in-snap symlinks
			return nil
		}
		dir, base := filepath.Split(path[len(snapManRoot):])
		if !strings.HasPrefix(base, inSnapPrefix) {
			return nil
		}
		targetDir := filepath.Join(dirs.SnapManpagesDir, dir)
		if err := os.MkdirAll(targetDir, 0755); err != nil {
			return err
		}
		target := filepath.Join(targetDir, onDiskPrefix+base[len(inSnapPrefix):])
		err = os.Symlink(path, target)
		if os.IsExist(err) {
			// can happen during a Retry of LinkSnap
			err = os.Remove(target)
			if err == nil {
				err = os.Symlink(path, target)
			}
		}
		return err
	})
}

// removeUp removes the file at the given path and all directories above it
// until "stop" is reached, bailing at first error. "stop" is not removed.
//
// if the error is because a directory being removed is not empty, return nil,
// otherwise return the error
//
// TODO: move to osutil? wrap error in os.PathError?
// XXX: TEST ME MORE
func removeUp(path, stop string) error {
	err := syscall.Unlink(path)
	for err == nil {
		path = filepath.Dir(path)
		if path == stop {
			break
		}
		err = syscall.Rmdir(path)
	}
	if err == syscall.ENOTEMPTY {
		return nil
	}
	return err
}

func RemoveSnapManpages(s *snap.Info) error {
	pfx := s.MountDir()
	return filepath.Walk(dirs.SnapManpagesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			// ignore walk errors
			return nil
		}
		str, err := os.Readlink(path)
		if err != nil {
			return nil
		}
		if strings.HasPrefix(str, pfx) {
			return removeUp(path, dirs.SnapManpagesDir)
		}
		return nil
	})
}
