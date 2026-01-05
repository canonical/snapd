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
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/strutil/quantity"
)

// DefaultCachePolicyCore is a recommended default policy for Core systems.
var DefaultCachePolicyCore = CachePolicy{
	// at most this many unreferenced items
	MaxItems: 5,
	// unreferenced items older than 30 days are removed
	MaxAge: 30 * 24 * time.Hour,
	// try to keep cache < 1GB
	MaxSizeBytes: 1 * 1024 * 1024 * 1024,
}

// DefaultCachePolicyClassic is a recommended default policy for classic
// systems.
var DefaultCachePolicyClassic = CachePolicy{
	// at most this many unreferenced items
	MaxItems: 5,
	// unreferenced items older than 30 days are removed
	MaxAge: 30 * 24 * time.Hour,
	// policy for classic systems has no size limit
}

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

// entriesByMtime sorts by the mtime of files
type entriesByMtime []os.FileInfo

func (s entriesByMtime) Len() int           { return len(s) }
func (s entriesByMtime) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s entriesByMtime) Less(i, j int) bool { return s[i].ModTime().Before(s[j].ModTime()) }

// cacheManager implements a downloadCache via content based hard linking
type CacheManager struct {
	// cleanupLock is used as a 'cleanup' synchronization point, where operations
	// for putting, getting files from the cache take a read cleanupLock, while the
	// actual cleanup operation takes the cleanupLock for writing
	cleanupLock sync.RWMutex
	cacheDir    string
	cachePolicy CachePolicy
}

// NewCacheManager returns a new CacheManager with the given cacheDir and the
// given cache policy. The idea behind it is the following algorithm:
//
//  1. When starting a download, check if it exists in $cacheDir
//  2. If found, update its mtime, hardlink into target location, and
//     return success
//  3. If not found, download the snap
//  4. On success, hardlink into $cacheDir/<digest>
//  5. Apply cache policy and remove items identified by the policy.
//
// The caching part is done here, the downloading happens in the store.go
// code.
func NewCacheManager(cacheDir string, policy CachePolicy) *CacheManager {
	return &CacheManager{
		cacheDir:    cacheDir,
		cachePolicy: policy,
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

// invokes Cleanup(), but ignores ErrCleanupBusy errors.
func (cm *CacheManager) opportunisticCleanup() error {
	if err := cm.Cleanup(); err != ErrCleanupBusy {
		return err
	}
	return nil
}

// Cleanup applies the cache policy to remove items. May return ErrCleanupBusy
// if the cleanup lock cannot be taken in which case the cleanup is skipped.
func (cm *CacheManager) Cleanup() error {
	// try to obtain exclusive lock on the cache
	if !cm.cleanupLock.TryLock() {
		return ErrCleanupBusy
	}
	defer cm.cleanupLock.Unlock()

	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		return err
	}

	var removedSize uint64
	var removedCount uint
	err = cm.cachePolicy.Apply(entries, time.Now(), func(fi os.FileInfo) error {
		path := cm.path(fi.Name())
		sz := fi.Size()
		logger.Debugf("removing %v", path)
		err := osRemove(path)
		if err != nil {
			if !os.IsNotExist(err) {
				// error here does not interrupt the cleanup, the cache policy
				// still tries to meet the targets
				logger.Noticef("cannot remove cache entry: %s", err)
				return err
			}
		} else {
			removedCount++
			removedSize += uint64(sz)
		}
		return nil
	})
	if err != nil {
		logger.Noticef("cannot apply downloads cache policy: %v", err)
	}

	logger.Noticef("removed %v entries/%s from downloads cache",
		removedCount, quantity.FormatAmount(removedSize, -1))

	return err
}

// hardLinkCount returns the number of hardlinks for the given path
func hardLinkCount(fi os.FileInfo) (uint64, error) {
	if stat, ok := fi.Sys().(*syscall.Stat_t); ok && stat != nil {
		return uint64(stat.Nlink), nil
	}
	return 0, fmt.Errorf("internal error: cannot read hardlink count from %s", fi.Name())
}

type CacheEntry struct {
	Info os.FileInfo
	// Candidate is true if the entry is a candidate for removal
	Candidate bool
	// Remove is true when entry would be removed according to the cache policy
	Remove bool
}

// StoreCacheStats contains some statistics about the store cache.
type StoreCacheStats struct {
	// TotalSize is a sum of sizes of all entries in the cache.
	TotalSize uint64
	// Entries in the cache, sorted by their modification time, starting from
	// oldest.
	Entries []CacheEntry
}

// Status returns statistics about the store cache.
func (cm *CacheManager) Stats() (*StoreCacheStats, error) {
	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		return nil, err
	}

	removeByName := map[string]bool{}
	err = cm.cachePolicy.Apply(entries, time.Now(), func(info os.FileInfo) error {
		removeByName[info.Name()] = true
		return nil
	})
	if err != nil {
		return nil, err
	}

	stats := StoreCacheStats{}

	for _, entry := range entries {
		fi, err := entry.Info()
		if err != nil {
			return nil, err
		}

		stats.TotalSize += uint64(fi.Size())

		stats.Entries = append(stats.Entries, CacheEntry{
			Info:      fi,
			Candidate: cm.cachePolicy.isCandidate(fi),
			Remove:    removeByName[fi.Name()],
		})
	}

	// TODO:GOVERSION: use slices.SortFunc
	sort.Slice(stats.Entries, func(i, j int) bool {
		return stats.Entries[i].Info.ModTime().Before(stats.Entries[j].Info.ModTime())
	})
	return &stats, nil
}

