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
	"github.com/snapcore/snapd/weld"
)

type cmdWeld struct {
	Positional struct {
		ModelAssertionFn string `positional-arg-name:"model-assertion" description:"the model assertion name"`
	} `positional-args:"yes" required:"yes"`

	Rootdir         string   `long:"root-dir" description:"the dir that snapd considers the image rootdir" required:"yes"`
	GadgetUnpackDir string   `long:"gadget-unpack-dir" description:"the dir that the gadget snap is unpacked to" required:"yes"`
	ExtraSnaps      []string `long:"extra-snaps" description:"extra snaps to be installed"`
	Channel         string   `long:"channel" description:"the channel to use"`
}

func init() {
	cmd := addCommand("weld",
		i18n.G("Weld a snappy system"),
		i18n.G("Weld a snappy system"),
		func() flags.Commander {
			return &cmdWeld{}
		})
	cmd.hidden = true
}

func (x *cmdWeld) Execute(args []string) error {
	opts := &weld.Options{
		ModelAssertionFn: x.Positional.ModelAssertionFn,

		Rootdir:         x.Rootdir,
		GadgetUnpackDir: x.GadgetUnpackDir,
		Channel:         x.Channel,
		Snaps:           x.ExtraSnaps,
	}

	return weld.Weld(opts)
}
