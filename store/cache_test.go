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
	"github.com/snapcore/snapd/testutil"
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

func (s *cacheSuite) TestPutMany(c *C) {
	for i := 1; i < s.maxItems+10; i++ {
		err := s.cm.Put(fmt.Sprintf("cacheKey-%d", i), s.makeTestFile(c, fmt.Sprintf("f%d", i), fmt.Sprintf("%d", i)))
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
	c.Check(err, ErrorMatches, `link .*: no such file or directory`)
}

func (s *cacheSuite) TestGet(c *C) {
	canary := "some content"
	p := s.makeTestFile(c, "foo", canary)
	err := s.cm.Put("some-cache-key", p)
	c.Assert(err, IsNil)

	targetPath := filepath.Join(s.tmp, "new-location")
	err = s.cm.Get("some-cache-key", targetPath)
	c.Check(err, IsNil)
	c.Check(osutil.FileExists(targetPath), Equals, true)
	c.Assert(targetPath, testutil.FileEquals, canary)
}

func (s *cacheSuite) TestClenaup(c *C) {
	// add files, add more than
	cacheKeys := make([]string, s.maxItems+2)
	for i := 0; i < s.maxItems+2; i++ {
		p := s.makeTestFile(c, fmt.Sprintf("f%d", i), strconv.Itoa(i))
		cacheKey := fmt.Sprintf("cacheKey-%d", i)
		cacheKeys[i] = cacheKey
		s.cm.Put(cacheKey, p)

		// mtime is not very granular
		time.Sleep(10 * time.Millisecond)
	}
	c.Check(s.cm.Count(), Equals, s.maxItems)

	// the oldest files are removed from the cache
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[0])), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[1])), Equals, false)

	// the newest files are still there
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[2])), Equals, true)
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[len(cacheKeys)-1])), Equals, true)

}
