// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020-2022 Canonical Ltd
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
	"errors"
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

	seq1_1111r5 *asserts.TestOnlySeq
	seq1_1111r6 *asserts.TestOnlySeq
	seq2_1111r7 *asserts.TestOnlySeq
	seq3_1111r5 *asserts.TestOnlySeq

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

	// sequence-forming

	a, err = s.hub.Sign(asserts.TestOnlySeqType, map[string]interface{}{
		"n":        "1111",
		"sequence": "1",
		"id":       "one",
		"dev-id":   "developer1",
		"revision": "5",
	}, nil, "")
	c.Assert(err, IsNil)
	s.seq1_1111r5 = a.(*asserts.TestOnlySeq)

	a, err = s.hub.Sign(asserts.TestOnlySeqType, map[string]interface{}{
		"n":        "1111",
		"sequence": "1",
		"id":       "one",
		"dev-id":   "developer1",
		"revision": "6",
	}, nil, "")
	c.Assert(err, IsNil)
	s.seq1_1111r6 = a.(*asserts.TestOnlySeq)

	a, err = s.hub.Sign(asserts.TestOnlySeqType, map[string]interface{}{
		"n":        "1111",
		"sequence": "2",
		"id":       "one",
		"dev-id":   "developer1",
		"revision": "7",
	}, nil, "")
	c.Assert(err, IsNil)
	s.seq2_1111r7 = a.(*asserts.TestOnlySeq)

	a, err = s.hub.Sign(asserts.TestOnlySeqType, map[string]interface{}{
		"n":        "1111",
		"sequence": "3",
		"id":       "one",
		"dev-id":   "developer1",
		"revision": "4",
	}, nil, "")
	c.Assert(err, IsNil)
	s.seq3_1111r5 = a.(*asserts.TestOnlySeq)

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

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {at1},
	})
	c.Check(toResolveSeq, HasLen, 0)
}

func (s *poolSuite) TestAddUnresolvedPredefined(c *C) {
	pool := asserts.NewPool(s.db, 64)

	at := s.hub.TrustedAccount.At()
	at.Revision = asserts.RevisionNotKnown
	err := pool.AddUnresolved(at, "for_one")
	c.Assert(err, IsNil)

	// nothing to resolve
	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)
}

func (s *poolSuite) TestAddUnresolvedGrouping(c *C) {
	pool := asserts.NewPool(s.db, 64)

	storeKeyAt := s.hub.StoreAccountKey("").At()

	pool.AddUnresolved(storeKeyAt, "for_two") // group num: 0
	pool.AddUnresolved(storeKeyAt, "for_one") // group num: 1

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0, 1): {storeKeyAt},
	})
	c.Check(toResolveSeq, HasLen, 0)
}

func (s *poolSuite) TestAddUnresolvedDup(c *C) {
	pool := asserts.NewPool(s.db, 64)

	storeKeyAt := s.hub.StoreAccountKey("").At()

	pool.AddUnresolved(storeKeyAt, "for_one") // group num: 0
	pool.AddUnresolved(storeKeyAt, "for_one") // group num: 0

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKeyAt},
	})
	c.Check(toResolveSeq, HasLen, 0)
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

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {at1111},
	})
	c.Check(toResolveSeq, HasLen, 0)

	ok, err := pool.Add(s.rev1_1111, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
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
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)
}

func (s *poolSuite) TestFetchSequenceForming(c *C) {
	pool := asserts.NewPool(s.db, 64)

	// revision and sequence not set
	atseq := &asserts.AtSequence{
		Type:        asserts.TestOnlySeqType,
		SequenceKey: []string{"1111"},
		Revision:    asserts.RevisionNotKnown,
	}
	err := pool.AddUnresolvedSequence(atseq, "for_one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, DeepEquals, map[asserts.Grouping][]*asserts.AtSequence{
		asserts.MakePoolGrouping(0): {atseq},
	})

	// resolve
	ok, err := pool.Add(s.seq1_1111r5, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)

	storeKeyAt := s.hub.StoreAccountKey("").At()
	storeKeyAt.Revision = asserts.RevisionNotKnown
	c.Check(toResolveSeq, HasLen, 0)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKeyAt},
	})

	ok, err = pool.Add(s.hub.StoreAccountKey(""), asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	c.Check(pool.Err("for_one"), IsNil)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Assert(pool.Err("for_one"), IsNil)
}

