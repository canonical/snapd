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

	"github.com/snapcore/snapd/asserts"
	snapCmd "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/snap"
)

// mockDownloadStore is a helper that can mock a store. It does not provide
// more support than errors right now. To be a fully functional store it
// needs full assertion mocking.
type mockDownloadStore struct{}

func (m *mockDownloadStore) DownloadSnap(name string, opts image.DownloadOptions) (targetFn string, info *snap.Info, err error) {
	return "", nil, fmt.Errorf("mockDownloadStore cannot provide snaps")
}

func (m *mockDownloadStore) AssertionFetcher(db *asserts.Database, save func(asserts.Assertion) error) asserts.Fetcher {
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return nil, fmt.Errorf("mockDownloadStore does not have assertions")
	}
	return asserts.NewFetcher(db, retrieve, save)
}

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

	c.Check(err, check.ErrorMatches, "Please specify a single channel")
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
	snapCmd.PrintInstallHint("foo_1.assert", "foo_1.snap")
	c.Check(s.Stdout(), check.Equals, `Install the snap with:
   snap ack foo_1.assert
   snap install foo_1.snap
`)
	s.stdout.Reset()

	cwd, err := os.Getwd()
	c.Assert(err, check.IsNil)
	as := filepath.Join(cwd, "some-dir/foo_1.assert")
	sn := filepath.Join(cwd, "some-dir/foo_1.snap")
	snapCmd.PrintInstallHint(as, sn)
	c.Check(s.Stdout(), check.Equals, `Install the snap with:
   snap ack some-dir/foo_1.assert
   snap install some-dir/foo_1.snap
`)

}

func (s *SnapSuite) TestDownloadDirectStoreError(c *check.C) {
	restore := snapCmd.MockNewDownloadStore(func() (snapCmd.DownloadStore, error) {
		return &mockDownloadStore{}, nil
	})
	defer restore()

	// we just test here we hit our fake tooling store, mocking
	// the full download is more work, i.e. we need to mock the
	// assertions download as well
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{"download", "a-snap"})
	c.Assert(err, check.ErrorMatches, "mockDownloadStore cannot provide snaps")
}
