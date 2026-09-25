// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package cli_test

import (
	"errors"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	cmdsnap "github.com/snapcore/snapd/cmd/snapd/cli"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
)

type SnapPrepareImageSuite struct {
	BaseSnapSuite
}

var _ = Suite(&SnapPrepareImageSuite{})

func (s *SnapPrepareImageSuite) TestPrepareImageCore(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:  "model",
		PrepareDir: "prepare-dir",
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageClassic(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--classic", "model", "prepare-dir"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		Classic:    true,
		ModelFile:  "model",
		PrepareDir: "prepare-dir",
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageClassicArch(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--classic", "--arch", "i386", "model", "prepare-dir"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		Classic:      true,
		Architecture: "i386",
		ModelFile:    "model",
		PrepareDir:   "prepare-dir",
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageClassicWideCohort(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	os.Setenv("UBUNTU_STORE_COHORT_KEY", "is-six-centuries")

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--classic", "model", "prepare-dir"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		Classic:       true,
		WideCohortKey: "is-six-centuries",
		ModelFile:     "model",
		PrepareDir:    "prepare-dir",
	})

	os.Unsetenv("UBUNTU_STORE_COHORT_KEY")
}

func (s *SnapPrepareImageSuite) TestPrepareImageExtraSnaps(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--channel", "candidate", "--snap", "foo", "--snap", "bar=t/edge", "--snap", "local.snap", "--extra-snaps", "local2.snap", "--extra-snaps", "store-snap", "--comp", "local.comp", "--comp", "bar+comp1"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:    "model",
		Channel:      "candidate",
		PrepareDir:   "prepare-dir",
		Snaps:        []string{"foo", "bar", "local.snap", "local2.snap", "store-snap"},
		Components:   []string{"local.comp", "bar+comp1"},
		SnapChannels: map[string]string{"bar": "t/edge"},
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageCustomize(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	tmpdir := c.MkDir()
	customizeFile := filepath.Join(tmpdir, "custo.json")
	err := os.WriteFile(customizeFile, []byte(`{
  "console-conf": "disabled",
  "cloud-init-user-data": "cloud-init-user-data"
}`), 0644)
	c.Assert(err, IsNil)

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--customize", customizeFile})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:  "model",
		PrepareDir: "prepare-dir",
		Customizations: image.Customizations{
			ConsoleConf:       "disabled",
			CloudInitUserData: "cloud-init-user-data",
		},
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageCustomizeValidated(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	tmpdir := c.MkDir()
	customizeFile := filepath.Join(tmpdir, "custo.json")
	err := os.WriteFile(customizeFile, []byte(`{
  "console-conf": "disabled",
  "cloud-init-user-data": "cloud-init-user-data"
}`), 0644)
	c.Assert(err, IsNil)

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--validation", "enforce", "--customize", customizeFile})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:  "model",
		PrepareDir: "prepare-dir",
		Customizations: image.Customizations{
			ConsoleConf:       "disabled",
			CloudInitUserData: "cloud-init-user-data",
			Validation:        "enforce",
		},
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageAllowSnapdKernelMismatch(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--allow-snapd-kernel-mismatch"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:                "model",
		PrepareDir:               "prepare-dir",
		AllowSnapdKernelMismatch: true,
	})
}

func (s *SnapPrepareImageSuite) TestReadSeedManifest(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	var readManifestCalls int
	r = cmdsnap.MockSeedWriterReadManifest(func(manifestFile string) (*seedwriter.Manifest, error) {
		readManifestCalls++
		c.Check(manifestFile, Equals, "seed.manifest")
		return seedwriter.MockManifest(map[string]*seedwriter.ManifestSnapRevision{"snapd": {SnapName: "snapd", Revision: snap.R(100)}}, nil, nil, nil), nil
	})
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--revisions", "seed.manifest"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(readManifestCalls, Equals, 1)
	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:    "model",
		PrepareDir:   "prepare-dir",
		SeedManifest: seedwriter.MockManifest(map[string]*seedwriter.ManifestSnapRevision{"snapd": {SnapName: "snapd", Revision: snap.R(100)}}, nil, nil, nil),
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImagePreseedArgError(c *C) {
	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--preseed-sign-key", "key", "model", "prepare-dir"})
	c.Assert(err, ErrorMatches, `--preseed-sign-key cannot be used without --preseed`)
}

func (s *SnapPrepareImageSuite) TestPrepareImagePreseed(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--preseed", "--preseed-sign-key", "key", "--apparmor-features-dir", "aafeatures-dir", "--sysfs-overlay", "sys-overlay", "model", "prepare-dir"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:                 "model",
		PrepareDir:                "prepare-dir",
		Preseed:                   true,
		PreseedSignKey:            "key",
		SysfsOverlay:              "sys-overlay",
		AppArmorKernelFeaturesDir: "aafeatures-dir",
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageWriteRevisions(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--write-revisions"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:        "model",
		PrepareDir:       "prepare-dir",
		SeedManifestPath: "./seed.manifest",
	})

	rest, err = cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--write-revisions=/tmp/seed.manifest"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:        "model",
		PrepareDir:       "prepare-dir",
		SeedManifestPath: "/tmp/seed.manifest",
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageValidation(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--validation", "ignore", "model", "prepare-dir"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:  "model",
		PrepareDir: "prepare-dir",
		Customizations: image.Customizations{
			Validation: "ignore",
		},
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageCoreExtraAssertions(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "model", "prepare-dir", "--assert", "extra1", "--assert", "extra2"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:            "model",
		PrepareDir:           "prepare-dir",
		ExtraAssertionsFiles: []string{"extra1", "extra2"},
	})
}

func (s *SnapPrepareImageSuite) TestPrepareImageHintsRequiresPreseed(c *C) {
	var materialized bool
	r := cmdsnap.MockPreseedCreateSysfsOverlayFromHints(func(hintsFile string) (string, func(), error) {
		materialized = true
		return "", func() {}, nil
	})
	defer r()

	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--hints", "hints.json", "model", "prepare-dir"})
	c.Assert(err, ErrorMatches, `--hints cannot be used without --preseed`)
	// the policy check happens before any materialization
	c.Check(materialized, Equals, false)
}

func (s *SnapPrepareImageSuite) TestPrepareImageHintsExcludesSysfsOverlay(c *C) {
	var materialized bool
	r := cmdsnap.MockPreseedCreateSysfsOverlayFromHints(func(hintsFile string) (string, func(), error) {
		materialized = true
		return "", func() {}, nil
	})
	defer r()

	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--preseed", "--hints", "hints.json", "--sysfs-overlay", "sys-overlay", "model", "prepare-dir"})
	c.Assert(err, ErrorMatches, `--hints cannot be used together with --sysfs-overlay`)
	c.Check(materialized, Equals, false)
}

func (s *SnapPrepareImageSuite) TestPrepareImageHints(c *C) {
	var opts *image.Options
	prep := func(o *image.Options) error {
		opts = o
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	var hintsFiles []string
	var cleanedUp int
	r = cmdsnap.MockPreseedCreateSysfsOverlayFromHints(func(hintsFile string) (string, func(), error) {
		hintsFiles = append(hintsFiles, hintsFile)
		return "materialized-overlay", func() { cleanedUp++ }, nil
	})
	defer r()

	rest, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--preseed", "--hints", "hints.json", "model", "prepare-dir"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(hintsFiles, DeepEquals, []string{"hints.json"})
	// the materialized overlay is handed over as a plain sysfs overlay
	c.Check(opts, DeepEquals, &image.Options{
		ModelFile:    "model",
		PrepareDir:   "prepare-dir",
		Preseed:      true,
		SysfsOverlay: "materialized-overlay",
	})
	// and it is cleaned up once, after image preparation returned
	c.Check(cleanedUp, Equals, 1)
}

func (s *SnapPrepareImageSuite) TestPrepareImageHintsCleanupAfterImagePrepareError(c *C) {
	var overlayAtPrepare string
	prep := func(o *image.Options) error {
		overlayAtPrepare = o.SysfsOverlay
		return errors.New("cannot prepare image")
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	var cleanedUp int
	r = cmdsnap.MockPreseedCreateSysfsOverlayFromHints(func(hintsFile string) (string, func(), error) {
		return "materialized-overlay", func() { cleanedUp++ }, nil
	})
	defer r()

	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--preseed", "--hints", "hints.json", "model", "prepare-dir"})
	c.Assert(err, ErrorMatches, `cannot prepare image`)

	// the overlay was available for the whole of image preparation and is
	// cleaned up even when it fails
	c.Check(overlayAtPrepare, Equals, "materialized-overlay")
	c.Check(cleanedUp, Equals, 1)
}

func (s *SnapPrepareImageSuite) TestPrepareImageHintsError(c *C) {
	var prepared bool
	prep := func(o *image.Options) error {
		prepared = true
		return nil
	}
	r := cmdsnap.MockImagePrepare(prep)
	defer r()

	r = cmdsnap.MockPreseedCreateSysfsOverlayFromHints(func(hintsFile string) (string, func(), error) {
		return "", nil, errors.New(`cannot open preseed hints file: no such file or directory`)
	})
	defer r()

	_, err := cmdsnap.Parser(cmdsnap.Client()).ParseArgs([]string{"prepare-image", "--preseed", "--hints", "hints.json", "model", "prepare-dir"})
	c.Assert(err, ErrorMatches, `cannot open preseed hints file: no such file or directory`)
	// a failed materialization does not start image preparation
	c.Check(prepared, Equals, false)
}
