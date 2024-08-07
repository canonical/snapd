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

package snapasserts_test

import (
	"crypto"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/sha3"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

func TestSnapasserts(t *testing.T) { TestingT(t) }

type snapassertsSuite struct {
	testutil.BaseTest

	storeSigning *assertstest.StoreStack
	dev1Acct     *asserts.Account
	dev1Signing  *assertstest.SigningDB

	localDB *asserts.Database
}

var _ = Suite(&snapassertsSuite{})

func (s *snapassertsSuite) SetUpTest(c *C) {
	s.storeSigning = assertstest.NewStoreStack("can0nical", nil)

	s.dev1Acct = assertstest.NewAccount(s.storeSigning, "developer1", nil, "")

	localDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore: asserts.NewMemoryBackstore(),
		Trusted:   s.storeSigning.Trusted,
	})
	c.Assert(err, IsNil)

	s.localDB = localDB

	// add in prereqs assertions
	err = s.localDB.Add(s.storeSigning.StoreAccountKey(""))
	c.Assert(err, IsNil)
	err = s.localDB.Add(s.dev1Acct)
	c.Assert(err, IsNil)

	privKey, _ := assertstest.GenerateKey(752)
	accKey := assertstest.NewAccountKey(s.storeSigning, s.dev1Acct, nil, privKey.PublicKey(), "")
	err = s.localDB.Add(accKey)
	c.Assert(err, IsNil)

	s.dev1Signing = assertstest.NewSigningDB(s.dev1Acct.AccountID(), privKey)

	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapDecl)
	c.Assert(err, IsNil)

	s.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
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

func (s *snapassertsSuite) TestCrossCheckHappy(c *C) {
	digest := makeDigest(12)
	size := uint64(len(fakeSnap(12)))
	headers := map[string]interface{}{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-revision": "12",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapRev)
	c.Assert(err, IsNil)

	si := &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(12),
	}

	// everything cross checks, with the regular snap name
	checkedRev, err := snapasserts.CrossCheck("foo", digest, "", size, si, nil, s.localDB)
	c.Assert(err, IsNil)
	c.Check(checkedRev, DeepEquals, snapRev)

	// and a snap instance name
	_, err = snapasserts.CrossCheck("foo_instance", digest, "", size, si, nil, s.localDB)
	c.Check(err, IsNil)
}

func (s *snapassertsSuite) TestCrossCheckErrors(c *C) {
	digest := makeDigest(12)
	size := uint64(len(fakeSnap(12)))
	headers := map[string]interface{}{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-revision": "12",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapRev)
	c.Assert(err, IsNil)

	si := &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(12),
	}

	// different size
	_, err = snapasserts.CrossCheck("foo", digest, "", size+1, si, nil, s.localDB)
	c.Check(err, ErrorMatches, fmt.Sprintf(`snap "foo" file does not have expected size according to signatures \(download is broken or tampered\): %d != %d`, size+1, size))
	_, err = snapasserts.CrossCheck("foo_instance", digest, "", size+1, si, nil, s.localDB)
	c.Check(err, ErrorMatches, fmt.Sprintf(`snap "foo_instance" file does not have expected size according to signatures \(download is broken or tampered\): %d != %d`, size+1, size))

	// mismatched revision vs what we got from store original info
	_, err = snapasserts.CrossCheck("foo", digest, "", size, &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(21),
	}, nil, s.localDB)
	c.Check(err, ErrorMatches, `snap "foo" does not have expected ID or revision according to assertions \(metadata is broken or tampered\): 21 / snap-id-1 != 12 / snap-id-1`)
	_, err = snapasserts.CrossCheck("foo_instance", digest, "", size, &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(21),
	}, nil, s.localDB)
	c.Check(err, ErrorMatches, `snap "foo_instance" does not have expected ID or revision according to assertions \(metadata is broken or tampered\): 21 / snap-id-1 != 12 / snap-id-1`)

	// mismatched snap id vs what we got from store original info
	_, err = snapasserts.CrossCheck("foo", digest, "", size, &snap.SideInfo{
		SnapID:   "snap-id-other",
		Revision: snap.R(12),
	}, nil, s.localDB)
	c.Check(err, ErrorMatches, `snap "foo" does not have expected ID or revision according to assertions \(metadata is broken or tampered\): 12 / snap-id-other != 12 / snap-id-1`)
	_, err = snapasserts.CrossCheck("foo_instance", digest, "", size, &snap.SideInfo{
		SnapID:   "snap-id-other",
		Revision: snap.R(12),
	}, nil, s.localDB)
	c.Check(err, ErrorMatches, `snap "foo_instance" does not have expected ID or revision according to assertions \(metadata is broken or tampered\): 12 / snap-id-other != 12 / snap-id-1`)

	// changed name
	_, err = snapasserts.CrossCheck("baz", digest, "", size, si, nil, s.localDB)
	c.Check(err, ErrorMatches, `cannot install "baz", snap "baz" is undergoing a rename to "foo"`)
	_, err = snapasserts.CrossCheck("baz_instance", digest, "", size, si, nil, s.localDB)
	c.Check(err, ErrorMatches, `cannot install "baz_instance", snap "baz" is undergoing a rename to "foo"`)

}

