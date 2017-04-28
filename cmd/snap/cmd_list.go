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
	"sort"
	"text/tabwriter"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"

	"github.com/jessevdk/go-flags"
)

var shortListHelp = i18n.G("List installed snaps")
var longListHelp = i18n.G(`
The list command displays a summary of snaps installed in the current system.`)

type cmdList struct {
	Positional struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`

	All bool `long:"all"`
}

func init() {
	addCommand("list", shortListHelp, longListHelp, func() flags.Commander { return &cmdList{} },
		map[string]string{"all": i18n.G("Show all revisions")}, nil)
}

type snapsByName []*client.Snap

func (s snapsByName) Len() int           { return len(s) }
func (s snapsByName) Less(i, j int) bool { return s[i].Name < s[j].Name }
func (s snapsByName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func (x *cmdList) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	names := make([]string, len(x.Positional.Snaps))
	for i, name := range x.Positional.Snaps {
		names[i] = string(name)
	}

	return listSnaps(names, x.All)
}

var ErrNoMatchingSnaps = errors.New(i18n.G("no matching snaps installed"))

func listSnaps(names []string, all bool) error {
	cli := Client()
	snaps, err := cli.List(names, &client.ListOptions{All: all})
	if err != nil {
		if err == client.ErrNoSnapsInstalled {
			if len(names) == 0 {
				fmt.Fprintln(Stderr, i18n.G("No snaps are installed yet. Try \"snap install hello-world\"."))
				return nil
			} else {
				return ErrNoMatchingSnaps
			}
		}
		return err
	} else if len(snaps) == 0 {
		return ErrNoMatchingSnaps
	}
	sort.Sort(snapsByName(snaps))

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Name\tVersion\tRev\tDeveloper\tNotes"))

	for _, snap := range snaps {
		// TODO: make JailMode a flag in the snap itself
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", snap.Name, snap.Version, snap.Revision, snap.Developer, NotesFromLocal(snap))
	}

	return nil
}

func tabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
}
