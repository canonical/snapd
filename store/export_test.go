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

package store

import (
	"io"

	"github.com/juju/ratelimit"
	"gopkg.in/retry.v1"

	"github.com/snapcore/snapd/testutil"
)

var (
	HardLinkCount = hardLinkCount
)

// MockDefaultRetryStrategy mocks the retry strategy used by several store requests
func MockDefaultRetryStrategy(t *testutil.BaseTest, strategy retry.Strategy) {
	originalDefaultRetryStrategy := defaultRetryStrategy
	defaultRetryStrategy = strategy
	t.AddCleanup(func() {
		defaultRetryStrategy = originalDefaultRetryStrategy
	})
}

func (cm *CacheManager) CacheDir() string {
	return cm.cacheDir
}

func (cm *CacheManager) Cleanup() error {
	return cm.cleanup()
}

func (cm *CacheManager) Count() int {
	return cm.count()
}

func MockOsRemove(f func(name string) error) func() {
	oldOsRemove := osRemove
	osRemove = f
	return func() {
		osRemove = oldOsRemove
	}
}

func MockRatelimitReader(f func(r io.Reader, bucket *ratelimit.Bucket) io.Reader) (restore func()) {
	oldRatelimitReader := ratelimitReader
	ratelimitReader = f
	return func() {
		ratelimitReader = oldRatelimitReader
	}
}
