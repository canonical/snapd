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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
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
	s.cm = store.NewCacheManager(c.MkDir(), store.CachePolicy{
		MaxItems: s.maxItems,
	})
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

		// update mtime
		err := os.Chtimes(p, time.Time{}, time.Now().Add(-24*time.Hour*time.Duration((n-i-1))))
		c.Assert(err, IsNil)
	}
	return cacheKeys, testFiles
}

func (s *cacheSuite) TestCleanup(c *C) {
	cacheKeys, testFiles := s.makeTestFiles(c, s.maxItems+12)

	// Nothing was removed at this point because the test files are
	// still in place and we just hardlink to them. The cache cleanup
	// will only clean files with a link-count of 1.
	c.Check(s.cm.Count(), Equals, s.maxItems+12)

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
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[len(cacheKeys)-s.maxItems])), Equals, true)
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[len(cacheKeys)-1])), Equals, true)

	c.Check(s.cm.Count(), Equals, s.maxItems)
}

func (s *cacheSuite) TestStats(c *C) {
	_, testFiles := s.makeTestFiles(c, s.maxItems+12)
	testFilesNames := map[string]bool{}
	for _, tf := range testFiles {
		testFilesNames[filepath.Base(tf)] = true
	}

	c.Check(s.cm.Count(), Equals, s.maxItems+12)

	// Remove the test files again, they are now only in the cache, we should
	// observe some candidates for pruning next.
	for _, p := range testFiles {
		err := os.Remove(p)
		c.Assert(err, IsNil)
	}
	stats, err := s.cm.Stats()
	c.Assert(err, IsNil)

	c.Check(int(stats.TotalSize), Equals, 24)
	c.Check(len(stats.Entries), Equals, s.maxItems+12)
	candidates := map[string]bool{}
	for _, en := range stats.Entries {
		_, ok := candidates[en.Info.Name()]
		c.Check(ok, Equals, false)
		candidates[en.Info.Name()] = true
	}
}

func (s *cacheSuite) TestCleanupContinuesOnError(c *C) {
	cacheKeys, testFiles := s.makeTestFiles(c, s.maxItems+3)
	for _, p := range testFiles {
		err := os.Remove(p)
		c.Assert(err, IsNil)
	}

	fail := true
	// simulate error with the removal of a file in cachedir
	restore := store.MockOsRemove(func(name string) error {
		if name == filepath.Join(s.cm.CacheDir(), cacheKeys[0]) && fail {
			return fmt.Errorf("simulated error 1")
		}
		if name == filepath.Join(s.cm.CacheDir(), cacheKeys[1]) && fail {
			return fmt.Errorf("simulated error 2")
		}
		return os.Remove(name)
	})
	defer restore()

	// verify that cleanup returns the last error
	err := s.cm.Cleanup()
	c.Check(err, ErrorMatches, "simulated error 1\nsimulated error 2")

	// and also verify that the cache still got cleaned up except for the entry
	// which failed
	c.Check(s.cm.Count(), Equals, s.maxItems)

	// even though the "unremovable" file is still in the cache
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[0])), Equals, true)
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[1])), Equals, true)
	// since the unremovable entries were not dropped, the next one did
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[2])), Equals, false)

	// add a new file
	p := s.makeTestFile(c, "f999", "")
	s.cm.Put("cacheKey-999", p)
	c.Assert(os.Remove(p), IsNil)

	fail = false
	err = s.cm.Cleanup()
	c.Check(err, IsNil)
	// now the file is cleaned up
	c.Check(s.cm.Count(), Equals, s.maxItems)
	// the first "unremovable" file is now gone
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[0])), Equals, false)
	// the other is still within the limit
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[1])), Equals, true)
}

