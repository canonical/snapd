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

package assertstate_test

import (
	"bytes"
	"context"
	"crypto"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/sha3"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/httputil"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/testutil"
)

func TestAssertManager(t *testing.T) { TestingT(t) }

type assertMgrSuite struct {
	testutil.BaseTest

	o     *overlord.Overlord
	state *state.State
	se    *overlord.StateEngine
	mgr   *assertstate.AssertManager

	storeSigning *assertstest.StoreStack
	dev1Acct     *asserts.Account
	dev1AcctKey  *asserts.AccountKey
	dev1Signing  *assertstest.SigningDB

	fakeStore        snapstate.StoreService
	trivialDeviceCtx snapstate.DeviceContext
}

var _ = Suite(&assertMgrSuite{})

type fakeStore struct {
	storetest.Store
	state                           *state.State
	db                              asserts.RODatabase
	maxDeclSupportedFormat          int
	maxValidationSetSupportedFormat int

	requestedTypes [][]string
	opts           *store.RefreshOptions

	snapActionErr         error
	downloadAssertionsErr error
}

func (sto *fakeStore) pokeStateLock() {
	// the store should be called without the state lock held. Try
	// to acquire it.
	sto.state.Lock()
	sto.state.Unlock()
}

func (sto *fakeStore) Assertion(assertType *asserts.AssertionType, key []string, _ *auth.UserState) (asserts.Assertion, error) {
	sto.pokeStateLock()

	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, sto.maxDeclSupportedFormat)
	defer restore()

	ref := &asserts.Ref{Type: assertType, PrimaryKey: key}
	return ref.Resolve(sto.db.Find)
}

func (sto *fakeStore) SeqFormingAssertion(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
	sto.pokeStateLock()

	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, sto.maxDeclSupportedFormat)
	defer restore()

	ref := &asserts.AtSequence{
		Type:        assertType,
		SequenceKey: sequenceKey,
		Sequence:    sequence,
		Pinned:      sequence > 0,
	}

	if ref.Sequence <= 0 {
		hdrs, err := asserts.HeadersFromSequenceKey(ref.Type, ref.SequenceKey)
		if err != nil {
			return nil, err
		}
		return sto.db.FindSequence(ref.Type, hdrs, -1, -1)
	}

	return ref.Resolve(sto.db.Find)
}

func (sto *fakeStore) SnapAction(_ context.Context, currentSnaps []*store.CurrentSnap, actions []*store.SnapAction, assertQuery store.AssertionQuery, user *auth.UserState, opts *store.RefreshOptions) ([]store.SnapActionResult, []store.AssertionResult, error) {
	sto.pokeStateLock()

	if len(currentSnaps) != 0 || len(actions) != 0 {
		panic("only assertion query supported")
	}

	toResolve, toResolveSeq, err := assertQuery.ToResolve()
	if err != nil {
		return nil, nil, err
	}

	if sto.snapActionErr != nil {
		return nil, nil, sto.snapActionErr
	}

	sto.opts = opts

	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, sto.maxDeclSupportedFormat)
	defer restore()

	restoreSeq := asserts.MockMaxSupportedFormat(asserts.ValidationSetType, sto.maxValidationSetSupportedFormat)
	defer restoreSeq()

	reqTypes := make(map[string]bool)
	ares := make([]store.AssertionResult, 0, len(toResolve)+len(toResolveSeq))
	for g, ats := range toResolve {
		urls := make([]string, 0, len(ats))
		for _, at := range ats {
			reqTypes[at.Ref.Type.Name] = true
			a, err := at.Ref.Resolve(sto.db.Find)
			if err != nil {
				assertQuery.AddError(err, &at.Ref)
				continue
			}
			if a.Revision() > at.Revision {
				urls = append(urls, fmt.Sprintf("/assertions/%s", at.Unique()))
			}
		}
		ares = append(ares, store.AssertionResult{
			Grouping:   asserts.Grouping(g),
			StreamURLs: urls,
		})
	}

	for g, ats := range toResolveSeq {
		urls := make([]string, 0, len(ats))
		for _, at := range ats {
			reqTypes[at.Type.Name] = true
			var a asserts.Assertion
			headers, err := asserts.HeadersFromSequenceKey(at.Type, at.SequenceKey)
			if err != nil {
				return nil, nil, err
			}
			if !at.Pinned {
				a, err = sto.db.FindSequence(at.Type, headers, -1, asserts.ValidationSetType.MaxSupportedFormat())
			} else {
				a, err = at.Resolve(sto.db.Find)
			}
			if err != nil {
				assertQuery.AddSequenceError(err, at)
				continue
			}
			storeVs := a.(*asserts.ValidationSet)
			if storeVs.Sequence() > at.Sequence || (storeVs.Sequence() == at.Sequence && storeVs.Revision() >= at.Revision) {
				urls = append(urls, fmt.Sprintf("/assertions/%s/%s", a.Type().Name, strings.Join(a.At().PrimaryKey, "/")))
			}
		}
		ares = append(ares, store.AssertionResult{
			Grouping:   asserts.Grouping(g),
			StreamURLs: urls,
		})
	}

	// behave like the actual SnapAction if there are no results
	if len(ares) == 0 {
		return nil, ares, &store.SnapActionError{
			NoResults: true,
		}
	}

	typeNames := make([]string, 0, len(reqTypes))
	for k := range reqTypes {
		typeNames = append(typeNames, k)
	}
	sort.Strings(typeNames)
	sto.requestedTypes = append(sto.requestedTypes, typeNames)

	return nil, ares, nil
}

func (sto *fakeStore) DownloadAssertions(urls []string, b *asserts.Batch, user *auth.UserState) error {
	sto.pokeStateLock()

	if sto.downloadAssertionsErr != nil {
		return sto.downloadAssertionsErr
	}

	resolve := func(ref *asserts.Ref) (asserts.Assertion, error) {
		restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, sto.maxDeclSupportedFormat)
		defer restore()

		restoreSeq := asserts.MockMaxSupportedFormat(asserts.ValidationSetType, sto.maxValidationSetSupportedFormat)
		defer restoreSeq()
		return ref.Resolve(sto.db.Find)
	}

	for _, u := range urls {
		comps := strings.Split(u, "/")

		if len(comps) < 4 {
			return fmt.Errorf("cannot use URL: %s", u)
		}

		assertType := asserts.Type(comps[2])
		key := comps[3:]
		ref := &asserts.Ref{Type: assertType, PrimaryKey: key}
		a, err := resolve(ref)
		if err != nil {
			return err
		}
		if err := b.Add(a); err != nil {
			return err
		}
	}

	return nil
}

var (
	dev1PrivKey, _ = assertstest.GenerateKey(752)
)

func (s *assertMgrSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	s.storeSigning = assertstest.NewStoreStack("can0nical", nil)
	s.AddCleanup(sysdb.InjectTrusted(s.storeSigning.Trusted))

	s.dev1Acct = assertstest.NewAccount(s.storeSigning, "developer1", nil, "")
	err := s.storeSigning.Add(s.dev1Acct)
	c.Assert(err, IsNil)

	// developer signing
	s.dev1AcctKey = assertstest.NewAccountKey(s.storeSigning, s.dev1Acct, nil, dev1PrivKey.PublicKey(), "")
	err = s.storeSigning.Add(s.dev1AcctKey)
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

	s.fakeStore = &fakeStore{
		state: s.state,
		db:    s.storeSigning,
		// leave this comment to keep old gofmt happy
		maxDeclSupportedFormat:          asserts.SnapDeclarationType.MaxSupportedFormat(),
		maxValidationSetSupportedFormat: asserts.ValidationSetType.MaxSupportedFormat(),
	}
	s.trivialDeviceCtx = &snapstatetest.TrivialDeviceContext{
		CtxStore: s.fakeStore,
	}
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

func (s *assertMgrSuite) TestAddBatch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

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

	err = assertstate.AddBatch(s.state, batch, nil)
	c.Assert(err, IsNil)

	db := assertstate.DB(s.state)
	devAcct, err := db.Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	})
	c.Assert(err, IsNil)
	c.Check(devAcct.(*asserts.Account).Username(), Equals, "developer1")
}

func (s *assertMgrSuite) TestAddBatchPartial(c *C) {
	// Commit does add any successful assertion until the first error
	s.state.Lock()
	defer s.state.Unlock()

	// store key already present
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
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

	err = assertstate.AddBatch(s.state, batch, nil)
	c.Check(err, ErrorMatches, `(?ms).*validity.*`)

	// snap-declaration was added anyway
	_, err = assertstate.DB(s.state).Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(err, IsNil)
}

func (s *assertMgrSuite) TestAddBatchPrecheckPartial(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// store key already present
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
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

	err = assertstate.AddBatch(s.state, batch, &asserts.CommitOptions{
		Precheck: true,
	})
	c.Check(err, ErrorMatches, `(?ms).*validity.*`)

	// nothing was added
	_, err = assertstate.DB(s.state).Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (s *assertMgrSuite) TestAddBatchPrecheckHappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// store key already present
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
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

	err = assertstate.AddBatch(s.state, batch, &asserts.CommitOptions{
		Precheck: true,
	})
	c.Assert(err, IsNil)

	_, err = assertstate.DB(s.state).Find(asserts.SnapRevisionType, map[string]string{
		"snap-sha3-384": revDigest,
	})
	c.Check(err, IsNil)
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

func (s *assertMgrSuite) makeTestSnap(c *C, r int, extra string) string {
	yaml := `name: foo
version: %d
%s
`
	yaml = fmt.Sprintf(yaml, r, extra)
	return snaptest.MakeTestSnapWithFiles(c, yaml, nil)
}

func (s *assertMgrSuite) prereqSnapAssertions(c *C, provenance string, revisions ...int) (paths map[int]string, digests map[int]string) {
	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	if provenance != "" {
		headers["revision-authority"] = []interface{}{
			map[string]interface{}{
				"account-id": s.dev1Acct.AccountID(),
				"provenance": []interface{}{
					provenance,
				},
			},
		}
	}

	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapDecl)
	c.Assert(err, IsNil)

	paths = make(map[int]string)
	digests = make(map[int]string)

	for _, rev := range revisions {
		snapPath := s.makeTestSnap(c, rev, "")
		digest, sz, err := asserts.SnapFileSHA3_384(snapPath)
		c.Assert(err, IsNil)
		paths[rev] = snapPath
		digests[rev] = digest

		headers = map[string]interface{}{
			"snap-id":       "snap-id-1",
			"snap-sha3-384": digest,
			"snap-size":     fmt.Sprintf("%d", sz),
			"snap-revision": fmt.Sprintf("%d", rev),
			"developer-id":  s.dev1Acct.AccountID(),
			"timestamp":     time.Now().Format(time.RFC3339),
		}
		signer := assertstest.SignerDB(s.storeSigning)
		if provenance != "" {
			headers["provenance"] = provenance
			signer = s.dev1Signing
		}

		snapRev, err := signer.Sign(asserts.SnapRevisionType, headers, nil, "")
		c.Assert(err, IsNil)
		err = s.storeSigning.Add(snapRev)
		c.Assert(err, IsNil)
	}

	return paths, digests
}

func (s *assertMgrSuite) prereqComponentAssertions(c *C, provenance string, snapRev, compRev snap.Revision) (compPath string, digest string) {
	const (
		resourceName  = "test-component"
		snapID        = "snap-id-1"
		componentYaml = `component: snap+test-component
type: test
version: 1.0.2
`
	)

	compPath = snaptest.MakeTestComponentWithFiles(c, resourceName+".comp", componentYaml, nil)

	digest, size, err := asserts.SnapFileSHA3_384(compPath)
	c.Assert(err, IsNil)

	revHeaders := map[string]interface{}{
		"snap-id":           snapID,
		"resource-name":     resourceName,
		"resource-sha3-384": digest,
		"resource-revision": compRev.String(),
		"resource-size":     strconv.Itoa(int(size)),
		"developer-id":      s.dev1Acct.AccountID(),
		"timestamp":         time.Now().Format(time.RFC3339),
	}

	signer := assertstest.SignerDB(s.storeSigning)
	if provenance != "" {
		revHeaders["provenance"] = provenance
		signer = s.dev1Signing
	}

	resourceRev, err := signer.Sign(asserts.SnapResourceRevisionType, revHeaders, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(resourceRev)
	c.Assert(err, IsNil)

	pairHeaders := map[string]interface{}{
		"snap-id":           snapID,
		"resource-name":     resourceName,
		"resource-revision": compRev.String(),
		"snap-revision":     snapRev.String(),
		"developer-id":      s.dev1Acct.AccountID(),
		"timestamp":         time.Now().Format(time.RFC3339),
	}
	if provenance != "" {
		pairHeaders["provenance"] = provenance
	}

	resourcePair, err := signer.Sign(asserts.SnapResourcePairType, pairHeaders, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(resourcePair)
	c.Assert(err, IsNil)

	return compPath, digest
}

func (s *assertMgrSuite) TestDoFetch(c *C) {
	_, digests := s.prereqSnapAssertions(c, "", 10)

	s.state.Lock()
	defer s.state.Unlock()

	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{digests[10]},
	}

	err := assertstate.DoFetch(s.state, 0, s.trivialDeviceCtx, nil, func(f asserts.Fetcher) error {
		return f.Fetch(ref)
	})
	c.Assert(err, IsNil)

	snapRev, err := ref.Resolve(assertstate.DB(s.state).Find)
	c.Assert(err, IsNil)
	c.Check(snapRev.(*asserts.SnapRevision).SnapRevision(), Equals, 10)
}

func (s *assertMgrSuite) TestFetchIdempotent(c *C) {
	_, digests := s.prereqSnapAssertions(c, "", 10, 11)

	s.state.Lock()
	defer s.state.Unlock()

	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{digests[10]},
	}
	fetching := func(f asserts.Fetcher) error {
		return f.Fetch(ref)
	}

	err := assertstate.DoFetch(s.state, 0, s.trivialDeviceCtx, nil, fetching)
	c.Assert(err, IsNil)

	ref = &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{digests[11]},
	}

	err = assertstate.DoFetch(s.state, 0, s.trivialDeviceCtx, nil, fetching)
	c.Assert(err, IsNil)

	snapRev, err := ref.Resolve(assertstate.DB(s.state).Find)
	c.Assert(err, IsNil)
	c.Check(snapRev.(*asserts.SnapRevision).SnapRevision(), Equals, 11)
}

func (s *assertMgrSuite) settle(c *C) {
	err := s.o.Settle(5 * time.Second)
	c.Assert(err, IsNil)
}

