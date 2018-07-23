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
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/release"
)

func runSnapRepair(cmdStr string, args []string) error {
	// do not even try to run snap-repair on classic, some distros
	// may not even package it
	if release.OnClassic {
		return fmt.Errorf(i18n.G("repairs are not available on a classic system"))
	}

	snapRepairPath := filepath.Join(dirs.GlobalRootDir, dirs.CoreLibExecDir, "snap-repair")
	args = append([]string{cmdStr}, args...)
	cmd := exec.Command(snapRepairPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type cmdShowRepair struct {
	Positional struct {
		Repair []string `positional-arg-name:"<repair>"`
	} `positional-args:"yes"`
}

var shortRepairHelp = i18n.G("Show specific repairs")
var longRepairHelp = i18n.G(`
The repair command shows the details about one or multiple repairs.
`)

func init() {
	cmd := addCommand("repair", shortRepairHelp, longRepairHelp, func() flags.Commander {
		return &cmdShowRepair{}
	}, nil, nil)
	if release.OnClassic {
		cmd.hidden = true
	}
}

func (x *cmdShowRepair) Execute(args []string) error {
	return runSnapRepair("show", x.Positional.Repair)
}

type cmdListRepairs struct{}

var shortRepairsHelp = i18n.G("Lists all repairs")
var longRepairsHelp = i18n.G(`
The repairs command lists all processed repairs for this device.
`)

func init() {
	cmd := addCommand("repairs", shortRepairsHelp, longRepairsHelp, func() flags.Commander {
		return &cmdListRepairs{}
	}, nil, nil)
	if release.OnClassic {
		cmd.hidden = true
	}
}

func (x *cmdListRepairs) Execute(args []string) error {
	return runSnapRepair("list", args)
}
