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

// Package tools contains the various snapctl subcommands.
package tools

import (
	"bytes"
	"fmt"
	"io"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/hookstate"

	"github.com/jessevdk/go-flags"
)

type baseCommand struct {
	stdout  io.Writer
	stderr  io.Writer
	handler hookstate.Handler
}

func (c *baseCommand) setStdout(w io.Writer) {
	c.stdout = w
}

func (c *baseCommand) writeStdout(format string, a ...interface{}) {
	c.stdout.Write([]byte(fmt.Sprintf(format, a...)))
}

func (c *baseCommand) setStderr(w io.Writer) {
	c.stderr = w
}

func (c *baseCommand) writeStderr(format string, a ...interface{}) {
	c.stderr.Write([]byte(fmt.Sprintf(format, a...)))
}

func (c *baseCommand) setHandler(handler hookstate.Handler) {
	c.handler = handler
}

func (c *baseCommand) getHandler() hookstate.Handler {
	return c.handler
}

type toolCommand interface {
	setStdout(w io.Writer)
	setStderr(w io.Writer)
	setHandler(handler hookstate.Handler)
	getHandler() hookstate.Handler

	Execute(args []string) error
}

var commands map[string]toolCommand

func addCommand(name string, command toolCommand) {
	if commands == nil {
		commands = make(map[string]toolCommand)
	}
	commands[name] = command
}

func parser() *flags.Parser {
	parser := flags.NewParser(nil, flags.PassDoubleDash|flags.PassAfterNonOption)

	for name, command := range commands {
		_, err := parser.AddCommand(name, "", "", command)
		if err != nil {
			logger.Panicf("cannot add command %q: %s", name, err)
		}
	}

	return parser
}

// RunCommand runs the requested command.
func RunCommand(handler hookstate.Handler, args []string) (stdout, stderr []byte, err error) {
	parser := parser()

	// Create stdout/stderr buffers, and make sure commands use them.
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer

	// We override the CommandHandler here to inject our stdout/stderr as well
	// as the handler.
	parser.CommandHandler = func(command flags.Commander, args []string) error {
		if command == nil {
			return fmt.Errorf("no command to handle arguments")
		}

		if cmd, ok := command.(toolCommand); ok {
			cmd.setStdout(&stdoutBuffer)
			cmd.setStderr(&stderrBuffer)
			cmd.setHandler(handler)
			return cmd.Execute(args)
		}

		return fmt.Errorf("tool is not of expected type")
	}

	_, err = parser.ParseArgs(args)
	return stdoutBuffer.Bytes(), stderrBuffer.Bytes(), err
}