func (s *assertMgrSuite) TestFetchUnsupportedUpdateIgnored(c *C) {
	// ATM in principle we ignore updated assertions with unsupported formats
	// NB: this scenario can only happen if there is a bug
	// we ask the store to filter what is returned by max supported format!
	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 111)
	defer restore()

	logbuf, restore := logger.MockLogger()
	defer restore()

	snapDeclFoo0 := s.snapDecl(c, "foo", nil)

	s.state.Lock()
	defer s.state.Unlock()
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo0)
	c.Assert(err, IsNil)

	var snapDeclFoo1 *asserts.SnapDeclaration
	(func() {
		restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 999)
		defer restore()
		snapDeclFoo1 = s.snapDecl(c, "foo", map[string]interface{}{
			"format":   "999",
			"revision": "1",
		})
	})()
	c.Check(snapDeclFoo1.Revision(), Equals, 1)

	ref := &asserts.Ref{
		Type:       asserts.SnapDeclarationType,
		PrimaryKey: []string{"16", "foo-id"},
	}
	fetching := func(f asserts.Fetcher) error {
		return f.Fetch(ref)
	}

	s.fakeStore.(*fakeStore).maxDeclSupportedFormat = 999
	err = assertstate.DoFetch(s.state, 0, s.trivialDeviceCtx, nil, fetching)
	// no error and the old one was kept
	c.Assert(err, IsNil)
	snapDecl, err := ref.Resolve(assertstate.DB(s.state).Find)
	c.Assert(err, IsNil)
	c.Check(snapDecl.Revision(), Equals, 0)

	// we log the issue
	c.Check(logbuf.String(), testutil.Contains, `Cannot update assertion snap-declaration (foo-id;`)
}

func (s *assertMgrSuite) TestFetchUnsupportedError(c *C) {
	// NB: this scenario can only happen if there is a bug
	// we ask the store to filter what is returned by max supported format!

	restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 111)
	defer restore()

	s.state.Lock()
	defer s.state.Unlock()

	var snapDeclFoo1 *asserts.SnapDeclaration
	(func() {
		restore := asserts.MockMaxSupportedFormat(asserts.SnapDeclarationType, 999)
		defer restore()
		snapDeclFoo1 = s.snapDecl(c, "foo", map[string]interface{}{
			"format":   "999",
			"revision": "1",
		})
	})()
	c.Check(snapDeclFoo1.Revision(), Equals, 1)

	ref := &asserts.Ref{
		Type:       asserts.SnapDeclarationType,
		PrimaryKey: []string{"16", "foo-id"},
	}
	fetching := func(f asserts.Fetcher) error {
		return f.Fetch(ref)
	}

	s.fakeStore.(*fakeStore).maxDeclSupportedFormat = 999
	err := assertstate.DoFetch(s.state, 0, s.trivialDeviceCtx, nil, fetching)
	c.Check(err, ErrorMatches, `(?s).*proposed "snap-declaration" assertion has format 999 but 111 is latest supported.*`)
}

func (s *assertMgrSuite) setModel(model *asserts.Model) {
	deviceCtx := &snapstatetest.TrivialDeviceContext{
		DeviceModel: model,
		CtxStore:    s.fakeStore,
	}
	s.AddCleanup(snapstatetest.MockDeviceContext(deviceCtx))
	s.state.Set("seeded", true)
}

func (s *assertMgrSuite) setupModelAndStore(c *C) *asserts.Store {
	// setup a model and store assertion
	a := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"store":        "my-brand-store",
		"gadget":       "gadget",
		"kernel":       "krnl",
	})
	s.setModel(a.(*asserts.Model))

	a, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"authority-id": s.storeSigning.AuthorityID,
		"operator-id":  s.storeSigning.AuthorityID,
		"store":        "my-brand-store",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	return a.(*asserts.Store)
}

func (s *assertMgrSuite) TestValidateSnap(c *C) {
	paths, digests := s.prereqSnapAssertions(c, "", 10)
	snapPath := paths[10]

	s.state.Lock()
	defer s.state.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

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
		"snap-sha3-384": digests[10],
	})
	c.Assert(err, IsNil)
	c.Check(snapRev.(*asserts.SnapRevision).SnapRevision(), Equals, 10)

	// store assertion was also fetched
	_, err = assertstate.DB(s.state).Find(asserts.StoreType, map[string]string{
		"store": "my-brand-store",
	})
	c.Assert(err, IsNil)
}

func (s *assertMgrSuite) TestValidateSnapStoreNotFound(c *C) {
	paths, digests := s.prereqSnapAssertions(c, "", 10)

	snapPath := paths[10]

	s.state.Lock()
	defer s.state.Unlock()

	// have a model and store but store assertion is not made available
	s.setupModelAndStore(c)

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
		"snap-sha3-384": digests[10],
	})
	c.Assert(err, IsNil)
	c.Check(snapRev.(*asserts.SnapRevision).SnapRevision(), Equals, 10)

	// store assertion was not found and ignored
	_, err = assertstate.DB(s.state).Find(asserts.StoreType, map[string]string{
		"store": "my-brand-store",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (s *assertMgrSuite) TestValidateSnapMissingSnapSetup(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "...")
	t := s.state.NewTask("validate-snap", "Fetch and check snap assertions")
	chg.AddTask(t)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), ErrorMatches, `(?s).*internal error: cannot obtain snap setup: no state entry for key.*`)
}

func (s *assertMgrSuite) TestValidateSnapNotFound(c *C) {
	snapPath := s.makeTestSnap(c, 33, "")

	s.state.Lock()
	defer s.state.Unlock()

	s.setModel(sysdb.GenericClassicModel())

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
	paths, _ := s.prereqSnapAssertions(c, "", 10)

	snapPath := paths[10]

	s.state.Lock()
	defer s.state.Unlock()

	s.setModel(sysdb.GenericClassicModel())

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

	c.Assert(chg.Err(), ErrorMatches, `(?s).*cannot install "f", snap "f" is undergoing a rename to "foo".*`)
}

func (s *assertMgrSuite) TestValidateDelegatedSnap(c *C) {
	snapPath := s.makeTestSnap(c, 10, `provenance: delegated-prov`)
	digest, sz, err := asserts.SnapFileSHA3_384(snapPath)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": s.dev1Acct.AccountID(),
				"provenance": []interface{}{"delegated-prov"},
				"on-store":   []interface{}{"my-brand-store"},
				"on-model":   []interface{}{"my-brand/my-model"},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapDecl)
	c.Assert(err, IsNil)

	headers = map[string]interface{}{
		"authority-id":  s.dev1Acct.AccountID(),
		"series":        "16",
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"provenance":    "delegated-prov",
		"snap-size":     fmt.Sprintf("%d", sz),
		"snap-revision": "10",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.dev1Signing.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapRev)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err = s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

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
		ExpectedProvenance: "delegated-prov",
	}
	t.Set("snap-setup", snapsup)
	chg.AddTask(t)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	snapRev1, err := assertstate.DB(s.state).Find(asserts.SnapRevisionType, map[string]string{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"provenance":    "delegated-prov",
	})
	c.Assert(err, IsNil)
	c.Check(snapRev1.(*asserts.SnapRevision).SnapRevision(), Equals, 10)

	// store assertion was also fetched
	_, err = assertstate.DB(s.state).Find(asserts.StoreType, map[string]string{
		"store": "my-brand-store",
	})
	c.Assert(err, IsNil)
}

func (s *assertMgrSuite) TestValidateDelegatedSnapProvenanceMismatch(c *C) {
	err := s.testValidateDelegatedSnapMismatch(c, `provenance: delegated-prov-other`, "delegated-prov-other", "delegated-prov", map[string]interface{}{
		"account-id": s.dev1Acct.AccountID(),
		"provenance": []interface{}{"delegated-prov"},
	})
	c.Check(err, ErrorMatches, `(?s).*cannot verify snap "foo", no matching signatures found.*`)
}

func (s *assertMgrSuite) TestValidateDelegatedSnapStoreProvenanceMismatch(c *C) {
	// this is a scenario where a store is serving information matching
	// the assertions which themselves don't match the snap
	err := s.testValidateDelegatedSnapMismatch(c, `provenance: delegated-prov-other`, "delegated-prov", "delegated-prov", map[string]interface{}{
		"account-id": s.dev1Acct.AccountID(),
		"provenance": []interface{}{"delegated-prov"},
	})
	c.Check(err, ErrorMatches, `(?s).*snap ".*foo.*\.snap" has been signed under provenance "delegated-prov" different from the metadata one: "delegated-prov-other".*`)
}

func (s *assertMgrSuite) testValidateDelegatedSnapMismatch(c *C, provenanceFrag, expectedProv, revProvenance string, revisionAuthority map[string]interface{}) error {
	snapPath := s.makeTestSnap(c, 10, provenanceFrag)
	digest, sz, err := asserts.SnapFileSHA3_384(snapPath)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"revision-authority": []interface{}{
			revisionAuthority,
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapDecl)
	c.Assert(err, IsNil)

	headers = map[string]interface{}{
		"authority-id":  s.dev1Acct.AccountID(),
		"series":        "16",
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", sz),
		"snap-revision": "10",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	if revProvenance != "" {
		headers["provenance"] = revProvenance
	}
	snapRev, err := s.dev1Signing.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapRev)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err = s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

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
		ExpectedProvenance: expectedProv,
	}
	t.Set("snap-setup", snapsup)
	chg.AddTask(t)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	return chg.Err()
}

func (s *assertMgrSuite) TestValidateDelegatedSnapDeviceMismatch(c *C) {
	err := s.testValidateDelegatedSnapMismatch(c, `provenance: delegated-prov`, "delegated-prov", "delegated-prov", map[string]interface{}{
		"account-id": s.dev1Acct.AccountID(),
		"provenance": []interface{}{"delegated-prov"},
		"on-store":   []interface{}{"other-store"},
	})
	c.Check(err, ErrorMatches, `(?s).*snap "foo" revision assertion with provenance "delegated-prov" is not signed by an authority authorized on this device: .*`)
}

func (s *assertMgrSuite) TestValidateDelegatedSnapDefaultProvenanceMismatch(c *C) {
	err := s.testValidateDelegatedSnapMismatch(c, "", "", "delegated-prov", map[string]interface{}{
		"account-id": s.dev1Acct.AccountID(),
		"provenance": []interface{}{"delegated-prov"},
		"on-store":   []interface{}{"my-brand-store"},
	})
	c.Check(err, ErrorMatches, `(?s).*cannot verify snap "foo", no matching signatures found.*`)
}

func (s *assertMgrSuite) validationSetAssert(c *C, name, sequence, revision string, snapPresence, requiredRevision string) *asserts.ValidationSet {
	snaps := []interface{}{map[string]interface{}{
		"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
		"name":     "foo",
		"presence": snapPresence,
	}}
	if requiredRevision != "" {
		snaps[0].(map[string]interface{})["revision"] = requiredRevision
	}
	return s.validationSetAssertForSnaps(c, name, sequence, revision, snaps)
}

func (s *assertMgrSuite) validationSetAssertForSnaps(c *C, name, sequence, revision string, snaps []interface{}) *asserts.ValidationSet {
	headers := map[string]interface{}{
		"series":       "16",
		"account-id":   s.dev1Acct.AccountID(),
		"authority-id": s.dev1Acct.AccountID(),
		"publisher-id": s.dev1Acct.AccountID(),
		"name":         name,
		"sequence":     sequence,
		"snaps":        snaps,
		"timestamp":    time.Now().Format(time.RFC3339),
		"revision":     revision,
	}
	a, err := s.dev1Signing.Sign(asserts.ValidationSetType, headers, nil, "")
	c.Assert(err, IsNil)
	return a.(*asserts.ValidationSet)
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

func (s *assertMgrSuite) stateFromDecl(c *C, decl *asserts.SnapDeclaration, instanceName string, revno snap.Revision) {
	snapName, instanceKey := snap.SplitInstanceName(instanceName)
	if snapName == "" {
		snapName = decl.SnapName()
		instanceName = snapName
	}

	c.Assert(snapName, Equals, decl.SnapName())

	snapID := decl.SnapID()
	snapstate.Set(s.state, instanceName, &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: snapName, SnapID: snapID, Revision: revno},
		}),
		Current:     revno,
		InstanceKey: instanceKey,
	})
}

