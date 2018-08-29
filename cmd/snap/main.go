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
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jessevdk/go-flags"

	"golang.org/x/crypto/ssh/terminal"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/cmd"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

func init() {
	// set User-Agent for when 'snap' talks to the store directly (snap download etc...)
	httputil.SetUserAgentFromVersion(cmd.Version, "snap")

	if osutil.GetenvBool("SNAPD_DEBUG") || osutil.GetenvBool("SNAPPY_TESTING") {
		// in tests or when debugging, enforce the "tidy" lint checks
		noticef = logger.Panicf
	}

	// plug/slot sanitization not used nor possible from snap command, make it no-op
	snap.SanitizePlugsSlots = func(snapInfo *snap.Info) {}
}

var (
	// Standard streams, redirected for testing.
	Stdin  io.Reader = os.Stdin
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
	// overridden for testing
	ReadPassword = terminal.ReadPassword
	// set to logger.Panicf in testing
	noticef = logger.Noticef
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
	alias                     string
}

// commands holds information about all non-debug commands.
var commands []*cmdInfo

// debugCommands holds information about all debug commands.
var debugCommands []*cmdInfo

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

// addDebugCommand replaces parser.addCommand() in a way that is
// compatible with re-constructing a pristine parser. It is meant for
// adding debug commands.
func addDebugCommand(name, shortHelp, longHelp string, builder func() flags.Commander, optDescs map[string]string, argDescs []argDesc) *cmdInfo {
	info := &cmdInfo{
		name:      name,
		shortHelp: shortHelp,
		longHelp:  longHelp,
		builder:   builder,
		optDescs:  optDescs,
		argDescs:  argDescs,
	}
	debugCommands = append(debugCommands, info)
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
		// decode the first rune instead of converting all of desc into []rune
		r, _ := utf8.DecodeRuneInString(desc)
		// note IsLower != !IsUpper for runes with no upper/lower.
		// Also note that login.u.c. is the only exception we're allowing for
		// now, but the list of exceptions could grow -- if it does, we might
		// want to change it to check for urlish things instead of just
		// login.u.c.
		if unicode.IsLower(r) && !strings.HasPrefix(desc, "login.ubuntu.com") {
			noticef("description of %s's %q is lowercase: %q", cmdName, optName, desc)
		}
	}
}

func lintArg(cmdName, optName, desc, origDesc string) {
	lintDesc(cmdName, optName, desc, origDesc)
	if optName[0] != '<' || optName[len(optName)-1] != '>' {
		noticef("argument %q's %q should be wrapped in <>s", cmdName, optName)
	}
}

