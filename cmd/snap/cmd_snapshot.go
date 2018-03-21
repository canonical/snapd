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
	"strconv"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/strutil/quantity"
)

func fmtSize(size int64) string {
	return quantity.FormatAmount(uint64(size), -1)
}

type savedCmd struct {
	timeMixin
	Wide       bool       `long:"wide"`
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

func wjoin(shots []*client.Snapshot, width int) string {
	if len(shots) == 0 {
		return ""
	}
	// we know snap names are all ascii, and … is 3 bytes
	out := make([]byte, width+2)
	w := 0
	shots, last := shots[:len(shots)-1], shots[len(shots)-1]
	for _, sh := range shots {
		// if you can't put at least ", …" after this, stop now
		if w+len(sh.Snap)+3 > width {
			w += copy(out[w:], "…")
			return string(out[:w])
		}
		w += copy(out[w:], sh.Snap)
		w += copy(out[w:], ", ")
	}
	if w+len(last.Snap) > width {
		w += copy(out[w:], "…")
		return string(out[:w])
	}
	w += copy(out[w:], last.Snap)
	return string(out[:w])
}

func (x *savedCmd) tabline(sg *client.SnapshotSet, extraWidth int) string {
	if len(sg.Snapshots) == 0 {
		return fmt.Sprintf("%d\t-\t-\t-", sg.ID)
	}
	mint := sg.Time()
	sz := sg.Size()

	width := 1000
	if !x.Wide {
		width, _ = termSize()
		// size (5) + gutters (3+2+2; why is the first gutter 3?)
		width -= 12 + extraWidth
	}
	return fmt.Sprintf("%d\t%s\t%s\t%s", sg.ID, x.fmtTime(mint), wjoin(sg.Snapshots, width), fmtSize(sz))
}

func (x *savedCmd) Execute([]string) error {
	list, err := Client().Snapshots(uint64(x.ID), installedSnapNames(x.Positional.Snaps))
	if err != nil {
		return err
	}
	if len(list) == 0 {
		fmt.Fprintln(Stdout, "No snapshots found.")
		return nil
	}
	w := tabWriter()
	defer w.Flush()

	// TRANSLATORS: 'Set' as in group or bag of things
	fmt.Fprintln(w, "Set\tDate\tSnaps\tSize")
	// the list is ordered by id
	minTimeWidth := len(x.fmtTime(list[0].Time()))
	maxIDwidth := len(strconv.FormatUint(list[len(list)-1].ID, 10))
	extraWidth := maxIDwidth + minTimeWidth

	for _, sg := range list {
		fmt.Fprintln(w, x.tabline(&sg, extraWidth))
	}

	return nil
}

type saveCmd struct {
	waitMixin
	timeMixin
	Users      []string `long:"users"`
	Positional struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
}

var shortSaveHelp = i18n.G("Save a snapshot of the current data")
var longSaveHelp = i18n.G(`
The save command creates a snapshot of the current data for the given snaps.
`)

func (x *saveCmd) Execute([]string) error {
	cli := Client()
	changeID, err := cli.SnapshotMany(installedSnapNames(x.Positional.Snaps), x.Users)
	if err != nil {
		return err
	}
	chg, err := x.wait(cli, changeID)
	if err == noWait {
		return nil
	}
	if err != nil {
		return err
	}

	var shID snapshotID
	chg.Get("snapshot-id", &shID)
	y := &savedCmd{
		timeMixin: x.timeMixin,
		ID:        shID,
	}
	y.Positional.Snaps = x.Positional.Snaps
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
	cli := Client()
	snaps := installedSnapNames(x.Positional.Snaps)
	changeID, err := cli.ForgetSnapshot(uint64(x.Positional.ID), snaps)
	if err != nil {
		return err
	}
	_, err = x.wait(cli, changeID)
	if err == noWait {
		return nil
	}
	if err != nil {
		return err
	}

	if len(snaps) > 0 {
		// TRANSLATORS: the %s is a comma-separated list of quoted snap names
		fmt.Fprintf(Stdout, i18n.G("Snapshot #%d of snaps %s forgotten.\n"), x.Positional.ID, strutil.Quoted(snaps))
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
The check command checks a snapshot against its hashsums.
`)

func (x *checkSnapshotCmd) Execute([]string) error {
	cli := Client()
	snaps := installedSnapNames(x.Positional.Snaps)
	users := strings.Split(x.Users, ",")
	changeID, err := cli.CheckSnapshot(uint64(x.Positional.ID), snaps, users)
	if err != nil {
		return err
	}
	_, err = x.wait(cli, changeID)
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
	cli := Client()
	snaps := installedSnapNames(x.Positional.Snaps)
	users := strings.Split(x.Users, ",")
	changeID, err := cli.RestoreSnapshot(uint64(x.Positional.ID), snaps, users)
	if err != nil {
		return err
	}
	_, err = x.wait(cli, changeID)
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
		timeDescs.also(map[string]string{
			"wide": i18n.G("Ignore terminal width and print all available information"),
			"id":   i18n.G("Only list this snapshot."),
		}),
		nil)

	addCommand("save",
		shortSaveHelp,
		longSaveHelp,
		func() flags.Commander {
			return &saveCmd{}
		}, timeDescs.also(waitDescs).also(map[string]string{
			"users": i18n.G("Only snapshot these users' files."),
		}), nil)

	addCommand("restore",
		shortRestoreHelp,
		longRestoreHelp,
		func() flags.Commander {
			return &restoreCmd{}
		}, waitDescs.also(map[string]string{
			"users": i18n.G("Only restore these users' files."),
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
			"users": i18n.G("Only check these users' files."),
		}), nil)
}
