// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	"os"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/userd/ui"
)

type cmdHandleLink struct {
	waitMixin

	Positional struct {
		Uri string `positional-arg-name:"<uri>"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	addCommand("handle-link",
		i18n.G("Handle a snap:// URI"),
		i18n.G("The handle-link command installs the gnome-software snap and then invokes it."),
		func() flags.Commander {
			return &cmdHandleLink{}
		}, nil, nil)
}

func (x *cmdHandleLink) ensureGnomeSoftwareInstalled(cli *client.Client) error {
	// If the gnome-software snap is installed, our work is done
	if _, _, err := cli.Snap("gnome-software"); err == nil {
		return nil
	}

	dialog, err := ui.New()
	if err != nil {
		return err
	}
	answeredYes := dialog.YesNo(
		i18n.G("Install snap-aware GNOME Software snap?"),
		i18n.G("GNOME Software is required to open snaps from a web browser."),
		&ui.DialogOptions{
			Timeout: 5 * time.Minute,
			Footer:  i18n.G("This dialog will close automatically after 5 minutes of inactivity."),
		})
	if !answeredYes {
		return fmt.Errorf(i18n.G("GNOME Software required"))
	}

	opts := client.SnapOptions{
		Channel: "edge", // FIXME: remove this when gnome-software published to stable
		Classic: true,
	}
	changeID, err := cli.Install("gnome-software", &opts)
	if err != nil {
		return err
	}
	_, err = x.wait(cli, changeID)
	if err != nil && err != noWait {
		return err
	}
	return nil
}

func (x *cmdHandleLink) Execute([]string) error {
	cli := Client()

	if err := x.ensureGnomeSoftwareInstalled(cli); err != nil {
		return err
	}

	argv := []string{"snap", "run", "gnome-software", x.Positional.Uri}
	return syscall.Exec("/proc/self/exe", argv, os.Environ())
}
