// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/logger"
)

var (
	Stderr io.Writer = os.Stderr
	Stdout io.Writer = os.Stdout

	opts   struct{}
	parser *flags.Parser = flags.NewParser(&opts, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
)

const (
	shortHelp = "Handle snapd daemon failures"
	longHelp  = `
snap-failure is a tool that handles failures of the snapd daemon and
reverts if appropriate.
`
)

func init() {
	mylog.Check(logger.SimpleSetup(nil))
}

func main() {
	mylog.Check(run())
}

func run() error {
	mylog.Check(parseArgs(os.Args[1:]))

	return nil
}

func parseArgs(args []string) error {
	parser.ShortDescription = shortHelp
	parser.LongDescription = longHelp

	_ := mylog.Check2(parser.ParseArgs(args))
	return err
}
