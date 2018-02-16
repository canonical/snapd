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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/snap/pack"
)

type packCmd struct {
	Positional struct {
		SnapDir   string `positional-arg-name:"<snap-dir>"`
		TargetDir string `positional-arg-name:"<target-dir>"`
	} `positional-args:"yes"`
}

var shortPackHelp = i18n.G("Pack the given target dir as a snap")
var longPackHelp = i18n.G(`
The pack command packs the given snap-dir as a snap.`)

func init() {
	addCommand("pack",
		shortPackHelp,
		longPackHelp,
		func() flags.Commander {
			return &packCmd{}
		}, nil, nil)
}

func (x *packCmd) Execute([]string) error {
	if x.Positional.SnapDir == "" {
		x.Positional.SnapDir = "."
	}
	if x.Positional.TargetDir == "" {
		x.Positional.TargetDir = "."
	}

	snapPath, err := pack.Snap(x.Positional.SnapDir, x.Positional.TargetDir)
	if err != nil {
		return fmt.Errorf("cannot pack %q: %v", x.Positional.SnapDir, err)

	}
	fmt.Fprintf(Stdout, "built: %s\n", snapPath)
	return nil
}
