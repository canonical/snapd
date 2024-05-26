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

package main

import (
	"fmt"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"
)

type cmdConnectivityCheck struct {
	clientMixin
}

func init() {
	addDebugCommand("connectivity",
		"Check network connectivity status",
		"The connectivity command checks the network connectivity of snapd.",
		func() flags.Commander {
			return &cmdConnectivityCheck{}
		}, nil, nil)
}

func (x *cmdConnectivityCheck) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	var status struct {
		Unreachable []string
	}
	mylog.Check(x.client.DebugGet("connectivity", &status, nil))

	fmt.Fprintf(Stdout, "Connectivity status:\n")
	if len(status.Unreachable) == 0 {
		fmt.Fprintf(Stdout, " * PASS\n")
		return nil
	}

	for _, uri := range status.Unreachable {
		fmt.Fprintf(Stdout, " * %s: unreachable\n", uri)
	}
	return fmt.Errorf("%v servers unreachable", len(status.Unreachable))
}
