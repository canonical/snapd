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

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
)

var (
	shortInstallHelp = i18n.G("Install a snap to the system")
	shortRemoveHelp  = i18n.G("Remove a snap from the system")
	shortRefreshHelp = i18n.G("Refresh a snap in the system")
	shortTryHelp     = i18n.G("Test a snap in the system")
	shortEnableHelp  = i18n.G("Enable a snap in the system")
	shortDisableHelp = i18n.G("Disable a snap in the system")
)

var longInstallHelp = i18n.G(`
The install command installs the named snaps in the system.

With no further options, the snaps are installed tracking the stable channel,
with strict security confinement.

Revision choice via the --revision override requires the the user to
have developer access to the snap, either directly or through the
store's collaboration feature, and to be logged in (see 'snap help login').

Note a later refresh will typically undo a revision override, taking the snap
back to the current revision of the channel it's tracking.
`)

var longRemoveHelp = i18n.G(`
The remove command removes the named snap from the system.

By default all the snap revisions are removed, including their data and the
common data directory. When a --revision option is passed only the specified
revision is removed.
`)

var longRefreshHelp = i18n.G(`
The refresh command updates the specified snaps, or all snaps in the system if
none are specified.

With no further options, the snaps are refreshed to the current revision of the
channel they're tracking, preserving their confinement options.

Revision choice via the --revision override requires the the user to
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
	Positional struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdRemove) removeOne(opts *client.SnapOptions) error {
	name := string(x.Positional.Snaps[0])

	cli := Client()
	changeID, err := cli.Remove(name, opts)
	if err != nil {
		msg, err := errorToCmdMessage(name, err, opts)
		if err != nil {
			return err
		}
		fmt.Fprintln(Stderr, msg)
		return nil
	}

	if _, err := x.wait(cli, changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("%s removed\n"), name)
	return nil
}

func (x *cmdRemove) removeMany(opts *client.SnapOptions) error {
	names := installedSnapNames(x.Positional.Snaps)
	cli := Client()
	changeID, err := cli.RemoveMany(names, opts)
	if err != nil {
		return err
	}

	chg, err := x.wait(cli, changeID)
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
	opts := &client.SnapOptions{Revision: x.Revision}
	if len(x.Positional.Snaps) == 1 {
		return x.removeOne(opts)
	}

	if x.Revision != "" {
		return errors.New(i18n.G("a single snap name is needed to specify the revision"))
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
	"channel":   i18n.G("Use this channel instead of stable"),
	"beta":      i18n.G("Install from the beta channel"),
	"edge":      i18n.G("Install from the edge channel"),
	"candidate": i18n.G("Install from the candidate channel"),
	"stable":    i18n.G("Install from the stable channel"),
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

	if !strings.Contains(mx.Channel, "/") && mx.Channel != "" && mx.Channel != "edge" && mx.Channel != "beta" && mx.Channel != "candidate" && mx.Channel != "stable" {
		// shortcut to jump to a different track, e.g.
		// snap install foo --channel=3.4 # implies 3.4/stable
		mx.Channel += "/stable"
	}

	return nil
}

// show what has been done
func showDone(names []string, op string) error {
	cli := Client()
	snaps, err := cli.List(names, nil)
	if err != nil {
		return err
	}

	for _, snap := range snaps {
		channelStr := ""
		if snap.Channel != "" && snap.Channel != "stable" {
			channelStr = fmt.Sprintf(" (%s)", snap.Channel)
		}
		switch op {
		case "install":
			if snap.Publisher != nil {
				// TRANSLATORS: the args are a snap name optionally followed by a channel, then a version, then the developer name (e.g. "some-snap (beta) 1.3 from 'alice' installed")
				fmt.Fprintf(Stdout, i18n.G("%s%s %s from '%s' installed\n"), snap.Name, channelStr, snap.Version, snap.Publisher.Username)
			} else {
				// TRANSLATORS: the args are a snap name optionally followed by a channel, then a version (e.g. "some-snap (beta) 1.3 installed")
				fmt.Fprintf(Stdout, i18n.G("%s%s %s installed\n"), snap.Name, channelStr, snap.Version)
			}
		case "refresh":
			if snap.Publisher != nil {
				// TRANSLATORS: the args are a snap name optionally followed by a channel, then a version, then the developer name (e.g. "some-snap (beta) 1.3 from 'alice' refreshed")
				fmt.Fprintf(Stdout, i18n.G("%s%s %s from '%s' refreshed\n"), snap.Name, channelStr, snap.Version, snap.Publisher.Username)
			} else {
				// TRANSLATORS: the args are a snap name optionally followed by a channel, then a version (e.g. "some-snap (beta) 1.3 refreshed")
				fmt.Fprintf(Stdout, i18n.G("%s%s %s refreshed\n"), snap.Name, channelStr, snap.Version)
			}
		case "revert":
			// TRANSLATORS: first %s is a snap name, second %s is a revision
			fmt.Fprintf(Stdout, i18n.G("%s reverted to %s\n"), snap.Name, snap.Version)
		default:
			fmt.Fprintf(Stdout, "internal error: unknown op %q", op)
		}
		if snap.TrackingChannel != snap.Channel && snap.Channel != "" {
			// TRANSLATORS: first %s is a channel name, following %s is a snap name, last %s is a channel name again.
			fmt.Fprintf(Stdout, i18n.G("Channel %s for %s is closed; temporarily forwarding to %s.\n"), snap.TrackingChannel, snap.Name, snap.Channel)
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
	"classic":  i18n.G("Put snap in classic mode and disable security confinement"),
	"devmode":  i18n.G("Put snap in development mode and disable security confinement"),
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
	waitMixin

	channelMixin
	modeMixin
	Revision string `long:"revision"`

	Dangerous bool `long:"dangerous"`
	// alias for --dangerous, deprecated but we need to support it
	// because we released 2.14.2 with --force-dangerous
	ForceDangerous bool `long:"force-dangerous" hidden:"yes"`

	Unaliased bool `long:"unaliased"`

	Instance string `long:"instance" hidden:"yes"`

	Positional struct {
		Snaps []remoteSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdInstall) installOne(name string, opts *client.SnapOptions) error {
	var err error
	var installFromFile bool
	var changeID string

	cli := Client()
	if strings.Contains(name, "/") || strings.HasSuffix(name, ".snap") || strings.Contains(name, ".snap.") {
		installFromFile = true
		changeID, err = cli.InstallPath(name, opts)
	} else {
		if opts.Instance != "" {
			return errors.New(i18n.G("cannot use instance name when installing from store"))
		}
		changeID, err = cli.Install(name, opts)
	}
	if err != nil {
		msg, err := errorToCmdMessage(name, err, opts)
		if err != nil {
			return err
		}
		fmt.Fprintln(Stderr, msg)
		return nil
	}

	chg, err := x.wait(cli, changeID)
	if err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	// extract the snapName from the change, important for sideloaded
	var snapName string

	if installFromFile {
		if err := chg.Get("snap-name", &snapName); err != nil {
			return fmt.Errorf("cannot extract the snap-name from local file %q: %s", name, err)
		}
		name = snapName
	}

	return showDone([]string{name}, "install")
}

func (x *cmdInstall) installMany(names []string, opts *client.SnapOptions) error {
	// sanity check
	for _, name := range names {
		if strings.Contains(name, "/") || strings.HasSuffix(name, ".snap") || strings.Contains(name, ".snap.") {
			return fmt.Errorf("only one snap file can be installed at a time")
		}
	}

	cli := Client()
	changeID, err := cli.InstallMany(names, opts)
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

	chg, err := x.wait(cli, changeID)
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
		if err := showDone(installed, "install"); err != nil {
			return err
		}
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
		Channel:   x.Channel,
		Revision:  x.Revision,
		Dangerous: dangerous,
		Unaliased: x.Unaliased,
		Instance:  x.Instance,
	}
	x.setModes(opts)

	names := remoteSnapNames(x.Positional.Snaps)
	if len(names) == 1 {
		return x.installOne(names[0], opts)
	}

	if x.asksForMode() || x.asksForChannel() {
		return errors.New(i18n.G("a single snap name is needed to specify mode or channel flags"))
	}

	if x.Instance != "" {
		return errors.New(i18n.G("cannot use instance name when installing multiple snaps"))
	}
	return x.installMany(names, nil)
}

type cmdRefresh struct {
	timeMixin
	waitMixin
	channelMixin
	modeMixin

	Amend            bool   `long:"amend"`
	Revision         string `long:"revision"`
	List             bool   `long:"list"`
	Time             bool   `long:"time"`
	IgnoreValidation bool   `long:"ignore-validation"`
	Positional       struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
}

func (x *cmdRefresh) refreshMany(snaps []string, opts *client.SnapOptions) error {
	cli := Client()
	changeID, err := cli.RefreshMany(snaps, opts)
	if err != nil {
		return err
	}

	chg, err := x.wait(cli, changeID)
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
		return showDone(refreshed, "refresh")
	}

	fmt.Fprintln(Stderr, i18n.G("All snaps up to date."))

	return nil
}

func (x *cmdRefresh) refreshOne(name string, opts *client.SnapOptions) error {
	cli := Client()
	changeID, err := cli.Refresh(name, opts)
	if err != nil {
		msg, err := errorToCmdMessage(name, err, opts)
		if err != nil {
			return err
		}
		fmt.Fprintln(Stderr, msg)
		return nil
	}

	if _, err := x.wait(cli, changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	return showDone([]string{name}, "refresh")
}

func (x *cmdRefresh) showRefreshTimes() error {
	cli := Client()
	sysinfo, err := cli.SysInfo()
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
	if t, err := time.Parse(time.RFC3339, sysinfo.Refresh.Last); err == nil {
		fmt.Fprintf(Stdout, "last: %s\n", x.fmtTime(t))
	} else {
		fmt.Fprintf(Stdout, "last: n/a\n")
	}

	if t, err := time.Parse(time.RFC3339, sysinfo.Refresh.Hold); err == nil {
		fmt.Fprintf(Stdout, "hold: %s\n", x.fmtTime(t))
	}
	if t, err := time.Parse(time.RFC3339, sysinfo.Refresh.Next); err == nil {
		fmt.Fprintf(Stdout, "next: %s\n", x.fmtTime(t))
	} else {
		fmt.Fprintf(Stdout, "next: n/a\n")
	}
	return nil
}

func (x *cmdRefresh) listRefresh() error {
	cli := Client()
	snaps, _, err := cli.Find(&client.FindOptions{
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

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Name\tVersion\tRev\tPublisher\tNotes"))
	for _, snap := range snaps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", snap.Name, snap.Version, snap.Revision, snap.Publisher.Username, NotesFromRemote(snap, nil))
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
			return errors.New(i18n.G("--time does not take mode nor channel flags"))
		}
		return x.showRefreshTimes()
	}

	if x.List {
		if x.asksForMode() || x.asksForChannel() {
			return errors.New(i18n.G("--list does not take mode nor channel flags"))
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
			Revision:         x.Revision,
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
	cli := Client()
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

	changeID, err := cli.Try(path, opts)
	if e, ok := err.(*client.Error); ok && e.Kind == client.ErrorKindNotSnap {
		return fmt.Errorf(i18n.G(`%q does not contain an unpacked snap.

Try 'snapcraft prime' in your project directory, then 'snap try' again.`), path)
	}
	if err != nil {
		return err
	}

	chg, err := x.wait(cli, changeID)
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
	snaps, err := cli.List([]string{name}, nil)
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
	cli := Client()
	name := string(x.Positional.Snap)
	opts := &client.SnapOptions{}
	changeID, err := cli.Enable(name, opts)
	if err != nil {
		return err
	}

	if _, err := x.wait(cli, changeID); err != nil {
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
	cli := Client()
	name := string(x.Positional.Snap)
	opts := &client.SnapOptions{}
	changeID, err := cli.Disable(name, opts)
	if err != nil {
		return err
	}

	if _, err := x.wait(cli, changeID); err != nil {
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
	Revision   string `long:"revision"`
	Positional struct {
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

	cli := Client()
	name := string(x.Positional.Snap)
	opts := &client.SnapOptions{Revision: x.Revision}
	x.setModes(opts)
	changeID, err := cli.Revert(name, opts)
	if err != nil {
		return err
	}

	if _, err := x.wait(cli, changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	return showDone([]string{name}, "revert")
}

var shortSwitchHelp = i18n.G("Switches snap to a different channel")
var longSwitchHelp = i18n.G(`
The switch command switches the given snap to a different channel without
doing a refresh.
`)

type cmdSwitch struct {
	waitMixin
	channelMixin

	Positional struct {
		Snap installedSnapName `positional-arg-name:"<snap>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func (x cmdSwitch) Execute(args []string) error {
	if err := x.setChannelFromCommandline(); err != nil {
		return err
	}
	if x.Channel == "" {
		return fmt.Errorf("missing --channel=<channel-name> parameter")
	}

	cli := Client()
	name := string(x.Positional.Snap)
	channel := string(x.Channel)
	opts := &client.SnapOptions{
		Channel: channel,
	}
	changeID, err := cli.Switch(name, opts)
	if err != nil {
		return err
	}

	if _, err := x.wait(cli, changeID); err != nil {
		if err == noWait {
			return nil
		}
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("%q switched to the %q channel\n"), name, channel)
	return nil
}

