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
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/progress"
)

func lastLogStr(logs []string) string {
	if len(logs) == 0 {
		return ""
	}
	return logs[len(logs)-1]
}

var (
	maxGoneTime = 5 * time.Second
	pollTime    = 100 * time.Millisecond
)

func wait(cli *client.Client, id string) (*client.Change, error) {
	pb := progress.NewTextProgress()
	defer func() {
		pb.Finished()
	}()

	tMax := time.Time{}

	var lastID string
	lastLog := map[string]string{}
	for {
		chg, err := cli.Change(id)
		if err != nil {
			// a client.Error means we were able to communicate with
			// the server (got an answer)
			if e, ok := err.(*client.Error); ok {
				return nil, e
			}

			// an non-client error here means the server most
			// likely went away
			// XXX: it actually can be a bunch of other things; fix client to expose it better
			now := time.Now()
			if tMax.IsZero() {
				tMax = now.Add(maxGoneTime)
			}
			if now.After(tMax) {
				return nil, err
			}
			pb.Spin(i18n.G("Waiting for server to restart"))
			time.Sleep(pollTime)
			continue
		}
		if !tMax.IsZero() {
			pb.Finished()
			tMax = time.Time{}
		}

		for _, t := range chg.Tasks {
			switch {
			case t.Status != "Doing":
				continue
			case t.Progress.Total == 1:
				pb.Spin(t.Summary)
				nowLog := lastLogStr(t.Log)
				if lastLog[t.ID] != nowLog {
					pb.Notify(nowLog)
					lastLog[t.ID] = nowLog
				}
			case t.ID == lastID:
				pb.Set(float64(t.Progress.Done))
			default:
				pb.Start(t.Progress.Label, float64(t.Progress.Total))
				lastID = t.ID
			}
			break
		}

		if chg.Ready {
			if chg.Status == "Done" {
				return chg, nil
			}

			if chg.Err != "" {
				return chg, errors.New(chg.Err)
			}

			return nil, fmt.Errorf(i18n.G("change finished in status %q with no error message"), chg.Status)
		}

		// note this very purposely is not a ticker; we want
		// to sleep 100ms between calls, not call once every
		// 100ms.
		time.Sleep(pollTime)
	}
}

var (
	shortInstallHelp = i18n.G("Installs a snap to the system")
	shortRemoveHelp  = i18n.G("Removes a snap from the system")
	shortRefreshHelp = i18n.G("Refreshes a snap in the system")
	shortTryHelp     = i18n.G("Tests a snap in the system")
	shortEnableHelp  = i18n.G("Enables a snap in the system")
	shortDisableHelp = i18n.G("Disables a snap in the system")
)

var longInstallHelp = i18n.G(`
The install command installs the named snap in the system.
`)

var longRemoveHelp = i18n.G(`
The remove command removes the named snap from the system.

By default all the snap revisions are removed, including their data and the common
data directory. When a --revision option is passed only the specified revision is
removed.
`)

var longRefreshHelp = i18n.G(`
The refresh command refreshes (updates) the named snap.
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
snap will no longer be available. But all the data is still available
and the snap can easily be enabled again.
`)

type cmdRemove struct {
	Revision   string `long:"revision"`
	Positional struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdRemove) removeOne(opts *client.SnapOptions) error {
	name := x.Positional.Snaps[0]

	cli := Client()
	changeID, err := cli.Remove(string(name), opts)
	if e, ok := err.(*client.Error); ok && e.Kind == client.ErrorKindSnapNotInstalled {
		fmt.Fprintf(Stderr, e.Message+"\n")
		return nil
	}
	if err != nil {
		return err
	}

	if _, err := wait(cli, changeID); err != nil {
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("%s removed\n"), name)
	return nil
}

