// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

	"github.com/snapcore/snapd/sandbox/gpio"
	"github.com/snapcore/snapd/snapdtool"
)

type options struct {
	CmdExportChardev   cmdExportChardev   `command:"export-chardev"`
	CmdUnexportChardev cmdUnexportChardev `command:"unexport-chardev"`
}

var gpioEnsureAggregatorDriver = gpio.EnsureAggregatorDriver

func run(args []string) error {
	// Make sure the gpio-aggregator module is loaded because the
	// systemd security backend comes before the kmod security
	// backend, there is an edge case on first connection where
	// the helper service could be started before the gpio-aggregator
	// module is loaded.
	if err := gpioEnsureAggregatorDriver(); err != nil {
		return nil
	}

	var opts options
	p := flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash)

	if _, err := p.ParseArgs(args); err != nil {
		return err
	}
	return nil
}

func main() {
	snapdtool.ExecInSnapdOrCoreSnap()

	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
