// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

var shortConfinementHelp = i18n.G("Print the confinement mode the system operates in")
var longConfinementHelp = i18n.G(`
The confinement command will print the confinement mode (strict,
partial or none) the system operates in.
`)

type cmdConfinement struct {
	clientMixin
}

func init() {
	addDebugCommand("confinement", shortConfinementHelp, longConfinementHelp, func() flags.Commander {
		return &cmdConfinement{}
	}, nil, nil)
}

func (cmd cmdConfinement) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	sysInfo, err := cmd.client.SysInfo()
	if err != nil {
		return err
	}
	fmt.Fprintf(Stdout, "%s\n", sysInfo.Confinement)
	return nil
}
