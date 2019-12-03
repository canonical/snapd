// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/image"
)

type cmdPrepareImage struct {
	Classic      bool   `long:"classic"`
	Architecture string `long:"arch"`

	Positional struct {
		ModelAssertionFn string
		TargetDir        string
	} `positional-args:"yes" required:"yes"`

	Channel string `long:"channel"`
	// TODO: introduce SnapWithChannel?
	Snaps      []string `long:"snap" value-name:"<snap>[=<channel>]"`
	ExtraSnaps []string `long:"extra-snaps" hidden:"yes"` // DEPRECATED
}

func init() {
	addCommand("prepare-image",
		i18n.G("Prepare a device image"),
		i18n.G(`
The prepare-image command performs some of the steps necessary for
creating device images.

For core images it is not invoked directly but usually via
ubuntu-image.

For preparing classic images it supports a --classic mode`),
		func() flags.Commander { return &cmdPrepareImage{} },
		map[string]string{
			// TRANSLATORS: This should not start with a lowercase letter.
			"classic": i18n.G("Enable classic mode to prepare a classic model image"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"arch": i18n.G("Specify an architecture for snaps for --classic when the model does not"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"snap": i18n.G("Include the given snap from the store or a local file and/or specify the channel to track for the given snap"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"extra-snaps": i18n.G("Extra snaps to be installed (DEPRECATED)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"channel": i18n.G("The channel to use"),
		}, []argDesc{
			{
				// TRANSLATORS: This needs to begin with < and end with >
				name: i18n.G("<model-assertion>"),
				// TRANSLATORS: This should not start with a lowercase letter.
				desc: i18n.G("The model assertion name"),
			}, {
				// TRANSLATORS: This needs to begin with < and end with >
				name: i18n.G("<target-dir>"),
				// TRANSLATORS: This should not start with a lowercase letter.
				desc: i18n.G("The target directory"),
			},
		})
}

var imagePrepare = image.Prepare

func (x *cmdPrepareImage) Execute(args []string) error {
	opts := &image.Options{
		Snaps:        x.ExtraSnaps,
		ModelFile:    x.Positional.ModelAssertionFn,
		Channel:      x.Channel,
		Architecture: x.Architecture,
	}

	snaps := make([]string, 0, len(x.Snaps)+len(x.ExtraSnaps))
	snapChannels := make(map[string]string)
	for _, snapWChannel := range x.Snaps {
		snapAndChannel := strings.SplitN(snapWChannel, "=", 2)
		snaps = append(snaps, snapAndChannel[0])
		if len(snapAndChannel) == 2 {
			snapChannels[snapAndChannel[0]] = snapAndChannel[1]
		}
	}

	snaps = append(snaps, x.ExtraSnaps...)

	if len(snaps) != 0 {
		opts.Snaps = snaps
	}
	if len(snapChannels) != 0 {
		opts.SnapChannels = snapChannels
	}

	opts.PrepareDir = x.Positional.TargetDir
	opts.Classic = x.Classic

	return imagePrepare(opts)
}
