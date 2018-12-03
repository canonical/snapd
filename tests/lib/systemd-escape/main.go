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
	"os"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/systemd"
)

var opts struct {
	Path bool `short:"p" long:"path" description:"When escaping/unescaping assume the string is a path"`
}

func main() {
	args, err := flags.ParseArgs(&opts, os.Args)
	if err != nil {
		os.Exit(1)
	}

	if !opts.Path {
		panic("cannot use this systemd-escape without --path")
	}

	for i, arg := range args[1:] {
		fmt.Print(systemd.EscapeUnitNamePath(arg))
		if i < len(args[1:])-1 {
			fmt.Printf(" ")
		}
	}
	fmt.Printf("\n")
}
