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

package assertstate_test

import (
	"bytes"
	"crypto"
	"fmt"
	"io/ioutil"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/sha3"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store/storetest"
)

func TestAssertManager(t *testing.T) { TestingT(t) }

type assertMgrSuite struct {
	o     *overlord.Overlord
	state *state.State
	se    *overlord.StateEngine
	mgr   *assertstate.AssertManager

	storeSigning *assertstest.StoreStack
	dev1Acct     *asserts.Account
	dev1Signing  *assertstest.SigningDB

	restore func()
}

var _ = Suite(&assertMgrSuite{})

type fakeStore struct {
	storetest.Store
	state *state.State
	db    asserts.RODatabase
}

func (sto *fakeStore) pokeStateLock() {
	// the store should be called without the state lock held. Try
	// to acquire it.
	sto.state.Lock()
	sto.state.Unlock()
}

func (sto *fakeStore) Assertion(assertType *asserts.AssertionType, key []string, _ *auth.UserState) (asserts.Assertion, error) {
	sto.pokeStateLock()
	ref := &asserts.Ref{Type: assertType, PrimaryKey: key}
	return ref.Resolve(sto.db.Find)
}

func (s *assertMgrSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	s.storeSigning = assertstest.NewStoreStack("can0nical", nil)
	s.restore = sysdb.InjectTrusted(s.storeSigning.Trusted)

	dev1PrivKey, _ := assertstest.GenerateKey(752)
	s.dev1Acct = assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	err := s.storeSigning.Add(s.dev1Acct)
	c.Assert(err, IsNil)

	// developer signing
	dev1AcctKey := assertstest.NewAccountKey(s.storeSigning, s.dev1Acct, nil, dev1PrivKey.PublicKey(), "")
	err = s.storeSigning.Add(dev1AcctKey)
	c.Assert(err, IsNil)

	s.dev1Signing = assertstest.NewSigningDB(s.dev1Acct.AccountID(), dev1PrivKey)

	s.o = overlord.Mock()
	s.state = s.o.State()
	s.se = s.o.StateEngine()
	mgr, err := assertstate.Manager(s.state, s.o.TaskRunner())
	c.Assert(err, IsNil)
	s.mgr = mgr
	s.o.AddManager(s.mgr)

	s.o.AddManager(s.o.TaskRunner())

	s.state.Lock()
	snapstate.ReplaceStore(s.state, &fakeStore{
		state: s.state,
		db:    s.storeSigning,
	})
	s.state.Unlock()
}

func (s *assertMgrSuite) TearDownTest(c *C) {
	s.restore()
}

func (s *assertMgrSuite) TestDB(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	db := assertstate.DB(s.state)
	c.Check(db, FitsTypeOf, (*asserts.Database)(nil))
}

func (s *assertMgrSuite) TestAdd(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// prereq store key
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)

	db := assertstate.DB(s.state)
	devAcct, err := db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	})
	c.Assert(err, IsNil)
	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")
}

func (s *assertMgrSuite) TestBatchAddStream(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	b := &bytes.Buffer{}
	enc := asserts.NewEncoder(b)
	// wrong order is ok
	err := enc.Encode(s.dev1Acct)
	c.Assert(err, IsNil)
	enc.Encode(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	batch := assertstate.NewBatch()
	refs, err := batch.AddStream(b)
	c.Assert(err, IsNil)
	c.Check(refs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{s.dev1Acct.AccountID()}},
		{Type: asserts.AccountKeyType, PrimaryKey: []string{s.storeSigning.StoreAccountKey("").PublicKeyID()}},
	})

	// noop
	err = batch.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	err = batch.Commit(s.state)
	c.Assert(err, IsNil)

	db := assertstate.DB(s.state)
	devAcct, err := db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	})
	c.Assert(err, IsNil)
	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")
}

func (s *assertMgrSuite) TestBatchConsiderPreexisting(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// prereq store key
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	batch := assertstate.NewBatch()
	err = batch.Add(s.dev1Acct)
	c.Assert(err, IsNil)

	err = batch.Commit(s.state)
	c.Assert(err, IsNil)

	db := assertstate.DB(s.state)
	devAcct, err := db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	})
	c.Assert(err, IsNil)
	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")
}