func (s *poolSuite) TestCompleteFetch(c *C) {
	pool := asserts.NewPool(s.db, 64)

	at1111 := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"1111"}},
		Revision: asserts.RevisionNotKnown,
	}
	err := pool.AddUnresolved(at1111, "for_one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {at1111},
	})
	c.Check(toResolveSeq, HasLen, 0)

	ok, err := pool.Add(s.rev1_1111, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	dev1AcctAt := s.dev1Acct.At()
	dev1AcctAt.Revision = asserts.RevisionNotKnown
	decl1At := s.decl1.At()
	decl1At.Revision = asserts.RevisionNotKnown
	storeKey := s.hub.StoreAccountKey("")
	storeKeyAt := storeKey.At()
	storeKeyAt.Revision = asserts.RevisionNotKnown
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKeyAt, dev1AcctAt, decl1At},
	})
	c.Check(toResolveSeq, HasLen, 0)

	b := asserts.NewBatch(nil)
	err = b.Add(s.decl1)
	c.Assert(err, IsNil)
	err = b.Add(storeKey)
	c.Assert(err, IsNil)
	err = b.Add(s.dev1Acct)
	c.Assert(err, IsNil)

	ok, err = pool.AddBatch(b, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Assert(pool.Err("for_one"), IsNil)

	a, err := at1111.Ref.Resolve(s.db.Find)
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.TestOnlyRev).H(), Equals, "1111")
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

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {at1111},
	})
	c.Check(toResolveSeq, HasLen, 0)

	ok, err := pool.Add(s.rev1_1111, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	// push prerequisite suggestion
	ok, err = pool.Add(s.decl1, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	dev1AcctAt := s.dev1Acct.At()
	dev1AcctAt.Revision = asserts.RevisionNotKnown
	storeKey := s.hub.StoreAccountKey("")
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKey.At(), dev1AcctAt},
	})
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)

	ok, err = pool.Add(s.dev1Acct, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Assert(pool.Err("for_one"), IsNil)

	a, err := at1111.Ref.Resolve(s.db.Find)
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.TestOnlyRev).H(), Equals, "1111")
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

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {atOne},
	})
	c.Check(toResolveSeq, HasLen, 0)

	ok, err := pool.Add(s.decl1, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	// new push suggestion
	ok, err = pool.Add(s.rev1_1111, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	dev1AcctAt := s.dev1Acct.At()
	dev1AcctAt.Revision = asserts.RevisionNotKnown
	storeKeyAt := s.hub.StoreAccountKey("").At()
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKeyAt, dev1AcctAt},
	})
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)

	ok, err = pool.Add(s.dev1Acct, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Assert(pool.Err("for_one"), IsNil)

	a, err := s.rev1_1111.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.TestOnlyRev).H(), Equals, "1111")
}

func (s *poolSuite) TestPushSuggestionForNewSeqForming(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""))

	pool := asserts.NewPool(s.db, 64)

	atOne := &asserts.AtSequence{
		Type:        asserts.TestOnlySeqType,
		SequenceKey: []string{"1111"},
		Revision:    asserts.RevisionNotKnown,
	}
	err := pool.AddUnresolvedSequence(atOne, "for_one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, DeepEquals, map[asserts.Grouping][]*asserts.AtSequence{
		asserts.MakePoolGrouping(0): {atOne},
	})

	ok, err := pool.Add(s.seq1_1111r5, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	// new push suggestion
	ok, err = pool.Add(s.seq2_1111r7, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	dev1AcctAt := s.dev1Acct.At()
	dev1AcctAt.Revision = asserts.RevisionNotKnown

	c.Check(toResolve, HasLen, 1)
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Assert(pool.Err("for_one"), IsNil)

	a, err := s.seq2_1111r7.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.TestOnlySeq).N(), Equals, "1111")
	c.Check(a.Revision(), Equals, 7)
}

func (s *poolSuite) TestPushSuggestionForNewViaBatch(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""))

	pool := asserts.NewPool(s.db, 64)

	atOne := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyDeclType, PrimaryKey: []string{"one"}},
		Revision: asserts.RevisionNotKnown,
	}
	err := pool.AddUnresolved(atOne, "for_one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {atOne},
	})
	c.Check(toResolveSeq, HasLen, 0)

	b := asserts.NewBatch(nil)
	err = b.Add(s.decl1)
	c.Assert(err, IsNil)

	// new push suggestions
	err = b.Add(s.rev1_1111)
	c.Assert(err, IsNil)
	err = b.Add(s.rev1_3333)
	c.Assert(err, IsNil)

	ok, err := pool.AddBatch(b, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	dev1AcctAt := s.dev1Acct.At()
	dev1AcctAt.Revision = asserts.RevisionNotKnown
	storeKeyAt := s.hub.StoreAccountKey("").At()
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKeyAt, dev1AcctAt},
	})
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)

	ok, err = pool.Add(s.dev1Acct, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Assert(pool.Err("for_one"), IsNil)

	a, err := s.rev1_1111.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.TestOnlyRev).H(), Equals, "1111")

	a, err = s.rev1_3333.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.TestOnlyRev).H(), Equals, "3333")
}

func (s *poolSuite) TestAddUnresolvedUnresolved(c *C) {
	pool := asserts.NewPool(s.db, 64)

	at1 := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"1111"}},
		Revision: asserts.RevisionNotKnown,
	}
	err := pool.AddUnresolved(at1, "for_one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {at1},
	})
	c.Check(toResolveSeq, HasLen, 0)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), Equals, asserts.ErrUnresolved)
}

