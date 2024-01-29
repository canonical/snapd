// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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
	"strings"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

var (
	_ = Suite(&snapResourceRevSuite{})
	_ = Suite(&snapResourcePairSuite{})
)

type snapResourceRevSuite struct {
	ts     time.Time
	tsLine string
}

func (s *snapResourceRevSuite) SetUpSuite(c *C) {
	s.ts = time.Now().Truncate(time.Second).UTC()
	s.tsLine = "timestamp: " + s.ts.Format(time.RFC3339) + "\n"
}

func (s *snapResourceRevSuite) makeValidEncoded() string {
	return "type: snap-resource-revision\n" +
		"authority-id: store-id1\n" +
		"snap-id: snap-id-1\n" +
		"resource-name: comp-name\n" +
		"resource-sha3-384: " + blobSHA3_384 + "\n" +
		"resource-revision: 4\n" +
		"resource-size: 127\n" +
		"developer-id: dev-id1\n" +
		"revision: 1\n" +
		s.tsLine +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
}

func makeSnapResourceRevisionHeaders(overrides map[string]interface{}) map[string]interface{} {
	headers := map[string]interface{}{
		"authority-id":      "canonical",
		"snap-id":           "snap-id-1",
		"resource-name":     "comp-name",
		"resource-sha3-384": blobSHA3_384,
		"resource-size":     "127",
		"resource-revision": "4",
		"developer-id":      "dev-id1",
		"revision":          "1",
		"timestamp":         time.Now().Format(time.RFC3339),
	}
	for k, v := range overrides {
		headers[k] = v
	}
	return headers
}

func (s *snapResourceRevSuite) makeHeaders(overrides map[string]interface{}) map[string]interface{} {
	return makeSnapResourceRevisionHeaders(overrides)
}

func (s *snapResourceRevSuite) TestDecodeOK(c *C) {
	encoded := s.makeValidEncoded()
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapResourceRevisionType)
	snapResourceRev := a.(*asserts.SnapResourceRevision)
	c.Check(snapResourceRev.AuthorityID(), Equals, "store-id1")
	c.Check(snapResourceRev.Timestamp(), Equals, s.ts)
	c.Check(snapResourceRev.SnapID(), Equals, "snap-id-1")
	c.Check(snapResourceRev.ResourceName(), Equals, "comp-name")
	c.Check(snapResourceRev.ResourceSHA3_384(), Equals, blobSHA3_384)
	c.Check(snapResourceRev.ResourceSize(), Equals, uint64(127))
	c.Check(snapResourceRev.ResourceRevision(), Equals, 4)
	c.Check(snapResourceRev.DeveloperID(), Equals, "dev-id1")
	c.Check(snapResourceRev.Revision(), Equals, 1)
	c.Check(snapResourceRev.Provenance(), Equals, "global-upload")
}

func (s *snapResourceRevSuite) TestDecodeOKWithProvenance(c *C) {
	encoded := s.makeValidEncoded()
	encoded = strings.Replace(encoded, "snap-id: snap-id-1", "provenance: foo\nsnap-id: snap-id-1", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapResourceRevisionType)
	snapResourceRev := a.(*asserts.SnapResourceRevision)
	c.Check(snapResourceRev.AuthorityID(), Equals, "store-id1")
	c.Check(snapResourceRev.Timestamp(), Equals, s.ts)
	c.Check(snapResourceRev.SnapID(), Equals, "snap-id-1")
	c.Check(snapResourceRev.ResourceName(), Equals, "comp-name")
	c.Check(snapResourceRev.ResourceSHA3_384(), Equals, blobSHA3_384)
	c.Check(snapResourceRev.ResourceSize(), Equals, uint64(127))
	c.Check(snapResourceRev.ResourceRevision(), Equals, 4)
	c.Check(snapResourceRev.DeveloperID(), Equals, "dev-id1")
	c.Check(snapResourceRev.Revision(), Equals, 1)
	c.Check(snapResourceRev.Provenance(), Equals, "foo")
}

const (
	snapResourceRevErrPrefix = "assertion snap-resource-revision: "
)

