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

var mountpointCmd = "mountpoint"

// isMounted returns true if the given path is a mountpoint
func isMounted(path string) (bool, error) {
	cmd := exec.Command(mountpointCmd, path)
	output, err := cmd.CombinedOutput()
	exitCode, err := helpers.ExitCode(err)
	if err != nil {
		return false, err
	}
	// FIXME: the interface of "mountpoint" is not ideal, it will
	//        return "1" on error but also when the dir is not
	//        a mountpoint. there is also a string "%s is mountpoint"
	//        but that string is translated
	if exitCode != 0 && exitCode != 1 {
		return false, fmt.Errorf("got unexpected exit code %v from the mountpoint command: %s", exitCode, output)
	}

	// man-page: zero if the directory is a mountpoint, non-zero if not
	return exitCode == 0, nil
}

// bindmount will bind mount the src path into the dstPath of the
// ubuntu classic environment
func bindmount(src, dstPath, remountArg string) error {
	dst := filepath.Join(dirs.ClassicDir, dstPath)
	alreadyMounted, err := isMounted(dst)
	if err != nil {
		return err
	}
	if alreadyMounted {
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
