// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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
	"sort"
	"text/tabwriter"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/snapstate"
)

var (
	shortComponentsHelp = i18n.G("List available and installed components for the calling snap")
	longComponentsHelp  = i18n.G(`
The components command displays a summary of the components that are installed
and available for the calling snap.
`)
)

func init() {
	addCommand("components", shortComponentsHelp, longComponentsHelp, func() command {
		return &componentsCommand{}
	})
}

type componentsCommand struct {
	baseCommand
}

func (c *componentsCommand) Execute(args []string) error {
	if len(args) > 0 {
		return fmt.Errorf("unexpected arguments: %v", args)
	}

	ctx, err := c.ensureContext()
	if err != nil {
		return err
	}

	snapName := ctx.InstanceName()
	st := ctx.State()
	st.Lock()
	defer st.Unlock()

	snapInfo, err := snapstate.CurrentInfo(st, snapName)
	if err != nil {
		return fmt.Errorf("cannot get snap info: %w", err)
	}

	if len(snapInfo.Components) == 0 {
		fmt.Fprintln(c.stderr, i18n.G("No components are available for this snap."))
		return nil
	}

	var snapst snapstate.SnapState
	if err := snapstate.Get(st, snapName, &snapst); err != nil {
		return fmt.Errorf("cannot get snap state: %w", err)
	}

	compStates := snapst.Sequence.ComponentsForRevision(snapst.Current)
	installed := make(map[string]bool, len(compStates))
	for _, cs := range compStates {
		installed[cs.SideInfo.Component.ComponentName] = true
	}

	type compRow struct {
		name   string
		status string
		ctype  string
	}

	rows := make([]compRow, 0, len(snapInfo.Components))
	for name, comp := range snapInfo.Components {
		status := "available"
		if installed[name] {
			status = "installed"
		}
		rows = append(rows, compRow{
			name:   "+" + name,
			status: status,
			ctype:  string(comp.Type),
		})
	}

	// installed first, then alphabetical within each group
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].status != rows[j].status {
			return rows[i].status == "installed"
		}
		return rows[i].name < rows[j].name
	})

	// same as snap command
	w := tabwriter.NewWriter(c.stdout, 5, 3, 2, ' ', 0)
	fmt.Fprintln(w, "Component\tStatus\tType")
	for _, r := range rows {
		fmt.Fprintf(w, "%s\t%s\t%s\n", r.name, r.status, r.ctype)
	}
	return w.Flush()
}
