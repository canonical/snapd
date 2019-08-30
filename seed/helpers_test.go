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

package seed_test

import (
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type helpersSuite struct {
	testutil.BaseTest

	*seedtest.TestingSeed
	devAcct *asserts.Account
}

var _ = Suite(&helpersSuite{})

func (s *helpersSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.TestingSeed = &seedtest.TestingSeed{}
	s.SetupAssertSigning("canonical", s)

	dir := c.MkDir()

	s.SnapsDir = filepath.Join(dir, "snaps")
	s.AssertsDir = filepath.Join(dir, "assertions")
	err := os.MkdirAll(s.SnapsDir, 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(s.AssertsDir, 0755)
	c.Assert(err, IsNil)

	s.devAcct = assertstest.NewAccount(s.StoreSigning, "developer", map[string]interface{}{
		"account-id": "developerid",
	}, "")

}

const fooSnap = `type: app
name: foo
version: 1.0
`

const barSnap = `type: app
name: bar
version: 2.0
`

func (s *helpersSuite) TestLoadAssertionsNoAssertions(c *C) {
	os.Remove(s.AssertsDir)

	b, err := seed.LoadAssertions(s.AssertsDir, nil)
	c.Check(err, Equals, seed.ErrNoAssertions)
	c.Check(b, IsNil)
}

func (s *helpersSuite) TestLoadAssertions(c *C) {
	_, fooDecl, fooRev := s.MakeAssertedSnap(c, fooSnap, nil, snap.R(1), "developerid")
	_, barDecl, barRev := s.MakeAssertedSnap(c, barSnap, nil, snap.R(2), "developerid")

	s.WriteAssertions("ground.asserts", s.StoreSigning.StoreAccountKey(""))
	s.WriteAssertions("foo.asserts", s.devAcct, fooDecl, fooRev)
	s.WriteAssertions("bar.asserts", barDecl, barRev)

	b, err := seed.LoadAssertions(s.AssertsDir, nil)
	c.Assert(err, IsNil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.StoreSigning.Trusted,
	})
	c.Assert(err, IsNil)

	err = b.CommitTo(db, nil)
	c.Assert(err, IsNil)

	_, err = db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": fooRev.SnapSHA3_384(),
	})
	c.Check(err, IsNil)

	_, err = db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": barRev.SnapSHA3_384(),
	})
	c.Check(err, IsNil)
}

func (s *helpersSuite) TestLoadAssertionsLoadedCallback(c *C) {
	_, fooDecl, fooRev := s.MakeAssertedSnap(c, fooSnap, nil, snap.R(1), "developerid")
	_, barDecl, barRev := s.MakeAssertedSnap(c, barSnap, nil, snap.R(2), "developerid")

	s.WriteAssertions("ground.asserts", s.StoreSigning.StoreAccountKey(""))
	s.WriteAssertions("foo.asserts", s.devAcct, fooDecl, fooRev)
	s.WriteAssertions("bar.asserts", barDecl, barRev)

	counts := make(map[string]int)
	seen := make(map[string]bool)

	loaded := func(ref *asserts.Ref) error {
		if ref.Type == asserts.SnapDeclarationType {
			seen[ref.PrimaryKey[1]] = true
		}
		counts[ref.Type.Name]++
		return nil
	}

	_, err := seed.LoadAssertions(s.AssertsDir, loaded)
	c.Assert(err, IsNil)

	c.Check(seen, DeepEquals, map[string]bool{
		"bardidididididididididididididid": true,
		"foodidididididididididididididid": true,
	})

	// overall
	c.Check(counts, DeepEquals, map[string]int{
		"account":          1,
		"account-key":      1,
		"snap-declaration": 2,
		"snap-revision":    2,
	})
}

func (s *helpersSuite) TestLoadAssertionsLoadedCallbackError(c *C) {
	s.WriteAssertions("ground.asserts", s.StoreSigning.StoreAccountKey(""))

	loaded := func(ref *asserts.Ref) error {
		return fmt.Errorf("boom")

	}

	_, err := seed.LoadAssertions(s.AssertsDir, loaded)
	c.Assert(err, ErrorMatches, "boom")
}
