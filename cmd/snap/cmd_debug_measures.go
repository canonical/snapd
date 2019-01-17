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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
)

var shortMeasuresHelp = i18n.G("Print sandbox features available on the system")
var longMeasuresHelp = i18n.G(`
The sandbox command prints tags describing features of individual sandbox
components used by snapd on a given system.
`)

type cmdMeasures struct {
	clientMixin
}

func init() {
	addDebugCommand("measures", shortMeasuresHelp, longMeasuresHelp, func() flags.Commander {
		return &cmdMeasures{}
	}, nil, nil)
}

func (cmd cmdMeasures) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	var resp struct {
		Measures string `json:"measures"`
	}
	if err := cmd.client.Debug("get-measures", nil, &resp); err != nil {
		return err
	}
	fmt.Fprintln(Stdout, "Measures:")
	fmt.Fprintf(Stdout, "%s\n", resp.Measures)
	return nil
}