func (s *snapassertsSuite) TestCrossCheckRevokedSnapDecl(c *C) {
	// revoked snap declaration (snap-name=="") !
	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "",
		"publisher-id": s.dev1Acct.AccountID(),
		"revision":     "1",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapDecl)
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
	err = s.localDB.Add(snapRev)
	c.Assert(err, IsNil)

	si := &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(12),
	}

	_, err = snapasserts.CrossCheck("foo", digest, "", size, si, nil, s.localDB)
	c.Check(err, ErrorMatches, `cannot install snap "foo" with a revoked snap declaration`)
	_, err = snapasserts.CrossCheck("foo_instance", digest, "", size, si, nil, s.localDB)
	c.Check(err, ErrorMatches, `cannot install snap "foo_instance" with a revoked snap declaration`)
}

func (s *snapassertsSuite) TestDeriveSideInfoHappy(c *C) {
	fooSnap := snaptest.MakeTestSnapWithFiles(c, `name: foo
version: 1`, nil)
	digest, size, err := asserts.SnapFileSHA3_384(fooSnap)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-revision": "42",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapRev)
	c.Assert(err, IsNil)

	si, err := snapasserts.DeriveSideInfo(fooSnap, nil, s.localDB)
	c.Assert(err, IsNil)
	c.Check(si, DeepEquals, &snap.SideInfo{
		RealName: "foo",
		SnapID:   "snap-id-1",
		Revision: snap.R(42),
		Channel:  "",
	})
}

func (s *snapassertsSuite) TestDeriveSideInfoNoSignatures(c *C) {
	tempdir := c.MkDir()
	snapPath := filepath.Join(tempdir, "anon.snap")
	err := os.WriteFile(snapPath, fakeSnap(42), 0644)
	c.Assert(err, IsNil)

	_, err = snapasserts.DeriveSideInfo(snapPath, nil, s.localDB)
	// cannot find signatures with metadata for snap
	c.Assert(errors.Is(err, &asserts.NotFoundError{}), Equals, true)
}

func (s *snapassertsSuite) TestDeriveSideInfoSizeMismatch(c *C) {
	digest := makeDigest(42)
	size := uint64(len(fakeSnap(42)))
	headers := map[string]interface{}{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size+5), // broken
		"snap-revision": "42",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapRev)
	c.Assert(err, IsNil)

	tempdir := c.MkDir()
	snapPath := filepath.Join(tempdir, "anon.snap")
	err = os.WriteFile(snapPath, fakeSnap(42), 0644)
	c.Assert(err, IsNil)

	_, err = snapasserts.DeriveSideInfo(snapPath, nil, s.localDB)
	c.Check(err, ErrorMatches, fmt.Sprintf(`snap %q does not have expected size according to signatures \(broken or tampered\): %d != %d`, snapPath, size, size+5))
}

func (s *snapassertsSuite) TestDeriveSideInfoRevokedSnapDecl(c *C) {
	// revoked snap declaration (snap-name=="") !
	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "",
		"publisher-id": s.dev1Acct.AccountID(),
		"revision":     "1",
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapDecl)
	c.Assert(err, IsNil)

	digest := makeDigest(42)
	size := uint64(len(fakeSnap(42)))
	headers = map[string]interface{}{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-revision": "42",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapRev)
	c.Assert(err, IsNil)

	tempdir := c.MkDir()
	snapPath := filepath.Join(tempdir, "anon.snap")
	err = os.WriteFile(snapPath, fakeSnap(42), 0644)
	c.Assert(err, IsNil)

	_, err = snapasserts.DeriveSideInfo(snapPath, nil, s.localDB)
	c.Check(err, ErrorMatches, fmt.Sprintf(`cannot install snap %q with a revoked snap declaration`, snapPath))
}

func (s *snapassertsSuite) TestCrossCheckDelegatedSnapHappy(c *C) {
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"revision":     "1",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": s.dev1Acct.AccountID(),
				"provenance": []interface{}{
					"prov1",
				},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapDecl)
	c.Assert(err, IsNil)

	digest := makeDigest(42)
	size := uint64(len(fakeSnap(42)))
	headers := map[string]interface{}{
		"authority-id":  s.dev1Acct.AccountID(),
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"provenance":    "prov1",
		"snap-revision": "42",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.dev1Signing.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = s.localDB.Add(snapRev)
	c.Check(err, IsNil)

	si := &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(42),
	}

	// everything cross checks, with the regular snap name
	checkedRev, err := snapasserts.CrossCheck("foo", digest, "prov1", size, si, nil, s.localDB)
	c.Assert(err, IsNil)
	c.Check(checkedRev, DeepEquals, snapRev)
	// and a snap instance name
	_, err = snapasserts.CrossCheck("foo_instance", digest, "prov1", size, si, nil, s.localDB)
	c.Check(err, IsNil)
}

