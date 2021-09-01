// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

// Package ctlcmd contains the various snapctl subcommands.
package ctlcmd

import (
	"bytes"
	"fmt"
	"io"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/strutil"
)

type MissingContextError struct {
	subcommand string
}

func (e *MissingContextError) Error() string {
	return fmt.Sprintf(`cannot invoke snapctl operation commands (here %q) from outside of a snap`, e.subcommand)
}

type baseCommand struct {
	stdout io.Writer
	stderr io.Writer
	c      *hookstate.Context
	name   string
}

func (c *baseCommand) setName(name string) {
	c.name = name
}

func (c *baseCommand) setStdout(w io.Writer) {
	c.stdout = w
}

func (c *baseCommand) printf(format string, a ...interface{}) {
	if c.stdout != nil {
		fmt.Fprintf(c.stdout, format, a...)
	}
}

func (c *baseCommand) setStderr(w io.Writer) {
	c.stderr = w
}

func (c *baseCommand) errorf(format string, a ...interface{}) {
	if c.stderr != nil {
		fmt.Fprintf(c.stderr, format, a...)
	}
}

func (c *baseCommand) setContext(context *hookstate.Context) {
	c.c = context
}

func (c *baseCommand) context() *hookstate.Context {
	return c.c
}

func (c *baseCommand) ensureContext() (context *hookstate.Context, err error) {
	if c.c == nil {
		err = &MissingContextError{c.name}
	}
	return c.c, err
}

type command interface {
	setName(name string)

	setStdout(w io.Writer)
	setStderr(w io.Writer)

	setContext(context *hookstate.Context)
	context() *hookstate.Context

	Execute(args []string) error
}

type commandInfo struct {
	shortHelp string
	longHelp  string
	generator func() command
	hidden    bool
}

var commands = make(map[string]*commandInfo)

func addCommand(name, shortHelp, longHelp string, generator func() command) *commandInfo {
	cmd := &commandInfo{
		shortHelp: shortHelp,
		longHelp:  longHelp,
		generator: generator,
	}
	commands[name] = cmd
	return cmd
}

// UnsuccessfulError carries a specific exit code to be returned to the client.
type UnsuccessfulError struct {
	ExitCode int
}

func (e UnsuccessfulError) Error() string {
	return fmt.Sprintf("unsuccessful with exit code: %d", e.ExitCode)
}

// ForbiddenCommandError conveys that a command cannot be invoked in some context
type ForbiddenCommandError struct {
	Message string
}

func (f ForbiddenCommandError) Error() string {
	return f.Message
}

// nonRootAllowed lists the commands that can be performed even when snapctl
// is invoked not by root.
var nonRootAllowed = []string{"get", "services", "set-health", "is-connected", "system-mode"}

// Run runs the requested command.
func Run(context *hookstate.Context, args []string, uid uint32) (stdout, stderr []byte, err error) {
	if len(args) == 0 {
		return nil, nil, fmt.Errorf("internal error: snapctl cannot run without args")
	}

	if !isAllowedToRun(uid, args) {
		return nil, nil, &ForbiddenCommandError{Message: fmt.Sprintf("cannot use %q with uid %d, try with sudo", args[0], uid)}
	}

	parser := flags.NewNamedParser("snapctl", flags.PassDoubleDash|flags.HelpFlag)

	// Create stdout/stderr buffers, and make sure commands use them.
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	for name, cmdInfo := range commands {
		cmd := cmdInfo.generator()
		cmd.setName(name)
		cmd.setStdout(&stdoutBuffer)
		cmd.setStderr(&stderrBuffer)
		cmd.setContext(context)

		theCmd, err := parser.AddCommand(name, cmdInfo.shortHelp, cmdInfo.longHelp, cmd)
		theCmd.Hidden = cmdInfo.hidden
		if err != nil {
			logger.Panicf("cannot add command %q: %s", name, err)
		}
	}

	_, err = parser.ParseArgs(args)
	return stdoutBuffer.Bytes(), stderrBuffer.Bytes(), err
}

func isAllowedToRun(uid uint32, args []string) bool {
	// A command can run if any of the following are true:
	//	* It runs as root
	//	* It's contained in nonRootAllowed
	//	* It's used with the -h or --help flags
	// note: commands still need valid context and snaps can only access own config.
	return uid == 0 ||
		strutil.ListContains(nonRootAllowed, args[0]) ||
		strutil.ListContains(args, "-h") ||
		strutil.ListContains(args, "--help")
}
