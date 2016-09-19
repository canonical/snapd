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
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"

	"github.com/jessevdk/go-flags"
)

// Standard streams, redirected for testing.
var (
	Stdin  io.Reader = os.Stdin
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
)

type options struct {
	Version func() `long:"version"`
}

type argDesc struct {
	name string
	desc string
}

var optionsData options

// ErrExtraArgs is returned  if extra arguments to a command are found
var ErrExtraArgs = fmt.Errorf(i18n.G("too many arguments for command"))

// cmdInfo holds information needed to call parser.AddCommand(...).
type cmdInfo struct {
	name, shortHelp, longHelp string
	builder                   func() flags.Commander
	hidden                    bool
	optDescs                  map[string]string
	argDescs                  []argDesc
}

// commands holds information about all non-experimental commands.
var commands []*cmdInfo

// experimentalCommands holds information about all experimental commands.
var experimentalCommands []*cmdInfo

// addCommand replaces parser.addCommand() in a way that is compatible with
// re-constructing a pristine parser.
func addCommand(name, shortHelp, longHelp string, builder func() flags.Commander, optDescs map[string]string, argDescs []argDesc) *cmdInfo {
	info := &cmdInfo{
		name:      name,
		shortHelp: shortHelp,
		longHelp:  longHelp,
		builder:   builder,
		optDescs:  optDescs,
		argDescs:  argDescs,
	}
	commands = append(commands, info)
	return info
}

// addExperimentalCommand replaces parser.addCommand() in a way that is
// compatible with re-constructing a pristine parser. It is meant for
// adding experimental commands.
func addExperimentalCommand(name, shortHelp, longHelp string, builder func() flags.Commander) *cmdInfo {
	info := &cmdInfo{
		name:      name,
		shortHelp: shortHelp,
		longHelp:  longHelp,
		builder:   builder,
	}
	experimentalCommands = append(experimentalCommands, info)
	return info
}

type parserSetter interface {
	setParser(*flags.Parser)
}

func lintDesc(cmdName, optName, desc, origDesc string) {
	if len(optName) == 0 {
		logger.Panicf("option on %q has no name", cmdName)
	}
	if len(origDesc) != 0 {
		logger.Panicf("description of %s's %q of %q set from tag (=> no i18n)", cmdName, optName, origDesc)
	}
	if len(desc) > 0 {
		if !unicode.IsUpper(([]rune)(desc)[0]) {
			logger.Panicf("description of %s's %q not uppercase: %q", cmdName, optName, desc)
		}
	}
}

func lintArg(cmdName, optName, desc, origDesc string) {
	lintDesc(cmdName, optName, desc, origDesc)
	if optName[0] != '<' || optName[len(optName)-1] != '>' {
		logger.Panicf("argument %q's %q should have <>s", cmdName, optName)
	}
}

