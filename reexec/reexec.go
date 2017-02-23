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

package reexec

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"regexp"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/strutil"
)

// The SNAP_REEXEC environment variable controls whether the command
// will attempt to re-exec itself from inside an ubuntu-core snap
// present on the system. If not present in the environ it's assumed
// to be set to 1 (do re-exec); that is: set it to 0 to disable.
const key = "SNAP_REEXEC"

// newCore is the place to look for the core snap; everything in this
// location will be new enough to re-exec into.
const newCore = "/snap/core/current"

// oldCore is the previous location of the core snap. Only things
// newer than minOldRevno will be ok to re-exec into.
const oldCore = "/snap/ubuntu-core/current"

// Path returns the full path for a re-exec or "" if no reexec should
// be performed.
func Path(Version string) string {
	if !release.OnClassic {
		// you're already the real deal, natch
		return ""
	}

	// should we re-exec? no option in the environment means yes
	if !osutil.GetenvBool(key, true) {
		logger.Debugf("re-exec disabled by user")
		return ""
	}

	// can we re-exec? some distributions will need extra work before re-exec really works.
	switch release.ReleaseInfo.ID {
	case "fedora", "centos", "rhel":
		logger.Debugf("re-exec not supported on distro %q yet", release.ReleaseInfo.ID)
		return ""
	}

	exe, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return ""
	}

	corePath := newCore
	full := filepath.Join(newCore, exe)
	if !osutil.FileExists(full) {
		corePath = oldCore
		full = filepath.Join(oldCore, exe)
		if !osutil.FileExists(full) {
			return ""
		}
	}

	// ensure we do not re-exec into an older version of snapd, look
	// for info file and ignore version of core that do not yet have
	// it
	fullInfo := filepath.Join(corePath, "/usr/lib/snapd/info")
	if !osutil.FileExists(fullInfo) {
		logger.Debugf("not restarting into %q (no version info): older than %q (%s)", full, exe, Version)
		return ""
	}
	content, err := ioutil.ReadFile(fullInfo)
	if err != nil {
		logger.Noticef("cannot read info file %q: %s", fullInfo, err)
		return ""
	}
	ver := regexp.MustCompile("(?m)^VERSION=(.*)$").FindStringSubmatch(string(content))
	if len(ver) != 2 {
		logger.Noticef("cannot find version information in %q", content)
	}
	// > 0 means our Version is bigger than the version of snapd in core
	res, err := strutil.VersionCompare(Version, ver[1])
	if err != nil {
		logger.Debugf("cannot version compare %q and %q: %s", Version, ver[1], res)
		return ""
	}
	if res > 0 {
		logger.Debugf("not restarting into %q (%s): older than %q (%s)", full, ver, exe, Version)
		return ""
	}

	return full
}
