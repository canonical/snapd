// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2022 Canonical Ltd
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
	"crypto"
	"fmt"
	"time"

	"golang.org/x/crypto/sha3"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/release"
)

type fetcherSuite struct {
	storeSigning *assertstest.StoreStack
}

var _ = Suite(&fetcherSuite{})

func (s *fetcherSuite) SetUpTest(c *C) {
	s.storeSigning = assertstest.NewStoreStack("can0nical", nil)
}

func fakeSnap(rev int) []byte {
	fake := fmt.Sprintf("hsqs________________%d", rev)
	return []byte(fake)
}

func fakeHash(rev int) []byte {
	h := sha3.Sum384(fakeSnap(rev))
	return h[:]
}

func makeDigest(rev int) string {
	d, err := asserts.EncodeDigest(crypto.SHA3_384, fakeHash(rev))
	if err != nil {
		panic(err)
	}
	return string(d)
}

func (s *fetcherSuite) prereqSnapAssertions(c *C, revisions ...int) {
	dev1Acct := assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	err := s.storeSigning.Add(dev1Acct)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapDecl)
	c.Assert(err, IsNil)

	for _, rev := range revisions {
		headers = map[string]interface{}{
			"series":        "16",
			"snap-id":       "snap-id-1",
			"snap-sha3-384": makeDigest(rev),
			"snap-size":     "1000",
			"snap-revision": fmt.Sprintf("%d", rev),
			"developer-id":  dev1Acct.AccountID(),
			"timestamp":     time.Now().Format(time.RFC3339),
		}
		snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
		c.Assert(err, IsNil)
		err = s.storeSigning.Add(snapRev)
		c.Assert(err, IsNil)
	}
}

func (s *fetcherSuite) TestFetch(c *C) {
	s.prereqSnapAssertions(c, 10)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)

	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{makeDigest(10)},
	}

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(s.storeSigning.Find)
	}

	f := asserts.NewFetcher(db, retrieve, db.Add)

	err = f.Fetch(ref)
	c.Assert(err, IsNil)

	snapRev, err := ref.Resolve(db.Find)
	c.Assert(err, IsNil)
	c.Check(snapRev.(*asserts.SnapRevision).SnapRevision(), Equals, 10)

	snapDecl, err := db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "snap-id-1",
	})
	c.Assert(err, IsNil)
	c.Check(snapDecl.(*asserts.SnapDeclaration).SnapName(), Equals, "foo")
}

func (s *fetcherSuite) TestFetchCircularReference(c *C) {
	s.prereqSnapAssertions(c, 10)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)

	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{makeDigest(10)},
	}

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(s.storeSigning.Find)
	}

	f := asserts.NewFetcher(db, retrieve, db.Add)

	// Mock that we refer to ourself
	r := asserts.MockAssertionPrereqs(func(a asserts.Assertion) []*asserts.Ref {
		return []*asserts.Ref{ref}
	})
	defer r()

	err = f.Fetch(ref)
	c.Assert(err, ErrorMatches, `circular assertions are not expected: snap-revision \(tzGsQxT_xJGzbnJ_-25Bbj_8lBHY39c5uUuQWgDTGxAEd0NALdxVaSAD59Pou_Ko;\)`)
}

func (s *fetcherSuite) TestSave(c *C) {
	s.prereqSnapAssertions(c, 10)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(s.storeSigning.Find)
	}

	f := asserts.NewFetcher(db, retrieve, db.Add)

	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{makeDigest(10)},
	}
	rev, err := ref.Resolve(s.storeSigning.Find)
	c.Assert(err, IsNil)

	err = f.Save(rev)
	c.Assert(err, IsNil)

	snapRev, err := ref.Resolve(db.Find)
	c.Assert(err, IsNil)
	c.Check(snapRev.(*asserts.SnapRevision).SnapRevision(), Equals, 10)

	snapDecl, err := db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "snap-id-1",
	})
	c.Assert(err, IsNil)
	c.Check(snapDecl.(*asserts.SnapDeclaration).SnapName(), Equals, "foo")
}

func (s *fetcherSuite) prereqValidationSetAssertion(c *C) {
	vs, err := s.storeSigning.Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "can0nical",
		"series":       "16",
		"account-id":   "can0nical",
		"name":         "base-set",
		"sequence":     "2",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       "123456ididididididididididididid",
				"presence": "required",
				"revision": "7",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(vs)
	c.Check(err, IsNil)
}

