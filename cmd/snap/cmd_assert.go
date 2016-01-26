// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
	"io/ioutil"

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

type assertOptions struct {
	AssertionFile string `positional-arg-name:"assertion-file" description:"assertion file"`
}

type cmdAssert struct {
	assertOptions `positional-args:"true" required:"true"`
}

var (
	shortAssertHelp = i18n.G("Assert tries to add an assertion to the system")
	longAssertHelp  = i18n.G(`This command tries to add an assertion to the system assertion database.

The assertion may also be a newer revision of a preexisting assertion that it will replace.

To succeed the assertion must be valid, its signature verified with a known public key and the assertion consistent with and its prerequisite in the database.`)
)

func init() {
	_, err := parser.AddCommand("assert", shortAssertHelp, longAssertHelp, &cmdAssert{})
	if err != nil {
		logger.Panicf("unable to add assert command: %v", err)
	}
}

func (x *cmdAssert) Execute(args []string) error {
	assertFile := x.assertOptions.AssertionFile

	assertData, err := ioutil.ReadFile(assertFile)
	if err != nil {
		return err
	}

	return client.New().Assert(assertData)
}
