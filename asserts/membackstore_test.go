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

package asserts_test

import (
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
		"primary-key: foo" +
		"\n\n" +
		"openpgp c2ln"
	a, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)
	mbss.a = a
}

func (mbss *memBackstoreSuite) TestPutAndGet(c *C) {
	err := mbss.bs.Put(asserts.TestOnlyType, mbss.a)
	c.Assert(err, IsNil)

	a, err := mbss.bs.Get(asserts.TestOnlyType, []string{"foo"})
	c.Assert(err, IsNil)

	c.Check(a, Equals, mbss.a)
}

func (mbss *memBackstoreSuite) TestGetNotFound(c *C) {
	a, err := mbss.bs.Get(asserts.TestOnlyType, []string{"foo"})
	c.Assert(err, Equals, asserts.ErrNotFound)
	c.Check(a, IsNil)

	err = mbss.bs.Put(asserts.TestOnlyType, mbss.a)
	c.Assert(err, IsNil)

	a, err = mbss.bs.Get(asserts.TestOnlyType, []string{"bar"})
	c.Assert(err, Equals, asserts.ErrNotFound)
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
		"other: other1" +
		"\n\n" +
		"openpgp c2ln"
	a1, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	encoded = "type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: two\n" +
		"other: other2" +
		"\n\n" +
		"openpgp c2ln"
	a2, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	err = mbss.bs.Put(asserts.TestOnlyType, a1)
	c.Assert(err, IsNil)
	err = mbss.bs.Put(asserts.TestOnlyType, a2)
	c.Assert(err, IsNil)

	found := map[string]asserts.Assertion{}
	cb := func(a asserts.Assertion) {
		found[a.Header("primary-key")] = a
	}
	err = mbss.bs.Search(asserts.TestOnlyType, nil, cb)
	c.Assert(err, IsNil)
	c.Check(found, HasLen, 2)

	found = map[string]asserts.Assertion{}
	err = mbss.bs.Search(asserts.TestOnlyType, map[string]string{
		"primary-key": "one",
	}, cb)
	c.Assert(err, IsNil)
	c.Check(found, DeepEquals, map[string]asserts.Assertion{
		"one": a1,
	})

	found = map[string]asserts.Assertion{}
	err = mbss.bs.Search(asserts.TestOnlyType, map[string]string{
		"other": "other2",
	}, cb)
	c.Assert(err, IsNil)
	c.Check(found, DeepEquals, map[string]asserts.Assertion{
		"two": a2,
	})

	found = map[string]asserts.Assertion{}
	err = mbss.bs.Search(asserts.TestOnlyType, map[string]string{
		"primary-key": "two",
		"other":       "other1",
	}, cb)
	c.Assert(err, IsNil)
	c.Check(found, HasLen, 0)
}

func (mbss *memBackstoreSuite) TestSearch2Levels(c *C) {
	encoded := "type: test-only-2\n" +
		"authority-id: auth-id1\n" +
		"pk1: a\n" +
		"pk2: x" +
		"\n\n" +
		"openpgp c2ln"
	aAX, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	encoded = "type: test-only-2\n" +
		"authority-id: auth-id1\n" +
		"pk1: b\n" +
		"pk2: x" +
		"\n\n" +
		"openpgp c2ln"
	aBX, err := asserts.Decode([]byte(encoded))
	c.Assert(err, IsNil)

	err = mbss.bs.Put(asserts.TestOnly2Type, aAX)
	c.Assert(err, IsNil)
	err = mbss.bs.Put(asserts.TestOnly2Type, aBX)
	c.Assert(err, IsNil)

	found := map[string]asserts.Assertion{}
	cb := func(a asserts.Assertion) {
		found[a.Header("pk1")+":"+a.Header("pk2")] = a
	}
	err = mbss.bs.Search(asserts.TestOnly2Type, map[string]string{
		"pk2": "x",
	}, cb)
	c.Assert(err, IsNil)
	c.Check(found, HasLen, 2)
}

func (mbss *memBackstoreSuite) TestPutOldRevision(c *C) {
	bs := asserts.NewMemoryBackstore()

	// Create two revisions of assertion.
	a0, err := asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: foo\n" +
		"\n" +
		"openpgp c2ln"))
	c.Assert(err, IsNil)
	a1, err := asserts.Decode([]byte("type: test-only\n" +
		"authority-id: auth-id1\n" +
		"primary-key: foo\n" +
		"revision: 1\n" +
		"\n" +
		"openpgp c2ln"))
	c.Assert(err, IsNil)

	// Put newer revision, follwed by old revision.
	err = bs.Put(asserts.TestOnlyType, a1)
	c.Assert(err, IsNil)
	err = bs.Put(asserts.TestOnlyType, a0)

	c.Check(err, ErrorMatches, `revision 0 is older than current revision 1`)
	c.Check(err, DeepEquals, &asserts.RevisionError{Current: 1, Used: 0})
}
