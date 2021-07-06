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

type cmdGetStacktraces struct {
	clientMixin
}

func init() {
	addDebugCommand("stacktraces",
		"Obtain stacktraces of all snapd goroutines",
		"Obtain stacktraces of all snapd goroutines.",
		func() flags.Commander {
			return &cmdGetStacktraces{}
		}, nil, nil)
}

func (x *cmdGetStacktraces) Execute(args []string) error {
	var stacktraces string
	if err := x.client.Debug("stacktraces", nil, &stacktraces); err != nil {
		return err
	}
	fmt.Fprintf(Stdout, stacktraces)
	return nil
}