func (s *poolSuite) TestAddFormatTooNew(c *C) {
	pool := asserts.NewPool(s.db, 64)

	_, _, err := pool.ToResolve()
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

	ok, err := pool.Add(a, gSuggestion)
	c.Check(err, IsNil)
	c.Check(ok, Equals, false)
	c.Assert(pool.Err("suggestion"), ErrorMatches, `proposed "test-only-decl" assertion has format 2 but 0 is latest supported`)
}

func (s *poolSuite) TestAddOlderIgnored(c *C) {
	pool := asserts.NewPool(s.db, 64)

	_, _, err := pool.ToResolve()
	c.Assert(err, IsNil)

	gSuggestion, err := pool.Singleton("suggestion")
	c.Assert(err, IsNil)

	ok, err := pool.Add(s.decl1_1, gSuggestion)
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	ok, err = pool.Add(s.decl1, gSuggestion)
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	dev1AcctAt := s.dev1Acct.At()
	dev1AcctAt.Revision = asserts.RevisionNotKnown
	storeKeyAt := s.hub.StoreAccountKey("").At()
	storeKeyAt.Revision = asserts.RevisionNotKnown

	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		gSuggestion: {storeKeyAt, dev1AcctAt},
	})
	c.Check(toResolveSeq, HasLen, 0)
}

func (s *poolSuite) TestUnknownGroup(c *C) {
	pool := asserts.NewPool(s.db, 64)

	_, err := pool.Singleton("suggestion")
	c.Assert(err, IsNil)
	// validity
	c.Check(pool.Err("suggestion"), IsNil)

	c.Check(pool.Err("foo"), Equals, asserts.ErrUnknownPoolGroup)
}

func (s *poolSuite) TestAddCurrentRevision(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""), s.dev1Acct, s.decl1)

	pool := asserts.NewPool(s.db, 64)

	atDev1Acct := s.dev1Acct.At()
	atDev1Acct.Revision = asserts.RevisionNotKnown
	err := pool.AddUnresolved(atDev1Acct, "one")
	c.Assert(err, IsNil)

	atDecl1 := s.decl1.At()
	atDecl1.Revision = asserts.RevisionNotKnown
	err = pool.AddUnresolved(atDecl1, "one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)

	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {s.dev1Acct.At(), s.decl1.At()},
	})
	c.Check(toResolveSeq, HasLen, 0)

	// re-adding of current revisions, is not what we expect
	// but needs to not produce unnecessary roundtrips

	ok, err := pool.Add(s.hub.StoreAccountKey(""), asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	// this will be kept marked as unresolved until the ToResolve
	ok, err = pool.Add(s.dev1Acct, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	ok, err = pool.Add(s.decl1_1, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Assert(toResolve, HasLen, 0)
	c.Assert(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("one"), IsNil)
}

func (s *poolSuite) TestAddCurrentRevisionSeqForming(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""), s.dev1Acct, s.decl1)

	pool := asserts.NewPool(s.db, 64)

	atSeq := &asserts.AtSequence{
		Type:        asserts.TestOnlySeqType,
		SequenceKey: []string{"1111"},
		Revision:    asserts.RevisionNotKnown,
	}
	err := pool.AddUnresolvedSequence(atSeq, "one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)

	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, DeepEquals, map[asserts.Grouping][]*asserts.AtSequence{
		asserts.MakePoolGrouping(0): {
			&asserts.AtSequence{
				Type:        s.seq1_1111r5.Type(),
				SequenceKey: []string{"1111"},
				Revision:    asserts.RevisionNotKnown,
			}},
	})

	// re-adding of current revisions, is not what we expect
	// but needs to not produce unnecessary roundtrips

	ok, err := pool.Add(s.hub.StoreAccountKey(""), asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	// this will be kept marked as unresolved until the ToResolve
	ok, err = pool.Add(s.seq1_1111r5, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Assert(toResolve, HasLen, 0)
	c.Assert(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("one"), IsNil)
}

func (s *poolSuite) TestUpdate(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""))
	assertstest.AddMany(s.db, s.dev1Acct, s.decl1, s.rev1_1111)
	assertstest.AddMany(s.db, s.dev2Acct, s.decl2, s.rev2_2222)

	pool := asserts.NewPool(s.db, 64)

	err := pool.AddToUpdate(s.decl1.Ref(), "for_one") // group num: 0
	c.Assert(err, IsNil)
	err = pool.AddToUpdate(s.decl2.Ref(), "for_two") // group num: 1
	c.Assert(err, IsNil)

	storeKeyAt := s.hub.StoreAccountKey("").At()

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0, 1): {storeKeyAt},
		asserts.MakePoolGrouping(0):    {s.dev1Acct.At(), s.decl1.At()},
		asserts.MakePoolGrouping(1):    {s.dev2Acct.At(), s.decl2.At()},
	})
	c.Check(toResolveSeq, HasLen, 0)

	ok, err := pool.Add(s.decl1_1, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	at2222 := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"2222"}},
		Revision: asserts.RevisionNotKnown,
	}
	err = pool.AddUnresolved(at2222, "for_two")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(1): {&asserts.AtRevision{
			Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"2222"}},
			Revision: 0,
		}},
	})
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)
	c.Check(pool.Err("for_two"), IsNil)
}

