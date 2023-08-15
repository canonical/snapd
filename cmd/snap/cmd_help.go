// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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
	"io"
	"regexp"
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
			// this means *either* a bare 'snap --help',
			// *or* 'snap --help command'
			//
			// If we return nil in the first case go-flags
			// will throw up an ErrCommandRequired on its
			// own, but in the second case it'll go on to
			// run the command, which is very unexpected.
			//
			// So we force the ErrCommandRequired here.

			// toplevel --help gets handled via ErrCommandRequired
			return &flags.Error{Type: flags.ErrCommandRequired}
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
		Subs []string `positional-arg-name:"<command>"`
	} `positional-args:"yes"`
	parser *flags.Parser
}

func init() {
	addCommand("help", shortHelpHelp, longHelpHelp, func() flags.Commander { return &cmdHelp{} },
		map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"all": i18n.G("Show a short summary of all commands"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"man": i18n.G("Generate the manpage"),
		}, nil)
}

func (cmd *cmdHelp) setParser(parser *flags.Parser) {
	cmd.parser = parser
}

// manfixer is a hackish way to fix drawbacks in the generated manpage:
// - no way to get it into section 8
// - duplicated TP lines that break older groff (e.g. 14.04), lp:1814767
type manfixer struct {
	bytes.Buffer
	done bool
}

func (w *manfixer) Write(buf []byte) (int, error) {
	if !w.done {
		w.done = true
		if bytes.HasPrefix(buf, []byte(".TH snap 1 ")) {
			// io.Writer.Write must not modify the buffer, even temporarily
			n, _ := w.Buffer.Write(buf[:9])
			w.Buffer.Write([]byte{'8'})
			m, err := w.Buffer.Write(buf[10:])
			return n + m + 1, err
		}
	}
	return w.Buffer.Write(buf)
}

var tpRegexp = regexp.MustCompile(`(?m)(?:^\.TP\n)+`)

func (w *manfixer) flush() {
	str := tpRegexp.ReplaceAllLiteralString(w.Buffer.String(), ".TP\n")
	io.Copy(Stdout, strings.NewReader(str))
}

func manExtend(out io.Writer) {
	out.Write([]byte(`
.SH NOTES
.IP " 1. " 4
Online documentation
.RS 4
\%https://docs.snapcraft.io
.RE
.SH BUGS
.sp
Please report all bugs with \fI\%https://bugs.launchpad.net/snapd/+filebug\fP
`))
}

func (cmd cmdHelp) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	if cmd.Manpage {
		// you shouldn't try to to combine --man with --all nor a
		// subcommand, but --man is hidden so no real need to check.
		out := &manfixer{}
		cmd.parser.WriteManPage(out)
		manExtend(out)
		out.flush()
		return nil
	}
	if cmd.All {
		if len(cmd.Positional.Subs) > 0 {
			return fmt.Errorf(i18n.G("help accepts a command, or '--all', but not both."))
		}
		printLongHelp(cmd.parser)
		return nil
	}

	var subcmd = cmd.parser.Command
	for _, subname := range cmd.Positional.Subs {
		subcmd = subcmd.Find(subname)
		if subcmd == nil {
			sug := "snap help"
			if x := cmd.parser.Command.Active; x != nil && x.Name != "help" {
				sug = "snap help " + x.Name
			}
			// TRANSLATORS: %q is the command the user entered; %s is 'snap help' or 'snap help <cmd>'
			return fmt.Errorf(i18n.G("unknown command %q, see '%s'."), subname, sug)
		}
		// this makes "snap help foo" work the same as "snap foo --help"
		cmd.parser.Command.Active = subcmd
	}
	if subcmd != cmd.parser.Command {
		return &flags.Error{Type: flags.ErrHelp}
	}
	return &flags.Error{Type: flags.ErrCommandRequired}
}

type helpCategory struct {
	Label string
	// Other is set if the category Commands should be listed
	// together under "... Other" in the `snap help` list.
	Other       bool
	Description string
	// Commands list commands belonging to the category that should
	// be listed under both `snap help` and "snap help --all`.
	Commands []string
	// AllOnlyCommands list commands belonging to the category that should
	// be listed only under "snap help --all`.
	AllOnlyCommands []string
}

