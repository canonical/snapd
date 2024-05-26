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

package asserts_test

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
)

type fsBackstoreSuite struct{}

var _ = Suite(&fsBackstoreSuite{})

func (fsbss *fsBackstoreSuite) TestOpenOK(c *C) {
	// ensure umask is clean when creating the DB dir
	oldUmask := syscall.Umask(0)
	defer syscall.Umask(oldUmask)

	topDir := filepath.Join(c.MkDir(), "asserts-db")

	bs := mylog.Check2(asserts.OpenFSBackstore(topDir))
	c.Check(err, IsNil)
	c.Check(bs, NotNil)

	info := mylog.Check2(os.Stat(filepath.Join(topDir, "asserts-v0")))

	c.Assert(info.IsDir(), Equals, true)
	c.Check(info.Mode().Perm(), Equals, os.FileMode(0775))
}

func (fsbss *fsBackstoreSuite) TestOpenCreateFail(c *C) {
	parent := filepath.Join(c.MkDir(), "var")
	topDir := filepath.Join(parent, "asserts-db")
	mylog.
		// make it not writable
		Check(os.Mkdir(parent, 0555))


	bs := mylog.Check2(asserts.OpenFSBackstore(topDir))
	c.Assert(err, ErrorMatches, "cannot create assert storage root: .*")
	c.Check(bs, IsNil)
}

func (fsbss *fsBackstoreSuite) TestOpenWorldWritableFail(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	// make it world-writable
	oldUmask := syscall.Umask(0)
	os.MkdirAll(filepath.Join(topDir, "asserts-v0"), 0777)
	syscall.Umask(oldUmask)

	bs := mylog.Check2(asserts.OpenFSBackstore(topDir))
	c.Assert(err, ErrorMatches, "assert storage root unexpectedly world-writable: .*")
	c.Check(bs, IsNil)
}

func (fsbss *fsBackstoreSuite) TestPutOldRevision(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs := mylog.Check2(asserts.OpenFSBackstore(topDir))


	// Create two revisions of assertion.
	a0 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: foo\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	a1 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: foo\n" +
		"revision: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.

		// Put newer revision, follwed by old revision.
		Check(bs.Put(asserts.TestOnlyType, a1))

	mylog.Check(bs.Put(asserts.TestOnlyType, a0))

	c.Check(err, ErrorMatches, `revision 0 is older than current revision 1`)
	c.Check(err, DeepEquals, &asserts.RevisionError{Current: 1, Used: 0})
}

func (fsbss *fsBackstoreSuite) TestGetFormat(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs := mylog.Check2(asserts.OpenFSBackstore(topDir))


	af0 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: foo\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	af1 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: foo\n" +
		"format: 1\n" +
		"revision: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	af2 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: zoo\n" +
		"format: 2\n" +
		"revision: 22\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, af0))

	mylog.Check(bs.Put(asserts.TestOnlyType, af1))


	a := mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"foo"}, 1))

	c.Check(a.Revision(), Equals, 1)

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"foo"}, 0))

	c.Check(a.Revision(), Equals, 0)

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"zoo"}, 0))
	c.Assert(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
		// Headers can be omitted by Backstores
	})
	c.Check(a, IsNil)
	mylog.Check(bs.Put(asserts.TestOnlyType, af2))


	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"zoo"}, 1))
	c.Assert(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
	})
	c.Check(a, IsNil)

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"zoo"}, 2))

	c.Check(a.Revision(), Equals, 22)
}

