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
	"strings"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
)

// Enabled returns true if the classic mode is already enabled
func Enabled() bool {
	return osutil.FileExists(filepath.Join(dirs.ClassicDir, "etc", "apt", "sources.list"))
}

var mountpointCmd = "mountpoint"

// isMounted returns true if the given path is a mountpoint
func isMounted(path string) (bool, error) {
	cmd := exec.Command(mountpointCmd, path)
	// mountpoint uses translated messages, ensure we get the
	// english ones
	cmd.Env = []string{"LC_ALL=C"}

	output, err := cmd.CombinedOutput()
	exitCode, err := osutil.ExitCode(err)
	// if we get anything other than "ExitError" er error here
	if err != nil {
		return false, err
	}

	// mountpoint.c from util-linux always returns 0 or 1
	// (unless e.g. signal)
	if exitCode != 0 && exitCode != 1 {
		return false, fmt.Errorf("got unexpected exit code %v from the mountpoint command: %s", exitCode, output)
	}

	// exitCode == 0 means it is a mountpoint, do we are done
	if exitCode == 0 {
		return true, nil
	}

	// exitCode == 1 either means something went wrong *or*
	//               the path is not a mount point
	//               (thanks mountpoint.c :/)
	if strings.Contains(string(output), "is not a mountpoint") {
		return false, nil
	}

	return false, fmt.Errorf("unexpected output from mountpoint: %s (%v)", output, exitCode)
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

	// do the actual mount, we use "rbind" so that we get all submounts
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
