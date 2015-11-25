// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

	"github.com/ubuntu-core/snappy/logger"

	"github.com/jessevdk/go-flags"
)

type options struct {
	// No global options yet
}

var optionsData options

var parser = flags.NewParser(&optionsData, flags.HelpFlag|flags.PassDoubleDash)

func init() {
	err := logger.SimpleSetup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to activate logging: %s\n", err)
	}
}

func main() {
	if _, err := parser.Parse(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if _, ok := err.(*flags.Error); !ok {
			logger.Debugf("%v failed: %v", os.Args, err)
		}
		os.Exit(1)
	}
}