func (s *assertMgrSuite) TestRefreshAssertionsRefreshSnapDeclarationsAndValidationSets(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	storeAs := s.setupModelAndStore(c)
	snapDeclFoo := s.snapDecl(c, "foo", nil)

	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))
	c.Assert(s.storeSigning.Add(storeAs), IsNil)

	// previous state
	c.Assert(assertstate.Add(s.state, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(s.state, snapDeclFoo), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1AcctKey), IsNil)

	vsetAs1 := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(assertstate.Add(s.state, vsetAs1), IsNil)
	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	// changed snap decl assertion
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

	// changed validation set assertion
	vsetAs2 := s.validationSetAssert(c, "bar", "2", "3", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	err = assertstate.RefreshSnapAssertions(s.state, 0, &assertstate.RefreshAssertionsOptions{IsRefreshOfAllSnaps: true})
	c.Assert(err, IsNil)

	a, err := assertstate.DB(s.state).Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.SnapDeclaration).SnapName(), Equals, "fo-o")

	a, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
	c.Check(a.Revision(), Equals, 3)

	c.Assert(err, IsNil)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, false)

	// changed validation set assertion again
	vsetAs3 := s.validationSetAssert(c, "bar", "4", "5", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs3), IsNil)

	// but pretend it's not a refresh of all snaps
	err = assertstate.RefreshSnapAssertions(s.state, 0, &assertstate.RefreshAssertionsOptions{IsRefreshOfAllSnaps: false})
	c.Assert(err, IsNil)

	// so the assertion is not updated
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "4",
	})
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsTooEarly(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	r := snapstatetest.MockDeviceModel(nil)
	defer r()

	err := assertstate.RefreshSnapDeclarations(s.state, 0, nil)
	c.Check(err, FitsTypeOf, &snapstate.ChangeConflictError{})
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsNop(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.setModel(sysdb.GenericClassicModel())

	err := assertstate.RefreshSnapDeclarations(s.state, 0, &assertstate.RefreshAssertionsOptions{IsAutoRefresh: true})
	c.Assert(err, IsNil)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, true)
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsNoStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.setModel(sysdb.GenericClassicModel())

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", nil)

	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))
	s.stateFromDecl(c, snapDeclBar, "", snap.R(3))
	snapstate.Set(s.state, "local", &snapstate.SnapState{
		Active: false,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "local", Revision: snap.R(-1)},
		}),
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

	err = assertstate.RefreshSnapDeclarations(s.state, 0, nil)
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

	err = assertstate.RefreshSnapDeclarations(s.state, 0, nil)
	c.Assert(err, IsNil)

	a, err = assertstate.DB(s.state).Find(asserts.AccountType, map[string]string{
		"account-id": s.dev1Acct.AccountID(),
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.Account).DisplayName(), Equals, "Dev 1 edited display-name")

	// change snap decl to something that has a too new format
	s.fakeStore.(*fakeStore).maxDeclSupportedFormat = 999
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
	err = assertstate.RefreshSnapDeclarations(s.state, 0, nil)
	c.Assert(err, IsNil)

	a, err = assertstate.DB(s.state).Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.SnapDeclaration).SnapName(), Equals, "fo-o")
	c.Check(a.(*asserts.SnapDeclaration).Revision(), Equals, 1)
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsChangingKey(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.setModel(sysdb.GenericClassicModel())

	snapDeclFoo := s.snapDecl(c, "foo", nil)

	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))

	// previous state
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)

	storePrivKey2, _ := assertstest.GenerateKey(752)
	err = s.storeSigning.ImportKey(storePrivKey2)
	c.Assert(err, IsNil)
	storeKey2 := assertstest.NewAccountKey(s.storeSigning.RootSigning, s.storeSigning.TrustedAccount, map[string]interface{}{
		"name": "store2",
	}, storePrivKey2.PublicKey(), "")
	err = s.storeSigning.Add(storeKey2)
	c.Assert(err, IsNil)

	// one changed assertion signed with different key
	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "foo-id",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
		"revision":     "1",
	}
	storeKey2ID := storePrivKey2.PublicKey().ID()
	snapDeclFoo1, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, storeKey2ID)
	c.Assert(err, IsNil)
	c.Check(snapDeclFoo1.SignKeyID(), Not(Equals), snapDeclFoo.SignKeyID())
	err = s.storeSigning.Add(snapDeclFoo1)
	c.Assert(err, IsNil)

	_, err = storeKey2.Ref().Resolve(assertstate.DB(s.state).Find)
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	err = assertstate.RefreshSnapDeclarations(s.state, 0, nil)
	c.Assert(err, IsNil)

	a, err := assertstate.DB(s.state).Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(err, IsNil)
	c.Check(a.Revision(), Equals, 1)
	c.Check(a.SignKeyID(), Equals, storeKey2ID)

	// key was fetched as well
	_, err = storeKey2.Ref().Resolve(assertstate.DB(s.state).Find)
	c.Check(err, IsNil)
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsWithStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	storeAs := s.setupModelAndStore(c)

	snapDeclFoo := s.snapDecl(c, "foo", nil)

	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))

	// previous state
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo)
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

	// store assertion is missing
	err = assertstate.RefreshSnapDeclarations(s.state, 0, nil)
	c.Assert(err, IsNil)

	a, err := assertstate.DB(s.state).Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.SnapDeclaration).SnapName(), Equals, "fo-o")

	// changed again
	headers = map[string]interface{}{
		"series":       "16",
		"snap-id":      "foo-id",
		"snap-name":    "f-oo",
		"publisher-id": s.dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
		"revision":     "2",
	}
	snapDeclFoo2, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(snapDeclFoo2)
	c.Assert(err, IsNil)

	// store assertion is available
	err = s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	err = assertstate.RefreshSnapDeclarations(s.state, 0, nil)
	c.Assert(err, IsNil)

	a, err = assertstate.DB(s.state).Find(asserts.SnapDeclarationType, map[string]string{
		"series":  "16",
		"snap-id": "foo-id",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.SnapDeclaration).SnapName(), Equals, "f-oo")

	_, err = assertstate.DB(s.state).Find(asserts.StoreType, map[string]string{
		"store": "my-brand-store",
	})
	c.Assert(err, IsNil)

	// store assertion has changed
	a, err = s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"authority-id": s.storeSigning.AuthorityID,
		"operator-id":  s.storeSigning.AuthorityID,
		"store":        "my-brand-store",
		"location":     "the-cloud",
		"revision":     "1",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	storeAs = a.(*asserts.Store)
	err = s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	err = assertstate.RefreshSnapDeclarations(s.state, 0, nil)
	c.Assert(err, IsNil)
	a, err = assertstate.DB(s.state).Find(asserts.StoreType, map[string]string{
		"store": "my-brand-store",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.Store).Location(), Equals, "the-cloud")
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsDownloadError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.setModel(sysdb.GenericClassicModel())

	snapDeclFoo := s.snapDecl(c, "foo", nil)

	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))

	// previous state
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo)
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

	s.fakeStore.(*fakeStore).downloadAssertionsErr = errors.New("download error")

	err = assertstate.RefreshSnapDeclarations(s.state, 0, nil)
	c.Assert(err, ErrorMatches, `cannot refresh snap-declarations for snaps:
 - foo: download error`)
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsPersistentNetworkError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.setModel(sysdb.GenericClassicModel())

	snapDeclFoo := s.snapDecl(c, "foo", nil)

	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))

	// previous state
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo)
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

	pne := new(httputil.PersistentNetworkError)
	s.fakeStore.(*fakeStore).snapActionErr = pne

	err = assertstate.RefreshSnapDeclarations(s.state, 0, nil)
	c.Assert(err, Equals, pne)
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsNoStoreFallback(c *C) {
	// test that if we get a 4xx or 500 error from the store trying bulk
	// assertion refresh we fall back to the old logic
	s.fakeStore.(*fakeStore).snapActionErr = &store.UnexpectedHTTPStatusError{StatusCode: 400}

	logbuf, restore := logger.MockLogger()
	defer restore()

	s.TestRefreshSnapDeclarationsNoStore(c)

	c.Check(logbuf.String(), Matches, "(?m).*bulk refresh of snap-declarations failed, falling back to one-by-one assertion fetching:.*HTTP status code 400.*")
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsNoStoreFallbackUnexpectedSnapActionError(c *C) {
	// test that if we get an unexpected SnapAction error from the
	// store trying bulk assertion refresh we fall back to the old
	// logic
	s.fakeStore.(*fakeStore).snapActionErr = &store.SnapActionError{
		NoResults: true,
		Other:     []error{errors.New("unexpected error")},
	}

	logbuf, restore := logger.MockLogger()
	defer restore()

	s.TestRefreshSnapDeclarationsNoStore(c)

	c.Check(logbuf.String(), Matches, "(?m).*bulk refresh of snap-declarations failed, falling back to one-by-one assertion fetching:.*unexpected error.*")
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsWithStoreFallback(c *C) {
	// test that if we get a 4xx or 500 error from the store trying bulk
	// assertion refresh we fall back to the old logic
	s.fakeStore.(*fakeStore).snapActionErr = &store.UnexpectedHTTPStatusError{StatusCode: 500}

	logbuf, restore := logger.MockLogger()
	defer restore()

	s.TestRefreshSnapDeclarationsWithStore(c)

	c.Check(logbuf.String(), Matches, "(?m).*bulk refresh of snap-declarations failed, falling back to one-by-one assertion fetching:.*HTTP status code 500.*")
}

// the following tests cover what happens when refreshing snap-declarations
// need to support overflowing the chosen asserts.Pool maximum groups

func (s *assertMgrSuite) testRefreshSnapDeclarationsMany(c *C, n int) error {
	// reduce maxGroups to test and stress the logic that deals
	// with overflowing it
	s.AddCleanup(assertstate.MockMaxGroups(16))

	// previous state
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)

	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("foo%d", i)
		snapDeclFooX := s.snapDecl(c, name, nil)

		s.stateFromDecl(c, snapDeclFooX, "", snap.R(7+i))

		// previous state
		err = assertstate.Add(s.state, snapDeclFooX)
		c.Assert(err, IsNil)

		// make an update on top
		headers := map[string]interface{}{
			"series":       "16",
			"snap-id":      name + "-id",
			"snap-name":    fmt.Sprintf("fo-o-%d", i),
			"publisher-id": s.dev1Acct.AccountID(),
			"timestamp":    time.Now().Format(time.RFC3339),
			"revision":     "1",
		}
		snapDeclFooX1, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
		c.Assert(err, IsNil)
		err = s.storeSigning.Add(snapDeclFooX1)
		c.Assert(err, IsNil)
	}

	err = assertstate.RefreshSnapDeclarations(s.state, 0, nil)
	if err != nil {
		// fot the caller to check
		return err
	}

	// check we got the updates
	for i := 1; i <= n; i++ {
		name := fmt.Sprintf("foo%d", i)
		a, err := assertstate.DB(s.state).Find(asserts.SnapDeclarationType, map[string]string{
			"series":  "16",
			"snap-id": name + "-id",
		})
		c.Assert(err, IsNil)
		c.Check(a.(*asserts.SnapDeclaration).SnapName(), Equals, fmt.Sprintf("fo-o-%d", i))
	}

	return nil
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsMany14NoStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.setModel(sysdb.GenericClassicModel())

	err := s.testRefreshSnapDeclarationsMany(c, 14)
	c.Assert(err, IsNil)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		{"account", "account-key", "snap-declaration"},
	})
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsMany16NoStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.setModel(sysdb.GenericClassicModel())

	err := s.testRefreshSnapDeclarationsMany(c, 16)
	c.Assert(err, IsNil)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		{"account", "account-key", "snap-declaration"},
	})
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsMany16WithStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	err = s.testRefreshSnapDeclarationsMany(c, 16)
	c.Assert(err, IsNil)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		// first 16 groups request
		{"account", "account-key", "snap-declaration"},
		// final separate request covering store only
		{"store"},
	})

	// store assertion was also fetched
	_, err = assertstate.DB(s.state).Find(asserts.StoreType, map[string]string{
		"store": "my-brand-store",
	})
	c.Assert(err, IsNil)
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsMany17NoStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.setModel(sysdb.GenericClassicModel())

	err := s.testRefreshSnapDeclarationsMany(c, 17)
	c.Assert(err, IsNil)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		// first 16 groups request
		{"account", "account-key", "snap-declaration"},
		// final separate request for the rest
		{"snap-declaration"},
	})
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsMany17NoStoreMergeErrors(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.setModel(sysdb.GenericClassicModel())

	s.fakeStore.(*fakeStore).downloadAssertionsErr = errors.New("download error")

	err := s.testRefreshSnapDeclarationsMany(c, 17)
	c.Check(err, ErrorMatches, `(?s)cannot refresh snap-declarations for snaps:
 - foo1: download error.* - foo9: download error`)
	// all foo* snaps accounted for
	c.Check(strings.Count(err.Error(), "foo"), Equals, 17)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		// first 16 groups request
		{"account", "account-key", "snap-declaration"},
		// final separate request for the rest
		{"snap-declaration"},
	})
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsMany31WithStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	err = s.testRefreshSnapDeclarationsMany(c, 31)
	c.Assert(err, IsNil)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		// first 16 groups request
		{"account", "account-key", "snap-declaration"},
		// final separate request for the rest and store
		{"snap-declaration", "store"},
	})

	// store assertion was also fetched
	_, err = assertstate.DB(s.state).Find(asserts.StoreType, map[string]string{
		"store": "my-brand-store",
	})
	c.Assert(err, IsNil)
}

func (s *assertMgrSuite) TestRefreshSnapDeclarationsMany32WithStore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	err = s.testRefreshSnapDeclarationsMany(c, 32)
	c.Assert(err, IsNil)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		// first 16 groups request
		{"account", "account-key", "snap-declaration"},
		// 2nd round request
		{"snap-declaration"},
		// final separate request covering store
		{"store"},
	})

	// store assertion was also fetched
	_, err = assertstate.DB(s.state).Find(asserts.StoreType, map[string]string{
		"store": "my-brand-store",
	})
	c.Assert(err, IsNil)
}

func (s *assertMgrSuite) TestValidateRefreshesNothing(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	validated, err := assertstate.ValidateRefreshes(s.state, nil, nil, 0, s.trivialDeviceCtx)
	c.Assert(err, IsNil)
	c.Check(validated, HasLen, 0)
}

func (s *assertMgrSuite) TestValidateRefreshesNoControl(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", nil)
	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))
	s.stateFromDecl(c, snapDeclBar, "", snap.R(3))

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

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooRefresh}, nil, 0, s.trivialDeviceCtx)
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
	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))
	s.stateFromDecl(c, snapDeclBar, "", snap.R(3))

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

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooRefresh}, nil, 0, s.trivialDeviceCtx)
	c.Assert(err, ErrorMatches, `cannot refresh "foo" to revision 9: no validation by "bar"`)
	c.Check(validated, HasLen, 0)
}

func (s *assertMgrSuite) TestParallelInstanceValidateRefreshesMissingValidation(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", map[string]interface{}{
		"refresh-control": []interface{}{"foo-id"},
	})
	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))
	s.stateFromDecl(c, snapDeclFoo, "foo_instance", snap.R(7))
	s.stateFromDecl(c, snapDeclBar, "", snap.R(3))

	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclBar)
	c.Assert(err, IsNil)

	fooInstanceRefresh := &snap.Info{
		SideInfo:    snap.SideInfo{RealName: "foo", SnapID: "foo-id", Revision: snap.R(9)},
		InstanceKey: "instance",
	}

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooInstanceRefresh}, nil, 0, s.trivialDeviceCtx)
	c.Assert(err, ErrorMatches, `cannot refresh "foo_instance" to revision 9: no validation by "bar"`)
	c.Check(validated, HasLen, 0)
}

func (s *assertMgrSuite) TestValidateRefreshesMissingValidationButIgnore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", map[string]interface{}{
		"refresh-control": []interface{}{"foo-id"},
	})
	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))
	s.stateFromDecl(c, snapDeclBar, "", snap.R(3))

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

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooRefresh}, map[string]bool{"foo": true}, 0, s.trivialDeviceCtx)
	c.Assert(err, IsNil)
	c.Check(validated, DeepEquals, []*snap.Info{fooRefresh})
}

func (s *assertMgrSuite) TestParallelInstanceValidateRefreshesMissingValidationButIgnore(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", map[string]interface{}{
		"refresh-control": []interface{}{"foo-id"},
	})
	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))
	s.stateFromDecl(c, snapDeclFoo, "foo_instance", snap.R(7))
	s.stateFromDecl(c, snapDeclBar, "", snap.R(3))

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
	fooInstanceRefresh := &snap.Info{
		SideInfo:    snap.SideInfo{RealName: "foo", SnapID: "foo-id", Revision: snap.R(9)},
		InstanceKey: "instance",
	}

	// validation is ignore for foo_instance but not for foo
	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooRefresh, fooInstanceRefresh}, map[string]bool{"foo_instance": true}, 0, s.trivialDeviceCtx)
	c.Assert(err, ErrorMatches, `cannot refresh "foo" to revision 9: no validation by "bar"`)
	c.Check(validated, DeepEquals, []*snap.Info{fooInstanceRefresh})
}

