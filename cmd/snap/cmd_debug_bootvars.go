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
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
)

type cmdBootvars struct{}

func init() {
	cmd := addDebugCommand("boot-vars",
		"(internal) obtain the snapd boot variables",
		"(internal) obtain the snapd boot variables",
		func() flags.Commander {
			return &cmdBootvars{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdBootvars) Execute(args []string) error {
	return boot.DumpBootVars(Stdout)
}