// CachePolicy defines the caching policy. Setting any of the limits to its zero
// value effectively disables it. A zero value (all fields in their default
// values) of CachePolicy means that no items would be dropped from cache,
// however places where it is used, such as Store.SetCachePolicy() may choose to
// disable all caching instead.
type CachePolicy struct {
	// MaxItems sets a target for maximum number of unique cache items.
	MaxItems int
	// MaxSizeBytes sets a target for maximum size of all unique items.
	MaxSizeBytes uint64
	// MaxAge sets a target for maximum age of unique cache items.
	MaxAge time.Duration
}

func (cp *CachePolicy) isCandidate(fi os.FileInfo) bool {
	n, err := hardLinkCount(fi)
	if err != nil {
		logger.Noticef("cannot inspect cache: %s", err)
	}

	// If the file is referenced in the filesystem somewhere else our copy
	// is "free" so skip it.
	return n <= 1
}

// Apply applies the cache policy for a given set of items and calls the
// provided drop callback to remove items from the cache.
//
// Internally, attempts to meet all targets defined in the cache policy, by
// processing unique cache items starting from oldest ones. Errors to drop items
// are collected and returned, but processing continues until targets are met or
// candidates list is exhausted.
func (cp *CachePolicy) Apply(entries []os.DirEntry, now time.Time, remove func(info os.FileInfo) error) error {
	// most of the entries will have more than one hardlink, but a minority may
	// be referenced only from the cache and thus be a candidate for pruning
	candidates := make([]os.FileInfo, 0, len(entries)/5)
	candidatesSize := uint64(0)

	for _, entry := range entries {
		fi, err := entry.Info()
		if err != nil {
			return err
		}

		if cp.isCandidate(fi) {
			candidates = append(candidates, fi)
			candidatesSize += uint64(fi.Size())
		}
	}

	sort.Sort(entriesByMtime(candidates))

	if len(candidates) > 0 {
		logger.Debugf("store cache cleanup candidates %v total %v", len(candidates),
			quantity.FormatAmount(uint64(candidatesSize), -1))
		for _, c := range candidates {
			logger.Debugf("%s, size: %v, mod %s", c.Name(), quantity.FormatAmount(uint64(c.Size()), -1), c.ModTime())
		}
	}

	var lastErr error
	removeCount := 0
	removeSize := uint64(0)
	for _, c := range candidates {
		doRemove := false
		if cp.MaxAge != 0 && c.ModTime().Add(cp.MaxAge).Before(now) {
			doRemove = true
		}

		if !doRemove && cp.MaxItems != 0 && len(candidates)-removeCount > cp.MaxItems {
			doRemove = true
		}

		if !doRemove && cp.MaxSizeBytes != 0 && candidatesSize-removeSize > cp.MaxSizeBytes {
			doRemove = true
		}

		logger.Debugf("entry %v remove %v", c.Name(), doRemove)
		if doRemove {
			if err := remove(c); err != nil {
				lastErr = strutil.JoinErrors(lastErr, err)
			} else {
				// managed to drop the items, update the counts
				removeCount++
				removeSize += uint64(c.Size())
			}
		}
	}

	logger.Debugf("cache candidates to remove %v/%s", removeCount, quantity.FormatAmount(removeSize, -1))

	return lastErr
}
