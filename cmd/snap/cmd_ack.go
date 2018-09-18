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
	"fmt"
	"io/ioutil"

	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

type cmdAck struct {
	AckOptions struct {
		AssertionFile flags.Filename
	} `positional-args:"true" required:"true"`
}

var shortAckHelp = i18n.G("Add an assertion to the system")
var longAckHelp = i18n.G(`
The ack command tries to add an assertion to the system assertion database.

The assertion may also be a newer revision of a pre-existing assertion that it
will replace.

To succeed the assertion must be valid, its signature verified with a known
public key and the assertion consistent with and its prerequisite in the
database.
`)

func init() {
	addCommand("ack", shortAckHelp, longAckHelp, func() flags.Commander {
		return &cmdAck{}
	}, nil, []argDesc{{
		// TRANSLATORS: This needs to be wrapped in <>s.
		name: i18n.G("<assertion file>"),
		// TRANSLATORS: This should probably not start with a lowercase letter.
		desc: i18n.G("Assertion file"),
	}})
}

func ackFile(assertFile string) error {
	assertData, err := ioutil.ReadFile(assertFile)
	if err != nil {
		return err
	}

	return Client().Ack(assertData)
}

func (x *cmdAck) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	if err := ackFile(string(x.AckOptions.AssertionFile)); err != nil {
		return fmt.Errorf("cannot assert: %v", err)
	}
	return nil
}