func (s *poolSuite) TestUpdateSeqFormingUnpinnedNewerSequence(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""), s.seq1_1111r5)

	pool := asserts.NewPool(s.db, 64)

	atseq := &asserts.AtSequence{
		Type:        s.seq1_1111r5.Type(),
		SequenceKey: []string{"1111"},
		Sequence:    1,
		Revision:    5,
	}
	err := pool.AddSequenceToUpdate(atseq, "for_one") // group num: 0
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {s.hub.StoreAccountKey(s.dev1Acct.SignKeyID()).At()},
	})
	c.Check(toResolveSeq, DeepEquals, map[asserts.Grouping][]*asserts.AtSequence{
		asserts.MakePoolGrouping(0): {
			&asserts.AtSequence{
				Type:        s.seq1_1111r5.Type(),
				SequenceKey: []string{"1111"},
				Sequence:    1,
				Revision:    5,
			}},
	})

	c.Check(pool.Err("for_one"), IsNil)

	// resolve with sequence 3
	ok, err := pool.Add(s.seq3_1111r5, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)

	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)
	c.Check(pool.Err("for_one"), IsNil)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Assert(pool.Err("for_one"), IsNil)

	// sequence point 1, revision 5 is still in the db.
	_, err = s.seq1_1111r5.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)

	// and sequence point 3 revision 5 is in the database.
	_, err = s.seq3_1111r5.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)
}

func (s *poolSuite) TestUpdateSeqFormingUnpinnedSameSequenceNewerRev(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""), s.seq1_1111r5)

	pool := asserts.NewPool(s.db, 64)

	atseq := &asserts.AtSequence{
		Type:        s.seq1_1111r5.Type(),
		SequenceKey: []string{"1111"},
		Sequence:    1,
		Revision:    5,
	}
	err := pool.AddSequenceToUpdate(atseq, "for_one") // group num: 0
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {s.hub.StoreAccountKey(s.dev1Acct.SignKeyID()).At()},
	})
	c.Check(toResolveSeq, DeepEquals, map[asserts.Grouping][]*asserts.AtSequence{
		asserts.MakePoolGrouping(0): {
			&asserts.AtSequence{
				Type:        s.seq1_1111r5.Type(),
				SequenceKey: []string{"1111"},
				Sequence:    1,
				Revision:    5,
			}},
	})

	c.Check(pool.Err("for_one"), IsNil)

	// resolve
	ok, err := pool.Add(s.seq1_1111r6, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)

	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)
	c.Check(pool.Err("for_one"), IsNil)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Assert(pool.Err("for_one"), IsNil)

	// sequence point 1, revision 5 is still in the database.
	_, err = s.seq1_1111r5.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)

	// and sequence point 1 revision 6 is in the database.
	_, err = s.seq1_1111r6.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)
}

func (s *poolSuite) TestUpdateSeqFormingUnpinnedSameSequenceSameRevNoop(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""), s.seq1_1111r5)

	pool := asserts.NewPool(s.db, 64)

	atseq := &asserts.AtSequence{
		Type:        s.seq1_1111r5.Type(),
		SequenceKey: []string{"1111"},
		Sequence:    1,
		Revision:    5,
	}
	err := pool.AddSequenceToUpdate(atseq, "for_one") // group num: 0
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {s.hub.StoreAccountKey(s.dev1Acct.SignKeyID()).At()},
	})
	c.Check(toResolveSeq, DeepEquals, map[asserts.Grouping][]*asserts.AtSequence{
		asserts.MakePoolGrouping(0): {
			&asserts.AtSequence{
				Type:        s.seq1_1111r5.Type(),
				SequenceKey: []string{"1111"},
				Sequence:    1,
				Revision:    5,
			}},
	})

	c.Check(pool.Err("for_one"), IsNil)

	// update with same assertion
	ok, err := pool.Add(s.seq1_1111r5, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)
	c.Check(pool.Err("for_one"), IsNil)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Assert(pool.Err("for_one"), IsNil)

	// sequence point 1, revision 5 is still in the database.
	_, err = s.seq1_1111r5.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)
}

