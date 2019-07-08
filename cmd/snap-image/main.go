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
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
)

var (
	// Standard streams, redirected for testing.
	Stdin  io.Reader = os.Stdin
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
	// set to logger.Panicf in testing
	noticef = logger.Noticef

	errorPrefix = i18n.G("error: %v\n")

	commands []*cmdInfo
)

// cmdInfo holds information needed to call parser.AddCommand(...).
type cmdInfo struct {
	name, shortHelp, longHelp string
	builder                   func() flags.Commander
	optDescs                  map[string]string
	argDescs                  []argDesc
	extra                     func(*flags.Command)
}

type argDesc struct {
	name string
	desc string
}

func addCommand(name string, builder func() flags.Commander, optDescs map[string]string, argDescs []argDesc) *cmdInfo {
	info := &cmdInfo{
		name:     name,
		builder:  builder,
		optDescs: optDescs,
		argDescs: argDescs,
	}
	commands = append(commands, info)
	return info
}

func parser() *flags.Parser {
	flagopts := flags.Options(flags.PassDoubleDash | flags.HelpFlag)
	parser := flags.NewParser(nil, flagopts)
	// parser.Usage = ""

	for _, c := range commands {
		obj := c.builder()
		cmd, err := parser.AddCommand(c.name, c.shortHelp, strings.TrimSpace(c.longHelp), obj)
		if err != nil {
			logger.Panicf("cannot add command %q: %v", c.name, err)
		}

		opts := cmd.Options()
		if c.optDescs != nil && len(opts) != len(c.optDescs) {
			logger.Panicf("wrong number of option descriptions for %s: expected %d, got %d", c.name, len(opts), len(c.optDescs))
		}
		for _, opt := range opts {
			name := opt.LongName
			if name == "" {
				name = string(opt.ShortName)
			}
			desc, ok := c.optDescs[name]
			if !(c.optDescs == nil || ok) {
				logger.Panicf("%s missing description for %s", c.name, name)
			}
			if desc != "" {
				opt.Description = desc
			}
		}

		args := cmd.Args()
		if c.argDescs != nil && len(args) != len(c.argDescs) {
			logger.Panicf("wrong number of argument descriptions for %s: expected %d, got %d", c.name, len(args), len(c.argDescs))
		}
		for i, arg := range args {
			name, desc := arg.Name, ""
			if c.argDescs != nil {
				name = c.argDescs[i].name
				desc = c.argDescs[i].desc
			}
			arg.Name = name
			arg.Description = desc
		}
	}

	return parser
}

func main() {
	if err := logger.SimpleSetup(); err != nil {
		fmt.Fprintf(Stderr, "failed to prepare logging: %v\n", err)
	}

	if err := run(); err != nil {
		fmt.Fprintf(Stderr, errorPrefix, err)
		os.Exit(1)
	}

}

func run() error {
	parser := parser()
	_, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); ok {
			switch e.Type {
			case flags.ErrCommandRequired:
				return nil
			case flags.ErrHelp:
				parser.WriteHelp(Stderr)
				return nil
			case flags.ErrUnknownCommand:
				sub := os.Args[1]
				// TRANSLATORS: %q is the command the user entered; %s is 'snap help' or 'snap help <cmd>'
				return fmt.Errorf(i18n.G("unknown command %q"), sub)
			}
		}

		return err
	}

	return nil
}
