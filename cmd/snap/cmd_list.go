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
	"io"
	"reflect"
	"sort"
	"strings"
	"text/tabwriter"
	"text/template"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/strutil"
)

var shortListHelp = i18n.G("List installed snaps")
var longListHelp = i18n.G(`
The list command displays a summary of snaps installed in the current system.
`)

type cmdList struct {
	Positional struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`

	All    bool   `long:"all"`
	Format string `long:"format"`
}

func init() {
	addCommand("list", shortListHelp, longListHelp, func() flags.Commander { return &cmdList{} },
		map[string]string{
			"all":    i18n.G("Show all revisions"),
			"format": i18n.G("Use format string for output (try --format=help)"),
		}, nil)
}

type snapsByName []*client.Snap

func (s snapsByName) Len() int           { return len(s) }
func (s snapsByName) Less(i, j int) bool { return s[i].Name < s[j].Name }
func (s snapsByName) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }

func (x *cmdList) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	return listSnaps(installedSnapNames(x.Positional.Snaps), x.Format, x.All)
}

var ErrNoMatchingSnaps = errors.New(i18n.G("no matching snaps installed"))

// snapd will give us  and we want
// "" (local snap)     "-"
// risk                risk
// track               track        (not yet returned by snapd)
// track/stable        track
// track/risk          track/risk
// risk/branch         risk/…
// track/risk/branch   track/risk/…
func fmtChannel(ch string) string {
	if ch == "" {
		// "" -> "-" (local snap)
		return "-"
	}
	idx := strings.IndexByte(ch, '/')
	if idx < 0 {
		// risk -> risk
		return ch
	}
	first, rest := ch[:idx], ch[idx+1:]
	if rest == "stable" && first != "" {
		// track/stable -> track
		return first
	}
	if idx2 := strings.IndexByte(rest, '/'); idx2 >= 0 {
		// track/risk/branch -> track/risk/…
		return ch[:idx2+idx+2] + "…"
	}
	// so it's foo/bar -> either risk/branch, or track/risk.
	if strutil.ListContains(channelRisks, first) {
		// risk/branch -> risk/…
		return first + "/…"
	}
	// track/risk -> track/risk
	return ch
}

func listSnaps(names []string, format string, all bool) error {
	w := tabWriter()
	defer w.Flush()

	if format == "help" {
		return describeListFormat(w)
	}

	cli := Client()
	snaps, err := cli.List(names, &client.ListOptions{All: all})
	if err != nil {
		if err == client.ErrNoSnapsInstalled {
			if len(names) == 0 {
				fmt.Fprintln(Stderr, i18n.G("No snaps are installed yet. Try 'snap install hello-world'."))
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

	switch format {
	case "":
		return outputSnapsDefault(w, snaps)
	default:
		return outputSnapsWithFormat(w, snaps, format)
	}
}

type fieldDesc struct {
	name string
	help string
}

func clientSnapFields() []fieldDesc {
	v := reflect.TypeOf(client.Snap{})
	n := v.NumField()
	fields := make([]fieldDesc, n)
	for i := 0; i < n; i++ {
		field := v.Field(i)
		fields[i].name = strings.Split(field.Tag.Get("json"), ",")[0]
		fields[i].help = field.Tag.Get("help")
	}

	return fields
}

func clientSnapMap(s *client.Snap) map[string]interface{} {
	res := make(map[string]interface{})

	v := reflect.Indirect(reflect.ValueOf(s))
	t := v.Type()
	n := v.NumField()
	for i := 0; i < n; i++ {
		field := v.Field(i)
		name := strings.Split(t.Field(i).Tag.Get("json"), ",")[0]
		res[name] = field.Interface()
	}

	return res
}

func describeListFormat(w io.Writer) error {
	fmt.Fprintf(w, `Format uses a simple template system.

Use --format="{{.name}} {{.version}}" to get started.

All the elements available for snaps are:
`)
	for _, fld := range clientSnapFields() {
		if fld.help != "" {
			fmt.Fprintf(w, " - %s:\t%s\n", fld.name, fld.help)
		}
	}

	return nil
}

func outputSnapsWithFormat(w io.Writer, snaps []*client.Snap, format string) error {
	t, err := template.New("list-output").Parse(format)
	if err != nil {
		return fmt.Errorf("cannot use given template (try --format=help): %s", err)
	}

	for _, snap := range snaps {
		m := clientSnapMap(snap)
		if err := t.Execute(w, m); err != nil {
			return err
		}
		fmt.Fprint(w, "\n")
	}
	return nil
}

func outputSnapsDefault(w io.Writer, snaps []*client.Snap) error {
	fmt.Fprintln(w, i18n.G("Name\tVersion\tRev\tTracking\tPublisher\tNotes"))

	for _, snap := range snaps {
		// Aid parsing of the output by not leaving the field empty.
		publisher := "-"
		if snap.Publisher != nil {
			publisher = snap.Publisher.Username
		}
		// doing it this way because otherwise it's a sea of %s\t%s\t%s
		line := []string{
			snap.Name,
			snap.Version,
			snap.Revision.String(),
			fmtChannel(snap.TrackingChannel),
			publisher,
			NotesFromLocal(snap).String(),
		}
		fmt.Fprintln(w, strings.Join(line, "\t"))
	}

	return nil
}

func tabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(Stdout, 5, 3, 2, ' ', 0)
}