func (s *snapResourceRevSuite) TestDecodeInvalid(c *C) {
	encoded := s.makeValidEncoded()

	digestHdr := "resource-sha3-384: " + blobSHA3_384 + "\n"
	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"snap-id: snap-id-1\n", "", `"snap-id" header is mandatory`},
		{"snap-id: snap-id-1\n", "snap-id: \n", `"snap-id" header should not be empty`},
		{"resource-name: comp-name\n", "", `"resource-name" header is mandatory`},
		{"resource-name: comp-name\n", "resource-name: \n", `"resource-name" header should not be empty`},
		{"resource-name: comp-name\n", "resource-name: --comp-name\n", `invalid resource name "--comp-name"`},
		{digestHdr, "", `"resource-sha3-384" header is mandatory`},
		{digestHdr, "resource-sha3-384: \n", `"resource-sha3-384" header should not be empty`},
		{digestHdr, "resource-sha3-384: #\n", `"resource-sha3-384" header cannot be decoded:.*`},
		{digestHdr, "resource-sha3-384: eHl6\n", `"resource-sha3-384" header does not have the expected bit length: 24`},
		{"snap-id: snap-id-1\n", "provenance: \nsnap-id: snap-id-1\n", `"provenance" header should not be empty`},
		{"snap-id: snap-id-1\n", "provenance: *\nsnap-id: snap-id-1\n", `"provenance" header contains invalid characters: "\*"`},
		{"resource-size: 127\n", "", `"resource-size" header is mandatory`},
		{"resource-size: 127\n", "resource-size: \n", `"resource-size" header should not be empty`},
		{"resource-size: 127\n", "resource-size: -1\n", `"resource-size" header is not an unsigned integer: -1`},
		{"resource-size: 127\n", "resource-size: zzz\n", `"resource-size" header is not an unsigned integer: zzz`},
		{"resource-revision: 4\n", "", `"resource-revision" header is mandatory`},
		{"resource-revision: 4\n", "resource-revision: \n", `"resource-revision" header should not be empty`},
		{"resource-revision: 4\n", "resource-revision: -1\n", `"resource-revision" header must be >=1: -1`},
		{"resource-revision: 4\n", "resource-revision: 0\n", `"resource-revision" header must be >=1: 0`},
		{"resource-revision: 4\n", "resource-revision: zzz\n", `"resource-revision" header is not an integer: zzz`},
		{"developer-id: dev-id1\n", "", `"developer-id" header is mandatory`},
		{"developer-id: dev-id1\n", "developer-id: \n", `"developer-id" header should not be empty`},
		{s.tsLine, "", `"timestamp" header is mandatory`},
		{s.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{s.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, snapResourceRevErrPrefix+test.expectedErr)
	}
}

func (s *snapResourceRevSuite) TestPrerequisites(c *C) {
	encoded := s.makeValidEncoded()
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	prereqs := a.Prerequisites()
	c.Assert(prereqs, HasLen, 2)
	c.Check(prereqs[0], DeepEquals, &asserts.Ref{
		Type:       asserts.SnapDeclarationType,
		PrimaryKey: []string{"16", "snap-id-1"},
	})
	c.Check(prereqs[1], DeepEquals, &asserts.Ref{
		Type:       asserts.AccountType,
		PrimaryKey: []string{"dev-id1"},
	})
}

func (s *snapResourceRevSuite) TestPrimaryKey(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)
	prereqSnapDecl(c, storeDB, db)

	headers := s.makeHeaders(nil)
	snapResRev, err := storeDB.Sign(asserts.SnapResourceRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(snapResRev)
	c.Assert(err, IsNil)

	_, err = db.Find(asserts.SnapResourceRevisionType, map[string]string{
		"snap-id":           "snap-id-1",
		"resource-name":     "comp-name",
		"resource-sha3-384": blobSHA3_384,
	})
	c.Assert(err, IsNil)
}