func (s *poolSuite) TestUpdateSeqFormingPinnedNewerSequenceSameRevisionNoop(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""), s.seq1_1111r5)

	pool := asserts.NewPool(s.db, 64)

	atseq := &asserts.AtSequence{
		Type:        s.seq1_1111r5.Type(),
		SequenceKey: []string{"1111"},
		Sequence:    1,
		Revision:    5,
		Pinned:      true,
	}
	err := pool.AddSequenceToUpdate(atseq, "for_one") // group num: 0
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {s.hub.StoreAccountKey(s.dev1Acct.SignKeyID()).At()},
	})
	c.Check(toResolveSeq, DeepEquals, map[asserts.Grouping][]*asserts.AtSequence{
		asserts.MakePoolGrouping(0): {
			&asserts.AtSequence{
				Type:        s.seq1_1111r5.Type(),
				SequenceKey: []string{"1111"},
				Sequence:    1,
				Revision:    5,
				Pinned:      true,
			}},
	})

	c.Check(pool.Err("for_one"), IsNil)

	// resolve
	ok, err := pool.Add(s.seq3_1111r5, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)
	c.Check(pool.Err("for_one"), IsNil)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Assert(pool.Err("for_one"), IsNil)

	// sequence point 1, revision 5 is still the latest.
	_, err = s.seq1_1111r5.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)

	// and sequence point 3 revision 5 wasn't added to asserts database.
	_, err = s.seq3_1111r5.Ref().Resolve(s.db.Find)
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (s *poolSuite) TestUpdateSeqFormingPinnedNewerSequenceNewerRevisionNoop(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""), s.seq1_1111r5)

	pool := asserts.NewPool(s.db, 64)

	atseq := &asserts.AtSequence{
		Type:        s.seq1_1111r5.Type(),
		SequenceKey: []string{"1111"},
		Sequence:    1,
		Revision:    5,
		Pinned:      true,
	}
	err := pool.AddSequenceToUpdate(atseq, "for_one") // group num: 0
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {s.hub.StoreAccountKey(s.dev1Acct.SignKeyID()).At()},
	})
	c.Check(toResolveSeq, DeepEquals, map[asserts.Grouping][]*asserts.AtSequence{
		asserts.MakePoolGrouping(0): {
			&asserts.AtSequence{
				Type:        s.seq1_1111r5.Type(),
				SequenceKey: []string{"1111"},
				Sequence:    1,
				Revision:    5,
				Pinned:      true,
			}},
	})

	c.Check(pool.Err("for_one"), IsNil)

	// resolve
	ok, err := pool.Add(s.seq2_1111r7, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Assert(pool.Err("for_one"), IsNil)

	// sequence point 1, revision 5 is still the latest.
	_, err = s.seq1_1111r5.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)

	// and sequence point 2 revision 7 wasn't added to asserts database.
	_, err = s.seq2_1111r7.Ref().Resolve(s.db.Find)
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (s *poolSuite) TestUpdateSeqFormingPinnedSameSequenceNewerRevision(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""), s.seq1_1111r5)
	pool := asserts.NewPool(s.db, 64)

	atseq := &asserts.AtSequence{
		Type:        s.seq1_1111r5.Type(),
		SequenceKey: []string{"1111"},
		Sequence:    1,
		Revision:    5,
		Pinned:      true,
	}
	err := pool.AddSequenceToUpdate(atseq, "for_one") // group num: 0
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {s.hub.StoreAccountKey(s.dev1Acct.SignKeyID()).At()},
	})
	c.Check(toResolveSeq, DeepEquals, map[asserts.Grouping][]*asserts.AtSequence{
		asserts.MakePoolGrouping(0): {
			&asserts.AtSequence{
				Type:        s.seq1_1111r5.Type(),
				SequenceKey: []string{"1111"},
				Sequence:    1,
				Revision:    5,
				Pinned:      true,
			}},
	})

	c.Check(pool.Err("for_one"), IsNil)

	// resolve
	ok, err := pool.Add(s.seq1_1111r6, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Assert(pool.Err("for_one"), IsNil)

	// sequence point 1, revision 6 is in db.
	_, err = s.seq1_1111r6.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)
}

func (s *poolSuite) TestUpdateSeqFormingUseAssertRevision(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""), s.seq1_1111r5)

	pool := asserts.NewPool(s.db, 64)

	atseq := &asserts.AtSequence{
		Type:        s.seq1_1111r5.Type(),
		SequenceKey: []string{"1111"},
		Sequence:    1,
		Revision:    0, // intentionaly unset
	}
	err := pool.AddSequenceToUpdate(atseq, "for_one") // group num: 0
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {s.hub.StoreAccountKey(s.dev1Acct.SignKeyID()).At()},
	})

	// verify that revision number from the existing assertion to update was used.
	c.Check(toResolveSeq, DeepEquals, map[asserts.Grouping][]*asserts.AtSequence{
		asserts.MakePoolGrouping(0): {
			&asserts.AtSequence{
				Type:        s.seq1_1111r5.Type(),
				SequenceKey: []string{"1111"},
				Sequence:    1,
				Revision:    5,
			}},
	})
}