func (s *snapassertsSuite) TestCrossCheckWithDeviceDelegatedSnapHappy(c *C) {
	a, err := s.dev1Signing.Sign(asserts.ModelType, map[string]interface{}{
		"brand-id":     s.dev1Acct.AccountID(),
		"series":       "16",
		"model":        "dev-model",
		"store":        "substore",
		"architecture": "amd64",
		"base":         "core18",
		"kernel":       "krnl",
		"gadget":       "gadget",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)

	substore, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":           "substore",
		"operator-id":     "can0nical",
		"friendly-stores": []interface{}{"store1"},
		"timestamp":       time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(substore)
	c.Assert(err, IsNil)

	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"revision":     "1",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": s.dev1Acct.AccountID(),
				"provenance": []interface{}{
					"prov1",
				},
				"on-store": []interface{}{
					"store1",
				},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapDecl)
	c.Assert(err, IsNil)

	digest := makeDigest(42)
	size := uint64(len(fakeSnap(42)))
	headers := map[string]interface{}{
		"authority-id":  s.dev1Acct.AccountID(),
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"provenance":    "prov1",
		"snap-revision": "42",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.dev1Signing.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = s.localDB.Add(snapRev)
	c.Check(err, IsNil)

	si := &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(42),
	}

	// everything cross checks, with the regular snap name
	checkedRev, err := snapasserts.CrossCheck("foo", digest, "prov1", size, si, model, s.localDB)
	c.Assert(err, IsNil)
	c.Check(checkedRev, Equals, snapRev)
	// and a snap instance name
	_, err = snapasserts.CrossCheck("foo_instance", digest, "prov1", size, si, model, s.localDB)
	c.Check(err, IsNil)
}

func (s *snapassertsSuite) TestCrossCheckWithDeviceDelegatedSnapUnhappy(c *C) {
	a, err := s.dev1Signing.Sign(asserts.ModelType, map[string]interface{}{
		"brand-id":     s.dev1Acct.AccountID(),
		"series":       "16",
		"model":        "dev-model",
		"store":        "substore",
		"architecture": "amd64",
		"base":         "core18",
		"kernel":       "krnl",
		"gadget":       "gadget",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)

	substore, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":           "substore",
		"operator-id":     "can0nical",
		"friendly-stores": []interface{}{"store1"},
		"timestamp":       time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(substore)
	c.Assert(err, IsNil)

	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"revision":     "1",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": s.dev1Acct.AccountID(),
				"provenance": []interface{}{
					"prov1",
				},
				"on-store": []interface{}{
					"store2",
				},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapDecl)
	c.Assert(err, IsNil)

	digest := makeDigest(42)
	size := uint64(len(fakeSnap(42)))
	headers := map[string]interface{}{
		"authority-id":  s.dev1Acct.AccountID(),
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"provenance":    "prov1",
		"snap-revision": "42",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.dev1Signing.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = s.localDB.Add(snapRev)
	c.Check(err, IsNil)

	si := &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(42),
	}

	_, err = snapasserts.CrossCheck("foo", digest, "prov1", size, si, model, s.localDB)
	c.Check(err, ErrorMatches, `snap "foo" revision assertion with provenance "prov1" is not signed by an authority authorized on this device: .*`)
}

func (s *snapassertsSuite) TestCrossCheckSpuriousProvenanceUnhappy(c *C) {
	digest := makeDigest(12)
	size := uint64(len(fakeSnap(12)))
	headers := map[string]interface{}{
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-revision": "12",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.storeSigning.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapRev)
	c.Assert(err, IsNil)

	si := &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(12),
	}

	_, err = snapasserts.CrossCheck("foo", digest, "prov", size, si, nil, s.localDB)
	c.Check(err, ErrorMatches, `.*cannot find pre-populated snap-revision assertion for "foo": .*provenance: prov`)
}

