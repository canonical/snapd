// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package store

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil/quantity"
)

// overridden in the unit tests
var osRemove = os.Remove

var ErrCleanupBusy = errors.New("cannot perform cache cleanup: cache is busy")

// downloadCache is the interface that a store download cache must provide
type downloadCache interface {
	// Get retrieves the given cacheKey content and puts it into targetPath. Returns
	// true if a cached file was moved to targetPath or if one was already there.
	Get(cacheKey, targetPath string) bool
	// Put adds a new file to the cache
	Put(cacheKey, sourcePath string) error
	// Get full path of the file in cache
	GetPath(cacheKey string) string
	// Best effort cleanup of outstanding cache items. Returns ErrCleanupBusy
	// when the cache is in use and cleanup should be retried at some later
	// time.
	Cleanup() error
}

// nullCache is cache that does not cache
type nullCache struct{}

func (cm *nullCache) Get(cacheKey, targetPath string) bool {
	return false
}
func (cm *nullCache) GetPath(cacheKey string) string {
	return ""
}
func (cm *nullCache) Put(cacheKey, sourcePath string) error { return nil }

func (cm *nullCache) Cleanup() error { return nil }

// changesByMtime sorts by the mtime of files
type changesByMtime []os.FileInfo

func (s changesByMtime) Len() int           { return len(s) }
func (s changesByMtime) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s changesByMtime) Less(i, j int) bool { return s[i].ModTime().Before(s[j].ModTime()) }

// cacheManager implements a downloadCache via content based hard linking
type CacheManager struct {
	// cleanupLock is used as a 'cleanup' synchronization point, where operations
	// for putting, getting files from the cache take a read cleanupLock, while the
	// actual cleanup operation takes the cleanupLock for writing
	cleanupLock sync.RWMutex
	cacheDir    string
	maxItems    int
}

// NewCacheManager returns a new CacheManager with the given cacheDir
// and the given maximum amount of items. The idea behind it is the
// following algorithm:
//
//  1. When starting a download, check if it exists in $cacheDir
//  2. If found, update its mtime, hardlink into target location, and
//     return success
//  3. If not found, download the snap
//  4. On success, hardlink into $cacheDir/<digest>
//  5. If cache dir has more than maxItems entries, remove oldest mtimes
//     until it has maxItems
//
// The caching part is done here, the downloading happens in the store.go
// code.
func NewCacheManager(cacheDir string, maxItems int) *CacheManager {
	return &CacheManager{
		cacheDir: cacheDir,
		maxItems: maxItems,
	}
}

// GetPath returns the full path of the given content in the cache or empty
// string. The path may be removed at any time. The caller needs to ensure that
// they properly handle ErrNotExist when using returned path.
func (cm *CacheManager) GetPath(cacheKey string) string {
	cm.cleanupLock.RLock()
	defer cm.cleanupLock.RUnlock()

	if _, err := os.Stat(cm.path(cacheKey)); os.IsNotExist(err) {
		return ""
	}

	return cm.path(cacheKey)
}

// Get retrieves the given cacheKey content and puts it into targetPath. Returns
// true if a cached file was moved to targetPath or if one was already there.
func (cm *CacheManager) Get(cacheKey, targetPath string) bool {
	cm.cleanupLock.RLock()
	defer cm.cleanupLock.RUnlock()

	if err := os.Link(cm.path(cacheKey), targetPath); err != nil && !errors.Is(err, os.ErrExist) {
		return false
	}

	logger.Debugf("using cache for %s", targetPath)
	now := time.Now()
	// the modification time is updated on a best-effort basis
	_ = os.Chtimes(targetPath, now, now)
	return true
}

// Put adds a new file to the cache with the given cacheKey
func (cm *CacheManager) Put(cacheKey, sourcePath string) error {
	// always try to create the cache dir first or the following
	// osutil.IsWritable will always fail if the dir is missing
	_ = os.MkdirAll(cm.cacheDir, 0700)

	// happens on e.g. `snap download` which runs as the user
	if !osutil.IsWritable(cm.cacheDir) {
		return nil
	}

	err := func() error {
		cm.cleanupLock.RLock()
		defer cm.cleanupLock.RUnlock()

		err := os.Link(sourcePath, cm.path(cacheKey))
		if errors.Is(err, fs.ErrExist) {
			now := time.Now()
			return os.Chtimes(cm.path(cacheKey), now, now)
		}
		return err
	}()
	if err != nil {
		return err
	}

	return cm.opportunisticCleanup()
}

// count returns the number of items in the cache
func (cm *CacheManager) count() int {
	// TODO: Use something more effective than a list of all entries
	//       here. This will waste a lot of memory on large dirs.
	if l, err := os.ReadDir(cm.cacheDir); err == nil {
		return len(l)
	}
	return 0
}

