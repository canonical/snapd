// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2018 Canonical Ltd
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

	"gopkg.in/yaml.v2"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/asserts"
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

func maybePrintID(w io.Writer, snap *client.Snap) {
	if snap.ID != "" {
		fmt.Fprintf(w, "snap-id:\t%s\n", snap.ID)
	}
}

func tryDirect(w io.Writer, path string, verbose bool) bool {
	path = norm(path)

	snapf, err := snap.Open(path)
	if err != nil {
		return false
	}

	var sha3_384 string
	if verbose && !osutil.IsDirectory(path) {
		var err error
		sha3_384, _, err = asserts.SnapFileSHA3_384(path)
		if err != nil {
			return false
		}
	}

	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	if err != nil {
		return false
	}
	fmt.Fprintf(w, "path:\t%q\n", path)
	fmt.Fprintf(w, "name:\t%s\n", info.Name())
	fmt.Fprintf(w, "summary:\t%s\n", formatSummary(info.Summary()))

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
	if sha3_384 != "" {
		fmt.Fprintf(w, "sha3-384:\t%s\n", sha3_384)
	}

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
// - trim whitespace
// - word wrap at "max" chars
// - keep \n intact and break here
// - ignore \r
func formatDescr(descr string, max int) string {
	out := bytes.NewBuffer(nil)
	for _, line := range strings.Split(strings.TrimSpace(descr), "\n") {
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
		if app.IsService() {
			continue
		}

		cmdStr := snap.JoinSnapApp(snapName, app.Name)
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

func maybePrintServices(w io.Writer, snapName string, allApps []client.AppInfo, n int) {
	if len(allApps) == 0 {
		return
	}

	services := make([]string, 0, len(allApps))
	for _, app := range allApps {
		if !app.IsService() {
			continue
		}

		var active, enabled string
		if app.Active {
			active = "active"
		} else {
			active = "inactive"
		}
		if app.Enabled {
			enabled = "enabled"
		} else {
			enabled = "disabled"
		}
		services = append(services, fmt.Sprintf("  %s:\t%s, %s, %s", snap.JoinSnapApp(snapName, app.Name), app.Daemon, enabled, active))
	}
	if len(services) == 0 {
		return
	}

	fmt.Fprintf(w, "services:\n")
	for _, svc := range services {
		fmt.Fprintln(w, svc)
	}
}

// displayChannels displays channels and tracks in the right order
func displayChannels(w io.Writer, remote *client.Snap) {
	// \t\t\t so we get "installed" lined up with "channels"
	fmt.Fprintf(w, "channels:\t\t\t\n")

	// order by tracks
	for _, tr := range remote.Tracks {
		trackHasOpenChannel := false
		for _, risk := range []string{"stable", "candidate", "beta", "edge"} {
			chName := fmt.Sprintf("%s/%s", tr, risk)
			ch, ok := remote.Channels[chName]
			if tr == "latest" {
				chName = risk
			}
			var version, revision, size, notes string
			if ok {
				version = ch.Version
				revision = fmt.Sprintf("(%s)", ch.Revision)
				size = strutil.SizeToStr(ch.Size)
				notes = NotesFromChannelSnapInfo(ch).String()
				trackHasOpenChannel = true
			} else {
				if trackHasOpenChannel {
					version = "↑"
				} else {
					version = "–" // that's an en dash (so yaml is happy)
				}
			}
			fmt.Fprintf(w, "  %s:\t%s\t%s\t%s\t%s\n", chName, version, revision, size, notes)
		}
	}
}

func formatSummary(raw string) string {
	s, err := yaml.Marshal(raw)
	if err != nil {
		return fmt.Sprintf("cannot marshal summary: %s", err)
	}
	return strings.TrimSpace(string(s))
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
			if len(x.Positional.Snaps) == 1 {
				return fmt.Errorf("no snap found for %q", snapName)
			}

			fmt.Fprintf(w, fmt.Sprintf(i18n.G("warning:\tno snap found for %q\n"), snapName))
			continue
		}
		noneOK = false

		fmt.Fprintf(w, "name:\t%s\n", both.Name)
		fmt.Fprintf(w, "summary:\t%s\n", formatSummary(both.Summary))
		// TODO: have publisher; use publisher here,
		// and additionally print developer if publisher != developer
		fmt.Fprintf(w, "publisher:\t%s\n", both.Developer)
		if both.Contact != "" {
			fmt.Fprintf(w, "contact:\t%s\n", strings.TrimPrefix(both.Contact, "mailto:"))
		}
		license := both.License
		if license == "" {
			license = "- (undefined)"
		}
		fmt.Fprintf(w, "license:\t%s\n", license)
		maybePrintPrice(w, remote, resInfo)
		// FIXME: find out for real
		termWidth := 77
		fmt.Fprintf(w, "description: |\n%s\n", formatDescr(both.Description, termWidth))
		maybePrintType(w, both.Type)
		maybePrintID(w, both)
		maybePrintCommands(w, snapName, both.Apps, termWidth)
		maybePrintServices(w, snapName, both.Apps, termWidth)

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

				fmt.Fprintf(w, "  ignore-validation:\t%t\n", local.IgnoreValidation)
			} else {
				notes = NotesFromLocal(local)
			}

			fmt.Fprintf(w, "tracking:\t%s\n", local.TrackingChannel)
			fmt.Fprintf(w, "installed:\t%s\t(%s)\t%s\t%s\n", local.Version, local.Revision, strutil.SizeToStr(local.InstalledSize), notes)
			fmt.Fprintf(w, "refreshed:\t%s\n", local.InstallDate)
		}

		if remote != nil && remote.Channels != nil && remote.Tracks != nil {
			displayChannels(w, remote)
		}

	}
	w.Flush()

	if noneOK {
		return fmt.Errorf(i18n.G("no valid snaps given"))
	}

	return nil
}
