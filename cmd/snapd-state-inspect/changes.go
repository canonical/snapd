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
	"fmt"
	"sort"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/overlord/state"
)

type changesCommand struct {
	baseCommand
}

var shortChangesHelp = i18n.G("The changes command prints all changes.")

func init() {
	addCommand("changes", shortChangesHelp, "", func() command {
		return &changesCommand{}
	})
}

func (c *changesCommand) showChanges(st *state.State) error {
	st.Lock()
	defer st.Unlock()

	changes := st.Changes()
	sort.Sort(byChangeID(changes))

	fmt.Fprintf(c.out, "ID\tStatus\tSpawn\tReady\tLabel\tSummary\n")
	for _, chg := range changes {
		fmt.Fprintf(c.out, "%s\t%s\t%s\t%s\t%s\t%s\n", chg.ID(), chg.Status().String(), formatTime(chg.SpawnTime()), formatTime(chg.ReadyTime()), chg.Kind(), chg.Summary())
	}
	c.out.Flush()

	return nil
}

func (c *changesCommand) Execute(args []string) error {
	st, err := loadState(c.Positional.StateFilePath)
	if err != nil {
		return err
	}

	return c.showChanges(st)
}
