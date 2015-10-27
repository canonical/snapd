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
	"testing"

	"github.com/jessevdk/go-flags"
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/logger"
)

// Hook up check.v1 into the "go test" runner
func Test(t *testing.T) { TestingT(t) }

type CmdTestSuite struct {
}

var _ = Suite(&CmdTestSuite{})

func (s *CmdTestSuite) TestAddOptionDescriptionOrPanicForOption(c *C) {
	type cmdMock struct {
		Verbose bool `short:"v" long:"verbose"`
	}

	parser := flags.NewParser(&struct{}{}, 0)
	arg, err := parser.AddCommand("mock", "shortHelp", "longHelp", &cmdMock{})
	c.Assert(err, IsNil)
	c.Assert(arg.Options()[0].LongName, Equals, "verbose")
	c.Assert(arg.Options()[0].Description, Equals, "")
	addOptionDescription(arg, "verbose", "verbose description")
	c.Assert(arg.Options()[0].Description, Equals, "verbose description")
}

func (s *CmdTestSuite) TestAddOptionDescriptionOrPanicForPositional(c *C) {
	type cmdMock struct {
		Positional struct {
			PackageName string `positional-arg-name:"package name"`
		} `positional-args:"yes"`
	}

	parser := flags.NewParser(&struct{}{}, 0)
	arg, err := parser.AddCommand("mock", "shortHelp", "longHelp", &cmdMock{})
	c.Assert(err, IsNil)
	c.Assert(arg.Args()[0].Name, Equals, "package name")
	c.Assert(arg.Args()[0].Description, Equals, "")
	addOptionDescription(arg, "package name", "pkgname description")
	c.Assert(arg.Args()[0].Description, Equals, "pkgname description")
}

func (s *CmdTestSuite) TestAddOptionDescriptionOrPanicWillPanic(c *C) {
	// disable logging so log doesn't scare people
	logger.SetLogger(logger.NullLogger)
	defer func() { c.Check(logger.SimpleSetup(), IsNil) }()

	parser := flags.NewParser(&struct{}{}, 0)
	arg, err := parser.AddCommand("mock", "shortHelp", "longHelp", &struct{}{})
	c.Assert(err, IsNil)
	f := func() {
		addOptionDescription(arg, "package name", "pkgname description")
	}
	c.Assert(f, PanicMatches, "can not set option description for \"package name\"")
}