func (s *assertMgrSuite) TestBatchAddStreamReturnsEffectivelyAddedRefs(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	b := &bytes.Buffer{}
	enc := asserts.NewEncoder(b)
	// wrong order is ok
	err := enc.Encode(s.dev1Acct)
	c.Assert(err, IsNil)
	enc.Encode(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	batch := assertstate.NewBatch()

	err = batch.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	refs, err := batch.AddStream(b)
	c.Assert(err, IsNil)
	c.Check(refs, DeepEquals, []*asserts.Ref{
		{Type: asserts.AccountType, PrimaryKey: []string{s.dev1Acct.AccountID()}},
	})

	err = batch.Commit(s.state)
	c.Assert(err, IsNil)

	db := assertstate.DB(s.state)
	devAcct, err := db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	})
	c.Assert(err, IsNil)
	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")
}

func (s *assertMgrSuite) TestBatchCommitRefusesSelfSignedKey(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

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

	batch := assertstate.NewBatch()

	err = batch.Add(repair)
	c.Assert(err, IsNil)

	err = batch.Add(acctKey)
	c.Assert(err, IsNil)

	// this must fail
	err = batch.Commit(s.state)
	c.Assert(err, ErrorMatches, `circular assertions are not expected:.*`)
}

func (s *assertMgrSuite) TestBatchAddUnsupported(c *C) {
	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 111)
	defer restore()

	batch := assertstate.NewBatch()

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

func (s *assertMgrSuite) prereqSnapAssertions(c *C, revisions ...int) {
	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapDecl)
	c.Assert(err, IsNil)

	for _, rev := range revisions {
		headers = map[string]interface{}{
			"snap-id":       "snap-id-1",
			"snap-sha3-384": makeDigest(rev),
			"snap-size":     fmt.Sprintf("%d", len(fakeSnap(rev))),
			"snap-revision": fmt.Sprintf("%d", rev),
			"developer-id":  s.dev1Acct.AccountID(),
			"timestamp":     time.Now().Format(time.RFC3339),
		}
		snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
		c.Assert(err, IsNil)
		err = s.storeSigning.Add(snapRev)
		c.Assert(err, IsNil)
	}
}

func (s *assertMgrSuite) TestDoFetch(c *C) {
	s.prereqSnapAssertions(c, 10)

	s.state.Lock()
	defer s.state.Unlock()

	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{makeDigest(10)},
	}

	err := assertstate.DoFetch(s.state, 0, func(f asserts.Fetcher) error {
		return f.Fetch(ref)
	})
	c.Assert(err, IsNil)

	snapRev, err := ref.Resolve(assertstate.DB(s.state).Find)
	c.Assert(err, IsNil)
	c.Check(snapRev.(*asserts.SnapRevision).SnapRevision(), Equals, 10)
}

func (s *assertMgrSuite) TestFetchIdempotent(c *C) {
	s.prereqSnapAssertions(c, 10, 11)

	s.state.Lock()
	defer s.state.Unlock()

	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{makeDigest(10)},
	}
	fetching := func(f asserts.Fetcher) error {
		return f.Fetch(ref)
	}

	err := assertstate.DoFetch(s.state, 0, fetching)
	c.Assert(err, IsNil)

	ref = &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{makeDigest(11)},
	}

	err = assertstate.DoFetch(s.state, 0, fetching)
	c.Assert(err, IsNil)

	snapRev, err := ref.Resolve(assertstate.DB(s.state).Find)
	c.Assert(err, IsNil)
	c.Check(snapRev.(*asserts.SnapRevision).SnapRevision(), Equals, 11)
}

func (s *assertMgrSuite) settle(c *C) {
	err := s.o.Settle(5 * time.Second)
	c.Assert(err, IsNil)
}

func (s *assertMgrSuite) TestValidateSnap(c *C) {
	s.prereqSnapAssertions(c, 10)

	tempdir := c.MkDir()
	snapPath := filepath.Join(tempdir, "foo.snap")
	err := ioutil.WriteFile(snapPath, fakeSnap(10), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "...")
	t := s.state.NewTask("validate-snap", "Fetch and check snap assertions")
	snapsup := snapstate.SnapSetup{
		SnapPath: snapPath,
		UserID:   0,
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			SnapID:   "snap-id-1",
			Revision: snap.R(10),
		},
	}
	t.Set("snap-setup", snapsup)
	chg.AddTask(t)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	snapRev, err := assertstate.DB(s.state).Find(asserts.SnapRevisionType, map[string]string{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": makeDigest(10),
	})
	c.Assert(err, IsNil)
	c.Check(snapRev.(*asserts.SnapRevision).SnapRevision(), Equals, 10)
}

