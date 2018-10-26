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
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/strutil/quantity"
)

func fmtSize(size int64) string {
	return quantity.FormatAmount(uint64(size), -1)
}

type savedCmd struct {
	clientMixin
	durationMixin
	ID         snapshotID `long:"id"`
	Positional struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
}

var (
	shortSavedHelp   = i18n.G("List currently stored snapshots")
	shortSaveHelp    = i18n.G("Save a snapshot of the current data")
	shortForgetHelp  = i18n.G("Delete a snapshot")
	shortCheckHelp   = i18n.G("Check a snapshot")
	shortRestoreHelp = i18n.G("Restore a snapshot")
)

var longSavedHelp = i18n.G(`
The saved command displays a list of snapshots that have been created
previously with the 'save' command.
`)
var longSaveHelp = i18n.G(`
The save command creates a snapshot of the current user, system and
configuration data for the given snaps.

By default, this command saves the data of all snaps for all users.
Alternatively, you can specify the data of which snaps to save, or
for which users, or a combination of these.

If a snap is included in a save operation, excluding its system and
configuration data from the snapshot is not currently possible. This
restriction may be lifted in the future.
`)
var longForgetHelp = i18n.G(`
The forget command deletes a snapshot. This operation can not be
undone.

A snapshot contains archives for the user, system and configuration
data of each snap included in the snapshot.

By default, this command forgets all the data in a snapshot.
Alternatively, you can specify the data of which snaps to forget.
`)
var longCheckHelp = i18n.G(`
The check-snapshot command verifies the user, system and configuration
data of the snaps included in the specified snapshot.

The check operation runs the same data integrity verification that is
performed when a snapshot is restored.

By default, this command checks all the data in a snapshot.
Alternatively, you can specify the data of which snaps to check, or
for which users, or a combination of these.

If a snap is included in a check-snapshot operation, excluding its
system and configuration data from the check is not currently
possible. This restriction may be lifted in the future.
`)
var longRestoreHelp = i18n.G(`
The restore command replaces the current user, system and
configuration data of included snaps, with the corresponding data from
the specified snapshot.

By default, this command restores all the data in a snapshot.
Alternatively, you can specify the data of which snaps to restore, or
for which users, or a combination of these.

If a snap is included in a restore operation, excluding its system and
configuration data from the restore is not currently possible. This
restriction may be lifted in the future.
`)

func (x *savedCmd) Execute([]string) error {
	list, err := x.client.SnapshotSets(uint64(x.ID), installedSnapNames(x.Positional.Snaps))
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(Stdout, i18n.G("No snapshots found."))
		return nil
	}
	w := tabWriter()
	defer w.Flush()

	fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
		// TRANSLATORS: 'Set' as in group or bag of things
		i18n.G("Set"),
		"Snap",
		// TRANSLATORS: 'Age' as in how old something is
		i18n.G("Age"),
		i18n.G("Version"),
		// TRANSLATORS: 'Rev' is an abbreviation of 'Revision'
		i18n.G("Rev"),
		i18n.G("Size"),
		// TRANSLATORS: 'Notes' as in 'Comments'
		i18n.G("Notes"))
	for _, sg := range list {
		for _, sh := range sg.Snapshots {
			note := "-"
			if sh.Broken != "" {
				note = "broken: " + sh.Broken
			}
			size := quantity.FormatAmount(uint64(sh.Size), -1) + "B"
			fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\t%s\t%s\n", sg.ID, sh.Snap, x.fmtDuration(sh.Time), sh.Version, sh.Revision, size, note)
		}
	}
	return nil
}

type saveCmd struct {
	waitMixin
	durationMixin
	Users      string `long:"users"`
	Positional struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
}

