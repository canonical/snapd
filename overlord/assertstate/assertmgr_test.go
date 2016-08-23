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
	"crypto"
	"encoding/hex"
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
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

func TestAssertManager(t *testing.T) { TestingT(t) }

type assertMgrSuite struct {
	state *state.State
	mgr   *assertstate.AssertManager

	storeSigning *assertstest.StoreStack
	dev1Acct     *asserts.Account

	restore func()
}

var _ = Suite(&assertMgrSuite{})

type fakeStore struct {
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
	a, err := ref.Resolve(sto.db.Find)
	if err != nil {
		return nil, store.ErrAssertionNotFound
	}
	return a, nil
}

func (sto *fakeStore) Snap(name, channel string, devmode bool, user *auth.UserState) (*snap.Info, error) {
	panic("fakeStore.Snap not expected")
}

func (sto *fakeStore) Find(*store.Search, *auth.UserState) ([]*snap.Info, error) {
	panic("fakeStore.Find not expected")
}

func (sto *fakeStore) ListRefresh([]*store.RefreshCandidate, *auth.UserState) ([]*snap.Info, error) {
	panic("fakeStore.ListRefresh not expected")
}

func (sto *fakeStore) Download(string, *snap.DownloadInfo, progress.Meter, *auth.UserState) (string, error) {
	panic("fakeStore.Download not expected")
}

func (sto *fakeStore) SuggestedCurrency() string {
	panic("fakeStore.SuggestedCurrency not expected")
}

func (sto *fakeStore) Buy(*store.BuyOptions, *auth.UserState) (*store.BuyResult, error) {
	panic("fakeStore.Buy not expected")
}

func (sto *fakeStore) PaymentMethods(*auth.UserState) (*store.PaymentInformation, error) {
	panic("fakeStore.PaymentMethods not expected")
}

func (s *assertMgrSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())

	rootPrivKey, _ := assertstest.GenerateKey(1024)
	storePrivKey, _ := assertstest.GenerateKey(752)
	s.storeSigning = assertstest.NewStoreStack("can0nical", rootPrivKey, storePrivKey)
	s.restore = sysdb.InjectTrusted(s.storeSigning.Trusted)

	s.dev1Acct = assertstest.NewAccount(s.storeSigning, "developer1", nil, "")

	s.state = state.New(nil)
	mgr, err := assertstate.Manager(s.state)
	c.Assert(err, IsNil)
	s.mgr = mgr

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

func makeHexDigest(rev int) string {
	return hex.EncodeToString(fakeHash(rev))
}

func (s *assertMgrSuite) prereqSnapAssertions(c *C, revisions ...int) {
	err := s.storeSigning.Add(s.dev1Acct)
	c.Assert(err, IsNil)

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

func (s *assertMgrSuite) TestFetch(c *C) {
	s.prereqSnapAssertions(c, 10)

	s.state.Lock()
	defer s.state.Unlock()

	ref := &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{makeDigest(10)},
	}

	err := assertstate.Fetch(s.state, ref, 0)
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

	err := assertstate.Fetch(s.state, ref, 0)
	c.Assert(err, IsNil)

	ref = &asserts.Ref{
		Type:       asserts.SnapRevisionType,
		PrimaryKey: []string{makeDigest(11)},
	}

	err = assertstate.Fetch(s.state, ref, 0)
	c.Assert(err, IsNil)

	snapRev, err := ref.Resolve(assertstate.DB(s.state).Find)
	c.Assert(err, IsNil)
	c.Check(snapRev.(*asserts.SnapRevision).SnapRevision(), Equals, 11)
}

func (s *assertMgrSuite) settle() {
	// XXX: would like to use Overlord.Settle but not enough control there
	for i := 0; i < 50; i++ {
		s.mgr.Ensure()
		s.mgr.Wait()
	}
}

func (s *assertMgrSuite) TestFetchCheckSnapAssertions(c *C) {
	s.prereqSnapAssertions(c, 10)

	tempdir := c.MkDir()
	snapPath := filepath.Join(tempdir, "foo.snap")
	err := ioutil.WriteFile(snapPath, fakeSnap(10), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "...")
	t := s.state.NewTask("fetch-check-snap-assertions", "Fetch and check snap assertions")
	ss := snapstate.SnapSetup{
		SnapPath: snapPath,
		UserID:   0,
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			SnapID:   "snap-id-1",
			Revision: snap.R(10),
		},
	}
	t.Set("snap-setup", ss)
	chg.AddTask(t)

	s.state.Unlock()
	defer s.mgr.Stop()
	s.settle()
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)

	snapRev, err := assertstate.DB(s.state).Find(asserts.SnapRevisionType, map[string]string{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": makeDigest(10),
	})
	c.Assert(err, IsNil)
	c.Check(snapRev.(*asserts.SnapRevision).SnapRevision(), Equals, 10)
}