func (s *assertMgrSuite) TestValidateSnapNotFound(c *C) {
	tempdir := c.MkDir()
	snapPath := filepath.Join(tempdir, "foo.snap")
	err := ioutil.WriteFile(snapPath, fakeSnap(33), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "...")
	t := s.state.NewTask("validate-snap", "Fetch and check snap assertions")
	snapsup := snapstate.SnapSetup{
		SnapPath: snapPath,
		UserID:   0,
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			SnapID:   "snap-id-1",
			Revision: snap.R(33),
		},
	}
	t.Set("snap-setup", snapsup)
	chg.AddTask(t)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), ErrorMatches, `(?s).*cannot verify snap "foo", no matching signatures found.*`)
}

func (s *assertMgrSuite) TestValidateSnapCrossCheckFail(c *C) {
	s.prereqSnapAssertions(c, 10)

	tempdir := c.MkDir()
	snapPath := filepath.Join(tempdir, "foo.snap")
	err := ioutil.WriteFile(snapPath, fakeSnap(10), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "...")
	t := s.state.NewTask("validate-snap", "Fetch and check snap assertions")
	snapsup := snapstate.SnapSetup{
		SnapPath: snapPath,
		UserID:   0,
		SideInfo: &snap.SideInfo{
			RealName: "f",
			SnapID:   "snap-id-1",
			Revision: snap.R(10),
		},
	}
	t.Set("snap-setup", snapsup)
	chg.AddTask(t)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), ErrorMatches, `(?s).*cannot install snap "f" that is undergoing a rename to "foo".*`)
}

func (s *assertMgrSuite) TestValidateSnapSnapDeclIsTooNewFirstInstall(c *C) {
	c.Skip("the assertion service will make this scenario not possible")

	s.prereqSnapAssertions(c, 10)

	tempdir := c.MkDir()
	snapPath := filepath.Join(tempdir, "foo.snap")
	err := ioutil.WriteFile(snapPath, fakeSnap(10), 0644)
	c.Assert(err, IsNil)

	// update snap decl with one that is too new
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
		snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
		c.Assert(err, IsNil)
		err = s.storeSigning.Add(snapDecl)
		c.Assert(err, IsNil)
	})()

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "...")
	t := s.state.NewTask("validate-snap", "Fetch and check snap assertions")
	snapsup := snapstate.SnapSetup{
		SnapPath: snapPath,
		UserID:   0,
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			SnapID:   "snap-id-1",
			Revision: snap.R(10),
		},
	}
	t.Set("snap-setup", snapsup)
	chg.AddTask(t)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), ErrorMatches, `(?s).*proposed "snap-declaration" assertion has format 999 but 0 is latest supported.*`)
}

func (s *assertMgrSuite) snapDecl(c *C, name string, extraHeaders map[string]interface{}) *asserts.SnapDeclaration {
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

func (s *assertMgrSuite) stateFromDecl(decl *asserts.SnapDeclaration, revno snap.Revision) {
	name := decl.SnapName()
	snapID := decl.SnapID()
	snapstate.Set(s.state, name, &snapstate.SnapState{
		Active: true,
		Sequence: []*snap.SideInfo{
			{RealName: name, SnapID: snapID, Revision: revno},
		},
		Current: revno,
	})
}

func (s *assertMgrSuite) TestRefreshSnapDeclarations(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", nil)

	s.stateFromDecl(snapDeclFoo, snap.R(7))
	s.stateFromDecl(snapDeclBar, snap.R(3))
	snapstate.Set(s.state, "local", &snapstate.SnapState{
		Active: false,
		Sequence: []*snap.SideInfo{
			{RealName: "local", Revision: snap.R(-1)},
		},
		Current: snap.R(-1),
	})

	// previous state
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclBar)
	c.Assert(err, IsNil)

	// one changed assertion
	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "foo-id",
		"snap-name":    "fo-o",
		"publisher-id": s.dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
		"revision":     "1",
	}
	snapDeclFoo1, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapDeclFoo1)
	c.Assert(err, IsNil)

	err = assertstate.RefreshSnapDeclarations(s.state, 0)
	c.Assert(err, IsNil)

	a, err := assertstate.DB(s.state).Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.SnapDeclaration).SnapName(), Equals, "fo-o")

	// another one
	// one changed assertion
	headers = s.dev1Acct.Headers()
	headers["display-name"] = "Dev 1 edited display-name"
	headers["revision"] = "1"
	dev1Acct1, err := s.storeSigning.Sign(asserts.AccountType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(dev1Acct1)
	c.Assert(err, IsNil)

	err = assertstate.RefreshSnapDeclarations(s.state, 0)
	c.Assert(err, IsNil)

	a, err = assertstate.DB(s.state).Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.Account).DisplayName(), Equals, "Dev 1 edited display-name")

	// change snap decl to something that has a too new format

	(func() {
		restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 999)
		defer restore()

		headers := map[string]interface{}{
			"format":       "999",
			"series":       "16",
			"snap-id":      "foo-id",
			"snap-name":    "foo",
			"publisher-id": s.dev1Acct.AccountID(),
			"timestamp":    time.Now().Format(time.RFC3339),
			"revision":     "2",
		}

		snapDeclFoo2, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
		c.Assert(err, IsNil)
		err = s.storeSigning.Add(snapDeclFoo2)
		c.Assert(err, IsNil)
	})()

	// no error, kept the old one
	err = assertstate.RefreshSnapDeclarations(s.state, 0)
	c.Assert(err, IsNil)

	a, err = assertstate.DB(s.state).Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.SnapDeclaration).SnapName(), Equals, "fo-o")
	c.Check(a.(*asserts.SnapDeclaration).Revision(), Equals, 1)
}