func (s *cacheSuite) TestCleanupBusy(c *C) {
	_, testFiles := s.makeTestFiles(c, s.maxItems+2)
	for _, p := range testFiles {
		err := os.Remove(p)
		c.Assert(err, IsNil)
	}

	busyCheckDoneC := make(chan struct{})
	reachedRemoveC := make(chan struct{})
	cleanupErrC := make(chan error, 1)
	// block in os.Remove() so that we can trigger scenario leading to ErrCleanupBusy
	restore := store.MockOsRemove(func(name string) error {
		select {
		case <-busyCheckDoneC:
		default:
			close(reachedRemoveC)
			<-busyCheckDoneC
		}
		return os.Remove(name)
	})
	defer restore()

	go func() {
		cleanupErrC <- s.cm.Cleanup()
	}()

	<-reachedRemoveC
	err := s.cm.Cleanup()
	c.Check(err, ErrorMatches, "cannot perform cache cleanup: cache is busy")
	c.Check(errors.Is(err, store.ErrCleanupBusy), Equals, true)
	close(busyCheckDoneC)

	// verify that cleanup returns busy
	err = <-cleanupErrC
	c.Check(err, IsNil)

	// and also verify that the cache still got cleaned up
	c.Check(s.cm.Count(), Equals, s.maxItems)
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

func (s *cacheSuite) TestCleanupWithCachePolicy(c *C) {
	s.cm = store.NewCacheManager(c.MkDir(), store.CachePolicy{
		MaxItems:     3,
		MaxSizeBytes: 10 * 1024,
		MaxAge:       365 * 24 * time.Hour,
	})

	// use makeTestFiles to create files with proper modification times
	cacheKeys, testFiles := s.makeTestFiles(c, 5)

	// remove the test files so they're candidates for removal
	for _, p := range testFiles {
		err := os.Remove(p)
		c.Assert(err, IsNil)
	}

	// count before cleanup
	countBefore := s.cm.Count()
	c.Check(countBefore, Equals, 5)

	// cleanup should respect MaxItems
	err := s.cm.Cleanup()
	c.Check(err, IsNil)

	countAfter := s.cm.Count()
	c.Check(countAfter <= 3, Equals, true)

	// oldest files removed
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[0])), Equals, false)
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[1])), Equals, false)
	// newest ones are kept
	c.Check(osutil.FileExists(filepath.Join(s.cm.CacheDir(), cacheKeys[4])), Equals, true)
}

func (s *cacheSuite) TestCleanupConcurrent(c *C) {
	s.cm = store.NewCacheManager(c.MkDir(), store.CachePolicy{
		MaxItems:     3,
		MaxSizeBytes: 10 * 1024,
		MaxAge:       365 * 24 * time.Hour,
	})

	s.makeTestFiles(c, 1000)

	var wg sync.WaitGroup
	startC := make(chan struct{})
	resC := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-startC
			resC <- s.cm.Cleanup()
		}()
	}

	// cleanups start now
	close(startC)
	// cleanups are done
	wg.Wait()

	close(resC)
	for err := range resC {
		if err != nil {
			c.Logf("err: %v", err)
		}
		c.Check(err == nil || errors.Is(err, store.ErrCleanupBusy), Equals, true, Commentf("unexpected error: %v", err))
	}
}

func (s *cacheSuite) TestGetPutCleanupConcurrent(c *C) {
	// a very crude attempt at exercising concurrent cache operations
	s.cm = store.NewCacheManager(c.MkDir(), store.CachePolicy{
		MaxItems:     5,
		MaxSizeBytes: 100,
		MaxAge:       24 * time.Hour,
	})

	numWorkers := 10
	operationsPerWorker := 20
	var wg sync.WaitGroup
	startC := make(chan struct{})
	resultC := make(chan error, numWorkers*operationsPerWorker)

	const (
		opPut = iota
		opGet
		opCleanup
		opMax
	)

	// launch workers
	for i := 0; i < numWorkers; i++ {

		wg.Add(1)
		go func(workerID int) {
			defer wg.Done()
			<-startC

			for j := 0; j < operationsPerWorker; j++ {
				// each operation randomly chooses between Put, Get, or Cleanup
				op := (workerID*operationsPerWorker + j) % opMax

				switch op {
				case opPut: // 0
					p := s.makeTestFile(c, fmt.Sprintf("w%d_f%d", workerID, j), fmt.Sprintf("content-%d-%d", workerID, j))
					cacheKey := fmt.Sprintf("key-%d-%d", workerID, j)
					err := s.cm.Put(cacheKey, p)
					resultC <- err
					// remove it so that it may be cleaned up
					os.Remove(p)

				case opGet: // 1
					targetPath := filepath.Join(c.MkDir(), fmt.Sprintf("target-%d-%d", workerID, j))
					cacheKey := fmt.Sprintf("key-%d-%d", workerID, j)
					hit := s.cm.Get(cacheKey, targetPath)
					// hit might be false if the file was cleaned up, ignore it
					_ = hit
					resultC <- nil

				case opCleanup: // 2
					err := s.cm.Cleanup()
					// ErrCleanupBusy is expected when multiple cleanups race
					if err != nil && !errors.Is(err, store.ErrCleanupBusy) {
						resultC <- err
					} else {
						resultC <- nil
					}
				}
			}
		}(i)
	}

	// all workers start now
	close(startC)
	// wait for all workers to complete
	wg.Wait()
	close(resultC)

	// verify no unexpected errors occurred
	for err := range resultC {
		c.Check(err, IsNil)
	}
}

