// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package asserts_test

import (
	"sort"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/testutil"
)

type poolSuite struct {
	testutil.BaseTest

	hub      *assertstest.StoreStack
	dev1Acct *asserts.Account
	dev2Acct *asserts.Account

	decl1     *asserts.TestOnlyDecl
	decl1_1   *asserts.TestOnlyDecl
	rev1_1111 *asserts.TestOnlyRev
	rev1_3333 *asserts.TestOnlyRev

	decl2     *asserts.TestOnlyDecl
	rev2_2222 *asserts.TestOnlyRev

	db *asserts.Database
}

var _ = Suite(&poolSuite{})

func (s *poolSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.hub = assertstest.NewStoreStack("hub", nil)
	s.dev1Acct = assertstest.NewAccount(s.hub, "developer1", map[string]interface{}{
		"account-id": "developer1",
	}, "")
	s.dev2Acct = assertstest.NewAccount(s.hub, "developer2", map[string]interface{}{
		"account-id": "developer2",
	}, "")

	a, err := s.hub.Sign(asserts.TestOnlyDeclType, map[string]interface{}{
		"id":     "one",
		"dev-id": "developer1",
	}, nil, "")
	c.Assert(err, IsNil)
	s.decl1 = a.(*asserts.TestOnlyDecl)

	a, err = s.hub.Sign(asserts.TestOnlyDeclType, map[string]interface{}{
		"id":       "one",
		"dev-id":   "developer1",
		"revision": "1",
	}, nil, "")
	c.Assert(err, IsNil)
	s.decl1_1 = a.(*asserts.TestOnlyDecl)

	a, err = s.hub.Sign(asserts.TestOnlyDeclType, map[string]interface{}{
		"id":     "two",
		"dev-id": "developer2",
	}, nil, "")
	c.Assert(err, IsNil)
	s.decl2 = a.(*asserts.TestOnlyDecl)

	a, err = s.hub.Sign(asserts.TestOnlyRevType, map[string]interface{}{
		"h":      "1111",
		"id":     "one",
		"dev-id": "developer1",
	}, nil, "")
	c.Assert(err, IsNil)
	s.rev1_1111 = a.(*asserts.TestOnlyRev)

	a, err = s.hub.Sign(asserts.TestOnlyRevType, map[string]interface{}{
		"h":      "3333",
		"id":     "one",
		"dev-id": "developer1",
	}, nil, "")
	c.Assert(err, IsNil)
	s.rev1_3333 = a.(*asserts.TestOnlyRev)

	a, err = s.hub.Sign(asserts.TestOnlyRevType, map[string]interface{}{
		"h":      "2222",
		"id":     "two",
		"dev-id": "developer2",
	}, nil, "")
	c.Assert(err, IsNil)
	s.rev2_2222 = a.(*asserts.TestOnlyRev)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.hub.Trusted,
	})
	c.Assert(err, IsNil)
	s.db = db
}

func (s *poolSuite) TestAddUnresolved(c *C) {
	pool := asserts.NewPool(s.db, 64)

	at1 := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"1111"}},
		Revision: asserts.RevisionNotKnown,
	}
	err := pool.AddUnresolved(at1, "for_one") // group num: 0
	c.Assert(err, IsNil)

	toResolve, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {at1},
	})
}

func (s *poolSuite) TestAddUnresolvedPredefined(c *C) {
	pool := asserts.NewPool(s.db, 64)

	at := s.hub.TrustedAccount.At()
	at.Revision = asserts.RevisionNotKnown
	err := pool.AddUnresolved(at, "for_one")
	c.Assert(err, IsNil)

	// nothing to resolve
	toResolve, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
}

func (s *poolSuite) TestAddUnresolvedGrouping(c *C) {
	pool := asserts.NewPool(s.db, 64)

	storeKeyAt := s.hub.StoreAccountKey("").At()

	pool.AddUnresolved(storeKeyAt, "for_two") // group num: 0
	pool.AddUnresolved(storeKeyAt, "for_one") // group num: 1

	toResolve, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0, 1): {storeKeyAt},
	})
}

func (s *poolSuite) TestAddUnresolvedDup(c *C) {
	pool := asserts.NewPool(s.db, 64)

	storeKeyAt := s.hub.StoreAccountKey("").At()

	pool.AddUnresolved(storeKeyAt, "for_one") // group num: 0
	pool.AddUnresolved(storeKeyAt, "for_one") // group num: 0

	toResolve, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKeyAt},
	})
}

type byAtRevision []*asserts.AtRevision

func (ats byAtRevision) Len() int {
	return len(ats)
}

func (ats byAtRevision) Less(i, j int) bool {
	return ats[i].Ref.Unique() < ats[j].Ref.Unique()
}

func (ats byAtRevision) Swap(i, j int) {
	ats[i], ats[j] = ats[j], ats[i]
}

func sortToResolve(toResolve map[asserts.Grouping][]*asserts.AtRevision) {
	for _, ats := range toResolve {
		sort.Sort(byAtRevision(ats))
	}
}