func (s *snapassertsSuite) TestCheckProvenanceWithVerifiedRevision(c *C) {
	digest := makeDigest(12)
	size := uint64(len(fakeSnap(12)))
	snapRevGlobalUpload := assertstest.FakeAssertion(map[string]interface{}{
		"type":          "snap-revision",
		"authority-id":  "can0nical",
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-revision": "12",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}).(*asserts.SnapRevision)
	snapRevProv := assertstest.FakeAssertion(map[string]interface{}{
		"type":          "snap-revision",
		"authority-id":  s.dev1Acct.AccountID(),
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"provenance":    "prov",
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-revision": "12",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}).(*asserts.SnapRevision)
	snapRevProv2 := assertstest.FakeAssertion(map[string]interface{}{
		"type":          "snap-revision",
		"authority-id":  s.dev1Acct.AccountID(),
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"provenance":    "prov2",
		"snap-size":     fmt.Sprintf("%d", size),
		"snap-revision": "12",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}).(*asserts.SnapRevision)
	withProv := snaptest.MakeTestSnapWithFiles(c, `name: with-prov
version: 1
provenance: prov`, nil)
	defaultProv := snaptest.MakeTestSnapWithFiles(c, `name: defl
version: 1
`, nil)

	// matching
	c.Check(snapasserts.CheckProvenanceWithVerifiedRevision(withProv, snapRevProv), IsNil)
	c.Check(snapasserts.CheckProvenanceWithVerifiedRevision(defaultProv, snapRevGlobalUpload), IsNil)

	// mismatches
	mismatches := []struct {
		path         string
		snapRev      *asserts.SnapRevision
		metadataProv string
	}{
		{withProv, snapRevProv2, "prov"},
		{withProv, snapRevGlobalUpload, "prov"},
		{defaultProv, snapRevProv, "global-upload"},
	}
	for _, mism := range mismatches {
		c.Check(snapasserts.CheckProvenanceWithVerifiedRevision(mism.path, mism.snapRev), ErrorMatches, fmt.Sprintf("snap %q has been signed under provenance %q different from the metadata one: %q", mism.path, mism.snapRev.Provenance(), mism.metadataProv))
	}

}

func (s *snapassertsSuite) TestCheckComponentProvenanceWithVerifiedRevision(c *C) {
	digest := makeDigest(12)
	size := uint64(len(fakeSnap(12)))
	snapResRev := assertstest.FakeAssertion(map[string]interface{}{
		"type":              "snap-resource-revision",
		"authority-id":      s.dev1Acct.AccountID(),
		"snap-id":           "snap-id-1",
		"resource-name":     "comp1",
		"resource-sha3-384": digest,
		"developer-id":      s.dev1Acct.AccountID(),
		"provenance":        "prov",
		"resource-revision": "22",
		"resource-size":     fmt.Sprintf("%d", size),
		"timestamp":         time.Now().Format(time.RFC3339),
	}).(*asserts.SnapResourceRevision)
	snapResRev2 := assertstest.FakeAssertion(map[string]interface{}{
		"type":              "snap-resource-revision",
		"authority-id":      s.dev1Acct.AccountID(),
		"snap-id":           "snap-id-1",
		"resource-name":     "comp1",
		"resource-sha3-384": digest,
		"developer-id":      s.dev1Acct.AccountID(),
		"provenance":        "global-upload",
		"resource-revision": "22",
		"resource-size":     fmt.Sprintf("%d", size),
		"timestamp":         time.Now().Format(time.RFC3339),
	}).(*asserts.SnapResourceRevision)
	compPath := snaptest.MakeTestComponentWithFiles(c, "comp1", `component: snap+comp1
type: test
provenance: prov
version: 1.0.2
`, nil)
	compPath2 := snaptest.MakeTestComponentWithFiles(c, "comp1", `component: snap+comp1
type: test
version: 1.0.2
`, nil)

	// matching
	c.Check(snapasserts.CheckComponentProvenanceWithVerifiedRevision(compPath, snapResRev), IsNil)
	c.Check(snapasserts.CheckComponentProvenanceWithVerifiedRevision(compPath2, snapResRev2), IsNil)

	// mismatches
	mismatches := []struct {
		path         string
		snapRev      *asserts.SnapResourceRevision
		metadataProv string
	}{
		{compPath, snapResRev2, "prov"},
		{compPath2, snapResRev, "global-upload"},
	}
	for _, mism := range mismatches {
		c.Check(snapasserts.CheckComponentProvenanceWithVerifiedRevision(mism.path, mism.snapRev), ErrorMatches, fmt.Sprintf("component %q has been signed under provenance %q different from the metadata one: %q", mism.path, mism.snapRev.Provenance(), mism.metadataProv))
	}
}

func (s *snapassertsSuite) TestDeriveSideInfoFromDigestAndSizeDelegatedSnap(c *C) {
	withProv := snaptest.MakeTestSnapWithFiles(c, `name: with-prov
version: 1
provenance: prov`, nil)
	digest, size, err := asserts.SnapFileSHA3_384(withProv)
	c.Assert(err, IsNil)

	a, err := s.dev1Signing.Sign(asserts.ModelType, map[string]interface{}{
		"brand-id":     s.dev1Acct.AccountID(),
		"series":       "16",
		"model":        "dev-model",
		"store":        "substore",
		"architecture": "amd64",
		"base":         "core18",
		"kernel":       "krnl",
		"gadget":       "gadget",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	model := a.(*asserts.Model)

	substore, err := s.storeSigning.Sign(asserts.StoreType, map[string]interface{}{
		"store":           "substore",
		"operator-id":     "can0nical",
		"friendly-stores": []interface{}{"store1"},
		"timestamp":       time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(substore)
	c.Assert(err, IsNil)

	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"revision":     "1",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": s.dev1Acct.AccountID(),
				"provenance": []interface{}{
					"prov",
				},
				"on-store": []interface{}{
					"store1",
				},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapDecl)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"authority-id":  s.dev1Acct.AccountID(),
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"provenance":    "prov",
		"snap-revision": "41",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.dev1Signing.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = s.localDB.Add(snapRev)
	c.Check(err, IsNil)

	si, err := snapasserts.DeriveSideInfoFromDigestAndSize(withProv, digest, size, model, s.localDB)
	c.Check(err, IsNil)
	c.Check(si, DeepEquals, &snap.SideInfo{
		RealName: "foo",
		SnapID:   "snap-id-1",
		Revision: snap.R(41),
		Channel:  "",
	})
}

func (s *snapassertsSuite) TestDeriveSideInfoFromDigestAndSizeDelegatedSnapAmbiguous(c *C) {
	// this is not a fully realistic test as this unlikely
	// scenario would happen possibly across different delegated
	// accounts, the goal is simply to trigger the error
	// even if not in a realistic way
	withProv := snaptest.MakeTestSnapWithFiles(c, `name: with-prov
version: 1
provenance: prov`, nil)
	digest, size, err := asserts.SnapFileSHA3_384(withProv)
	c.Assert(err, IsNil)

	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"revision":     "1",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": s.dev1Acct.AccountID(),
				"provenance": []interface{}{
					"prov",
					"prov2",
				},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapDecl)
	c.Assert(err, IsNil)

	headers := map[string]interface{}{
		"authority-id":  s.dev1Acct.AccountID(),
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"provenance":    "prov",
		"snap-revision": "41",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev, err := s.dev1Signing.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = s.localDB.Add(snapRev)
	c.Check(err, IsNil)

	headers = map[string]interface{}{
		"authority-id":  s.dev1Acct.AccountID(),
		"snap-id":       "snap-id-1",
		"snap-sha3-384": digest,
		"snap-size":     fmt.Sprintf("%d", size),
		"provenance":    "prov2",
		"snap-revision": "82",
		"developer-id":  s.dev1Acct.AccountID(),
		"timestamp":     time.Now().Format(time.RFC3339),
	}
	snapRev2, err := s.dev1Signing.Sign(asserts.SnapRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = s.localDB.Add(snapRev2)
	c.Check(err, IsNil)

	_, err = snapasserts.DeriveSideInfoFromDigestAndSize(withProv, digest, size, nil, s.localDB)
	c.Check(err, ErrorMatches, `safely handling snaps with different provenance but same hash not yet supported`)
}

func assertedSnapID(snapName string) string {
	snapID := naming.WellKnownSnapID(snapName)
	if snapID != "" {
		return snapID
	}
	return snaptest.AssertedSnapID(snapName)
}

func (s *snapassertsSuite) makeUC20Model(c *C, extraHeaders map[string]interface{}) *asserts.Model {
	comps := map[string]interface{}{
		"comp1": "required",
		"comp2": "optional",
	}
	headers := map[string]interface{}{
		"brand-id":     s.dev1Acct.AccountID(),
		"series":       "16",
		"model":        "dev-model",
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core20",
		"timestamp":    time.Now().Format(time.RFC3339),
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              assertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              assertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":       "required20",
				"id":         assertedSnapID("required20"),
				"components": comps,
			}},
	}
	for k, v := range extraHeaders {
		headers[k] = v
	}

	model, err := s.dev1Signing.Sign(asserts.ModelType, headers, nil, "")
	c.Assert(err, IsNil)
	return model.(*asserts.Model)
}

func (s *snapassertsSuite) TestDeriveComponentSideInfoFromDigestAndSize(c *C) {
	model := s.makeUC20Model(c, nil)

	compPath := snaptest.MakeTestComponentWithFiles(c, "comp1", `component: snap+comp1
type: test
version: 1.0.2
`, nil)
	digest, size, err := asserts.SnapFileSHA3_384(compPath)
	c.Assert(err, IsNil)

	// Make sure we error if no assertion
	csi, err := snapasserts.DeriveComponentSideInfoFromDigestAndSize(
		"comp1", "snap", "snap-id-1", compPath, digest, size, model, s.localDB)
	c.Check(err, ErrorMatches, "snap-resource-revision assertion not found")
	c.Check(csi, IsNil)

	resRev, err := s.storeSigning.Sign(asserts.SnapResourceRevisionType, map[string]interface{}{
		"type":              "snap-resource-revision",
		"authority-id":      "can0nical",
		"snap-id":           "snap-id-1",
		"resource-name":     "comp1",
		"resource-sha3-384": digest,
		"developer-id":      s.dev1Acct.AccountID(),
		"provenance":        "global-upload",
		"resource-revision": "22",
		"resource-size":     fmt.Sprintf("%d", size),
		"timestamp":         time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(resRev)
	c.Assert(err, IsNil)

	csi, err = snapasserts.DeriveComponentSideInfoFromDigestAndSize(
		"comp1", "snap", "snap-id-1", compPath, digest, size, model, s.localDB)
	c.Check(err, IsNil)
	c.Check(csi, DeepEquals, &snap.ComponentSideInfo{
		Component: naming.NewComponentRef("snap", "comp1"),
		Revision:  snap.R(22),
	})
}

func (s *snapassertsSuite) TestDeriveComponentSideInfoFromDigestAndSizeWrongSize(c *C) {
	model := s.makeUC20Model(c, nil)
	compPath := snaptest.MakeTestComponentWithFiles(c, "comp1", `component: snap+comp1
type: test
version: 1.0.2
`, nil)
	digest, size, err := asserts.SnapFileSHA3_384(compPath)
	c.Assert(err, IsNil)

	resRev, err := s.storeSigning.Sign(asserts.SnapResourceRevisionType, map[string]interface{}{
		"type":              "snap-resource-revision",
		"authority-id":      "can0nical",
		"snap-id":           "snap-id-1",
		"resource-name":     "comp1",
		"resource-sha3-384": digest,
		"developer-id":      s.dev1Acct.AccountID(),
		"provenance":        "global-upload",
		"resource-revision": "22",
		"resource-size":     fmt.Sprintf("%d", size+1),
		"timestamp":         time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(resRev)
	c.Assert(err, IsNil)

	csi, err := snapasserts.DeriveComponentSideInfoFromDigestAndSize(
		"comp1", "snap", "snap-id-1", compPath, digest, size, model, s.localDB)
	c.Check(err, ErrorMatches, fmt.Sprintf(`resource "comp1" does not have the expected size according to signatures \(broken or tampered\): %d != %d`, size, size+1))
	c.Check(csi, IsNil)
}

func (s *snapassertsSuite) TestDeriveComponentSideInfoFromDigestAndSizeWithProvenance(c *C) {
	model := s.makeUC20Model(c, map[string]interface{}{"store": "store1"})
	compPath := snaptest.MakeTestComponentWithFiles(c, "comp1", `component: snap+comp1
type: test
provenance: prov
version: 1.0.2
`, nil)
	digest, size, err := asserts.SnapFileSHA3_384(compPath)
	c.Assert(err, IsNil)

	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"revision":     "1",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": s.dev1Acct.AccountID(),
				"provenance": []interface{}{
					"prov",
					"prov2",
				},
				"on-store": []interface{}{
					"store1",
				},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapDecl)
	c.Assert(err, IsNil)

	// Ok result
	resRev, err := s.dev1Signing.Sign(asserts.SnapResourceRevisionType, map[string]interface{}{
		"type":              "snap-resource-revision",
		"authority-id":      s.dev1Acct.AccountID(),
		"snap-id":           "snap-id-1",
		"resource-name":     "comp1",
		"resource-sha3-384": digest,
		"developer-id":      s.dev1Acct.AccountID(),
		"provenance":        "prov",
		"resource-revision": "22",
		"resource-size":     fmt.Sprintf("%d", size),
		"timestamp":         time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(resRev)
	c.Assert(err, IsNil)

	csi, err := snapasserts.DeriveComponentSideInfoFromDigestAndSize(
		"comp1", "snap", "snap-id-1", compPath, digest, size, model, s.localDB)
	c.Check(err, IsNil)
	c.Check(csi, DeepEquals, &snap.ComponentSideInfo{
		Component: naming.NewComponentRef("snap", "comp1"),
		Revision:  snap.R(22),
	})

	// Model not for the right store
	modelNoStore := s.makeUC20Model(c, nil)
	csi, err = snapasserts.DeriveComponentSideInfoFromDigestAndSize(
		"comp1", "snap", "snap-id-1", compPath, digest, size, modelNoStore, s.localDB)
	c.Assert(err, ErrorMatches, `snap resource "comp1" revision assertion with provenance "prov" is not signed by an authority authorized on this device:.*`)
	c.Check(csi, IsNil)

	// Same hash but different provenance
	resRev2, err := s.dev1Signing.Sign(asserts.SnapResourceRevisionType, map[string]interface{}{
		"type":              "snap-resource-revision",
		"authority-id":      s.dev1Acct.AccountID(),
		"snap-id":           "snap-id-1",
		"resource-name":     "comp1",
		"resource-sha3-384": digest,
		"developer-id":      s.dev1Acct.AccountID(),
		"provenance":        "prov2",
		"resource-revision": "22",
		"resource-size":     fmt.Sprintf("%d", size),
		"timestamp":         time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(resRev2)
	c.Assert(err, IsNil)
	csi, err = snapasserts.DeriveComponentSideInfoFromDigestAndSize(
		"comp1", "snap", "snap-id-1", compPath, digest, size, model, s.localDB)
	c.Check(err, ErrorMatches, "safely handling resources with different provenance but same hash not yet supported")
	c.Check(csi, IsNil)
}

func (s *snapassertsSuite) TestCrossCheckResource(c *C) {
	digest := makeDigest(12)
	componentRev := snap.R(24)
	snapRev := snap.R(12)
	const size = uint64(1024)
	const resourceName = "test-component"
	const snapID = "snap-id-1"

	revHeaders := map[string]interface{}{
		"snap-id":           snapID,
		"resource-name":     resourceName,
		"resource-sha3-384": digest,
		"resource-revision": componentRev.String(),
		"resource-size":     strconv.Itoa(int(size)),
		"developer-id":      s.dev1Acct.AccountID(),
		"timestamp":         time.Now().Format(time.RFC3339),
	}

	resourceRev, err := s.storeSigning.Sign(asserts.SnapResourceRevisionType, revHeaders, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(resourceRev)
	c.Assert(err, IsNil)

	pairHeaders := map[string]interface{}{
		"snap-id":           snapID,
		"resource-name":     resourceName,
		"resource-revision": componentRev.String(),
		"snap-revision":     snapRev.String(),
		"developer-id":      s.dev1Acct.AccountID(),
		"timestamp":         time.Now().Format(time.RFC3339),
	}

	resourcePair, err := s.storeSigning.Sign(asserts.SnapResourcePairType, pairHeaders, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(resourcePair)
	c.Assert(err, IsNil)

	si := &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(12),
	}

	csi := &snap.ComponentSideInfo{
		Component: naming.NewComponentRef("snap", "test-component"),
		Revision:  snap.R(24),
	}

	// everything cross checks, with the regular snap name
	err = snapasserts.CrossCheckResource(resourceName, digest, "", size, csi, si, nil, s.localDB)
	c.Assert(err, IsNil)
}

func (s *snapassertsSuite) TestCrossCheckResourceMissingRevisionAssertion(c *C) {
	s.testCrossCheckResourceMissingRevisionAssertion(c, "")
}

func (s *snapassertsSuite) TestCrossCheckResourceProvenanceMissingRevisionAssertion(c *C) {
	s.testCrossCheckResourceMissingRevisionAssertion(c, "prov")
}

func (s *snapassertsSuite) testCrossCheckResourceMissingRevisionAssertion(c *C, provenance string) {
	digest := makeDigest(12)
	const resourceName = "test-component"

	si := &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(12),
	}

	csi := &snap.ComponentSideInfo{
		Component: naming.NewComponentRef("snap", "test-component"),
		Revision:  snap.R(24),
	}

	err := snapasserts.CrossCheckResource(resourceName, digest, provenance, uint64(1024), csi, si, nil, s.localDB)
	c.Assert(err, NotNil)

	var suffix string
	if provenance != "" {
		suffix = fmt.Sprintf(" provenance: %s", provenance)
	}
	c.Assert(err, ErrorMatches, fmt.Sprintf("internal error: cannot find pre-populated snap-resource-revision assertion for %q: %s%s", resourceName, digest, suffix))
}

func (s *snapassertsSuite) TestCrossCheckResourceProvenance(c *C) {
	snapRev := snap.R(12)
	const (
		snapID     = "snap-id-1"
		provenance = "prov"
	)

	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      snapID,
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"revision":     "1",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": s.dev1Acct.AccountID(),
				"provenance": []interface{}{
					provenance,
				},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapDecl)
	c.Assert(err, IsNil)

	digest := makeDigest(12)
	componentRev := snap.R(24)
	const (
		size         = uint64(1024)
		resourceName = "test-component"
	)

	revHeaders := map[string]interface{}{
		"authority-id":      s.dev1Acct.AccountID(),
		"snap-id":           snapID,
		"resource-name":     resourceName,
		"resource-sha3-384": digest,
		"resource-revision": componentRev.String(),
		"resource-size":     strconv.Itoa(int(size)),
		"developer-id":      s.dev1Acct.AccountID(),
		"timestamp":         time.Now().Format(time.RFC3339),
		"provenance":        provenance,
	}

	resourceRev, err := s.dev1Signing.Sign(asserts.SnapResourceRevisionType, revHeaders, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(resourceRev)
	c.Assert(err, IsNil)

	pairHeaders := map[string]interface{}{
		"authority-id":      s.dev1Acct.AccountID(),
		"snap-id":           snapID,
		"resource-name":     resourceName,
		"resource-revision": componentRev.String(),
		"snap-revision":     snapRev.String(),
		"developer-id":      s.dev1Acct.AccountID(),
		"timestamp":         time.Now().Format(time.RFC3339),
		"provenance":        provenance,
	}

	resourcePair, err := s.dev1Signing.Sign(asserts.SnapResourcePairType, pairHeaders, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(resourcePair)
	c.Assert(err, IsNil)

	si := &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(12),
	}

	csi := &snap.ComponentSideInfo{
		Component: naming.NewComponentRef("snap", "test-component"),
		Revision:  snap.R(24),
	}

	err = snapasserts.CrossCheckResource(resourceName, digest, provenance, size, csi, si, nil, s.localDB)
	c.Assert(err, IsNil)

	// update the snap-decl with a mismatch provenance and check that the cross
	// check fails
	snapDecl, err = s.storeSigning.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      snapID,
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"revision":     "2",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": s.dev1Acct.AccountID(),
				"provenance": []interface{}{
					"new-prov",
				},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.localDB.Add(snapDecl)
	c.Assert(err, IsNil)

	err = snapasserts.CrossCheckResource(resourceName, digest, provenance, size, csi, si, nil, s.localDB)
	c.Assert(err, ErrorMatches, `snap resource \"test-component\" revision assertion with provenance \"prov\" is not signed by an authority authorized on this device:.*`)
}

func (s *snapassertsSuite) TestCrossCheckResourceErrors(c *C) {
	digest := makeDigest(12)
	componentRev := snap.R(24)
	snapRev := snap.R(12)
	const (
		size         = uint64(1024)
		resourceName = "test-component"
		snapID       = "snap-id-1"
	)

	originalRevHeaders := map[string]interface{}{
		"snap-id":           snapID,
		"resource-name":     resourceName,
		"resource-sha3-384": digest,
		"resource-revision": componentRev.String(),
		"resource-size":     strconv.Itoa(int(size)),
		"developer-id":      s.dev1Acct.AccountID(),
		"timestamp":         time.Now().Format(time.RFC3339),
	}

	originalPairHeaders := map[string]interface{}{
		"snap-id":           snapID,
		"resource-name":     resourceName,
		"resource-revision": componentRev.String(),
		"snap-revision":     snapRev.String(),
		"developer-id":      s.dev1Acct.AccountID(),
		"timestamp":         time.Now().Format(time.RFC3339),
	}

	headers := map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": s.dev1Acct.AccountID(),
		"timestamp":    time.Now().Format(time.RFC3339),
	}
	snapDecl, err := s.storeSigning.Sign(asserts.SnapDeclarationType, headers, nil, "")
	c.Assert(err, IsNil)

	type test struct {
		revisionOverrides map[string]interface{}
		pairOverrides     map[string]interface{}
		err               string
	}

	tests := []test{
		{
			revisionOverrides: map[string]interface{}{
				"resource-size": "1023",
			},
			err: `resource \"test-component\" file does not have expected size according to signatures \(download is broken or tampered\): 1024 != 1023`,
		},
		{
			revisionOverrides: map[string]interface{}{
				"resource-revision": "25",
			},
			err: `resource \"test-component\" does not have expected revision according to assertions \(metadata is broken or tampered\): 24 != 25`,
		},
		{
			pairOverrides: map[string]interface{}{
				"resource-revision": "25",
			},
			err: `cannot find snap-resource-pair for test-component: snap-resource-pair assertion not found`,
		},
		{
			revisionOverrides: map[string]interface{}{
				"resource-name": "nope",
			},
			err: `internal error: cannot find pre-populated snap-resource-revision assertion for \"test-component\": .*`,
		},
	}

	for _, t := range tests {
		localDB, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
			Backstore: asserts.NewMemoryBackstore(),
			Trusted:   s.storeSigning.Trusted,
		})
		c.Assert(err, IsNil)

		err = localDB.Add(s.storeSigning.StoreAccountKey(""))
		c.Assert(err, IsNil)
		err = localDB.Add(s.dev1Acct)
		c.Assert(err, IsNil)

		revHeaders := copyMapWithOverrides(originalRevHeaders, t.revisionOverrides)
		pairHeaders := copyMapWithOverrides(originalPairHeaders, t.pairOverrides)

		err = localDB.Add(snapDecl)
		c.Assert(err, IsNil)

		resourceRev, err := s.storeSigning.Sign(asserts.SnapResourceRevisionType, revHeaders, nil, "")
		c.Assert(err, IsNil)
		err = localDB.Add(resourceRev)
		c.Assert(err, IsNil)

		resourcePair, err := s.storeSigning.Sign(asserts.SnapResourcePairType, pairHeaders, nil, "")
		c.Assert(err, IsNil)
		err = localDB.Add(resourcePair)
		c.Assert(err, IsNil)

		si := &snap.SideInfo{
			SnapID:   "snap-id-1",
			Revision: snap.R(12),
		}

		csi := &snap.ComponentSideInfo{
			Component: naming.NewComponentRef("snap", "test-component"),
			Revision:  snap.R(24),
		}

		// everything cross checks, with the regular snap name
		err = snapasserts.CrossCheckResource(resourceName, digest, "", size, csi, si, nil, localDB)
		c.Assert(err, ErrorMatches, t.err)
	}
}

func copyMapWithOverrides(m map[string]interface{}, overrides map[string]interface{}) map[string]interface{} {
	c := make(map[string]interface{}, len(m))
	for k, v := range m {
		c[k] = v
	}
	for k, v := range overrides {
		c[k] = v
	}
	return c
}
