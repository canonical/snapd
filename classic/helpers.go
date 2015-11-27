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

package classic

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/helpers"
)

// Enabled returns true if the classic mode is already enabled
func Enabled() bool {
	return helpers.FileExists(filepath.Join(dirs.ClassicDir, "etc", "apt", "sources.list"))
}

// isMounted returns true if the given path is a mountpoint
func isMounted(path string) bool {
	err := exec.Command("mountpoint", path).Run()
	// man-page: zero if the directory is a mountpoint, non-zero if not
	return err == nil
}

// bindmount will bind mount the src path into the dstPath of the
// ubuntu classic environment
func bindmount(src, dstPath, remountArg string) error {
	dst := filepath.Join(dirs.ClassicDir, dstPath)
	if isMounted(dst) {
		return nil
	}

	// see if we need to create the dir in dstPath
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if st.IsDir() && (st.Mode()&os.ModeSymlink == 0) {
		if err := os.MkdirAll(dst, st.Mode().Perm()); err != nil {
			return err
		}
	}

	// do the actual mount
	cmd := exec.Command("mount", "--make-rprivate", "--rbind", "-o", "rbind", src, dst)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("bind mounting %s to %s failed: %s (%s)", src, dst, err, output)
	}

	// remount as needed (ro)
	if remountArg != "" {
		cmd := exec.Command("mount", "--rbind", "-o", "remount,"+remountArg, src, dst)
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("remount %s to %s failed: %s (%s)", src, dst, err, output)
		}
	}

	return nil
}

func umount(path string) error {
	if output, err := exec.Command("umount", path).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to umount %s: %s (%s)", path, err, output)
	}

	return nil
}