// helpCategories helps us by grouping commands
var helpCategories = []helpCategory{
	{
		Label:       i18n.G("Basics"),
		Description: i18n.G("basic snap management"),
		Commands:    []string{"find", "info", "install", "remove", "list"},
	}, {
		Label:       i18n.G("...more"),
		Description: i18n.G("slightly more advanced snap management"),
		Commands:    []string{"refresh", "revert", "switch", "disable", "enable", "create-cohort"},
	}, {
		Label:       i18n.G("History"),
		Description: i18n.G("manage system change transactions"),
		Commands:    []string{"changes", "tasks", "abort", "watch"},
	}, {
		Label:       i18n.G("Daemons"),
		Description: i18n.G("manage services"),
		Commands:    []string{"services", "start", "stop", "restart", "logs"},
	}, {
		Label:       i18n.G("Permissions"),
		Description: i18n.G("manage permissions"),
		Commands:    []string{"connections", "interface", "connect", "disconnect"},
	}, {
		Label:       i18n.G("Configuration"),
		Description: i18n.G("system administration and configuration"),
		Commands:    []string{"get", "set", "unset", "wait"},
	}, {
		Label:       i18n.G("App Aliases"),
		Description: i18n.G("manage aliases"),
		Commands:    []string{"alias", "aliases", "unalias", "prefer"},
	}, {
		Label:       i18n.G("Account"),
		Description: i18n.G("authentication to snapd and the snap store"),
		Commands:    []string{"login", "logout", "whoami"},
	}, {
		Label:           i18n.G("Snapshots"),
		Description:     i18n.G("archives of snap data"),
		Commands:        []string{"saved", "save", "check-snapshot", "restore", "forget"},
		AllOnlyCommands: []string{"export-snapshot", "import-snapshot"},
	}, {
		Label:       i18n.G("Device"),
		Description: i18n.G("manage device"),
		Commands:    []string{"model", "remodel", "reboot", "recovery"},
	}, {
		Label:       i18n.G("Warnings"),
		Other:       true,
		Description: i18n.G("manage warnings"),
		Commands:    []string{"warnings", "okay"},
	}, {
		Label:       i18n.G("Assertions"),
		Other:       true,
		Description: i18n.G("manage assertions"),
		Commands:    []string{"known", "ack"},
	}, {
		Label:           i18n.G("Introspection"),
		Other:           true,
		Description:     i18n.G("introspection and debugging of snapd"),
		Commands:        []string{"version"},
		AllOnlyCommands: []string{"debug"},
	}, {
		Label:           i18n.G("Development"),
		Description:     i18n.G("developer-oriented features"),
		Commands:        []string{"download", "pack", "run", "try"},
		AllOnlyCommands: []string{"prepare-image"},
	}, {
		Label:       i18n.G("Quota Groups"),
		Description: i18n.G("Manage quota groups for snaps"),
		Commands:    []string{"set-quota", "remove-quota", "quotas", "quota"},
	}, {
		Label:       i18n.G("Validation Sets"),
		Description: i18n.G("Manage validation sets"),
		Commands:    []string{"validate"},
	},
}

var (
	longSnapDescription = strings.TrimSpace(i18n.G(`
The snap command lets you install, configure, refresh and remove snaps.
Snaps are packages that work across many different Linux distributions,
enabling secure delivery and operation of the latest apps and utilities.
`))
	snapUsage               = i18n.G("Usage: snap <command> [<options>...]")
	snapHelpCategoriesIntro = i18n.G("Commonly used commands can be classified as follows:")
	snapHelpAllIntro        = i18n.G("Commands can be classified as follows:")
	snapHelpAllFooter       = i18n.G("For more information about a command, run 'snap help <command>'.")
	snapHelpFooter          = i18n.G("For a short summary of all commands, run 'snap help --all'.")
)

func printHelpHeader(cmdsIntro string) {
	fmt.Fprintln(Stdout, longSnapDescription)
	fmt.Fprintln(Stdout)
	fmt.Fprintln(Stdout, snapUsage)
	fmt.Fprintln(Stdout)
	fmt.Fprintln(Stdout, cmdsIntro)
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
	printHelpHeader(snapHelpCategoriesIntro)
	maxLen := utf8.RuneCountInString("... Other")
	var otherCommands []string
	var develCateg *helpCategory
	for _, categ := range helpCategories {
		if categ.Other {
			otherCommands = append(otherCommands, categ.Commands...)
			continue
		}
		if categ.Label == "Development" {
			develCateg = &categ
		}
		if l := utf8.RuneCountInString(categ.Label); l > maxLen {
			maxLen = l
		}
	}

	fmt.Fprintln(Stdout)
	for _, categ := range helpCategories {
		// Other and Development will come last
		if categ.Other || categ.Label == "Development" || len(categ.Commands) == 0 {
			continue
		}
		fmt.Fprintf(Stdout, "%*s: %s\n", maxLen+2, categ.Label, strings.Join(categ.Commands, ", "))
	}
	// ... Other
	if len(otherCommands) > 0 {
		fmt.Fprintf(Stdout, "%*s: %s\n", maxLen+2, "... Other", strings.Join(otherCommands, ", "))
	}
	// Development last
	if develCateg != nil && len(develCateg.Commands) > 0 {
		fmt.Fprintf(Stdout, "%*s: %s\n", maxLen+2, "Development", strings.Join(develCateg.Commands, ", "))
	}
	printHelpFooter()
}

// this is "snap help --all"
func printLongHelp(parser *flags.Parser) {
	printHelpHeader(snapHelpAllIntro)
	maxLen := 0
	for _, categ := range helpCategories {
		for _, command := range categ.Commands {
			if l := len(command); l > maxLen {
				maxLen = l
			}
		}
		for _, command := range categ.AllOnlyCommands {
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

	listCmds := func(cmds []string) {
		for _, name := range cmds {
			cmd := cmdLookup[name]
			if cmd == nil {
				fmt.Fprintf(Stderr, "??? Cannot find command %q mentioned in help categories, please report!\n", name)
			} else {
				fmt.Fprintf(Stdout, "    %*s  %s\n", -maxLen, name, cmd.ShortDescription)
			}
		}
	}

	for _, categ := range helpCategories {
		fmt.Fprintln(Stdout)
		fmt.Fprintf(Stdout, "  %s (%s):\n", categ.Label, categ.Description)
		listCmds(categ.Commands)
		listCmds(categ.AllOnlyCommands)
	}
	printHelpAllFooter()
}
