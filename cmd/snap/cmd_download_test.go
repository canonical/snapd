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
	"gopkg.in/check.v1"

	snap "github.com/snapcore/snapd/cmd/snap"
)

// these only cover errors that happen before hitting the network,
// because we're not (yet!) mocking the tooling store

func (s *SnapSuite) TestDownloadBadBasename(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"download", "--basename=/foo", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "cannot specify a path in basename .use --target-dir for that.")
}

func (s *SnapSuite) TestDownloadBadChannelCombo(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"download", "--beta", "--channel=foo", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "Please specify a single channel")
}

func (s *SnapSuite) TestDownloadCohortAndRevision(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"download", "--cohort=what", "--revision=1234", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "cannot specify both cohort and revision")
}

func (s *SnapSuite) TestDownloadChannelAndRevision(c *check.C) {
	_, err := snap.Parser(snap.Client()).ParseArgs([]string{
		"download", "--beta", "--revision=1234", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "cannot specify both channel and revision")
}
