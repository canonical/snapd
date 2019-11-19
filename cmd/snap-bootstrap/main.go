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
	"os"

	"github.com/jessevdk/go-flags"
	"github.com/snapcore/snapd/osutil"
)

var (
	shortHelp = "Bootstrap a Ubuntu Core system"
	longHelp  = `
snap-bootstrap is a tool to bootstrap Ubuntu Core from ephemeral systems
such as initramfs.
`

	opts   struct{}
	parser *flags.Parser = flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
)

func main() {
	err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %s\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if !osutil.GetenvBool("SNAPPY_TESTING") {
		return fmt.Errorf("cannot use outside of tests yet")
	}
	if os.Getuid() != 0 {
		return fmt.Errorf("please run as root")
	}

	return parseArgs(args)
}

func parseArgs(args []string) error {
	parser.ShortDescription = shortHelp
	parser.LongDescription = longHelp

	_, err := parser.ParseArgs(args)
	return err
}
