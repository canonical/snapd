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

	"github.com/ubuntu-core/snappy/i18n"
)

type cmdAssert struct {
	AssertOptions struct {
		AssertionFile string `positional-arg-name:"assertion-file" description:"assertion file"`
	} `positional-args:"true" required:"true"`
}

var shortAssertHelp = i18n.G("Adds an assertion to the system")
var longAssertHelp = i18n.G(`
The assert command tries to add an assertion to the system assertion database.

The assertion may also be a newer revision of a preexisting assertion that it
will replace.

To succeed the assertion must be valid, its signature verified with a known
public key and the assertion consistent with and its prerequisite in the
database.
`)

func init() {
	addCommand("assert", shortAssertHelp, longAssertHelp, func() interface{} {
		return &cmdAssert{}
	})
}

func (x *cmdAssert) Execute(args []string) error {
	assertFile := x.AssertOptions.AssertionFile

	assertData, err := ioutil.ReadFile(assertFile)
	if err != nil {
		return err
	}

	return Client().Assert(assertData)
}
