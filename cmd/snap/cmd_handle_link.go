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
	"errors"
	"os"
	"syscall"
	"time"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/usersession/userd/ui"
)

type cmdHandleLink struct {
	waitMixin

	Positional struct {
		Uri string `positional-arg-name:"<uri>"`
	} `positional-args:"yes" required:"yes"`
}

func init() {
	cmd := addCommand("handle-link",
		i18n.G("Handle a snap:// URI"),
		i18n.G("The handle-link command installs the snap-store snap and then invokes it."),
		func() flags.Commander {
			return &cmdHandleLink{}
		}, nil, nil)
	cmd.hidden = true
}

func (x *cmdHandleLink) ensureSnapStoreInstalled() error {
	// If the snap-store snap is installed, our work is done
	if _, _, err := x.client.Snap("snap-store"); err == nil {
		return nil
	}

	dialog, err := ui.New()
	if err != nil {
		return err
	}
	answeredYes := dialog.YesNo(
		i18n.G("Install snap-aware Snap Store snap?"),
		i18n.G("The Snap Store is required to open snaps from a web browser."),
		&ui.DialogOptions{
			Timeout: 5 * time.Minute,
			Footer:  i18n.G("This dialog will close automatically after 5 minutes of inactivity."),
		})
	if !answeredYes {
		return errors.New(i18n.G("Snap Store required"))
	}

	changeID, err := x.client.Install("snap-store", nil, nil)
	if err != nil {
		return err
	}
	_, err = x.wait(changeID)
	if err != nil && err != noWait {
		return err
	}
	return nil
}

func (x *cmdHandleLink) Execute([]string) error {
	if err := x.ensureSnapStoreInstalled(); err != nil {
		return err
	}

	argv := []string{"snap", "run", "snap-store", x.Positional.Uri}
	return syscall.Exec("/proc/self/exe", argv, os.Environ())
}