func (s *assertMgrSuite) TestFetchCheckSnapAssertionsNotFound(c *C) {
	tempdir := c.MkDir()
	snapPath := filepath.Join(tempdir, "foo.snap")
	err := ioutil.WriteFile(snapPath, fakeSnap(33), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "...")
	t := s.state.NewTask("fetch-check-snap-assertions", "Fetch and check snap assertions")
	ss := snapstate.SnapSetup{
		SnapPath: snapPath,
		UserID:   0,
		SideInfo: &snap.SideInfo{
			RealName: "foo",
			SnapID:   "snap-id-1",
			Revision: snap.R(33),
		},
	}
	t.Set("snap-setup", ss)
	chg.AddTask(t)

	s.state.Unlock()
	defer s.mgr.Stop()
	s.settle()
	s.state.Lock()

	c.Assert(chg.Err(), ErrorMatches, `(?s).*cannot verify snap "foo" and its hash, no matching assertions found.*`)
}

func (s *assertMgrSuite) TestCrossCheckSnapErrors(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// add in prereqs assertions
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDecl)
	c.Assert(err, IsNil)

	digest := makeDigest(12)
	size := uint64(len(fakeSnap(12)))
	headers = map[string]interface{}{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-revision": "12",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapRev)
	c.Assert(err, IsNil)

	si := &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(12),
	}

	// different size
	err = assertstate.CrossCheckSnap(s.state, "foo", digest, size+1, si)
	c.Check(err, ErrorMatches, fmt.Sprintf(`snap "foo" file does not have expected size according to assertions \(download is broken or tampered\): %d != %d`, size+1, size))

	// mismatched revision vs what we got from store original info
	err = assertstate.CrossCheckSnap(s.state, "foo", digest, size, &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(21),
	})
	c.Check(err, ErrorMatches, fmt.Sprintf(`snap "foo" file hash %q corresponding assertions implied snap id "snap-id-1" and revision 12 are not the ones expected for installing \(store metadata is broken or communication tampered\): "snap-id-1" and 21`, digest))

	// mismatched snap id vs what we got from store original info
	err = assertstate.CrossCheckSnap(s.state, "foo", digest, size, &snap.SideInfo{
		SnapID:   "snap-id-other",
		Revision: snap.R(12),
	})
	c.Check(err, ErrorMatches, fmt.Sprintf(`snap "foo" file hash %q corresponding assertions implied snap id "snap-id-1" and revision 12 are not the ones expected for installing \(store metadata is broken or communication tampered\): "snap-id-other" and 12`, digest))

	// changed name
	err = assertstate.CrossCheckSnap(s.state, "baz", digest, size, si)
	c.Check(err, ErrorMatches, `cannot install snap "baz" that is undergoing a rename to "foo"`)

}

func (s *assertMgrSuite) TestCrossCheckSnapRevokedSnapDecl(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// add in prereqs assertions
	err := assertstate.Add(s.state, s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, s.dev1Acct)
	c.Assert(err, IsNil)

	// revoked snap declaration (snap-name=="") !
	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "",
		"publisher-id": s.dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapDecl)
	c.Assert(err, IsNil)

	digest := makeDigest(12)
	size := uint64(len(fakeSnap(12)))
	headers = map[string]interface{}{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-revision": "12",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = assertstate.Add(s.state, snapRev)
	c.Assert(err, IsNil)

	si := &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(12),
	}

	err = assertstate.CrossCheckSnap(s.state, "foo", digest, size, si)
	c.Check(err, ErrorMatches, `cannot install snap "foo" with a revoked snap declaration`)
}

func (s *assertMgrSuite) TestFetchCheckSnapAssertionsCrossCheckFail(c *C) {
	s.prereqSnapAssertions(c, 10)

	tempdir := c.MkDir()
	snapPath := filepath.Join(tempdir, "foo.snap")
	err := ioutil.WriteFile(snapPath, fakeSnap(10), 0644)
	c.Assert(err, IsNil)

	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install", "...")
	t := s.state.NewTask("fetch-check-snap-assertions", "Fetch and check snap assertions")
	ss := snapstate.SnapSetup{
		SnapPath: snapPath,
		UserID:   0,
		SideInfo: &snap.SideInfo{
			RealName: "f",
			SnapID:   "snap-id-1",
			Revision: snap.R(10),
		},
	}
	t.Set("snap-setup", ss)
	chg.AddTask(t)

	s.state.Unlock()
	defer s.mgr.Stop()
	s.settle()
	s.state.Lock()

	c.Assert(chg.Err(), ErrorMatches, `(?s).*cannot install snap "f" that is undergoing a rename to "foo".*`)
}