func (s *snapResourceRevSuite) TestCheckMissingDeveloperAccount(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	headers := s.makeHeaders(nil)
	snapResRev, err := storeDB.Sign(asserts.SnapResourceRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapResRev)
	c.Assert(err, ErrorMatches, `snap-resource-revision assertion for snap id "snap-id-1" does not have a matching account assertion for the developer "dev-id1"`)
}

func (s *snapResourceRevSuite) TestCheckMissingDeclaration(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)

	headers := s.makeHeaders(nil)
	snapResRev, err := storeDB.Sign(asserts.SnapResourceRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapResRev)
	c.Assert(err, ErrorMatches, `snap-resource-revision assertion for snap id "snap-id-1" does not have a matching snap-declaration assertion`)
}

func (s *snapResourceRevSuite) TestCheckUntrustedAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	headers := s.makeHeaders(map[string]interface{}{
		"authority-id": "other",
	})
	snapResRev, err := otherDB.Sign(asserts.SnapResourceRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapResRev)
	c.Assert(err, ErrorMatches, `snap-resource-revision assertion for snap id "snap-id-1" is not signed by a store:.*`)
}

func (s *snapResourceRevSuite) TestRevisionAuthorityCheck(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	delegatedDB := setup3rdPartySigning(c, "delegated-id", storeDB, db)
	headers := s.makeHeaders(map[string]interface{}{
		"authority-id":      "delegated-id",
		"developer-id":      "delegated-id",
		"resource-revision": "200",
		"provenance":        "prov1",
	})
	a, err := delegatedDB.Sign(asserts.SnapResourceRevisionType, headers, nil, "")
	c.Assert(err, IsNil)
	snapResRev := a.(*asserts.SnapResourceRevision)

	tests := []struct {
		revAuth asserts.RevisionAuthority
		err     string
	}{
		{asserts.RevisionAuthority{
			AccountID:   "delegated-id",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
		}, ""},
		{asserts.RevisionAuthority{
			AccountID:   "delegated-id",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
			MaxRevision: 1000,
		}, ""},
		{asserts.RevisionAuthority{
			AccountID:   "delegated-id",
			Provenance:  []string{"prov2"},
			MinRevision: 1,
			MaxRevision: 1000,
		}, "provenance mismatch"},
		{asserts.RevisionAuthority{
			AccountID:   "delegated-id-2",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1,
			MaxRevision: 1000,
		}, "authority-id mismatch"},
		{asserts.RevisionAuthority{
			AccountID:   "delegated-id",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 1000,
		}, "resource revision 200 is less than min-revision 1000"},
		{asserts.RevisionAuthority{
			AccountID:   "delegated-id",
			Provenance:  []string{"prov1", "prov2"},
			MinRevision: 10,
			MaxRevision: 110,
		}, "resource revision 200 is greater than max-revision 110"},
	}

	for _, t := range tests {
		err := t.revAuth.CheckResourceRevision(snapResRev, nil, nil)
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *snapResourceRevSuite) TestSnapResourceRevisionDelegation(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	delegatedDB := setup3rdPartySigning(c, "delegated-id", storeDB, db)

	snapDecl, err := storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "delegated-id",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(snapDecl)
	c.Assert(err, IsNil)

	headers := s.makeHeaders(map[string]interface{}{
		"authority-id": "delegated-id",
		"developer-id": "delegated-id",
		"provenance":   "prov1",
	})
	snapResRev, err := delegatedDB.Sign(asserts.SnapResourceRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapResRev)
	c.Check(err, ErrorMatches, `snap-resource-revision assertion with provenance "prov1" for snap id "snap-id-1" is not signed by an authorized authority: delegated-id`)

	// establish delegation
	snapDecl, err = storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "delegated-id",
		"revision":     "1",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": "delegated-id",
				"provenance": []interface{}{
					"prov1",
				},
				// present but not checked at this level
				"on-store": []interface{}{
					"store1",
				},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(snapDecl)
	c.Assert(err, IsNil)

	// now revision should be accepted
	err = db.Check(snapResRev)
	c.Check(err, IsNil)
}

func (s *snapResourceRevSuite) TestSnapResourceRevisionDelegationRevisionOutOfRange(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	delegatedDB := setup3rdPartySigning(c, "delegated-id", storeDB, db)

	// establish delegation
	snapDecl, err := storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "delegated-id",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": "delegated-id",
				"provenance": []interface{}{
					"prov1",
				},
				// present but not checked at this level
				"on-store": []interface{}{
					"store1",
				},
				"max-revision": "200",
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(snapDecl)
	c.Assert(err, IsNil)

	headers := s.makeHeaders(map[string]interface{}{
		"authority-id":      "delegated-id",
		"developer-id":      "delegated-id",
		"provenance":        "prov1",
		"resource-revision": "1000",
	})
	snapResRev, err := delegatedDB.Sign(asserts.SnapResourceRevisionType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapResRev)
	c.Check(err, ErrorMatches, `snap-resource-revision assertion with provenance "prov1" for snap id "snap-id-1" is not signed by an authorized authority: delegated-id`)
}

type snapResourcePairSuite struct {
	ts     time.Time
	tsLine string
}

func (s *snapResourcePairSuite) SetUpSuite(c *C) {
	s.ts = time.Now().Truncate(time.Second).UTC()
	s.tsLine = "timestamp: " + s.ts.Format(time.RFC3339) + "\n"
}

func (s *snapResourcePairSuite) makeValidEncoded() string {
	return "type: snap-resource-pair\n" +
		"authority-id: store-id1\n" +
		"snap-id: snap-id-1\n" +
		"resource-name: comp-name\n" +
		"resource-revision: 4\n" +
		"snap-revision: 20\n" +
		"developer-id: dev-id1\n" +
		"revision: 1\n" +
		s.tsLine +
		"body-length: 0\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
}

func (s *snapResourcePairSuite) makeHeaders(overrides map[string]interface{}) map[string]interface{} {
	headers := map[string]interface{}{
		"authority-id":      "canonical",
		"snap-id":           "snap-id-1",
		"resource-name":     "comp-name",
		"resource-revision": "4",
		"snap-revision":     "20",
		"developer-id":      "dev-id1",
		"revision":          "1",
		"timestamp":         time.Now().Format(time.RFC3339),
	}
	for k, v := range overrides {
		headers[k] = v
	}
	return headers
}

func (s *snapResourcePairSuite) TestDecodeOK(c *C) {
	encoded := s.makeValidEncoded()
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapResourcePairType)
	snapResourcePair := a.(*asserts.SnapResourcePair)
	c.Check(snapResourcePair.AuthorityID(), Equals, "store-id1")
	c.Check(snapResourcePair.Timestamp(), Equals, s.ts)
	c.Check(snapResourcePair.SnapID(), Equals, "snap-id-1")
	c.Check(snapResourcePair.ResourceName(), Equals, "comp-name")
	c.Check(snapResourcePair.ResourceRevision(), Equals, 4)
	c.Check(snapResourcePair.SnapRevision(), Equals, 20)
	c.Check(snapResourcePair.DeveloperID(), Equals, "dev-id1")
	c.Check(snapResourcePair.Revision(), Equals, 1)
	c.Check(snapResourcePair.Provenance(), Equals, "global-upload")
}

