// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/ifacestate"
	"github.com/snapcore/snapd/overlord/state"
)

type efiSecurebootDBUpdateCommand struct {
	baseCommand
	Startup bool `long:"startup" description:"Startup"`

	Prepare bool `long:"prepare" description:"Prepare"`
	PK      bool `long:"pk"`
	KEK     bool `long:"kek"`
	DB      bool `long:"db"`
	DBX     bool `long:"dbx"`

	Cleanup bool `long:"cleanup" description:"Cleanup"`
}

var shortEFISecurebootDBUpdateHelp = i18n.G("Notify of EFI Secure Boot DB update")

var longEFISecurebootDBUpdateHelp = i18n.G(`
The efi-secureboot-db-update command is used to notify snapd of a pending change
to the EFI secure boot key databases, by supplying information on the state of
the update and indicating which database is a subject to the update. It is
expected that the hook will be called by the process acting as a local manager
of the relevant EFI key databases.

Notify snapd of local manager startup:
$ snapctl efi-secureboot-db-update --startup

THe snap issuing the request is considered to be the local EFI database manager.
All subsequent commands issued on behalf of other entities will return an error.

Notify snapd of a pending update to one of the key databases:
$ snapctl efi-secureboot-db-update --prepare [--pk|--kek|--db|--dbx]

The prepare call should be done **before** attempting to update a relevant
database. An error returned by snapd should be considered a fatal error and the
update process should be aborted.

Notify snapd of a completed update (either successful or failed):
$ snapctl efi-secureboot-db-update --cleanup
`[1:])

func init() {
	addCommand("efi-secureboot-db-update", shortEFISecurebootDBUpdateHelp, longEFISecurebootDBUpdateHelp,
		func() command { return &efiSecurebootDBUpdateCommand{} })
}

func (c *efiSecurebootDBUpdateCommand) Execute(args []string) error {
	context, err := c.ensureContext()
	if err != nil {
		return err
	}
	context.Lock()
	defer context.Unlock()

	countTrue := func(vals ...bool) int {
		count := 0
		for _, v := range vals {
			if v {
				count++
			}
		}
		return count
	}

	switch {
	case c.Startup:
		if c.Cleanup || c.Prepare {
			return fmt.Errorf("--startup cannot be called with other actions")
		}

		if c.PK || c.KEK || c.DB || c.DBX {
			return fmt.Errorf("UEFI key database cannot be used with --startup")
		}

		if err := isFwupdConnected(context.State(), context.InstanceName()); err != nil {
			return err
		}

		return fdestate.EFISecureBootDBManagerStartup(context.State(), fdestate.EFIKeyManagerIdentity{
			SnapInstanceName: context.InstanceName(),
		})
	case c.Prepare:
		if c.Cleanup || c.Startup {
			return fmt.Errorf("--prepare cannot be called with other actions")
		}

		if !(c.PK || c.KEK || c.DB || c.DBX) {
			return fmt.Errorf("at least one database must be selected")
		}

		if countTrue(c.PK, c.KEK, c.DB, c.DBX) > 1 {
			return fmt.Errorf("only one key database can be selected")
		}

		if c.PK || c.KEK || c.DB {
			return fmt.Errorf("updates of PK, KEK or DB are not supported")
		}

		var payload []byte

		if err := context.Get("stdin", &payload); err != nil {
			return fmt.Errorf("cannot extract payload: %w", err)
		}

		if err := isFwupdConnected(context.State(), context.InstanceName()); err != nil {
			return err
		}

		return fdestate.EFISecureBootDBUpdatePrepare(context.State(),
			fdestate.EFIKeyManagerIdentity{
				SnapInstanceName: context.InstanceName(),
			},
			fdestate.EFISecurebootDBX, // we only support updating DBX
			payload)
	case c.Cleanup:
		if c.Prepare || c.Startup {
			return fmt.Errorf("--cleanup cannot be called with other actions")
		}

		if c.PK || c.KEK || c.DB || c.DBX {
			return fmt.Errorf("UEFI key database cannot be used with --cleanup")
		}

		if err := isFwupdConnected(context.State(), context.InstanceName()); err != nil {
			return err
		}

		return fdestate.EFISecureBootDBUpdateCleanup(context.State(), fdestate.EFIKeyManagerIdentity{
			SnapInstanceName: context.InstanceName(),
		})
	default:
		return fmt.Errorf("no action provided")
	}
}

func isFwupdConnected(st *state.State, snapName string) error {
	conns, err := ifacestate.ConnectionStates(st)
	if err != nil {
		return fmt.Errorf("internal error: cannot get connections: %s", err)
	}

	for connId, connState := range conns {
		if connState.Interface != "fwupd" || !connState.Active() {
			continue
		}

		connRef, err := interfaces.ParseConnRef(connId)
		if err != nil {
			return err
		}

		// only slot side allows manipulating EFI variables
		if connRef.SlotRef.Snap == snapName {
			return nil
		}
	}
	return fmt.Errorf("required interface fwupd is not connected")

}
