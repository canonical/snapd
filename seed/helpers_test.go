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

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type helpersSuite struct {
	testutil.BaseTest

	*seedtest.SeedSnaps

	assertsDir string

	devAcct *asserts.Account
}

var _ = Suite(&helpersSuite{})

func (s *helpersSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))

	s.SeedSnaps = &seedtest.SeedSnaps{}
	s.SetupAssertSigning("canonical")

	dir := c.MkDir()
	s.assertsDir = filepath.Join(dir, "assertions")
	mylog.Check(os.MkdirAll(s.assertsDir, 0755))


	s.devAcct = assertstest.NewAccount(s.StoreSigning, "developer", map[string]interface{}{
		"account-id": "developerid",
	}, "")
}

func (s *helpersSuite) writeAssertions(fn string, assertions ...asserts.Assertion) {
	fn = filepath.Join(s.assertsDir, fn)
	seedtest.WriteAssertions(fn, assertions...)
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
	os.Remove(s.assertsDir)

	b := mylog.Check2(seed.LoadAssertions(s.assertsDir, nil))
	c.Check(err, Equals, seed.ErrNoAssertions)
	c.Check(b, IsNil)
}

func (s *helpersSuite) TestLoadAssertions(c *C) {
	fooDecl, fooRev := s.MakeAssertedSnap(c, fooSnap, nil, snap.R(1), "developerid")
	barDecl, barRev := s.MakeAssertedSnap(c, barSnap, nil, snap.R(2), "developerid")

	s.writeAssertions("ground.asserts", s.StoreSigning.StoreAccountKey(""))
	s.writeAssertions("foo.asserts", s.devAcct, fooDecl, fooRev)
	s.writeAssertions("bar.asserts", barDecl, barRev)

	b := mylog.Check2(seed.LoadAssertions(s.assertsDir, nil))


	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.StoreSigning.Trusted,
	}))

	mylog.Check(b.CommitTo(db, nil))


	_ = mylog.Check2(db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": fooRev.SnapSHA3_384(),
	}))
	c.Check(err, IsNil)

	_ = mylog.Check2(db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": barRev.SnapSHA3_384(),
	}))
	c.Check(err, IsNil)
}

func (s *helpersSuite) TestLoadAssertionsLoadedCallback(c *C) {
	fooDecl, fooRev := s.MakeAssertedSnap(c, fooSnap, nil, snap.R(1), "developerid")
	barDecl, barRev := s.MakeAssertedSnap(c, barSnap, nil, snap.R(2), "developerid")

	s.writeAssertions("ground.asserts", s.StoreSigning.StoreAccountKey(""))
	s.writeAssertions("foo.asserts", s.devAcct, fooDecl, fooRev)
	s.writeAssertions("bar.asserts", barDecl, barRev)

	counts := make(map[string]int)
	seen := make(map[string]bool)

	loaded := func(ref *asserts.Ref) error {
		if ref.Type == asserts.SnapDeclarationType {
			seen[ref.PrimaryKey[1]] = true
		}
		counts[ref.Type.Name]++
		return nil
	}

	_ := mylog.Check2(seed.LoadAssertions(s.assertsDir, loaded))


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
	s.writeAssertions("ground.asserts", s.StoreSigning.StoreAccountKey(""))

	loaded := func(ref *asserts.Ref) error {
		return fmt.Errorf("boom")
	}

	_ := mylog.Check2(seed.LoadAssertions(s.assertsDir, loaded))
	c.Assert(err, ErrorMatches, "boom")
}
