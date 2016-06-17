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
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/progress"

	"github.com/jessevdk/go-flags"
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

func wait(client *client.Client, id string) (*client.Change, error) {
	pb := progress.NewTextProgress()
	defer func() {
		pb.Finished()
		fmt.Fprint(Stdout, "\n")
	}()

	tMax := time.Time{}

	var lastID string
	lastLog := map[string]string{}
	for {
		chg, err := client.Change(id)
		if err != nil {
			// an error here means the server most likely went away
			// XXX: it actually can be a bunch of other things; fix client to expose it better
			now := time.Now()
			if tMax.IsZero() {
				tMax = now.Add(maxGoneTime)
			}
			if now.After(tMax) {
				return nil, err
			}
			pb.Spin("Waiting for server to restart")
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
				pb.Start(t.Summary, float64(t.Progress.Total))
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

			return nil, fmt.Errorf("change finished in status %q with no error message", chg.Status)
		}

		// note this very purposely is not a ticker; we want
		// to sleep 100ms between calls, not call once every
		// 100ms.
		time.Sleep(pollTime)
	}
}

var (
	shortInstallHelp = i18n.G("Install a snap to the system")
	shortRemoveHelp  = i18n.G("Remove a snap from the system")
	shortRefreshHelp = i18n.G("Refresh a snap in the system")
	shortTryHelp     = i18n.G("Try an unpacked snap in the system")
)

var longInstallHelp = i18n.G(`
The install command installs the named snap in the system.
`)

var longRemoveHelp = i18n.G(`
The remove command removes the named snap from the system.

The snap's data is currently not removed; use purge for that. This behaviour
will change before 16.04 is final.
`)

var longRefreshHelp = i18n.G(`
The refresh command refreshes (updates) the named snap.
`)

var longTryHelp = i18n.G(`
The try command installs an unpacked snap into the system for testing purposes.
The unpacked snap content continues to be used even after installation, so
non-metadata changes there go live instantly. Metadata changes such as those
performed in snap.yaml will require reinstallation to go live.
`)

type cmdRemove struct {
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdRemove) Execute([]string) error {
	cli := Client()
	name := x.Positional.Snap
	changeID, err := cli.Remove(name, nil)
	if err != nil {
		return err
	}

	if _, err := wait(cli, changeID); err != nil {
		return err
	}
	fmt.Fprintln(Stdout, "Done")
	return nil
}

type channelMixin struct {
	Channel string `long:"channel" description:"Use this channel instead of stable"`

	// shortcuts
	EdgeChannel      bool `long:"edge" description:"Install from the edge channel"`
	BetaChannel      bool `long:"beta" description:"Install from the beta channel"`
	CandidateChannel bool `long:"candidate" description:"Install from the candidate channel"`
	StableChannel    bool `long:"stable" description:"Install from the stable channel"`
}

func (x *channelMixin) setChannelFromCommandline() error {
	for _, ch := range []struct {
		enabled bool
		chName  string
	}{
		{x.StableChannel, "stable"},
		{x.CandidateChannel, "candidate"},
		{x.BetaChannel, "beta"},
		{x.EdgeChannel, "edge"},
	} {
		if !ch.enabled {
			continue
		}
		if x.Channel != "" {
			return fmt.Errorf("Please specify a single channel")
		}
		x.Channel = ch.chName
	}

	return nil
}

type cmdInstall struct {
	channelMixin

	DevMode    bool `long:"devmode" description:"Install the snap with non-enforcing security"`
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdInstall) Execute([]string) error {
	if err := x.setChannelFromCommandline(); err != nil {
		return err
	}

	var changeID string
	var err error
	var installFromFile bool

	cli := Client()
	name := x.Positional.Snap
	opts := &client.SnapOptions{Channel: x.Channel, DevMode: x.DevMode}
	if strings.Contains(name, "/") || strings.HasSuffix(name, ".snap") || strings.Contains(name, ".snap.") {
		installFromFile = true
		changeID, err = cli.InstallPath(name, opts)
	} else {
		changeID, err = cli.Install(name, opts)
	}
	if err != nil {
		return err
	}

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

	return listSnaps([]string{name})
}

type cmdRefresh struct {
	channelMixin

	List       bool `long:"list" description:"show available snaps for refresh"`
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"yes"`
}

func refreshAll() error {
	// FIXME: move this to snapd instead and have a new refresh-all endpoint
	cli := Client()
	updates, _, err := cli.Find(&client.FindOptions{Refresh: true})
	if err != nil {
		return fmt.Errorf("cannot list updates: %s", err)
	}
	// nothing to update/list
	if len(updates) == 0 {
		fmt.Fprintln(Stderr, i18n.G("All snaps up-to-date."))
		return nil
	}

	names := make([]string, len(updates))
	for i, update := range updates {
		changeID, err := cli.Refresh(update.Name, &client.SnapOptions{Channel: update.Channel})
		if err != nil {
			return err
		}
		if _, err := wait(cli, changeID); err != nil {
			return err
		}
		names[i] = update.Name
	}

	return listSnaps(names)
}

func refreshOne(name, channel string) error {
	cli := Client()
	changeID, err := cli.Refresh(name, &client.SnapOptions{Channel: channel})
	if err != nil {
		return err
	}

	if _, err := wait(cli, changeID); err != nil {
		return err
	}

	return listSnaps([]string{name})
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
		fmt.Fprintln(Stderr, i18n.G("All snaps up-to-date."))
		return nil
	}

	sort.Sort(snapsByName(snaps))

	w := tabWriter()
	defer w.Flush()

	fmt.Fprintln(w, i18n.G("Name\tVersion\tRev\tDeveloper\tNotes"))
	for _, snap := range snaps {
		notes := &Notes{
			Private: snap.Private,
			DevMode: snap.DevMode,
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", snap.Name, snap.Version, snap.Revision, snap.Developer, notes)
	}

	return nil
}

func (x *cmdRefresh) Execute([]string) error {
	if err := x.setChannelFromCommandline(); err != nil {
		return err
	}

	if x.List {
		return listRefresh()
	}
	if x.Positional.Snap == "" {
		return refreshAll()
	}
	return refreshOne(x.Positional.Snap, x.Channel)
}

type cmdTry struct {
	DevMode    bool `long:"devmode" description:"Install in development mode and disable confinement"`
	Positional struct {
		SnapDir string `positional-arg-name:"<snap-dir>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdTry) Execute([]string) error {
	cli := Client()
	name := x.Positional.SnapDir
	opts := &client.SnapOptions{
		DevMode: x.DevMode,
	}

	path, err := filepath.Abs(name)
	if err != nil {
		return fmt.Errorf("cannot get full path for %q: %s", name, err)
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
		return fmt.Errorf("cannot extract the snap-name from local file %q: %s", name, err)
	}
	name = snapName

	return listSnaps([]string{name})
}

func init() {
	addCommand("remove", shortRemoveHelp, longRemoveHelp, func() flags.Commander { return &cmdRemove{} })
	addCommand("install", shortInstallHelp, longInstallHelp, func() flags.Commander { return &cmdInstall{} })
	addCommand("refresh", shortRefreshHelp, longRefreshHelp, func() flags.Commander { return &cmdRefresh{} })
	addCommand("try", shortTryHelp, longTryHelp, func() flags.Commander { return &cmdTry{} })
}
