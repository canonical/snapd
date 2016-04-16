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
	"strings"
	"time"

	"github.com/ubuntu-core/snappy/client"
	"github.com/ubuntu-core/snappy/i18n"
	"github.com/ubuntu-core/snappy/progress"

	"github.com/jessevdk/go-flags"
)

func wait(client *client.Client, id string) error {
	pb := progress.NewTextProgress()
	defer func() {
		pb.Finished()
		fmt.Print("\n")
	}()

	var lastID string
	for {
		chg, err := client.Change(id)
		if err != nil {
			return err
		}

		for _, t := range chg.Tasks {
			switch {
			case t.Status != "Doing":
				continue
			case t.Progress.Total == 1:
				pb.Spin(t.Summary)
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
				return nil
			}

			if chg.Err != "" {
				return errors.New(chg.Err)
			}

			return fmt.Errorf("change finished in status %q with no error message", chg.Status)
		}

		// note this very purposely is not a ticker; we want
		// to sleep 100ms between calls, not call once every
		// 100ms.
		time.Sleep(100 * time.Millisecond)
	}
}

var (
	shortInstallHelp = i18n.G("Install a snap to the system")
	shortRemoveHelp  = i18n.G("Remove a snap from the system")
	shortRefreshHelp = i18n.G("Refresh a snap in the system")
)

var longInstallHelp = i18n.G(`
The install command installs and activates the named snap in the system.
`)

var longRemoveHelp = i18n.G(`
The remove command removes the named snap from the system.

The snap's data is currently not removed; use purge for that. This behaviour
will change before 16.04 is final.
`)

var longRefreshHelp = i18n.G(`
The refresh command refreshes (updates) the named snap.
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

	return wait(cli, changeID)
}

type cmdInstall struct {
	Channel    string `long:"channel" description:"Install from this channel instead of the device's default"`
	DevMode    bool   `long:"devmode" description:"Install the snap with non-enforcing security"`
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdInstall) Execute([]string) error {
	var changeID string
	var err error

	cli := Client()
	name := x.Positional.Snap
	opts := &client.SnapOptions{Channel: x.Channel, DevMode: x.DevMode}
	if strings.Contains(name, "/") || strings.HasSuffix(name, ".snap") || strings.Contains(name, ".snap.") {
		changeID, err = cli.InstallPath(name, opts)
	} else {
		changeID, err = cli.Install(name, opts)
	}
	if err != nil {
		return err
	}

	return wait(cli, changeID)
}

type cmdRefresh struct {
	Channel    string `long:"channel" description:"Refresh to the latest on this channel, and track this channel henceforth"`
	Positional struct {
		Snap string `positional-arg-name:"<snap>"`
	} `positional-args:"yes" required:"yes"`
}

func (x *cmdRefresh) Execute([]string) error {
	cli := Client()
	name := x.Positional.Snap
	opts := &client.SnapOptions{Channel: x.Channel}
	changeID, err := cli.Refresh(name, opts)
	if err != nil {
		return err
	}

	return wait(cli, changeID)
}

func init() {
	addCommand("remove", shortRemoveHelp, longRemoveHelp, func() flags.Commander { return &cmdRemove{} })
	addCommand("install", shortInstallHelp, longInstallHelp, func() flags.Commander { return &cmdInstall{} })
	addCommand("refresh", shortRefreshHelp, longRefreshHelp, func() flags.Commander { return &cmdRefresh{} })
}
