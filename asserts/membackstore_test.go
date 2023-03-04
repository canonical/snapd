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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
)

type memBackstoreSuite struct {
	bs asserts.Backstore
	a  asserts.Assertion
}

var _ = Suite(&memBackstoreSuite{})

func (mbss *memBackstoreSuite) SetUpTest(c *C) {
	mbss.bs = asserts.NewMemoryBackstore()

	encoded := "type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: foo\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	mbss.a = a
}

func (mbss *memBackstoreSuite) TestPutAndGet(c *C) {
	err := mbss.bs.Put(asserts.TestOnlyType, mbss.a)
	c.Assert(err, IsNil)

	a, err := mbss.bs.Get(asserts.TestOnlyType, []string{"foo"}, 0)
	c.Assert(err, IsNil)

	c.Check(a, Equals, mbss.a)
}

func (mbss *memBackstoreSuite) TestGetNotFound(c *C) {
	a, err := mbss.bs.Get(asserts.TestOnlyType, []string{"foo"}, 0)
	c.Assert(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
		// Headers can be omitted by Backstores
	})
	c.Check(a, IsNil)

	err = mbss.bs.Put(asserts.TestOnlyType, mbss.a)
	c.Assert(err, IsNil)

	a, err = mbss.bs.Get(asserts.TestOnlyType, []string{"bar"}, 0)
	c.Assert(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
	})
	c.Check(a, IsNil)
}

func (mbss *memBackstoreSuite) TestPutNotNewer(c *C) {
	err := mbss.bs.Put(asserts.TestOnlyType, mbss.a)
	c.Assert(err, IsNil)

	err = mbss.bs.Put(asserts.TestOnlyType, mbss.a)
	c.Check(err, ErrorMatches, "revision 0 is already the current revision")
}

func (mbss *memBackstoreSuite) TestSearch(c *C) {
	encoded := "type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: one\n" +
		"other: other1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	a1, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	encoded = "type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: two\n" +
		"other: other2\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	a2, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	err = mbss.bs.Put(asserts.TestOnlyType, a1)
	c.Assert(err, IsNil)
	err = mbss.bs.Put(asserts.TestOnlyType, a2)
	c.Assert(err, IsNil)

	found := map[string]asserts.Assertion{}
	cb := func(a asserts.Assertion) {
		found[a.HeaderString("primary-key")] = a
	}
	err = mbss.bs.Search(asserts.TestOnlyType, nil, cb, 0)
	c.Assert(err, IsNil)
	c.Check(found, HasLen, 2)

	found = map[string]asserts.Assertion{}
	err = mbss.bs.Search(asserts.TestOnlyType, map[string]string{
		"primary-key": "one",
	}, cb, 0)
	c.Assert(err, IsNil)
	c.Check(found, DeepEquals, map[string]asserts.Assertion{
		"one": a1,
	})

	found = map[string]asserts.Assertion{}
	err = mbss.bs.Search(asserts.TestOnlyType, map[string]string{
		"other": "other2",
	}, cb, 0)
	c.Assert(err, IsNil)
	c.Check(found, DeepEquals, map[string]asserts.Assertion{
		"two": a2,
	})

	found = map[string]asserts.Assertion{}
	err = mbss.bs.Search(asserts.TestOnlyType, map[string]string{
		"primary-key": "two",
		"other":       "other1",
	}, cb, 0)
	c.Assert(err, IsNil)
	c.Check(found, HasLen, 0)
}

func (mbss *memBackstoreSuite) TestSearch2Levels(c *C) {
	encoded := "type: test-only-2\n" +
		"authority-id: auth-id1\n" +
		"pk1: a\n" +
		"pk2: x\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	aAX, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	encoded = "type: test-only-2\n" +
		"authority-id: auth-id1\n" +
		"pk1: b\n" +
		"pk2: x\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="
	aBX, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	err = mbss.bs.Put(asserts.TestOnly2Type, aAX)
	c.Assert(err, IsNil)
	err = mbss.bs.Put(asserts.TestOnly2Type, aBX)
	c.Assert(err, IsNil)

	found := map[string]asserts.Assertion{}
	cb := func(a asserts.Assertion) {
		found[a.HeaderString("pk1")+":"+a.HeaderString("pk2")] = a
	}
	err = mbss.bs.Search(asserts.TestOnly2Type, map[string]string{
		"pk2": "x",
	}, cb, 0)
	c.Assert(err, IsNil)
	c.Check(found, HasLen, 2)
}