type cachePolicySuite struct {
	tmp string
}

var _ = Suite(&cachePolicySuite{})

func (s *cachePolicySuite) SetUpTest(c *C) {
	s.tmp = c.MkDir()
}

func (s *cachePolicySuite) makeTestDirEntry(c *C, name string, size int64, modTime time.Time) os.DirEntry {
	p := filepath.Join(s.tmp, name)
	content := make([]byte, size)
	err := os.WriteFile(p, content, 0644)
	c.Assert(err, IsNil)

	err = os.Chtimes(p, modTime, modTime)
	c.Assert(err, IsNil)

	entry, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)
	for _, e := range entry {
		if e.Name() == name {
			return e
		}
	}
	c.Fatal("entry not found")
	return nil
}

func (s *cachePolicySuite) TestNoRemoval(c *C) {
	// within all limits
	cp := store.CachePolicy{
		MaxItems:     10,
		MaxSizeBytes: 1024,
		MaxAge:       30 * 24 * time.Hour,
	}

	s.makeTestDirEntry(c, "file1", 100, time.Now())
	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)

	now := time.Now()
	removedCount, removedSize, err := cp.Apply(entries, now, func(fi os.FileInfo) error {
		panic("unexpected call")
	})
	c.Assert(err, IsNil)
	c.Check(removedCount, Equals, 0)
	c.Check(removedSize, Equals, uint64(0))
}

func (s *cachePolicySuite) TestMaxItems(c *C) {
	// removal up to max limit
	cp := store.CachePolicy{
		MaxItems:     2,
		MaxSizeBytes: 10 * 1024 * 1024,
		MaxAge:       30 * 24 * time.Hour,
	}

	s.makeTestDirEntry(c, "file1", 100, time.Now().Add(-48*time.Hour))
	s.makeTestDirEntry(c, "file2", 100, time.Now().Add(-24*time.Hour))
	s.makeTestDirEntry(c, "file3", 100, time.Now())

	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)

	now := time.Now()
	var removed []string
	removedCount, removedSize, err := cp.Apply(entries, now, func(fi os.FileInfo) error {
		removed = append(removed, fi.Name())
		return nil
	})
	c.Assert(err, IsNil)
	c.Check(removed, DeepEquals, []string{"file1"})
	c.Check(removedCount, Equals, 1)
	c.Check(removedSize, Equals, uint64(100))
}

func (s *cachePolicySuite) TestMaxAgeTooOld(c *C) {
	// cleanup of items over the age threshold
	cp := store.CachePolicy{
		MaxItems:     10,
		MaxSizeBytes: 10 * 1024 * 1024,
		MaxAge:       24 * time.Hour,
	}

	now := time.Now()
	s.makeTestDirEntry(c, "old_file", 100, now.Add(-48*time.Hour))
	s.makeTestDirEntry(c, "recent_file", 100, now.Add(-12*time.Hour))

	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)

	var removed []string
	removedCount, removedSize, err := cp.Apply(entries, now, func(fi os.FileInfo) error {
		removed = append(removed, fi.Name())
		return nil
	})
	c.Assert(err, IsNil)

	// should remove file older than MaxAge
	c.Check(removed, DeepEquals, []string{"old_file"})
	c.Check(removedCount, Equals, 1)
	c.Check(removedSize, Equals, uint64(100))
}

func (s *cachePolicySuite) TestMaxSize(c *C) {
	// test that items are removed when total size exceeds MaxSize
	cp := store.CachePolicy{
		MaxItems:     10,
		MaxSizeBytes: 250, // 250B
		MaxAge:       30 * 24 * time.Hour,
	}

	now := time.Now()
	// files will be removed starting from the oldest until we meet the size limit
	s.makeTestDirEntry(c, "file1", 100, now.Add(-25*time.Hour))
	s.makeTestDirEntry(c, "file2", 100, now.Add(-24*time.Hour))
	s.makeTestDirEntry(c, "file3", 100, now.Add(-23*time.Hour))
	s.makeTestDirEntry(c, "file4", 100, now.Add(-22*time.Hour))
	s.makeTestDirEntry(c, "file5", 100, now.Add(-21*time.Hour))

	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)

	var removed []string
	removedCount, removedSize, err := cp.Apply(entries, now, func(fi os.FileInfo) error {
		removed = append(removed, fi.Name())
		return nil
	})
	c.Assert(err, IsNil)
	// removing files 1, 2, 3, makes the cache meet the size limit
	c.Check(removed, DeepEquals, []string{
		"file1", "file2", "file3",
	})
	c.Check(removedCount, Equals, 3)
	c.Check(removedSize, Equals, uint64(300))
}

