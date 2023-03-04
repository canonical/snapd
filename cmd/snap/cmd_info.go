// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"
	"unicode"

	"github.com/jessevdk/go-flags"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/client/clientutil"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/squashfs"
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

func clientSnapFromPath(path string) (*client.Snap, error) {
	snapf, err := snapfile.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := snap.ReadInfoFromSnapFile(snapf, nil)
	if err != nil {
		return nil, err
	}

	direct, err := clientutil.ClientSnapFromSnapInfo(info, nil)
	if err != nil {
		return nil, err
	}

	return direct, nil
}

func norm(path string) string {
	path = filepath.Clean(path)
	if osutil.IsDirectory(path) {
		path = path + "/"
	}

	return path
}

// wrapFlow wraps the text using yaml's flow style, allowing indent
// characters for the first line.
func wrapFlow(out io.Writer, text []rune, indent string, termWidth int) error {
	return strutil.WordWrap(out, text, indent, "  ", termWidth)
}

func quotedIfNeeded(raw string) []rune {
	// simplest way of checking to see if it needs quoting is to try
	raw = strings.TrimSpace(raw)
	type T struct {
		S string
	}
	if len(raw) == 0 {
		raw = `""`
	} else if err := yaml.UnmarshalStrict([]byte("s: "+raw), &T{}); err != nil {
		raw = strconv.Quote(raw)
	}
	return []rune(raw)
}

// printDescr formats a given string (typically a snap description)
// in a user friendly way.
//
// The rules are (intentionally) very simple:
// - trim trailing whitespace
// - word wrap at "max" chars preserving line indent
// - keep \n intact and break there
func printDescr(w io.Writer, descr string, termWidth int) error {
	var err error
	descr = strings.TrimRightFunc(descr, unicode.IsSpace)
	for _, line := range strings.Split(descr, "\n") {
		err = strutil.WordWrapPadded(w, []rune(line), "  ", termWidth)
		if err != nil {
			break
		}
	}
	return err
}

type writeflusher interface {
	io.Writer
	Flush() error
}

type infoWriter struct {
	// fields that are set every iteration
	theSnap    *client.Snap
	diskSnap   *client.Snap
	localSnap  *client.Snap
	remoteSnap *client.Snap
	resInfo    *client.ResultInfo
	path       string
	// fields that don't change and so can be set once
	writeflusher
	esc       *escapes
	termWidth int
	fmtTime   func(time.Time) string
	absTime   bool
	verbose   bool
}

func (iw *infoWriter) setupDiskSnap(path string, diskSnap *client.Snap) {
	iw.localSnap, iw.remoteSnap, iw.resInfo = nil, nil, nil
	iw.path = path
	iw.diskSnap = diskSnap
	iw.theSnap = diskSnap
}

func (iw *infoWriter) setupSnap(localSnap, remoteSnap *client.Snap, resInfo *client.ResultInfo) {
	iw.path, iw.diskSnap = "", nil
	iw.localSnap = localSnap
	iw.remoteSnap = remoteSnap
	iw.resInfo = resInfo
	if localSnap != nil {
		iw.theSnap = localSnap
	} else {
		iw.theSnap = remoteSnap
	}
}

func (iw *infoWriter) maybePrintPrice() {
	if iw.resInfo == nil {
		return
	}
	price, currency, err := getPrice(iw.remoteSnap.Prices, iw.resInfo.SuggestedCurrency)
	if err != nil {
		return
	}
	fmt.Fprintf(iw, "price:\t%s\n", formatPrice(price, currency))
}

func (iw *infoWriter) maybePrintType() {
	// XXX: using literals here until we reshuffle snap & client properly
	// (and os->core rename happens, etc)
	t := iw.theSnap.Type
	switch t {
	case "", "app", "application":
		return
	case "os":
		t = "core"
	}

	fmt.Fprintf(iw, "type:\t%s\n", t)
}

func (iw *infoWriter) maybePrintID() {
	if iw.theSnap.ID != "" {
		fmt.Fprintf(iw, "snap-id:\t%s\n", iw.theSnap.ID)
	}
}

