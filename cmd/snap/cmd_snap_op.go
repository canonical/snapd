// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2017 Canonical Ltd
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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/strutil"
)

var (
	shortInstallHelp = i18n.G("Install snaps on the system")
	shortRemoveHelp  = i18n.G("Remove snaps from the system")
	shortRefreshHelp = i18n.G("Refresh snaps in the system")
	shortTryHelp     = i18n.G("Test an unpacked snap in the system")
	shortEnableHelp  = i18n.G("Enable a snap in the system")
	shortDisableHelp = i18n.G("Disable a snap in the system")
)

var longInstallHelp = i18n.G(`
The install command installs the named snaps on the system.

To install multiple instances of the same snap, append an underscore and a
unique identifier (for each instance) to a snap's name.

With no further options, the snaps are installed tracking the stable channel,
with strict security confinement. All available channels of a snap are listed in
its 'snap info' output.

Revision choice via the --revision override requires the user to
have developer access to the snap, either directly or through the
store's collaboration feature, and to be logged in (see 'snap help login').

Note that a later refresh will typically undo a revision override, taking the snap
back to the current revision of the channel it's tracking.

Use --name to set the instance name when installing from snap file.
`)

var longRemoveHelp = i18n.G(`
The remove command removes the named snap instance from the system.

By default all the snap revisions are removed, including their data and the
common data directory. When a --revision option is passed only the specified
revision is removed.

Unless automatic snapshots are disabled, a snapshot of all data for the snap is 
saved upon removal, which is then available for future restoration with snap
restore. The --purge option disables automatically creating snapshots.
`)

var longRefreshHelp = i18n.G(`
The refresh command updates the specified snaps, or all snaps in the system if
none are specified.

With no further options, the snaps are refreshed to the current revision of the
channel they're tracking, preserving their confinement options. All available
channels of a snap are listed in its 'snap info' output.

Revision choice via the --revision override requires the user to
have developer access to the snap, either directly or through the
store's collaboration feature, and to be logged in (see 'snap help login').

Note a later refresh will typically undo a revision override.
`)

var longTryHelp = i18n.G(`
The try command installs an unpacked snap into the system for testing purposes.
The unpacked snap content continues to be used even after installation, so
non-metadata changes there go live instantly. Metadata changes such as those
performed in snap.yaml will require reinstallation to go live.

If snap-dir argument is omitted, the try command will attempt to infer it if
either snapcraft.yaml file and prime directory or meta/snap.yaml file can be
found relative to current working directory.
`)

var longEnableHelp = i18n.G(`
The enable command enables a snap that was previously disabled.
`)

var longDisableHelp = i18n.G(`
The disable command disables a snap. The binaries and services of the
snap will no longer be available, but all the data is still available
and the snap can easily be enabled again.
`)