func (s *snapResourcePairSuite) TestDecodeOKWithProvenance(c *C) {
	encoded := s.makeValidEncoded()
	encoded = strings.Replace(encoded, "snap-id: snap-id-1", "provenance: foo\nsnap-id: snap-id-1", 1)
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	c.Check(a.Type(), Equals, asserts.SnapResourcePairType)
	snapResourcePair := a.(*asserts.SnapResourcePair)
	c.Check(snapResourcePair.AuthorityID(), Equals, "store-id1")
	c.Check(snapResourcePair.Timestamp(), Equals, s.ts)
	c.Check(snapResourcePair.SnapID(), Equals, "snap-id-1")
	c.Check(snapResourcePair.ResourceName(), Equals, "comp-name")
	c.Check(snapResourcePair.ResourceRevision(), Equals, 4)
	c.Check(snapResourcePair.SnapRevision(), Equals, 20)
	c.Check(snapResourcePair.DeveloperID(), Equals, "dev-id1")
	c.Check(snapResourcePair.Revision(), Equals, 1)
	c.Check(snapResourcePair.Provenance(), Equals, "foo")
}

const (
	snapResourcePairErrPrefix = "assertion snap-resource-pair: "
)

func (s *snapResourcePairSuite) TestDecodeInvalid(c *C) {
	encoded := s.makeValidEncoded()

	invalidTests := []struct{ original, invalid, expectedErr string }{
		{"snap-id: snap-id-1\n", "", `"snap-id" header is mandatory`},
		{"snap-id: snap-id-1\n", "snap-id: \n", `"snap-id" header should not be empty`},
		{"resource-name: comp-name\n", "", `"resource-name" header is mandatory`},
		{"resource-name: comp-name\n", "resource-name: \n", `"resource-name" header should not be empty`},
		{"resource-name: comp-name\n", "resource-name: --comp-name\n", `invalid resource name "--comp-name"`},
		{"snap-id: snap-id-1\n", "provenance: \nsnap-id: snap-id-1\n", `"provenance" header should not be empty`},
		{"snap-id: snap-id-1\n", "provenance: *\nsnap-id: snap-id-1\n", `"provenance" header contains invalid characters: "\*"`},
		{"resource-revision: 4\n", "", `"resource-revision" header is mandatory`},
		{"resource-revision: 4\n", "resource-revision: \n", `"resource-revision" header should not be empty`},
		{"resource-revision: 4\n", "resource-revision: -1\n", `"resource-revision" header must be >=1: -1`},
		{"resource-revision: 4\n", "resource-revision: 0\n", `"resource-revision" header must be >=1: 0`},
		{"resource-revision: 4\n", "resource-revision: zzz\n", `"resource-revision" header is not an integer: zzz`},
		{"snap-revision: 20\n", "", `"snap-revision" header is mandatory`},
		{"snap-revision: 20\n", "snap-revision: \n", `"snap-revision" header should not be empty`},
		{"snap-revision: 20\n", "snap-revision: -1\n", `"snap-revision" header must be >=1: -1`},
		{"snap-revision: 20\n", "snap-revision: 0\n", `"snap-revision" header must be >=1: 0`},
		{"snap-revision: 20\n", "snap-revision: zzz\n", `"snap-revision" header is not an integer: zzz`},
		{"developer-id: dev-id1\n", "", `"developer-id" header is mandatory`},
		{"developer-id: dev-id1\n", "developer-id: \n", `"developer-id" header should not be empty`},
		{s.tsLine, "", `"timestamp" header is mandatory`},
		{s.tsLine, "timestamp: \n", `"timestamp" header should not be empty`},
		{s.tsLine, "timestamp: 12:30\n", `"timestamp" header is not a RFC3339 date: .*`},
	}

	for _, test := range invalidTests {
		invalid := strings.Replace(encoded, test.original, test.invalid, 1)
		_, err := asserts.Decode([]byte(invalid))
		c.Check(err, ErrorMatches, snapResourcePairErrPrefix+test.expectedErr)
	}
}

