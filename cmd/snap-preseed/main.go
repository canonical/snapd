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
	"io"
	"os"

	"github.com/jessevdk/go-flags"
)

const (
	shortHelp = "Prerun the first boot seeding of snaps in a chroot with a snapd seed."
	longHelp  = `
	Prerun the first boot seeding of snaps in an Ubuntu-based chroot directory
	with a snapd seed (/var/lib/snapd/seed directory set up for seeding).
`
)

var (
	osGetuid           = os.Getuid
	Stdout   io.Writer = os.Stdout
	Stderr   io.Writer = os.Stderr

	opts struct{}
)

func Parser() *flags.Parser {
	parser := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
	parser.ShortDescription = shortHelp
	parser.LongDescription = longHelp
	return parser
}

func main() {
	parser := Parser()
	if err := run(parser, os.Args[1:]); err != nil {
		fmt.Fprintf(Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run(parser *flags.Parser, args []string) error {
	if osGetuid() != 0 {
		return fmt.Errorf("must be run as root")
	}

	rest, err := parser.ParseArgs(args)
	if err != nil {
		return err
	}

	if len(rest) == 0 {
		return fmt.Errorf("need the chroot path as argument")
	}

	chrootDir := rest[0]
	if err := checkChroot(chrootDir); err != nil {
		return err
	}

	cleanup, err := prepareChroot(chrootDir)
	if err != nil {
		return err
	}

	// executing inside the chroot
	err = runPreseedMode(chrootDir)
	cleanup()
	return err
}
