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
	"strings"
	"unicode/utf8"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
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
		// this function is called via --help (or -h). In that
		// case, parser.Command.Active should be the command
		// on which help is being requested (like "snap foo
		// --help", active is foo), or nil in the toplevel.
		if parser.Command.Active == nil {
			// toplevel --help will get handled via ErrCommandRequired
			return nil
		}
		// not toplevel, so ask for regular help
		return &flags.Error{Type: flags.ErrHelp}
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
	All        bool `long:"all"`
	Manpage    bool `long:"man" hidden:"true"`
	Positional struct {
		// TODO: find a way to make Command tab-complete
		Sub string `positional-arg-name:"<command>"`
	} `positional-args:"yes"`
	parser *flags.Parser
}

func init() {
	addCommand("help", shortHelpHelp, longHelpHelp, func() flags.Commander { return &cmdHelp{} },
		map[string]string{
			"all": i18n.G("Show a short summary of all commands"),
			"man": i18n.G("Generate the manpage"),
		}, nil)
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
		return nil
	}
	if cmd.All {
		printLongHelp(cmd.parser)
		return nil
	}

	if cmd.Positional.Sub != "" {
		subcmd := cmd.parser.Find(cmd.Positional.Sub)
		if subcmd == nil {
			return fmt.Errorf(i18n.G("Unknown command %q. Try 'snap help'."), cmd.Positional.Sub)
		}
		// this makes "snap help foo" work the same as "snap foo --help"
		cmd.parser.Command.Active = subcmd
		return &flags.Error{Type: flags.ErrHelp}
	}

	return &flags.Error{Type: flags.ErrCommandRequired}
}

type helpCategory struct {
	Label       string
	Description string
	Commands    []string
}

// helpCategories helps us by grouping commands
var helpCategories = []helpCategory{
	{
		Label:       i18n.G("Basics"),
		Description: i18n.G("basic snap management"),
		Commands:    []string{"find", "info", "install", "list", "remove"},
	}, {
		Label:       i18n.G("...more"),
		Description: i18n.G("slightly more advanced snap management"),
		Commands:    []string{"refresh", "revert", "switch", "disable", "enable"},
	}, {
		Label:       i18n.G("History"),
		Description: i18n.G("manage system change transactions"),
		Commands:    []string{"changes", "tasks", "abort", "watch"},
	}, {
		Label:       i18n.G("Daemons"),
		Description: i18n.G("manage services"),
		Commands:    []string{"services", "start", "stop", "restart", "logs"},
	}, {
		Label:       i18n.G("Commands"),
		Description: i18n.G("manage aliases"),
		Commands:    []string{"alias", "aliases", "unalias", "prefer"},
	}, {
		Label:       i18n.G("Configuration"),
		Description: i18n.G("system administration and configuration"),
		Commands:    []string{"get", "set", "wait"},
	}, {
		Label:       i18n.G("Account"),
		Description: i18n.G("authentication to snapd and the snap store"),
		Commands:    []string{"login", "logout", "whoami"},
	}, {
		Label:       i18n.G("Permissions"),
		Description: i18n.G("manage permissions"),
		Commands:    []string{"interfaces", "interface", "connect", "disconnect"},
	}, {
		Label:       i18n.G("Other"),
		Description: i18n.G("miscelanea"),
		Commands:    []string{"version", "warnings", "okay"},
	}, {
		Label:       i18n.G("Development"),
		Description: i18n.G("developer-oriented features"),
		Commands:    []string{"run", "pack", "try", "ack", "known", "download"},
	},
}

var (
	longSnapDescription = strings.TrimSpace(i18n.G(`
The snap command lets you install, configure, refresh and remove snaps.
Snaps are packages that work across many different Linux distributions,
enabling secure delivery and operation of the latest apps and utilities.
`))
	snapUsage               = i18n.G("Usage: snap <command> [<options>...]")
	snapHelpCategoriesIntro = i18n.G("Commands can be classified as follows:")
	snapHelpAllFooter       = i18n.G("For more information about a command, run 'snap help <command>'.")
	snapHelpFooter          = i18n.G("For a short summary of all commands, run 'snap help --all'.")
)

func printHelpHeader() {
	fmt.Fprintln(Stdout, longSnapDescription)
	fmt.Fprintln(Stdout)
	fmt.Fprintln(Stdout, snapUsage)
	fmt.Fprintln(Stdout)
	fmt.Fprintln(Stdout, snapHelpCategoriesIntro)
	fmt.Fprintln(Stdout)
}

func printHelpAllFooter() {
	fmt.Fprintln(Stdout)
	fmt.Fprintln(Stdout, snapHelpAllFooter)
}

func printHelpFooter() {
	printHelpAllFooter()
	fmt.Fprintln(Stdout, snapHelpFooter)
}

// this is called when the Execute returns a flags.Error with ErrCommandRequired
func printShortHelp() {
	printHelpHeader()
	maxLen := 0
	for _, categ := range helpCategories {
		if l := utf8.RuneCountInString(categ.Label); l > maxLen {
			maxLen = l
		}
	}
	for _, categ := range helpCategories {
		fmt.Fprintf(Stdout, "%*s: %s\n", maxLen+2, categ.Label, strings.Join(categ.Commands, ", "))
	}
	printHelpFooter()
}

// this is "snap help --all"
func printLongHelp(parser *flags.Parser) {
	printHelpHeader()
	maxLen := 0
	for _, categ := range helpCategories {
		for _, command := range categ.Commands {
			if l := len(command); l > maxLen {
				maxLen = l
			}
		}
	}

	// flags doesn't have a LookupCommand?
	commands := parser.Commands()
	cmdLookup := make(map[string]*flags.Command, len(commands))
	for _, cmd := range commands {
		cmdLookup[cmd.Name] = cmd
	}

	for _, categ := range helpCategories {
		fmt.Fprintln(Stdout)
		fmt.Fprintf(Stdout, "  %s (%s):\n", categ.Label, categ.Description)
		for _, name := range categ.Commands {
			cmd := cmdLookup[name]
			if cmd == nil {
				fmt.Fprintf(Stderr, "??? Cannot find command %q mentioned in help categories, please report!\n", name)
			} else {
				fmt.Fprintf(Stdout, "    %*s  %s\n", -maxLen, name, cmd.ShortDescription)
			}
		}
	}
	printHelpAllFooter()
}
