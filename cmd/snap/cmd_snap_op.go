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
	"time"

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
)

func wait(client *client.Client, uuid string) error {
	for {
		op, err := client.Operation(uuid)
		if err != nil {
			return err
		}

		if !op.Running() {
			return op.Err()
		}

		time.Sleep(100 * time.Millisecond)
	}
}

var (
	shortAddHelp        = i18n.G("Add a snap to the system")
	shortRemoveHelp     = i18n.G("Remove a snap from the system")
	shortPurgeHelp      = i18n.G("Purge a snap's data from the system")
	shortRefreshHelp    = i18n.G("Refresh a snap in the system")
	shortRollbackHelp   = i18n.G("Rollback a snap to its previous known-good version")
	shortActivateHelp   = i18n.G("Activate a snap that is installed but inactive")
	shortDeactivateHelp = i18n.G("Deactivate an installed active snap")
)

var longAddHelp = i18n.G(`
The add command installs and activates the named snap in the system.
`)

var longRemoveHelp = i18n.G(`
The remove command removes the named snap from the system.

The snap's data is currently not removed; use purge for that. This behaviour
will change before 16.04 is final.
`)

var longPurgeHelp = i18n.G(`
The purge command removes the data of the named snap from the system.
`)

var longRefreshHelp = i18n.G(`
The refresh command refreshes (updates) the named snap.
`)

var longRollbackHelp = i18n.G(`
The rollback command reverts an installed snap to its previous revision.
`)

var longActivateHelp = i18n.G(`
The activate command activates an installed but inactive snap.

Snaps that are not active don't have their apps available for use.
`)

var longDeactivateHelp = i18n.G(`
The deactivate command deactivates an installed, active snap.

Snaps that are not active don't have their apps available for use.
`)

type cmdOp struct {
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
	op func(*client.Client, string) (string, error)
}

func init() {
	for _, s := range []struct {
		name  string
		short string
		long  string
		op    func(*client.Client, string) (string, error)
	}{
		{"add", shortAddHelp, longAddHelp, (*client.Client).AddSnap},
		{"remove", shortRemoveHelp, longRemoveHelp, (*client.Client).RemoveSnap},
		{"purge", shortPurgeHelp, longPurgeHelp, (*client.Client).PurgeSnap},
		{"refresh", shortRefreshHelp, longRefreshHelp, (*client.Client).RefreshSnap},
		{"rollback", shortRollbackHelp, longRollbackHelp, (*client.Client).RollbackSnap},
		{"activate", shortActivateHelp, longActivateHelp, (*client.Client).ActivateSnap},
		{"deactivate", shortDeactivateHelp, longDeactivateHelp, (*client.Client).DeactivateSnap},
	} {
		op := s.op
		addCommand(s.name, s.short, s.long, func() interface{} { return &cmdOp{op: op} })
	}
}

func (x *cmdOp) Execute([]string) error {
	cli := Client()
	uuid, err := x.op(cli, x.Positional.Snap)
	if err != nil {
		return err
	}

	return wait(cli, uuid)
}