func (s *cachePolicySuite) TestApplyMultipleLimits(c *C) {
	// test that all three policy constraints are respected
	cp := store.CachePolicy{
		MaxItems:     2,
		MaxSizeBytes: 150,
		MaxAge:       24 * time.Hour,
	}

	now := time.Now()
	// arrange a scenario where files will be dropped in order to satisfy different limits in the policy
	s.makeTestDirEntry(c, "very_old", 100, now.Add(-72*time.Hour)) // hits the age limit
	s.makeTestDirEntry(c, "old", 100, now.Add(-48*time.Hour))      // victim to items limit
	s.makeTestDirEntry(c, "recent", 100, now.Add(-12*time.Hour))   // caught by cache size limit
	s.makeTestDirEntry(c, "newest", 100, now)

	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)

	var removed []string
	removedCount, removedSize, err := cp.Apply(entries, now, func(fi os.FileInfo) error {
		removed = append(removed, fi.Name())
		return nil
	})
	c.Assert(err, IsNil)

	c.Check(removed, DeepEquals, []string{
		"very_old", "old", "recent",
	})
	c.Check(removedCount, Equals, 3)
	c.Check(removedSize, Equals, uint64(300))
}

func (s *cachePolicySuite) TestEmptyCache(c *C) {
	cp := store.CachePolicy{
		MaxItems:     5,
		MaxSizeBytes: 1 * 1024 * 1024,
		MaxAge:       24 * time.Hour,
	}

	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)

	now := time.Now()
	removedCount, removedSize, err := cp.Apply(entries, now, func(fi os.FileInfo) error {
		panic("unexpected call")
	})
	c.Assert(err, IsNil)
	c.Check(removedCount, Equals, 0)
	c.Check(removedSize, Equals, uint64(0))
}

func (s *cachePolicySuite) TestSingleFile(c *C) {
	cp := store.CachePolicy{
		MaxItems:     1,
		MaxSizeBytes: 100,
		MaxAge:       24 * time.Hour,
	}

	s.makeTestDirEntry(c, "single", 50, time.Now())

	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)

	now := time.Now()
	removedCount, removedSize, err := cp.Apply(entries, now, func(fi os.FileInfo) error {
		panic("unexpected call")
	})
	c.Assert(err, IsNil)
	c.Check(removedCount, Equals, 0)
	c.Check(removedSize, Equals, uint64(0))
}

func (s *cachePolicySuite) TestZeroMaxItems(c *C) {
	// test that MaxItems=0 has no effect
	cp := store.CachePolicy{
		MaxItems:     0,
		MaxSizeBytes: 10 * 1024 * 1024,
		MaxAge:       30 * 24 * time.Hour,
	}

	now := time.Now()
	s.makeTestDirEntry(c, "file1", 100, now.Add(-12*time.Hour))
	s.makeTestDirEntry(c, "file2", 100, now)

	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)

	removedCount, removedSize, err := cp.Apply(entries, now, func(fi os.FileInfo) error {
		panic("unexpected call")
	})
	c.Assert(err, IsNil)
	c.Check(removedCount, Equals, 0)
	c.Check(removedSize, Equals, uint64(0))
}

func (s *cachePolicySuite) TestZeroMaxSize(c *C) {
	// test that MaxSize=0 has no effect
	cp := store.CachePolicy{
		MaxItems:     10,
		MaxSizeBytes: 0,
		MaxAge:       30 * 24 * time.Hour,
	}

	now := time.Now()
	s.makeTestDirEntry(c, "file1", 100, now.Add(-12*time.Hour))
	s.makeTestDirEntry(c, "file2", 100, now)

	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)

	removedCount, removedSize, err := cp.Apply(entries, now, func(fi os.FileInfo) error {
		panic("unexpected call")
	})
	c.Assert(err, IsNil)
	c.Check(removedCount, Equals, 0)
	c.Check(removedSize, Equals, uint64(0))
}

func (s *cachePolicySuite) TestZeroMaxAge(c *C) {
	// test that MaxAge=0 has no effect
	cp := store.CachePolicy{
		MaxItems:     10,
		MaxSizeBytes: 10 * 1024 * 1024,
		MaxAge:       0,
	}

	now := time.Now()
	s.makeTestDirEntry(c, "file1", 100, now.Add(-12*time.Hour))
	s.makeTestDirEntry(c, "file2", 100, now)

	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)

	removedCount, removedSize, err := cp.Apply(entries, now, func(fi os.FileInfo) error {
		panic("unexpected call")
	})
	c.Assert(err, IsNil)
	c.Check(removedCount, Equals, 0)
	c.Check(removedSize, Equals, uint64(0))
}

