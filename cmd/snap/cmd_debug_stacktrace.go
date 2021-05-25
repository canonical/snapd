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

package main

import (
	"fmt"

	"github.com/jessevdk/go-flags"
)

type cmdGetStacktrace struct {
	clientMixin
}

func init() {
	addDebugCommand("stacktrace",
		"obtain stacktrace of all snapd goroutines",
		"obtain stacktrace of all snapd goroutines",
		func() flags.Commander {
			return &cmdGetStacktrace{}
		}, nil, nil)
}

func printStacktrace(stacktrace []string) {
	for _, line := range stacktrace {
		fmt.Fprintf(Stdout, "%s\n", line)
	}
}

func (x *cmdGetStacktrace) Execute(args []string) error {
	var stacktrace []string
	if err := x.client.DebugGetStacktrace(&stacktrace); err != nil {
		return err
	}
	printStacktrace(stacktrace)
	return nil
}