func (x *cmdRemove) removeMany(opts *client.SnapOptions) error {
	names := make([]string, len(x.Positional.Snaps))
	for i, s := range x.Positional.Snaps {
		names[i] = string(s)
	}

	cli := Client()
	changeID, err := cli.RemoveMany(names, opts)
	if err != nil {
		return err
	}

	chg, err := wait(cli, changeID)
	if err != nil {
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
			if snap.Developer != "" {
				fmt.Fprintf(Stdout, i18n.G("%s%s %s from '%s' installed\n"), snap.Name, channelStr, snap.Version, snap.Developer)
			} else {
				fmt.Fprintf(Stdout, i18n.G("%s%s %s installed\n"), snap.Name, channelStr, snap.Version)
			}
		case "refresh":
			if snap.Developer != "" {
				fmt.Fprintf(Stdout, i18n.G("%s%s %s from '%s' refreshed\n"), snap.Name, channelStr, snap.Version, snap.Developer)
			} else {
				fmt.Fprintf(Stdout, i18n.G("%s%s %s refreshed\n"), snap.Name, channelStr, snap.Version)
			}
		default:
			fmt.Fprintf(Stdout, "internal error, unknown op %q", op)
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
	channelMixin
	modeMixin
	Revision string `long:"revision"`

	Dangerous bool `long:"dangerous"`
	// alias for --dangerous, deprecated but we need to support it
	// because we released 2.14.2 with --force-dangerous
	ForceDangerous bool `long:"force-dangerous" hidden:"yes"`

	Positional struct {
		Snaps []remoteSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func setupAbortHandler(changeId string) {
	// Intercept sigint
	c := make(chan os.Signal, 2)
	signal.Notify(c, syscall.SIGINT)
	go func() {
		<-c
		cli := Client()
		_, err := cli.Abort(changeId)
		if err != nil {
			fmt.Fprintf(Stderr, err.Error()+"\n")
		}
	}()
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
		changeID, err = cli.Install(name, opts)
	}
	if e, ok := err.(*client.Error); ok && e.Kind == client.ErrorKindSnapAlreadyInstalled {
		fmt.Fprintf(Stderr, i18n.G("snap %q is already installed, see \"snap refresh --help\"\n"), name)
		return nil
	}
	if err != nil {
		return err
	}

	setupAbortHandler(changeID)

	chg, err := wait(cli, changeID)
	if err != nil {
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
		return err
	}

	setupAbortHandler(changeID)

	chg, err := wait(cli, changeID)
	if err != nil {
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
	}
	x.setModes(opts)

	names := make([]string, len(x.Positional.Snaps))
	for i, name := range x.Positional.Snaps {
		names[i] = string(name)
	}

	if len(names) == 1 {
		return x.installOne(names[0], opts)
	}

	if x.asksForMode() || x.asksForChannel() {
		return errors.New(i18n.G("a single snap name is needed to specify mode or channel flags"))
	}

	return x.installMany(names, nil)
}

type cmdRefresh struct {
	channelMixin
	modeMixin

	Revision         string `long:"revision"`
	List             bool   `long:"list"`
	IgnoreValidation bool   `long:"ignore-validation"`
	Positional       struct {
		Snaps []installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
}

func refreshMany(snaps []string, opts *client.SnapOptions) error {
	cli := Client()
	changeID, err := cli.RefreshMany(snaps, opts)
	if err != nil {
		return err
	}

	chg, err := wait(cli, changeID)
	if err != nil {
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

func refreshOne(name string, opts *client.SnapOptions) error {
	cli := Client()
	changeID, err := cli.Refresh(name, opts)
	if e, ok := err.(*client.Error); ok && e.Kind == client.ErrorKindNoUpdateAvailable {
		fmt.Fprintf(Stderr, e.Message+"\n")
		return nil
	}
	if err != nil {
		return err
	}

	if _, err := wait(cli, changeID); err != nil {
		return err
	}

	return showDone([]string{name}, "refresh")
}

func listRefresh() error {
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

	fmt.Fprintln(w, i18n.G("Name\tVersion\tRev\tDeveloper\tNotes"))
	for _, snap := range snaps {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", snap.Name, snap.Version, snap.Revision, snap.Developer, NotesFromRemote(snap, nil))
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

	if x.List {
		if x.asksForMode() || x.asksForChannel() {
			return errors.New(i18n.G("--list does not take mode nor channel flags"))
		}

		return listRefresh()
	}

	if len(x.Positional.Snaps) == 0 && os.Getenv("SNAP_REFRESH_FROM_TIMER") == "1" {
		fmt.Fprintf(Stdout, "Ignoring `snap refresh` from the systemd timer")
		return nil
	}

	names := make([]string, len(x.Positional.Snaps))
	for i, name := range x.Positional.Snaps {
		names[i] = string(name)
	}
	if len(x.Positional.Snaps) == 1 {
		opts := &client.SnapOptions{
			Channel:          x.Channel,
			IgnoreValidation: x.IgnoreValidation,
			Revision:         x.Revision,
		}
		x.setModes(opts)
		return refreshOne(names[0], opts)
	}

	if x.asksForMode() || x.asksForChannel() {
		return errors.New(i18n.G("a single snap name is needed to specify mode or channel flags"))
	}

	if x.IgnoreValidation {
		return errors.New(i18n.G("a single snap name must be specified when ignoring validation"))
	}

	return refreshMany(names, nil)
}

type cmdTry struct {
	modeMixin
	Positional struct {
		SnapDir string `positional-arg-name:"<snap-dir>"`
	} `positional-args:"yes"`
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
		if osutil.FileExists("snapcraft.yaml") && osutil.IsDirectory("prime") {
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
	if err != nil {
		return err
	}

	chg, err := wait(cli, changeID)
	if err != nil {
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

	_, err = wait(cli, changeID)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("%s enabled\n"), name)
	return nil
}

type cmdDisable struct {
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

	_, err = wait(cli, changeID)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("%s disabled\n"), name)
	return nil
}

type cmdRevert struct {
	modeMixin
	Revision   string `long:"revision"`
	Positional struct {
		Snap installedSnapName `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
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

	if _, err := wait(cli, changeID); err != nil {
		return err
	}

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
	fmt.Fprintf(Stdout, i18n.G("%s reverted to %s\n"), name, snap.Version)
	return nil
}

var shortSwitchHelp = i18n.G("Switch snap to a different channel")
var longSwitchHelp = i18n.G(`
The switch command switch the given snap to a different channel without
actually doing a refresh.
`)

type cmdSwitch struct {
	channelMixin

	Positional struct {
		Snap    installedSnapName `positional-arg-name:"<snap>" required:"1"`
		Channel string            `positional-arg-name:"<channel>" required:"1"`
	} `positional-args:"yes" required:"yes"`
}

func (x cmdSwitch) Execute(args []string) error {
	cli := Client()
	name := string(x.Positional.Snap)
	channel := string(x.Positional.Channel)
	opts := &client.SnapOptions{
		Channel: channel,
	}
	changeID, err := cli.Switch(name, opts)
	if err != nil {
		return err
	}

	_, err = wait(cli, changeID)
	if err != nil {
		return err
	}

	fmt.Fprintf(Stdout, i18n.G("%s switched to %s\n"), name, channel)
	return nil
}

func init() {
	addCommand("remove", shortRemoveHelp, longRemoveHelp, func() flags.Commander { return &cmdRemove{} },
		map[string]string{"revision": i18n.G("Remove only the given revision")}, nil)
	addCommand("install", shortInstallHelp, longInstallHelp, func() flags.Commander { return &cmdInstall{} },
		channelDescs.also(modeDescs).also(map[string]string{
			"revision":        i18n.G("Install the given revision of a snap, to which you must have developer access"),
			"dangerous":       i18n.G("Install the given snap file even if there are no pre-acknowledged signatures for it, meaning it was not verified and could be dangerous (--devmode implies this)"),
			"force-dangerous": i18n.G("Alias for --dangerous (DEPRECATED)"),
		}), nil)
	addCommand("refresh", shortRefreshHelp, longRefreshHelp, func() flags.Commander { return &cmdRefresh{} },
		channelDescs.also(modeDescs).also(map[string]string{
			"revision":          i18n.G("Refresh to the given revision"),
			"list":              i18n.G("Show available snaps for refresh"),
			"ignore-validation": i18n.G("Ignore validation by other snaps blocking the refresh"),
		}), nil)
	addCommand("try", shortTryHelp, longTryHelp, func() flags.Commander { return &cmdTry{} }, modeDescs, nil)
	addCommand("enable", shortEnableHelp, longEnableHelp, func() flags.Commander { return &cmdEnable{} }, nil, nil)
	addCommand("disable", shortDisableHelp, longDisableHelp, func() flags.Commander { return &cmdDisable{} }, nil, nil)
	addCommand("revert", shortRevertHelp, longRevertHelp, func() flags.Commander { return &cmdRevert{} }, modeDescs.also(map[string]string{
		"revision": "Revert to the given revision",
	}), nil)
	addCommand("switch", shortSwitchHelp, longSwitchHelp, func() flags.Commander { return &cmdSwitch{} }, nil, nil)

}
