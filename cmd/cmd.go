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
	"fmt"
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
	if !strings.HasSuffix(exe, "/snapd") && !strings.HasSuffix(exe, ".test") {
		panic(fmt.Sprintf("InternalToolPath can only be used from snapd, got: %s", exe))
	}

	if !strings.HasPrefix(exe, dirs.SnapMountDir) {
		logger.Noticef("exe doesn't have snap mount dir prefix: %q vs %q", exe, dirs.SnapMountDir)
		return distroTool
	}

	// if we are re-execed, then the tool is at the same location
	// as snapd
	return filepath.Join(filepath.Dir(exe), tool)
}

// ExecInCoreSnap makes sure you're executing the binary that ships in
// the core snap.
func ExecInCoreSnap() {
	// Which executable are we?
	thisExe, err := os.Readlink(selfExe)
	if err != nil {
		logger.Noticef("cannot read self exec symlink (%q): %s\n", selfExe, err)
		return
	}

	// What is the path of the tool we wanted to run?
	didReExec := false
	tool := thisExe
	if strings.HasPrefix(tool, dirs.SnapMountDir) {
		// Chop off the snap mount directory
		tool = tool[len(dirs.SnapMountDir):]
		// Chop off the base snap name and revision.
		// Three is the number of slashees (/) in /$BASE/$REVISION/
		tool = "/" + strings.Join(strings.Split(tool, "/")[3:], "/")
		didReExec = true
	}

	// If we are re-executing from an old snapd (like 2.21-old which is forever
	// present in Debian 9) then we the meaning of the SNAP_REEXEC variable is
	// different and we may need to correct its value.
	// In old versions of snapd SNAP_REEXEC is indicating the "we did reexec"
	// flag instead of the "we want to reexec" preference (in subsequent
	// versions of snapd those became SNAP_DID_REEXEC and SNAP_REEXEC
	// respectively). Thus, if we see the current process is coming from a core
	// snap (so it re-executed already) *and* we see that SNAP_REEXEC=0 is set
	// then we need to just unset this variable.
	if didReExec && os.Getenv(reExecKey) == "0" {
		if err := os.Unsetenv(reExecKey); err != nil {
			logger.Panicf("cannot unset %s: %s", reExecKey, err)
		}
	}

	// If the distribution doesn't support re-exec or run-from-core then don't do it.
	if !distroSupportsReExec() {
		return
	}

	// What is the path of the tool in the core snap?
	corePath := newCore
	coreTool := filepath.Join(newCore, tool)
	if !osutil.FileExists(coreTool) {
		logger.Debugf("tool not present in the new core snap %q", coreTool)
		corePath = oldCore
		coreTool = filepath.Join(oldCore, tool)
		if !osutil.FileExists(coreTool) {
			logger.Debugf("tool not present in the old core snap %q", coreTool)
			return
		}
	}

	// If the core snap doesn't support re-exec or run-from-core then don't do it.
	if !coreSupportsReExec(corePath) {
		// This logs any potential reason so let's not repeat ourselves.
		return
	}

	// If we are asked not to re-execute use distribution packages. This is
	// "spiritual" re-exec so use the same environment variable to decide.
	if !osutil.GetenvBool(reExecKey, true) {
		logger.Debugf("re-exec disabled by user")
		return
	}

	// Did we already re-exec this exact program?
	coreToolDeref, err := filepath.EvalSymlinks(coreTool)
	if err != nil {
		logger.Noticef("cannot evaluate symlinks in %q: %s", coreTool, err)
		return
	}
	if coreToolDeref == thisExe {
		logger.Debugf("we already are running the tool we wanted to re-execute to")
		return
	}

	logger.Debugf("restarting from %q into %q", thisExe, coreToolDeref)
	panic(syscallExec(coreToolDeref, os.Args, os.Environ()))
}