func (s *poolSuite) TestAddSequenceToUpdateMissingSequenceError(c *C) {
	pool := asserts.NewPool(s.db, 64)
	atseq := &asserts.AtSequence{
		Type:        s.seq1_1111r5.Type(),
		SequenceKey: []string{"1111"},
		Revision:    asserts.RevisionNotKnown,
	}
	err := pool.AddSequenceToUpdate(atseq, "for_one")
	c.Assert(err, ErrorMatches, `internal error: sequence to update must have a sequence number set`)
}

func (s *poolSuite) TestAddUnresolvedSeqUnresolved(c *C) {
	pool := asserts.NewPool(s.db, 64)

	atseq := &asserts.AtSequence{
		Type:        s.seq1_1111r5.Type(),
		SequenceKey: []string{"1111"},
		Revision:    asserts.RevisionNotKnown,
		Sequence:    1,
	}
	err := pool.AddUnresolvedSequence(atseq, "for_one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, DeepEquals, map[asserts.Grouping][]*asserts.AtSequence{
		asserts.MakePoolGrouping(0): {
			&asserts.AtSequence{
				Type:        s.seq1_1111r5.Type(),
				SequenceKey: []string{"1111"},
				Revision:    asserts.RevisionNotKnown,
				Sequence:    1,
			}},
	})

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), Equals, asserts.ErrUnresolved)
}

func (s *poolSuite) TestAddUnresolvedSeqOnce(c *C) {
	pool := asserts.NewPool(s.db, 64)

	atseq := &asserts.AtSequence{
		Type:        s.seq1_1111r5.Type(),
		SequenceKey: []string{"1111"},
		Revision:    asserts.RevisionNotKnown,
		Sequence:    1,
	}
	err := pool.AddUnresolvedSequence(atseq, "for_one")
	c.Assert(err, IsNil)

	atseq.Sequence = 2
	atseq.Revision = 3
	err = pool.AddUnresolvedSequence(atseq, "for_one")
	c.Assert(err, ErrorMatches, `internal error: sequence \[1111\] is already being resolved`)
}

func (s *poolSuite) TestAddSeqToUpdateOnce(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""), s.seq1_1111r5)
	pool := asserts.NewPool(s.db, 64)

	atseq := &asserts.AtSequence{
		Type:        s.seq1_1111r5.Type(),
		SequenceKey: []string{"1111"},
		Revision:    2,
		Sequence:    1,
	}
	err := pool.AddSequenceToUpdate(atseq, "for_one")
	c.Assert(err, IsNil)

	atseq.Sequence = 3
	atseq.Revision = 3
	err = pool.AddSequenceToUpdate(atseq, "for_one")
	c.Assert(err, ErrorMatches, `internal error: sequence \[1111\] is already being resolved`)
}

func (s *poolSuite) TestAddSeqToUpdateNotFound(c *C) {
	pool := asserts.NewPool(s.db, 64)

	atseq := &asserts.AtSequence{
		Type:        s.seq1_1111r5.Type(),
		SequenceKey: []string{"1111"},
		Revision:    2,
		Sequence:    1,
	}
	err := pool.AddSequenceToUpdate(atseq, "for_one")
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

var errBoom = errors.New("boom")

func (s *poolSuite) TestAddErrorEarly(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""))

	pool := asserts.NewPool(s.db, 64)

	storeKey := s.hub.StoreAccountKey("")
	err := pool.AddToUpdate(storeKey.Ref(), "store_key")
	c.Assert(err, IsNil)

	at1111 := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"1111"}},
		Revision: asserts.RevisionNotKnown,
	}
	err = pool.AddUnresolved(at1111, "for_one")
	c.Assert(err, IsNil)

	seq1111 := &asserts.AtSequence{
		Type:        asserts.TestOnlySeqType,
		SequenceKey: []string{"1111"},
		Revision:    asserts.RevisionNotKnown,
	}
	err = pool.AddUnresolvedSequence(seq1111, "for_two")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKey.At()},
		asserts.MakePoolGrouping(1): {at1111},
	})
	c.Check(toResolveSeq, DeepEquals, map[asserts.Grouping][]*asserts.AtSequence{
		asserts.MakePoolGrouping(2): {seq1111},
	})

	err = pool.AddError(errBoom, storeKey.Ref())
	c.Assert(err, IsNil)

	err = pool.AddSequenceError(errBoom, seq1111)
	c.Assert(err, IsNil)

	ok, err := pool.Add(s.rev1_1111, asserts.MakePoolGrouping(1))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("store_key"), Equals, errBoom)
	c.Check(pool.Err("for_one"), Equals, errBoom)
	c.Check(pool.Err("for_two"), Equals, errBoom)
}

