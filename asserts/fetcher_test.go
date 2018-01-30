// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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