func init() {
	addCommand("remove", shortRemoveHelp, longRemoveHelp, func() flags.Commander { return &cmdRemove{} },
		waitDescs.also(map[string]string{"revision": i18n.G("Remove only the given revision")}), nil)
	addCommand("install", shortInstallHelp, longInstallHelp, func() flags.Commander { return &cmdInstall{} },
		waitDescs.also(channelDescs).also(modeDescs).also(map[string]string{
			"revision":        i18n.G("Install the given revision of a snap, to which you must have developer access"),
			"dangerous":       i18n.G("Install the given snap file even if there are no pre-acknowledged signatures for it, meaning it was not verified and could be dangerous (--devmode implies this)"),
			"force-dangerous": i18n.G("Alias for --dangerous (DEPRECATED)"),
			"unaliased":       i18n.G("Install the given snap without enabling its automatic aliases"),
			"instance":        i18n.G("Install the snap file as given snap instance"),
		}), nil)
	addCommand("refresh", shortRefreshHelp, longRefreshHelp, func() flags.Commander { return &cmdRefresh{} },
		waitDescs.also(channelDescs).also(modeDescs).also(timeDescs).also(map[string]string{
			"amend":             i18n.G("Allow refresh attempt on snap unknown to the store"),
			"revision":          i18n.G("Refresh to the given revision, to which you must have developer access"),
			"list":              i18n.G("Show available snaps for refresh but do not perform a refresh"),
			"time":              i18n.G("Show auto refresh information but do not perform a refresh"),
			"ignore-validation": i18n.G("Ignore validation by other snaps blocking the refresh"),
		}), nil)
	addCommand("try", shortTryHelp, longTryHelp, func() flags.Commander { return &cmdTry{} }, waitDescs.also(modeDescs), nil)
	addCommand("enable", shortEnableHelp, longEnableHelp, func() flags.Commander { return &cmdEnable{} }, waitDescs, nil)
	addCommand("disable", shortDisableHelp, longDisableHelp, func() flags.Commander { return &cmdDisable{} }, waitDescs, nil)
	addCommand("revert", shortRevertHelp, longRevertHelp, func() flags.Commander { return &cmdRevert{} }, waitDescs.also(modeDescs).also(map[string]string{
		"revision": "Revert to the given revision",
	}), nil)
	addCommand("switch", shortSwitchHelp, longSwitchHelp, func() flags.Commander { return &cmdSwitch{} }, waitDescs.also(channelDescs), nil)
}