// Parser creates and populates a fresh parser.
// Since commands have local state a fresh parser is required to isolate tests
// from each other.
func Parser() *flags.Parser {
	optionsData.Version = func() {
		sv, err := Client().ServerVersion()
		if err != nil {
			sv = &client.ServerVersion{
				Version:     i18n.G("unavailable"),
				Series:      "-",
				OSID:        "-",
				OSVersionID: "-",
			}
		}

		w := tabWriter()

		fmt.Fprintf(w, "snap\t%s\n", cmd.Version)
		fmt.Fprintf(w, "snapd\t%s\n", sv.Version)
		fmt.Fprintf(w, "series\t%s\n", sv.Series)
		if sv.OnClassic {
			fmt.Fprintf(w, "%s\t%s\n", sv.OSID, sv.OSVersionID)
		}
		w.Flush()

		panic(&exitStatus{0})
	}
	parser := flags.NewParser(&optionsData, flags.HelpFlag|flags.PassDoubleDash|flags.PassAfterNonOption)
	parser.ShortDescription = i18n.G("Tool to interact with snaps")
	parser.LongDescription = i18n.G(`
The snap tool interacts with the snapd daemon to control the snappy software platform.
`)
	parser.FindOptionByLongName("version").Description = i18n.G("Print the version and exit")

	// Add all regular commands
	for _, c := range commands {
		obj := c.builder()
		if x, ok := obj.(parserSetter); ok {
			x.setParser(parser)
		}

		cmd, err := parser.AddCommand(c.name, c.shortHelp, strings.TrimSpace(c.longHelp), obj)
		if err != nil {

			logger.Panicf("cannot add command %q: %v", c.name, err)
		}
		cmd.Hidden = c.hidden

		opts := cmd.Options()
		if c.optDescs != nil && len(opts) != len(c.optDescs) {
			logger.Panicf("wrong number of option descriptions for %s: expected %d, got %d", c.name, len(opts), len(c.optDescs))
		}
		for _, opt := range opts {
			name := opt.LongName
			if name == "" {
				name = string(opt.ShortName)
			}
			desc, ok := c.optDescs[name]
			if !(c.optDescs == nil || ok) {
				logger.Panicf("%s missing description for %s", c.name, name)
			}
			lintDesc(c.name, name, desc, opt.Description)
			if desc != "" {
				opt.Description = desc
			}
		}

		args := cmd.Args()
		if c.argDescs != nil && len(args) != len(c.argDescs) {
			logger.Panicf("wrong number of argument descriptions for %s: expected %d, got %d", c.name, len(args), len(c.argDescs))
		}
		for i, arg := range args {
			name, desc := arg.Name, ""
			if c.argDescs != nil {
				name = c.argDescs[i].name
				desc = c.argDescs[i].desc
			}
			lintArg(c.name, name, desc, arg.Description)
			arg.Name = name
			arg.Description = desc
		}
	}
	// Add the experimental command
	experimentalCommand, err := parser.AddCommand("experimental", shortExperimentalHelp, longExperimentalHelp, &cmdExperimental{})
	experimentalCommand.Hidden = true
	if err != nil {
		logger.Panicf("cannot add command %q: %v", "experimental", err)
	}
	// Add all the sub-commands of the experimental command
	for _, c := range experimentalCommands {
		cmd, err := experimentalCommand.AddCommand(c.name, c.shortHelp, strings.TrimSpace(c.longHelp), c.builder())
		if err != nil {
			logger.Panicf("cannot add experimental command %q: %v", c.name, err)
		}
		cmd.Hidden = c.hidden
	}
	return parser
}

// ClientConfig is the configuration of the Client used by all commands.
var ClientConfig client.Config

// Client returns a new client using ClientConfig as configuration.
func Client() *client.Client {
	return client.New(&ClientConfig)
}

func init() {
	err := logger.SimpleSetup()
	if err != nil {
		fmt.Fprintf(Stderr, i18n.G("WARNING: failed to activate logging: %v\n"), err)
	}
}

func main() {
	cmd.ExecInCoreSnap()

	// magic \o/
	snapApp := filepath.Base(os.Args[0])
	if osutil.IsSymlink(filepath.Join(dirs.SnapBinariesDir, snapApp)) {
		cmd := &cmdRun{}
		args := []string{snapApp}
		args = append(args, os.Args[1:]...)
		// this will call syscall.Exec() so it does not return
		// *unless* there is an error, i.e. we setup a wrong
		// symlink (or syscall.Exec() fails for strange reasons)
		err := cmd.Execute(args)
		fmt.Fprintf(Stderr, i18n.G("internal error, please report: running %q failed: %v\n"), snapApp, err)
		os.Exit(46)
	}

	defer func() {
		if v := recover(); v != nil {
			if e, ok := v.(*exitStatus); ok {
				os.Exit(e.code)
			}
			panic(v)
		}
	}()

	// no magic /o\
	if err := run(); err != nil {
		fmt.Fprintf(Stderr, i18n.G("error: %v\n"), err)
		os.Exit(1)
	}
}

type exitStatus struct {
	code int
}

func (e *exitStatus) Error() string {
	return fmt.Sprintf("internal error: exitStatus{%d} being handled as normal error", e.code)
}

func run() error {
	parser := Parser()
	_, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); ok && e.Type == flags.ErrHelp {
			if parser.Command.Active != nil && parser.Command.Active.Name == "help" {
				parser.Command.Active = nil
			}
			parser.WriteHelp(Stdout)
			return nil

		}
		if e, ok := err.(*client.Error); ok && e.Kind == client.ErrorKindLoginRequired {
			u, _ := user.Current()
			if u != nil && u.Username == "root" {
				return fmt.Errorf(i18n.G(`%s (see "snap login --help")`), e.Message)
			}

			// TRANSLATORS: %s will be a message along the lines of "login required"
			return fmt.Errorf(i18n.G(`%s (try with sudo)`), e.Message)
		}
	}

	return err
}
