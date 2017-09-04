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
	"net/url"
	"reflect"

	"github.com/snapcore/snapd/testutil"

	"gopkg.in/retry.v1"
)

// MockDefaultRetryStrategy mocks the retry strategy used by several store requests
func MockDefaultRetryStrategy(t *testutil.BaseTest, strategy retry.Strategy) {
	originalDefaultRetryStrategy := defaultRetryStrategy
	defaultRetryStrategy = strategy
	t.AddCleanup(func() {
		defaultRetryStrategy = originalDefaultRetryStrategy
	})
}

func (cfg *Config) apiURIs() map[string]*url.URL {
	urls := map[string]*url.URL{}

	v := reflect.ValueOf(*cfg)
	t := reflect.TypeOf(*cfg)
	n := v.NumField()
	for i := 0; i < n; i++ {
		vf := v.Field(i)
		if u, ok := vf.Interface().(*url.URL); ok {
			urls[t.Field(i).Name] = u
		}
	}

	return urls
}
