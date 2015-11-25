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

	"github.com/ubuntu-core/snappy/classic"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

type cmdShell struct {
	Positional struct {
		ShellType string `positional-arg-name:"shell-type"`
	} `positional-args:"yes"`
}

func init() {
	arg, err := parser.AddCommand("shell",
		"Run snappy shell interface",
		"Run snappy shell interface",
		&cmdShell{})
	if err != nil {
		logger.Panicf("Unable to : %q", err)
	}
	addOptionDescription(arg, "shell-type", i18n.G("The type of shell you want"))
}

func (x *cmdShell) Execute(args []string) error {
	shellType := x.Positional.ShellType
	if shellType == "classic" {
		if !classic.Enabled() {
			return fmt.Errorf(i18n.G(`Classic dimension disabled on this system.
Use "sudo snappy enable-classic" to enable it.`))
		}

		fmt.Println(i18n.G(`All background processes will be killed when you leave this shell`))
		return classic.Run()
	}

	return fmt.Errorf("unsupported shell %v", shellType)
}
