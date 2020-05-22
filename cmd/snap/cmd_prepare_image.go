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

	"os"
	"fmt"
	"path/filepath"

	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/timings"
)

type cmdPrepareImage struct {
	Classic      bool   `long:"classic"`
	Architecture string `long:"arch"`

	Append bool `long:"append"`
	Remove bool `long:"remove"`

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
			"append": i18n.G("Append snaps to existing seed, instead of creating a new one"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"remove": i18n.G("Remove snaps from existing seed, instead of creating a new one"),
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
		ModelFile:    x.Positional.ModelAssertionFn,
		Channel:      x.Channel,
		Architecture: x.Architecture,
		Classic:      x.Classic,
		PrepareDir:   x.Positional.TargetDir,
		// store-wide cohort key via env, see image/options.go
		WideCohortKey: os.Getenv("UBUNTU_STORE_COHORT_KEY"),
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

	if x.Append || x.Remove {
		if !x.Classic {
			return fmt.Errorf("Append/Remove only supported in --classic mode")
		}
		if x.Append && x.Remove {
			return fmt.Errorf("Only one of Append or Remove can be used")
		}
		seedDir := filepath.Join(opts.PrepareDir, "/var/lib/snapd/seed")
		seed, err := seed.Open(seedDir, "")
		if err != nil {
			return err
		}
		if err := seed.LoadAssertions(nil, nil); err != nil {
			return err
		}
		if err := seed.LoadMeta(timings.New(nil)); err != nil {
			return err
		}
		modeSnaps, err := seed.ModeSnaps("run")
		if err != nil {
			return err
		}
		// Populate seedsnaps & snapChannels for seed.yaml snaps
		seedsnapsIter := append(seed.EssentialSnaps(), modeSnaps...)
		seedsnaps := make([]string, 0, len(seedsnapsIter))
		for _, seedsnap := range seedsnapsIter {
			// but skip any we were asked to remove
			if x.Remove {
				// Below is `if seedsnap in snaps: continue`
				// XXX TODO this should also _remove_ the snap & the snap revision assertion from disk
				skip := false
				for _, argsnap := range snaps {
					if seedsnap.SnapName() == argsnap {
						skip = true
						break
					}
				}
				if skip {
					continue
				}
			}
			seedsnaps = append(seedsnaps, seedsnap.SnapName())
			snapChannels[seedsnap.SnapName()] = seedsnap.Channel
		}

		// If append, final list of snaps is seed + arg snaps
		if x.Append {
			snaps = append(seedsnaps, snaps...)
		}
		// If remove, final list of snaps is filtered seed snaps only
		if x.Remove {
			snaps = seedsnaps
		}
	}
	if len(snaps) != 0 {
		opts.Snaps = snaps
	}
	if len(snapChannels) != 0 {
		opts.SnapChannels = snapChannels
	}

	return imagePrepare(opts)
}