func (s *snapResourcePairSuite) TestPrerequisites(c *C) {
	encoded := s.makeValidEncoded()
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	prereqs := a.Prerequisites()
	c.Assert(prereqs, HasLen, 1)
	c.Check(prereqs[0], DeepEquals, &asserts.Ref{
		Type:       asserts.SnapDeclarationType,
		PrimaryKey: []string{"16", "snap-id-1"},
	})
}

func (s *snapResourcePairSuite) TestPrimaryKey(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)
	prereqSnapDecl(c, storeDB, db)

	headers := s.makeHeaders(nil)
	snapResPair, err := storeDB.Sign(asserts.SnapResourcePairType, headers, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(snapResPair)
	c.Assert(err, IsNil)

	_, err = db.Find(asserts.SnapResourcePairType, map[string]string{
		"snap-id":           "snap-id-1",
		"resource-name":     "comp-name",
		"resource-revision": "4",
		"snap-revision":     "20",
	})
	c.Assert(err, IsNil)
}

func (s *snapResourcePairSuite) TestCheckMissingDeveloperAccount(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	headers := s.makeHeaders(nil)
	snapResPair, err := storeDB.Sign(asserts.SnapResourcePairType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapResPair)
	c.Assert(err, ErrorMatches, `snap-resource-pair assertion for snap id "snap-id-1" does not have a matching account assertion for the developer "dev-id1"`)
}

