// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2021 Canonical Ltd
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
	"bytes"
	"errors"
	"fmt"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

type batchSuite struct {
	storeSigning *assertstest.StoreStack
	dev1Acct     *asserts.Account

	db *asserts.Database
}

var _ = Suite(&batchSuite{})

func (s *batchSuite) SetUpTest(c *C) {
	s.storeSigning = assertstest.NewStoreStack("can0nical", nil)

	s.dev1Acct = assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	err := s.storeSigning.Add(s.dev1Acct)
	c.Assert(err, IsNil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)
	s.db = db
}

func (s *batchSuite) snapDecl(c *C, name string, extraHeaders map[string]interface{}) *asserts.SnapDeclaration {
	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      name + "-id",
		"snap-name":    name,
		"publisher-id": s.dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	for h, v := range extraHeaders {
		headers[h] = v
	}
	decl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(decl)
	c.Assert(err, IsNil)
	return decl.(*asserts.SnapDeclaration)
}

func (s *batchSuite) TestAddStream(c *C) {
	b := &bytes.Buffer{}
	enc := asserts.NewEncoder(b)
	// wrong order is ok
	err := enc.Encode(s.dev1Acct)
	c.Assert(err, IsNil)
	enc.Encode(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	batch := asserts.NewBatch(nil)
	refs, err := batch.AddStream(b)
	c.Assert(err, IsNil)
	c.Check(refs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{s.dev1Acct.AccountID()}},
		{Type: asserts.AccountKeyType, PrimaryKey: []string{s.storeSigning.StoreAccountKey("").PublicKeyID()}},
	})

	// noop
	err = batch.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	err = batch.CommitTo(s.db, nil)
	c.Assert(err, IsNil)

	devAcct, err := s.db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	})
	c.Assert(err, IsNil)
	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")
}

func (s *batchSuite) TestCommitToAndObserve(c *C) {
	b := &bytes.Buffer{}
	enc := asserts.NewEncoder(b)
	// wrong order is ok
	err := enc.Encode(s.dev1Acct)
	c.Assert(err, IsNil)
	enc.Encode(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	batch := asserts.NewBatch(nil)
	refs, err := batch.AddStream(b)
	c.Assert(err, IsNil)
	c.Check(refs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{s.dev1Acct.AccountID()}},
		{Type: asserts.AccountKeyType, PrimaryKey: []string{s.storeSigning.StoreAccountKey("").PublicKeyID()}},
	})

	// noop
	err = batch.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	var seen []*asserts.Ref
	obs := func(verified asserts.Assertion) {
		seen = append(seen, verified.Ref())
	}
	err = batch.CommitToAndObserve(s.db, obs, nil)
	c.Assert(err, IsNil)

	devAcct, err := s.db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	})
	c.Assert(err, IsNil)
	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")

	// this is the order they needed to be added
	c.Check(seen, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountKeyType, PrimaryKey: []string{s.storeSigning.StoreAccountKey("").PublicKeyID()}},
		{Type: asserts.AccountType, PrimaryKey: []string{s.dev1Acct.AccountID()}},
	})
}

func (s *batchSuite) TestAddEmptyStream(c *C) {
	b := &bytes.Buffer{}

	batch := asserts.NewBatch(nil)
	refs, err := batch.AddStream(b)
	c.Assert(err, IsNil)
	c.Check(refs, HasLen, 0)
}

func (s *batchSuite) TestConsiderPreexisting(c *C) {
	// prereq store key
	err := s.db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	batch := asserts.NewBatch(nil)
	err = batch.Add(s.dev1Acct)
	c.Assert(err, IsNil)

	err = batch.CommitTo(s.db, nil)
	c.Assert(err, IsNil)

	devAcct, err := s.db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	})
	c.Assert(err, IsNil)
	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")
}

func (s *batchSuite) TestAddStreamReturnsEffectivelyAddedRefs(c *C) {
	batch := asserts.NewBatch(nil)

	err := batch.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	b := &bytes.Buffer{}
	enc := asserts.NewEncoder(b)
	// wrong order is ok
	err = enc.Encode(s.dev1Acct)
	c.Assert(err, IsNil)
	// this was already added to the batch
	enc.Encode(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	// effectively adds only the developer1 account
	refs, err := batch.AddStream(b)
	c.Assert(err, IsNil)
	c.Check(refs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{s.dev1Acct.AccountID()}},
	})

	err = batch.CommitTo(s.db, nil)
	c.Assert(err, IsNil)

	devAcct, err := s.db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	})
	c.Assert(err, IsNil)
	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")
}

