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

package snapasserts_test

import (
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
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/snap"
)

func TestSnapasserts(t *testing.T) { TestingT(t) }

type snapassertsSuite struct {
	storeSigning *assertstest.StoreStack
	dev1Acct     *asserts.Account

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
	err = snapasserts.CrossCheck("foo", digest, size, si, s.localDB)
	c.Check(err, IsNil)
	// and a snap instance name
	err = snapasserts.CrossCheck("foo_instance", digest, size, si, s.localDB)
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
	err = snapasserts.CrossCheck("foo", digest, size+1, si, s.localDB)
	c.Check(err, ErrorMatches, fmt.Sprintf(`snap "foo" file does not have expected size according to signatures \(download is broken or tampered\): %d != %d`, size+1, size))
	err = snapasserts.CrossCheck("foo_instance", digest, size+1, si, s.localDB)
	c.Check(err, ErrorMatches, fmt.Sprintf(`snap "foo_instance" file does not have expected size according to signatures \(download is broken or tampered\): %d != %d`, size+1, size))

	// mismatched revision vs what we got from store original info
	err = snapasserts.CrossCheck("foo", digest, size, &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(21),
	}, s.localDB)
	c.Check(err, ErrorMatches, `snap "foo" does not have expected ID or revision according to assertions \(metadata is broken or tampered\): 21 / snap-id-1 != 12 / snap-id-1`)
	err = snapasserts.CrossCheck("foo_instance", digest, size, &snap.SideInfo{
		SnapID:   "snap-id-1",
		Revision: snap.R(21),
	}, s.localDB)
	c.Check(err, ErrorMatches, `snap "foo_instance" does not have expected ID or revision according to assertions \(metadata is broken or tampered\): 21 / snap-id-1 != 12 / snap-id-1`)

	// mismatched snap id vs what we got from store original info
	err = snapasserts.CrossCheck("foo", digest, size, &snap.SideInfo{
		SnapID:   "snap-id-other",
		Revision: snap.R(12),
	}, s.localDB)
	c.Check(err, ErrorMatches, `snap "foo" does not have expected ID or revision according to assertions \(metadata is broken or tampered\): 12 / snap-id-other != 12 / snap-id-1`)
	err = snapasserts.CrossCheck("foo_instance", digest, size, &snap.SideInfo{
		SnapID:   "snap-id-other",
		Revision: snap.R(12),
	}, s.localDB)
	c.Check(err, ErrorMatches, `snap "foo_instance" does not have expected ID or revision according to assertions \(metadata is broken or tampered\): 12 / snap-id-other != 12 / snap-id-1`)

	// changed name
	err = snapasserts.CrossCheck("baz", digest, size, si, s.localDB)
	c.Check(err, ErrorMatches, `cannot install "baz", snap "baz" is undergoing a rename to "foo"`)
	err = snapasserts.CrossCheck("baz_instance", digest, size, si, s.localDB)
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

	err = snapasserts.CrossCheck("foo", digest, size, si, s.localDB)
	c.Check(err, ErrorMatches, `cannot install snap "foo" with a revoked snap declaration`)
	err = snapasserts.CrossCheck("foo_instance", digest, size, si, s.localDB)
	c.Check(err, ErrorMatches, `cannot install snap "foo_instance" with a revoked snap declaration`)
}

func (s *snapassertsSuite) TestDeriveSideInfoHappy(c *C) {
	digest := makeDigest(42)
	size := uint64(len(fakeSnap(42)))
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

	tempdir := c.MkDir()
	snapPath := filepath.Join(tempdir, "anon.snap")
	err = ioutil.WriteFile(snapPath, fakeSnap(42), 0644)
	c.Assert(err, IsNil)

	si, err := snapasserts.DeriveSideInfo(snapPath, s.localDB)
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
	err := ioutil.WriteFile(snapPath, fakeSnap(42), 0644)
	c.Assert(err, IsNil)

	_, err = snapasserts.DeriveSideInfo(snapPath, s.localDB)
	// cannot find signatures with metadata for snap
	c.Assert(asserts.IsNotFound(err), Equals, true)
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
	err = ioutil.WriteFile(snapPath, fakeSnap(42), 0644)
	c.Assert(err, IsNil)

	_, err = snapasserts.DeriveSideInfo(snapPath, s.localDB)
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
	err = ioutil.WriteFile(snapPath, fakeSnap(42), 0644)
	c.Assert(err, IsNil)

	_, err = snapasserts.DeriveSideInfo(snapPath, s.localDB)
	c.Check(err, ErrorMatches, fmt.Sprintf(`cannot install snap %q with a revoked snap declaration`, snapPath))
}
