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
	"bytes"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

type infoCmd struct {
	Verbose    bool `long:"verbose"`
	Positional struct {
		Snaps []anySnapName `positional-arg-name:"<snap>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

var shortInfoHelp = i18n.G("show detailed information about a snap")
var longInfoHelp = i18n.G(`
The info command shows detailed information about a snap, be it by name or by path.`)

func init() {
	addCommand("info",
		shortInfoHelp,
		longInfoHelp,
		func() flags.Commander {
			return &infoCmd{}
		}, map[string]string{
			"verbose": i18n.G("Include a verbose list of a snap's notes (otherwise, summarise notes)"),
		}, nil)
}

func norm(path string) string {
	path = filepath.Clean(path)
	if osutil.IsDirectory(path) {
		path = path + "/"
	}

	return path
}

func maybePrintPrice(w io.Writer, snap *client.Snap, resInfo *client.ResultInfo) {
	if resInfo == nil {
		return
	}
	price, currency, err := getPrice(snap.Prices, resInfo.SuggestedCurrency)
	if err != nil {
		return
	}
	fmt.Fprintf(w, "price:\t%s\n", formatPrice(price, currency))
}

func maybePrintType(w io.Writer, t string) {
	// XXX: using literals here until we reshuffle snap & client properly
	// (and os->core rename happens, etc)
	switch t {
	case "", "app", "application":
		return
	case "os":
		t = "core"
	}

	fmt.Fprintf(w, "type:\t%s\n", t)
}

func tryDirect(w io.Writer, path string, verbose bool) bool {
	path = norm(path)

	snapf, err := snap.Open(path)
	if err != nil {
		return false
	}

	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	if err != nil {
		return false
	}
	fmt.Fprintf(w, "path:\t%q\n", path)
	fmt.Fprintf(w, "name:\t%s\n", info.Name())
	fmt.Fprintf(w, "summary:\t%q\n", info.Summary())

	var notes *Notes
	if verbose {
		fmt.Fprintln(w, "notes:\t")
		fmt.Fprintf(w, "  confinement:\t%s\n", info.Confinement)
		if info.Broken == "" {
			fmt.Fprintln(w, "  broken:\tfalse")
		} else {
			fmt.Fprintf(w, "  broken:\ttrue (%s)\n", info.Broken)
		}

	} else {
		notes = NotesFromInfo(info)
	}
	fmt.Fprintf(w, "version:\t%s %s\n", info.Version, notes)
	maybePrintType(w, string(info.Type))

	return true
}

func coalesce(snaps ...*client.Snap) *client.Snap {
	for _, s := range snaps {
		if s != nil {
			return s
		}
	}
	return nil
}

// formatDescr formats a given string (typically a snap description)
// in a user friendly way.
//
// The rules are (intentionally) very simple:
// - word wrap at "max" chars
// - keep \n intact and break here
// - ignore \r
func formatDescr(descr string, max int) string {
	out := bytes.NewBuffer(nil)
	for _, line := range strings.Split(descr, "\n") {
		if len(line) > max {
			for _, chunk := range strutil.WordWrap(line, max) {
				fmt.Fprintf(out, "  %s\n", chunk)
			}
		} else {
			fmt.Fprintf(out, "  %s\n", line)
		}
	}

	return strings.TrimSuffix(out.String(), "\n")
}

func maybePrintCommands(w io.Writer, snapName string, allApps []client.AppInfo, n int) {
	if len(allApps) == 0 {
		return
	}

	commands := make([]string, 0, len(allApps))
	for _, app := range allApps {
		if app.Daemon != "" {
			continue
		}

		// TODO: helper for this?
		cmdStr := app.Name
		if cmdStr != snapName {
			cmdStr = fmt.Sprintf("%s.%s", snapName, cmdStr)
		}

		if len(app.Aliases) != 0 {
			cmdStr = fmt.Sprintf("%s (%s)", cmdStr, strings.Join(app.Aliases, ","))
		}

		commands = append(commands, cmdStr)
	}
	if len(commands) == 0 {
		return
	}

	fmt.Fprintf(w, "commands:\n")
	for _, cmd := range commands {
		fmt.Fprintf(w, "  - %s\n", cmd)
	}
}

func (x *infoCmd) Execute([]string) error {
	cli := Client()

	w := tabwriter.NewWriter(Stdout, 2, 2, 1, ' ', 0)

	noneOK := true
	for i, snapName := range x.Positional.Snaps {
		snapName := string(snapName)
		if i > 0 {
			fmt.Fprintln(w, "---")
		}

		if tryDirect(w, snapName, x.Verbose) {
			noneOK = false
			continue
		}
		remote, resInfo, _ := cli.FindOne(snapName)
		local, _, _ := cli.Snap(snapName)

		both := coalesce(local, remote)

		if both == nil {
			fmt.Fprintf(w, "argument:\t%q\nwarning:\t%s\n", snapName, i18n.G("not a valid snap"))
			continue
		}
		noneOK = false

		fmt.Fprintf(w, "name:\t%s\n", both.Name)
		fmt.Fprintf(w, "summary:\t%q\n", both.Summary)
		// TODO: have publisher; use publisher here,
		// and additionally print developer if publisher != developer
		fmt.Fprintf(w, "publisher:\t%s\n", both.Developer)
		if both.Contact != "" {
			fmt.Fprintf(w, "contact:\t%s\n", strings.TrimPrefix(both.Contact, "mailto:"))
		}
		maybePrintPrice(w, remote, resInfo)
		// FIXME: find out for real
		termWidth := 77
		fmt.Fprintf(w, "description: |\n%s\n", formatDescr(both.Description, termWidth))
		maybePrintType(w, both.Type)
		maybePrintCommands(w, snapName, both.Apps, termWidth)

		if x.Verbose {
			fmt.Fprintln(w, "notes:\t")
			fmt.Fprintf(w, "  private:\t%t\n", both.Private)
			fmt.Fprintf(w, "  confinement:\t%s\n", both.Confinement)
		}

		if local != nil {
			var notes *Notes
			if x.Verbose {
				jailMode := local.Confinement == client.DevModeConfinement && !local.DevMode
				fmt.Fprintf(w, "  devmode:\t%t\n", local.DevMode)
				fmt.Fprintf(w, "  jailmode:\t%t\n", jailMode)
				fmt.Fprintf(w, "  trymode:\t%t\n", local.TryMode)
				fmt.Fprintf(w, "  enabled:\t%t\n", local.Status == client.StatusActive)
				if local.Broken == "" {
					fmt.Fprintf(w, "  broken:\t%t\n", false)
				} else {
					fmt.Fprintf(w, "  broken:\t%t (%s)\n", true, local.Broken)
				}
			} else {
				notes = NotesFromLocal(local)
			}

			fmt.Fprintf(w, "tracking:\t%s\n", local.TrackingChannel)
			fmt.Fprintf(w, "installed:\t%s\t(%s)\t%s\t%s\n", local.Version, local.Revision, strutil.SizeToStr(local.InstalledSize), notes)
			fmt.Fprintf(w, "refreshed:\t%s\n", local.InstallDate)
		}

		if remote != nil && remote.Tracks != nil {
			// \t\t\t so we get "installed" lined up with "channels"
			fmt.Fprintf(w, "tracks:\t\t\t\n")
			for tr := range remote.Tracks {
				fmt.Fprintf(w, "  - %s:\n", tr)
				for ch := range remote.Tracks[tr] {
					m := remote.Tracks[tr][ch]
					fmt.Fprintf(w, "      %s:\t%s\t(%s)\t%s\t%s\n", ch, m.Version, m.Revision, strutil.SizeToStr(m.Size), NotesFromChannelSnapInfo(m))
				}
			}
		}

		if remote != nil && remote.Channels != nil {
			// \t\t\t so we get "installed" lined up with "channels"
			fmt.Fprintf(w, "channels:\t\t\t\n")
			for _, ch := range []string{"stable", "candidate", "beta", "edge"} {
				m := remote.Channels[ch]
				if m == nil {
					continue
				}
				fmt.Fprintf(w, "  %s:\t%s\t(%s)\t%s\t%s\n", ch, m.Version, m.Revision, strutil.SizeToStr(m.Size), NotesFromChannelSnapInfo(m))
			}
		}
	}
	w.Flush()

	if noneOK {
		return fmt.Errorf(i18n.G("no valid snaps given"))
	}

	return nil
}
