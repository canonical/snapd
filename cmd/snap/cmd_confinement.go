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
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/release"

	"fmt"

	"github.com/jessevdk/go-flags"
)

var shortConfinementHelp = i18n.G("Prints the confinement mode the system operates in")
var longConfinementHelp = i18n.G(`
The confinement command will print the confinement mode (strict, partial or none)
the system operates in.
`)

type cmdConfinement struct{}

func init() {
	addDebugCommand("confinement", shortConfinementHelp, longConfinementHelp, func() flags.Commander { return &cmdConfinement{} })
}

func (cmd cmdConfinement) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	// NOTE: Right now we don't have a good way to differentiate if we
	// only have partial confinement (ala AppArmor disabled and Seccomp
	// enabled) or no confinement at all. Once we have a better system
	// in place how we can dynamically retrieve these information from
	// snapd we will use this here.
	if !release.ReleaseInfo.ForceDevMode() {
		fmt.Printf("strict\n")
	} else {
		fmt.Printf("none\n")
	}

	return nil
}
