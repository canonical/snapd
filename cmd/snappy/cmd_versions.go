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

	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/logger"
)

type cmdVersions struct {
}

var shortVersionsHelp = i18n.G("(deprecated) please use \"list\"")

var longVersionsHelp = i18n.G("This command is no longer available, please use the \"list\" command")

func init() {
	_, err := parser.AddCommand("versions",
		shortVersionsHelp,
		longVersionsHelp,
		&cmdVersions{})
	if err != nil {
		logger.Panicf("Unable to versions: %v", err)
	}
}

func (x *cmdVersions) Execute(args []string) error {
	fmt.Println(i18n.G(`The "versions" command is no longer available.

Please use the "list" command instead to see what is installed.
The "list -u" (or "list --updates") will show you the available updates
and "list -v" (or "list --verbose") will show all installed versions.
`))

	return nil
}
