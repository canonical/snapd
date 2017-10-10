// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2017 Canonical Ltd
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

package store_test

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"strconv"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/store"
)

type cacheSuite struct {
	cm       *store.CacheManager
	tmp      string
	maxItems int
}

var _ = Suite(&cacheSuite{})

func (s *cacheSuite) SetUpTest(c *C) {
	s.tmp = c.MkDir()

	s.maxItems = 5
	s.cm = store.NewCacheManager(c.MkDir(), s.maxItems)
	// sanity
	c.Check(s.cm.Count(), Equals, 0)
}

func (s *cacheSuite) makeTestFile(c *C, name, content string) string {
	p := filepath.Join(c.MkDir(), name)
	err := ioutil.WriteFile(p, []byte(content), 0644)
	c.Assert(err, IsNil)
	return p
}

func (s *cacheSuite) TestPutSameContent(c *C) {
	// adding the same content with different names is fine and results
	// only in a single cache item
	for i := 0; i < 5; i++ {
		p := s.makeTestFile(c, fmt.Sprintf("f%d", i), "ni! ni! ni!")

		err := s.cm.Put(p)
		c.Check(err, IsNil)
		c.Check(s.cm.Count(), Equals, 1)
	}
}

func (s *cacheSuite) TestPutMany(c *C) {
	for i := 1; i < s.maxItems+10; i++ {
		err := s.cm.Put(s.makeTestFile(c, fmt.Sprintf("f%d", i), fmt.Sprintf("%d", i)))
		c.Check(err, IsNil)
		if i < s.maxItems {
			c.Check(s.cm.Count(), Equals, i)
		} else {
			c.Check(s.cm.Count(), Equals, s.maxItems)
		}
	}
}

func (s *cacheSuite) TestGetNotExistant(c *C) {
	err := s.cm.Get("hash-not-in-cache", "some-target-path")
	c.Check(err, ErrorMatches, `cannot find "hash-not-in-cache" in .*`)
}

func (s *cacheSuite) TestGet(c *C) {
	canary := "some content"
	p := s.makeTestFile(c, "foo", canary)
	err := s.cm.Put(p)
	c.Assert(err, IsNil)
	sha3_384, err := s.cm.Digest(p)
	c.Assert(err, IsNil)

	targetPath := filepath.Join(s.tmp, "new-location")
	err = s.cm.Get(sha3_384, targetPath)
	c.Check(err, IsNil)
	c.Check(osutil.FileExists(targetPath), Equals, true)
	content, err := ioutil.ReadFile(targetPath)
	c.Assert(err, IsNil)
	c.Check(string(content), Equals, canary)
}

func (s *cacheSuite) TestLookupUnhappy(c *C) {
	c.Check(s.cm.Lookup("not-such-hash-in-the-cache"), Equals, false)
}

func (s *cacheSuite) TestLookupHappy(c *C) {
	p := s.makeTestFile(c, "foo", "some content")

	sha3_384, err := s.cm.Digest(p)
	c.Assert(err, IsNil)

	err = s.cm.Put(p)
	c.Assert(err, IsNil)

	c.Check(s.cm.Lookup(sha3_384), Equals, true)
}

func (s *cacheSuite) TestClenaup(c *C) {
	// add files, add more than
	digests := make([]string, s.maxItems+2)
	for i := 0; i < s.maxItems+2; i++ {
		p := s.makeTestFile(c, fmt.Sprintf("f%d", i), strconv.Itoa(i))
		sha3_384, err := s.cm.Digest(p)
		c.Assert(err, IsNil)
		digests[i] = sha3_384

		s.cm.Put(p)
		// mtime is not very granular
		time.Sleep(10 * time.Millisecond)
	}
	c.Check(s.cm.Count(), Equals, s.maxItems)

	// the oldest files are removed from the cache
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), digests[0])), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), digests[1])), Equals, false)

	// the newest files are still there
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), digests[2])), Equals, true)
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), digests[len(digests)-1])), Equals, true)

}