func (s *snapResourcePairSuite) TestCheckMissingDeclaration(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	prereqDevAccount(c, storeDB, db)

	headers := s.makeHeaders(nil)
	snapResPair, err := storeDB.Sign(asserts.SnapResourcePairType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapResPair)
	c.Assert(err, ErrorMatches, `snap-resource-pair assertion for snap id "snap-id-1" does not have a matching snap-declaration assertion`)
}

func (s *snapResourcePairSuite) TestCheckUntrustedAuthority(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	otherDB := setup3rdPartySigning(c, "other", storeDB, db)

	headers := s.makeHeaders(map[string]interface{}{
		"authority-id": "other",
	})
	snapResPair, err := otherDB.Sign(asserts.SnapResourcePairType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapResPair)
	c.Assert(err, ErrorMatches, `snap-resource-pair assertion for snap id "snap-id-1" is not signed by a store:.*`)
}

func (s *snapResourcePairSuite) TestDelegation(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	delegatedDB := setup3rdPartySigning(c, "delegated-id", storeDB, db)

	snapDecl, err := storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "delegated-id",
		"timestamp":    time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(snapDecl)
	c.Assert(err, IsNil)

	headers := s.makeHeaders(map[string]interface{}{
		"authority-id": "delegated-id",
		"developer-id": "delegated-id",
		"provenance":   "prov1",
	})
	snapResPair, err := delegatedDB.Sign(asserts.SnapResourcePairType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapResPair)
	c.Check(err, ErrorMatches, `snap-resource-pair assertion with provenance "prov1" for snap id "snap-id-1" is not signed by an authorized authority: delegated-id`)

	// establish delegation
	snapDecl, err = storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "delegated-id",
		"revision":     "1",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": "delegated-id",
				"provenance": []interface{}{
					"prov1",
				},
				// present but not checked at this level
				"on-store": []interface{}{
					"store1",
				},
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(snapDecl)
	c.Assert(err, IsNil)

	// now revision should be accepted
	err = db.Check(snapResPair)
	c.Check(err, IsNil)
}

func (s *snapResourcePairSuite) TestDelegationRevisionOutOfRange(c *C) {
	storeDB, db := makeStoreAndCheckDB(c)

	delegatedDB := setup3rdPartySigning(c, "delegated-id", storeDB, db)

	// establish delegation
	snapDecl, err := storeDB.Sign(asserts.SnapDeclarationType, map[string]interface{}{
		"series":       "16",
		"snap-id":      "snap-id-1",
		"snap-name":    "foo",
		"publisher-id": "delegated-id",
		"revision-authority": []interface{}{
			map[string]interface{}{
				"account-id": "delegated-id",
				"provenance": []interface{}{
					"prov1",
				},
				// present but not checked at this level
				"on-store": []interface{}{
					"store1",
				},
				"max-revision": "200",
			},
		},
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = db.Add(snapDecl)
	c.Assert(err, IsNil)

	headers := s.makeHeaders(map[string]interface{}{
		"authority-id":  "delegated-id",
		"developer-id":  "delegated-id",
		"provenance":    "prov1",
		"snap-revision": "1000",
	})
	snapResPair, err := delegatedDB.Sign(asserts.SnapResourcePairType, headers, nil, "")
	c.Assert(err, IsNil)

	err = db.Check(snapResPair)
	c.Check(err, ErrorMatches, `snap-resource-pair assertion with provenance "prov1" for snap id "snap-id-1" is not signed by an authorized authority: delegated-id`)
}
