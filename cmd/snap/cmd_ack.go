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
	"os"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdAck struct {
	clientMixin
	AckOptions struct {
		AssertionFile flags.Filename
	} `positional-args:"true" required:"true"`
}

var (
	shortAckHelp = i18n.G("Add an assertion to the system")
	longAckHelp  = i18n.G(`
The ack command tries to add an assertion to the system assertion database.

The assertion may also be a newer revision of a pre-existing assertion that it
will replace.

To succeed the assertion must be valid, its signature verified with a known
public key and the assertion consistent with and its prerequisite in the
database.
`)
)

func init() {
	addCommand("ack", shortAckHelp, longAckHelp, func() flags.Commander {
		return &cmdAck{}
	}, nil, []argDesc{{
		// TRANSLATORS: This needs to begin with < and end with >
		name: i18n.G("<assertion file>"),
		// TRANSLATORS: This should not start with a lowercase letter.
		desc: i18n.G("Assertion file"),
	}})
}

func ackFile(cli *client.Client, assertFile string) error {
	assertData := mylog.Check2(os.ReadFile(assertFile))

	return cli.Ack(assertData)
}

func (x *cmdAck) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	mylog.Check(ackFile(x.client, string(x.AckOptions.AssertionFile)))

	return nil
}
