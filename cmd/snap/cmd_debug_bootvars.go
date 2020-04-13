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
	"github.com/snapcore/snapd/i18n"
)

type cmdBootvars struct {
	UC20 bool   `long:"uc20"`
	Dir  string `long:"dir"`
}

func init() {
	cmd := addDebugCommand("boot-vars",
		"(internal) obtain the snapd boot variables",
		"(internal) obtain the snapd boot variables",
		func() flags.Commander {
			return &cmdBootvars{}
		}, map[string]string{
			"uc20": i18n.G("Whether to use uc20 boot vars or not"),
			"dir":  i18n.G("What directory to look for boot variables in"),
		}, nil)
	cmd.hidden = true
}

func (x *cmdBootvars) Execute(args []string) error {
	return boot.DumpBootVars(Stdout, x.Dir, x.UC20)
}
