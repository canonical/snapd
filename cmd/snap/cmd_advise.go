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

type cmdAdviseSnap struct {
	Positionals struct {
		CommandOrPkg string `required:"yes"`
	} `positional-args:"true"`

	Format  string `long:"format" default:"pretty" choice:"pretty" choice:"json"`
	Command bool   `long:"command"`
}

var shortAdviseSnapHelp = i18n.G("Advise on available snaps")
var longAdviseSnapHelp = i18n.G(`
The advise-snap command searches for and suggests the installation of snaps.

If --command is given, it suggests snaps that provide the given command.
Otherwise it suggests snaps with the given name.
`)

func init() {
	cmd := addCommand("advise-snap", shortAdviseSnapHelp, longAdviseSnapHelp, func() flags.Commander {
		return &cmdAdviseSnap{}
	}, map[string]string{
		"command": i18n.G("Advise on snaps that provide the given command"),
		"format":  i18n.G("Use the given output format"),
	}, []argDesc{
		{name: "<command or pkg>"},
	})
	cmd.hidden = true
}

func outputAdviseExactText(command string, result []advisor.Command) error {
	fmt.Fprintf(Stdout, "\n")
	fmt.Fprintf(Stdout, i18n.G("Command %q not found, but can be installed with:\n"), command)
	fmt.Fprintf(Stdout, "\n")
	for _, snap := range result {
		fmt.Fprintf(Stdout, "sudo snap install %s\n", snap.Snap)
	}
	fmt.Fprintf(Stdout, "\n")
	fmt.Fprintf(Stdout, "See 'snap info <snap name>' for additional versions.\n")
	fmt.Fprintf(Stdout, "\n")
	return nil
}

func outputAdviseMisspellText(command string, result []advisor.Command) error {
	fmt.Fprintf(Stdout, "\n")
	fmt.Fprintf(Stdout, i18n.G("Command %q not found, did you mean:\n"), command)
	fmt.Fprintf(Stdout, "\n")
	for _, snap := range result {
		fmt.Fprintf(Stdout, " command %q from snap %q\n", snap.Command, snap.Snap)
	}
	fmt.Fprintf(Stdout, "\n")
	fmt.Fprintf(Stdout, "See 'snap info <snap name>' for additional versions.\n")
	fmt.Fprintf(Stdout, "\n")
	return nil
}

func outputAdviseJSON(command string, results []advisor.Command) error {
	enc := json.NewEncoder(Stdout)
	enc.Encode(results)
	return nil
}

func (x *cmdAdviseSnap) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if x.Command {
		return adviseCommand(x.Positionals.CommandOrPkg, x.Format)
	}

	return advisePkg(x.Positionals.CommandOrPkg)
}

func advisePkg(pkgName string) error {
	match, err := advisor.FindPackage(pkgName)
	if err != nil {
		return fmt.Errorf("advise for pkgname failed: %s", err)
	}
	if match != nil {
		fmt.Fprintf(Stdout, i18n.G("Packages matching %q:\n"), pkgName)
		fmt.Fprintf(Stdout, " * %s - %s\n", match.Snap, match.Summary)
		fmt.Fprintf(Stdout, i18n.G("Try: snap install <selected snap>\n"))
	}

	// FIXME: find mispells

	return nil
}

func adviseCommand(cmd string, format string) error {
	// find exact matches
	matches, err := advisor.FindCommand(cmd)
	if err != nil {
		return fmt.Errorf("advise for command failed: %s", err)
	}
	if len(matches) > 0 {
		switch format {
		case "json":
			return outputAdviseJSON(cmd, matches)
		case "pretty":
			return outputAdviseExactText(cmd, matches)
		default:
			return fmt.Errorf("unsupported format %q", format)
		}
	}

	// find misspellings
	matches, err = advisor.FindMisspelledCommand(cmd)
	if err != nil {
		return err
	}
	if len(matches) > 0 {
		switch format {
		case "json":
			return outputAdviseJSON(cmd, matches)
		case "pretty":
			return outputAdviseMisspellText(cmd, matches)
		default:
			return fmt.Errorf("unsupported format %q", format)
		}
	}

	return fmt.Errorf("%s: command not found", cmd)
}
