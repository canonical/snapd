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
	"github.com/ubuntu-core/snappy/i18n"

	"github.com/jessevdk/go-flags"
)

type cmdMan struct{}

var shortManHelp = i18n.G("produces manpage")
var longManHelp = i18n.G("produces manpage")

func init() {
	cmd := addCommand("man", shortManHelp, longManHelp, func() flags.Commander {
		return &cmdMan{}
	})
	cmd.hidden = true
}

func (*cmdMan) Execute([]string) error {
	parser := Parser()
	parser.ShortDescription = "Tool to interact with snaps"
	parser.LongDescription = `
The snap tool interacts with the snapd daemon to control the snappy software platform.
`
	parser.WriteManPage(Stdout)

	return nil
}
