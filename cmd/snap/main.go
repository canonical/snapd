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

	"github.com/ubuntu-core/snappy/logger"

	"github.com/jessevdk/go-flags"
)

type options struct {
	// No global options yet
}

var optionsData options

var parser *flags.Parser

// Stdout is the standard output stream, it is redirected for testing.
var Stdout io.Writer = os.Stdout

// Stderr is the standard error stream, it is redirected for testing.
var Stderr io.Writer = os.Stderr

// Parser creates and populates a fresh parser.
// Since commands have local state a fresh parser is required to isolate tests
// from each other.
func Parser() *flags.Parser {
	parser := flags.NewParser(&optionsData, flags.HelpFlag|flags.PassDoubleDash)
	// Add all regular commands
	for _, c := range []struct {
		name, shortHelp, longHelp string
		obj                       interface{}
	}{
		{"assert", shortAssertHelp, longAssertHelp, &cmdAssert{}},
		{"asserts", shortAssertsHelp, longAssertsHelp, &cmdAsserts{}},
		{"find", shortFindHelp, longFindHelp, &cmdFind{}},
		{"grant", shortGrantHelp, longGrantHelp, &cmdGrant{}},
		{"revoke", shortRevokeHelp, longRevokeHelp, &cmdRevoke{}},
		{"skills", shortSkillsHelp, longSkillsHelp, &cmdSkills{}},
		{"add-cap", shortAddCapHelp, longAddCapHelp, &cmdAddCap{}},
		{"list-caps", shortListCapsHelp, longListCapsHelp, &cmdListCaps{}},
		{"remove-cap", shortRemoveCapHelp, longRemoveCapHelp, &cmdRemoveCap{}},
	} {
		if _, err := parser.AddCommand(c.name, c.shortHelp, c.longHelp, c.obj); err != nil {
			logger.Panicf("unable to add command %q: %v", c.name, err)
		}
	}
	// Add the experimental command
	experimentalCommand, err := parser.AddCommand("experimental", shortExperimentalHelp, longExperimentalHelp, &cmdExperimental{})
	if err != nil {
		logger.Panicf("unable to add command %q: %v", "experimental", err)
	}
	// Add all the sub-commands of the experimental command
	for _, c := range []struct {
		name, shortHelp, longHelp string
		obj                       interface{}
	}{
		{"add-skill", shortAddSkillHelp, longAddSkillHelp, &cmdAddSkill{}},
		{"add-skill-slot", shortAddSkillSlotHelp, longAddSkillSlotHelp, &cmdAddSkillSlot{}},
		{"remove-skill", shortRemoveSkillHelp, longRemoveSkillHelp, &cmdRemoveSkill{}},
		{"remove-skill-slot", shortRemoveSkillSlotHelp, longRemoveSkillSlotHelp, &cmdRemoveSkillSlot{}},
	} {
		if _, err = experimentalCommand.AddCommand(c.name, c.shortHelp, c.longHelp, c.obj); err != nil {
			logger.Panicf("unable to add command %q: %v", c.name, err)
		}
	}
	return parser
}

func init() {
	parser = Parser()
	err := logger.SimpleSetup()
	if err != nil {
		fmt.Fprintf(Stderr, "WARNING: failed to activate logging: %s\n", err)
	}
}

func main() {
	if err := run(); err != nil {
		isHelp := false
		switch err.(type) {
		case *flags.Error:
			if err.(*flags.Error).Type == flags.ErrHelp {
				isHelp = true
			}
		}
		if isHelp {
			fmt.Fprintf(Stdout, "%v\n", err)
			os.Exit(0)
		} else {
			fmt.Fprintf(Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	}
}

func run() error {
	_, err := parser.Parse()
	if err != nil {
		return err
	}
	if _, ok := err.(*flags.Error); !ok {
		logger.Debugf("cannot parse arguments: %v: %v", os.Args, err)
	}
	return nil
}
