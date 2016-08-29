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

// Package ctlcmd contains the various snapctl subcommands.
package ctlcmd

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
	context *hookstate.Context
}

func (c *baseCommand) setStdout(w io.Writer) {
	c.stdout = w
}

func (c *baseCommand) printf(format string, a ...interface{}) {
	if c.stdout != nil {
		c.stdout.Write([]byte(fmt.Sprintf(format, a...)))
	}
}

func (c *baseCommand) setStderr(w io.Writer) {
	c.stderr = w
}

func (c *baseCommand) errorf(format string, a ...interface{}) {
	if c.stderr != nil {
		c.stderr.Write([]byte(fmt.Sprintf(format, a...)))
	}
}

func (c *baseCommand) setContext(context *hookstate.Context) {
	c.context = context
}

func (c *baseCommand) getContext() *hookstate.Context {
	return c.context
}

type ctlCommand interface {
	setStdout(w io.Writer)
	setStderr(w io.Writer)
	setContext(context *hookstate.Context)
	getContext() *hookstate.Context

	Execute(args []string) error
}

type commandGenerator func() ctlCommand

var commandGenerators map[string]commandGenerator

func addCommand(name string, generator commandGenerator) {
	if commandGenerators == nil {
		commandGenerators = make(map[string]commandGenerator)
	}
	commandGenerators[name] = generator
}

// Run runs the requested command.
func Run(context *hookstate.Context, args []string) (stdout, stderr []byte, err error) {
	parser := flags.NewParser(nil, flags.PassDoubleDash)

	// Create stdout/stderr buffers, and make sure commands use them.
	var stdoutBuffer bytes.Buffer
	var stderrBuffer bytes.Buffer
	for name, generator := range commandGenerators {
		command := generator()
		command.setStdout(&stdoutBuffer)
		command.setStderr(&stderrBuffer)
		command.setContext(context)

		_, err = parser.AddCommand(name, "", "", command)
		if err != nil {
			logger.Panicf("cannot add command %q: %s", name, err)
		}
	}

	_, err = parser.ParseArgs(args)
	return stdoutBuffer.Bytes(), stderrBuffer.Bytes(), err
}
