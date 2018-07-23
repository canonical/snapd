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

	"github.com/jessevdk/go-flags"
)

type cmdInternalFirstBoot struct{}

func init() {
	cmd := addCommand("firstboot",
		"Internal",
		"The firstboot command is only retained for backwards compatibility.",
		func() flags.Commander {
			return &cmdInternalFirstBoot{}
		}, nil, nil)
	cmd.hidden = true
}

// WARNING: do not remove this command, older systems may still have
//          a systemd snapd.firstboot.service job in /etc/systemd/system
//          that we did not cleanup. so we need this dummy command or
//          those units will start failing.
func (x *cmdInternalFirstBoot) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}
	fmt.Fprintf(Stderr, "firstboot command is deprecated\n")
	return nil
}
