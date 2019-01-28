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
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jessevdk/go-flags"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/strutil"
)

type infoCmd struct {
	clientMixin
	colorMixin
	timeMixin

	Verbose    bool `long:"verbose"`
	Positional struct {
		Snaps []anySnapName `positional-arg-name:"<snap>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

var shortInfoHelp = i18n.G("Show detailed information about snaps")
var longInfoHelp = i18n.G(`
The info command shows detailed information about snaps.

The snaps can be specified by name or by path; names are looked for both in the
store and in the installed snaps; paths can refer to a .snap file, or to a
directory that contains an unpacked snap suitable for 'snap try' (an example
of this would be the 'prime' directory snapcraft produces).
`)

func init() {
	addCommand("info",
		shortInfoHelp,
		longInfoHelp,
		func() flags.Commander {
			return &infoCmd{}
		}, colorDescs.also(timeDescs).also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"verbose": i18n.G("Include more details on the snap (expanded notes, base, etc.)"),
		}), nil)
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

func maybePrintBase(w io.Writer, base string, verbose bool) {
	if verbose && base != "" {
		fmt.Fprintf(w, "base:\t%s\n", base)
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
	fmt.Fprintf(w, "name:\t%s\n", info.InstanceName())
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
	maybePrintBase(w, info.Base, verbose)
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

// runesTrimRightSpace returns text, with any trailing whitespace dropped.
func runesTrimRightSpace(text []rune) []rune {
	j := len(text)
	for j > 0 && unicode.IsSpace(text[j-1]) {
		j--
	}
	return text[:j]
}

// runesLastIndexSpace returns the index of the last whitespace rune
// in the text. If the text has no whitespace, returns -1.
func runesLastIndexSpace(text []rune) int {
	for i := len(text) - 1; i >= 0; i-- {
		if unicode.IsSpace(text[i]) {
			return i
		}
	}
	return -1
}

// wrapLine wraps a line to fit into width, preserving the line's indent, and
// writes it out prepending padding to each line.
func wrapLine(out io.Writer, text []rune, pad string, width int) error {
	// Note: this is _wrong_ for much of unicode (because the width of a rune on
	//       the terminal is anything between 0 and 2, not always 1 as this code
	//       assumes) but fixing that is Hard. Long story short, you can get close
	//       using a couple of big unicode tables (which is what wcwidth
	//       does). Getting it 100% requires a terminfo-alike of unicode behaviour.
	//       However, before this we'd count bytes instead of runes, so we'd be
	//       even more broken. Think of it as successive approximations... at least
	//       with this work we share tabwriter's opinion on the width of things!

	// This (and possibly printDescr below) should move to strutil once
	// we're happy with it getting wider (heh heh) use.

	// discard any trailing whitespace
	text = runesTrimRightSpace(text)
	// establish the indent of the whole block
	idx := 0
	for idx < len(text) && unicode.IsSpace(text[idx]) {
		idx++
	}
	indent := pad + string(text[:idx])
	text = text[idx:]
	width -= idx + utf8.RuneCountInString(pad)
	var err error
	for len(text) > width && err == nil {
		// find a good place to chop the text
		idx = runesLastIndexSpace(text[:width+1])
		if idx < 0 {
			// there's no whitespace; just chop at line width
			idx = width
		}
		_, err = fmt.Fprint(out, indent, string(text[:idx]), "\n")
		// prune any remaining whitespace before the start of the next line
		for idx < len(text) && unicode.IsSpace(text[idx]) {
			idx++
		}
		text = text[idx:]
	}
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(out, indent, string(text), "\n")
	return err
}

// printDescr formats a given string (typically a snap description)
// in a user friendly way.
//
// The rules are (intentionally) very simple:
// - trim trailing whitespace
// - word wrap at "max" chars preserving line indent
// - keep \n intact and break there
func printDescr(w io.Writer, descr string, max int) error {
	var err error
	descr = strings.TrimRightFunc(descr, unicode.IsSpace)
	for _, line := range strings.Split(descr, "\n") {
		err = wrapLine(w, []rune(line), "  ", max)
		if err != nil {
			break
		}
	}
	return err
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

var channelRisks = []string{"stable", "candidate", "beta", "edge"}

type channelInfo struct {
	indent, name, version, released, revision, size, notes string
}

type channelInfos struct {
	channels              []*channelInfo
	maxRevLen, maxSizeLen int
	releasedfmt, chantpl  string
	needsHeader           bool
	esc                   *escapes
}

func (chInfos *channelInfos) add(indent, name, version string, revision snap.Revision, released time.Time, size int64, notes *Notes) {
	chInfo := &channelInfo{
		indent:   indent,
		name:     name,
		version:  version,
		revision: fmt.Sprintf("(%s)", revision),
		size:     strutil.SizeToStr(size),
		notes:    notes.String(),
	}
	if !released.IsZero() {
		chInfo.released = released.Format(chInfos.releasedfmt)
	}
	if len(chInfo.revision) > chInfos.maxRevLen {
		chInfos.maxRevLen = len(chInfo.revision)
	}
	if len(chInfo.size) > chInfos.maxSizeLen {
		chInfos.maxSizeLen = len(chInfo.size)
	}
	chInfos.channels = append(chInfos.channels, chInfo)
}

func (chInfos *channelInfos) addFromLocal(local *client.Snap) {
	chInfos.add("", "installed", local.Version, local.Revision, time.Time{}, local.InstalledSize, NotesFromLocal(local))
}

func (chInfos *channelInfos) addOpenChannel(name, version string, revision snap.Revision, released time.Time, size int64, notes *Notes) {
	chInfos.add("  ", name, version, revision, released, size, notes)
}

func (chInfos *channelInfos) addClosedChannel(name string, trackHasOpenChannel bool) {
	chInfo := &channelInfo{indent: "  ", name: name}
	if trackHasOpenChannel {
		chInfo.version = chInfos.esc.uparrow
	} else {
		chInfo.version = chInfos.esc.dash
	}

	chInfos.channels = append(chInfos.channels, chInfo)
}

func (chInfos *channelInfos) addFromRemote(remote *client.Snap) {
	// order by tracks
	for _, tr := range remote.Tracks {
		trackHasOpenChannel := false
		for _, risk := range channelRisks {
			chName := fmt.Sprintf("%s/%s", tr, risk)
			ch, ok := remote.Channels[chName]
			if tr == "latest" {
				chName = risk
			}
			if ok {
				chInfos.addOpenChannel(chName, ch.Version, ch.Revision, ch.ReleasedAt, ch.Size, NotesFromChannelSnapInfo(ch))
				trackHasOpenChannel = true
			} else {
				chInfos.addClosedChannel(chName, trackHasOpenChannel)
			}
		}
	}
	chInfos.needsHeader = len(chInfos.channels) > 0
}

func (chInfos *channelInfos) dump(w io.Writer) {
	if chInfos.needsHeader {
		fmt.Fprintln(w, "channels:")
	}
	for _, c := range chInfos.channels {
		fmt.Fprintf(w, chInfos.chantpl, c.indent, c.name, c.version, c.released, chInfos.maxRevLen, c.revision, chInfos.maxSizeLen, c.size, c.notes)
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
	termWidth, _ := termSize()
	termWidth -= 3
	if termWidth > 100 {
		// any wider than this and it gets hard to read
		termWidth = 100
	}

	esc := x.getEscapes()
	w := tabwriter.NewWriter(Stdout, 2, 2, 1, ' ', 0)

	noneOK := true
	for i, snapName := range x.Positional.Snaps {
		snapName := string(snapName)
		if i > 0 {
			fmt.Fprintln(w, "---")
		}
		if snapName == "system" {
			fmt.Fprintln(w, "system: You can't have it.")
			continue
		}

		if tryDirect(w, snapName, x.Verbose) {
			noneOK = false
			continue
		}
		remote, resInfo, _ := x.client.FindOne(snapName)
		local, _, _ := x.client.Snap(snapName)

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
		fmt.Fprintf(w, "publisher:\t%s\n", longPublisher(esc, both.Publisher))
		if both.Contact != "" {
			fmt.Fprintf(w, "contact:\t%s\n", strings.TrimPrefix(both.Contact, "mailto:"))
		}
		license := both.License
		if license == "" {
			license = "unset"
		}
		fmt.Fprintf(w, "license:\t%s\n", license)
		maybePrintPrice(w, remote, resInfo)
		fmt.Fprintln(w, "description: |")
		printDescr(w, both.Description, termWidth)
		maybePrintCommands(w, snapName, both.Apps, termWidth)
		maybePrintServices(w, snapName, both.Apps, termWidth)

		if x.Verbose {
			fmt.Fprintln(w, "notes:\t")
			fmt.Fprintf(w, "  private:\t%t\n", both.Private)
			fmt.Fprintf(w, "  confinement:\t%s\n", both.Confinement)
		}

		if local != nil {
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
			}
		}
		// stops the notes etc trying to be aligned with channels
		w.Flush()
		maybePrintType(w, both.Type)
		maybePrintBase(w, both.Base, x.Verbose)
		maybePrintID(w, both)
		if local != nil {
			if local.TrackingChannel != "" {
				fmt.Fprintf(w, "tracking:\t%s\n", local.TrackingChannel)
			}
			if !local.InstallDate.IsZero() {
				fmt.Fprintf(w, "refresh-date:\t%s\n", x.fmtTime(local.InstallDate))
			}
		}

		chInfos := channelInfos{
			chantpl:     "%s%s:\t%s %s%*s %*s %s\n",
			releasedfmt: "2006-01-02",
			esc:         esc,
		}
		if x.AbsTime {
			chInfos.releasedfmt = time.RFC3339
		}
		if remote != nil && remote.Channels != nil && remote.Tracks != nil {
			w.Flush()
			chInfos.chantpl = "%s%s:\t%s\t%s\t%*s\t%*s\t%s\n"
			chInfos.addFromRemote(remote)
		}
		if local != nil {
			chInfos.addFromLocal(local)
		}
		chInfos.dump(w)
	}
	w.Flush()

	if noneOK {
		return fmt.Errorf(i18n.G("no valid snaps given"))
	}

	return nil
}