func (s *assertMgrSuite) TestValidateRefreshesNothing(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	validated, err := assertstate.ValidateRefreshes(s.state, nil, nil, 0)
	c.Assert(err, IsNil)
	c.Check(validated, HasLen, 0)
}

func (s *assertMgrSuite) TestValidateRefreshesNoControl(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", nil)
	s.stateFromDecl(snapDeclFoo, snap.R(7))
	s.stateFromDecl(snapDeclBar, snap.R(3))

	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclBar)
	c.Assert(err, IsNil)

	fooRefresh := &snap.Info{
		SideInfo: snap.SideInfo{RealName: "foo", SnapID: "foo-id", Revision: snap.R(9)},
	}

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooRefresh}, nil, 0)
	c.Assert(err, IsNil)
	c.Check(validated, DeepEquals, []*snap.Info{fooRefresh})
}

func (s *assertMgrSuite) TestValidateRefreshesMissingValidation(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", map[string]interface{}{
		"refresh-control": []interface{}{"foo-id"},
	})
	s.stateFromDecl(snapDeclFoo, snap.R(7))
	s.stateFromDecl(snapDeclBar, snap.R(3))

	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclBar)
	c.Assert(err, IsNil)

	fooRefresh := &snap.Info{
		SideInfo: snap.SideInfo{RealName: "foo", SnapID: "foo-id", Revision: snap.R(9)},
	}

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooRefresh}, nil, 0)
	c.Assert(err, ErrorMatches, `cannot refresh "foo" to revision 9: no validation by "bar"`)
	c.Check(validated, HasLen, 0)
}

func (s *assertMgrSuite) TestValidateRefreshesMissingValidationButIgnore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", map[string]interface{}{
		"refresh-control": []interface{}{"foo-id"},
	})
	s.stateFromDecl(snapDeclFoo, snap.R(7))
	s.stateFromDecl(snapDeclBar, snap.R(3))

	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclBar)
	c.Assert(err, IsNil)

	fooRefresh := &snap.Info{
		SideInfo: snap.SideInfo{RealName: "foo", SnapID: "foo-id", Revision: snap.R(9)},
	}

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooRefresh}, map[string]bool{"foo-id": true}, 0)
	c.Assert(err, IsNil)
	c.Check(validated, DeepEquals, []*snap.Info{fooRefresh})
}

