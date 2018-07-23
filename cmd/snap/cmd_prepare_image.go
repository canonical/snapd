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
	Positional struct {
		ModelAssertionFn string
		Rootdir          string
	} `positional-args:"yes" required:"yes"`

	ExtraSnaps []string `long:"extra-snaps"`
	Channel    string   `long:"channel" default:"stable"`
}

func init() {
	cmd := addCommand("prepare-image",
		i18n.G("Prepare a core device image"),
		i18n.G(`
The prepare-image command performs some of the steps necessary for creating
core device images.
`),
		func() flags.Commander {
			return &cmdPrepareImage{}
		}, map[string]string{
			"extra-snaps": "Extra snaps to be installed",
			"channel":     "The channel to use",
		}, []argDesc{
			{
				// TRANSLATORS: This needs to be wrapped in <>s.
				name: i18n.G("<model-assertion>"),
				// TRANSLATORS: This should probably not start with a lowercase letter.
				desc: i18n.G("The model assertion name"),
			}, {
				// TRANSLATORS: This needs to be wrapped in <>s.
				name: i18n.G("<root-dir>"),
				// TRANSLATORS: This should probably not start with a lowercase letter.
				desc: i18n.G("The output directory"),
			},
		})
	cmd.hidden = true
}

func (x *cmdPrepareImage) Execute(args []string) error {
	opts := &image.Options{
		ModelFile: x.Positional.ModelAssertionFn,

		RootDir:         filepath.Join(x.Positional.Rootdir, "image"),
		GadgetUnpackDir: filepath.Join(x.Positional.Rootdir, "gadget"),
		Channel:         x.Channel,
		Snaps:           x.ExtraSnaps,
	}

	return image.Prepare(opts)
}
