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
	"os"

	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

var shortHelpHelp = i18n.G("Help")
var longHelpHelp = i18n.G(`
How help for the snap command.`)

type cmdHelp struct {
	Manpage bool `long:"man"`
	parser  *flags.Parser
}

func init() {
	addCommand("help", shortHelpHelp, longHelpHelp, func() flags.Commander { return &cmdHelp{} },
		map[string]string{"man": i18n.G("Generate the manpage")}, nil)
}

func (cmd *cmdHelp) setParser(parser *flags.Parser) {
	cmd.parser = parser
}

func (cmd cmdHelp) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if cmd.Manpage {
		cmd.parser.WriteManPage(Stdout)
		os.Exit(0)
	}

	return &flags.Error{
		Type: flags.ErrHelp,
	}
}