func (s *assertMgrSuite) TestParallelInstanceValidateRefreshesMissingValidationButIgnoreInstanceKeyed(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", map[string]interface{}{
		"refresh-control": []interface{}{"foo-id"},
	})
	s.stateFromDecl(c, snapDeclFoo, "foo_instance", snap.R(7))
	s.stateFromDecl(c, snapDeclBar, "", snap.R(3))

	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclFoo)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDeclBar)
	c.Assert(err, IsNil)

	fooInstanceRefresh := &snap.Info{
		SideInfo:    snap.SideInfo{RealName: "foo", SnapID: "foo-id", Revision: snap.R(9)},
		InstanceKey: "instance",
	}

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooInstanceRefresh}, map[string]bool{"foo_instance": true}, 0, s.trivialDeviceCtx)
	c.Assert(err, IsNil)
	c.Check(validated, DeepEquals, []*snap.Info{fooInstanceRefresh})
}

func (s *assertMgrSuite) TestParallelInstanceValidateRefreshesMissingValidationButIgnoreBothOneIgnored(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapDeclFoo := s.snapDecl(c, "foo", nil)
	snapDeclBar := s.snapDecl(c, "bar", map[string]interface{}{
		"refresh-control": []interface{}{"foo-id"},
	})
	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))
	s.stateFromDecl(c, snapDeclFoo, "foo_instance", snap.R(7))
	s.stateFromDecl(c, snapDeclBar, "", snap.R(3))

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
	fooInstanceRefresh := &snap.Info{
		SideInfo:    snap.SideInfo{RealName: "foo", SnapID: "foo-id", Revision: snap.R(9)},
		InstanceKey: "instance",
	}

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooRefresh, fooInstanceRefresh}, map[string]bool{"foo_instance": true}, 0, s.trivialDeviceCtx)
	c.Assert(err, ErrorMatches, `cannot refresh "foo" to revision 9: no validation by "bar"`)
	c.Check(validated, DeepEquals, []*snap.Info{fooInstanceRefresh})
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
	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))
	s.stateFromDecl(c, snapDeclFoo, "foo_instance", snap.R(7))
	s.stateFromDecl(c, snapDeclBar, "", snap.R(3))
	s.stateFromDecl(c, snapDeclBaz, "", snap.R(1))
	snapstate.Set(s.state, "local", &snapstate.SnapState{
		Active: false,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "local", Revision: snap.R(-1)},
		}),
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
	fooInstanceRefresh := &snap.Info{
		SideInfo:    snap.SideInfo{RealName: "foo", SnapID: "foo-id", Revision: snap.R(9)},
		InstanceKey: "instance",
	}

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooRefresh, fooInstanceRefresh}, nil, 0, s.trivialDeviceCtx)
	c.Assert(err, IsNil)
	c.Check(validated, DeepEquals, []*snap.Info{fooRefresh, fooInstanceRefresh})
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
	s.stateFromDecl(c, snapDeclFoo, "", snap.R(7))
	s.stateFromDecl(c, snapDeclBar, "", snap.R(3))
	s.stateFromDecl(c, snapDeclBaz, "", snap.R(1))
	snapstate.Set(s.state, "local", &snapstate.SnapState{
		Active: false,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "local", Revision: snap.R(-1)},
		}),
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

	validated, err := assertstate.ValidateRefreshes(s.state, []*snap.Info{fooRefresh}, nil, 0, s.trivialDeviceCtx)
	c.Assert(err, ErrorMatches, `(?s).*cannot refresh "foo" to revision 9: validation by "baz" \(id "baz-id"\) revoked.*`)
	c.Check(validated, HasLen, 0)
}

func (s *assertMgrSuite) TestBaseSnapDeclaration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	r1 := assertstest.MockBuiltinBaseDeclaration(nil)
	defer r1()

	baseDecl, err := assertstate.BaseDeclaration(s.state)
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
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
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

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
			map[string]interface{}{
				"name":   "alias-missing",
				"target": "cmd-missing",
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
		Apps: map[string]*snap.AppInfo{
			"cmd1": {},
			"cmd2": {},
			// no cmd-missing
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
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	acct, err := assertstate.Publisher(s.state, "foo-id")
	c.Assert(err, IsNil)
	c.Check(acct.AccountID(), Equals, s.dev1Acct.AccountID())
	c.Check(acct.Username(), Equals, "developer1")
}

func (s *assertMgrSuite) TestPublisherStoreAccount(c *C) {
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
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	acct, err := assertstate.PublisherStoreAccount(s.state, "foo-id")
	c.Assert(err, IsNil)
	c.Check(acct.ID, Equals, s.dev1Acct.AccountID())
	c.Check(acct.Username, Equals, "developer1")
	c.Check(acct.DisplayName, Equals, "Developer1")
	c.Check(acct.Validation, Equals, s.dev1Acct.Validation())
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
	c.Check(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	store, err := assertstate.Store(s.state, "foo")
	c.Assert(err, IsNil)
	c.Check(store.Store(), Equals, "foo")
}

// validation-sets related tests

func (s *assertMgrSuite) TestRefreshValidationSetAssertionsNop(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.setModel(sysdb.GenericClassicModel())

	err := assertstate.RefreshValidationSetAssertions(s.state, 0, nil)
	c.Assert(err, IsNil)
}

func (s *assertMgrSuite) TestValidationSetAssertionsAutoRefresh(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)

	// store key already present
	c.Assert(assertstate.Add(s.state, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1AcctKey), IsNil)

	vsetAs1 := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(assertstate.Add(s.state, vsetAs1), IsNil)

	vsetAs2 := s.validationSetAssert(c, "bar", "2", "3", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	c.Assert(assertstate.AutoRefreshAssertions(s.state, 0), IsNil)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, true)

	a, err := assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
	c.Check(a.Revision(), Equals, 3)
}

func (s *assertMgrSuite) TestValidationSetAssertionsAutoRefreshError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)
	err := assertstate.AutoRefreshAssertions(s.state, 0)
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (s *assertMgrSuite) TestRefreshValidationSetAssertionsStoreError(c *C) {
	s.fakeStore.(*fakeStore).snapActionErr = &store.UnexpectedHTTPStatusError{StatusCode: 400}
	s.state.Lock()
	defer s.state.Unlock()

	s.setModel(sysdb.GenericClassicModel())

	// store key already present
	c.Assert(assertstate.Add(s.state, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1AcctKey), IsNil)

	vsetAs1 := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(assertstate.Add(s.state, vsetAs1), IsNil)

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	err := assertstate.RefreshValidationSetAssertions(s.state, 0, nil)
	c.Assert(err, ErrorMatches, `cannot refresh validation set assertions: cannot : got unexpected HTTP status code 400.*`)
}

func (s *assertMgrSuite) TestRefreshValidationSetAssertions(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	// store key already present
	c.Assert(assertstate.Add(s.state, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1AcctKey), IsNil)

	vsetAs1 := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(assertstate.Add(s.state, vsetAs1), IsNil)

	vsetAs2 := s.validationSetAssert(c, "bar", "1", "2", "required", "1")
	err = s.storeSigning.Add(vsetAs2)
	c.Assert(err, IsNil)

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	err = assertstate.RefreshValidationSetAssertions(s.state, 0, &assertstate.RefreshAssertionsOptions{IsAutoRefresh: true})
	c.Assert(err, IsNil)

	a, err := assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "1",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.ValidationSet).Name(), Equals, "bar")
	c.Check(a.Revision(), Equals, 2)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		{"account", "account-key", "validation-set"},
	})
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, true)

	// sequence changed in the store to 4
	vsetAs3 := s.validationSetAssert(c, "bar", "4", "3", "required", "1")
	err = s.storeSigning.Add(vsetAs3)
	c.Assert(err, IsNil)

	// precondition check - sequence 4 not available locally yet
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "4",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	s.fakeStore.(*fakeStore).requestedTypes = nil
	err = assertstate.RefreshValidationSetAssertions(s.state, 0, nil)
	c.Assert(err, IsNil)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		{"account", "account-key", "validation-set"},
	})

	// new sequence is available in the db
	a, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "4",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.ValidationSet).Name(), Equals, "bar")

	// tracking current was updated
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)
	c.Check(tr.Current, Equals, 4)
}

func (s *assertMgrSuite) TestRefreshValidationSetAssertionsPinned(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	// store key already present
	err = assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	c.Assert(assertstate.Add(s.state, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1AcctKey), IsNil)

	vsetAs1 := s.validationSetAssert(c, "bar", "2", "1", "required", "1")
	c.Assert(assertstate.Add(s.state, vsetAs1), IsNil)

	vsetAs2 := s.validationSetAssert(c, "bar", "2", "5", "required", "1")
	err = s.storeSigning.Add(vsetAs2)
	c.Assert(err, IsNil)

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		Current:   2,
		PinnedAt:  2,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	err = assertstate.RefreshValidationSetAssertions(s.state, 0, nil)
	c.Assert(err, IsNil)

	a, err := assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.ValidationSet).Name(), Equals, "bar")
	c.Check(a.(*asserts.ValidationSet).Sequence(), Equals, 2)
	c.Check(a.Revision(), Equals, 5)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		{"account", "account-key", "validation-set"},
	})

	// sequence changed in the store to 7
	vsetAs3 := s.validationSetAssert(c, "bar", "7", "8", "required", "1")
	err = s.storeSigning.Add(vsetAs3)
	c.Assert(err, IsNil)

	s.fakeStore.(*fakeStore).requestedTypes = nil
	err = assertstate.RefreshValidationSetAssertions(s.state, 0, nil)
	c.Assert(err, IsNil)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		{"account", "account-key", "validation-set"},
	})

	// new sequence is not available in the db
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "7",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	// tracking current remains at 2
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)
	c.Check(tr.Current, Equals, 2)
}

func (s *assertMgrSuite) TestRefreshValidationSetAssertionsLocalOnlyFailed(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	// store key already present
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add to local database
	vsetAs1 := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(assertstate.Add(st, vsetAs1), IsNil)
	vsetAs2 := s.validationSetAssert(c, "baz", "3", "1", "required", "1")
	c.Assert(assertstate.Add(st, vsetAs2), IsNil)

	// vset2 present and updated in the store
	vsetAs2_2 := s.validationSetAssert(c, "baz", "3", "2", "required", "1")
	err = s.storeSigning.Add(vsetAs2_2)
	c.Assert(err, IsNil)

	tr1 := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		Current:   1,
		PinnedAt:  1,
		LocalOnly: true,
	}
	tr2 := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "baz",
		Mode:      assertstate.Monitor,
		Current:   3,
		PinnedAt:  3,
	}
	assertstate.UpdateValidationSet(s.state, &tr1)
	assertstate.UpdateValidationSet(s.state, &tr2)

	err = assertstate.RefreshValidationSetAssertions(s.state, 0, nil)
	c.Assert(err, IsNil)

	// precondition - local assertion vsetAs1 is the latest
	a, err := assertstate.DB(s.state).FindSequence(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "1",
	}, -1, -1)
	c.Assert(err, IsNil)
	vs := a.(*asserts.ValidationSet)
	c.Check(vs.Name(), Equals, "bar")
	c.Check(vs.Sequence(), Equals, 1)
	c.Check(vs.Revision(), Equals, 1)

	// but vsetAs2 was updated with vsetAs2_2
	a, err = assertstate.DB(s.state).FindSequence(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "baz",
		"sequence":   "1",
	}, -1, -1)
	c.Assert(err, IsNil)
	vs = a.(*asserts.ValidationSet)
	c.Check(vs.Name(), Equals, "baz")
	c.Check(vs.Sequence(), Equals, 3)
	c.Check(vs.Revision(), Equals, 2)
}

func (s *assertMgrSuite) TestRefreshValidationSetAssertionsEnforcingModeHappyNotPinned(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "foo", Revision: snap.R(1), SnapID: "qOqKhntON3vR7kwEbVPsILm7bUViPDzz"}}),
		Current:  snap.R(1),
	})
	snaptest.MockSnap(c, string(`name: foo
version: 1`), &snap.SideInfo{
		Revision: snap.R("1")})

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	// store key already present
	c.Assert(assertstate.Add(s.state, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1AcctKey), IsNil)

	vsetAs1 := s.validationSetAssert(c, "foo", "1", "1", "required", "1")
	c.Assert(assertstate.Add(s.state, vsetAs1), IsNil)

	vsetAs2 := s.validationSetAssert(c, "bar", "1", "2", "required", "1")
	c.Assert(assertstate.Add(s.state, vsetAs2), IsNil)

	// in the store
	vsetAs3 := s.validationSetAssert(c, "foo", "1", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs3), IsNil)

	vsetAs4 := s.validationSetAssert(c, "bar", "2", "3", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs4), IsNil)

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Enforce,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)
	tr = assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	err = assertstate.RefreshValidationSetAssertions(s.state, 0, nil)
	c.Assert(err, IsNil)

	a, err := assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "foo",
		"sequence":   "1",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.ValidationSet).Name(), Equals, "foo")
	c.Check(a.Revision(), Equals, 2)

	a, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.ValidationSet).Name(), Equals, "bar")
	c.Check(a.(*asserts.ValidationSet).Sequence(), Equals, 2)
	c.Check(a.Revision(), Equals, 3)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		{"account", "account-key", "validation-set"},
	})

	// tracking current was updated
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)
	c.Check(tr.Current, Equals, 2)
}

func (s *assertMgrSuite) TestRefreshValidationSetAssertionsEnforcingModeHappyPinned(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active:   true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{{RealName: "foo", Revision: snap.R(1), SnapID: "qOqKhntON3vR7kwEbVPsILm7bUViPDzz"}}),
		Current:  snap.R(1),
	})
	snaptest.MockSnap(c, string(`name: foo
version: 1`), &snap.SideInfo{Revision: snap.R("1")})

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	// store key already present
	c.Assert(assertstate.Add(s.state, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1AcctKey), IsNil)

	vsetAs1 := s.validationSetAssert(c, "bar", "1", "2", "required", "1")
	c.Assert(assertstate.Add(s.state, vsetAs1), IsNil)

	// in the store
	c.Assert(s.storeSigning.Add(vsetAs1), IsNil)

	vsetAs2 := s.validationSetAssert(c, "bar", "2", "3", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  1,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	c.Assert(assertstate.RefreshValidationSetAssertions(s.state, 0, nil), IsNil)

	a, err := assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "1",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.ValidationSet).Name(), Equals, "bar")
	c.Check(a.(*asserts.ValidationSet).Sequence(), Equals, 1)
	c.Check(a.Revision(), Equals, 2)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		{"account", "account-key", "validation-set"},
	})

	// tracking current was updated
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)
	c.Check(tr.Current, Equals, 1)
}

