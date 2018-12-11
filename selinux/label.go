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

	"github.com/snapcore/snapd/osutil"
)

var (
	// actual matchpathcon -V output:
	// /home/guest/snap has context unconfined_u:object_r:user_home_t:s0, should be unconfined_u:object_r:snappy_home_t:s0
	matchIncorrectLabel = regexp.MustCompile("^.* has context .* should be .*\n$")
)

// Verifypathcon checks whether a given path is labeled according to its default
// SELinux context
func Verifypathcon(aPath string) (bool, error) {
	if _, err := os.Stat(aPath); err != nil {
		// path that cannot be accessed cannot be verified
		return false, err
	}
	// if matchpathcon is found we may verify SELinux context
	matchpathconPath, err := exec.LookPath("matchpathcon")
	if err != nil {
		return false, err
	}
	// -V: verify
	cmd := exec.Command(matchpathconPath, "-V", aPath)
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err == nil {
		// the path was verified
		return true, nil
	}
	exit, _ := osutil.ExitCode(err)
	if exit == 1 && matchIncorrectLabel.Match(out) {
		return false, nil
	}
	return false, err
}

// Restorecon restores the default SELinux context of given path
func Restorecon(aPath string, recursive bool) error {
	if _, err := os.Stat(aPath); err != nil {
		// path that cannot be accessed cannot be restored
		return err
	}
	// if restorecon is found we may restore SELinux context
	restoreconPath, err := exec.LookPath("restorecon")
	if err != nil {
		return err
	}

	args := make([]string, 0, 2)
	if recursive {
		// -R: recursive
		args = append(args, "-R")
	}
	args = append(args, aPath)

	return exec.Command(restoreconPath, args...).Run()
}
