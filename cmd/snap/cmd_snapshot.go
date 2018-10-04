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

var shortSavedHelp = i18n.G("List currently stored snapshots")
var longSavedHelp = i18n.G(`
The saved command lists the snapshots that have been created previously with
the 'save' command.
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

var shortSaveHelp = i18n.G("Save a snapshot of the current data")
var longSaveHelp = i18n.G(`
The save command creates a snapshot of the current data for the given snaps.
`)

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

var shortForgetHelp = i18n.G("Delete a snapshot")
var longForgetHelp = i18n.G(`
The forget command deletes a snapshot.
`)

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

var shortCheckHelp = i18n.G("Check a snapshot")
var longCheckHelp = i18n.G(`
The check-snapshot command checks a snapshot against its hashsums.
`)

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
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d of snaps %s check passed.\n"),
			x.Positional.ID, strutil.Quoted(snaps))
	} else {
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d check passed.\n"), x.Positional.ID)
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

var shortRestoreHelp = i18n.G("Restore a snapshot")
var longRestoreHelp = i18n.G(`
The restore command restores a snapshot.
`)

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

	// TODO: also mention the home archives that were actually restoreed
	if len(snaps) > 0 {
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d of %s restored.\n"),
			x.Positional.ID, strutil.Quoted(snaps))
	} else {
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d restored.\n"), x.Positional.ID)
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
			"id": i18n.G("Only list this snapshot."),
		}),
		nil)

	addCommand("save",
		shortSaveHelp,
		longSaveHelp,
		func() flags.Commander {
			return &saveCmd{}
		}, durationDescs.also(waitDescs).also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"users": i18n.G("Only snapshot these users' files (a comma-separated list)."),
		}), nil)

	addCommand("restore",
		shortRestoreHelp,
		longRestoreHelp,
		func() flags.Commander {
			return &restoreCmd{}
		}, waitDescs.also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"users": i18n.G("Only restore these users' files (a comma-separated list)."),
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
			"users": i18n.G("Only check these users' files (a comma-separated list)."),
		}), nil)
}
