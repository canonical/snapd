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

	"github.com/snapcore/snapd/i18n"
)

func init() {
	short := i18n.G("Fetch new repair-assertions")
	long := i18n.G("")

	if _, err := parser.AddCommand("fetch", short, long, &cmdFetch{}); err != nil {
		panic(err)
	}
}

type cmdFetch struct{}

func (c *cmdFetch) Execute(args []string) error {
	fmt.Fprintln(Stdout, "fetch is not implemented yet")
	return nil
}
