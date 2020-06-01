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
)

var registeredAssets = map[string][]byte{}

// RegisterAsset registers a boot asset under the given name.
func RegisterAsset(name string, data []byte) {
	if _, ok := registeredAssets[name]; ok {
		panic(fmt.Sprintf("asset %v is already registered", name))
	}
	registeredAssets[name] = data
}

// GetBootAsset returns the content of boot asset registered under the given
// name, or nil when none was found.
func GetBootAsset(name string) []byte {
	return registeredAssets[name]
}

// MockBootAsset mocks the contents of boot asset for use in testing.
func MockBootAsset(name string, data []byte) (restore func()) {
	old := registeredAssets[name]
	registeredAssets[name] = data
	return func() {
		registeredAssets[name] = old
	}
}
