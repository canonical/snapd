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
	"bytes"
	"fmt"
	"os"

	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

var shortHelpHelp = i18n.G("Show help about a command")
var longHelpHelp = i18n.G(`
The help command displays information about snap commands.
`)

// addHelp adds --help like what go-flags would do for us, but hidden
func addHelp(parser *flags.Parser) error {
	var help struct {
		ShowHelp func() error `short:"h" long:"help"`
	}
	help.ShowHelp = func() error {
		var buf bytes.Buffer
		parser.WriteHelp(&buf)
		return &flags.Error{
			Type:    flags.ErrHelp,
			Message: buf.String(),
		}
	}
	hlpgrp, err := parser.AddGroup("Help Options", "", &help)
	if err != nil {
		return err
	}
	hlpgrp.Hidden = true
	hlp := parser.FindOptionByLongName("help")
	hlp.Description = i18n.G("Show this help message")
	hlp.Hidden = true

	return nil
}

type cmdHelp struct {
	Manpage    bool `long:"man" hidden:"true"`
	Positional struct {
		// TODO: find a way to make Command tab-complete
		Sub string `positional-arg-name:"<command>"`
	} `positional-args:"yes"`
	parser *flags.Parser
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
	if cmd.Positional.Sub != "" {
		subcmd := cmd.parser.Find(cmd.Positional.Sub)
		if subcmd == nil {
			return fmt.Errorf(i18n.G("Unknown command %q. Try 'snap help'."), cmd.Positional.Sub)
		}
		cmd.parser.Command.Active = subcmd
	}
	if cmd.Manpage {
		cmd.parser.WriteManPage(Stdout)
		os.Exit(0)
	}

	return &flags.Error{
		Type: flags.ErrHelp,
	}
}