func (x *saveCmd) Execute([]string) error {
	var users []string
	if len(x.Users) > 0 {
		users = strings.Split(x.Users, ",")
	}
	setID, changeID, err := x.client.SnapshotMany(installedSnapNames(x.Positional.Snaps), users)
	if err != nil {
		return err
	}
	if _, err := x.wait(changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	y := &savedCmd{
		clientMixin:   x.clientMixin,
		durationMixin: x.durationMixin,
		ID:            snapshotID(setID),
	}
	return y.Execute(nil)
}

type forgetCmd struct {
	waitMixin
	Positional struct {
		ID    snapshotID          `positional-arg-name:"<id>"`
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *forgetCmd) Execute([]string) error {
	snaps := installedSnapNames(x.Positional.Snaps)
	changeID, err := x.client.ForgetSnapshots(uint64(x.Positional.ID), snaps)
	if err != nil {
		return err
	}
	_, err = x.wait(changeID)
	if err == noWait {
		return nil
	}
	if err != nil {
		return err
	}

	if len(snaps) > 0 {
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		fmt.Fprintf(Stdout, i18n.NG("Snapshot #%d of snap %s forgotten.\n", "Snapshot #%d of snaps %s forgotten.\n", len(snaps)), x.Positional.ID, strutil.Quoted(snaps))
	} else {
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d forgotten.\n"), x.Positional.ID)
	}
	return nil
}

type checkSnapshotCmd struct {
	waitMixin
	Users      string `long:"users"`
	Positional struct {
		ID    snapshotID          `positional-arg-name:"<id>"`
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *checkSnapshotCmd) Execute([]string) error {
	snaps := installedSnapNames(x.Positional.Snaps)
	var users []string
	if len(x.Users) > 0 {
		users = strings.Split(x.Users, ",")
	}
	changeID, err := x.client.CheckSnapshots(uint64(x.Positional.ID), snaps, users)
	if err != nil {
		return err
	}
	_, err = x.wait(changeID)
	if err == noWait {
		return nil
	}
	if err != nil {
		return err
	}

	// TODO: also mention the home archives that were actually checked
	if len(snaps) > 0 {
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d of snaps %s verified successfully.\n"),
			x.Positional.ID, strutil.Quoted(snaps))
	} else {
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d verified successfully.\n"), x.Positional.ID)
	}
	return nil
}

type restoreCmd struct {
	waitMixin
	Users      string `long:"users"`
	Positional struct {
		ID    snapshotID          `positional-arg-name:"<id>"`
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *restoreCmd) Execute([]string) error {
	snaps := installedSnapNames(x.Positional.Snaps)
	var users []string
	if len(x.Users) > 0 {
		users = strings.Split(x.Users, ",")
	}
	changeID, err := x.client.RestoreSnapshots(uint64(x.Positional.ID), snaps, users)
	if err != nil {
		return err
	}
	_, err = x.wait(changeID)
	if err == noWait {
		return nil
	}
	if err != nil {
		return err
	}

	// TODO: also mention the home archives that were actually restored
	if len(snaps) > 0 {
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		fmt.Fprintf(Stdout, i18n.G("Restored snapshot #%d of snaps %s.\n"),
			x.Positional.ID, strutil.Quoted(snaps))
	} else {
		fmt.Fprintf(Stdout, i18n.G("Restored snapshot #%d.\n"), x.Positional.ID)
	}
	return nil
}

func init() {
	addCommand("saved",
		shortSavedHelp,
		longSavedHelp,
		func() flags.Commander {
			return &savedCmd{}
		},
		durationDescs.also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"id": i18n.G("Show only a specific snapshot."),
		}),
		nil)

	addCommand("save",
		shortSaveHelp,
		longSaveHelp,
		func() flags.Commander {
			return &saveCmd{}
		}, durationDescs.also(waitDescs).also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"users": i18n.G("Snapshot data of only specific users (comma-separated) (default: all users)"),
		}), nil)

	addCommand("restore",
		shortRestoreHelp,
		longRestoreHelp,
		func() flags.Commander {
			return &restoreCmd{}
		}, waitDescs.also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"users": i18n.G("Restore data of only specific users (comma-separated) (default: all users)"),
		}), nil)

	addCommand("forget",
		shortForgetHelp,
		longForgetHelp,
		func() flags.Commander {
			return &forgetCmd{}
		}, waitDescs, nil)

	addCommand("check-snapshot",
		shortCheckHelp,
		longCheckHelp,
		func() flags.Commander {
			return &checkSnapshotCmd{}
		}, waitDescs.also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"users": i18n.G("Check data of only specific users (comma-separated) (default: all users)"),
		}), nil)
}
