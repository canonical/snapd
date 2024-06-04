// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package fde

import (
	"bytes"
	"fmt"
	"time"

	"github.com/snapcore/snapd/systemd"
)

// fdeInitramfsHelperRuntimeMax is the maximum runtime a helper can execute
// XXX: what is a reasonable default here?
var fdeInitramfsHelperRuntimeMax = 2 * time.Minute

func runFDEinitramfsHelper(name string, stdin []byte) (output []byte, err error) {
	command := []string{name}

	opts := &systemd.RunOptions{
		Properties: []string{
			"DefaultDependencies=no",
			"SystemCallFilter=~@mount",
			fmt.Sprintf("RuntimeMaxSec=%s", fdeInitramfsHelperRuntimeMax),
		},
		Stdin: bytes.NewReader(stdin),
	}

	sysd := systemd.New(systemd.SystemMode, nil)

	return sysd.Run(command, opts)
}