func (s *assertMgrSuite) TestValidateRefreshesValidationOK(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", map[string]interface{}{
		"refresh-control": []interface{}{"foo-id"},
	})
	snapDeclBaz := s.snapDecl(c, "baz", map[string]interface{}{
		"refresh-control": []interface{}{"foo-id"},
	})
	s.stateFromDecl(snapDeclFoo, snap.R(7))
	s.stateFromDecl(snapDeclBar, snap.R(3))
	s.stateFromDecl(snapDeclBaz, snap.R(1))
	snapstate.Set(s.state, "local", &snapstate.SnapState{
		Active: false,
		Sequence: []*snap.SideInfo{
			{RealName: "local", Revision: snap.R(-1)},
		},
		Current: snap.R(-1),
	})

	// validation by bar
	headers := map[string]interface{}{
		"series":                 "16",
		"snap-id":                "bar-id",
		"approved-snap-id":       "foo-id",
		"approved-snap-revision": "9",
		"timestamp":              time.Now().Format(time.RFC3339),
	}
	barValidation, err := s.dev1Signing.Sign(asserts.ValidationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(barValidation)
	c.Assert(err, IsNil)

	// validation by baz
	headers = map[string]interface{}{
		"series":                 "16",
		"snap-id":                "baz-id",
		"approved-snap-id":       "foo-id",
		"approved-snap-revision": "9",
		"timestamp":              time.Now().Format(time.RFC3339),
	}
	bazValidation, err := s.dev1Signing.Sign(asserts.ValidationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(bazValidation)
	c.Assert(err, IsNil)

	err = assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclBar)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclBaz)
	c.Assert(err, IsNil)

	fooRefresh := &snap.Info{
		SideInfo: snap.SideInfo{RealName: "foo", SnapID: "foo-id", Revision: snap.R(9)},
	}

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooRefresh}, nil, 0)
	c.Assert(err, IsNil)
	c.Check(validated, DeepEquals, []*snap.Info{fooRefresh})
}

func (s *assertMgrSuite) TestValidateRefreshesRevokedValidation(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", map[string]interface{}{
		"refresh-control": []interface{}{"foo-id"},
	})
	snapDeclBaz := s.snapDecl(c, "baz", map[string]interface{}{
		"refresh-control": []interface{}{"foo-id"},
	})
	s.stateFromDecl(snapDeclFoo, snap.R(7))
	s.stateFromDecl(snapDeclBar, snap.R(3))
	s.stateFromDecl(snapDeclBaz, snap.R(1))
	snapstate.Set(s.state, "local", &snapstate.SnapState{
		Active: false,
		Sequence: []*snap.SideInfo{
			{RealName: "local", Revision: snap.R(-1)},
		},
		Current: snap.R(-1),
	})

	// validation by bar
	headers := map[string]interface{}{
		"series":                 "16",
		"snap-id":                "bar-id",
		"approved-snap-id":       "foo-id",
		"approved-snap-revision": "9",
		"timestamp":              time.Now().Format(time.RFC3339),
	}
	barValidation, err := s.dev1Signing.Sign(asserts.ValidationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(barValidation)
	c.Assert(err, IsNil)

	// revoked validation by baz
	headers = map[string]interface{}{
		"series":                 "16",
		"snap-id":                "baz-id",
		"approved-snap-id":       "foo-id",
		"approved-snap-revision": "9",
		"revoked":                "true",
		"timestamp":              time.Now().Format(time.RFC3339),
	}
	bazValidation, err := s.dev1Signing.Sign(asserts.ValidationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(bazValidation)
	c.Assert(err, IsNil)

	err = assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclBar)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclBaz)
	c.Assert(err, IsNil)

	fooRefresh := &snap.Info{
		SideInfo: snap.SideInfo{RealName: "foo", SnapID: "foo-id", Revision: snap.R(9)},
	}

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooRefresh}, nil, 0)
	c.Assert(err, ErrorMatches, `(?s).*cannot refresh "foo" to revision 9: validation by "baz" \(id "baz-id"\) revoked.*`)
	c.Check(validated, HasLen, 0)
}

func (s *assertMgrSuite) TestBaseSnapDeclaration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	r1 := assertstest.MockBuiltinBaseDeclaration(nil)
	defer r1()

	baseDecl, err := assertstate.BaseDeclaration(s.state)
	c.Assert(asserts.IsNotFound(err), Equals, true)
	c.Check(baseDecl, IsNil)

	r2 := assertstest.MockBuiltinBaseDeclaration([]byte(`
type: base-declaration
authority-id: canonical
series: 16
plugs:
  iface: true
`))
	defer r2()

	baseDecl, err = assertstate.BaseDeclaration(s.state)
	c.Assert(err, IsNil)
	c.Check(baseDecl, NotNil)
	c.Check(baseDecl.PlugRule("iface"), NotNil)
}

func (s *assertMgrSuite) TestSnapDeclaration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// have a declaration in the system db
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	snapDeclFoo := s.snapDecl(c, "foo", nil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)

	_, err = assertstate.SnapDeclaration(s.state, "snap-id-other")
	c.Check(asserts.IsNotFound(err), Equals, true)

	snapDecl, err := assertstate.SnapDeclaration(s.state, "foo-id")
	c.Assert(err, IsNil)
	c.Check(snapDecl.SnapName(), Equals, "foo")
}

