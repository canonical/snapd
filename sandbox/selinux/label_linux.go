// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package selinux

import (
	"os"
	"os/exec"
	"regexp"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/osutil"
)

// actual matchpathcon -V output:
// /home/guest/snap has context unconfined_u:object_r:user_home_t:s0, should be unconfined_u:object_r:snappy_home_t:s0
var matchIncorrectLabel = regexp.MustCompile("^.* has context .* should be .*\n$")

// VerifyPathContext checks whether a given path is labeled according to its default
// SELinux context
func VerifyPathContext(aPath string) (bool, error) {
	mylog.Check2(os.Stat(aPath))
	// path that cannot be accessed cannot be verified

	// matchpathcon -V verifies whether the context of a path matches the
	// default
	cmd := exec.Command("matchpathcon", "-V", aPath)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out := mylog.Check2(cmd.Output())
	if err == nil {
		// the path was verified
		return true, nil
	}
	exit, _ := osutil.ExitCode(err)
	// exits with 1 when the verification failed or other error occurred,
	// when verification failed a message like this will be printed to
	// stdout:
	//   <the-path> has context <some-context>, should be <some-other-context>
	// match the message so that we can distinguish a failed verification
	// case from other errors
	if exit == 1 && matchIncorrectLabel.Match(out) {
		return false, nil
	}
	return false, err
}

// RestoreContext restores the default SELinux context of given path
func RestoreContext(aPath string, mode RestoreMode) error {
	mylog.Check2(os.Stat(aPath))
	// path that cannot be accessed cannot be restored

	args := make([]string, 0, 2)
	if mode.Recursive {
		// -R: recursive
		args = append(args, "-R")
	}
	args = append(args, aPath)

	return exec.Command("restorecon", args...).Run()
}

// SnapMountContext finds out the right context for mounting snaps
func SnapMountContext() string {
	// TODO: consider reading this from an external configuration file, such
	// as per app contexts, from
	// /etc/selinux/targeted/contexts/snapd_contexts like go-selinux and
	// podman do for container volumes.
	return "system_u:object_r:snappy_snap_t:s0"
}
