// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package assets

import (
	"fmt"

	"github.com/snapcore/snapd/osutil"
)

var registeredAssets = map[string][]byte{}

// registerInternal registers an internal asset under the given name.
func registerInternal(name string, data []byte) {
	if _, ok := registeredAssets[name]; ok {
		panic(fmt.Sprintf("asset %v is already registered", name))
	}
	registeredAssets[name] = data
}

// Internal returns the content of an internal asset registered under the given
// name, or nil when none was found.
func Internal(name string) []byte {
	return registeredAssets[name]
}

// MockInternal mocks the contents of an internal asset for use in testing.
func MockInternal(name string, data []byte) (restore func()) {
	osutil.MustTestBinary("mocking can be done only in tests")

	old, ok := registeredAssets[name]
	registeredAssets[name] = data
	return func() {
		if ok {
			registeredAssets[name] = old
		} else {
			delete(registeredAssets, name)
		}
	}
}
