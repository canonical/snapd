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

package helpers

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/ubuntu-core/snappy/cp"
	"github.com/ubuntu-core/snappy/osutil"
)

// RSyncWithDelete syncs srcDir to destDir
func RSyncWithDelete(srcDirName, destDirName string) error {
	// first remove everything thats not in srcdir
	err := filepath.Walk(destDirName, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// relative to the root "destDirName"
		relPath := path[len(destDirName):]
		if !osutil.FileExists(filepath.Join(srcDirName, relPath)) {
			if err := os.RemoveAll(path); err != nil {
				return err
			}
			if info.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	if err != nil {
		return err
	}

	// then copy or update the data from srcdir to destdir
	err = filepath.Walk(srcDirName, func(src string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// relative to the root "srcDirName"
		relPath := src[len(srcDirName):]
		dst := filepath.Join(destDirName, relPath)
		if info.IsDir() {
			if err := os.MkdirAll(dst, info.Mode()); err != nil {
				return err
			}

			// this can panic. The alternative would be to use the "st, ok" pattern, and then if !ok... panic?
			st := info.Sys().(*syscall.Stat_t)
			ts := []syscall.Timespec{st.Atim, st.Mtim}

			return syscall.UtimesNano(dst, ts)
		}
		if !FilesAreEqual(src, dst) {
			// XXX: we should (eventually) use CopyFile here,
			//      but we need to teach it about preserving
			//      of atime/mtime and permissions
			output, err := exec.Command("cp", "-va", src, dst).CombinedOutput()
			if err != nil {
				return fmt.Errorf("Failed to copy %s to %s (%s)", src, dst, output)
			}
		}
		return nil
	})

	return err
}

// CopyIfDifferent copies src to dst only if dst is different that src
func CopyIfDifferent(src, dst string) error {
	if !FilesAreEqual(src, dst) {
		if err := cp.CopyFile(src, dst, cp.CopyFlagSync|cp.CopyFlagOverwrite); err != nil {
			return err
		}
	}

	return nil
}