func (s *batchSuite) TestCommitRefusesSelfSignedKey(c *C) {
	aKey, _ := assertstest.GenerateKey(752)
	aSignDB := assertstest.NewSigningDB("can0nical", aKey)

	aKeyEncoded, err := asserts.EncodePublicKey(aKey.PublicKey())
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"authority-id":        "can0nical",
		"account-id":          "can0nical",
		"public-key-sha3-384": aKey.PublicKey().ID(),
		"name":                "default",
		"since":               time.Now().UTC().Format(time.RFC3339),
	}
	acctKey, err := aSignDB.Sign(asserts.AccountKeyType, headers, aKeyEncoded, "")
	c.Assert(err, IsNil)

	headers = map[string]interface{}{
		"authority-id": "can0nical",
		"brand-id":     "can0nical",
		"repair-id":    "2",
		"summary":      "repair two",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}
	repair, err := aSignDB.Sign(asserts.RepairType, headers, []byte("#script"), "")
	c.Assert(err, IsNil)

	batch := asserts.NewBatch(nil)

	err = batch.Add(repair)
	c.Assert(err, IsNil)

	err = batch.Add(acctKey)
	c.Assert(err, IsNil)

	// this must fail
	err = batch.CommitTo(s.db, nil)
	c.Assert(err, ErrorMatches, `circular assertions are not expected:.*`)
}

func (s *batchSuite) TestAddUnsupported(c *C) {
	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 111)
	defer restore()

	batch := asserts.NewBatch(nil)

	var a asserts.Assertion
	(func() {
		restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 999)
		defer restore()
		headers := map[string]interface{}{
			"format":       "999",
			"revision":     "1",
			"series":       "16",
			"snap-id":      "snap-id-1",
			"snap-name":    "foo",
			"publisher-id": s.dev1Acct.AccountID(),
			"timestamp":    time.Now().Format(time.RFC3339),
		}
		var err error
		a, err = s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
		c.Assert(err, IsNil)
	})()

	err := batch.Add(a)
	c.Check(err, ErrorMatches, `proposed "snap-declaration" assertion has format 999 but 111 is latest supported`)
}

func (s *batchSuite) TestAddUnsupportedIgnore(c *C) {
	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 111)
	defer restore()

	var uRef *asserts.Ref
	unsupported := func(ref *asserts.Ref, _ error) error {
		uRef = ref
		return nil
	}

	batch := asserts.NewBatch(unsupported)

	var a asserts.Assertion
	(func() {
		restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 999)
		defer restore()
		headers := map[string]interface{}{
			"format":       "999",
			"revision":     "1",
			"series":       "16",
			"snap-id":      "snap-id-1",
			"snap-name":    "foo",
			"publisher-id": s.dev1Acct.AccountID(),
			"timestamp":    time.Now().Format(time.RFC3339),
		}
		var err error
		a, err = s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
		c.Assert(err, IsNil)
	})()

	err := batch.Add(a)
	c.Check(err, IsNil)
	c.Check(uRef, DeepEquals, &asserts.Ref{
		Type:       asserts.SnapDeclarationType,
		PrimaryKey: []string{"16", "snap-id-1"},
	})
}

