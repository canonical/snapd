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

	"github.com/ddkwork/golibrary/mylog"
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
	mylog.Check(s.storeSigning.Add(s.dev1Acct))


	db := mylog.Check2(asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	}))

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
	decl := mylog.Check2(s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, ""))

	mylog.Check(s.storeSigning.Add(decl))

	return decl.(*asserts.SnapDeclaration)
}

func (s *batchSuite) TestAddStream(c *C) {
	b := &bytes.Buffer{}
	enc := asserts.NewEncoder(b)
	mylog.
		// wrong order is ok
		Check(enc.Encode(s.dev1Acct))

	enc.Encode(s.storeSigning.StoreAccountKey(""))


	batch := asserts.NewBatch(nil)
	refs := mylog.Check2(batch.AddStream(b))

	c.Check(refs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{s.dev1Acct.AccountID()}},
		{Type: asserts.AccountKeyType, PrimaryKey: []string{s.storeSigning.StoreAccountKey("").PublicKeyID()}},
	})
	mylog.

		// noop
		Check(batch.Add(s.storeSigning.StoreAccountKey("")))

	mylog.Check(batch.CommitTo(s.db, nil))


	devAcct := mylog.Check2(s.db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	}))

	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")
}

func (s *batchSuite) TestCommitToAndObserve(c *C) {
	b := &bytes.Buffer{}
	enc := asserts.NewEncoder(b)
	mylog.
		// wrong order is ok
		Check(enc.Encode(s.dev1Acct))

	enc.Encode(s.storeSigning.StoreAccountKey(""))


	batch := asserts.NewBatch(nil)
	refs := mylog.Check2(batch.AddStream(b))

	c.Check(refs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{s.dev1Acct.AccountID()}},
		{Type: asserts.AccountKeyType, PrimaryKey: []string{s.storeSigning.StoreAccountKey("").PublicKeyID()}},
	})
	mylog.

		// noop
		Check(batch.Add(s.storeSigning.StoreAccountKey("")))


	var seen []*asserts.Ref
	obs := func(verified asserts.Assertion) {
		seen = append(seen, verified.Ref())
	}
	mylog.Check(batch.CommitToAndObserve(s.db, obs, nil))


	devAcct := mylog.Check2(s.db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	}))

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
	refs := mylog.Check2(batch.AddStream(b))

	c.Check(refs, HasLen, 0)
}

func (s *batchSuite) TestConsiderPreexisting(c *C) {
	mylog.
		// prereq store key
		Check(s.db.Add(s.storeSigning.StoreAccountKey("")))


	batch := asserts.NewBatch(nil)
	mylog.Check(batch.Add(s.dev1Acct))

	mylog.Check(batch.CommitTo(s.db, nil))


	devAcct := mylog.Check2(s.db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	}))

	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")
}

func (s *batchSuite) TestAddStreamReturnsEffectivelyAddedRefs(c *C) {
	batch := asserts.NewBatch(nil)
	mylog.Check(batch.Add(s.storeSigning.StoreAccountKey("")))


	b := &bytes.Buffer{}
	enc := asserts.NewEncoder(b)
	mylog.
		// wrong order is ok
		Check(enc.Encode(s.dev1Acct))

	// this was already added to the batch
	enc.Encode(s.storeSigning.StoreAccountKey(""))


	// effectively adds only the developer1 account
	refs := mylog.Check2(batch.AddStream(b))

	c.Check(refs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{s.dev1Acct.AccountID()}},
	})
	mylog.Check(batch.CommitTo(s.db, nil))


	devAcct := mylog.Check2(s.db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	}))

	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")
}