func (iw *infoWriter) maybePrintHealth() {
	if iw.localSnap == nil {
		return
	}
	health := iw.localSnap.Health
	if health == nil {
		if !iw.verbose {
			return
		}
		health = &client.SnapHealth{
			Status:  "unknown",
			Message: "health has not been set",
		}
	}
	if health.Status == "okay" && !iw.verbose {
		return
	}

	fmt.Fprintln(iw, "health:")
	fmt.Fprintf(iw, "  status:\t%s\n", health.Status)
	if health.Message != "" {
		strutil.WordWrap(iw, quotedIfNeeded(health.Message), "  message:\t", "    ", iw.termWidth)
	}
	if health.Code != "" {
		fmt.Fprintf(iw, "  code:\t%s\n", health.Code)
	}
	if !health.Timestamp.IsZero() {
		fmt.Fprintf(iw, "  checked:\t%s\n", iw.fmtTime(health.Timestamp))
	}
	if !health.Revision.Unset() {
		fmt.Fprintf(iw, "  revision:\t%s\n", health.Revision)
	}
	iw.Flush()
}

func (iw *infoWriter) maybePrintTrackingChannel() {
	if iw.localSnap == nil {
		return
	}
	if iw.localSnap.TrackingChannel == "" {
		return
	}
	fmt.Fprintf(iw, "tracking:\t%s\n", iw.localSnap.TrackingChannel)
}

func (iw *infoWriter) maybePrintRefreshInfo() {
	if iw.localSnap == nil {
		return
	}

	if !iw.localSnap.InstallDate.IsZero() {
		fmt.Fprintf(iw, "refresh-date:\t%s\n", iw.fmtTime(iw.localSnap.InstallDate))
	}

	maybePrintHold := func(key string, hold *time.Time) {
		if hold == nil || hold.Before(timeNow()) {
			return
		}

		longTime := timeNow().Add(100 * 365 * 24 * time.Hour)
		if hold.After(longTime) {
			fmt.Fprintf(iw, "%s:\tforever\n", key)
		} else {
			fmt.Fprintf(iw, "%s:\t%s\n", key, iw.fmtTime(*hold))
		}
	}

	maybePrintHold("hold", iw.localSnap.Hold)
	maybePrintHold("hold-by-gating", iw.localSnap.GatingHold)
}

func (iw *infoWriter) maybePrintChinfo() {
	if iw.diskSnap != nil {
		return
	}
	chInfos := channelInfos{
		chantpl:     "%s%s:\t%s %s%*s %*s %s\n",
		releasedfmt: "2006-01-02",
		esc:         iw.esc,
	}
	if iw.absTime {
		chInfos.releasedfmt = time.RFC3339
	}
	if iw.remoteSnap != nil && iw.remoteSnap.Channels != nil && iw.remoteSnap.Tracks != nil {
		iw.Flush()
		chInfos.chantpl = "%s%s:\t%s\t%s\t%*s\t%*s\t%s\n"
		chInfos.addFromRemote(iw.remoteSnap)
	}
	if iw.localSnap != nil {
		chInfos.addFromLocal(iw.localSnap)
	}
	chInfos.dump(iw)
}

func (iw *infoWriter) maybePrintBase() {
	if iw.verbose && iw.theSnap.Base != "" {
		fmt.Fprintf(iw, "base:\t%s\n", iw.theSnap.Base)
	}
}

func (iw *infoWriter) maybePrintPath() {
	if iw.path != "" {
		fmt.Fprintf(iw, "path:\t%q\n", iw.path)
	}
}

func (iw *infoWriter) printName() {
	fmt.Fprintf(iw, "name:\t%s\n", iw.theSnap.Name)
}

func (iw *infoWriter) printSummary() {
	wrapFlow(iw, quotedIfNeeded(iw.theSnap.Summary), "summary:\t", iw.termWidth)
}

func (iw *infoWriter) maybePrintStoreURL() {
	storeURL := ""
	// XXX: store-url for local snaps comes from aux data, but that gets
	// updated only when the snap is refreshed, be smart and poke remote
	// snap info if available
	switch {
	case iw.theSnap.StoreURL != "":
		storeURL = iw.theSnap.StoreURL
	case iw.remoteSnap != nil && iw.remoteSnap.StoreURL != "":
		storeURL = iw.remoteSnap.StoreURL
	}
	if storeURL == "" {
		return
	}
	fmt.Fprintf(iw, "store-url:\t%s\n", storeURL)
}

