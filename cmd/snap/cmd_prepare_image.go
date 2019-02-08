// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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
	"path/filepath"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/image"
)

type cmdPrepareImage struct {
	Classic      bool   `long:"classic"`
	Architecture string `long:"arch"`

	Positional struct {
		ModelAssertionFn string
		Rootdir          string
	} `positional-args:"yes" required:"yes"`

	Channel    string   `long:"channel" default:"stable"`
	ExtraSnaps []string `long:"extra-snaps"`
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
			"extra-snaps": i18n.G("Extra snaps to be installed"),
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
				name: i18n.G("<root-dir>"),
				// TRANSLATORS: This should not start with a lowercase letter.
				desc: i18n.G("The output directory"),
			},
		})
}

func (x *cmdPrepareImage) Execute(args []string) error {
	opts := &image.Options{
		ModelFile:    x.Positional.ModelAssertionFn,
		Channel:      x.Channel,
		Snaps:        x.ExtraSnaps,
		Architecture: x.Architecture,
	}

	if x.Classic {
		opts.Classic = true
		opts.RootDir = x.Positional.Rootdir
	} else {
		opts.RootDir = filepath.Join(x.Positional.Rootdir, "image")
		opts.GadgetUnpackDir = filepath.Join(x.Positional.Rootdir, "gadget")
	}

	return image.Prepare(opts)
}
