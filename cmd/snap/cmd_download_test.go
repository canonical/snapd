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
	"crypto"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/client"
	snapCmd "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

// these only cover errors that happen before hitting the network,
// because we're not (yet!) mocking the tooling store

func (s *SnapSuite) TestDownloadBadBasename(c *check.C) {
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--basename=/foo", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "cannot specify a path in basename .use --target-dir for that.")
}

func (s *SnapSuite) TestDownloadBadBasenameIndirect(c *check.C) {
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--indirect", "--basename=/foo", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "cannot specify a path in basename .use --target-dir for that.")
}

func (s *SnapSuite) TestDownloadBadChannelCombo(c *check.C) {
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--beta", "--channel=foo", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "Please specify a single channel")
}

func (s *SnapSuite) TestDownloadBadChannelComboIndirect(c *check.C) {
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--indirect", "--beta", "--channel=foo", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "Please specify a single channel")
}

func (s *SnapSuite) TestDownloadCohortAndRevision(c *check.C) {
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--cohort=what", "--revision=1234", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "cannot specify both cohort and revision")
}

func (s *SnapSuite) TestDownloadCohortAndRevisionIndirect(c *check.C) {
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--indirect", "--cohort=what", "--revision=1234", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "cannot specify both cohort and revision")
}

func (s *SnapSuite) TestDownloadChannelAndRevision(c *check.C) {
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--beta", "--revision=1234", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "cannot specify both channel and revision")
}

func (s *SnapSuite) TestDownloadChannelAndRevisionIndirect(c *check.C) {
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{
		"download", "--indirect", "--beta", "--revision=1234", "a-snap",
	})

	c.Check(err, check.ErrorMatches, "cannot specify both channel and revision")
}

func (s *SnapSuite) TestDownloadViaSnapd(c *check.C) {
	n := 0
	s.RedirectClientToTestServer(func(w http.ResponseWriter, r *http.Request) {
		switch n {
		case 0:
			c.Check(r.URL.Path, check.Equals, "/v2/download")
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Header().Set("Content-Disposition", "attachment; filename=a-snap_1.snap")
			mockContent := []byte("file-content\n")
			h := crypto.SHA3_384.New()
			h.Write(mockContent)
			w.Header().Set("Snap-Sha3-384", fmt.Sprintf("%x", h.Sum(nil)))
			w.Write(mockContent)
		case 1:
			c.Check(r.URL.Path, check.Equals, "/v2/assertions/snap-revision")
			w.WriteHeader(418)
		default:
			c.Fatalf("expected to get 1 requests, now on %d", n+1)
		}
		n++
	})

	// we just test here that we hits the snapd API, testing this fully
	// would require a full assertion chain
	tmpdir := c.MkDir()
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{"download", "--indirect", "a-snap", "--target-directory", tmpdir})
	c.Assert(err, check.ErrorMatches, `server error: "418 I'm a teapot"`)
	c.Assert(n, check.Equals, 2)
	c.Assert(filepath.Join(tmpdir, "a-snap_1.snap"), testutil.FilePresent)
}

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

func (s *SnapSuite) TestDownloadDirect(c *check.C) {
	restore := snapCmd.MockNewDownloadStore(func() (snapCmd.DownloadStore, error) {
		return &mockDownloadStore{}, nil
	})
	defer restore()

	// we just test here that direct hits our fake tooling store,
	// mocking the full download is more work, i.e. we need to mock
	// the assertions download as well
	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{"download", "a-snap"})
	c.Assert(err, check.ErrorMatches, "mockDownloadStore cannot provide snaps")
}

func (s *SnapSuite) TestDownloadAutoFallback(c *check.C) {
	restore := snapCmd.MockNewDownloadStore(func() (snapCmd.DownloadStore, error) {
		return &mockDownloadStore{}, nil
	})
	defer restore()

	// we pretend we can't talk to snapd
	n := 0
	cli := snapCmd.Client()
	cli.Hijack(func(*http.Request) (*http.Response, error) {
		n++
		return nil, client.ConnectionError{Err: fmt.Errorf("no snapd")}
	})

	// ensure we hit the tooling store, testing this fully would require
	// a full assertion chain
	_, err := snapCmd.Parser(cli).ParseArgs([]string{"download", "--indirect", "a-snap"})
	c.Assert(err, check.ErrorMatches, "mockDownloadStore cannot provide snaps")
	c.Assert(n, check.Equals, 1)
}

func (s *SnapSuite) TestPrintInstallHint(c *check.C) {
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
