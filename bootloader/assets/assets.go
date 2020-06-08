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
	"os"
	"strings"
)

var registeredAssets = map[string][]byte{}

// registerAsset registers an asset under the given name.
func registerAsset(name string, data []byte) {
	if _, ok := registeredAssets[name]; ok {
		panic(fmt.Sprintf("asset %v is already registered", name))
	}
	registeredAssets[name] = data
}

// Internal returns the content of internal registered under the given name, or
// nil when none was found.
func Internal(name string) []byte {
	return registeredAssets[name]
}

// MockInternalAsset mocks the contents of an asset for use in testing.
func MockInternalAsset(name string, data []byte) (restore func()) {
	var isSnapdTest = len(os.Args) > 0 && strings.HasSuffix(os.Args[0], ".test")
	if !isSnapdTest {
		panic("mocking can be done only in tests")
	}

	old := registeredAssets[name]
	registeredAssets[name] = data
	return func() {
		registeredAssets[name] = old
	}
}
