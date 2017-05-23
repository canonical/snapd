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
	"io"
	"os"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/release"
)

var (
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr

	opts   struct{}
	parser *flags.Parser = flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)

	shortHelp = i18n.G("Repair an Ubuntu Core system")
	longHelp  = i18n.G(`
snap-repair is a tool to fetch and run repair-assertions
which are used to do emergency repairs on Ubuntu Core systems.
`)

	errOnClassic = fmt.Errorf("cannot use snap-repair on a classic system")
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(Stderr, "cannot snap-repair: %s\n", err)
		if err != errOnClassic {
			os.Exit(1)
		}
	}
}

func run() error {
	if release.OnClassic {
		return errOnClassic
	}

	if err := parseArgs(os.Args[1:]); err != nil {
		return err
	}

	return nil
}

func parseArgs(args []string) error {
	parser.ShortDescription = shortHelp
	parser.LongDescription = longHelp

	_, err := parser.ParseArgs(args)
	return err
}
