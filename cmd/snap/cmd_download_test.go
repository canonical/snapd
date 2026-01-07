// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package main_test

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	snapCmd "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/store/tooling"
)

// these only cover errors that happen before hitting the network,
// because we're not (yet!) mocking the tooling store

func (s *SnapSuite) TestDownloadBadBasename(c *check.C) {
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--basename=/foo", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "cannot specify a path in basename .use --target-dir for that.")
}

func (s *SnapSuite) TestDownloadBadChannelCombo(c *check.C) {
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--beta", "--channel=foo", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "please specify a single channel")
}

func (s *SnapSuite) TestDownloadCohortAndRevision(c *check.C) {
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--cohort=what", "--revision=1234", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "cannot specify both cohort and revision")
}

func (s *SnapSuite) TestDownloadChannelAndRevision(c *check.C) {
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--beta", "--revision=1234", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "cannot specify both channel and revision")
}

func (s *SnapSuite) TestPrintInstalHint(c *check.C) {
	snapCmd.PrintInstallHint("foo_1.assert", []string{"foo_1.snap"})
	c.Check(s.Stdout(), check.Equals, `Install the snap with:
   snap ack foo_1.assert
   snap install foo_1.snap
`)
	s.stdout.Reset()

	cwd, err := os.Getwd()
	c.Assert(err, check.IsNil)
	as := filepath.Join(cwd, "some-dir/foo_1.assert")
	sn := filepath.Join(cwd, "some-dir/foo_1.snap")
	snapCmd.PrintInstallHint(as, []string{sn})
	c.Check(s.Stdout(), check.Equals, `Install the snap with:
   snap ack some-dir/foo_1.assert
   snap install some-dir/foo_1.snap
`)
}

func (s *SnapSuite) TestDownloadDirect(c *check.C) {
	var n int
	restore := snapCmd.MockDownloadContainers(func(snapName string, components []string, tsto *tooling.ToolingStore, opts tooling.DownloadSnapOptions) (*tooling.DownloadedSnap, error) {
		c.Check(snapName, check.Equals, "a-snap")
		c.Check(opts.Revision, check.Equals, snap.R(0))
		c.Check(opts.Basename, check.Equals, "some-base-name")
		c.Check(opts.TargetDir, check.Equals, "some-target-dir")
		c.Check(opts.Channel, check.Equals, "some-channel")
		c.Check(opts.CohortKey, check.Equals, "some-cohort")
		c.Check(opts.OnlyComponents, check.Equals, false)
		n++
		return &tooling.DownloadedSnap{
			Path: "a-snap_1.snap",
			Info: &snap.Info{
				SideInfo: snap.SideInfo{
					RealName: "a-snap",
				},
			},
		}, nil
	})
	defer restore()

	restore = snapCmd.MockDownloadAssertions(func(info *snap.Info, snapPath string, components map[string]*snap.ComponentInfo, tsto *tooling.ToolingStore, opts tooling.DownloadSnapOptions) (string, error) {
		c.Check(info.RealName, check.Equals, "a-snap")
		c.Check(snapPath, check.Equals, "a-snap_1.snap")
		c.Check(components, check.HasLen, 0)
		return "foo_1.assert", nil
	})
	defer restore()

	// check that a direct download got issued
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download",
		"--target-directory=some-target-dir",
		"--basename=some-base-name",
		"--channel=some-channel",
		"--cohort=some-cohort",
		"a-snap"},
	)
	c.Assert(err, check.IsNil)
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestDownloadDirectWithComponents(c *check.C) {
	const basename = ""
	const onlyComponents = false
	s.testDownloadDirectWithComponents(c, basename, onlyComponents)
}

func (s *SnapSuite) TestDownloadDirectWithComponentsOnlyComponents(c *check.C) {
	const basename = ""
	const onlyComponents = true
	s.testDownloadDirectWithComponents(c, basename, onlyComponents)
}

func (s *SnapSuite) TestDownloadDirectWithComponentsBasename(c *check.C) {
	const basename = "base-name"
	const onlyComponents = false
	s.testDownloadDirectWithComponents(c, basename, onlyComponents)
}

func (s *SnapSuite) testDownloadDirectWithComponents(c *check.C, basename string, onlyComponents bool) {
	var n int
	restore := snapCmd.MockDownloadContainers(func(snapName string, components []string, tsto *tooling.ToolingStore, opts tooling.DownloadSnapOptions) (*tooling.DownloadedSnap, error) {
		c.Check(snapName, check.Equals, "a-snap")
		c.Check(components, check.DeepEquals, []string{"comp-1", "comp-2"})
		c.Check(opts.Revision, check.Equals, snap.R(0))
		c.Check(opts.OnlyComponents, check.Equals, onlyComponents)
		c.Check(opts.Basename, check.Equals, basename)
		n++
		return &tooling.DownloadedSnap{
			Path: "a-snap_1.snap",
			Info: &snap.Info{
				SideInfo: snap.SideInfo{
					RealName: "a-snap",
					Revision: snap.R(1),
				},
			},
			Components: []*tooling.DownloadedComponent{
				{
					Path: "a-snap+comp-1_2.comp",
					Info: &snap.ComponentInfo{
						Component: naming.NewComponentRef("a-snap", "comp-1"),
						Type:      snap.StandardComponent,
					},
				},
				{
					Path: "a-snapcomp-2_3.comp",
					Info: &snap.ComponentInfo{
						Component: naming.NewComponentRef("a-snap", "comp-2"),
						Type:      snap.StandardComponent,
					},
				},
			},
		}, nil
	})
	defer restore()

	restore = snapCmd.MockDownloadAssertions(func(info *snap.Info, snapPath string, components map[string]*snap.ComponentInfo, tsto *tooling.ToolingStore, opts tooling.DownloadSnapOptions) (string, error) {
		c.Check(info.RealName, check.Equals, "a-snap")
		c.Check(snapPath, check.Equals, "a-snap_1.snap")
		c.Check(components, check.HasLen, 2)
		return "a-snap_1.assert", nil
	})
	defer restore()

	args := []string{"download", "a-snap+comp-1+comp-2"}
	if basename != "" {
		args = append(args, "--basename="+basename)
	}
	if onlyComponents {
		args = append(args, "--only-components")
	}

	// check that a direct download got issued
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs(args)
	c.Assert(err, check.IsNil)
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestDownloadDirectErrors(c *check.C) {
	var n int
	restore := snapCmd.MockDownloadContainers(func(snapName string, components []string, tsto *tooling.ToolingStore, opts tooling.DownloadSnapOptions) (*tooling.DownloadedSnap, error) {
		n++
		return nil, fmt.Errorf("some-error")
	})
	defer restore()

	// check that a direct download got issued
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download",
		"a-snap"},
	)
	c.Assert(err, check.ErrorMatches, "some-error")
	c.Check(n, check.Equals, 1)
}
