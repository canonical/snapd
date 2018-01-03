// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"encoding/json"
	"fmt"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/advisor"
	"github.com/snapcore/snapd/i18n"
)

type cmdAdviceCommand struct {
	Positionals struct {
		Command string `required:"yes"`
	} `positional-args:"true"`

	Format string `long:"format"`
}

var shortAdviceCommandHelp = i18n.G("Advice on available snaps.")
var longAdviceCommandHelp = i18n.G(`
The advice-command command shows what snaps with the given command are 
available.
`)

func init() {
	cmd := addCommand("advice-command", shortAdviceCommandHelp, longAdviceCommandHelp, func() flags.Commander {
		return &cmdAdviceCommand{}
	}, nil, []argDesc{
		{name: "<command>"},
	})
	cmd.hidden = true
}

func outputText(command string, snaps []string) error {
	fmt.Fprintf(Stdout, i18n.G("The program %q can be found in the following snaps:\n"), command)
	for _, snap := range snaps {
		fmt.Fprintf(Stdout, " * %s\n", snap)
	}
	fmt.Fprintf(Stdout, i18n.G("Try: snap install <selected snap>\n"))
	return nil
}

func outputJSON(command string, snaps []string) error {
	enc := json.NewEncoder(Stdout)
	enc.Encode(snaps)
	return nil
}

func (x *cmdAdviceCommand) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	needle := x.Positionals.Command

	snaps, err := advisor.Commands.Find(needle)
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		return nil
	}

	switch x.Format {
	case "json":
		return outputJSON(needle, snaps)
	case "text", "":
		return outputText(needle, snaps)
	default:
		return fmt.Errorf("unsupported format %q", x.Format)
	}

	return nil
}
