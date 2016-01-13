// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package partition

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// This is a var instead of a function to making mocking in the tests easier
var runCommandWithStdout = runCommandWithStdoutImpl

// Run command specified by args and return the output
func runCommandWithStdoutImpl(args ...string) (string, error) {
	if len(args) == 0 {

		return "", errors.New("no command specified")
	}

	output, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		cmdline := strings.Join(args, " ")
		return "", fmt.Errorf("Failed to run command %q: %q (%s)",
			cmdline, output, err)
	}

	return string(output), err
}