func (s *poolSuite) TestAddErrorLater(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""))

	pool := asserts.NewPool(s.db, 64)

	storeKey := s.hub.StoreAccountKey("")
	err := pool.AddToUpdate(storeKey.Ref(), "store_key")
	c.Assert(err, IsNil)

	at1111 := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"1111"}},
		Revision: asserts.RevisionNotKnown,
	}
	err = pool.AddUnresolved(at1111, "for_one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKey.At()},
		asserts.MakePoolGrouping(1): {at1111},
	})
	c.Check(toResolveSeq, HasLen, 0)

	ok, err := pool.Add(s.rev1_1111, asserts.MakePoolGrouping(1))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	err = pool.AddError(errBoom, storeKey.Ref())
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("store_key"), Equals, errBoom)
	c.Check(pool.Err("for_one"), Equals, errBoom)
}

func (s *poolSuite) TestNopUpdatePlusFetchOfPushed(c *C) {
	storeKey := s.hub.StoreAccountKey("")
	assertstest.AddMany(s.db, storeKey)
	assertstest.AddMany(s.db, s.dev1Acct)
	assertstest.AddMany(s.db, s.decl1)
	assertstest.AddMany(s.db, s.rev1_1111)

	pool := asserts.NewPool(s.db, 64)

	atOne := s.decl1.At()
	err := pool.AddToUpdate(&atOne.Ref, "for_one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKey.At(), s.dev1Acct.At(), atOne},
	})
	c.Check(toResolveSeq, HasLen, 0)

	// no updates but
	// new push suggestion

	gSuggestion, err := pool.Singleton("suggestion")
	c.Assert(err, IsNil)

	ok, err := pool.Add(s.rev1_3333, gSuggestion)
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Assert(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("for_one"), IsNil)

	pool.AddGroupingError(errBoom, gSuggestion)

	c.Assert(pool.Err("for_one"), IsNil)
	c.Assert(pool.Err("suggestion"), Equals, errBoom)

	at3333 := s.rev1_3333.At()
	at3333.Revision = asserts.RevisionNotKnown
	err = pool.AddUnresolved(at3333, at3333.Unique())
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Assert(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)

	c.Assert(pool.Err(at3333.Unique()), IsNil)

	a, err := s.rev1_3333.Ref().Resolve(s.db.Find)
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.TestOnlyRev).H(), Equals, "3333")
}

func (s *poolSuite) TestAddToUpdateThenUnresolved(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""))

	pool := asserts.NewPool(s.db, 64)

	storeKey := s.hub.StoreAccountKey("")
	storeKeyAt := storeKey.At()
	storeKeyAt.Revision = asserts.RevisionNotKnown

	err := pool.AddToUpdate(storeKey.Ref(), "for_one")
	c.Assert(err, IsNil)
	err = pool.AddUnresolved(storeKeyAt, "for_one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKey.At()},
	})
	c.Check(toResolveSeq, HasLen, 0)
}

func (s *poolSuite) TestAddUnresolvedThenToUpdate(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""))

	pool := asserts.NewPool(s.db, 64)

	storeKey := s.hub.StoreAccountKey("")
	storeKeyAt := storeKey.At()
	storeKeyAt.Revision = asserts.RevisionNotKnown

	err := pool.AddUnresolved(storeKeyAt, "for_one")
	c.Assert(err, IsNil)
	err = pool.AddToUpdate(storeKey.Ref(), "for_one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKey.At()},
	})
	c.Check(toResolveSeq, HasLen, 0)
}

func (s *poolSuite) TestNopUpdatePlusFetch(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""))

	pool := asserts.NewPool(s.db, 64)

	storeKey := s.hub.StoreAccountKey("")
	err := pool.AddToUpdate(storeKey.Ref(), "store_key")
	c.Assert(err, IsNil)

	at1111 := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"1111"}},
		Revision: asserts.RevisionNotKnown,
	}
	err = pool.AddUnresolved(at1111, "for_one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKey.At()},
		asserts.MakePoolGrouping(1): {at1111},
	})
	c.Check(toResolveSeq, HasLen, 0)

	ok, err := pool.Add(s.rev1_1111, asserts.MakePoolGrouping(1))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	dev1AcctAt := s.dev1Acct.At()
	dev1AcctAt.Revision = asserts.RevisionNotKnown
	decl1At := s.decl1.At()
	decl1At.Revision = asserts.RevisionNotKnown
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(1): {dev1AcctAt, decl1At},
	})
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("store_key"), IsNil)
	c.Check(pool.Err("for_one"), IsNil)
}

