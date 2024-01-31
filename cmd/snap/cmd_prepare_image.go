// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2023 Canonical Ltd
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
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jessevdk/go-flags"

	"github.com/snapcore/snapd/i18n"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/interfaces/builtin"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
)

type cmdPrepareImage struct {
	Classic        bool   `long:"classic"`
	Preseed        bool   `long:"preseed"`
	PreseedSignKey string `long:"preseed-sign-key"`
	// optional path to AppArmor kernel features directory
	AppArmorKernelFeaturesDir string `long:"apparmor-features-dir"`
	// optional sysfs overlay
	SysfsOverlay string `long:"sysfs-overlay"`
	Architecture string `long:"arch"`

	Positional struct {
		ModelAssertionFn string
		TargetDir        string
	} `positional-args:"yes" required:"yes"`

	Channel string `long:"channel"`

	Customize string `long:"customize" hidden:"yes"`

	// TODO: introduce SnapWithChannel?
	Snaps              []string `long:"snap" value-name:"<snap>[=<channel>]"`
	ExtraSnaps         []string `long:"extra-snaps" hidden:"yes"` // DEPRECATED
	RevisionsFile      string   `long:"revisions"`
	WriteRevisionsFile string   `long:"write-revisions" optional:"true" optional-value:"./seed.manifest"`
	Validation         string   `long:"validation" choice:"ignore" choice:"enforce"`
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
			"preseed": i18n.G("Preseed (UC20+ only)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"preseed-sign-key": i18n.G("Name of the key to use to sign preseed assertion, otherwise use the default key"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"sysfs-overlay": i18n.G("Optional sysfs overlay to be used when running preseeding steps"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"apparmor-features-dir": i18n.G("Optional path to apparmor kernel features directory (UC20+ only)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"arch": i18n.G("Specify an architecture for snaps for --classic when the model does not"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"snap": i18n.G("Include the given snap from the store or a local file and/or specify the channel to track for the given snap"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"extra-snaps": i18n.G("Extra snaps to be installed (DEPRECATED)"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"revisions": i18n.G("Specify a seeds.manifest file referencing the exact revisions of the provided snaps which should be installed"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"write-revisions": i18n.G("Writes a manifest file containing references to the exact snap revisions used for the image. A path for the manifest is optional."),
			// TRANSLATORS: This should not start with a lowercase letter.
			"channel": i18n.G("The channel to use"),
			// TRANSLATORS: This should not start with a lowercase letter.
			"customize": i18n.G("Image customizations specified as JSON file."),
			// TRANSLATORS: This should not start with a lowercase letter.
			"validation": i18n.G("Control whether validations should be ignored or enforced. (default: ignore)"),
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
var seedwriterReadManifest = seedwriter.ReadManifest

func (x *cmdPrepareImage) Execute(args []string) error {
	// plug/slot sanitization is disabled (no-op) by default at the package
	// level for "snap" command, for seed/seedwriter used by image however
	// we want real validation.
	snap.SanitizePlugsSlots = builtin.SanitizePlugsSlots
	imageCustomizations := image.Customizations{
		Validation: x.Validation,
	}

	opts := &image.Options{
		Snaps:            x.ExtraSnaps,
		ModelFile:        x.Positional.ModelAssertionFn,
		Channel:          x.Channel,
		Architecture:     x.Architecture,
		SeedManifestPath: x.WriteRevisionsFile,
		Customizations:   imageCustomizations,
	}

	if x.RevisionsFile != "" {
		seedManifest, err := seedwriterReadManifest(x.RevisionsFile)
		if err != nil {
			return err
		}
		opts.SeedManifest = seedManifest
	}

	if x.Customize != "" {
		custo, err := readImageCustomizations(x.Customize)
		if err != nil {
			return err
		}
		opts.Customizations = *custo
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

	// store-wide cohort key via env, see image/options.go
	opts.WideCohortKey = os.Getenv("UBUNTU_STORE_COHORT_KEY")

	opts.PrepareDir = x.Positional.TargetDir
	opts.Classic = x.Classic

	if x.PreseedSignKey != "" && !x.Preseed {
		return fmt.Errorf("--preseed-sign-key cannot be used without --preseed")
	}

	if x.SysfsOverlay != "" && !x.Preseed {
		return fmt.Errorf("--sysfs-overlay cannot be used without --preseed")
	}

	opts.Preseed = x.Preseed
	opts.PreseedSignKey = x.PreseedSignKey
	opts.AppArmorKernelFeaturesDir = x.AppArmorKernelFeaturesDir
	opts.SysfsOverlay = x.SysfsOverlay

	return imagePrepare(opts)
}

func readImageCustomizations(customizationsFile string) (*image.Customizations, error) {
	f, err := os.Open(customizationsFile)
	if err != nil {
		return nil, fmt.Errorf("cannot read image customizations: %v", err)
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var custo image.Customizations
	if err := dec.Decode(&custo); err != nil {
		return nil, fmt.Errorf("cannot parse customizations %q: %v", customizationsFile, err)
	}
	return &custo, nil
}
