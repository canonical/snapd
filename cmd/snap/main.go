// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2023 Canonical Ltd
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
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jessevdk/go-flags"
	"golang.org/x/crypto/ssh/terminal"
	"golang.org/x/xerrors"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/squashfs"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/snapdtool"
)

func init() {
	// set User-Agent for when 'snap' talks to the store directly (snap download etc...)
	snapdenv.SetUserAgentFromVersion(snapdtool.Version, nil, "snap")

	// plug/slot sanitization not used by snap commands (except for snap
	// pack and snap prepare-iamge, which re-sets it), make it no-op.
	snap.SanitizePlugsSlots = func(snapInfo *snap.Info) {}
}

var (
	// Standard streams, redirected for testing.
	Stdin  io.Reader = os.Stdin
	Stdout io.Writer = os.Stdout
	Stderr io.Writer = os.Stderr
	// overridden for testing
	ReadPassword = terminal.ReadPassword
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
var ErrExtraArgs = errors.New(i18n.G("too many arguments for command"))

// cmdInfo holds information needed to call parser.AddCommand(...).
type cmdInfo struct {
	name, shortHelp, longHelp string
	builder                   func() flags.Commander
	hidden                    bool
	// completeHidden set to true forces completion even of
	// a hidden command
	completeHidden bool
	optDescs       map[string]string
	argDescs       []argDesc
	alias          string
	extra          func(*flags.Command)
}

// commands holds information about all non-debug commands.
var commands []*cmdInfo

// debugCommands holds information about all debug commands.
var debugCommands []*cmdInfo

// routineCommands holds information about all internal commands.
var routineCommands []*cmdInfo

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

// addRoutineCommand replaces parser.addCommand() in a way that is
// compatible with re-constructing a pristine parser. It is meant for
// adding "snap routine" commands.
func addRoutineCommand(name, shortHelp, longHelp string, builder func() flags.Commander, optDescs map[string]string, argDescs []argDesc) *cmdInfo {
	info := &cmdInfo{
		name:      name,
		shortHelp: shortHelp,
		longHelp:  longHelp,
		builder:   builder,
		optDescs:  optDescs,
		argDescs:  argDescs,
	}
	routineCommands = append(routineCommands, info)
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
		if unicode.IsLower(r) && !strings.HasPrefix(desc, "login.ubuntu.com") && !strings.HasPrefix(desc, cmdName) {
			panicOnDebug("description of %s's %q is lowercase in locale %q: %q", cmdName, optName, i18n.CurrentLocale(), desc)
		}
	}
}

func lintArg(cmdName, optName, desc, origDesc string) {
	lintDesc(cmdName, optName, desc, origDesc)
	if len(optName) > 0 && optName[0] == '<' && optName[len(optName)-1] == '>' {
		return
	}
	if len(optName) > 0 && optName[0] == '<' && strings.HasSuffix(optName, ">s") {
		// see comment in fixupArg about the >s case
		return
	}
	panicOnDebug("argument %q's %q should begin with < and end with >", cmdName, optName)
}

func fixupArg(optName string) string {
	// Due to misunderstanding some localized versions of option name are
	// literally "<option>s" instead of "<option>". While translators can
	// improve this over time we can be smarter and avoid silly messages
	// logged whenever "snap" command is used.
	//
	// See: https://bugs.launchpad.net/snapd/+bug/1806761
	if strings.HasSuffix(optName, ">s") {
		return optName[:len(optName)-1]
	}
	return optName
}

type clientSetter interface {
	setClient(*client.Client)
}

type clientMixin struct {
	client *client.Client
}

func (ch *clientMixin) setClient(cli *client.Client) {
	ch.client = cli
}

func firstNonOptionIsRun() bool {
	if len(os.Args) < 2 {
		return false
	}
	for _, arg := range os.Args[1:] {
		if len(arg) == 0 || arg[0] == '-' {
			continue
		}
		return arg == "run"
	}
	return false
}

// noCompletion marks command descriptions of commands that should not
// be completed
var noCompletion = make(map[string]bool)

func markForNoCompletion(ci *cmdInfo) {
	if ci.hidden && !ci.completeHidden {
		if ci.shortHelp == "" {
			logger.Panicf("%q missing short help", ci.name)
		}
		noCompletion[ci.shortHelp] = true
	}
}

// completionHandler filters out unwanted completions based on
// the noCompletion map before dumping them to stdout.
func completionHandler(comps []flags.Completion) {
	for _, comp := range comps {
		if noCompletion[comp.Description] {
			continue
		}
		fmt.Fprintln(Stdout, comp.Item)
	}
}

func registerCommands(cli *client.Client, parser *flags.Parser, baseCmd *flags.Command, commands []*cmdInfo, checkUnique func(*cmdInfo)) {
	for _, c := range commands {
		checkUnique(c)
		markForNoCompletion(c)

		obj := c.builder()
		if x, ok := obj.(clientSetter); ok {
			x.setClient(cli)
		}
		if x, ok := obj.(parserSetter); ok {
			x.setParser(parser)
		}

		cmd, err := baseCmd.AddCommand(c.name, c.shortHelp, strings.TrimSpace(c.longHelp), obj)
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
			name = fixupArg(name)
			arg.Name = name
			arg.Description = desc
		}
		if c.extra != nil {
			c.extra(cmd)
		}
	}
}

