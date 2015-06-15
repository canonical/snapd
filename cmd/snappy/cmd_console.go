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
	"strings"

	"github.com/peterh/liner"

	"launchpad.net/snappy/logger"
)

type cmdConsole struct {
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

func (x *cmdConsole) doConsole() error {
	repl := liner.NewLiner()
	defer repl.Close()

	// FIXME: add history (ReadHistory/WriteHistory)

	repl.SetCompleter(func(line string) (c []string) {
		// FIXME: add smartz and also complete arguments of
		//        commands
		for _, cmd := range parser.Commands() {
			if strings.HasPrefix(cmd.Name, strings.ToLower(line)) {
				c = append(c, cmd.Name)
			}
		}
		// FIXME: meh
		if strings.HasPrefix("help", line) {
			c = append(c, "help")
		}

		return c
	})

	fmt.Println("Welcome to the snappy console")
	fmt.Println("Type 'help' for help")
	for {
		if line, err := repl.Prompt("> "); err != nil {
			return err
		} else {
			// FIXME: generalize
			if strings.HasPrefix(line, "help") {
				// FIXME: support "help subcommand" by
				//        just finding subcommand in parser
				//        and setting it to "Active"
				parser.Active = nil
				parser.WriteHelp(os.Stdout)
				continue
			}

			// do it
			_, err := parser.ParseArgs(strings.Fields(line))
			if err != nil {
				fmt.Println(err)
			}
			repl.AppendHistory(line)
		}
	}

	return nil
}
