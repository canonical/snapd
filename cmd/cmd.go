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
	// newCore is the place to look for the core snap; everything in this
	// location will be new enough to re-exec into.
	newCore = "/snap/core/current"

	// oldCore is the previous location of the core snap. Only things
	// newer than minOldRevno will be ok to re-exec into.
	oldCore = "/snap/ubuntu-core/current"

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
	switch release.ReleaseInfo.ID {
	case "fedora", "centos", "rhel", "opensuse", "suse", "poky":
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
	if !strings.HasSuffix(exe, "/snapd") {
		panic("InternalToolPath can only be used from snapd")
	}

	if !strings.HasPrefix(exe, dirs.SnapMountDir) {
		return distroTool
	}

	// if we are re-execed, then the tool is at the same location
	// as snapd
	return filepath.Join(filepath.Dir(exe), tool)
}

// ExecInCoreSnap makes sure you're executing the binary that ships in
// the core snap.
func ExecInCoreSnap() {
	// If we are asked not to re-execute use distribution packages. This is
	// "spiritual" re-exec so use the same environment variable to decide.
	if !osutil.GetenvBool(reExecKey, true) {
		logger.Debugf("re-exec disabled by user")
		return
	}

	// Did we already re-exec?
	if osutil.GetenvBool("SNAP_DID_REEXEC") {
		return
	}

	// If the distribution doesn't support re-exec or run-from-core then don't do it.
	if !distroSupportsReExec() {
		return
	}

	// Which executable are we?
	exe, err := os.Readlink(selfExe)
	if err != nil {
		return
	}

	// Is this executable in the core snap too?
	corePath := newCore
	full := filepath.Join(newCore, exe)
	if !osutil.FileExists(full) {
		corePath = oldCore
		full = filepath.Join(oldCore, exe)
		if !osutil.FileExists(full) {
			return
		}
	}

	// If the core snap doesn't support re-exec or run-from-core then don't do it.
	if !coreSupportsReExec(corePath) {
		return
	}

	logger.Debugf("restarting into %q", full)
	env := append(os.Environ(), "SNAP_DID_REEXEC=1")
	panic(syscallExec(full, os.Args, env))
}
