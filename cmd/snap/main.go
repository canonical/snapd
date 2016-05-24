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
	"io"
	"os"
	"strings"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"

	"github.com/jessevdk/go-flags"
)

// Standard streams, redirected for testing.
var (
	Stdin  io.Reader = os.Stdin
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
)

type options struct {
	Version func() `long:"version" description:"print the version and exit"`
}

var optionsData options

// cmdInfo holds information needed to call parser.AddCommand(...).
type cmdInfo struct {
	name, shortHelp, longHelp string
	builder                   func() flags.Commander
	hidden                    bool
}

// commands holds information about all non-experimental commands.
var commands []*cmdInfo

// experimentalCommands holds information about all experimental commands.
var experimentalCommands []*cmdInfo

// addCommand replaces parser.addCommand() in a way that is compatible with
// re-constructing a pristine parser.
func addCommand(name, shortHelp, longHelp string, builder func() flags.Commander) *cmdInfo {
	info := &cmdInfo{
		name:      name,
		shortHelp: shortHelp,
		longHelp:  longHelp,
		builder:   builder,
	}
	commands = append(commands, info)
	return info
}

// addExperimentalCommand replaces parser.addCommand() in a way that is
// compatible with re-constructing a pristine parser. It is meant for
// adding experimental commands.
func addExperimentalCommand(name, shortHelp, longHelp string, builder func() flags.Commander) *cmdInfo {
	info := &cmdInfo{
		name:      name,
		shortHelp: shortHelp,
		longHelp:  longHelp,
		builder:   builder,
	}
	experimentalCommands = append(experimentalCommands, info)
	return info
}

type parserSetter interface {
	setParser(*flags.Parser)
}

// Parser creates and populates a fresh parser.
// Since commands have local state a fresh parser is required to isolate tests
// from each other.
func Parser() *flags.Parser {
	optionsData.Version = func() {
		cv, err := Client().ServerVersion()
		if err != nil {
			cv = i18n.G("unavailable")
		}

		fmt.Fprintf(Stdout, "snap  %s\nsnapd %s\n", cmd.Version, cv)

		os.Exit(0)
	}
	parser := flags.NewParser(&optionsData, flags.HelpFlag|flags.PassDoubleDash)
	parser.ShortDescription = "Tool to interact with snaps"
	parser.LongDescription = `
The snap tool interacts with the snapd daemon to control the snappy software platform.
`

	// Add all regular commands
	for _, c := range commands {
		obj := c.builder()
		if x, ok := obj.(parserSetter); ok {
			x.setParser(parser)
		}

		cmd, err := parser.AddCommand(c.name, c.shortHelp, strings.TrimSpace(c.longHelp), obj)
		if err != nil {

			logger.Panicf("cannot add command %q: %v", c.name, err)
		}
		cmd.Hidden = c.hidden
	}
	// Add the experimental command
	experimentalCommand, err := parser.AddCommand("experimental", shortExperimentalHelp, longExperimentalHelp, &cmdExperimental{})
	experimentalCommand.Hidden = true
	if err != nil {
		logger.Panicf("cannot add command %q: %v", "experimental", err)
	}
	// Add all the sub-commands of the experimental command
	for _, c := range experimentalCommands {
		cmd, err := experimentalCommand.AddCommand(c.name, c.shortHelp, strings.TrimSpace(c.longHelp), c.builder())
		if err != nil {
			logger.Panicf("cannot add experimental command %q: %v", c.name, err)
		}
		cmd.Hidden = c.hidden
	}
	return parser
}

// ClientConfig is the configuration of the Client used by all commands.
var ClientConfig client.Config

// Client returns a new client using ClientConfig as configuration.
func Client() *client.Client {
	return client.New(&ClientConfig)
}

func init() {
	err := logger.SimpleSetup()
	if err != nil {
		fmt.Fprintf(Stderr, "WARNING: failed to activate logging: %s\n", err)
	}
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	parser := Parser()
	_, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); ok && e.Type == flags.ErrHelp {
			if parser.Command.Active != nil && parser.Command.Active.Name == "help" {
				parser.Command.Active = nil
			}
			parser.WriteHelp(Stdout)
			return nil

		}
		if e, ok := err.(*client.Error); ok && e.Kind == client.ErrorKindLoginRequired {
			return fmt.Errorf("%s (snap login --help)", e.Message)

		}
	}

	return err
}
