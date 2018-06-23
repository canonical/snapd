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

package squashfs

import (
	"os/exec"
	"strings"

	"github.com/snapcore/snapd/osutil"
)

// useFuse detects if we should be using squashfuse instead
var useFuse = useFuseImpl

func useFuseImpl() bool {
	if !osutil.FileExists("/dev/fuse") {
		return false
	}

	if !osutil.ExecutableExists("squashfuse") && !osutil.ExecutableExists("snapfuse") {
		return false
	}

	out, err := exec.Command("systemd-detect-virt", "--container").Output()
	if err != nil {
		return false
	}

	virt := strings.TrimSpace(string(out))
	if virt != "none" {
		return true
	}

	return false
}

// MockUseFuse is exported so useFuse can be overridden by testing.
func MockUseFuse(r bool) func() {
	oldUseFuse := useFuse
	useFuse = func() bool {
		return r
	}
	return func() { useFuse = oldUseFuse }
}

// FsType returns what fstype to use for squashfs mounts and what
// mount options
func FsType() (fstype string, options []string, err error) {
	fstype = "squashfs"
	options = []string{"ro", "x-gdu.hide"}

	if useFuse() {
		options = append(options, "allow_other")
		switch {
		case osutil.ExecutableExists("squashfuse"):
			fstype = "fuse.squashfuse"
		case osutil.ExecutableExists("snapfuse"):
			fstype = "fuse.snapfuse"
		default:
			panic("cannot happen because useFuse() ensures one of the two executables is there")
		}
	}

	return fstype, options, nil
}