func (fsbss *fsBackstoreSuite) TestSearchFormat(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs := mylog.Check2(asserts.OpenFSBackstore(topDir))


	af0 := mylog.Check2(asserts.Decode([]byte("type: test-only-2\n" +
		"authority-id: auth-id1\n" +
		"pk1: foo\n" +
		"pk2: bar\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	af1 := mylog.Check2(asserts.Decode([]byte("type: test-only-2\n" +
		"authority-id: auth-id1\n" +
		"pk1: foo\n" +
		"pk2: bar\n" +
		"format: 1\n" +
		"revision: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))


	af2 := mylog.Check2(asserts.Decode([]byte("type: test-only-2\n" +
		"authority-id: auth-id1\n" +
		"pk1: foo\n" +
		"pk2: baz\n" +
		"format: 2\n" +
		"revision: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnly2Type, af0))


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
		mylog.Check(bs.Search(asserts.TestOnly2Type, q, foundCb, 1))

		c.Check(a.Revision(), Equals, 0)
	}
	mylog.Check(bs.Put(asserts.TestOnly2Type, af1))


	for _, q := range queries {
		var a asserts.Assertion
		foundCb := func(a1 asserts.Assertion) {
			a = a1
		}
		mylog.Check(bs.Search(asserts.TestOnly2Type, q, foundCb, 1))

		c.Check(a.Revision(), Equals, 1)
		mylog.Check(bs.Search(asserts.TestOnly2Type, q, foundCb, 0))

		c.Check(a.Revision(), Equals, 0)
	}
	mylog.Check(bs.Put(asserts.TestOnly2Type, af2))


	var as []asserts.Assertion
	foundCb := func(a1 asserts.Assertion) {
		as = append(as, a1)
	}
	mylog.Check(bs.Search(asserts.TestOnly2Type, map[string]string{
		"pk1": "foo",
	}, foundCb, 1)) // will not find af2

	c.Check(as, HasLen, 1)
	c.Check(as[0].Revision(), Equals, 1)
}

func (fsbss *fsBackstoreSuite) TestSequenceMemberAfter(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs := mylog.Check2(asserts.OpenFSBackstore(topDir))


	other1 := mylog.Check2(asserts.Decode([]byte("type: test-only-seq\n" +
		"authority-id: auth-id1\n" +
		"n: other\n" +
		"sequence: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))


	sq1f0 := mylog.Check2(asserts.Decode([]byte("type: test-only-seq\n" +
		"authority-id: auth-id1\n" +
		"n: s1\n" +
		"sequence: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))


	sq2f0 := mylog.Check2(asserts.Decode([]byte("type: test-only-seq\n" +
		"authority-id: auth-id1\n" +
		"n: s1\n" +
		"sequence: 2\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))


	sq2f1 := mylog.Check2(asserts.Decode([]byte("type: test-only-seq\n" +
		"authority-id: auth-id1\n" +
		"format: 1\n" +
		"n: s1\n" +
		"sequence: 2\n" +
		"revision: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))


	sq3f1 := mylog.Check2(asserts.Decode([]byte("type: test-only-seq\n" +
		"authority-id: auth-id1\n" +
		"format: 1\n" +
		"n: s1\n" +
		"sequence: 3\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))


	sq3f2 := mylog.Check2(asserts.Decode([]byte("type: test-only-seq\n" +
		"authority-id: auth-id1\n" +
		"format: 2\n" +
		"n: s1\n" +
		"sequence: 3\n" +
		"revision: 1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))


	for _, a := range []asserts.Assertion{other1, sq1f0, sq2f0, sq2f1, sq3f1, sq3f2} {
		mylog.Check(bs.Put(asserts.TestOnlySeqType, a))

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
		a := mylog.Check2(bs.SequenceMemberAfter(asserts.TestOnlySeqType, seqKey, t.after, t.maxFormat))
		if t.sequence == -1 {
			c.Check(err, DeepEquals, &asserts.NotFoundError{
				Type: asserts.TestOnlySeqType,
			})
		} else {

			c.Assert(a.HeaderString("n"), Equals, "s1")
			c.Check(a.Sequence(), Equals, t.sequence)
			c.Check(a.Format(), Equals, t.format)
			c.Check(a.Revision(), Equals, t.revision)
		}
	}

	_ = mylog.Check2(bs.SequenceMemberAfter(asserts.TestOnlySeqType, []string{"s2"}, -1, 2))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlySeqType,
	})
}

func (fsbss *fsBackstoreSuite) TestOptionalPrimaryKeys(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs := mylog.Check2(asserts.OpenFSBackstore(topDir))


	a1 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k1\n" +
		"marker: a1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a1))


	a := mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k1"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k1"})

	r := asserts.MockOptionalPrimaryKey(asserts.TestOnlyType, "opt1", "o1-defl")
	defer r()

	a2 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k2\n" +
		"marker: a2\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a2))

	a3 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k3\n" +
		"opt1: o1-a3\n" +
		"marker: a3\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a3))


	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k1"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k1", "o1-defl"})
	c.Check(a.HeaderString("marker"), Equals, "a1")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k1", "o1-defl"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k1", "o1-defl"})
	c.Check(a.HeaderString("marker"), Equals, "a1")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k2"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k2", "o1-defl"})
	c.Check(a.HeaderString("marker"), Equals, "a2")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k3", "o1-a3"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k3", "o1-a3"})
	c.Check(a.HeaderString("marker"), Equals, "a3")

	r2 := asserts.MockOptionalPrimaryKey(asserts.TestOnlyType, "opt2", "o2-defl")
	defer r()

	a4 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k4\n" +
		"marker: a4\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a4))

	a5 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k3\n" +
		"opt2: o2-a5\n" +
		"marker: a5\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a5))

	a6 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k5\n" +
		"opt1: o1-a6\n" +
		"opt2: o2-a6\n" +
		"marker: a6\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a6))


	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k1"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k1", "o1-defl", "o2-defl"})
	c.Check(a.HeaderString("marker"), Equals, "a1")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k1", "o1-defl"}, 0))

	c.Check(a.HeaderString("marker"), Equals, "a1")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k2", "o1-defl", "o2-defl"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k2", "o1-defl", "o2-defl"})
	c.Check(a.HeaderString("marker"), Equals, "a2")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k3", "o1-a3"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k3", "o1-a3", "o2-defl"})
	c.Check(a.HeaderString("marker"), Equals, "a3")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k4"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k4", "o1-defl", "o2-defl"})
	c.Check(a.HeaderString("marker"), Equals, "a4")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k3", "o1-defl", "o2-a5"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k3", "o1-defl", "o2-a5"})
	c.Check(a.HeaderString("marker"), Equals, "a5")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k5", "o1-a6", "o2-a6"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k5", "o1-a6", "o2-a6"})
	c.Check(a.HeaderString("marker"), Equals, "a6")

	// revert the previous type definition
	r2()

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k1"}, 0))

	c.Check(a.HeaderString("marker"), Equals, "a1")
	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k1", "o1-defl"})
	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k1", "o1-defl"}, 0))

	c.Check(a.HeaderString("marker"), Equals, "a1")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k2", "o1-defl"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k2", "o1-defl"})
	c.Check(a.HeaderString("marker"), Equals, "a2")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k3", "o1-a3"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k3", "o1-a3"})
	c.Check(a.HeaderString("marker"), Equals, "a3")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k4"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k4", "o1-defl"})
	c.Check(a.HeaderString("marker"), Equals, "a4")

	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k3", "o1-defl"}, 0))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
	})
	c.Check(a, IsNil)
	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k5", "o1-a6"}, 0))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
	})
	c.Check(a, IsNil)

	// revert to initial type definition
	r()
	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k1"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k1"})
	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k2"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k2"})
	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k3"}, 0))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
	})
	c.Check(a, IsNil)
	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k4"}, 0))

	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k4"})
	a = mylog.Check2(bs.Get(asserts.TestOnlyType, []string{"k5"}, 0))
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
	})
	c.Check(a, IsNil)
}