func (s *assertMgrSuite) TestRefreshValidationSetAssertionsEnforcingModeConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	logbuf, restore := logger.MockLogger()
	defer restore()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	// store key already present
	c.Assert(assertstate.Add(s.state, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1AcctKey), IsNil)

	vsetAs1 := s.validationSetAssert(c, "foo", "1", "1", "required", "1")
	c.Assert(assertstate.Add(s.state, vsetAs1), IsNil)

	vsetAs2 := s.validationSetAssert(c, "bar", "1", "2", "required", "1")
	c.Assert(assertstate.Add(s.state, vsetAs2), IsNil)

	// in the store
	vsetAs3 := s.validationSetAssert(c, "foo", "2", "2", "invalid", "")
	c.Assert(s.storeSigning.Add(vsetAs3), IsNil)

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Enforce,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)
	tr = assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	c.Assert(assertstate.RefreshValidationSetAssertions(s.state, 0, nil), IsNil)
	c.Assert(logbuf.String(), Matches, `.*cannot refresh to conflicting validation set assertions: validation sets are in conflict:\n- cannot constrain snap "foo" as both invalid .* and required at revision 1.*\n`)

	a, err := assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "foo",
		"sequence":   "1",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.ValidationSet).Name(), Equals, "foo")
	c.Check(a.Revision(), Equals, 1)

	// new assertion wasn't committed to the database.
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "foo",
		"sequence":   "2",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		{"account", "account-key", "validation-set"},
	})

	// tracking current wasn't updated
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "foo", &tr), IsNil)
	c.Check(tr.Current, Equals, 1)
}

func (s *assertMgrSuite) TestRefreshValidationSetAssertionsEnforcingModeMissingSnap(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	logbuf, restore := logger.MockLogger()
	defer restore()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	// store key already present
	c.Assert(assertstate.Add(s.state, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1AcctKey), IsNil)

	// currently tracked, but snap is not installed (it's optional)
	vsetAs1 := s.validationSetAssert(c, "foo", "1", "1", "optional", "1")
	c.Assert(assertstate.Add(s.state, vsetAs1), IsNil)

	// in the store, snap is now required
	vsetAs3 := s.validationSetAssert(c, "foo", "2", "2", "required", "")
	c.Assert(s.storeSigning.Add(vsetAs3), IsNil)

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Enforce,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	c.Assert(assertstate.RefreshValidationSetAssertions(s.state, 0, nil), IsNil)
	c.Assert(logbuf.String(), Matches, `.*cannot refresh to validation set assertions that do not satisfy installed snaps: validation sets assertions are not met:\n- missing required snaps:\n  - foo \(required at any revision by sets .*/foo\)\n`)

	a, err := assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "foo",
		"sequence":   "1",
	})
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.ValidationSet).Name(), Equals, "foo")
	c.Check(a.Revision(), Equals, 1)

	// new assertion wasn't committed to the database.
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "foo",
		"sequence":   "2",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		{"account", "account-key", "validation-set"},
	})

	// tracking current wasn't updated
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "foo", &tr), IsNil)
	c.Check(tr.Current, Equals, 1)
}

func (s *assertMgrSuite) TestRefreshValidationSetAssertionsEnforcingModeWrongSnapRevisionOK(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	// store key already present
	c.Assert(assertstate.Add(s.state, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1AcctKey), IsNil)

	snapstate.Set(s.state, "foo", &snapstate.SnapState{
		Active: false,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: "foo", Revision: snap.R(1)},
		}),
		Current: snap.R(1),
	})

	// snap revision 1 is installed
	vsetAs1 := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(assertstate.Add(s.state, vsetAs1), IsNil)

	// in the store, revision 2 required
	vsetAs3 := s.validationSetAssert(c, "bar", "2", "2", "required", "2")
	c.Assert(s.storeSigning.Add(vsetAs3), IsNil)

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   1,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	c.Assert(assertstate.RefreshValidationSetAssertions(s.state, 0, nil), IsNil)

	// new assertion has been committed to the database.
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)

	c.Check(s.fakeStore.(*fakeStore).requestedTypes, DeepEquals, [][]string{
		{"account", "account-key", "validation-set"},
	})

	// tracking current has been updated
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)
	c.Check(tr.Current, Equals, 2)
}

func (s *assertMgrSuite) TestValidationSetAssertionForMonitorLocalFallbackForPinned(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add to local database
	vsetAs := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(assertstate.Add(st, vsetAs), IsNil)

	opts := assertstate.ResolveOptions{AllowLocalFallback: true}
	vs, local, err := assertstate.ValidationSetAssertionForMonitor(st, s.dev1Acct.AccountID(), "bar", 1, true, 0, &opts)
	c.Assert(err, IsNil)
	c.Assert(vs, NotNil)
	c.Assert(local, Equals, true)
}

