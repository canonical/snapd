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
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/progress"

	"github.com/jessevdk/go-flags"
)

func wait(client *client.Client, id string) error {
	pb := progress.NewTextProgress()
	defer func() {
		pb.Finished()
		fmt.Print("\n")
	}()

	var lastID string
	for {
		chg, err := client.Change(id)
		if err != nil {
			return err
		}

		for _, t := range chg.Tasks {
			switch {
			case t.Status != "Doing":
				continue
			case t.Progress.Total == 1:
				pb.Spin(t.Summary)
			case t.ID == lastID:
				pb.Set(float64(t.Progress.Done))
			default:
				pb.Start(t.Summary, float64(t.Progress.Total))
				lastID = t.ID
			}
			break
		}

		if chg.Ready {
			if chg.Status == "Done" {
				return nil
			}

			if chg.Err != "" {
				return errors.New(chg.Err)
			}

			return fmt.Errorf("change finished in status %q with no error message", chg.Status)
		}

		// note this very purposely is not a ticker; we want
		// to sleep 100ms between calls, not call once every
		// 100ms.
		time.Sleep(100 * time.Millisecond)
	}
}

var (
	shortInstallHelp    = i18n.G("Install a snap to the system")
	shortRemoveHelp     = i18n.G("Remove a snap from the system")
	shortPurgeHelp      = i18n.G("Purge a snap's data from the system")
	shortRefreshHelp    = i18n.G("Refresh a snap in the system")
	shortRollbackHelp   = i18n.G("Rollback a snap to its previous known-good version")
	shortActivateHelp   = i18n.G("Activate a snap that is installed but inactive")
	shortDeactivateHelp = i18n.G("Deactivate an installed active snap")
)

var longInstallHelp = i18n.G(`
The install command installs and activates the named snap in the system.
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

func (x *cmdOp) Execute([]string) error {
	cli := Client()
	changeID, err := x.op(cli, x.Positional.Snap)
	if err != nil {
		return err
	}

	return wait(cli, changeID)
}

type cmdInstall struct {
	Channel    string `long:"channel" description:"Install from this channel instead of the device's default"`
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdInstall) Execute([]string) error {
	var changeID string
	var err error

	cli := Client()
	name := x.Positional.Snap
	if strings.Contains(name, "/") || strings.HasSuffix(name, ".snap") || strings.Contains(name, ".snap.") {
		changeID, err = cli.InstallSnapPath(name)
	} else {
		changeID, err = cli.InstallSnap(name, x.Channel)
	}
	if err != nil {
		return err
	}

	return wait(cli, changeID)
}

type cmdRefresh struct {
	Channel    string `long:"channel" description:"Refresh to the latest on this channel, and track this channel henceforth"`
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdRefresh) Execute([]string) error {
	cli := Client()
	changeID, err := cli.RefreshSnap(x.Positional.Snap, x.Channel)
	if err != nil {
		return err
	}

	return wait(cli, changeID)
}

func init() {
	for _, s := range []struct {
		name  string
		short string
		long  string
		op    func(*client.Client, string) (string, error)
	}{
		{"remove", shortRemoveHelp, longRemoveHelp, (*client.Client).RemoveSnap},
		{"purge", shortPurgeHelp, longPurgeHelp, (*client.Client).PurgeSnap},
		// FIXME: re-enable once the state engine is ready
		/*
			{"rollback", shortRollbackHelp, longRollbackHelp, (*client.Client).RollbackSnap},
			{"activate", shortActivateHelp, longActivateHelp, (*client.Client).ActivateSnap},
			{"deactivate", shortDeactivateHelp, longDeactivateHelp, (*client.Client).DeactivateSnap},
		*/
	} {
		op := s.op
		addCommand(s.name, s.short, s.long, func() flags.Commander { return &cmdOp{op: op} })
	}

	addCommand("install", shortInstallHelp, longInstallHelp, func() flags.Commander { return &cmdInstall{} })
	addCommand("refresh", shortRefreshHelp, longRefreshHelp, func() flags.Commander { return &cmdRefresh{} })
}