func (iw *infoWriter) maybePrintPublisher() {
	if iw.diskSnap != nil {
		// snaps read from disk won't have a publisher
		return
	}
	fmt.Fprintf(iw, "publisher:\t%s\n", longPublisher(iw.esc, iw.theSnap.Publisher))
}

func (iw *infoWriter) maybePrintStandaloneVersion() {
	if iw.diskSnap == nil {
		// snaps not read from disk will have version information shown elsewhere
		return
	}
	version := iw.diskSnap.Version
	if version == "" {
		version = iw.esc.dash
	}
	// NotesFromRemote might be better called NotesFromNotInstalled but that's nasty
	fmt.Fprintf(iw, "version:\t%s %s\n", version, NotesFromRemote(iw.diskSnap, nil))
}

func (iw *infoWriter) maybePrintBuildDate() {
	if iw.diskSnap == nil {
		return
	}
	if osutil.IsDirectory(iw.path) {
		return
	}
	buildDate := squashfs.BuildDate(iw.path)
	if buildDate.IsZero() {
		return
	}
	fmt.Fprintf(iw, "build-date:\t%s\n", iw.fmtTime(buildDate))
}

func (iw *infoWriter) maybePrintLinks() {
	contact := strings.TrimPrefix(iw.theSnap.Contact, "mailto:")
	if contact != "" {
		fmt.Fprintf(iw, "contact:\t%s\n", contact)
	}
	if !iw.verbose || len(iw.theSnap.Links) == 0 {
		return
	}
	links := iw.theSnap.Links
	fmt.Fprintln(iw, "links:")
	linkKeys := make([]string, 0, len(iw.theSnap.Links))
	for k := range links {
		linkKeys = append(linkKeys, k)
	}
	sort.Strings(linkKeys)
	for _, k := range linkKeys {
		fmt.Fprintf(iw, "  %s:\n", k)
		for _, v := range links[k] {
			fmt.Fprintf(iw, "    - %s\n", v)
		}
	}
}

func (iw *infoWriter) printLicense() {
	license := iw.theSnap.License
	if license == "" {
		license = "unset"
	}
	fmt.Fprintf(iw, "license:\t%s\n", license)
}

func (iw *infoWriter) printDescr() {
	fmt.Fprintln(iw, "description: |")
	printDescr(iw, iw.theSnap.Description, iw.termWidth)
}

func (iw *infoWriter) maybePrintCommands() {
	if len(iw.theSnap.Apps) == 0 {
		return
	}

	commands := make([]string, 0, len(iw.theSnap.Apps))
	for _, app := range iw.theSnap.Apps {
		if app.IsService() {
			continue
		}

		cmdStr := snap.JoinSnapApp(iw.theSnap.Name, app.Name)
		commands = append(commands, cmdStr)
	}
	if len(commands) == 0 {
		return
	}

	fmt.Fprintf(iw, "commands:\n")
	for _, cmd := range commands {
		fmt.Fprintf(iw, "  - %s\n", cmd)
	}
}

func (iw *infoWriter) maybePrintServices() {
	if len(iw.theSnap.Apps) == 0 {
		return
	}

	services := make([]string, 0, len(iw.theSnap.Apps))
	for _, app := range iw.theSnap.Apps {
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
		services = append(services, fmt.Sprintf("  %s:\t%s, %s, %s", snap.JoinSnapApp(iw.theSnap.Name, app.Name), app.Daemon, enabled, active))
	}
	if len(services) == 0 {
		return
	}

	fmt.Fprintf(iw, "services:\n")
	for _, svc := range services {
		fmt.Fprintln(iw, svc)
	}
}

