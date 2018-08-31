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

package cmd

import (
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"regexp"
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

// distroSupportsReExec returns true if the distribution we are running on can use re-exec.
//
// This is true by default except for a "core/all" snap system where it makes
// no sense and in certain distributions that we don't want to enable re-exec
// yet because of missing validation or other issues.
func distroSupportsReExec() bool {
	if !release.OnClassic {
		return false
	}
	if !release.DistroLike("debian", "ubuntu") {
		logger.Debugf("re-exec not supported on distro %q yet", release.ReleaseInfo.ID)
		return false
	}
	return true
}

// coreSupportsReExec returns true if the given core snap should be used as re-exec target.
//
// Ensure we do not use older version of snapd, look for info file and ignore
// version of core that do not yet have it.
func coreSupportsReExec(corePath string) bool {
	fullInfo := filepath.Join(corePath, filepath.Join(dirs.CoreLibExecDir, "info"))
	if !osutil.FileExists(fullInfo) {
		return false
	}
	content, err := ioutil.ReadFile(fullInfo)
	if err != nil {
		logger.Noticef("cannot read snapd info file %q: %s", fullInfo, err)
		return false
	}
	ver := regexp.MustCompile("(?m)^VERSION=(.*)$").FindStringSubmatch(string(content))
	if len(ver) != 2 {
		logger.Noticef("cannot find snapd version information in %q", content)
		return false
	}
	// > 0 means our Version is bigger than the version of snapd in core
	res, err := strutil.VersionCompare(Version, ver[1])
	if err != nil {
		logger.Debugf("cannot version compare %q and %q: %s", Version, ver[1], res)
		return false
	}
	if res > 0 {
		logger.Debugf("core snap (at %q) is older (%q) than distribution package (%q)", corePath, ver[1], Version)
		return false
	}
	return true
}

// InternalToolPath returns the path of an internal snapd tool. The tool
// *must* be located inside /usr/lib/snapd/.
//
// The return value is either the path of the tool in the current distribution
// or in the core snap (or the ubuntu-core snap). This handles spiritual
// "re-exec" where we run the tool from the core snap if the environment allows
// us to do so.
func InternalToolPath(tool string) string {
	distroTool := filepath.Join(dirs.DistroLibExecDir, tool)

	// find the internal path relative to the running snapd, this
	// ensure we don't rely on the state of the system (like
	// having a valid "current" symlink).
	exe, err := osReadlink("/proc/self/exe")
	if err != nil {
		logger.Noticef("cannot read /proc/self/exe: %v, using tool outside core", err)
		return distroTool
	}

	// ensure we never use this helper from anything but
	if !strings.HasSuffix(exe, "/snapd") && !strings.HasSuffix(exe, ".test") {
		log.Panicf("InternalToolPath can only be used from snapd, got: %s", exe)
	}

	if !strings.HasPrefix(exe, dirs.SnapMountDir) {
		logger.Debugf("exe doesn't have snap mount dir prefix: %q vs %q", exe, dirs.SnapMountDir)
		return distroTool
	}

	// if we are re-execed, then the tool is at the same location
	// as snapd
	return filepath.Join(filepath.Dir(exe), tool)
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
	exe, err := os.Readlink(selfExe)
	if err != nil {
		logger.Noticef("cannot read /proc/self/exe: %v", err)
		return
	}

	// Special case for snapd re-execing from 2.21. In this
	// version of snap/snapd we did set SNAP_REEXEC=0 when we
	// re-execed. In this case we need to unset the reExecKey to
	// ensure that subsequent run of snap/snapd (e.g. when using
	// classic confinement) will *not* prevented from re-execing.
	if strings.HasPrefix(exe, dirs.SnapMountDir) && !osutil.GetenvBool(reExecKey, true) {
		mustUnsetenv(reExecKey)
		return
	}

	// If we are asked not to re-execute use distribution packages. This is
	// "spiritual" re-exec so use the same environment variable to decide.
	if !osutil.GetenvBool(reExecKey, true) {
		logger.Debugf("re-exec disabled by user")
		return
	}

	// Did we already re-exec?
	if strings.HasPrefix(exe, dirs.SnapMountDir) {
		return
	}

	// If the distribution doesn't support re-exec or run-from-core then don't do it.
	if !distroSupportsReExec() {
		return
	}

	// Is this executable in the core snap too?
	corePath := snapdSnap
	full := filepath.Join(snapdSnap, exe)
	if !osutil.FileExists(full) {
		corePath = coreSnap
		full = filepath.Join(coreSnap, exe)
		if !osutil.FileExists(full) {
			return
		}
	}

	// If the core snap doesn't support re-exec or run-from-core then don't do it.
	if !coreSupportsReExec(corePath) {
		return
	}

	logger.Debugf("restarting into %q", full)
	panic(syscallExec(full, os.Args, os.Environ()))
}
