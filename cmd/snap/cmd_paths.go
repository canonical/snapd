// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
)

var pathsHelp = i18n.G("Print system paths")
var longPathsHelp = i18n.G(`
The paths command prints the list of paths detected and used by snapd.
`)

type cmdPaths struct{}

func init() {
	addDebugCommand("paths", pathsHelp, longPathsHelp, func() flags.Commander {
		return &cmdPaths{}
	}, nil, nil)
}

func (cmd cmdPaths) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	w := tabWriter()
	defer w.Flush()

	// TODO: include paths reported by snap-confine
	for _, p := range []struct {
		name string
		path string
	}{
		{"SNAPD_MOUNT", dirs.SnapMountDir},
		{"SNAPD_BIN", dirs.SnapBinariesDir},
		{"SNAPD_LIBEXEC", dirs.DistroLibExecDir},
	} {
		fmt.Fprintf(w, "%s=%s\n", p.name, p.path)
	}

	return nil
}
