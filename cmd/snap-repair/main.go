// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017-2020 Canonical Ltd
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

	// TODO: consider not using go-flags at all
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/snapdtool"
)

var (
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr

	opts   struct{}
	parser *flags.Parser = flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
)

const (
	shortHelp = "Repair an Ubuntu Core system"
	longHelp  = `
snap-repair is a tool to fetch and run repair assertions
which are used to do emergency repairs on the device.
`
)

func init() {
	err := logger.SimpleSetup(nil)
	if err != nil {
		fmt.Fprintf(Stderr, "WARNING: failed to activate logging: %v\n", err)
	}
}

var errOnClassic = fmt.Errorf("cannot use snap-repair on a classic system")

func main() {
	// TODO setup FIPS if needed?

	if err := run(); err != nil {
		fmt.Fprintf(Stderr, "error: %v\n", err)
		if err != errOnClassic {
			os.Exit(1)
		}
	}
}

var osGetuid = os.Getuid

func run() error {
	if release.OnClassic {
		return errOnClassic
	}
	if osGetuid() != 0 {
		return fmt.Errorf("must be run as root")
	}
	snapdenv.SetUserAgentFromVersion(snapdtool.Version, nil, "snap-repair")

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