// Parser creates and populates a fresh parser.
// Since commands have local state a fresh parser is required to isolate tests
// from each other.
func Parser() *flags.Parser {
	optionsData.Version = func() {
		printVersions()
		panic(&exitStatus{0})
	}
	parser := flags.NewParser(&optionsData, flags.PassDoubleDash|flags.PassAfterNonOption)
	parser.ShortDescription = i18n.G("Tool to interact with snaps")
	parser.LongDescription = i18n.G(`
Install, configure, refresh and remove snap packages. Snaps are
'universal' packages that work across many different Linux systems,
enabling secure distribution of the latest apps and utilities for
cloud, servers, desktops and the internet of things.

This is the CLI for snapd, a background service that takes care of
snaps on the system. Start with 'snap list' to see installed snaps.`)
	// hide the unhelpful "[OPTIONS]" from help output
	parser.Usage = ""
	if version := parser.FindOptionByLongName("version"); version != nil {
		version.Description = i18n.G("Print the version and exit")
		version.Hidden = true
	}
	// add --help like what go-flags would do for us, but hidden
	addHelp(parser)

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
		if c.alias != "" {
			cmd.Aliases = append(cmd.Aliases, c.alias)
		}

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
	// Add the debug command
	debugCommand, err := parser.AddCommand("debug", shortDebugHelp, longDebugHelp, &cmdDebug{})
	debugCommand.Hidden = true
	if err != nil {
		logger.Panicf("cannot add command %q: %v", "debug", err)
	}
	// Add all the sub-commands of the debug command
	for _, c := range debugCommands {
		cmd, err := debugCommand.AddCommand(c.name, c.shortHelp, strings.TrimSpace(c.longHelp), c.builder())
		if err != nil {
			logger.Panicf("cannot add debug command %q: %v", c.name, err)
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
	return parser
}

var isStdinTTY = terminal.IsTerminal(0)

// ClientConfig is the configuration of the Client used by all commands.
var ClientConfig = client.Config{
	// we need the powerful snapd socket
	Socket: dirs.SnapdSocket,
	// Allow interactivity if we have a terminal
	Interactive: isStdinTTY,
}

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

func resolveApp(snapApp string) (string, error) {
	target, err := os.Readlink(filepath.Join(dirs.SnapBinariesDir, snapApp))
	if err != nil {
		return "", err
	}
	if filepath.Base(target) == target { // alias pointing to an app command in /snap/bin
		return target, nil
	}
	return snapApp, nil
}

func main() {
	cmd.ExecInSnapdOrCoreSnap()

	// check for magic symlink to /usr/bin/snap:
	// 1. symlink from command-not-found to /usr/bin/snap: run c-n-f
	if os.Args[0] == filepath.Join(dirs.GlobalRootDir, "/usr/lib/command-not-found") {
		cmd := &cmdAdviseSnap{
			Command: true,
			Format:  "pretty",
		}
		// the bash.bashrc handler runs:
		//    /usr/lib/command-not-found -- "$1"
		// so skip over any "--"
		for _, arg := range os.Args[1:] {
			if arg != "--" {
				cmd.Positionals.CommandOrPkg = arg
				break
			}
		}
		if err := cmd.Execute(nil); err != nil {
			fmt.Fprintln(Stderr, err)
		}
		return
	}

	// 2. symlink from /snap/bin/$foo to /usr/bin/snap: run snapApp
	snapApp := filepath.Base(os.Args[0])
	if osutil.IsSymlink(filepath.Join(dirs.SnapBinariesDir, snapApp)) {
		var err error
		snapApp, err = resolveApp(snapApp)
		if err != nil {
			fmt.Fprintf(Stderr, i18n.G("cannot resolve snap app %q: %v"), snapApp, err)
			os.Exit(46)
		}
		cmd := &cmdRun{}
		args := []string{snapApp}
		args = append(args, os.Args[1:]...)
		// this will call syscall.Exec() so it does not return
		// *unless* there is an error, i.e. we setup a wrong
		// symlink (or syscall.Exec() fails for strange reasons)
		err = cmd.Execute(args)
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
		fmt.Fprintf(Stderr, errorPrefix, err)
		os.Exit(1)
	}
}

type exitStatus struct {
	code int
}

func (e *exitStatus) Error() string {
	return fmt.Sprintf("internal error: exitStatus{%d} being handled as normal error", e.code)
}

var wrongDashes = string([]rune{
	0x2010, // hyphen
	0x2011, // non-breaking hyphen
	0x2012, // figure dash
	0x2013, // en dash
	0x2014, // em dash
	0x2015, // horizontal bar
	0xfe58, // small em dash
	0x2015, // figure dash
	0x2e3a, // two-em dash
	0x2e3b, // three-em dash
})

func run() error {
	parser := Parser()
	_, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); ok {
			if e.Type == flags.ErrHelp || e.Type == flags.ErrCommandRequired {
				if parser.Command.Active != nil && parser.Command.Active.Name == "help" {
					parser.Command.Active = nil
				}
				parser.WriteHelp(Stdout)
				return nil
			}
			if e.Type == flags.ErrUnknownCommand {
				return fmt.Errorf(i18n.G(`unknown command %q, see 'snap help'`), os.Args[1])
			}
		}

		msg, err := errorToCmdMessage("", err, nil)

		if cmdline := strings.Join(os.Args, " "); strings.ContainsAny(cmdline, wrongDashes) {
			// TRANSLATORS: the %+q is the commandline (+q means quoted, with any non-ascii character called out). Please keep the lines to at most 80 characters.
			fmt.Fprintf(Stderr, i18n.G(`Your command included some characters that look like dashes but are not:
    %+q
in some situations you might find that when copying from an online source such
as a blog you need to replace “typographic” dashes and quotes with their ASCII
equivalent.  Dashes in particular are homoglyphs on most terminals and in most
fixed-width fonts, so it can be hard to tell.

`), cmdline)
		}

		if err != nil {
			return err
		}

		fmt.Fprintln(Stderr, msg)
	}

	return nil
}
