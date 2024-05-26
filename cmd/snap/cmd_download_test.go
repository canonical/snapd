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

	"github.com/ddkwork/golibrary/mylog"
	snapCmd "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store/tooling"
)

// these only cover errors that happen before hitting the network,
// because we're not (yet!) mocking the tooling store

func (s *SnapSuite) TestDownloadBadBasename(c *check.C) {
	_ := mylog.Check2(snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--basename=/foo", "a-snap",
	}))

	c.Check(err, check.ErrorMatches, "cannot specify a path in basename .use --target-dir for that.")
}

func (s *SnapSuite) TestDownloadBadChannelCombo(c *check.C) {
	_ := mylog.Check2(snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--beta", "--channel=foo", "a-snap",
	}))

	c.Check(err, check.ErrorMatches, "Please specify a single channel")
}

func (s *SnapSuite) TestDownloadCohortAndRevision(c *check.C) {
	_ := mylog.Check2(snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--cohort=what", "--revision=1234", "a-snap",
	}))

	c.Check(err, check.ErrorMatches, "cannot specify both cohort and revision")
}

func (s *SnapSuite) TestDownloadChannelAndRevision(c *check.C) {
	_ := mylog.Check2(snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--beta", "--revision=1234", "a-snap",
	}))

	c.Check(err, check.ErrorMatches, "cannot specify both channel and revision")
}

func (s *SnapSuite) TestPrintInstalHint(c *check.C) {
	snapCmd.PrintInstallHint("foo_1.assert", "foo_1.snap")
	c.Check(s.Stdout(), check.Equals, `Install the snap with:
   snap ack foo_1.assert
   snap install foo_1.snap
`)
	s.stdout.Reset()

	cwd := mylog.Check2(os.Getwd())
	c.Assert(err, check.IsNil)
	as := filepath.Join(cwd, "some-dir/foo_1.assert")
	sn := filepath.Join(cwd, "some-dir/foo_1.snap")
	snapCmd.PrintInstallHint(as, sn)
	c.Check(s.Stdout(), check.Equals, `Install the snap with:
   snap ack some-dir/foo_1.assert
   snap install some-dir/foo_1.snap
`)
}

func (s *SnapSuite) TestDownloadDirect(c *check.C) {
	var n int
	restore := snapCmd.MockDownloadDirect(func(snapName string, revision snap.Revision, dlOpts tooling.DownloadSnapOptions) error {
		c.Check(snapName, check.Equals, "a-snap")
		c.Check(revision, check.Equals, snap.R(0))
		c.Check(dlOpts.Basename, check.Equals, "some-base-name")
		c.Check(dlOpts.TargetDir, check.Equals, "some-target-dir")
		c.Check(dlOpts.Channel, check.Equals, "some-channel")
		c.Check(dlOpts.CohortKey, check.Equals, "some-cohort")
		n++
		return nil
	})
	defer restore()

	// check that a direct download got issued
	_ := mylog.Check2(snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download",
		"--target-directory=some-target-dir",
		"--basename=some-base-name",
		"--channel=some-channel",
		"--cohort=some-cohort",
		"a-snap",
	},
	))
	c.Assert(err, check.IsNil)
	c.Check(n, check.Equals, 1)
}

func (s *SnapSuite) TestDownloadDirectErrors(c *check.C) {
	var n int
	restore := snapCmd.MockDownloadDirect(func(snapName string, revision snap.Revision, dlOpts tooling.DownloadSnapOptions) error {
		n++
		return fmt.Errorf("some-error")
	})
	defer restore()

	// check that a direct download got issued
	_ := mylog.Check2(snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download",
		"a-snap",
	},
	))
	c.Assert(err, check.ErrorMatches, "some-error")
	c.Check(n, check.Equals, 1)
}
