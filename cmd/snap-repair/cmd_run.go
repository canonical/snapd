// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
)

func init() {
	const (
		short = "Fetch and run repair assertions as necessary for the device"
		long  = ""
	)

	if _, err := parser.AddCommand("run", short, long, &cmdRun{}); err != nil {
		panic(err)
	}

}

type cmdRun struct{}

func (c *cmdRun) Execute(args []string) error {
	fmt.Fprintf(Stdout, "run is not implemented yet\n")
	return nil
}
