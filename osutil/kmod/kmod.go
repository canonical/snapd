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

package kmod

import (
	"fmt"
	"os/exec"

	"github.com/snapcore/snapd/osutil"
)

var modprobeCommand = func(args ...string) error {
	allArgs := append([]string{"--syslog"}, args...)
	err := exec.Command("modprobe", allArgs...).Run()
	if err != nil {
		exitCode, err := osutil.ExitCode(err)
		if err != nil {
			return err
		}
		return fmt.Errorf("modprobe failed with exit status %d (see syslog for details)", exitCode)
	}
	return nil
}

func LoadModule(module string, options []string) error {
	return modprobeCommand(append([]string{module}, options...)...)
}

func UnloadModule(module string) error {
	return modprobeCommand("-r", module)
}
