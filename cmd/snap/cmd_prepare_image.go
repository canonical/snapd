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
	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/image"
)

type cmdPrepareImage struct {
	Positional struct {
		ModelAssertionFn string `positional-arg-name:"model-assertion" description:"the model assertion name"`
	} `positional-args:"yes" required:"yes"`

	Rootdir         string   `long:"root-dir" description:"the dir that snapd considers the image rootdir" required:"yes"`
	GadgetUnpackDir string   `long:"gadget-unpack-dir" description:"the dir that the gadget snap is unpacked to" required:"yes"`
	ExtraSnaps      []string `long:"extra-snaps" description:"extra snaps to be installed"`
	Channel         string   `long:"channel" description:"the channel to use"`
}

func init() {
	cmd := addCommand("prepare-image",
		i18n.G("Prepare a snappy image"),
		i18n.G("Prepare a snappy image"),
		func() flags.Commander {
			return &cmdPrepareImage{}
		})
	cmd.hidden = true
}

func (x *cmdPrepareImage) Execute(args []string) error {
	opts := &image.Options{
		ModelAssertionFn: x.Positional.ModelAssertionFn,

		Rootdir:         x.Rootdir,
		GadgetUnpackDir: x.GadgetUnpackDir,
		Channel:         x.Channel,
		Snaps:           x.ExtraSnaps,
	}

	return image.Weld(opts)
}