type cmdRemove struct {
	waitMixin

	Revision   string `long:"revision"`
	Purge      bool   `long:"purge"`
	Positional struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdRemove) removeOne(opts *client.SnapOptions) error {
	name := string(x.Positional.Snaps[0])

	changeID, err := x.client.Remove(name, opts)
	if err != nil {
		msg, err := errorToCmdMessage(name, err, opts)
		if err != nil {
			return err
		}
		fmt.Fprintln(Stderr, msg)
		return nil
	}

	if _, err := x.wait(changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	if opts.Revision != "" {
		fmt.Fprintf(Stdout, i18n.G("%s (revision %s) removed\n"), name, opts.Revision)
	} else {
		fmt.Fprintf(Stdout, i18n.G("%s removed\n"), name)
	}
	return nil
}

func (x *cmdRemove) removeMany(opts *client.SnapOptions) error {
	names := installedSnapNames(x.Positional.Snaps)
	changeID, err := x.client.RemoveMany(names, opts)
	if err != nil {
		return err
	}

	chg, err := x.wait(changeID)
	if err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	var removed []string
	if err := chg.Get("snap-names", &removed); err != nil && err != client.ErrNoData {
		return err
	}

	seen := make(map[string]bool)
	for _, name := range removed {
		fmt.Fprintf(Stdout, i18n.G("%s removed\n"), name)
		seen[name] = true
	}
	for _, name := range names {
		if !seen[name] {
			// FIXME: this is the only reason why a name can be
			// skipped, but it does feel awkward
			fmt.Fprintf(Stdout, i18n.G("%s not installed\n"), name)
		}
	}

	return nil

}

func (x *cmdRemove) Execute([]string) error {
	opts := &client.SnapOptions{Revision: x.Revision, Purge: x.Purge}
	if len(x.Positional.Snaps) == 1 {
		return x.removeOne(opts)
	}

	if x.Purge || x.Revision != "" {
		return errors.New(i18n.G("a single snap name is needed to specify options"))
	}
	return x.removeMany(nil)
}

type channelMixin struct {
	Channel string `long:"channel"`

	// shortcuts
	EdgeChannel      bool `long:"edge"`
	BetaChannel      bool `long:"beta"`
	CandidateChannel bool `long:"candidate"`
	StableChannel    bool `long:"stable" `
}

type mixinDescs map[string]string

func (mxd mixinDescs) also(m map[string]string) mixinDescs {
	n := make(map[string]string, len(mxd)+len(m))
	for k, v := range mxd {
		n[k] = v
	}
	for k, v := range m {
		n[k] = v
	}
	return n
}

var channelDescs = mixinDescs{
	// TRANSLATORS: This should not start with a lowercase letter.
	"channel": i18n.G("Use this channel instead of stable"),
	// TRANSLATORS: This should not start with a lowercase letter.
	"beta": i18n.G("Install from the beta channel"),
	// TRANSLATORS: This should not start with a lowercase letter.
	"edge": i18n.G("Install from the edge channel"),
	// TRANSLATORS: This should not start with a lowercase letter.
	"candidate": i18n.G("Install from the candidate channel"),
	// TRANSLATORS: This should not start with a lowercase letter.
	"stable": i18n.G("Install from the stable channel"),
}

func (mx *channelMixin) setChannelFromCommandline() error {
	for _, ch := range []struct {
		enabled bool
		chName  string
	}{
		{mx.StableChannel, "stable"},
		{mx.CandidateChannel, "candidate"},
		{mx.BetaChannel, "beta"},
		{mx.EdgeChannel, "edge"},
	} {
		if !ch.enabled {
			continue
		}
		if mx.Channel != "" {
			return fmt.Errorf("Please specify a single channel")
		}
		mx.Channel = ch.chName
	}

	if mx.Channel != "" {
		if _, err := channel.Parse(mx.Channel, ""); err != nil {
			full, er := channel.Full(mx.Channel)
			if er != nil {
				// the parse error has more detailed info
				return err
			}

			// TODO: get escapes in here so we can bold the Warning
			head := i18n.G("Warning:")
			msg := i18n.G("Specifying a channel %q is relying on undefined behaviour. Interpreting it as %q for now, but this will be an error later.\n")
			warn := fill(fmt.Sprintf(msg, mx.Channel, full), utf8.RuneCountInString(head)+1) // +1 for the space
			fmt.Fprint(Stderr, head, " ", warn, "\n\n")
			mx.Channel = full // so a malformed-but-eh channel will always be full, i.e. //stable// -> latest/stable
		}
	}

	return nil
}

// isSnapInPath checks whether the snap binaries dir (e.g. /snap/bin)
// is in $PATH.
//
// TODO: consider symlinks
func isSnapInPath() bool {
	paths := filepath.SplitList(os.Getenv("PATH"))
	for _, path := range paths {
		if filepath.Clean(path) == dirs.SnapBinariesDir {
			return true
		}
	}
	return false
}

func isSameRisk(tracking, current string) (bool, error) {
	if tracking == current {
		return true, nil
	}
	var trackingRisk, currentRisk string
	if tracking != "" {
		traCh, err := channel.Parse(tracking, "")
		if err != nil {
			return false, err
		}
		trackingRisk = traCh.Risk
	}
	if current != "" {
		curCh, err := channel.Parse(current, "")
		if err != nil {
			return false, err
		}
		currentRisk = curCh.Risk
	}
	return trackingRisk == currentRisk, nil
}

func maybeWithSudoSecurePath() bool {
	// Some distributions set secure_path to a known list of paths in
	// /etc/sudoers. The snapd package currently has no means of overwriting
	// or extending that setting.
	if _, isSet := os.LookupEnv("SUDO_UID"); !isSet {
		return false
	}
	// Known distros setting secure_path that does not include
	// $SNAP_MOUNT_DIR/bin:
	return release.DistroLike("fedora", "opensuse", "debian")
}

// show what has been done
func showDone(cli *client.Client, names []string, op string, opts *client.SnapOptions, esc *escapes) error {
	snaps, err := cli.List(names, nil)
	if err != nil {
		return err
	}

	needsPathWarning := !isSnapInPath() && !maybeWithSudoSecurePath()
	for _, snap := range snaps {
		channelStr := ""
		if snap.Channel != "" {
			ch, err := channel.Parse(snap.Channel, "")
			if err != nil {
				return err
			}
			if ch.Name != "stable" {
				channelStr = fmt.Sprintf(" (%s)", ch.Name)
			}
		}
		switch op {
		case "install":
			if needsPathWarning {
				head := i18n.G("Warning:")
				warn := fill(fmt.Sprintf(i18n.G("%s was not found in your $PATH. If you've not restarted your session since you installed snapd, try doing that. Please see https://forum.snapcraft.io/t/9469 for more details."), dirs.SnapBinariesDir), utf8.RuneCountInString(head)+1) // +1 for the space
				fmt.Fprint(Stderr, esc.bold, head, esc.end, " ", warn, "\n\n")
				needsPathWarning = false
			}

			if opts != nil && opts.Classic && snap.Confinement != client.ClassicConfinement {
				// requested classic but the snap is not classic
				head := i18n.G("Warning:")
				// TRANSLATORS: the arg is a snap name (e.g. "some-snap")
				warn := fill(fmt.Sprintf(i18n.G("flag --classic ignored for strictly confined snap %s"), snap.Name), utf8.RuneCountInString(head)+1) // +1 for the space
				fmt.Fprint(Stderr, esc.bold, head, esc.end, " ", warn, "\n\n")
			}

			if snap.Publisher != nil {
				// TRANSLATORS: the args are a snap name optionally followed by a channel, then a version, then the developer name (e.g. "some-snap (beta) 1.3 from Alice installed")
				fmt.Fprintf(Stdout, i18n.G("%s%s %s from %s installed\n"), snap.Name, channelStr, snap.Version, longPublisher(esc, snap.Publisher))
			} else {
				// TRANSLATORS: the args are a snap name optionally followed by a channel, then a version (e.g. "some-snap (beta) 1.3 installed")
				fmt.Fprintf(Stdout, i18n.G("%s%s %s installed\n"), snap.Name, channelStr, snap.Version)
			}
		case "refresh":
			if snap.Publisher != nil {
				// TRANSLATORS: the args are a snap name optionally followed by a channel, then a version, then the developer name (e.g. "some-snap (beta) 1.3 from Alice refreshed")
				fmt.Fprintf(Stdout, i18n.G("%s%s %s from %s refreshed\n"), snap.Name, channelStr, snap.Version, longPublisher(esc, snap.Publisher))
			} else {
				// TRANSLATORS: the args are a snap name optionally followed by a channel, then a version (e.g. "some-snap (beta) 1.3 refreshed")
				fmt.Fprintf(Stdout, i18n.G("%s%s %s refreshed\n"), snap.Name, channelStr, snap.Version)
			}
		case "revert":
			// TRANSLATORS: first %s is a snap name, second %s is a revision
			fmt.Fprintf(Stdout, i18n.G("%s reverted to %s\n"), snap.Name, snap.Version)
		case "switch":
			switchCohort := opts.CohortKey != ""
			switchChannel := opts.Channel != ""
			var msg string
			// we have three boolean things to check, meaning 2³=8 possibilities,
			// minus 3 error cases which are handled before the call to showDone.
			switch {
			case switchCohort && !opts.LeaveCohort && !switchChannel:
				// TRANSLATORS: the first %q will be the (quoted) snap name, the second an ellipted cohort string
				msg = fmt.Sprintf(i18n.G("%q switched to the %q cohort\n"), snap.Name, strutil.ElliptLeft(opts.CohortKey, 10))
			case switchCohort && !opts.LeaveCohort && switchChannel:
				// TRANSLATORS: the first %q will be the (quoted) snap name, the second a channel, the third an ellipted cohort string
				msg = fmt.Sprintf(i18n.G("%q switched to the %q channel and the %q cohort\n"), snap.Name, snap.TrackingChannel, strutil.ElliptLeft(opts.CohortKey, 10))
			case !switchCohort && !opts.LeaveCohort && switchChannel:
				// TRANSLATORS: the first %q will be the (quoted) snap name, the second a channel
				msg = fmt.Sprintf(i18n.G("%q switched to the %q channel\n"), snap.Name, snap.TrackingChannel)
			case !switchCohort && opts.LeaveCohort && switchChannel:
				// TRANSLATORS: the first %q will be the (quoted) snap name, the second a channel
				msg = fmt.Sprintf(i18n.G("%q left the cohort, and switched to the %q channel"), snap.Name, snap.TrackingChannel)
			case !switchCohort && opts.LeaveCohort && !switchChannel:
				// TRANSLATORS: %q will be the (quoted) snap name
				msg = fmt.Sprintf(i18n.G("%q left the cohort"), snap.Name)
			}
			fmt.Fprintln(Stdout, msg)
		default:
			fmt.Fprintf(Stdout, "internal error: unknown op %q", op)
		}
		if op == "install" || op == "refresh" {
			if snap.TrackingChannel != snap.Channel && snap.Channel != "" {
				if sameRisk, err := isSameRisk(snap.TrackingChannel, snap.Channel); err == nil && !sameRisk {
					// TRANSLATORS: first %s is a channel name, following %s is a snap name, last %s is a channel name again.
					fmt.Fprintf(Stdout, i18n.G("Channel %s for %s is closed; temporarily forwarding to %s.\n"), snap.TrackingChannel, snap.Name, snap.Channel)
				}
			}
		}
	}

	return nil
}

func (mx *channelMixin) asksForChannel() bool {
	return mx.Channel != ""
}

type modeMixin struct {
	DevMode  bool `long:"devmode"`
	JailMode bool `long:"jailmode"`
	Classic  bool `long:"classic"`
}

var modeDescs = mixinDescs{
	// TRANSLATORS: This should not start with a lowercase letter.
	"classic": i18n.G("Put snap in classic mode and disable security confinement"),
	// TRANSLATORS: This should not start with a lowercase letter.
	"devmode": i18n.G("Put snap in development mode and disable security confinement"),
	// TRANSLATORS: This should not start with a lowercase letter.
	"jailmode": i18n.G("Put snap in enforced confinement mode"),
}

var errModeConflict = errors.New(i18n.G("cannot use devmode and jailmode flags together"))

func (mx modeMixin) validateMode() error {
	if mx.DevMode && mx.JailMode {
		return errModeConflict
	}
	return nil
}

func (mx modeMixin) asksForMode() bool {
	return mx.DevMode || mx.JailMode || mx.Classic
}

func (mx modeMixin) setModes(opts *client.SnapOptions) {
	opts.DevMode = mx.DevMode
	opts.JailMode = mx.JailMode
	opts.Classic = mx.Classic
}

type cmdInstall struct {
	colorMixin
	waitMixin

	channelMixin
	modeMixin
	Revision string `long:"revision"`

	Dangerous bool `long:"dangerous"`
	// alias for --dangerous, deprecated but we need to support it
	// because we released 2.14.2 with --force-dangerous
	ForceDangerous bool `long:"force-dangerous" hidden:"yes"`

	Unaliased bool `long:"unaliased"`

	Name string `long:"name"`

	Cohort           string `long:"cohort"`
	IgnoreValidation bool   `long:"ignore-validation"`
	IgnoreRunning    bool   `long:"ignore-running" hidden:"yes"`
	Positional       struct {
		Snaps []remoteSnapName `positional-arg-name:"<snap>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdInstall) installOne(nameOrPath, desiredName string, opts *client.SnapOptions) error {
	var err error
	var changeID string
	var snapName string
	var path string

	if isLocalSnap(nameOrPath) {
		path = nameOrPath
		changeID, err = x.client.InstallPath(path, x.Name, opts)
	} else {
		snapName = nameOrPath
		if desiredName != "" {
			return errors.New(i18n.G("cannot use explicit name when installing from store"))
		}
		changeID, err = x.client.Install(snapName, opts)
	}
	if err != nil {
		msg, err := errorToCmdMessage(nameOrPath, err, opts)
		if err != nil {
			return err
		}
		fmt.Fprintln(Stderr, msg)
		return nil
	}

	chg, err := x.wait(changeID)
	if err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	// extract the snapName from the change, important for sideloaded
	if path != "" {
		if err := chg.Get("snap-name", &snapName); err != nil {
			return fmt.Errorf("cannot extract the snap-name from local file %q: %s", nameOrPath, err)
		}
	}

	// TODO: mention details of the install (e.g. like switch does)
	return showDone(x.client, []string{snapName}, "install", opts, x.getEscapes())
}

func isLocalSnap(name string) bool {
	return strings.Contains(name, "/") || strings.HasSuffix(name, ".snap") || strings.Contains(name, ".snap.")
}

func (x *cmdInstall) installMany(names []string, opts *client.SnapOptions) error {
	isLocal := isLocalSnap(names[0])
	for _, name := range names {
		if isLocalSnap(name) != isLocal {
			return fmt.Errorf(i18n.G("cannot install local and store snaps at the same time"))
		}
	}

	var changeID string
	var err error

	if isLocal {
		changeID, err = x.client.InstallPathMany(names, opts)
	} else {
		if x.asksForMode() {
			return errors.New(i18n.G("cannot specify mode for multiple store snaps (only for one store snap or several local ones)"))
		}

		// install many doesn't support opts
		changeID, err = x.client.InstallMany(names, nil)
	}

	if err != nil {
		var snapName string
		if err, ok := err.(*client.Error); ok {
			snapName, _ = err.Value.(string)
		}
		msg, err := errorToCmdMessage(snapName, err, opts)
		if err != nil {
			return err
		}
		fmt.Fprintln(Stderr, msg)
		return nil
	}

	chg, err := x.wait(changeID)
	if err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	var installed []string
	if err := chg.Get("snap-names", &installed); err != nil && err != client.ErrNoData {
		return err
	}

	if len(installed) > 0 {
		if err := showDone(x.client, installed, "install", opts, x.getEscapes()); err != nil {
			return err
		}
	}

	// local installs aren't skipped if the snap is installed
	if isLocal {
		return nil
	}

	// show skipped
	seen := make(map[string]bool)
	for _, name := range installed {
		seen[name] = true
	}
	for _, name := range names {
		if !seen[name] {
			// FIXME: this is the only reason why a name can be
			// skipped, but it does feel awkward
			fmt.Fprintf(Stdout, i18n.G("%s already installed\n"), name)
		}
	}

	return nil
}

func (x *cmdInstall) Execute([]string) error {
	if err := x.setChannelFromCommandline(); err != nil {
		return err
	}
	if err := x.validateMode(); err != nil {
		return err
	}

	dangerous := x.Dangerous || x.ForceDangerous
	opts := &client.SnapOptions{
		Channel:          x.Channel,
		Revision:         x.Revision,
		Dangerous:        dangerous,
		Unaliased:        x.Unaliased,
		CohortKey:        x.Cohort,
		IgnoreValidation: x.IgnoreValidation,
		IgnoreRunning:    x.IgnoreRunning,
	}
	x.setModes(opts)

	names := remoteSnapNames(x.Positional.Snaps)
	for _, name := range names {
		if len(name) == 0 {
			return errors.New(i18n.G("cannot install snap with empty name"))
		}
	}

	if len(names) == 1 {
		return x.installOne(names[0], x.Name, opts)
	}

	if x.asksForChannel() {
		return errors.New(i18n.G("a single snap name is needed to specify channel flags"))
	}
	if x.IgnoreValidation {
		return errors.New(i18n.G("a single snap name must be specified when ignoring validation"))
	}

	if x.Name != "" {
		return errors.New(i18n.G("cannot use instance name when installing multiple snaps"))
	}
	return x.installMany(names, opts)
}

type cmdRefresh struct {
	colorMixin
	timeMixin
	waitMixin
	channelMixin
	modeMixin

	Amend            bool   `long:"amend"`
	Revision         string `long:"revision"`
	Cohort           string `long:"cohort"`
	LeaveCohort      bool   `long:"leave-cohort"`
	List             bool   `long:"list"`
	Time             bool   `long:"time"`
	IgnoreValidation bool   `long:"ignore-validation"`
	IgnoreRunning    bool   `long:"ignore-running" hidden:"yes"`
	Positional       struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
}

func (x *cmdRefresh) refreshMany(snaps []string, opts *client.SnapOptions) error {
	changeID, err := x.client.RefreshMany(snaps, opts)
	if err != nil {
		return err
	}

	chg, err := x.wait(changeID)
	if err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	var refreshed []string
	if err := chg.Get("snap-names", &refreshed); err != nil && err != client.ErrNoData {
		return err
	}

	if len(refreshed) > 0 {
		return showDone(x.client, refreshed, "refresh", opts, x.getEscapes())
	}

	fmt.Fprintln(Stderr, i18n.G("All snaps up to date."))

	return nil
}

func (x *cmdRefresh) refreshOne(name string, opts *client.SnapOptions) error {
	changeID, err := x.client.Refresh(name, opts)
	if err != nil {
		msg, err := errorToCmdMessage(name, err, opts)
		if err != nil {
			return err
		}
		fmt.Fprintln(Stderr, msg)
		return nil
	}

	if _, err := x.wait(changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	// TODO: this doesn't really tell about all the things you
	// could set while refreshing (something switch does)
	return showDone(x.client, []string{name}, "refresh", opts, x.getEscapes())
}

func parseSysinfoTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func (x *cmdRefresh) showRefreshTimes() error {
	sysinfo, err := x.client.SysInfo()
	if err != nil {
		return err
	}

	if sysinfo.Refresh.Timer != "" {
		fmt.Fprintf(Stdout, "timer: %s\n", sysinfo.Refresh.Timer)
	} else if sysinfo.Refresh.Schedule != "" {
		fmt.Fprintf(Stdout, "schedule: %s\n", sysinfo.Refresh.Schedule)
	} else {
		return errors.New("internal error: both refresh.timer and refresh.schedule are empty")
	}
	last := parseSysinfoTime(sysinfo.Refresh.Last)
	hold := parseSysinfoTime(sysinfo.Refresh.Hold)
	next := parseSysinfoTime(sysinfo.Refresh.Next)

	if !last.IsZero() {
		fmt.Fprintf(Stdout, "last: %s\n", x.fmtTime(last))
	} else {
		fmt.Fprintf(Stdout, "last: n/a\n")
	}
	if !hold.IsZero() {
		fmt.Fprintf(Stdout, "hold: %s\n", x.fmtTime(hold))
	}
	// only show "next" if its after "hold" to not confuse users
	if !next.IsZero() {
		// Snapstate checks for holdTime.After(limitTime) so we need
		// to check for before or equal here to be fully correct.
		if next.Before(hold) || next.Equal(hold) {
			fmt.Fprintf(Stdout, "next: %s (but held)\n", x.fmtTime(next))
		} else {
			fmt.Fprintf(Stdout, "next: %s\n", x.fmtTime(next))
		}
	} else {
		fmt.Fprintf(Stdout, "next: n/a\n")
	}
	return nil
}

func (x *cmdRefresh) listRefresh() error {
	snaps, _, err := x.client.Find(&client.FindOptions{
		Refresh: true,
	})
	if err != nil {
		return err
	}
	if len(snaps) == 0 {
		fmt.Fprintln(Stderr, i18n.G("All snaps up to date."))
		return nil
	}

	sort.Sort(snapsByName(snaps))

	esc := x.getEscapes()
	w := tabWriter()
	defer w.Flush()

	// TRANSLATORS: the %s is to insert a filler escape sequence (please keep it flush to the column header, with no extra spaces)
	fmt.Fprintf(w, i18n.G("Name\tVersion\tRev\tSize\tPublisher%s\tNotes\n"), fillerPublisher(esc))
	for _, snap := range snaps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", snap.Name, snap.Version, snap.Revision, strutil.SizeToStr(snap.DownloadSize), shortPublisher(esc, snap.Publisher), NotesFromRemote(snap, nil))
	}

	return nil
}

func (x *cmdRefresh) Execute([]string) error {
	if err := x.setChannelFromCommandline(); err != nil {
		return err
	}
	if err := x.validateMode(); err != nil {
		return err
	}

	if x.Time {
		if x.asksForMode() || x.asksForChannel() {
			return errors.New(i18n.G("--time does not take mode or channel flags"))
		}
		return x.showRefreshTimes()
	}

	if x.List {
		if len(x.Positional.Snaps) > 0 || x.asksForMode() || x.asksForChannel() {
			return errors.New(i18n.G("--list does not accept additional arguments"))
		}

		return x.listRefresh()
	}

	if len(x.Positional.Snaps) == 0 && os.Getenv("SNAP_REFRESH_FROM_TIMER") == "1" {
		fmt.Fprintf(Stdout, "Ignoring `snap refresh` from the systemd timer")
		return nil
	}

	names := installedSnapNames(x.Positional.Snaps)
	if len(names) == 1 {
		opts := &client.SnapOptions{
			Amend:            x.Amend,
			Channel:          x.Channel,
			IgnoreValidation: x.IgnoreValidation,
			IgnoreRunning:    x.IgnoreRunning,
			Revision:         x.Revision,
			CohortKey:        x.Cohort,
			LeaveCohort:      x.LeaveCohort,
		}
		x.setModes(opts)
		return x.refreshOne(names[0], opts)
	}

	if x.asksForMode() || x.asksForChannel() {
		return errors.New(i18n.G("a single snap name is needed to specify mode or channel flags"))
	}

	if x.IgnoreValidation {
		return errors.New(i18n.G("a single snap name must be specified when ignoring validation"))
	}
	if x.IgnoreRunning {
		return errors.New(i18n.G("a single snap name must be specified when ignoring running apps and hooks"))
	}

	return x.refreshMany(names, nil)
}

type cmdTry struct {
	waitMixin

	modeMixin
	Positional struct {
		SnapDir string `positional-arg-name:"<snap-dir>"`
	} `positional-args:"yes"`
}

func hasSnapcraftYaml() bool {
	for _, loc := range []string{
		"snap/snapcraft.yaml",
		"snapcraft.yaml",
		".snapcraft.yaml",
	} {
		if osutil.FileExists(loc) {
			return true
		}
	}

	return false
}

func (x *cmdTry) Execute([]string) error {
	if err := x.validateMode(); err != nil {
		return err
	}
	name := x.Positional.SnapDir
	opts := &client.SnapOptions{}
	x.setModes(opts)

	if name == "" {
		if hasSnapcraftYaml() && osutil.IsDirectory("prime") {
			name = "prime"
		} else {
			if osutil.FileExists("meta/snap.yaml") {
				name = "./"
			}
		}
		if name == "" {
			return fmt.Errorf(i18n.G("error: the `<snap-dir>` argument was not provided and couldn't be inferred"))
		}
	}

	path, err := filepath.Abs(name)
	if err != nil {
		// TRANSLATORS: %q gets what the user entered, %v gets the resulting error message
		return fmt.Errorf(i18n.G("cannot get full path for %q: %v"), name, err)
	}

	changeID, err := x.client.Try(path, opts)
	if err != nil {
		msg, err := errorToCmdMessage(name, err, opts)
		if err != nil {
			return err
		}
		fmt.Fprintln(Stderr, msg)
		return nil
	}

	chg, err := x.wait(changeID)
	if err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	// extract the snap name
	var snapName string
	if err := chg.Get("snap-name", &snapName); err != nil {
		// TRANSLATORS: %q gets the snap name, %v gets the resulting error message
		return fmt.Errorf(i18n.G("cannot extract the snap-name from local file %q: %v"), name, err)
	}
	name = snapName

	// show output as speced
	snaps, err := x.client.List([]string{name}, nil)
	if err != nil {
		return err
	}
	if len(snaps) != 1 {
		// TRANSLATORS: %q gets the snap name, %v the list of things found when trying to list it
		return fmt.Errorf(i18n.G("cannot get data for %q: %v"), name, snaps)
	}
	snap := snaps[0]
	// TRANSLATORS: 1. snap name, 2. snap version (keep those together please). the 3rd %s is a path (where it's mounted from).
	fmt.Fprintf(Stdout, i18n.G("%s %s mounted from %s\n"), name, snap.Version, path)
	return nil
}

type cmdEnable struct {
	waitMixin

	Positional struct {
		Snap installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdEnable) Execute([]string) error {
	name := string(x.Positional.Snap)
	opts := &client.SnapOptions{}
	changeID, err := x.client.Enable(name, opts)
	if err != nil {
		return err
	}

	if _, err := x.wait(changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("%s enabled\n"), name)
	return nil
}

type cmdDisable struct {
	waitMixin

	Positional struct {
		Snap installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdDisable) Execute([]string) error {
	name := string(x.Positional.Snap)
	opts := &client.SnapOptions{}
	changeID, err := x.client.Disable(name, opts)
	if err != nil {
		return err
	}

	if _, err := x.wait(changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("%s disabled\n"), name)
	return nil
}

type cmdRevert struct {
	waitMixin

	modeMixin
	Revision      string `long:"revision"`
	IgnoreRunning bool   `long:"ignore-running" hidden:"yes"`
	Positional    struct {
		Snap installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

var shortRevertHelp = i18n.G("Reverts the given snap to the previous state")
var longRevertHelp = i18n.G(`
The revert command reverts the given snap to its state before
the latest refresh. This will reactivate the previous snap revision,
and will use the original data that was associated with that revision,
discarding any data changes that were done by the latest revision. As
an exception, data which the snap explicitly chooses to share across
revisions is not touched by the revert process.
`)

func (x *cmdRevert) Execute(args []string) error {
	if len(args) > 0 {
		return ErrExtraArgs
	}

	if err := x.validateMode(); err != nil {
		return err
	}

	name := string(x.Positional.Snap)
	opts := &client.SnapOptions{
		Revision:      x.Revision,
		IgnoreRunning: x.IgnoreRunning,
	}
	x.setModes(opts)
	changeID, err := x.client.Revert(name, opts)
	if err != nil {
		return err
	}

	if _, err := x.wait(changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	return showDone(x.client, []string{name}, "revert", nil, nil)
}

var shortSwitchHelp = i18n.G("Switches snap to a different channel")
var longSwitchHelp = i18n.G(`
The switch command switches the given snap to a different channel without
doing a refresh. All available channels of a snap are listed in
its 'snap info' output.
`)

type cmdSwitch struct {
	waitMixin
	channelMixin

	Cohort      string `long:"cohort"`
	LeaveCohort bool   `long:"leave-cohort"`

	Positional struct {
		Snap installedSnapName `positional-arg-name:"<snap>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func (x cmdSwitch) Execute(args []string) error {
	if err := x.setChannelFromCommandline(); err != nil {
		return err
	}

	name := string(x.Positional.Snap)
	channel := string(x.Channel)

	switchCohort := x.Cohort != ""
	switchChannel := x.Channel != ""

	// we have three boolean things to check, meaning 2³=8 possibilities
	// of which 3 are errors (which is why we look at the errors first).
	// the 5 valid cases are handled by showDone.
	if switchCohort && x.LeaveCohort {
		// this one counts as two (no channel filter)
		return fmt.Errorf(i18n.G("cannot specify both --cohort and --leave-cohort"))
	}
	if !switchCohort && !x.LeaveCohort && !switchChannel {
		return fmt.Errorf(i18n.G("nothing to switch; specify --channel (and/or one of --cohort/--leave-cohort)"))
	}

	opts := &client.SnapOptions{
		Channel:     channel,
		CohortKey:   x.Cohort,
		LeaveCohort: x.LeaveCohort,
	}
	changeID, err := x.client.Switch(name, opts)
	if err != nil {
		return err
	}

	if _, err := x.wait(changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	return showDone(x.client, []string{name}, "switch", opts, nil)
}

func init() {
	addCommand("remove", shortRemoveHelp, longRemoveHelp, func() flags.Commander { return &cmdRemove{} },
		waitDescs.also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"revision": i18n.G("Remove only the given revision"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"purge": i18n.G("Remove the snap without saving a snapshot of its data"),
		}), nil)
	addCommand("install", shortInstallHelp, longInstallHelp, func() flags.Commander { return &cmdInstall{} },
		colorDescs.also(waitDescs).also(channelDescs).also(modeDescs).also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"revision": i18n.G("Install the given revision of a snap, to which you must have developer access"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"dangerous": i18n.G("Install the given snap file even if there are no pre-acknowledged signatures for it, meaning it was not verified and could be dangerous (--devmode implies this)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"force-dangerous": i18n.G("Alias for --dangerous (DEPRECATED)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"unaliased": i18n.G("Install the given snap without enabling its automatic aliases"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"name": i18n.G("Install the snap file under the given instance name"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"cohort": i18n.G("Install the snap in the given cohort"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"ignore-validation": i18n.G("Ignore validation by other snaps blocking the installation"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"ignore-running": i18n.G("Ignore running hooks or applications blocking the installation"),
		}), nil)
	addCommand("refresh", shortRefreshHelp, longRefreshHelp, func() flags.Commander { return &cmdRefresh{} },
		colorDescs.also(waitDescs).also(channelDescs).also(modeDescs).also(timeDescs).also(map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"amend": i18n.G("Allow refresh attempt on snap unknown to the store"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"revision": i18n.G("Refresh to the given revision, to which you must have developer access"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"list": i18n.G("Show the new versions of snaps that would be updated with the next refresh"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"time": i18n.G("Show auto refresh information but do not perform a refresh"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"ignore-validation": i18n.G("Ignore validation by other snaps blocking the refresh"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"ignore-running": i18n.G("Ignore running hooks or applications blocking the refresh"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"cohort": i18n.G("Refresh the snap into the given cohort"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"leave-cohort": i18n.G("Refresh the snap out of its cohort"),
		}), nil)
	addCommand("try", shortTryHelp, longTryHelp, func() flags.Commander { return &cmdTry{} }, waitDescs.also(modeDescs), nil)
	addCommand("enable", shortEnableHelp, longEnableHelp, func() flags.Commander { return &cmdEnable{} }, waitDescs, nil)
	addCommand("disable", shortDisableHelp, longDisableHelp, func() flags.Commander { return &cmdDisable{} }, waitDescs, nil)
	addCommand("revert", shortRevertHelp, longRevertHelp, func() flags.Commander { return &cmdRevert{} }, waitDescs.also(modeDescs).also(map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"revision": i18n.G("Revert to the given revision"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"ignore-running": i18n.G("Ignore running hooks or applications blocking the revert"),
	}), nil)
	addCommand("switch", shortSwitchHelp, longSwitchHelp, func() flags.Commander { return &cmdSwitch{} }, waitDescs.also(channelDescs).also(map[string]string{
		// TRANSLATORS: This should not start with a lowercase letter.
		"cohort": i18n.G("Switch the snap into the given cohort"),
		// TRANSLATORS: This should not start with a lowercase letter.
		"leave-cohort": i18n.G("Switch the snap out of its cohort"),
	}), nil)
}