func (iw *infoWriter) maybePrintNotes() {
	if !iw.verbose {
		return
	}
	fmt.Fprintln(iw, "notes:\t")
	fmt.Fprintf(iw, "  private:\t%t\n", iw.theSnap.Private)
	fmt.Fprintf(iw, "  confinement:\t%s\n", iw.theSnap.Confinement)
	if iw.localSnap == nil {
		return
	}
	jailMode := iw.localSnap.Confinement == client.DevModeConfinement && !iw.localSnap.DevMode
	fmt.Fprintf(iw, "  devmode:\t%t\n", iw.localSnap.DevMode)
	fmt.Fprintf(iw, "  jailmode:\t%t\n", jailMode)
	fmt.Fprintf(iw, "  trymode:\t%t\n", iw.localSnap.TryMode)
	fmt.Fprintf(iw, "  enabled:\t%t\n", iw.localSnap.Status == client.StatusActive)
	if iw.localSnap.Broken == "" {
		fmt.Fprintf(iw, "  broken:\t%t\n", false)
	} else {
		fmt.Fprintf(iw, "  broken:\t%t (%s)\n", true, iw.localSnap.Broken)
	}

	fmt.Fprintf(iw, "  ignore-validation:\t%t\n", iw.localSnap.IgnoreValidation)
}

func (iw *infoWriter) maybePrintCohortKey() {
	if !iw.verbose {
		return
	}
	if iw.localSnap == nil {
		return
	}
	coh := iw.localSnap.CohortKey
	if coh == "" {
		return
	}
	if isStdoutTTY {
		// 15 is 1 + the length of "refresh-date: "
		coh = strutil.ElliptLeft(iw.localSnap.CohortKey, iw.termWidth-15)
	}
	fmt.Fprintf(iw, "cohort:\t%s\n", coh)
}

func (iw *infoWriter) maybePrintSum() {
	if !iw.verbose {
		return
	}
	if iw.diskSnap == nil {
		// TODO: expose the sha via /v2/snaps and /v2/find
		return
	}
	if osutil.IsDirectory(iw.path) {
		// no sha3_384 of a directory :-)
		return
	}
	sha3_384, _, _ := asserts.SnapFileSHA3_384(iw.path)
	if sha3_384 == "" {
		return
	}
	fmt.Fprintf(iw, "sha3-384:\t%s\n", sha3_384)
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

func (x *infoCmd) Execute([]string) error {
	termWidth, _ := termSize()
	termWidth -= 3
	if termWidth > 100 {
		// any wider than this and it gets hard to read
		termWidth = 100
	}

	esc := x.getEscapes()
	w := tabwriter.NewWriter(Stdout, 2, 2, 1, ' ', 0)
	iw := &infoWriter{
		writeflusher: w,
		esc:          esc,
		termWidth:    termWidth,
		verbose:      x.Verbose,
		fmtTime:      x.fmtTime,
		absTime:      x.AbsTime,
	}

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

		if diskSnap, err := clientSnapFromPath(snapName); err == nil {
			iw.setupDiskSnap(norm(snapName), diskSnap)
		} else {
			remoteSnap, resInfo, _ := x.client.FindOne(snap.InstanceSnap(snapName))
			localSnap, _, _ := x.client.Snap(snapName)
			iw.setupSnap(localSnap, remoteSnap, resInfo)
		}
		// note diskSnap == nil, or localSnap == nil and remoteSnap == nil

		if iw.theSnap == nil {
			if len(x.Positional.Snaps) == 1 {
				w.Flush()
				return fmt.Errorf("no snap found for %q", snapName)
			}

			fmt.Fprintf(w, fmt.Sprintf(i18n.G("warning:\tno snap found for %q\n"), snapName))
			continue
		}
		noneOK = false

		iw.maybePrintPath()
		iw.printName()
		iw.printSummary()
		iw.maybePrintHealth()
		iw.maybePrintPublisher()
		iw.maybePrintStoreURL()
		iw.maybePrintStandaloneVersion()
		iw.maybePrintBuildDate()
		iw.maybePrintLinks()
		iw.printLicense()
		iw.maybePrintPrice()
		iw.printDescr()
		iw.maybePrintCommands()
		iw.maybePrintServices()
		iw.maybePrintNotes()
		// stops the notes etc trying to be aligned with channels
		iw.Flush()
		iw.maybePrintType()
		iw.maybePrintBase()
		iw.maybePrintSum()
		iw.maybePrintID()
		iw.maybePrintCohortKey()
		iw.maybePrintTrackingChannel()
		iw.maybePrintRefreshInfo()
		iw.maybePrintChinfo()
	}
	w.Flush()

	if noneOK {
		return fmt.Errorf(i18n.G("no valid snaps given"))
	}

	return nil
}
