// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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
	"os"
	"path/filepath"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

type fsBackstoreSuite struct{}

var _ = Suite(&fsBackstoreSuite{})

func (fsbss *fsBackstoreSuite) TestOpenOK(c *C) {
	// ensure umask is clean when creating the DB dir
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	topDir := filepath.Join(c.MkDir(), "asserts-db")

	bs, err := asserts.OpenFSBackstore(topDir)
	c.Check(err, IsNil)
	c.Check(bs, NotNil)

	info, err := os.Stat(filepath.Join(topDir, "asserts-v0"))
	c.Assert(err, IsNil)
	c.Assert(info.IsDir(), Equals, true)
	c.Check(info.Mode().Perm(), Equals, os.FileMode(0775))
}

func (fsbss *fsBackstoreSuite) TestOpenCreateFail(c *C) {
	parent := filepath.Join(c.MkDir(), "var")
	topDir := filepath.Join(parent, "asserts-db")
	// make it not writable
	err := os.Mkdir(parent, 0555)
	c.Assert(err, IsNil)

	bs, err := asserts.OpenFSBackstore(topDir)
	c.Assert(err, ErrorMatches, "cannot create assert storage root: .*")
	c.Check(bs, IsNil)
}

func (fsbss *fsBackstoreSuite) TestOpenWorldWritableFail(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	// make it world-writable
	oldUmask := syscall.Umask(0)
	os.MkdirAll(filepath.Join(topDir, "asserts-v0"), 0777)
	syscall.Umask(oldUmask)

	bs, err := asserts.OpenFSBackstore(topDir)
	c.Assert(err, ErrorMatches, "assert storage root unexpectedly world-writable: .*")
	c.Check(bs, IsNil)
}

func (fsbss *fsBackstoreSuite) TestPutOldRevision(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs, err := asserts.OpenFSBackstore(topDir)
	c.Assert(err, IsNil)

	// Create two revisions of assertion.
	a0, err := asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: foo\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)
	a1, err := asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: foo\n" +
		"revision: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)

	// Put newer revision, follwed by old revision.
	err = bs.Put(asserts.TestOnlyType, a1)
	c.Assert(err, IsNil)
	err = bs.Put(asserts.TestOnlyType, a0)

	c.Check(err, ErrorMatches, `revision 0 is older than current revision 1`)
	c.Check(err, DeepEquals, &asserts.RevisionError{Current: 1, Used: 0})
}

func (fsbss *fsBackstoreSuite) TestGetFormat(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs, err := asserts.OpenFSBackstore(topDir)
	c.Assert(err, IsNil)

	af0, err := asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: foo\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)
	af1, err := asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: foo\n" +
		"format: 1\n" +
		"revision: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)
	af2, err := asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: zoo\n" +
		"format: 2\n" +
		"revision: 22\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)

	err = bs.Put(asserts.TestOnlyType, af0)
	c.Assert(err, IsNil)
	err = bs.Put(asserts.TestOnlyType, af1)
	c.Assert(err, IsNil)

	a, err := bs.Get(asserts.TestOnlyType, []string{"foo"}, 1)
	c.Assert(err, IsNil)
	c.Check(a.Revision(), Equals, 1)

	a, err = bs.Get(asserts.TestOnlyType, []string{"foo"}, 0)
	c.Assert(err, IsNil)
	c.Check(a.Revision(), Equals, 0)

	a, err = bs.Get(asserts.TestOnlyType, []string{"zoo"}, 0)
	c.Assert(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
		// Headers can be omitted by Backstores
	})
	c.Check(a, IsNil)

	err = bs.Put(asserts.TestOnlyType, af2)
	c.Assert(err, IsNil)

	a, err = bs.Get(asserts.TestOnlyType, []string{"zoo"}, 1)
	c.Assert(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
	})
	c.Check(a, IsNil)

	a, err = bs.Get(asserts.TestOnlyType, []string{"zoo"}, 2)
	c.Assert(err, IsNil)
	c.Check(a.Revision(), Equals, 22)
}

