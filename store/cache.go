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
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"time"

	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/strutil/quantity"
)

// overridden in the unit tests
var osRemove = os.Remove

// downloadCache is the interface that a store download cache must provide
type downloadCache interface {
	// Get retrieves the given cacheKey content and puts it into targetPath. Returns
	// true if a cached file was moved to targetPath or if one was already there.
	Get(cacheKey, targetPath string) bool
	// Put adds a new file to the cache
	Put(cacheKey, sourcePath string) error
	// Get full path of the file in cache
	GetPath(cacheKey string) string
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

// changesByMtime sorts by the mtime of files
type changesByMtime []os.FileInfo

func (s changesByMtime) Len() int           { return len(s) }
func (s changesByMtime) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
func (s changesByMtime) Less(i, j int) bool { return s[i].ModTime().Before(s[j].ModTime()) }

// cacheManager implements a downloadCache via content based hard linking
type CacheManager struct {
	cacheDir string
	maxItems int
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

// GetPath returns the full path of the given content in the cache
// or empty string
func (cm *CacheManager) GetPath(cacheKey string) string {
	if _, err := os.Stat(cm.path(cacheKey)); os.IsNotExist(err) {
		return ""
	}
	return cm.path(cacheKey)
}

// Get retrieves the given cacheKey content and puts it into targetPath. Returns
// true if a cached file was moved to targetPath or if one was already there.
func (cm *CacheManager) Get(cacheKey, targetPath string) bool {
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

	err := os.Link(sourcePath, cm.path(cacheKey))
	if os.IsExist(err) {
		now := time.Now()
		err := os.Chtimes(cm.path(cacheKey), now, now)
		// this can happen if a cleanup happens in parallel, ie.
		// the file was there but cleanup() removed it between
		// the os.Link/os.Chtimes - no biggie, just link it again
		if os.IsNotExist(err) {
			return os.Link(sourcePath, cm.path(cacheKey))
		}
		return err
	}
	if err != nil {
		return err
	}
	return cm.cleanup()
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

// cleanup ensures that only maxItems are stored in the cache
func (cm *CacheManager) cleanup() error {
	entries, err := os.ReadDir(cm.cacheDir)
	if err != nil {
		return err
	}

	if len(entries) <= cm.maxItems {
		return nil
	}

	// most of the entries will have more than one hardlink, but a minority may
	// be referenced only the cache and thus be a candidate for pruning
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