func (s *poolSuite) TestParallelPartialResolutionFailure(c *C) {
	pool := asserts.NewPool(s.db, 64)

	atOne := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyDeclType, PrimaryKey: []string{"one"}},
		Revision: asserts.RevisionNotKnown,
	}
	err := pool.AddUnresolved(atOne, "one")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {atOne},
	})
	c.Check(toResolveSeq, HasLen, 0)

	ok, err := pool.Add(s.decl1, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	dev1AcctAt := s.dev1Acct.At()
	dev1AcctAt.Revision = asserts.RevisionNotKnown
	decl1At := s.decl1.At()
	decl1At.Revision = asserts.RevisionNotKnown
	storeKeyAt := s.hub.StoreAccountKey("").At()
	storeKeyAt.Revision = asserts.RevisionNotKnown
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKeyAt, dev1AcctAt},
	})
	c.Check(toResolveSeq, HasLen, 0)

	// failed to get prereqs
	c.Check(pool.AddGroupingError(errBoom, asserts.MakePoolGrouping(0)), IsNil)

	err = pool.AddUnresolved(atOne, "other")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Err("one"), Equals, errBoom)
	c.Check(pool.Err("other"), IsNil)

	// we fail at commit though
	err = pool.CommitTo(s.db)
	c.Check(err, IsNil)
	c.Check(pool.Err("one"), Equals, errBoom)
	c.Check(pool.Err("other"), ErrorMatches, "cannot resolve prerequisite assertion.*")
}

func (s *poolSuite) TestAddErrors(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""))

	pool := asserts.NewPool(s.db, 64)

	storeKey := s.hub.StoreAccountKey("")
	err := pool.AddToUpdate(storeKey.Ref(), "store_key")
	c.Assert(err, IsNil)

	at1111 := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"1111"}},
		Revision: asserts.RevisionNotKnown,
	}
	err = pool.AddUnresolved(at1111, "for_one")
	c.Assert(err, IsNil)

	seq1111 := &asserts.AtSequence{
		Type:        asserts.TestOnlySeqType,
		SequenceKey: []string{"1111"},
		Revision:    asserts.RevisionNotKnown,
	}
	err = pool.AddUnresolvedSequence(seq1111, "for_two")
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 2)
	c.Check(toResolveSeq, HasLen, 1)

	err = pool.AddError(errBoom, storeKey.Ref())
	c.Assert(err, IsNil)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	c.Check(pool.Errors(), DeepEquals, map[string]error{
		"store_key": errBoom,
		"for_one":   asserts.ErrUnresolved,
		"for_two":   asserts.ErrUnresolved,
	})
}

func (s *poolSuite) TestPoolReuseWithClearGroupsAndUnchanged(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""))
	assertstest.AddMany(s.db, s.dev1Acct, s.decl1)
	assertstest.AddMany(s.db, s.dev2Acct, s.decl2)

	pool := asserts.NewPool(s.db, 64)

	err := pool.AddToUpdate(s.decl1.Ref(), "for_one") // group num: 0
	c.Assert(err, IsNil)

	storeKeyAt := s.hub.StoreAccountKey("").At()

	toResolve, toResolveSeq, err := pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {storeKeyAt, s.dev1Acct.At(), s.decl1.At()},
	})
	c.Check(toResolveSeq, HasLen, 0)

	ok, err := pool.Add(s.decl1_1, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	c.Check(toResolve, HasLen, 0)
	c.Check(toResolveSeq, HasLen, 0)

	// clear the groups as we would do for real reuse when we have
	// exhausted allowed groups
	err = pool.ClearGroups()
	c.Assert(err, IsNil)

	err = pool.AddToUpdate(s.decl2.Ref(), "for_two") // group num: 0 again
	c.Assert(err, IsNil)

	// no reference to store key because it is remebered as unchanged
	// across the clearing
	toResolve, toResolveSeq, err = pool.ToResolve()
	c.Assert(err, IsNil)
	sortToResolve(toResolve)
	c.Check(toResolve, DeepEquals, map[asserts.Grouping][]*asserts.AtRevision{
		asserts.MakePoolGrouping(0): {s.dev2Acct.At(), s.decl2.At()},
	})
	c.Check(toResolveSeq, HasLen, 0)
}

func (s *poolSuite) TestBackstore(c *C) {
	assertstest.AddMany(s.db, s.hub.StoreAccountKey(""), s.dev1Acct)
	pool := asserts.NewPool(s.db, 64)

	at1111 := &asserts.AtRevision{
		Ref:      asserts.Ref{Type: asserts.TestOnlyRevType, PrimaryKey: []string{"1111"}},
		Revision: asserts.RevisionNotKnown,
	}
	c.Assert(pool.AddUnresolved(at1111, "for_one"), IsNil)
	res, _, err := pool.ToResolve()
	c.Assert(err, IsNil)
	c.Assert(res, HasLen, 1)

	// resolve (but do not commit)
	ok, err := pool.Add(s.rev1_1111, asserts.MakePoolGrouping(0))
	c.Assert(err, IsNil)
	c.Assert(ok, Equals, true)

	// the assertion should be available via pool's backstore
	bs := pool.Backstore()
	c.Assert(bs, NotNil)
	a, err := bs.Get(s.rev1_1111.Type(), s.rev1_1111.At().PrimaryKey, s.rev1_1111.Type().MaxSupportedFormat())
	c.Assert(err, IsNil)
	c.Assert(a, NotNil)
}