func (s *batchSuite) TestCommitRefusesSelfSignedKey(c *C) {
	aKey, _ := assertstest.GenerateKey(752)
	aSignDB := assertstest.NewSigningDB("can0nical", aKey)

	aKeyEncoded := mylog.Check2(asserts.EncodePublicKey(aKey.PublicKey()))


	headers := map[string]interface{}{
		"authority-id":        "can0nical",
		"account-id":          "can0nical",
		"public-key-sha3-384": aKey.PublicKey().ID(),
		"name":                "default",
		"since":               time.Now().UTC().Format(time.RFC3339),
	}
	acctKey := mylog.Check2(aSignDB.Sign(asserts.AccountKeyType, headers, aKeyEncoded, ""))


	headers = map[string]interface{}{
		"authority-id": "can0nical",
		"brand-id":     "can0nical",
		"repair-id":    "2",
		"summary":      "repair two",
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}
	repair := mylog.Check2(aSignDB.Sign(asserts.RepairType, headers, []byte("#script"), ""))


	batch := asserts.NewBatch(nil)
	mylog.Check(batch.Add(repair))

	mylog.Check(batch.Add(acctKey))

	mylog.

		// this must fail
		Check(batch.CommitTo(s.db, nil))
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

		a = mylog.Check2(s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, ""))

	})()
	mylog.Check(batch.Add(a))
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

		a = mylog.Check2(s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, ""))

	})()
	mylog.Check(batch.Add(a))
	c.Check(err, IsNil)
	c.Check(uRef, DeepEquals, &asserts.Ref{
		Type:       asserts.SnapDeclarationType,
		PrimaryKey: []string{"16", "snap-id-1"},
	})
}

func (s *batchSuite) TestCommitPartial(c *C) {
	mylog.
		// Commit does add any successful assertion until the first error
		Check(

			// store key already present
			s.db.Add(s.storeSigning.StoreAccountKey("")))


	batch := asserts.NewBatch(nil)

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	mylog.Check(batch.Add(snapDeclFoo))

	mylog.Check(batch.Add(s.dev1Acct))


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
	snapRev := mylog.Check2(s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, ""))

	mylog.Check(batch.Add(snapRev))

	mylog.Check(batch.CommitTo(s.db, &asserts.CommitOptions{Precheck: false}))
	c.Check(err, ErrorMatches, `(?ms).*validity.*`)

	// snap-declaration was added anyway
	_ = mylog.Check2(s.db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	}))

}

func (s *batchSuite) TestCommitMissing(c *C) {
	mylog.
		// store key already present
		Check(s.db.Add(s.storeSigning.StoreAccountKey("")))


	batch := asserts.NewBatch(nil)

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	mylog.Check(batch.Add(snapDeclFoo))

	mylog.Check(batch.CommitTo(s.db, nil))
	c.Check(err, ErrorMatches, `cannot resolve prerequisite assertion: account.*`)
}

func (s *batchSuite) TestPrecheckPartial(c *C) {
	mylog.
		// store key already present
		Check(s.db.Add(s.storeSigning.StoreAccountKey("")))


	batch := asserts.NewBatch(nil)

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	mylog.Check(batch.Add(snapDeclFoo))

	mylog.Check(batch.Add(s.dev1Acct))


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
	snapRev := mylog.Check2(s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, ""))

	mylog.Check(batch.Add(snapRev))

	mylog.Check(batch.CommitTo(s.db, &asserts.CommitOptions{Precheck: true}))
	c.Check(err, ErrorMatches, `(?ms).*validity.*`)

	// nothing was added
	_ = mylog.Check2(s.db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	}))
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (s *batchSuite) TestPrecheckHappy(c *C) {
	mylog.
		// store key already present
		Check(s.db.Add(s.storeSigning.StoreAccountKey("")))


	batch := asserts.NewBatch(nil)

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	mylog.Check(batch.Add(snapDeclFoo))

	mylog.Check(batch.Add(s.dev1Acct))


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
	snapRev := mylog.Check2(s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, ""))

	mylog.Check(batch.Add(snapRev))

	mylog.

		// test precheck on its own
		Check(batch.DoPrecheck(s.db))


	// nothing was added yet
	_ = mylog.Check2(s.db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	}))
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
	mylog.

		// commit (with precheck)
		Check(batch.CommitTo(s.db, &asserts.CommitOptions{Precheck: true}))


	_ = mylog.Check2(s.db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": revDigest,
	}))
	c.Check(err, IsNil)
}

func (s *batchSuite) TestFetch(c *C) {
	mylog.Check(s.db.Add(s.storeSigning.StoreAccountKey("")))


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
	snapRev := mylog.Check2(s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, ""))

	mylog.Check(s.storeSigning.Add(snapRev))

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
	mylog.Check(batch.Fetch(s.db, retrieve, fetching))


	// nothing was added yet
	_ = mylog.Check2(s.db.Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	}))
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
	mylog.

		// commit
		Check(batch.CommitTo(s.db, nil))


	_ = mylog.Check2(s.db.Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": revDigest,
	}))
	c.Check(err, IsNil)
}
