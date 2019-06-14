// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

	"github.com/snapcore/snapd/i18n"
)

type consistencyCommand struct {
	baseCommand
}

var shortConsistencyHelp = i18n.G("The consistency command checks consistency of the state and prints a report.")

func init() {
	addCommand("consistency", shortConsistencyHelp, "", func() command {
		return &changesCommand{}
	})
}

func (c *consistencyCommand) Execute(args []string) error {
	return fmt.Errorf("not implemented")
}
