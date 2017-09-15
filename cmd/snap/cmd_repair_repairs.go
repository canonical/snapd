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
	"os"
	"os/exec"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/release"
)

func runSnapRepair(cmdStr string, args []string) error {
	snapRepairPath := filepath.Join(dirs.DistroLibExecDir, "snap-repair")
	args = append([]string{cmdStr}, args...)
	cmd := exec.Command(snapRepairPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

type cmdShowRepair struct{}

var shortRepairHelp = i18n.G("Shows a specific repair")
var longRepairHelp = i18n.G(`
The repair command shows the details about a specific repair
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
	return runSnapRepair("show", args)
}

type cmdListRepairs struct{}

var shortRepairsHelp = i18n.G("Lists all repairs")
var longRepairsHelp = i18n.G(`
The repairs command lists all repairs for the given device.
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
