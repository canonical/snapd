// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"github.com/jessevdk/go-flags"

	"github.com/ubuntu-core/snappy/i18n"
)

type cmdAbort struct {
	Positional struct {
		Id string `positional-arg-name:"change-id"`
	} `positional-args:"yes" required:"yes"`
}

var shortAbortHelp = i18n.G("Abort a pending change")

var longAbortHelp = i18n.G(`
The abort command attempts to abort a change that still has pending tasks.
`)

func init() {
	addCommand("abort",
		shortAbortHelp,
		longAbortHelp,
		func() flags.Commander {
			return &cmdAbort{}
		})
}

func (x *cmdAbort) Execute(args []string) error {
	cli := Client()
	_, err := cli.Abort(x.Positional.Id)
	return err
}