// Parser creates and populates a fresh parser.
// Since commands have local state a fresh parser is required to isolate tests
// from each other.
func Parser(cli *client.Client) *flags.Parser {
	optionsData.Version = func() {
		printVersions(cli)
		panic(&exitStatus{0})
	}
	flagopts := flags.Options(flags.PassDoubleDash)
	if firstNonOptionIsRun() {
		flagopts |= flags.PassAfterNonOption
	}
	parser := flags.NewParser(&optionsData, flagopts)
	parser.CompletionHandler = completionHandler
	parser.ShortDescription = i18n.G("Tool to interact with snaps")
	parser.LongDescription = longSnapDescription
	// hide the unhelpful "[OPTIONS]" from help output
	parser.Usage = ""
	if version := parser.FindOptionByLongName("version"); version != nil {
		version.Description = i18n.G("Print the version and exit")
		version.Hidden = true
	}
	// add --help like what go-flags would do for us, but hidden
	addHelp(parser)

	seen := make(map[string]bool, len(commands)+len(debugCommands)+len(routineCommands))
	checkUnique := func(ci *cmdInfo, kind string) {
		if seen[ci.shortHelp] && ci.shortHelp != "Internal" && ci.shortHelp != "Deprecated (hidden)" {
			logger.Panicf(`%scommand %q has an already employed description != "Internal"|"Deprecated (hidden)": %s`, kind, ci.name, ci.shortHelp)
		}
		seen[ci.shortHelp] = true
	}

	// Add all regular commands
	registerCommands(cli, parser, parser.Command, commands, func(ci *cmdInfo) {
		checkUnique(ci, "")
	})
	// Add the debug command
	debugCommand, err := parser.AddCommand("debug", shortDebugHelp, longDebugHelp, &cmdDebug{})
	if err != nil {
		logger.Panicf("cannot add command %q: %v", "debug", err)
	}
	// Add all the sub-commands of the debug command
	registerCommands(cli, parser, debugCommand, debugCommands, func(ci *cmdInfo) {
		checkUnique(ci, "debug ")
	})
	// Add the internal command
	routineCommand, err := parser.AddCommand("routine", shortRoutineHelp, longRoutineHelp, &cmdRoutine{})
	routineCommand.Hidden = true
	if err != nil {
		logger.Panicf("cannot add command %q: %v", "internal", err)
	}
	// Add all the sub-commands of the routine command
	registerCommands(cli, parser, routineCommand, routineCommands, func(ci *cmdInfo) {
		checkUnique(ci, "routine ")
	})
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
// commands should (in general) not use this, and instead use clientMixin.
func mkClient() *client.Client {
	cfg := &ClientConfig
	// Set client user-agent when talking to the snapd daemon to the
	// same value as when talking to the store.
	cfg.UserAgent = snapdenv.UserAgent()

	cli := client.New(cfg)
	goos := runtime.GOOS
	if release.WSLVersion == 1 {
		goos = "Windows Subsystem for Linux 1"
	}
	if goos != "linux" {
		cli.Hijack(func(*http.Request) (*http.Response, error) {
			fmt.Fprintf(Stderr, i18n.G(`Interacting with snapd is not yet supported on %s.
This command has been left available for documentation purposes only.
`), goos)
			os.Exit(1)
			panic("execution continued past call to exit")
		})
	}
	return cli
}

func init() {
	err := logger.SimpleSetup(nil)
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

// exitCodeFromError takes an error and returns specific exit codes
// for some errors. Otherwise the generic exit code 1 is returned.
func exitCodeFromError(err error) int {
	var mksquashfsError squashfs.MksquashfsError
	var cmdlineFlagsError *flags.Error
	var unknownCmdError unknownCommandError

	switch {
	case err == nil:
		return 0
	case client.IsRetryable(err):
		return 10
	case xerrors.As(err, &mksquashfsError):
		return 20
	case xerrors.As(err, &cmdlineFlagsError) || xerrors.As(err, &unknownCmdError):
		// EX_USAGE, see sysexit.h
		return 64
	default:
		return 1
	}
}

func main() {
	snapdtool.ExecInSnapdOrCoreSnap()

	if err := snapdtool.MaybeSetupFIPS(); err != nil {
		fmt.Fprintf(os.Stderr, "cannot check or enable FIPS mode: %v", err)
		os.Exit(1)
	}

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
		cmd.client = mkClient()
		os.Args[0] = snapApp
		// this will call syscall.Exec() so it does not return
		// *unless* there is an error, i.e. we setup a wrong
		// symlink (or syscall.Exec() fails for strange reasons)
		err = cmd.Execute(os.Args)
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
		os.Exit(exitCodeFromError(err))
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

type unknownCommandError struct {
	msg string
}

func (e unknownCommandError) Error() string {
	return e.msg
}

func run() error {
	cli := mkClient()
	parser := Parser(cli)
	xtra, err := parser.Parse()
	if err != nil {
		if e, ok := err.(*flags.Error); ok {
			switch e.Type {
			case flags.ErrCommandRequired:
				printShortHelp()
				return nil
			case flags.ErrHelp:
				parser.WriteHelp(Stdout)
				return nil
			case flags.ErrUnknownCommand:
				sub := os.Args[1]
				sug := "snap help"
				if len(xtra) > 0 {
					sub = xtra[0]
					if x := parser.Command.Active; x != nil && x.Name != "help" {
						sug = "snap help " + x.Name
					}
				}
				// TRANSLATORS: %q is the command the user entered; %s is 'snap help' or 'snap help <cmd>'
				return unknownCommandError{fmt.Sprintf(i18n.G("unknown command %q, see '%s'."), sub, sug)}
			}
		}

		var cmdName string
		if parser.Active != nil {
			cmdName = parser.Active.Name
		}

		msg, err := errorToCmdMessage("", cmdName, err, nil)

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
		return nil
	}

	maybePresentWarnings(cli.WarningsSummary())

	return nil
}

func panicOnDebug(msg string, v ...interface{}) {
	if osutil.GetenvBool("SNAPD_DEBUG") || snapdenv.Testing() {
		logger.Panicf(msg, v...)
	}
}