func (fsbss *fsBackstoreSuite) TestSearchFormat(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs, err := asserts.OpenFSBackstore(topDir)
	c.Assert(err, IsNil)

	af0, err := asserts.Decode([]byte("type: test-only-2\n" +
		"authority-id: auth-id1\n" +
		"pk1: foo\n" +
		"pk2: bar\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)
	af1, err := asserts.Decode([]byte("type: test-only-2\n" +
		"authority-id: auth-id1\n" +
		"pk1: foo\n" +
		"pk2: bar\n" +
		"format: 1\n" +
		"revision: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)

	af2, err := asserts.Decode([]byte("type: test-only-2\n" +
		"authority-id: auth-id1\n" +
		"pk1: foo\n" +
		"pk2: baz\n" +
		"format: 2\n" +
		"revision: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)

	err = bs.Put(asserts.TestOnly2Type, af0)
	c.Assert(err, IsNil)

	queries := []map[string]string{
		{"pk1": "foo", "pk2": "bar"},
		{"pk1": "foo"},
		{"pk2": "bar"},
	}

	for _, q := range queries {
		var a asserts.Assertion
		foundCb := func(a1 asserts.Assertion) {
			a = a1
		}
		err := bs.Search(asserts.TestOnly2Type, q, foundCb, 1)
		c.Assert(err, IsNil)
		c.Check(a.Revision(), Equals, 0)
	}

	err = bs.Put(asserts.TestOnly2Type, af1)
	c.Assert(err, IsNil)

	for _, q := range queries {
		var a asserts.Assertion
		foundCb := func(a1 asserts.Assertion) {
			a = a1
		}
		err := bs.Search(asserts.TestOnly2Type, q, foundCb, 1)
		c.Assert(err, IsNil)
		c.Check(a.Revision(), Equals, 1)

		err = bs.Search(asserts.TestOnly2Type, q, foundCb, 0)
		c.Assert(err, IsNil)
		c.Check(a.Revision(), Equals, 0)
	}

	err = bs.Put(asserts.TestOnly2Type, af2)
	c.Assert(err, IsNil)

	var as []asserts.Assertion
	foundCb := func(a1 asserts.Assertion) {
		as = append(as, a1)
	}
	err = bs.Search(asserts.TestOnly2Type, map[string]string{
		"pk1": "foo",
	}, foundCb, 1) // will not find af2
	c.Assert(err, IsNil)
	c.Check(as, HasLen, 1)
	c.Check(as[0].Revision(), Equals, 1)

}

func (fsbss *fsBackstoreSuite) TestSequenceMemberAfter(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs, err := asserts.OpenFSBackstore(topDir)
	c.Assert(err, IsNil)

	other1, err := asserts.Decode([]byte("type: test-only-seq\n" +
		"authority-id: auth-id1\n" +
		"n: other\n" +
		"sequence: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)

	sq1f0, err := asserts.Decode([]byte("type: test-only-seq\n" +
		"authority-id: auth-id1\n" +
		"n: s1\n" +
		"sequence: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)

	sq2f0, err := asserts.Decode([]byte("type: test-only-seq\n" +
		"authority-id: auth-id1\n" +
		"n: s1\n" +
		"sequence: 2\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)

	sq2f1, err := asserts.Decode([]byte("type: test-only-seq\n" +
		"authority-id: auth-id1\n" +
		"format: 1\n" +
		"n: s1\n" +
		"sequence: 2\n" +
		"revision: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)

	sq3f1, err := asserts.Decode([]byte("type: test-only-seq\n" +
		"authority-id: auth-id1\n" +
		"format: 1\n" +
		"n: s1\n" +
		"sequence: 3\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)

	sq3f2, err := asserts.Decode([]byte("type: test-only-seq\n" +
		"authority-id: auth-id1\n" +
		"format: 2\n" +
		"n: s1\n" +
		"sequence: 3\n" +
		"revision: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)

	for _, a := range []asserts.Assertion{other1, sq1f0, sq2f0, sq2f1, sq3f1, sq3f2} {
		err = bs.Put(asserts.TestOnlySeqType, a)
		c.Assert(err, IsNil)
	}

	seqKey := []string{"s1"}
	tests := []struct {
		after     int
		maxFormat int
		sequence  int
		format    int
		revision  int
	}{
		{after: 0, maxFormat: 0, sequence: 1, format: 0, revision: 0},
		{after: 0, maxFormat: 2, sequence: 1, format: 0, revision: 0},
		{after: 1, maxFormat: 0, sequence: 2, format: 0, revision: 0},
		{after: 1, maxFormat: 1, sequence: 2, format: 1, revision: 1},
		{after: 1, maxFormat: 2, sequence: 2, format: 1, revision: 1},
		{after: 2, maxFormat: 0, sequence: -1},
		{after: 2, maxFormat: 1, sequence: 3, format: 1, revision: 0},
		{after: 2, maxFormat: 2, sequence: 3, format: 2, revision: 1},
		{after: 3, maxFormat: 0, sequence: -1},
		{after: 3, maxFormat: 2, sequence: -1},
		{after: 4, maxFormat: 2, sequence: -1},
		{after: -1, maxFormat: 0, sequence: 2, format: 0, revision: 0},
		{after: -1, maxFormat: 1, sequence: 3, format: 1, revision: 0},
		{after: -1, maxFormat: 2, sequence: 3, format: 2, revision: 1},
	}

	for _, t := range tests {
		a, err := bs.SequenceMemberAfter(asserts.TestOnlySeqType, seqKey, t.after, t.maxFormat)
		if t.sequence == -1 {
			c.Check(err, DeepEquals, &asserts.NotFoundError{
				Type: asserts.TestOnlySeqType,
			})
		} else {
			c.Assert(err, IsNil)
			c.Assert(a.HeaderString("n"), Equals, "s1")
			c.Check(a.Sequence(), Equals, t.sequence)
			c.Check(a.Format(), Equals, t.format)
			c.Check(a.Revision(), Equals, t.revision)
		}
	}

	_, err = bs.SequenceMemberAfter(asserts.TestOnlySeqType, []string{"s2"}, -1, 2)
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlySeqType,
	})
}