func (fsbss *fsBackstoreSuite) TestOptionalPrimaryKeysSearch(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs := mylog.Check2(asserts.OpenFSBackstore(topDir))


	a1 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k1\n" +
		"v: x\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a1))


	r := asserts.MockOptionalPrimaryKey(asserts.TestOnlyType, "opt1", "o1-defl")
	defer r()

	a2 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k1\n" +
		"opt1: A\n" +
		"v: y\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a2))


	a3 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k2\n" +
		"opt1: A\n" +
		"v: x\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a3))


	a4 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k3\n" +
		"opt1: B\n" +
		"v: y\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a4))


	a5 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k4\n" +
		"opt1: B\n" +
		"v: x\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a5))


	a6 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k3\n" +
		"v: y\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a6))


	var found map[string]string
	foundCb := func(a asserts.Assertion) {
		if found == nil {
			found = make(map[string]string)
		}
		found[strings.Join(a.Ref().PrimaryKey, "/")] = a.HeaderString("v")
	}
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"primary-key": "k1",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k1/o1-defl": "x",
		"k1/A":       "y",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"primary-key": "k3",
		"opt1":        "o1-defl",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k3/o1-defl": "y",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"opt1": "o1-defl",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k1/o1-defl": "x",
		"k3/o1-defl": "y",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"opt1": "A",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k1/A": "y",
		"k2/A": "x",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"opt1": "B",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k3/B": "y",
		"k4/B": "x",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"v": "x",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k1/o1-defl": "x",
		"k2/A":       "x",
		"k4/B":       "x",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"v": "y",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k1/A":       "y",
		"k3/B":       "y",
		"k3/o1-defl": "y",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, nil, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k1/o1-defl": "x",
		"k1/A":       "y",
		"k2/A":       "x",
		"k3/o1-defl": "y",
		"k3/B":       "y",
		"k4/B":       "x",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"primary-key": "k4",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k4/B": "x",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"primary-key": "k3",
		"opt1":        "B",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k3/B": "y",
	})

	// revert to initial type definition
	r()

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"primary-key": "k1",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k1": "x",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"primary-key": "k3",
		"opt1":        "o1-defl",
	}, foundCb, 0))

	// found nothing
	c.Check(found, IsNil)

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"v": "x",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k1": "x",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"v": "y",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k3": "y",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, nil, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k1": "x",
		"k3": "y",
	})
}