// path returns the full path of the given content in the cache
func (cm *CacheManager) path(cacheKey string) string {
	return filepath.Join(cm.cacheDir, cacheKey)
}

// invokes cleanup(), but ignores ErrCleanupBusy errors.
func (cm *CacheManager) opportunisticCleanup() error {
	if err := cm.cleanup(); err != ErrCleanupBusy {
		return err
	}
	return nil
}

// cleanup ensures that only maxItems are stored in the cache. May return
// ErrCleanupBusy if the cleanup lock cannot be taken in which case the cleanup
// is skipped.
func (cm *CacheManager) cleanup() error {
	// try to obtain exclusive lock on the cache
	if !cm.cleanupLock.TryLock() {
		return ErrCleanupBusy
	}
	defer cm.cleanupLock.Unlock()

	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		return err
	}

	if len(entries) <= cm.maxItems {
		return nil
	}

	// most of the entries will have more than one hardlink, but a minority may
	// be referenced only from the cache and thus be a candidate for pruning
	pruneCandidates := make([]os.FileInfo, 0, len(entries)/5)
	pruneCandidatesSize := int64(0)

	for _, entry := range entries {
		fi, err := entry.Info()
		if err != nil {
			return err
		}

		n, err := hardLinkCount(fi)
		if err != nil {
			logger.Noticef("cannot inspect cache: %s", err)
		}
		// If the file is referenced in the filesystem somewhere else our copy
		// is "free" so skip it.
		if n <= 1 {
			pruneCandidates = append(pruneCandidates, fi)
			pruneCandidatesSize += fi.Size()
		}
	}

	if len(pruneCandidates) > 0 {
		logger.Debugf("store cache cleanup candidates %v total %v", len(pruneCandidates),
			quantity.FormatAmount(uint64(pruneCandidatesSize), -1))
		for _, c := range pruneCandidates {
			logger.Debugf("%s, size: %v, mod %s", c.Name(), quantity.FormatAmount(uint64(c.Size()), -1), c.ModTime())
		}
	}

	if len(pruneCandidates) <= cm.maxItems {
		// nothing to prune
		return nil
	}

	var lastErr error
	sort.Sort(changesByMtime(pruneCandidates))
	numOwned := len(pruneCandidates)
	deleted := 0
	for _, fi := range pruneCandidates {
		path := cm.path(fi.Name())
		logger.Debugf("removing %v", path)
		if err := osRemove(path); err != nil {
			if !os.IsNotExist(err) {
				// If there is any error we cleanup the file (it is just a cache
				// after all).
				logger.Noticef("cannot cleanup cache: %s", err)
				lastErr = err
			}
			continue
		}
		deleted++
		remaining := numOwned - deleted
		if remaining <= cm.maxItems {
			logger.Debugf("cache size satisfied, remaining items: %v", remaining)
			break
		}
	}
	return lastErr
}

// hardLinkCount returns the number of hardlinks for the given path
func hardLinkCount(fi os.FileInfo) (uint64, error) {
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok && stat != nil {
		return uint64(stat.Nlink), nil
	}
	return 0, fmt.Errorf("internal error: cannot read hardlink count from %s", fi.Name())
}

// StoreCacheStats contains some statistics about the store cache.
type StoreCacheStats struct {
	// TotalEntries is a count of all entries in the cache.
	TotalEntries int
	// TotalSize is a sum of sizes of all entries in the cache.
	TotalSize uint64
	// PruneCandidates is a list of files which are candidates for removal.
	PruneCandidates []os.FileInfo
}

// Status returns statistics about the store cache.
func (cm *CacheManager) Stats() (*StoreCacheStats, error) {
	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		return nil, err
	}

	stats := StoreCacheStats{
		TotalEntries: len(entries),
	}

	// most of the entries will have more than one hardlink, but a minority may
	// be referenced only from the cache and thus be a candidate for pruning
	stats.PruneCandidates = make([]os.FileInfo, 0, len(entries)/5)

	for _, entry := range entries {
		fi, err := entry.Info()
		if err != nil {
			return nil, err
		}

		n, err := hardLinkCount(fi)
		if err != nil {
			return nil, err
		}

		// If the file is referenced in the filesystem somewhere else our copy
		// is "free" so skip it.
		if n <= 1 {
			stats.PruneCandidates = append(stats.PruneCandidates, fi)
		}

		stats.TotalSize += uint64(fi.Size())
	}

	sort.Sort(changesByMtime(stats.PruneCandidates))

	return &stats, nil
}

func (cm *CacheManager) Cleanup() error {
	return cm.cleanup()
}
