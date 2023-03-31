// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/snapdtool"
)

func init() {
	err := logger.SimpleSetup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to activate logging: %v\n", err)
	}
}

// selfExe is the path to a symlink pointing to the current executable
var selfExe = "/proc/self/exe"

func main() {
	snapdtool.ExecInSnapdOrCoreSnap()
	// This point is only reached if reexec did not happen
	exe, err := os.Readlink(selfExe)
	if err != nil {
		logger.Noticef("cannot read /proc/self/exe: %v", err)
		return
	}
	pyHelper := "snapd-aa-prompt-ui-gtk"
	cmd := exec.Command(filepath.Dir(exe), pyHelper)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		logger.Noticef("cannot run %v: %v", cmd, err)
		return
	}
}
