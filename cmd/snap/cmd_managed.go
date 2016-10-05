// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

var shortIsManagedHelp = i18n.G("Exits zero on managed system")
var longIsManagedHelp = i18n.G(`
The managed command will exit with a zero status if snapd has registered users.
`)

type cmdIsManaged struct {
	Quiet bool `short:"q"`
}

func init() {
	cmd := addCommand("managed", shortIsManagedHelp, longIsManagedHelp, func() flags.Commander { return &cmdIsManaged{} },
		map[string]string{
			"q": "No output unless there are errors",
		}, nil)
	cmd.hidden = true
}

func (cmd cmdIsManaged) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	sysinfo, err := Client().SysInfo()
	if err != nil {
		return err
	}

	status := 1
	if sysinfo.Managed {
		status = 0
	}

	if !cmd.Quiet {
		if sysinfo.Managed {
			fmt.Fprintln(Stdout, "system is managed")
		} else {
			fmt.Fprintln(Stdout, "system is not managed")
		}
	}

	panic(&exitStatus{status})
}