func (s *assertMgrSuite) TestValidationSetAssertionForMonitorPinnedRefreshedFromStore(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add to local database
	vsetAs1 := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(assertstate.Add(st, vsetAs1), IsNil)

	// newer revision available in the store
	vsetAs2 := s.validationSetAssert(c, "bar", "1", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	vs, local, err := assertstate.ValidationSetAssertionForMonitor(st, s.dev1Acct.AccountID(), "bar", 1, true, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(local, Equals, false)
	c.Check(vs.Revision(), Equals, 2)
	c.Check(vs.Sequence(), Equals, 1)
}

func (s *assertMgrSuite) TestValidationSetAssertionForMonitorUnpinnedRefreshedFromStore(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add to local database
	vsetAs1 := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(assertstate.Add(st, vsetAs1), IsNil)

	// newer assertion available in the store
	vsetAs2 := s.validationSetAssert(c, "bar", "3", "1", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	vs, local, err := assertstate.ValidationSetAssertionForMonitor(st, s.dev1Acct.AccountID(), "bar", 0, false, 0, nil)
	c.Assert(err, IsNil)
	c.Assert(local, Equals, false)
	c.Check(vs.Revision(), Equals, 1)
	c.Check(vs.Sequence(), Equals, 3)
}

func (s *assertMgrSuite) TestValidationSetAssertionForMonitorUnpinnedNotFound(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)

	_, _, err := assertstate.ValidationSetAssertionForMonitor(st, s.dev1Acct.AccountID(), "bar", 0, false, 0, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot fetch and resolve assertions:\n - validation-set/16/%s/bar: validation-set assertion not found.*`, s.dev1Acct.AccountID()))
}

// Test for enforce mode

func (s *assertMgrSuite) TestValidationSetAssertionForEnforceNotPinnedHappy(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add sequence to the store
	vsetAs := s.validationSetAssert(c, "bar", "2", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)

	snaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("foo", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
		snapasserts.NewInstalledSnap("other", "ididididid", snap.Revision{N: 1}),
	}

	sequence := 0
	vs, err := assertstate.ValidationSetAssertionForEnforce(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, nil)
	c.Assert(err, IsNil)
	c.Check(vs.Revision(), Equals, 2)
	c.Check(vs.Sequence(), Equals, 2)

	// and it has been committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
}

func (s *assertMgrSuite) TestValidationSetAssertionForEnforcePinnedHappy(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add sequence to the store
	vsetAs := s.validationSetAssert(c, "bar", "2", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)

	snaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("foo", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
	}

	sequence := 2
	vs, err := assertstate.ValidationSetAssertionForEnforce(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, nil)
	c.Assert(err, IsNil)
	c.Check(vs.Revision(), Equals, 2)
	c.Check(vs.Sequence(), Equals, 2)

	// and it has been committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, false)
}

func (s *assertMgrSuite) TestValidationSetAssertionForEnforceNotPinnedUnhappyMissingSnap(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add sequence to the store
	vsetAs := s.validationSetAssert(c, "bar", "2", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)

	snaps := []*snapasserts.InstalledSnap{}
	sequence := 0
	_, err := assertstate.ValidationSetAssertionForEnforce(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, nil)
	c.Assert(err, NotNil)
	verr, ok := err.(*snapasserts.ValidationSetsValidationError)
	c.Assert(ok, Equals, true)
	c.Check(verr.MissingSnaps, DeepEquals, map[string]map[snap.Revision][]string{
		"foo": {
			snap.R(1): []string{fmt.Sprintf("%s/bar", s.dev1Acct.AccountID())},
		},
	})

	// and it hasn't been committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (s *assertMgrSuite) TestValidationSetAssertionForEnforceNotPinnedUnhappyConflict(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add an assertion to local database
	vsetAs := s.validationSetAssert(c, "boo", "4", "4", "invalid", "")
	c.Assert(assertstate.Add(st, vsetAs), IsNil)
	// and to the store (for refresh to be happy)
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)

	// and pretend it was tracked already
	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "boo",
		Mode:      assertstate.Enforce,
		Current:   4,
	}
	assertstate.UpdateValidationSet(st, &tr)

	// add sequence to the store, it conflicts with boo
	vsetAs2 := s.validationSetAssert(c, "bar", "2", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	snaps := []*snapasserts.InstalledSnap{}
	sequence := 0
	_, err := assertstate.ValidationSetAssertionForEnforce(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, nil)
	c.Check(err, ErrorMatches, fmt.Sprintf(`validation sets are in conflict:\n- cannot constrain snap "foo" as both invalid \(%s/boo\) and required at revision 1 \(%s/bar\)`, s.dev1Acct.AccountID(), s.dev1Acct.AccountID()))

	// and it hasn't been committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (s *assertMgrSuite) TestValidationSetAssertionForEnforceNotPinnedAfterForgetHappy(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add an old assertion to local database; it's not tracked which is the
	// case after 'snap validate --forget' (we don't prune assertions from db).
	vsetAs1 := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(assertstate.Add(st, vsetAs1), IsNil)

	// newer sequence available in the store
	vsetAs2 := s.validationSetAssert(c, "bar", "3", "5", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	snaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("foo", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
	}

	sequence := 0
	vs, err := assertstate.ValidationSetAssertionForEnforce(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, nil)
	c.Assert(err, IsNil)
	// new assertion got fetched
	c.Check(vs.Revision(), Equals, 5)
	c.Check(vs.Sequence(), Equals, 3)

	// and it has been committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "3",
	})
	c.Assert(err, IsNil)
}

func (s *assertMgrSuite) TestValidationSetAssertionForEnforceNotPinnedAfterMonitorHappy(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add and old assertion to local database
	vsetAs1 := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(assertstate.Add(st, vsetAs1), IsNil)

	// and pretend it was tracked already in monitor mode
	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		Current:   1,
	}
	assertstate.UpdateValidationSet(st, &tr)

	// newer sequence available in the store
	vsetAs2 := s.validationSetAssert(c, "bar", "3", "5", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	snaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("foo", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
	}

	sequence := 0
	vs, err := assertstate.ValidationSetAssertionForEnforce(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, nil)
	c.Assert(err, IsNil)
	// doesn't fetch new assertion
	c.Check(vs.Revision(), Equals, 1)
	c.Check(vs.Sequence(), Equals, 1)

	// old assertion is stil in the database
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "1",
	})
	c.Assert(err, IsNil)
}

func (s *assertMgrSuite) TestTemporaryDB(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	err := assertstate.Add(st, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)

	a, err := s.storeSigning.Sign(asserts.ModelType, map[string]interface{}{
		"type":         "model",
		"series":       "16",
		"authority-id": s.storeSigning.AuthorityID,
		"brand-id":     s.storeSigning.AuthorityID,
		"model":        "my-model",
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "krnl",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)

	aRev2, err := s.storeSigning.Sign(asserts.ModelType, map[string]interface{}{
		"type":         "model",
		"series":       "16",
		"authority-id": s.storeSigning.AuthorityID,
		"brand-id":     s.storeSigning.AuthorityID,
		"model":        "my-model",
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "krnl",
		"timestamp":    time.Now().Format(time.RFC3339),
		"revision":     "2",
	}, nil, "")
	c.Assert(err, IsNil)
	modelRev2 := aRev2.(*asserts.Model)

	hdrs := map[string]string{
		"series":   "16",
		"model":    "my-model",
		"brand-id": s.storeSigning.AuthorityID,
	}
	// model isn't found in the main DB
	_, err = assertstate.DB(st).Find(asserts.ModelType, hdrs)
	c.Assert(err, NotNil)
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
	// let's get a temporary DB
	tempDB := assertstate.TemporaryDB(st)
	c.Assert(tempDB, NotNil)
	// and add the model to it
	err = tempDB.Add(model)
	c.Assert(err, IsNil)
	fromTemp, err := tempDB.Find(asserts.ModelType, hdrs)
	c.Assert(err, IsNil)
	c.Assert(fromTemp.(*asserts.Model), DeepEquals, model)
	// the model is only in the temp database
	_, err = assertstate.DB(st).Find(asserts.ModelType, hdrs)
	c.Assert(err, NotNil)
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)

	// let's add it to the DB now
	err = assertstate.Add(st, model)
	c.Assert(err, IsNil)
	// such that we can lookup the revision 2 in a temporary DB
	tempDB = assertstate.TemporaryDB(st)
	c.Assert(tempDB, NotNil)
	err = tempDB.Add(modelRev2)
	c.Assert(err, IsNil)
	fromTemp, err = tempDB.Find(asserts.ModelType, hdrs)
	c.Assert(err, IsNil)
	c.Assert(fromTemp.(*asserts.Model), DeepEquals, modelRev2)
	// but the main DB still returns the old model
	fromDB, err := assertstate.DB(st).Find(asserts.ModelType, hdrs)
	c.Assert(err, IsNil)
	c.Assert(fromDB.(*asserts.Model), DeepEquals, model)
}

func (s *assertMgrSuite) TestEnforceValidationSetAssertion(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add sequence to the store
	vsetAs := s.validationSetAssert(c, "bar", "2", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)

	snaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("foo", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
	}

	sequence := 2
	tracking, err := assertstate.FetchAndApplyEnforcedValidationSet(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, nil)
	c.Assert(err, IsNil)

	// and it has been committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, false)

	var tr assertstate.ValidationSetTracking
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)
	c.Check(tr, DeepEquals, *tracking)

	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  2,
		Current:   2,
	})

	// and it was added to the history
	vshist, err := assertstate.ValidationSetsHistory(st)
	c.Assert(err, IsNil)
	c.Check(vshist, DeepEquals, []map[string]*assertstate.ValidationSetTracking{{
		fmt.Sprintf("%s/bar", s.dev1Acct.AccountID()): {
			AccountID: s.dev1Acct.AccountID(),
			Name:      "bar",
			Mode:      assertstate.Enforce,
			PinnedAt:  2,
			Current:   2,
		},
	}})
}

func (s *assertMgrSuite) TestEnforceValidationSetAssertionUpdate(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add sequence to the store
	vsetAs := s.validationSetAssert(c, "bar", "2", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)

	snaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("foo", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
	}

	sequence := 2
	tracking, err := assertstate.FetchAndApplyEnforcedValidationSet(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, nil)
	c.Assert(err, IsNil)

	// and it has been committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, false)

	var tr assertstate.ValidationSetTracking
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)
	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  2,
		Current:   2,
	})
	c.Check(tr, DeepEquals, *tracking)

	// and it was added to the history
	vshist, err := assertstate.ValidationSetsHistory(st)
	c.Assert(err, IsNil)
	c.Check(vshist, DeepEquals, []map[string]*assertstate.ValidationSetTracking{{
		fmt.Sprintf("%s/bar", s.dev1Acct.AccountID()): {
			AccountID: s.dev1Acct.AccountID(),
			Name:      "bar",
			Mode:      assertstate.Enforce,
			PinnedAt:  2,
			Current:   2,
		},
	}})

	// not pinned
	sequence = 0
	tracking, err = assertstate.FetchAndApplyEnforcedValidationSet(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, nil)
	c.Assert(err, IsNil)

	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)
	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  0,
		Current:   2,
	})
	c.Check(tr, DeepEquals, *tracking)
}

func (s *assertMgrSuite) TestEnforceValidationSetAssertionPinToOlderSequence(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add sequences to the store
	vsetAs1 := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs1), IsNil)
	vsetAs2 := s.validationSetAssert(c, "bar", "2", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	snaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("foo", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
	}

	sequence := 2
	tracking, err := assertstate.FetchAndApplyEnforcedValidationSet(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, nil)
	c.Assert(err, IsNil)

	// and it has been committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, false)

	var tr assertstate.ValidationSetTracking
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)
	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  2,
		Current:   2,
	})
	c.Check(tr, DeepEquals, *tracking)

	// pin to older
	sequence = 1
	tracking, err = assertstate.FetchAndApplyEnforcedValidationSet(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, nil)
	c.Assert(err, IsNil)

	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)
	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  1,
		// and current points at the pinned sequence number
		Current: 1,
	})
	c.Check(tr, DeepEquals, *tracking)
}

func (s *assertMgrSuite) TestEnforceValidationSetAssertionAfterMonitor(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add and old assertion to local database
	vsetAs1 := s.validationSetAssert(c, "bar", "1", "1", "required", "1")
	c.Assert(assertstate.Add(st, vsetAs1), IsNil)

	monitor := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		Current:   1,
	}
	assertstate.UpdateValidationSet(st, &monitor)

	snaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("foo", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
	}

	// add a newer sequence to the store
	vsetAs := s.validationSetAssert(c, "bar", "2", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)

	sequence := 2
	tracking, err := assertstate.FetchAndApplyEnforcedValidationSet(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, nil)
	c.Assert(err, IsNil)

	// and it has been committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, false)

	var tr assertstate.ValidationSetTracking
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)

	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  2,
		Current:   2,
	})
	c.Check(tr, DeepEquals, *tracking)
}

func (s *assertMgrSuite) TestEnforceValidationSetAssertionIgnoreValidation(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add sequence to the store
	vsetAs := s.validationSetAssert(c, "bar", "2", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)

	snaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("foo", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 3}),
	}

	sequence := 2
	ignoreValidation := map[string]bool{}
	_, err := assertstate.FetchAndApplyEnforcedValidationSet(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, ignoreValidation)
	wrongRevErr, ok := err.(*snapasserts.ValidationSetsValidationError)
	c.Assert(ok, Equals, true)
	c.Check(wrongRevErr.WrongRevisionSnaps["foo"], NotNil)

	ignoreValidation["foo"] = true
	tracking, err := assertstate.FetchAndApplyEnforcedValidationSet(st, s.dev1Acct.AccountID(), "bar", sequence, 0, snaps, ignoreValidation)
	c.Assert(err, IsNil)

	// and it has been committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, false)

	var tr assertstate.ValidationSetTracking
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)

	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  2,
		Current:   2,
	})
	c.Check(tr, DeepEquals, *tracking)
}

func (s *assertMgrSuite) TestTryEnforceValidationSetsAssertionsValidationError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// pretend we are already enforcing a validation set foo
	snaps3 := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
		}}
	vsetAs3 := s.validationSetAssertForSnaps(c, "foo", "1", "1", snaps3)
	c.Assert(assertstate.Add(st, vsetAs3), IsNil)
	assertstate.UpdateValidationSet(st, &assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Enforce,
		Current:   1,
	})

	// add validation set assertions to the store
	snaps1 := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
			"revision": "3",
		},
		map[string]interface{}{
			"id":       "aAqKhntON3vR7kwEbVPsILm7bUViPDaa",
			"name":     "other-snap",
			"presence": "required",
		}}
	vsetAs := s.validationSetAssertForSnaps(c, "bar", "2", "2", snaps1)
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)
	snaps2 := []interface{}{
		map[string]interface{}{
			"id":       "cccchntON3vR7kwEbVPsILm7bUViPDcc",
			"name":     "invalid-snap",
			"presence": "invalid",
		}}
	vsetAs2 := s.validationSetAssertForSnaps(c, "baz", "1", "1", snaps2)
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	// try to enforce extra validation sets bar and baz. some-snap is present (and required by foo at any revision),
	// but needs to be at revision 3 to satisfy bar. invalid-snap is present but is invalid for baz.
	installedSnaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("some-snap", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
		snapasserts.NewInstalledSnap("invalid-snap", "cccchntON3vR7kwEbVPsILm7bUViPDcc", snap.Revision{N: 1}),
	}
	err := assertstate.TryEnforcedValidationSets(st, []string{fmt.Sprintf("%s/bar", s.dev1Acct.AccountID()), fmt.Sprintf("%s/baz", s.dev1Acct.AccountID())}, 0, installedSnaps, nil)
	verr, ok := err.(*snapasserts.ValidationSetsValidationError)
	c.Assert(ok, Equals, true)
	c.Check(verr.WrongRevisionSnaps, DeepEquals, map[string]map[snap.Revision][]string{
		"some-snap": {
			snap.R(3): []string{fmt.Sprintf("%s/bar", s.dev1Acct.AccountID())},
		},
	})
	c.Check(verr.MissingSnaps, DeepEquals, map[string]map[snap.Revision][]string{
		"other-snap": {
			snap.R(0): []string{fmt.Sprintf("%s/bar", s.dev1Acct.AccountID())},
		},
	})
	c.Check(verr.InvalidSnaps, DeepEquals, map[string][]string{"invalid-snap": {fmt.Sprintf("%s/baz", s.dev1Acct.AccountID())}})

	// new assertions were not committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "baz",
		"sequence":   "1",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, false)
}

func (s *assertMgrSuite) TestTryEnforceValidationSetsAssertionsOK(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// pretend we are already enforcing a validation set foo
	snaps3 := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
		}}
	vsetAs3 := s.validationSetAssertForSnaps(c, "foo", "1", "1", snaps3)
	c.Assert(assertstate.Add(st, vsetAs3), IsNil)
	assertstate.UpdateValidationSet(st, &assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Enforce,
		Current:   1,
	})

	// add validation set assertions to the store
	snaps1 := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
			"revision": "3",
		}}
	vsetAs := s.validationSetAssertForSnaps(c, "bar", "2", "2", snaps1)
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)
	snaps2 := []interface{}{
		map[string]interface{}{
			"id":       "aAqKhntON3vR7kwEbVPsILm7bUViPDaa",
			"name":     "other-snap",
			"presence": "optional",
		}}
	vsetAs2 := s.validationSetAssertForSnaps(c, "baz", "1", "1", snaps2)
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	installedSnaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("some-snap", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 3}),
	}
	err := assertstate.TryEnforcedValidationSets(st, []string{fmt.Sprintf("%s/bar", s.dev1Acct.AccountID()), fmt.Sprintf("%s/baz=1", s.dev1Acct.AccountID())}, 0, installedSnaps, nil)
	c.Assert(err, IsNil)

	// new assertions were committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)

	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "baz",
		"sequence":   "1",
	})
	c.Assert(err, IsNil)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, false)

	// tracking was updated
	var tr assertstate.ValidationSetTracking
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)
	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		Current:   2,
	})
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "baz", &tr), IsNil)
	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "baz",
		Mode:      assertstate.Enforce,
		Current:   1,
		PinnedAt:  1,
	})

	// and it was added to the history
	// note, normally there would be a map with just "foo" as well, but there isn't one
	// since we created the initial state in the test manually.
	vshist, err := assertstate.ValidationSetsHistory(st)
	c.Assert(err, IsNil)
	c.Check(vshist, DeepEquals, []map[string]*assertstate.ValidationSetTracking{{
		fmt.Sprintf("%s/foo", s.dev1Acct.AccountID()): {
			AccountID: s.dev1Acct.AccountID(),
			Name:      "foo",
			Mode:      assertstate.Enforce,
			Current:   1,
		},
		fmt.Sprintf("%s/bar", s.dev1Acct.AccountID()): {
			AccountID: s.dev1Acct.AccountID(),
			Name:      "bar",
			Mode:      assertstate.Enforce,
			Current:   2,
		},
		fmt.Sprintf("%s/baz", s.dev1Acct.AccountID()): {
			AccountID: s.dev1Acct.AccountID(),
			Name:      "baz",
			Mode:      assertstate.Enforce,
			PinnedAt:  1,
			Current:   1,
		},
	}})
}

func (s *assertMgrSuite) TestTryEnforceValidationSetsAssertionsAlreadyTrackedUpdateOK(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// pretend we are already enforcing a validation set foo
	snaps3 := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
		}}
	vsetAs1 := s.validationSetAssertForSnaps(c, "foo", "1", "1", snaps3)
	c.Assert(assertstate.Add(st, vsetAs1), IsNil)
	assertstate.UpdateValidationSet(st, &assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Enforce,
		Current:   1,
	})

	// add validation set assertions to the store
	snaps1 := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
			"revision": "3",
		}}
	vsetAs2 := s.validationSetAssertForSnaps(c, "foo", "2", "2", snaps1)
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	installedSnaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("some-snap", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 3}),
	}
	err := assertstate.TryEnforcedValidationSets(st, []string{fmt.Sprintf("%s/foo", s.dev1Acct.AccountID())}, 0, installedSnaps, nil)
	c.Assert(err, IsNil)

	// new assertion was committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "foo",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, false)

	// tracking was updated
	var tr assertstate.ValidationSetTracking
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "foo", &tr), IsNil)
	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Enforce,
		Current:   2,
	})

	// and it was added to the history
	// note, normally there would be a map with just "foo" as well, but there isn't one
	// since we created the initial state in the test manually.
	vshist, err := assertstate.ValidationSetsHistory(st)
	c.Assert(err, IsNil)
	c.Check(vshist, DeepEquals, []map[string]*assertstate.ValidationSetTracking{{
		fmt.Sprintf("%s/foo", s.dev1Acct.AccountID()): {
			AccountID: s.dev1Acct.AccountID(),
			Name:      "foo",
			Mode:      assertstate.Enforce,
			Current:   2,
		},
	}})
}

func (s *assertMgrSuite) TestTryEnforceValidationSetsAssertionsConflictError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// pretend we are already enforcing a validation set foo
	snaps3 := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
		}}
	vsetAs3 := s.validationSetAssertForSnaps(c, "foo", "1", "1", snaps3)
	c.Assert(assertstate.Add(st, vsetAs3), IsNil)
	assertstate.UpdateValidationSet(st, &assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Enforce,
		Current:   1,
	})

	// add a validation set assertion to the store
	snaps1 := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "invalid",
		}}
	vsetAs := s.validationSetAssertForSnaps(c, "bar", "2", "2", snaps1)
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)

	// try to enforce extra validation sets bar and baz. some-snap is present (and required by foo at any revision),
	// but needs to be at revision 3 to satisfy bar. invalid-snap is present but is invalid for baz.
	installedSnaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("some-snap", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
	}
	err := assertstate.TryEnforcedValidationSets(st, []string{fmt.Sprintf("%s/bar", s.dev1Acct.AccountID())}, 0, installedSnaps, nil)
	_, ok := err.(*snapasserts.ValidationSetsConflictError)
	c.Assert(ok, Equals, true)
	c.Assert(err, ErrorMatches, `validation sets are in conflict:\n- cannot constrain snap "some-snap" as both invalid \(.*/bar\) and required at any revision \(.*/foo\).*`)

	// new assertion wasn't committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, false)
}

func (s *assertMgrSuite) TestMonitorValidationSet(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add to the store
	vsetAs := s.validationSetAssert(c, "bar", "2", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)

	sequence := 2
	tr1, err := assertstate.MonitorValidationSet(st, s.dev1Acct.AccountID(), "bar", sequence, 0)
	c.Assert(err, IsNil)
	c.Check(tr1, DeepEquals, &assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		PinnedAt:  2,
		Current:   2,
	})

	// and it has been committed
	_, err = assertstate.DB(s.state).Find(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "bar",
		"sequence":   "2",
	})
	c.Assert(err, IsNil)
	c.Check(s.fakeStore.(*fakeStore).opts.Scheduled, Equals, false)

	var tr assertstate.ValidationSetTracking
	c.Assert(assertstate.GetValidationSet(s.state, s.dev1Acct.AccountID(), "bar", &tr), IsNil)

	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		PinnedAt:  2,
		Current:   2,
	})

	// and it was added to the history
	vshist, err := assertstate.ValidationSetsHistory(st)
	c.Assert(err, IsNil)
	c.Check(vshist, DeepEquals, []map[string]*assertstate.ValidationSetTracking{{
		fmt.Sprintf("%s/bar", s.dev1Acct.AccountID()): {
			AccountID: s.dev1Acct.AccountID(),
			Name:      "bar",
			Mode:      assertstate.Monitor,
			PinnedAt:  2,
			Current:   2,
		},
	}})
}

func (s *assertMgrSuite) TestForgetValidationSet(c *C) {
	st := s.state

	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// add to the store
	vsetAs1 := s.validationSetAssert(c, "bar", "2", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs1), IsNil)

	vsetAs2 := s.validationSetAssert(c, "baz", "2", "2", "required", "1")
	c.Assert(s.storeSigning.Add(vsetAs2), IsNil)

	tr1, err := assertstate.MonitorValidationSet(st, s.dev1Acct.AccountID(), "bar", 2, 0)
	c.Assert(err, IsNil)
	c.Check(tr1, DeepEquals, &assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		PinnedAt:  2,
		Current:   2,
	})

	tr2, err := assertstate.MonitorValidationSet(st, s.dev1Acct.AccountID(), "baz", 2, 0)
	c.Assert(err, IsNil)
	c.Check(tr2, DeepEquals, &assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "baz",
		Mode:      assertstate.Monitor,
		PinnedAt:  2,
		Current:   2,
	})

	c.Assert(assertstate.ForgetValidationSet(st, s.dev1Acct.AccountID(), "bar", assertstate.ForgetValidationSetOpts{}), IsNil)

	// and it was added to the history
	vshist, err := assertstate.ValidationSetsHistory(st)
	c.Assert(err, IsNil)
	c.Check(vshist, DeepEquals, []map[string]*assertstate.ValidationSetTracking{{
		fmt.Sprintf("%s/bar", s.dev1Acct.AccountID()): {
			AccountID: s.dev1Acct.AccountID(),
			Name:      "bar",
			Mode:      assertstate.Monitor,
			PinnedAt:  2,
			Current:   2,
		},
	}, {
		fmt.Sprintf("%s/bar", s.dev1Acct.AccountID()): {
			AccountID: s.dev1Acct.AccountID(),
			Name:      "bar",
			Mode:      assertstate.Monitor,
			PinnedAt:  2,
			Current:   2,
		},
		fmt.Sprintf("%s/baz", s.dev1Acct.AccountID()): {
			AccountID: s.dev1Acct.AccountID(),
			Name:      "baz",
			Mode:      assertstate.Monitor,
			PinnedAt:  2,
			Current:   2,
		},
	}, {
		fmt.Sprintf("%s/baz", s.dev1Acct.AccountID()): {
			AccountID: s.dev1Acct.AccountID(),
			Name:      "baz",
			Mode:      assertstate.Monitor,
			PinnedAt:  2,
			Current:   2,
		},
	}})
}

func (s *assertMgrSuite) TestEnforceValidationSets(c *C) {
	s.testEnforceValidationSets(c, 0)
}

func (s *assertMgrSuite) TestEnforceValidationSetsWithPinning(c *C) {
	s.testEnforceValidationSets(c, 1)
}

func (s *assertMgrSuite) testEnforceValidationSets(c *C, pinnedSeq int) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// have a model and the store assertion available
	snapstate.ReplaceStore(st, s.fakeStore)
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)

	// validation set that we refreshed to enforce
	snaps := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
		}}
	localVs := s.validationSetAssertForSnaps(c, "foo", "1", "1", snaps)
	c.Assert(assertstate.Add(st, localVs), IsNil)

	// add a more recent conflicting version to the store
	snaps = []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "invalid",
		}}
	remoteVs := s.validationSetAssertForSnaps(c, "foo", "2", "1", snaps)
	c.Assert(s.storeSigning.Add(remoteVs), IsNil)

	requestedValSet := fmt.Sprintf("%s/foo", s.dev1Acct.AccountID())
	var pinnedSeqs map[string]int
	if pinnedSeq != 0 {
		pinnedSeqs = make(map[string]int, 1)
		pinnedSeqs[requestedValSet] = pinnedSeq
	}
	valSets := map[string]*asserts.ValidationSet{
		requestedValSet: localVs,
	}
	installedSnaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("some-snap", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
	}
	err := assertstate.ApplyEnforcedValidationSets(st, valSets, pinnedSeqs, installedSnaps, nil, 0)
	c.Assert(err, IsNil)

	// the updated assertion wasn't fetched
	valsetAssrt, err := assertstate.DB(s.state).FindSequence(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "foo",
	}, -1, -1)
	c.Assert(err, IsNil)
	c.Assert(valsetAssrt, FitsTypeOf, &asserts.ValidationSet{})
	c.Check(valsetAssrt.(*asserts.ValidationSet).Sequence(), Equals, 1)

	var tr assertstate.ValidationSetTracking
	err = assertstate.GetValidationSet(st, s.dev1Acct.AccountID(), "foo", &tr)
	c.Assert(err, IsNil)
	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Enforce,
		Current:   1,
		PinnedAt:  pinnedSeq,
	})
}

func (s *assertMgrSuite) TestEnforceValidationSetsWithNoLocalAssertions(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// nothing in the DB; only in the store
	snapstate.ReplaceStore(st, s.fakeStore)
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)

	// validation set that we refreshed to enforce
	snaps := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
		}}

	oldVs := s.validationSetAssertForSnaps(c, "foo", "1", "1", snaps)
	c.Assert(s.storeSigning.Add(oldVs), IsNil)

	// add a more recent conflicting version to the store which shouldn't be pulled
	snaps = []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "invalid",
		}}
	newVs := s.validationSetAssertForSnaps(c, "foo", "2", "1", snaps)
	c.Assert(s.storeSigning.Add(newVs), IsNil)

	valSets := map[string]*asserts.ValidationSet{
		fmt.Sprintf("%s/foo", s.dev1Acct.AccountID()): oldVs,
	}
	pinnedSeqs := map[string]int{fmt.Sprintf("%s/foo", s.dev1Acct.AccountID()): 1}
	installedSnaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("some-snap", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
	}

	err := assertstate.ApplyEnforcedValidationSets(st, valSets, pinnedSeqs, installedSnaps, nil, 0)
	c.Assert(err, IsNil)

	// the old assertion is in the state
	vsAssrt, err := assertstate.DB(s.state).FindSequence(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "foo",
	}, -1, -1)
	c.Assert(err, IsNil)
	c.Assert(vsAssrt, FitsTypeOf, &asserts.ValidationSet{})
	c.Check(vsAssrt.(*asserts.ValidationSet).Sequence(), Equals, 1)

	assrt, err := assertstate.DB(s.state).Find(asserts.AccountKeyType, map[string]string{
		"public-key-sha3-384": oldVs.SignKeyID(),
	})
	c.Assert(err, IsNil)
	c.Assert(assrt, FitsTypeOf, &asserts.AccountKey{})

	var tr assertstate.ValidationSetTracking
	err = assertstate.GetValidationSet(st, s.dev1Acct.AccountID(), "foo", &tr)
	c.Assert(err, IsNil)
	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Enforce,
		Current:   1,
		// the map key contains a sequence number so this should be pinned
		PinnedAt: 1,
	})
}

func (s *assertMgrSuite) TestEnforceValidationSetsWithMismatchedPinnedSeq(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// nothing in the DB; only in the store
	snapstate.ReplaceStore(st, s.fakeStore)
	s.setupModelAndStore(c)

	vs := s.validationSetAssertForSnaps(c, "foo", "1", "1", []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
		}})

	// user requested op with sequence 2 but we're passing a different sequence
	vsKey := fmt.Sprintf("%s/foo", s.dev1Acct.AccountID())
	valSets := map[string]*asserts.ValidationSet{
		vsKey: vs,
	}
	pinnedSeqs := map[string]int{vsKey: 2}

	err := assertstate.ApplyEnforcedValidationSets(st, valSets, pinnedSeqs, nil, nil, 0)
	c.Assert(err, ErrorMatches, fmt.Sprintf("internal error: trying to enforce validation set %q with sequence point 1 different than pinned 2", vsKey))
}

func (s *assertMgrSuite) TestEnforceValidationSetsWithUnmetConstraints(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	snapstate.ReplaceStore(st, s.fakeStore)
	storeAs := s.setupModelAndStore(c)
	c.Assert(s.storeSigning.Add(storeAs), IsNil)

	snaps := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
			"revision": "1",
		}}

	vs := s.validationSetAssertForSnaps(c, "foo", "1", "1", snaps)
	c.Assert(s.storeSigning.Add(vs), IsNil)

	valSets := map[string]*asserts.ValidationSet{
		fmt.Sprintf("%s/foo", s.dev1Acct.AccountID()): vs,
	}

	installedSnaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("some-snap", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 2}),
	}

	err := assertstate.ApplyEnforcedValidationSets(st, valSets, nil, installedSnaps, nil, 0)
	c.Assert(err, FitsTypeOf, &snapasserts.ValidationSetsValidationError{})

	_, err = assertstate.DB(s.state).FindSequence(asserts.ValidationSetType, map[string]string{
		"series":     "16",
		"account-id": s.dev1Acct.AccountID(),
		"name":       "foo",
	}, -1, -1)
	c.Assert(err, testutil.ErrorIs, &asserts.NotFoundError{})

	_, err = assertstate.DB(s.state).Find(asserts.AccountKeyType, map[string]string{
		"public-key-sha3-384": vs.SignKeyID(),
	})
	c.Assert(err, testutil.ErrorIs, &asserts.NotFoundError{})

	err = assertstate.GetValidationSet(st, s.dev1Acct.AccountID(), "foo", &assertstate.ValidationSetTracking{})
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})
}

func (s *assertMgrSuite) testApplyLocalEnforcedValidationSets(c *C, pinnedSeq int) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	// validation set that we refreshed to enforce
	snaps := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
		}}
	localVs := s.validationSetAssertForSnaps(c, "foo", "1", "1", snaps)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)
	c.Assert(assertstate.Add(st, localVs), IsNil)

	vsKey := fmt.Sprintf("%s/foo", s.dev1Acct.AccountID())
	var pinnedSeqs map[string]int
	if pinnedSeq != 0 {
		pinnedSeqs = make(map[string]int, 1)
		pinnedSeqs[vsKey] = pinnedSeq
	}
	valSets := map[string][]string{
		vsKey: {release.Series, s.dev1Acct.AccountID(), "foo", "1"},
	}
	installedSnaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("some-snap", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 1}),
	}
	err := assertstate.ApplyLocalEnforcedValidationSets(st, valSets, pinnedSeqs, installedSnaps, nil)
	c.Assert(err, IsNil)

	var tr assertstate.ValidationSetTracking
	err = assertstate.GetValidationSet(st, s.dev1Acct.AccountID(), "foo", &tr)
	c.Assert(err, IsNil)
	c.Check(tr, DeepEquals, assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Enforce,
		Current:   1,
		PinnedAt:  pinnedSeq,
	})
}

func (s *assertMgrSuite) TestApplyLocalEnforcedValidationSets(c *C) {
	s.testApplyLocalEnforcedValidationSets(c, 0)
}

func (s *assertMgrSuite) TestApplyLocalEnforcedValidationSetsPinned(c *C) {
	s.testApplyLocalEnforcedValidationSets(c, 1)
}

func (s *assertMgrSuite) TestApplyLocalEnforcedValidationSetsWithMismatchedPinnedSeq(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	localVs := s.validationSetAssertForSnaps(c, "foo", "1", "1", []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
		},
	})
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)
	c.Assert(assertstate.Add(st, localVs), IsNil)

	// user requested op with sequence 2 but we're passing a different sequence
	vsKey := fmt.Sprintf("%s/foo", s.dev1Acct.AccountID())
	valSets := map[string][]string{
		vsKey: {release.Series, s.dev1Acct.AccountID(), "foo", "1"},
	}
	pinnedSeqs := map[string]int{vsKey: 2}

	err := assertstate.ApplyLocalEnforcedValidationSets(st, valSets, pinnedSeqs, nil, nil)
	c.Assert(err, ErrorMatches, fmt.Sprintf("internal error: trying to enforce validation set %q with sequence point 1 different than pinned 2", vsKey))
}

func (s *assertMgrSuite) TestApplyLocalEnforcedValidationSetsWithUnmetConstraints(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	snaps := []interface{}{
		map[string]interface{}{
			"id":       "qOqKhntON3vR7kwEbVPsILm7bUViPDzz",
			"name":     "some-snap",
			"presence": "required",
			"revision": "1",
		},
	}

	localVs := s.validationSetAssertForSnaps(c, "foo", "1", "1", snaps)
	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)
	c.Assert(assertstate.Add(st, localVs), IsNil)

	valSets := map[string][]string{
		fmt.Sprintf("%s/foo", s.dev1Acct.AccountID()): {release.Series, s.dev1Acct.AccountID(), "foo", "1"},
	}

	installedSnaps := []*snapasserts.InstalledSnap{
		snapasserts.NewInstalledSnap("some-snap", "qOqKhntON3vR7kwEbVPsILm7bUViPDzz", snap.Revision{N: 2}),
	}

	err := assertstate.ApplyLocalEnforcedValidationSets(st, valSets, nil, installedSnaps, nil)
	c.Assert(err, FitsTypeOf, &snapasserts.ValidationSetsValidationError{})

	err = assertstate.GetValidationSet(st, s.dev1Acct.AccountID(), "foo", &assertstate.ValidationSetTracking{})
	c.Assert(err, testutil.ErrorIs, &state.NoStateError{})
}

func (s *assertMgrSuite) mockDeviceWithValidationSets(c *C, validationSets []interface{}) {
	st := s.state
	a := assertstest.FakeAssertion(map[string]interface{}{
		"type":            "model",
		"authority-id":    "my-brand",
		"series":          "16",
		"brand-id":        "my-brand",
		"model":           "my-model",
		"architecture":    "amd64",
		"store":           "my-brand-store",
		"gadget":          "gadget",
		"kernel":          "krnl",
		"validation-sets": validationSets,
	})
	s.setModel(a.(*asserts.Model))

	a, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"authority-id": s.storeSigning.AuthorityID,
		"operator-id":  s.storeSigning.AuthorityID,
		"store":        "my-brand-store",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(a)
	c.Assert(err, IsNil)

	c.Assert(assertstate.Add(st, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(st, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(st, s.dev1AcctKey), IsNil)
}

func (s *assertMgrSuite) TestFetchAndApplyEnforcedValidationSetEnforceModeSequenceMismatch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.mockDeviceWithValidationSets(c, []interface{}{
		map[string]interface{}{
			"account-id": s.dev1Acct.AccountID(),
			"name":       "bar",
			"mode":       "enforce",
			"sequence":   "10",
		},
	})

	_, err := assertstate.FetchAndApplyEnforcedValidationSet(s.state, s.dev1Acct.AccountID(), "bar", 5, 0, nil, nil)
	c.Check(err, ErrorMatches, fmt.Sprintf(`cannot enforce sequence 5 of validation set %s/bar: only sequence 10 allowed by model`, s.dev1Acct.AccountID()))
}

func (s *assertMgrSuite) TestFetchAndApplyEnforcedValidationSetEnforceModeSequenceFromModel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.mockDeviceWithValidationSets(c, []interface{}{
		map[string]interface{}{
			"account-id": s.dev1Acct.AccountID(),
			"name":       "bar",
			"mode":       "enforce",
			"sequence":   "10",
		},
	})

	vsetAs := s.validationSetAssert(c, "bar", "10", "1", "optional", "1")
	c.Assert(assertstate.Add(s.state, vsetAs), IsNil)
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)

	vss, err := assertstate.FetchAndApplyEnforcedValidationSet(s.state, s.dev1Acct.AccountID(), "bar", 0, 0, nil, nil)
	c.Check(err, IsNil)
	c.Check(vss, DeepEquals, &assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Enforce,
		PinnedAt:  10,
		Current:   10,
	})
}

func (s *assertMgrSuite) TestMonitorValidationSetEnforceModeSequenceMismatch(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.mockDeviceWithValidationSets(c, []interface{}{
		map[string]interface{}{
			"account-id": s.dev1Acct.AccountID(),
			"name":       "bar",
			"mode":       "enforce",
			"sequence":   "3",
		},
	})

	_, err := assertstate.MonitorValidationSet(s.state, s.dev1Acct.AccountID(), "bar", 1, 0)
	c.Check(err, ErrorMatches, fmt.Sprintf(`cannot monitor sequence 1 of validation set %s/bar: only sequence 3 allowed by model`, s.dev1Acct.AccountID()))
}

func (s *assertMgrSuite) TestMonitorValidationSetEnforceModeSequenceFromModel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	s.mockDeviceWithValidationSets(c, []interface{}{
		map[string]interface{}{
			"account-id": s.dev1Acct.AccountID(),
			"name":       "bar",
			"mode":       "enforce",
			"sequence":   "5",
		},
	})

	vsetAs := s.validationSetAssert(c, "bar", "5", "1", "optional", "1")
	c.Assert(assertstate.Add(s.state, vsetAs), IsNil)
	c.Assert(s.storeSigning.Add(vsetAs), IsNil)

	vss, err := assertstate.MonitorValidationSet(s.state, s.dev1Acct.AccountID(), "bar", 0, 0)
	c.Check(err, IsNil)
	c.Check(vss, DeepEquals, &assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "bar",
		Mode:      assertstate.Monitor,
		PinnedAt:  5,
		Current:   5,
	})
}

func (s *assertMgrSuite) TestForgetValidationSetEnforcedByModel(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.mockDeviceWithValidationSets(c, []interface{}{
		map[string]interface{}{
			"account-id": s.dev1Acct.AccountID(),
			"name":       "foo",
			"mode":       "enforce",
			"sequence":   "9",
		},
	})

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Monitor,
		Current:   9,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	err := assertstate.ForgetValidationSet(s.state, s.dev1Acct.AccountID(), "foo", assertstate.ForgetValidationSetOpts{})
	c.Check(err, ErrorMatches, `validation-set is enforced by the model`)

	err = assertstate.ForgetValidationSet(s.state, s.dev1Acct.AccountID(), "foo", assertstate.ForgetValidationSetOpts{
		ForceForget: true,
	})
	c.Check(err, IsNil)

	vsets, err := assertstate.TrackedEnforcedValidationSets(s.state)
	c.Check(err, IsNil)
	c.Check(vsets.Keys(), HasLen, 0)
}

func (s *assertMgrSuite) TestForgetValidationSetPreferEnforcedByModelHappy(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	s.mockDeviceWithValidationSets(c, []interface{}{
		map[string]interface{}{
			"account-id": s.dev1Acct.AccountID(),
			"name":       "foo",
			"mode":       "prefer-enforce",
			"sequence":   "9",
		},
	})

	tr := assertstate.ValidationSetTracking{
		AccountID: s.dev1Acct.AccountID(),
		Name:      "foo",
		Mode:      assertstate.Monitor,
		Current:   9,
	}
	assertstate.UpdateValidationSet(s.state, &tr)

	err := assertstate.ForgetValidationSet(s.state, s.dev1Acct.AccountID(), "foo", assertstate.ForgetValidationSetOpts{})
	c.Check(err, IsNil)
}

type fakeAssertionStore struct {
	storetest.Store

	assertion           func(*asserts.AssertionType, []string, *auth.UserState) (asserts.Assertion, error)
	seqFormingAssertion func(*asserts.AssertionType, []string, int, *auth.UserState) (asserts.Assertion, error)
}

func (f *fakeAssertionStore) Assertion(assertType *asserts.AssertionType, key []string, userState *auth.UserState) (asserts.Assertion, error) {
	return f.assertion(assertType, key, userState)
}

func (f *fakeAssertionStore) SeqFormingAssertion(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
	return f.seqFormingAssertion(assertType, sequenceKey, sequence, user)
}

func (s *assertMgrSuite) TestFetchValidationSetsOnline(c *C) {
	s.testFetchValidationSets(c, testFetchValidationSetsOpts{})
}

func (s *assertMgrSuite) TestFetchValidationSetsOffline(c *C) {
	s.testFetchValidationSets(c, testFetchValidationSetsOpts{
		Offline: true,
	})
}

func (s *assertMgrSuite) TestValidationSetsFromModelOnline(c *C) {
	s.testFetchValidationSets(c, testFetchValidationSetsOpts{
		FromModel: true,
	})
}

func (s *assertMgrSuite) TestValidationSetsFromModelOffline(c *C) {
	s.testFetchValidationSets(c, testFetchValidationSetsOpts{
		Offline:   true,
		FromModel: true,
	})
}

type testFetchValidationSetsOpts struct {
	Offline   bool
	FromModel bool
}

func (s *assertMgrSuite) testFetchValidationSets(c *C, opts testFetchValidationSetsOpts) {
	s.state.Lock()
	defer s.state.Unlock()

	snapsInFoo := []interface{}{
		map[string]interface{}{
			"id":       snaptest.AssertedSnapID("some-snap"),
			"name":     "some-snap",
			"presence": "required",
		},
	}

	fooVset := s.validationSetAssertForSnaps(c, "foo", "1", "1", snapsInFoo)

	snapsInBar := []interface{}{
		map[string]interface{}{
			"id":       snaptest.AssertedSnapID("some-other-snap"),
			"name":     "some-other-snap",
			"presence": "required",
		},
	}

	barVset := s.validationSetAssertForSnaps(c, "bar", "2", "1", snapsInBar)

	var store snapstate.StoreService
	if opts.Offline {
		c.Assert(assertstate.Add(s.state, s.storeSigning.StoreAccountKey("")), IsNil)
		c.Assert(assertstate.Add(s.state, s.dev1Acct), IsNil)
		c.Assert(assertstate.Add(s.state, s.dev1AcctKey), IsNil)
		c.Assert(assertstate.Add(s.state, barVset), IsNil)
		c.Assert(assertstate.Add(s.state, fooVset), IsNil)

		// any store operations will panic since we're using storetest.Store
		store = &storetest.Store{}
	} else {
		c.Assert(s.storeSigning.Add(barVset), IsNil)
		c.Assert(s.storeSigning.Add(fooVset), IsNil)

		assertsFetched := 0
		seqFormingAssertsFetched := 0

		store = &fakeAssertionStore{
			assertion: func(assertType *asserts.AssertionType, key []string, user *auth.UserState) (asserts.Assertion, error) {
				assertsFetched++
				switch assertType.Name {
				case "account-key", "account":
					return s.fakeStore.Assertion(assertType, key, user)
				}
				return nil, fmt.Errorf("unexpected assertion type: %s", assertType.Name)
			},
			seqFormingAssertion: func(assertType *asserts.AssertionType, sequenceKey []string, sequence int, user *auth.UserState) (asserts.Assertion, error) {
				seqFormingAssertsFetched++
				if assertType.Name != "validation-set" {
					return nil, fmt.Errorf("unexpected assertion type: %s", assertType.Name)
				}
				return s.fakeStore.SeqFormingAssertion(assertType, sequenceKey, sequence, user)
			},
		}

		defer func() {
			// two account keys, one account
			c.Check(assertsFetched, Equals, 3)

			// two validation sets
			c.Check(seqFormingAssertsFetched, Equals, 2)
		}()
	}

	deviceCtx := &snapstatetest.TrivialDeviceContext{
		CtxStore: store,
	}

	var sets *snapasserts.ValidationSets

	if opts.FromModel {
		model := assertstest.FakeAssertion(map[string]interface{}{
			"type":         "model",
			"authority-id": "my-brand",
			"series":       "16",
			"brand-id":     "my-brand",
			"model":        "my-model",
			"architecture": "amd64",
			"gadget":       "gadget",
			"kernel":       "krnl",
			"validation-sets": []interface{}{
				map[string]interface{}{
					"account-id": s.dev1Acct.AccountID(),
					"name":       "foo",
					"mode":       "enforce",
				},
				map[string]interface{}{
					"account-id": s.dev1Acct.AccountID(),
					"name":       "bar",
					"sequence":   "2",
					"mode":       "enforce",
				},
			},
		}).(*asserts.Model)

		s.setModel(model)

		model.ValidationSets()

		var err error
		sets, err = assertstate.ValidationSetsFromModel(s.state, model, assertstate.FetchValidationSetsOptions{
			Offline: opts.Offline,
		}, deviceCtx)
		c.Assert(err, IsNil)
	} else {
		toFetch := []*asserts.AtSequence{
			{
				Type:        asserts.ValidationSetType,
				SequenceKey: []string{release.Series, s.dev1Acct.AccountID(), "foo"},
				Revision:    asserts.RevisionNotKnown,
			},
			{
				Type:        asserts.ValidationSetType,
				SequenceKey: []string{release.Series, s.dev1Acct.AccountID(), "bar"},
				Sequence:    2,
				Pinned:      true,
				Revision:    asserts.RevisionNotKnown,
			},
		}

		var err error
		sets, err = assertstate.FetchValidationSets(s.state, toFetch, assertstate.FetchValidationSetsOptions{
			Offline: opts.Offline,
		}, deviceCtx)
		c.Assert(err, IsNil)
	}

	c.Check(sets.RequiredSnaps(), testutil.DeepUnsortedMatches, []string{"some-snap", "some-other-snap"})
	c.Check(sets.Keys(), testutil.DeepUnsortedMatches, []snapasserts.ValidationSetKey{
		snapasserts.ValidationSetKey(fmt.Sprintf("16/%s/foo/1", s.dev1Acct.AccountID())),
		snapasserts.ValidationSetKey(fmt.Sprintf("16/%s/bar/2", s.dev1Acct.AccountID())),
	})
}

func (s *assertMgrSuite) TestValidationSetsFromModelConflict(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	model := assertstest.FakeAssertion(map[string]interface{}{
		"type":         "model",
		"authority-id": "my-brand",
		"series":       "16",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"architecture": "amd64",
		"gadget":       "gadget",
		"kernel":       "krnl",
		"validation-sets": []interface{}{
			map[string]interface{}{
				"account-id": s.dev1Acct.AccountID(),
				"name":       "foo",
				"mode":       "enforce",
			},
			map[string]interface{}{
				"account-id": s.dev1Acct.AccountID(),
				"name":       "bar",
				"sequence":   "2",
				"mode":       "enforce",
			},
		},
	}).(*asserts.Model)

	s.setModel(model)

	snapsInFoo := []interface{}{
		map[string]interface{}{
			"id":       snaptest.AssertedSnapID("some-snap"),
			"name":     "some-snap",
			"presence": "required",
		},
	}

	fooVset := s.validationSetAssertForSnaps(c, "foo", "1", "1", snapsInFoo)

	snapsInBar := []interface{}{
		map[string]interface{}{
			"id":       snaptest.AssertedSnapID("some-snap"),
			"name":     "some-snap",
			"presence": "invalid",
		},
	}

	barVset := s.validationSetAssertForSnaps(c, "bar", "2", "1", snapsInBar)

	c.Assert(assertstate.Add(s.state, s.storeSigning.StoreAccountKey("")), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1Acct), IsNil)
	c.Assert(assertstate.Add(s.state, s.dev1AcctKey), IsNil)
	c.Assert(assertstate.Add(s.state, barVset), IsNil)
	c.Assert(assertstate.Add(s.state, fooVset), IsNil)

	_, err := assertstate.ValidationSetsFromModel(s.state, model, assertstate.FetchValidationSetsOptions{
		Offline: true,
	}, s.trivialDeviceCtx)
	c.Check(err, testutil.ErrorIs, &snapasserts.ValidationSetsConflictError{})
}

func (s *assertMgrSuite) registry(c *C, name string, extraHeaders map[string]interface{}, body string) *asserts.Registry {
	headers := map[string]interface{}{
		"series":       "16",
		"account-id":   s.dev1AcctKey.AccountID(),
		"authority-id": s.dev1AcctKey.AccountID(),
		"name":         name,
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	for h, v := range extraHeaders {
		headers[h] = v
	}

	as, err := s.dev1Signing.Sign(asserts.RegistryType, headers, []byte(body), "")
	c.Assert(err, IsNil)

	return as.(*asserts.Registry)
}

func (s *assertMgrSuite) TestRegistry(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1AcctKey)
	c.Assert(err, IsNil)

	registryFoo := s.registry(c, "foo", map[string]interface{}{
		"views": map[string]interface{}{
			"a-view": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "a", "storage": "a"},
					map[string]interface{}{"request": "b", "storage": "b"},
				},
			},
		},
	},
		`{
  "storage": {
    "schema": {
      "a": "string",
      "b": "string"
    }
  }
}`)
	err = assertstate.Add(s.state, registryFoo)
	c.Assert(err, IsNil)

	_, err = assertstate.Registry(s.state, "no-account", "foo")
	c.Assert(err, testutil.ErrorIs, &asserts.NotFoundError{})

	registryAs, err := assertstate.Registry(s.state, s.dev1AcctKey.AccountID(), "foo")
	c.Assert(err, IsNil)

	registry := registryAs.Registry()
	c.Check(registry.Account, Equals, s.dev1AcctKey.AccountID())
	c.Check(registry.Name, Equals, "foo")
	c.Check(registry.Schema, NotNil)
}

func (s *assertMgrSuite) TestValidateComponent(c *C) {
	const provenance = ""
	s.testValidateComponent(c, provenance)
}

func (s *assertMgrSuite) TestValidateComponentProvenance(c *C) {
	const provenance = "provenance"
	s.testValidateComponent(c, provenance)
}

func (s *assertMgrSuite) testValidateComponent(c *C, provenance string) {
	snapRev, compRev := snap.R(10), snap.R(20)

	paths, _ := s.prereqSnapAssertions(c, provenance, 10)
	snapPath := paths[10]

	compPath, compDigest := s.prereqComponentAssertions(c, provenance, snapRev, compRev)

	s.state.Lock()
	defer s.state.Unlock()

	// have a model and the store assertion available
	storeAs := s.setupModelAndStore(c)
	err := s.storeSigning.Add(storeAs)
	c.Assert(err, IsNil)

	chg := s.state.NewChange("install", "...")
	t := s.state.NewTask("validate-component", "Fetch and check snap assertions")
	snapsup := snapstate.SnapSetup{
		SnapPath:           snapPath,
		UserID:             0,
		ExpectedProvenance: provenance,
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			SnapID:   "snap-id-1",
			Revision: snapRev,
		},
	}
	compsup := snapstate.ComponentSetup{
		CompPath: compPath,
		CompSideInfo: &snap.ComponentSideInfo{
			Component: naming.NewComponentRef("foo", "test-component"),
			Revision:  compRev,
		},
	}
	t.Set("snap-setup", snapsup)
	t.Set("component-setup", compsup)
	chg.AddTask(t)

	s.state.Unlock()
	defer s.se.Stop()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	db := assertstate.DB(s.state)

	headers := map[string]string{
		"resource-sha3-384": compDigest,
		"resource-name":     "test-component",
		"snap-id":           "snap-id-1",
	}
	if provenance != "" {
		headers["provenance"] = provenance
	}

	a, err := db.Find(asserts.SnapResourceRevisionType, headers)
	c.Assert(err, IsNil)
	c.Check(a.(*asserts.SnapResourceRevision).ResourceRevision(), Equals, 20)

	// TODO: remove this check if we decide we don't need it
	// store assertion was also fetched
	_, err = db.Find(asserts.StoreType, map[string]string{
		"store": "my-brand-store",
	})
	c.Assert(err, IsNil)
}
