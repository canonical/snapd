// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

type cmdRecovery struct {
	clientMixin
	colorMixin
}

var shortRecoveryHelp = i18n.G("List available recovery systems")
var longRecoveryHelp = i18n.G(`
The recovery command lists the available recovery systems.
`)

func init() {
	addCommand("recovery", shortRecoveryHelp, longRecoveryHelp, func() flags.Commander {
		// XXX: if we want more/nicer details we can add `snap recovery <system>` later
		return &cmdRecovery{}
	}, nil, nil)
}

func notesForSystem(sys *client.System) string {
	if sys.Current {
		return "current"
	}
	return "-"
}

func (x *cmdRecovery) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	systems, err := x.client.ListSystems()
	if err != nil {
		return err
	}
	if len(systems) == 0 {
		fmt.Fprintf(Stderr, i18n.G("No recovery systems available.\n"))
		return nil
	}

	esc := x.getEscapes()
	w := tabWriter()
	defer w.Flush()
	fmt.Fprintf(w, i18n.G("Label\tBrand\tModel\tNotes\n"))
	for _, sys := range systems {
		// doing it this way because otherwise it's a sea of %s\t%s\t%s
		line := []string{
			sys.Label,
			shortPublisher(esc, &sys.Brand),
			sys.Model.Model,
			notesForSystem(&sys),
		}
		fmt.Fprintln(w, strings.Join(line, "\t"))
	}

	return nil
}