func (s *batchSuite) TestCommitPartial(c *C) {
	// Commit does add any successful assertion until the first error

	// store key already present
	err := s.db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	batch := asserts.NewBatch(nil)

	snapDeclFoo := s.snapDecl(c, "foo", nil)

	err = batch.Add(snapDeclFoo)
	c.Assert(err, IsNil)
	err = batch.Add(s.dev1Acct)
	c.Assert(err, IsNil)

	// too old
	rev := 1
	headers := map[string]interface{}{
		"snap-id":       "foo-id",
		"snap-sha3-384": makeDigest(rev),
		"snap-size":     fmt.Sprintf("%d", len(fakeSnap(rev))),
		"snap-revision": fmt.Sprintf("%d", rev),
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Time{}.Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = batch.Add(snapRev)
	c.Assert(err, IsNil)

	err = batch.CommitTo(s.db, &asserts.CommitOptions{Precheck: false})
	c.Check(err, ErrorMatches, `(?ms).*validity.*`)

	// snap-declaration was added anyway
	_, err = s.db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(err, IsNil)
}

func (s *batchSuite) TestCommitMissing(c *C) {
	// store key already present
	err := s.db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	batch := asserts.NewBatch(nil)

	snapDeclFoo := s.snapDecl(c, "foo", nil)

	err = batch.Add(snapDeclFoo)
	c.Assert(err, IsNil)

	err = batch.CommitTo(s.db, nil)
	c.Check(err, ErrorMatches, `cannot resolve prerequisite assertion: account.*`)
}

func (s *batchSuite) TestPrecheckPartial(c *C) {
	// store key already present
	err := s.db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	batch := asserts.NewBatch(nil)

	snapDeclFoo := s.snapDecl(c, "foo", nil)

	err = batch.Add(snapDeclFoo)
	c.Assert(err, IsNil)
	err = batch.Add(s.dev1Acct)
	c.Assert(err, IsNil)

	// too old
	rev := 1
	headers := map[string]interface{}{
		"snap-id":       "foo-id",
		"snap-sha3-384": makeDigest(rev),
		"snap-size":     fmt.Sprintf("%d", len(fakeSnap(rev))),
		"snap-revision": fmt.Sprintf("%d", rev),
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Time{}.Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = batch.Add(snapRev)
	c.Assert(err, IsNil)

	err = batch.CommitTo(s.db, &asserts.CommitOptions{Precheck: true})
	c.Check(err, ErrorMatches, `(?ms).*validity.*`)

	// nothing was added
	_, err = s.db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (s *batchSuite) TestPrecheckHappy(c *C) {
	// store key already present
	err := s.db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	batch := asserts.NewBatch(nil)

	snapDeclFoo := s.snapDecl(c, "foo", nil)

	err = batch.Add(snapDeclFoo)
	c.Assert(err, IsNil)
	err = batch.Add(s.dev1Acct)
	c.Assert(err, IsNil)

	rev := 1
	revDigest := makeDigest(rev)
	headers := map[string]interface{}{
		"snap-id":       "foo-id",
		"snap-sha3-384": revDigest,
		"snap-size":     fmt.Sprintf("%d", len(fakeSnap(rev))),
		"snap-revision": fmt.Sprintf("%d", rev),
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = batch.Add(snapRev)
	c.Assert(err, IsNil)

	// test precheck on its own
	err = batch.DoPrecheck(s.db)
	c.Assert(err, IsNil)

	// nothing was added yet
	_, err = s.db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	// commit (with precheck)
	err = batch.CommitTo(s.db, &asserts.CommitOptions{Precheck: true})
	c.Assert(err, IsNil)

	_, err = s.db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": revDigest,
	})
	c.Check(err, IsNil)
}

func (s *batchSuite) TestFetch(c *C) {
	err := s.db.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	s.snapDecl(c, "foo", nil)

	rev := 10
	revDigest := makeDigest(rev)
	headers := map[string]interface{}{
		"snap-id":       "foo-id",
		"snap-sha3-384": revDigest,
		"snap-size":     fmt.Sprintf("%d", len(fakeSnap(rev))),
		"snap-revision": fmt.Sprintf("%d", rev),
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = s.storeSigning.Add(snapRev)
	c.Assert(err, IsNil)
	ref := snapRev.Ref()

	batch := asserts.NewBatch(nil)

	// retrieve from storeSigning
	retrieve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		return ref.Resolve(s.storeSigning.Find)
	}
	// fetching the snap-revision
	fetching := func(f asserts.Fetcher) error {
		return f.Fetch(ref)
	}

	err = batch.Fetch(s.db, retrieve, fetching)
	c.Assert(err, IsNil)

	// nothing was added yet
	_, err = s.db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	// commit
	err = batch.CommitTo(s.db, nil)
	c.Assert(err, IsNil)

	_, err = s.db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": revDigest,
	})
	c.Check(err, IsNil)
}
