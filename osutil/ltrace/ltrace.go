// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
 * adapted from `strace` by sascha.dewald@gmail.com
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

package ltrace

import (
	"fmt"
	"os/exec"
	"os/user"
)

// Command returns how to run ltrace in the users context
func Command(extraLtraceOpts []string, traceCmd ...string) (*exec.Cmd, error) {
	current, err := user.Current()
	if err != nil {
		return nil, err
	}
	sudoPath, err := exec.LookPath("sudo")
	if err != nil {
		return nil, fmt.Errorf("cannot use ltrace without sudo: %s", err)
	}

	ltracePath, err := exec.LookPath("ltrace")
	if err != nil {
		return nil, fmt.Errorf("cannot find an installed ltrace")
	}

	args := []string{
		sudoPath,
		"-E",
		ltracePath,
		"-u", current.Username,
		"-f",
	}
	args = append(args, extraLtraceOpts...)
	args = append(args, traceCmd...)

	return &exec.Cmd{
		Path: sudoPath,
		Args: args,
	}, nil
}