func (s *assertMgrSuite) TestAutoAliasesTemporaryFallback(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// prereqs for developer assertions in the system db
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)

	// not from the store
	aliases, err := assertstate.AutoAliases(s.state, &snap.Info{SuggestedName: "local"})
	c.Assert(err, IsNil)
	c.Check(aliases, HasLen, 0)

	// missing
	_, err = assertstate.AutoAliases(s.state, &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "baz",
			SnapID:   "baz-id",
		},
	})
	c.Check(err, ErrorMatches, `internal error: cannot find snap-declaration for installed snap "baz": snap-declaration \(baz-id; series:16\) not found`)

	info := snaptest.MockInfo(c, `
name: foo
version: 0
apps:
   cmd1:
     aliases: [alias1]
   cmd2:
     aliases: [alias2]
`, &snap.SideInfo{
		RealName: "foo",
		SnapID:   "foo-id",
	})

	// empty list
	// have a declaration in the system db
	snapDeclFoo := s.snapDecl(c, "foo", nil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)
	aliases, err = assertstate.AutoAliases(s.state, info)
	c.Assert(err, IsNil)
	c.Check(aliases, HasLen, 0)

	// some aliases
	snapDeclFoo = s.snapDecl(c, "foo", map[string]interface{}{
		"auto-aliases": []interface{}{"alias1", "alias2", "alias3"},
		"revision":     "1",
	})
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)
	aliases, err = assertstate.AutoAliases(s.state, info)
	c.Assert(err, IsNil)
	c.Check(aliases, DeepEquals, map[string]string{
		"alias1": "cmd1",
		"alias2": "cmd2",
	})
}

func (s *assertMgrSuite) TestAutoAliasesExplicit(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// prereqs for developer assertions in the system db
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)

	// not from the store
	aliases, err := assertstate.AutoAliases(s.state, &snap.Info{SuggestedName: "local"})
	c.Assert(err, IsNil)
	c.Check(aliases, HasLen, 0)

	// missing
	_, err = assertstate.AutoAliases(s.state, &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "baz",
			SnapID:   "baz-id",
		},
	})
	c.Check(err, ErrorMatches, `internal error: cannot find snap-declaration for installed snap "baz": snap-declaration \(baz-id; series:16\) not found`)

	// empty list
	// have a declaration in the system db
	snapDeclFoo := s.snapDecl(c, "foo", nil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)
	aliases, err = assertstate.AutoAliases(s.state, &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			SnapID:   "foo-id",
		},
	})
	c.Assert(err, IsNil)
	c.Check(aliases, HasLen, 0)

	// some aliases
	snapDeclFoo = s.snapDecl(c, "foo", map[string]interface{}{
		"aliases": []interface{}{
			map[string]interface{}{
				"name":   "alias1",
				"target": "cmd1",
			},
			map[string]interface{}{
				"name":   "alias2",
				"target": "cmd2",
			},
		},
		"revision": "1",
	})
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)
	aliases, err = assertstate.AutoAliases(s.state, &snap.Info{
		SideInfo: snap.SideInfo{
			RealName: "foo",
			SnapID:   "foo-id",
		},
	})
	c.Assert(err, IsNil)
	c.Check(aliases, DeepEquals, map[string]string{
		"alias1": "cmd1",
		"alias2": "cmd2",
	})
}

func (s *assertMgrSuite) TestPublisher(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// have a declaration in the system db
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	snapDeclFoo := s.snapDecl(c, "foo", nil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)

	_, err = assertstate.SnapDeclaration(s.state, "snap-id-other")
	c.Check(asserts.IsNotFound(err), Equals, true)

	acct, err := assertstate.Publisher(s.state, "foo-id")
	c.Assert(err, IsNil)
	c.Check(acct.AccountID(), Equals, s.dev1Acct.AccountID())
	c.Check(acct.Username(), Equals, "developer1")
}

func (s *assertMgrSuite) TestStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	storeHeaders := map[string]interface{}{
		"store":       "foo",
		"operator-id": s.dev1Acct.AccountID(),
		"timestamp":   time.Now().Format(time.RFC3339),
	}
	fooStore, err := s.storeSigning.Sign(asserts.StoreType, storeHeaders, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, fooStore)
	c.Assert(err, IsNil)

	_, err = assertstate.Store(s.state, "bar")
	c.Check(asserts.IsNotFound(err), Equals, true)

	store, err := assertstate.Store(s.state, "foo")
	c.Assert(err, IsNil)
	c.Check(store.Store(), Equals, "foo")
}
