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
	"strings"

	"github.com/peterh/liner"

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

// for testing
var stdout io.Writer = os.Stdout

type cmdConsole struct {
	repl          *liner.State
	extraCommands []consoleCommand
}

func init() {
	_, err := parser.AddCommand("console",
		i18n.G("Run snappy console interface"),
		i18n.G("Run snappy console interface"),
		&cmdConsole{})
	if err != nil {
		logger.Panicf("Unable to console: %v", err)
	}
}

func (x *cmdConsole) Execute(args []string) error {
	return x.doConsole()
}

type consoleCommand struct {
	name string
	fn   func(line string) error
}

func (x *cmdConsole) snappyCompleter(line string) (c []string) {
	// FIXME: add smartz and also complete arguments of
	//        commands
	for _, cmd := range parser.Commands() {
		if strings.HasPrefix(cmd.Name, strings.ToLower(line)) {
			c = append(c, cmd.Name)
		}
	}
	for _, cmd := range x.extraCommands {
		if strings.HasPrefix(cmd.name, line) {
			c = append(c, cmd.name)
		}
	}

	return c
}

func (x *cmdConsole) initConsole() error {
	// FIXME: add history (ReadHistory/WriteHistory)

	x.extraCommands = []consoleCommand{
		{"help", x.doHelp},
		{"shell", x.doShell},
	}

	x.repl = liner.NewLiner()
	x.repl.SetCompleter(x.snappyCompleter)

	return nil
}

func (x *cmdConsole) CloseConsole() {
	x.repl.Close()
}

func (x *cmdConsole) PrintWelcomeMessage() {
	fmt.Println("Welcome to the snappy console")
	fmt.Println("Type 'help' for help")
	fmt.Println("Type 'shell' for entering a shell")
}

func (x *cmdConsole) doShell(line string) error {
	// restore terminal for the shell
	x.CloseConsole()
	defer x.initConsole()

	l := strings.Split(line, " ")
	shellType := ""
	if len(l) > 1 {
		shellType = l[1]
	}
	cmd := cmdShell{}
	cmd.Positional.ShellType = shellType
	return cmd.Execute([]string{})
}

func (x *cmdConsole) doHelp(line string) error {
	line = strings.TrimPrefix(line, "help")
	line = strings.TrimSpace(line)
	parser.Active = nil
	// find subcmd
	for _, cmd := range parser.Commands() {
		if strings.HasPrefix(line, cmd.Name) {
			parser.Active = cmd
			break
		}
	}
	parser.WriteHelp(stdout)

	return nil
}

func (x *cmdConsole) doConsole() error {
	x.initConsole()
	defer x.CloseConsole()
	x.PrintWelcomeMessage()

outer:
	for {
		line, err := x.repl.Prompt("> ")
		if err != nil {
			return err
		}
		x.repl.AppendHistory(line)

		for _, cmd := range x.extraCommands {
			if strings.HasPrefix(line, cmd.name) {
				if err := cmd.fn(line); err != nil {
					fmt.Println(err)
				}
				continue outer
			}
		}

		if _, err = parser.ParseArgs(strings.Fields(line)); err != nil {
			fmt.Println(err)
		}

	}
}
