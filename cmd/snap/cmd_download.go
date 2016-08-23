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
	"os"
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/store"
)

type cmdDownload struct {
	channelMixin
	modeMixin

	Positional struct {
		Snap string `positional-arg-name:"<snap>" description:"snap name"`
	} `positional-args:"true" required:"true"`
}

var shortDownloadHelp = i18n.G("Download a given snap")
var longDownloadHelp = i18n.G(`
The download command will download the given snap to the current directory.
`)

func init() {
	addCommand("download", shortDownloadHelp, longDownloadHelp, func() flags.Commander {
		return &cmdDownload{}
	})
}

func (x *cmdDownload) Execute(args []string) error {
	if err := x.setChannelFromCommandline(); err != nil {
		return err
	}
	if err := x.validateMode(); err != nil {
		return err
	}

	if len(args) > 0 {
		return ErrExtraArgs
	}

	snapName := x.Positional.Snap

	// FIXME: set auth context
	var authContext auth.AuthContext
	var user *auth.UserState

	sto := store.New(nil, "", authContext)
	snap, err := sto.Snap(snapName, x.Channel, x.DevMode, user)
	if err != nil {
		return err
	}
	pb := progress.NewTextProgress()
	tmpName, err := sto.Download(snapName, &snap.DownloadInfo, pb, user)
	if err != nil {
		return err
	}
	defer os.Remove(tmpName)

	targetPath := filepath.Base(snap.MountFile())
	return osutil.CopyFile(tmpName, targetPath, 0)
}
