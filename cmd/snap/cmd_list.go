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
	"strings"
	"text/tabwriter"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
)

var (
	shortListHelp = i18n.G("List installed snaps")
	longListHelp  = i18n.G(`
The list command displays a summary of snaps installed in the current system.

A green check mark (given color and unicode support) after a publisher name
indicates that the publisher has been verified.
`)
)

type cmdList struct {
	clientMixin
	Positional struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`

	All bool `long:"all"`
	colorMixin
}

func init() {
	addCommand("list", shortListHelp, longListHelp, func() flags.Commander { return &cmdList{} },
		colorDescs.also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"all": i18n.G("Show all revisions"),
		}), nil)
}

type snapsByName []*client.Snap

func (s snapsByName) Len() int           { return len(s) }
func (s snapsByName) Less(i, j int) bool { return s[i].Name < s[j].Name }
func (s snapsByName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

var ErrNoMatchingSnaps = errors.New(i18n.G("no matching snaps installed"))

// snapd will give us  and we want
// "" (local snap)     "-"
// latest/risk         latest/risk
// track/risk          track/risk
// track/risk/branch   track/risk/…
// anything else       SISO
func fmtChannel(ch string) string {
	if ch == "" {
		// "" -> "-" (local snap)
		return "-"
	}
	if strings.Count(ch, "/") != 2 {
		return ch
	}
	idx := strings.LastIndexByte(ch, '/')
	return ch[:idx+1] + "…"
}

func fmtVersion(v string) string {
	if v == "" {
		// most likely a broken snap, leave a placeholder
		return "-"
	}
	return v
}

func (x *cmdList) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	names := installedSnapNames(x.Positional.Snaps)
	snaps := mylog.Check2(x.client.List(names, &client.ListOptions{All: x.All}))

	sort.Sort(snapsByName(snaps))

	esc := x.getEscapes()
	w := tabWriter()

	// TRANSLATORS: the %s is to insert a filler escape sequence (please keep it flush to the column header, with no extra spaces)
	fmt.Fprintf(w, i18n.G("Name\tVersion\tRev\tTracking\tPublisher%s\tNotes\n"), fillerPublisher(esc))

	for _, snap := range snaps {
		// doing it this way because otherwise it's a sea of %s\t%s\t%s
		line := []string{
			snap.Name,
			fmtVersion(snap.Version),
			snap.Revision.String(),
			fmtChannel(snap.TrackingChannel),
			shortPublisher(esc, snap.Publisher),
			NotesFromLocal(snap).String(),
		}
		fmt.Fprintln(w, strings.Join(line, "\t"))
	}
	w.Flush()

	return nil
}

func tabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
}
