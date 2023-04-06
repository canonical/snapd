// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"errors"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/release"
)

type cmdBootvarsGet struct {
	UC20    bool   `long:"uc20"`
	RootDir string `long:"root-dir"`
}

type cmdBootvarsSet struct {
	RootDir    string `long:"root-dir"`
	Recovery   bool   `long:"recovery"`
	Positional struct {
		VarEqValue []string `positional-arg-name:"<var-eq-value>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	cmdGet := addDebugCommand("boot-vars",
		"(internal) obtain the snapd boot variables",
		"(internal) obtain the snapd boot variables",
		func() flags.Commander {
			return &cmdBootvarsGet{}
		}, map[string]string{
			"uc20":     i18n.G("Whether to use UC20+ boot vars or not"),
			"root-dir": i18n.G("Root directory to look for boot variables in"),
		}, nil)

	cmdSet := addDebugCommand("set-boot-vars",
		"(internal) set snapd boot variables",
		"(internal) set snapd boot variables",
		func() flags.Commander {
			return &cmdBootvarsSet{}
		}, map[string]string{
			"root-dir": i18n.G("Root directory to look for boot variables in (implies UC20+)"),
			"recovery": i18n.G("Manipulate the recovery bootloader (implies UC20+)"),
		}, nil)

	if release.OnClassic {
		cmdGet.hidden = true
		cmdSet.hidden = true
	}
}

func (x *cmdBootvarsGet) Execute(args []string) error {
	if release.OnClassic {
		return errors.New(`the "boot-vars" command is not available on classic systems`)
	}
	return boot.DebugDumpBootVars(Stdout, x.RootDir, x.UC20)
}

func (x *cmdBootvarsSet) Execute(args []string) error {
	if release.OnClassic {
		return errors.New(`the "boot-vars" command is not available on classic systems`)
	}
	return boot.DebugSetBootVars(x.RootDir, x.Recovery, x.Positional.VarEqValue)
}
