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

	"github.com/jessevdk/go-flags"
)

type cmdConnectivityCheck struct{}

func init() {
	addDebugCommand("connectivity",
		"Check network connectivity status",
		"The connectivity-check command checks the network connectivity of snapd .",
		func() flags.Commander {
			return &cmdConnectivityCheck{}
		}, nil, nil)
}

func (x *cmdConnectivityCheck) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	cli := Client()

	var connectivity map[string]bool
	if err := cli.Debug("connectivity", nil, &connectivity); err != nil {
		return err
	}

	fmt.Fprintf(Stdout, "Connectivity status:\n")
	unreachable := 0
	for uri, reachable := range connectivity {
		if !reachable {
			fmt.Fprintf(Stdout, " * %s: unreachable\n", uri)
			unreachable++
		}
	}
	if unreachable > 0 {
		return fmt.Errorf("cannot connect to %v of %v servers", unreachable, len(connectivity))
	} else {
		fmt.Fprintf(Stdout, " * PASS\n")
	}

	return nil
}
