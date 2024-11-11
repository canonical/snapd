// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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

package snapdtool

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/strutil"
)

// The SNAP_REEXEC environment variable controls whether the command
// will attempt to re-exec itself from inside an ubuntu-core snap
// present on the system. If not present in the environ it's assumed
// to be set to 1 (do re-exec); that is: set it to 0 to disable.
const reExecKey = "SNAP_REEXEC"

var (
	// snapdSnap is the place to look for the snapd snap; we will re-exec
	// here
	snapdSnap = "/snap/snapd/current"

	// coreSnap is the place to look for the core snap; we will re-exec
	// here if there is no snapd snap
	coreSnap = "/snap/core/current"

	// selfExe is the path to a symlink pointing to the current executable
	selfExe = "/proc/self/exe"

	syscallExec = syscall.Exec
	osReadlink  = os.Readlink
)

// DistroSupportsReExec returns true if the distribution we are running on can use re-exec.
//
// This is true by default except for a "core/all" snap system where it makes
// no sense and in certain distributions that we don't want to enable re-exec
// yet because of missing validation or other issues.
func DistroSupportsReExec() bool {
	if !release.OnClassic {
		return false
	}
	if !release.DistroLike("debian", "ubuntu") {
		logger.Debugf("re-exec not supported on distro %q yet", release.ReleaseInfo.ID)
		return false
	}
	return true
}

// systemSnapSupportsReExec returns true if the given core/snapd snap should be used as re-exec target.
//
// Ensure we do not use older version of snapd, look for info file and ignore
// version of core that do not yet have it.
func systemSnapSupportsReExec(coreOrSnapdPath string) bool {
	infoDir := filepath.Join(coreOrSnapdPath, filepath.Join(dirs.CoreLibExecDir))
	ver, _, err := SnapdVersionFromInfoFile(infoDir)
	if err != nil {
		logger.Noticef("%v", err)
		return false
	}

	// > 0 means our Version is bigger than the version of snapd in core
	res, err := strutil.VersionCompare(Version, ver)
	if err != nil {
		logger.Debugf("cannot version compare %q and %q: %v", Version, ver, err)
		return false
	}
	if res > 0 {
		logger.Debugf("snap (at %q) is older (%q) than distribution package (%q)", coreOrSnapdPath, ver, Version)
		return false
	}
	return true
}

// InternalToolPath returns the path of an internal snapd tool. The tool
// *must* be located inside the same tree as the current binary.
//
// The return value is either the path of the tool in the current distribution
// or in the core/snapd snap (or the ubuntu-core snap) if the current binary is
// ran from that location.
func InternalToolPath(tool string) (string, error) {
	distroTool := filepath.Join(dirs.DistroLibExecDir, tool)

	// find the internal path relative to the running snapd, this
	// ensure we don't rely on the state of the system (like
	// having a valid "current" symlink).
	exe, err := osReadlink("/proc/self/exe")
	if err != nil {
		return "", err
	}

	if !strings.HasPrefix(exe, dirs.DistroLibExecDir) {
		// either running from mounted location or /usr/bin/snap*

		// find the local prefix to the snap:
		// /snap/snapd/123/usr/bin/snap       -> /snap/snapd/123
		// /snap/core/234/usr/lib/snapd/snapd -> /snap/core/234
		idx := strings.LastIndex(exe, "/usr/")
		if idx > 0 {
			// only assume mounted location when path contains
			// /usr/, but does not start with one
			prefix := exe[:idx]
			maybeTool := filepath.Join(prefix, "/usr/lib/snapd", tool)
			if osutil.IsExecutable(maybeTool) {
				return maybeTool, nil
			}
		}
	}

	// fallback to distro tool
	return distroTool, nil
}

// IsReexecEnabled checks the environment and configuration to assert whether
// reexec has been explicitly enabled/disabled.
func IsReexecEnabled() bool {
	// XXX for now we are only checking environment variables

	// If we are asked not to re-execute use distribution packages. This is
	// "spiritual" re-exec so use the same environment variable to decide.
	return osutil.GetenvBool(reExecKey, true)
}

