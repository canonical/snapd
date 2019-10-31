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
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	snapCmd "github.com/snapcore/snapd/cmd/snap"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

var brandPrivKey, _ = assertstest.GenerateKey(752)

// mockDownloadStore is a helper that can mock a store. It does not provide
// more support than errors right now. To be a fully functional store it
// needs full assertion mocking.
type mockDownloadStore struct {
	StoreSigning *assertstest.StoreStack

	// name -> path
	snaps map[string]string
	// name -> info
	infos map[string]*snap.Info

	downloadCalled    []string
	assertionsFetched []*asserts.Ref
}

func newMockDownloadStore() *mockDownloadStore {
	return &mockDownloadStore{
		StoreSigning: assertstest.NewStoreStack("can0nical", nil),
	}
}

func (m *mockDownloadStore) makeAssertedSnap(c *check.C, snapYaml string) {
	// XXX: maybe make configurable later
	developerID := "can0nical"
	revision := snap.R(1)

	info, err := snap.InfoFromSnapYaml([]byte(snapYaml))
	c.Assert(err, check.IsNil)
	snapName := info.SnapName()

	snapFile := snaptest.MakeTestSnapWithFiles(c, snapYaml, nil)

	snapID := snapName + "-id"
	declA, err := m.StoreSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      snapID,
		"publisher-id": developerID,
		"snap-name":    snapName,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)
	err = m.StoreSigning.Database.Add(declA)
	c.Assert(err, check.IsNil)

	sha3_384, size, err := asserts.SnapFileSHA3_384(snapFile)
	c.Assert(err, check.IsNil)

	revA, err := m.StoreSigning.Sign(asserts.SnapRevisionType, map[string]interface{}{
		"snap-sha3-384": sha3_384,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-id":       snapID,
		"developer-id":  developerID,
		"snap-revision": revision.String(),
		"timestamp":     time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, check.IsNil)
	err = m.StoreSigning.Add(revA)
	c.Assert(err, check.IsNil)

	if !revision.Unset() {
		info.SnapID = snapID
		info.Revision = revision
	}

	if m.snaps == nil {
		m.snaps = make(map[string]string)
		m.infos = make(map[string]*snap.Info)
	}

	m.snaps[snapName] = snapFile
	info.SideInfo.RealName = snapName
	m.infos[snapName] = info
}

func (m *mockDownloadStore) DownloadSnap(name string, opts image.DownloadOptions) (targetFn string, info *snap.Info, err error) {
	m.downloadCalled = append(m.downloadCalled, name)
	if snapPath := m.snaps[name]; snapPath != "" {
		return snapPath, m.infos[name], nil
	}
	return "", nil, fmt.Errorf("mockDownloadStore.DownloadSnap says NO")
}

func (m *mockDownloadStore) AssertionFetcher(db *asserts.Database, save func(asserts.Assertion) error) asserts.Fetcher {
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		headers, err := asserts.HeadersFromPrimaryKey(ref.Type, ref.PrimaryKey)
		if err != nil {
			return nil, err
		}
		m.assertionsFetched = append(m.assertionsFetched, ref)
		return m.StoreSigning.Database.Find(ref.Type, headers)
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

	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{"download", "foo"})
	c.Assert(err, check.ErrorMatches, "mockDownloadStore.DownloadSnap says NO")
}

func (s *SnapSuite) TestDownloadDirectStoreHappy(c *check.C) {
	mockStore := newMockDownloadStore()
	mockStore.makeAssertedSnap(c, "name: foo\nversion: 1.0")

	restore := snapCmd.MockNewDownloadStore(func() (snapCmd.DownloadStore, error) {
		return mockStore, nil
	})
	defer restore()
	restore = snapCmd.MockCmdDownloadTrusted(mockStore.StoreSigning.Trusted)
	defer restore()

	_, err := snapCmd.Parser(snapCmd.Client()).ParseArgs([]string{"download", "foo"})
	c.Assert(err, check.IsNil)
	c.Assert(mockStore.downloadCalled, check.DeepEquals, []string{"foo"})
	// snap-rev, snap-decl, account-key
	c.Assert(mockStore.assertionsFetched, check.HasLen, 3)
}
