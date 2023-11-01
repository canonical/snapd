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
	"os"
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
	// validity
	c.Check(s.cm.Count(), Equals, 0)
}

func (s *cacheSuite) makeTestFile(c *C, name, content string) string {
	return s.makeTestFileInDir(c, c.MkDir(), name, content)
}
func (s *cacheSuite) makeTestFileInDir(c *C, dir, name, content string) string {
	p := filepath.Join(dir, name)
	err := os.WriteFile(p, []byte(content), 0644)
	c.Assert(err, IsNil)
	return p
}

func (s *cacheSuite) TestPutMany(c *C) {
	dataDir, err := os.Open(c.MkDir())
	c.Assert(err, IsNil)
	defer dataDir.Close()
	cacheDir, err := os.Open(s.cm.CacheDir())
	c.Assert(err, IsNil)
	defer cacheDir.Close()

	for i := 1; i < s.maxItems+10; i++ {
		p := s.makeTestFileInDir(c, dataDir.Name(), fmt.Sprintf("f%d", i), fmt.Sprintf("%d", i))
		err := s.cm.Put(fmt.Sprintf("cacheKey-%d", i), p)
		c.Check(err, IsNil)

		// Remove the test file again, it is now only in the cache
		err = os.Remove(p)
		c.Assert(err, IsNil)
		// We need to sync the (meta)data here or the test is racy
		c.Assert(dataDir.Sync(), IsNil)
		c.Assert(cacheDir.Sync(), IsNil)

		if i <= s.maxItems {
			c.Check(s.cm.Count(), Equals, i)
		} else {
			// Count() will include the cache plus the
			// newly added test file. This latest testfile
			// had a link count of 2 when "cm.Put()" is
			// called because it still exists outside of
			// the cache dir so it's considered "free" in
			// the cache. This is why we need to have
			// Count()-1 here.
			c.Check(s.cm.Count()-1, Equals, s.maxItems)
		}
	}
}

func (s *cacheSuite) TestGetNotExistent(c *C) {
	cacheHit := s.cm.Get("hash-not-in-cache", "some-target-path")
	c.Check(cacheHit, Equals, false)
}

func (s *cacheSuite) TestGet(c *C) {
	canary := "some content"
	p := s.makeTestFile(c, "foo", canary)
	err := s.cm.Put("some-cache-key", p)
	c.Assert(err, IsNil)

	targetPath := filepath.Join(s.tmp, "new-location")
	cacheHit := s.cm.Get("some-cache-key", targetPath)
	c.Check(cacheHit, Equals, true)
	c.Check(osutil.FileExists(targetPath), Equals, true)
	c.Assert(targetPath, testutil.FileEquals, canary)
}

func (s *cacheSuite) makeTestFiles(c *C, n int) (cacheKeys []string, testFiles []string) {
	cacheKeys = make([]string, n)
	testFiles = make([]string, n)
	for i := 0; i < n; i++ {
		p := s.makeTestFile(c, fmt.Sprintf("f%d", i), strconv.Itoa(i))
		cacheKey := fmt.Sprintf("cacheKey-%d", i)
		cacheKeys[i] = cacheKey
		s.cm.Put(cacheKey, p)

		// keep track of the test files
		testFiles[i] = p

		// mtime is not very granular
		time.Sleep(10 * time.Millisecond)
	}
	return cacheKeys, testFiles
}

func (s *cacheSuite) TestCleanup(c *C) {
	cacheKeys, testFiles := s.makeTestFiles(c, s.maxItems+2)

	// Nothing was removed at this point because the test files are
	// still in place and we just hardlink to them. The cache cleanup
	// will only clean files with a link-count of 1.
	c.Check(s.cm.Count(), Equals, s.maxItems+2)

	// Remove the test files again, they are now only in the cache.
	for _, p := range testFiles {
		err := os.Remove(p)
		c.Assert(err, IsNil)
	}
	s.cm.Cleanup()

	// the oldest files are removed from the cache
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[0])), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[1])), Equals, false)

	// the newest files are still there
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[2])), Equals, true)
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[len(cacheKeys)-1])), Equals, true)
}

func (s *cacheSuite) TestCleanupContinuesOnError(c *C) {
	cacheKeys, testFiles := s.makeTestFiles(c, s.maxItems+2)
	for _, p := range testFiles {
		err := os.Remove(p)
		c.Assert(err, IsNil)
	}

	// simulate error with the removal of a file in cachedir
	restore := store.MockOsRemove(func(name string) error {
		if name == filepath.Join(s.cm.CacheDir(), cacheKeys[0]) {
			return fmt.Errorf("simulated error")
		}
		return os.Remove(name)
	})
	defer restore()

	// verify that cleanup returns the last error
	err := s.cm.Cleanup()
	c.Check(err, ErrorMatches, "simulated error")

	// and also verify that the cache still got cleaned up
	c.Check(s.cm.Count(), Equals, s.maxItems)

	// even though the "unremovable" file is still in the cache
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[0])), Equals, true)
}

func (s *cacheSuite) TestHardLinkCount(c *C) {
	p := filepath.Join(s.tmp, "foo")
	err := os.WriteFile(p, nil, 0644)
	c.Assert(err, IsNil)

	// trivial case
	fi, err := os.Stat(p)
	c.Assert(err, IsNil)
	n, err := store.HardLinkCount(fi)
	c.Assert(err, IsNil)
	c.Check(n, Equals, uint64(1))

	// add some hardlinks
	for i := 0; i < 10; i++ {
		err := os.Link(p, filepath.Join(s.tmp, strconv.Itoa(i)))
		c.Assert(err, IsNil)
	}
	fi, err = os.Stat(p)
	c.Assert(err, IsNil)
	n, err = store.HardLinkCount(fi)
	c.Assert(err, IsNil)
	c.Check(n, Equals, uint64(11))

	// and remove one
	err = os.Remove(filepath.Join(s.tmp, "0"))
	c.Assert(err, IsNil)
	fi, err = os.Stat(p)
	c.Assert(err, IsNil)
	n, err = store.HardLinkCount(fi)
	c.Assert(err, IsNil)
	c.Check(n, Equals, uint64(10))
}

func (s *cacheSuite) TestCacheHitOnErrExist(c *C) {
	targetPath := filepath.Join(s.tmp, "foo")
	err := os.WriteFile(targetPath, nil, 0644)
	c.Assert(err, IsNil)

	// put file in target path
	c.Assert(s.cm.Put("foo", targetPath), IsNil)

	// cache tries to link to an occupied path
	cacheHit := s.cm.Get("foo", targetPath)
	c.Assert(cacheHit, Equals, true)
}
