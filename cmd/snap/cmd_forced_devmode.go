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

	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

var shortForcedDevmodeHelp = i18n.G("Prints whether system is in forced devmode")
var longForcedDevmodeHelp = i18n.G(`
The forced-devmode command will print true or false informing whether
snapd is running in forced devmode.
`)

type cmdForcedDevmode struct{}

func init() {
	cmd := addCommand("forced-devmode", shortForcedDevmodeHelp, longForcedDevmodeHelp, func() flags.Commander { return &cmdIsManaged{} }, nil, nil)
	cmd.hidden = true
}

func (cmd cmdForcedDevmode) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	sysinfo, err := Client().SysInfo()
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, "%v\n", sysinfo.ForcedDevMode)

	if !sysinfo.ForcedDevMode {
		return fmt.Errorf("Forced devmode is not enabled")
	}
	return nil
}
