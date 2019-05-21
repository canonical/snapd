// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
		short = "Verify snaps against the  assertions in a directory"
		long  = ""
	)

	if _, err := parser.AddCommand("snaps", short, long, &cmdSnaps{}); err != nil {
		panic(err)
	}
}

type cmdSnaps struct {
	Positional struct {
		Assertsdir string   `required:"true"`
		Snaps      []string `required:"true"`
	} `positional-args:"yes"`
}

func (c *cmdSnaps) Execute(args []string) error {
	fmt.Printf("NOT using assertdir %s\n", c.Positional.Assertsdir)

	for _, snapFile := range c.Positional.Snaps {
		fmt.Printf("NOT validating %s\n", snapFile)
	}

	return nil
}