func (fsbss *fsBackstoreSuite) TestOptionalPrimaryKeysSearchTwoOptional(c *C) {
	topDir := filepath.Join(c.MkDir(), "asserts-db")
	bs := mylog.Check2(asserts.OpenFSBackstore(topDir))


	a1 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k1\n" +
		"v: x\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a1))


	a2 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k2\n" +
		"v: x\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a2))


	r := asserts.MockOptionalPrimaryKey(asserts.TestOnlyType, "opt1", "o1-defl")
	defer r()
	asserts.MockOptionalPrimaryKey(asserts.TestOnlyType, "opt2", "o2-defl")

	a3 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k1\n" +
		"opt1: A\n" +
		"v: y\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a3))


	a4 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k2\n" +
		"opt2: B\n" +
		"v: y\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a4))


	a5 := mylog.Check2(asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k2\n" +
		"opt1: A2\n" +
		"opt2: B2\n" +
		"v: x\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw==")))

	mylog.Check(bs.Put(asserts.TestOnlyType, a5))


	var found map[string]string
	foundCb := func(a asserts.Assertion) {
		if found == nil {
			found = make(map[string]string)
		}
		found[strings.Join(a.Ref().PrimaryKey, "/")] = a.HeaderString("v")
	}
	mylog.Check(bs.Search(asserts.TestOnlyType, nil, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k1/o1-defl/o2-defl": "x",
		"k2/o1-defl/o2-defl": "x",
		"k1/A/o2-defl":       "y",
		"k2/o1-defl/B":       "y",
		"k2/A2/B2":           "x",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"opt2": "B",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k2/o1-defl/B": "y",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"opt1": "o1-defl",
		"opt2": "B",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k2/o1-defl/B": "y",
	})

	found = nil
	mylog.Check(bs.Search(asserts.TestOnlyType, map[string]string{
		"opt1": "A2",
	}, foundCb, 0))

	c.Check(found, DeepEquals, map[string]string{
		"k2/A2/B2": "x",
	})
}