func (s *poolSuite) TestFetch(c *C) {
	pool := asserts.NewPool(s.db, 64)

	at1111 := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"1111"}},
		Revision: asserts.RevisionNotKnown,
	}
	err := pool.AddUnresolved(at1111, "for_one")
	c.Assert(err, IsNil)

	toResolve, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {at1111},
	})

	err = pool.Add(s.rev1_1111, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)

	toResolve, err = pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	dev1AcctAt := s.dev1Acct.At()
	dev1AcctAt.Revision = asserts.RevisionNotKnown
	decl1At := s.decl1.At()
	decl1At.Revision = asserts.RevisionNotKnown
	storeKeyAt := s.hub.StoreAccountKey("").At()
	storeKeyAt.Revision = asserts.RevisionNotKnown
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKeyAt, dev1AcctAt, decl1At},
	})

	c.Check(pool.Err("for_one"), IsNil)
}

func (s *poolSuite) TestPushSuggestionForPrerequisite(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""))

	pool := asserts.NewPool(s.db, 64)

	at1111 := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"1111"}},
		Revision: asserts.RevisionNotKnown,
	}
	err := pool.AddUnresolved(at1111, "for_one")
	c.Assert(err, IsNil)

	toResolve, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {at1111},
	})

	err = pool.Add(s.rev1_1111, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)

	// push prerequisite suggestion
	err = pool.Add(s.decl1, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)

	toResolve, err = pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	dev1AcctAt := s.dev1Acct.At()
	dev1AcctAt.Revision = asserts.RevisionNotKnown
	storeKey := s.hub.StoreAccountKey("")
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKey.At(), dev1AcctAt},
	})

	c.Check(pool.Err("for_one"), IsNil)

	err = pool.Add(s.dev1Acct, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)

	toResolve, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)

	// TODO: test after committing
}

func (s *poolSuite) TestPushSuggestionForNew(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""))

	pool := asserts.NewPool(s.db, 64)

	atOne := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyDeclType, PrimaryKey: []string{"one"}},
		Revision: asserts.RevisionNotKnown,
	}
	err := pool.AddUnresolved(atOne, "for_one")
	c.Assert(err, IsNil)

	toResolve, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {atOne},
	})

	err = pool.Add(s.decl1, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)

	// new push suggestion
	err = pool.Add(s.rev1_1111, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)

	toResolve, err = pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	dev1AcctAt := s.dev1Acct.At()
	dev1AcctAt.Revision = asserts.RevisionNotKnown
	storeKeyAt := s.hub.StoreAccountKey("").At()
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKeyAt, dev1AcctAt},
	})

	c.Check(pool.Err("for_one"), IsNil)

	err = pool.Add(s.dev1Acct, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)

	toResolve, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)

	// TODO: test after committing
}

func (s *poolSuite) TestAddUnresolvedUnresolved(c *C) {
	pool := asserts.NewPool(s.db, 64)

	at1 := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"1111"}},
		Revision: asserts.RevisionNotKnown,
	}
	err := pool.AddUnresolved(at1, "for_one")
	c.Assert(err, IsNil)

	toResolve, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {at1},
	})

	toResolve, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)

	c.Check(pool.Err("for_one"), Equals, asserts.ErrUnresolved)
}

func (s *poolSuite) TestAddFormatTooNew(c *C) {
	pool := asserts.NewPool(s.db, 64)

	_, err := pool.ToResolve()
	c.Assert(err, IsNil)

	var a asserts.Assertion
	(func() {
		restore := asserts.MockMaxSupportedFormat(asserts.TestOnlyDeclType, 2)
		defer restore()

		a, err = s.hub.Sign(asserts.TestOnlyDeclType, map[string]interface{}{
			"id":     "three",
			"dev-id": "developer1",
			"format": "2",
		}, nil, "")
		c.Assert(err, IsNil)
	})()

	gSuggestion, err := pool.Singleton("suggestion")
	c.Assert(err, IsNil)

	err = pool.Add(a, gSuggestion)
	c.Assert(err, ErrorMatches, `proposed "test-only-decl" assertion has format 2 but 0 is latest supported`)
}

func (s *poolSuite) TestAddOlderIgnored(c *C) {
	pool := asserts.NewPool(s.db, 64)

	_, err := pool.ToResolve()
	c.Assert(err, IsNil)

	gSuggestion, err := pool.Singleton("suggestion")
	c.Assert(err, IsNil)

	err = pool.Add(s.decl1_1, gSuggestion)
	c.Assert(err, IsNil)

	err = pool.Add(s.decl1, gSuggestion)
	c.Assert(err, IsNil)

	toResolve, err := pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	dev1AcctAt := s.dev1Acct.At()
	dev1AcctAt.Revision = asserts.RevisionNotKnown
	storeKeyAt := s.hub.StoreAccountKey("").At()
	storeKeyAt.Revision = asserts.RevisionNotKnown

	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		gSuggestion: {storeKeyAt, dev1AcctAt},
	})
}

func (s *poolSuite) TestUnknownGroup(c *C) {
	pool := asserts.NewPool(s.db, 64)

	_, err := pool.Singleton("suggestion")
	c.Assert(err, IsNil)
	// sanity
	c.Check(pool.Err("suggestion"), IsNil)

	c.Check(pool.Err("foo"), ErrorMatches, "unknown group: foo")
}