func (s *cachePolicySuite) TestMaxAgeAtBoundary(c *C) {
	maxAge := 24 * time.Hour
	cp := store.CachePolicy{
		MaxItems:     10,
		MaxSizeBytes: 10 * 1024 * 1024,
		MaxAge:       maxAge,
	}

	now := time.Now()
	// above the age threshold
	s.makeTestDirEntry(c, "past_boundary", 100, now.Add(-maxAge-time.Minute))
	// these are kept
	s.makeTestDirEntry(c, "at_boundary", 100, now.Add(-maxAge))
	s.makeTestDirEntry(c, "just_before", 100, now.Add(-maxAge+time.Minute))

	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)

	var removed []string
	removedCount, removedSize, err := cp.Apply(entries, now, func(fi os.FileInfo) error {
		removed = append(removed, fi.Name())
		return nil
	})
	c.Assert(err, IsNil)
	c.Check(removed, DeepEquals, []string{"past_boundary"})
	c.Check(removedCount, Equals, 1)
	c.Check(removedSize, Equals, uint64(100))
}

func (s *cachePolicySuite) TestApplyErr(c *C) {
	maxAge := 24 * time.Hour
	cp := store.CachePolicy{
		MaxItems:     10,
		MaxSizeBytes: 10 * 1024 * 1024,
		MaxAge:       maxAge,
	}

	now := time.Now()
	// above the age threshold
	s.makeTestDirEntry(c, "entry", 100, now.Add(-maxAge-time.Minute))

	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)
	c.Assert(entries, HasLen, 1)
	c.Assert(os.Remove(filepath.Join(s.tmp, entries[0].Name())), IsNil)

	removedCount, removedSize, err := cp.Apply(entries, now, func(fi os.FileInfo) error {
		panic("unexpected call")
	})
	c.Assert(err, ErrorMatches, ".*/entry: no such file or directory")
	c.Check(removedCount, Equals, 0)
	c.Check(removedSize, Equals, uint64(0))
}

func (s *cachePolicySuite) TestApplyContinuesOnError(c *C) {
	maxAge := 24 * time.Hour
	cp := store.CachePolicy{
		MaxItems: 4,
	}

	now := time.Now()
	s.makeTestDirEntry(c, "file1", 100, now.Add(-maxAge))
	s.makeTestDirEntry(c, "file2", 100, now.Add(-maxAge+time.Minute))
	s.makeTestDirEntry(c, "file3", 100, now.Add(-maxAge+2*time.Minute))
	s.makeTestDirEntry(c, "file4", 100, now.Add(-maxAge+3*time.Minute))
	s.makeTestDirEntry(c, "file5", 100, now.Add(-maxAge+4*time.Minute))
	s.makeTestDirEntry(c, "file6", 100, now.Add(-maxAge+5*time.Minute))

	entries, err := os.ReadDir(s.tmp)
	c.Assert(err, IsNil)
	c.Assert(entries, HasLen, 6)

	fail := true
	var removed []string

	rm := func(fi os.FileInfo) error {
		removed = append(removed, fi.Name())
		if fail {
			switch fi.Name() {
			case "file1":
				return fmt.Errorf("simulated error 1")
			case "file2":
				return fmt.Errorf("simulated error 2")
			default:
			}
		}
		// actually remove
		return os.Remove(filepath.Join(s.tmp, fi.Name()))
	}

	removedCount, removedSize, err := cp.Apply(entries, now, rm)
	c.Check(err, ErrorMatches, "simulated error 1\nsimulated error 2")
	c.Check(removed, DeepEquals, []string{
		"file1", // remove fails
		"file2", // removal fails, removed count is 0, candidates (items) count is 5
		"file3", // removing brings us to 4 items
		"file4", // removing brings us to 3 items, meeting the limit
	})
	c.Check(removedCount, Equals, 2)
	c.Check(removedSize, Equals, uint64(200))

	entries, err = os.ReadDir(s.tmp)
	c.Assert(err, IsNil)
	c.Assert(entries, HasLen, 4)

	// change the limit to 3 items
	cp.MaxItems = 3

	fail = false
	removed = []string{}
	// try again, no removal errors this time
	removedCount, removedSize, err = cp.Apply(entries, now, rm)
	c.Check(err, IsNil)
	c.Check(removed, DeepEquals, []string{
		"file1", // removing meets the items count target
	})
	c.Check(removedCount, Equals, 1)
	c.Check(removedSize, Equals, uint64(100))
}
