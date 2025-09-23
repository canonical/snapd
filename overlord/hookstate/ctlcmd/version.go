// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package ctlcmd

import (
	"fmt"
	"text/tabwriter"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snapdtool"
)

var (
	shortVersionHelp = i18n.G("Version information")
	longVersionHelp  = i18n.G(`
See snapd version`)
)

func init() {
	addCommand("version", shortVersionHelp, longVersionHelp, func() command { return &versionCommand{} })
}

type versionCommand struct {
	baseCommand
}

func (c *versionCommand) Execute(args []string) error {
	// same as snap command
	w := tabwriter.NewWriter(c.stdout, 5, 3, 2, ' ', 0)

	fmt.Fprintf(w, "snapd\t%s\n", snapdtool.Version)
	fmt.Fprintf(w, "series\t%s\n", release.Series)

	return w.Flush()
}
