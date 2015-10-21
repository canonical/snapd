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
	"errors"
	"fmt"
	"os"

	"launchpad.net/snappy/logger"
	"launchpad.net/snappy/snappy"
	//"launchpad.net/snappy/priv"

	"github.com/jessevdk/go-flags"
)

var (
	// ErrNotEnoughArgs is returned when absolute path to package.yaml is
	// not specified
	ErrNotEnoughArgs = errors.New("must supply path to package.yaml")

	// ErrPackageYamlNotFound is returned when the absolute path to
	// package.yaml does not exist
	ErrPackageYamlNotFound = errors.New("must supply path to package.yaml")
)

type options struct {
	Force []bool `short:"f" long:"force" description:"Force policy generation"`
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
	err := logger.SimpleSetup()
	if err != nil {
		fmt.Fprintf(os.Stderr, "WARNING: failed to activate logging: %s\n", err)
	}
	if _, err := parser.Parse(); err != nil {
		// FIXME: need root
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "must supply path to package.yaml")
		os.Exit(1)
	}

	fn := os.Args[1]
	if _, err := os.Stat(fn); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "no such file: %s\n", fn)
		os.Exit(1)
	}

	if err := snappy.GeneratePolicyFromFile(fn, optionsData.Force); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	os.Exit(0)
}