func (s *fetcherSuite) TestFetchSequence(c *C) {
	s.prereqValidationSetAssertion(c)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)

	seq := &asserts.AtSequence{
		Type:        asserts.ValidationSetType,
		SequenceKey: []string{release.Series, "can0nical", "base-set"},
		Sequence:    2,
		Revision:    asserts.RevisionNotKnown,
	}
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(s.storeSigning.Find)
	}
	retrieveSeq := func(seq *asserts.AtSequence) (asserts.Assertion, error) {
		return seq.Resolve(s.storeSigning.Find)
	}

	f := asserts.NewSequenceFormingFetcher(db, retrieve, retrieveSeq, db.Add)

	// Fetch the sequence, this will fetch the validation-set with sequence
	// 2. After that we should be able to find the validation-set (sequence 2)
	// in the DB.
	err = f.FetchSequence(seq)
	c.Assert(err, IsNil)

	// Calling resolve works when we provide the correct sequence number. This
	// will then find the assertion we just fetched
	vsa, err := seq.Resolve(db.Find)
	c.Assert(err, IsNil)
	c.Check(vsa.(*asserts.ValidationSet).Name(), Equals, "base-set")
	c.Check(vsa.(*asserts.ValidationSet).Sequence(), Equals, 2)

	// Calling resolve doesn't find the assertion when another sequence number
	// is provided.
	seq.Sequence = 4
	_, err = seq.Resolve(db.Find)
	c.Assert(err, ErrorMatches, `validation-set \(4; series:16 account-id:can0nical name:base-set\) not found`)
}

func (s *fetcherSuite) TestFetchSequenceCircularReference(c *C) {
	s.prereqValidationSetAssertion(c)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)

	seq := &asserts.AtSequence{
		Type:        asserts.ValidationSetType,
		SequenceKey: []string{release.Series, "can0nical", "base-set"},
		Sequence:    2,
		Revision:    asserts.RevisionNotKnown,
	}
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(s.storeSigning.Find)
	}
	retrieveSeq := func(seq *asserts.AtSequence) (asserts.Assertion, error) {
		return seq.Resolve(s.storeSigning.Find)
	}

	f := asserts.NewSequenceFormingFetcher(db, retrieve, retrieveSeq, db.Add)

	// Mock that we refer to ourself
	r := asserts.MockAssertionPrereqs(func(a asserts.Assertion) []*asserts.Ref {
		return []*asserts.Ref{
			{
				Type:       asserts.ValidationSetType,
				PrimaryKey: []string{release.Series, "can0nical", "base-set", "2"},
			},
		}
	})
	defer r()

	err = f.FetchSequence(seq)
	c.Assert(err, ErrorMatches, `circular assertions are not expected: validation-set \(2; series:16 account-id:can0nical name:base-set\)`)
}

func (s *fetcherSuite) TestFetchSequenceMultipleSequencesNotSupported(c *C) {
	s.prereqValidationSetAssertion(c)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)

	seq := &asserts.AtSequence{
		Type:        asserts.ValidationSetType,
		SequenceKey: []string{release.Series, "can0nical", "base-set"},
		Sequence:    2,
		Revision:    asserts.RevisionNotKnown,
	}
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(s.storeSigning.Find)
	}
	retrieveSeq := func(seq *asserts.AtSequence) (asserts.Assertion, error) {
		return seq.Resolve(s.storeSigning.Find)
	}

	f := asserts.NewSequenceFormingFetcher(db, retrieve, retrieveSeq, db.Add)
	err = f.FetchSequence(seq)
	c.Assert(err, IsNil)

	// Fetch same validation-set, but with a different sequence. Currently the
	// AtSequence.Unique() does not include the sequence number or revision, meaning
	// that the first sequence we fetch is the one that will be put into the DB.
	// XXX: This test is here to document the behavior. If we want it to spit an error
	//      or support multiple sequences of an assertion, then changes are required.
	seq.Sequence = 4
	err = f.FetchSequence(seq)
	c.Assert(err, IsNil)

	// We fetch 2 first, it should exist.
	seq.Sequence = 2
	vsa, err := seq.Resolve(db.Find)
	c.Assert(err, IsNil)
	c.Check(vsa.(*asserts.ValidationSet).Name(), Equals, "base-set")
	c.Check(vsa.(*asserts.ValidationSet).Sequence(), Equals, 2)

	// 4 will not exist, as 2 already was present.
	seq.Sequence = 4
	_, err = seq.Resolve(db.Find)
	c.Assert(err, ErrorMatches, `validation-set \(4; series:16 account-id:can0nical name:base-set\) not found`)
}

func (s *fetcherSuite) TestFetcherNotCreatedUsingNewSequenceFormingFetcher(c *C) {
	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)

	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(s.storeSigning.Find)
	}

	f := asserts.NewFetcher(db, retrieve, db.Add)
	c.Assert(f, NotNil)

	// Cast the fetcher to a SequenceFormingFetcher, which should succeed
	// since the fetcher actually implements FetchSequence.
	ff := f.(asserts.SequenceFormingFetcher)
	c.Assert(ff, NotNil)

	// Make sure this produces an error and not a crash
	err = f.(asserts.SequenceFormingFetcher).FetchSequence(nil)
	c.Check(err, ErrorMatches, `cannot fetch assertion sequence point, fetcher must be created using NewSequenceFormingFetcher`)
}