// IsReexecExplicitlyEnabled is a stronger check than IsReexecEnabled as it
// really expects the relevant environment variable to be set.
func IsReexecExplicitlyEnabled() bool {
	return os.Getenv(reExecKey) != "" && IsReexecEnabled()
}

// mustUnsetenv will unset the given environment key or panic if it
// cannot do that
func mustUnsetenv(key string) {
	if err := os.Unsetenv(key); err != nil {
		log.Panicf("cannot unset %s: %s", key, err)
	}
}

// ExecInSnapdOrCoreSnap makes sure you're executing the binary that ships in
// the snapd/core snap.
func ExecInSnapdOrCoreSnap() {
	// Which executable are we?
	rootDir, exe, err := exeAndRoot()
	if err != nil {
		logger.Noticef("cannot detect process exe location: %v", err)
		return
	}

	// Special case for snapd re-execing from 2.21. In this
	// version of snap/snapd we did set SNAP_REEXEC=0 when we
	// re-execed. In this case we need to unset the reExecKey to
	// ensure that subsequent run of snap/snapd (e.g. when using
	// classic confinement) will *not* prevented from re-execing.
	if strings.HasPrefix(rootDir, dirs.SnapMountDir) && !osutil.GetenvBool(reExecKey, true) {
		mustUnsetenv(reExecKey)
		return
	}

	if !IsReexecEnabled() {
		logger.Debugf("re-exec disabled by user")
		return
	}

	// Did we already re-exec?
	if strings.HasPrefix(rootDir, dirs.SnapMountDir) {
		return
	}

	// If the distribution doesn't support re-exec or run-from-core then don't do it.
	if !DistroSupportsReExec() {
		if IsReexecExplicitlyEnabled() {
			logger.Debugf("reexec explicitly enabled through environment")
		} else {
			return
		}
	}

	// TODO pay attention to libexecdir when enabling reexec on non-Ubuntu
	// with /usr/libexec/

	// Is this executable in the core snap too?
	coreOrSnapdPath := snapdSnap
	full := filepath.Join(snapdSnap, exe)
	if !osutil.FileExists(full) {
		coreOrSnapdPath = coreSnap
		full = filepath.Join(coreSnap, exe)
		if !osutil.FileExists(full) {
			return
		}
	}

	// If the core snap doesn't support re-exec or run-from-core then don't do it.
	if !systemSnapSupportsReExec(coreOrSnapdPath) {
		return
	}

	logger.Debugf("restarting into %q", full)
	panic(syscallExec(full, os.Args, os.Environ()))
}

// IsReexecd returns true when the current process binary is running from a snap.
func IsReexecd() (bool, error) {
	rootDir, _, err := exeAndRoot()
	if err != nil {
		return false, err
	}
	return strings.HasPrefix(rootDir, dirs.SnapMountDir), nil
}

// MockOsReadlink is for use in tests
func MockOsReadlink(f func(string) (string, error)) func() {
	realOsReadlink := osReadlink
	osReadlink = f
	return func() {
		osReadlink = realOsReadlink
	}
}

// exeAndRoot determines the current executable path and the root directory
// which can either the the global rootfs (/) or the snap mount directory if the
// process is executing from a snap. The returned executable path is relative to
// the root.
func exeAndRoot() (rootDir, exePath string, err error) {
	// TODO this is unlikely change for the current process at runtime,
	// consider memoizing the result
	exe, err := osReadlink(selfExe)
	if err != nil {
		return "", "", err
	}

	_, rest, found := strings.Cut(exe, dirs.SnapMountDir+string(filepath.Separator))
	if !found {
		rel, err := filepath.Rel(dirs.GlobalRootDir, exe)
		if err != nil {
			return "", "", err
		}
		return dirs.GlobalRootDir, rel, nil
	}

	snapName, rest, foundName := strings.Cut(rest, string(filepath.Separator))
	snapRev, exePath, foundRev := strings.Cut(rest, string(filepath.Separator))
	if !foundName || !foundRev {
		return "", "", fmt.Errorf("cannot parse snap tool path %q", exe)
	}

	return filepath.Join(dirs.SnapMountDir, snapName, snapRev), exePath, nil
}