func (mbss *memBackstoreSuite) TestPutOldRevision(c *C) {
	bs := asserts.NewMemoryBackstore()

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

func (mbss *memBackstoreSuite) TestGetFormat(c *C) {
	bs := asserts.NewMemoryBackstore()

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
	c.Assert(err, FitsTypeOf, &asserts.NotFoundError{})
	c.Check(a, IsNil)

	err = bs.Put(asserts.TestOnlyType, af2)
	c.Assert(err, IsNil)

	a, err = bs.Get(asserts.TestOnlyType, []string{"zoo"}, 1)
	c.Assert(err, FitsTypeOf, &asserts.NotFoundError{})
	c.Check(a, IsNil)

	a, err = bs.Get(asserts.TestOnlyType, []string{"zoo"}, 2)
	c.Assert(err, IsNil)
	c.Check(a.Revision(), Equals, 22)
}

func (mbss *memBackstoreSuite) TestSearchFormat(c *C) {
	bs := asserts.NewMemoryBackstore()

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

func (mbss *memBackstoreSuite) TestPutSequence(c *C) {
	bs := asserts.NewMemoryBackstore()

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

	for _, a := range []asserts.Assertion{sq3f1, sq1f0, sq2f0, sq2f1} {
		err = bs.Put(asserts.TestOnlySeqType, a)
		c.Assert(err, IsNil)
	}

	a, err := bs.Get(asserts.TestOnlySeqType, []string{"s1", "1"}, 0)
	c.Assert(err, IsNil)
	c.Check(a.(asserts.SequenceMember).Sequence(), Equals, 1)
	c.Check(a.Format(), Equals, 0)

	a, err = bs.Get(asserts.TestOnlySeqType, []string{"s1", "2"}, 0)
	c.Assert(err, IsNil)
	c.Check(a.(asserts.SequenceMember).Sequence(), Equals, 2)
	c.Check(a.Format(), Equals, 0)

	a, err = bs.Get(asserts.TestOnlySeqType, []string{"s1", "2"}, 1)
	c.Assert(err, IsNil)
	c.Check(a.(asserts.SequenceMember).Sequence(), Equals, 2)
	c.Check(a.Format(), Equals, 1)

	a, err = bs.Get(asserts.TestOnlySeqType, []string{"s1", "3"}, 0)
	c.Assert(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlySeqType,
	})
	c.Check(a, IsNil)

	a, err = bs.Get(asserts.TestOnlySeqType, []string{"s1", "3"}, 1)
	c.Assert(err, IsNil)
	c.Check(a.(asserts.SequenceMember).Sequence(), Equals, 3)
	c.Check(a.Format(), Equals, 1)

	err = bs.Put(asserts.TestOnlySeqType, sq2f0)
	c.Check(err, DeepEquals, &asserts.RevisionError{Current: 1, Used: 0})
}

func (mbss *memBackstoreSuite) TestSequenceMemberAfter(c *C) {
	bs := asserts.NewMemoryBackstore()

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

func (mbss *memBackstoreSuite) TestOptionalPrimaryKeys(c *C) {
	r := asserts.MockOptionalPrimaryKey(asserts.TestOnlyType, "opt1", "o1-defl")
	defer r()
	bs := mbss.bs

	a1, err := asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k1\n" +
		"marker: a1\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)
	err = bs.Put(asserts.TestOnlyType, a1)
	c.Assert(err, IsNil)

	a2, err := asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: k2\n" +
		"opt1: A\n" +
		"marker: a2\n" +
		"sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij" +
		"\n\n" +
		"AXNpZw=="))
	c.Assert(err, IsNil)
	err = bs.Put(asserts.TestOnlyType, a2)
	c.Assert(err, IsNil)

	a, err := bs.Get(asserts.TestOnlyType, []string{"k1"}, 0)
	c.Assert(err, IsNil)
	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k1", "o1-defl"})
	c.Check(a.HeaderString("marker"), Equals, "a1")

	a, err = bs.Get(asserts.TestOnlyType, []string{"k1", "o1-defl"}, 0)
	c.Assert(err, IsNil)
	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k1", "o1-defl"})
	c.Check(a.HeaderString("marker"), Equals, "a1")

	a, err = bs.Get(asserts.TestOnlyType, []string{"k2", "A"}, 0)
	c.Assert(err, IsNil)
	c.Check(a.Ref().PrimaryKey, DeepEquals, []string{"k2", "A"})
	c.Check(a.HeaderString("marker"), Equals, "a2")

	a, err = bs.Get(asserts.TestOnlyType, []string{"k2"}, 0)
	c.Check(err, DeepEquals, &asserts.NotFoundError{
		Type: asserts.TestOnlyType,
	})
	c.Check(a, IsNil)

	a, err = bs.Get(asserts.TestOnlyType, []string{}, 0)
	c.Check(err, ErrorMatches, `internal error: Backstore.Get given a key missing mandatory elements for "test-only":.*`)
	c.Check(a, IsNil)

	var found map[string]string
	cb := func(a asserts.Assertion) {
		if found == nil {
			found = make(map[string]string)
		}
		found[strings.Join(a.Ref().PrimaryKey, "/")] = a.HeaderString("marker")

	}
	err = mbss.bs.Search(asserts.TestOnlyType, nil, cb, 0)
	c.Assert(err, IsNil)
	c.Check(found, DeepEquals, map[string]string{
		"k1/o1-defl": "a1",
		"k2/A":       "a2",
	})

	found = nil
	err = mbss.bs.Search(asserts.TestOnlyType, map[string]string{
		"primary-key": "k1",
	}, cb, 0)
	c.Assert(err, IsNil)
	c.Check(found, DeepEquals, map[string]string{
		"k1/o1-defl": "a1",
	})

	found = nil
	err = mbss.bs.Search(asserts.TestOnlyType, map[string]string{
		"opt1": "o1-defl",
	}, cb, 0)
	c.Assert(err, IsNil)
	c.Check(found, DeepEquals, map[string]string{
		"k1/o1-defl": "a1",
	})

	found = nil
	err = mbss.bs.Search(asserts.TestOnlyType, map[string]string{
		"opt1": "A",
	}, cb, 0)
	c.Assert(err, IsNil)
	c.Check(found, DeepEquals, map[string]string{
		"k2/A": "a2",
	})
}
