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
	"os"
	"os/exec"
	"strings"

	"github.com/peterh/liner"

	"launchpad.net/snappy/logger"
)

type cmdConsole struct {
	repl *liner.State
}

func init() {
	_, err := parser.AddCommand("console",
		"Run snappy console interface",
		"Run snappy console interface",
		&cmdConsole{})
	if err != nil {
		logger.Panicf("Unable to console: %v", err)
	}
}

func (x *cmdConsole) Execute(args []string) error {
	return x.doConsole()
}

func (x *cmdConsole) InitConsole() error {
	// FIXME: add history (ReadHistory/WriteHistory)

	x.repl = liner.NewLiner()
	x.repl.SetCompleter(func(line string) (c []string) {
		// FIXME: add smartz and also complete arguments of
		//        commands
		for _, cmd := range parser.Commands() {
			if strings.HasPrefix(cmd.Name, strings.ToLower(line)) {
				c = append(c, cmd.Name)
			}
		}
		// FIXME: generalize the extra commands
		if strings.HasPrefix("help", line) {
			c = append(c, "help")
		}
		if strings.HasPrefix("shell", line) {
			c = append(c, "shell")
		}

		return c
	})

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

func (x *cmdConsole) doShell() error {
	// restore terminal for the shell
	x.CloseConsole()
	defer x.InitConsole()

	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/sh"
	}
	cmd := exec.Command(sh)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}

	return nil
}

func (x *cmdConsole) doHelp() error {
	// FIXME: support "help subcommand" by
	//        just finding subcommand in parser
	//        and setting it to "Active"
	parser.Active = nil
	parser.WriteHelp(os.Stdout)

	return nil
}

func (x *cmdConsole) doConsole() error {
	x.InitConsole()
	defer x.CloseConsole()
	x.PrintWelcomeMessage()

	for {
		line, err := x.repl.Prompt("> ")
		if err != nil {
			return err
		}

		switch {
		case strings.HasPrefix(line, "help"):
			x.doHelp()
		case strings.HasPrefix(line, "shell"):
			x.doShell()
		default:
			// do it
			_, err = parser.ParseArgs(strings.Fields(line))
			if err != nil {
				fmt.Println(err)
			}
		}

		x.repl.AppendHistory(line)
	}
}
