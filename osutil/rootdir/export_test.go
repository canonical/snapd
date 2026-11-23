// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package rootdir

// ResetCallbacks removes all registered callbacks, so the tests in
// this package start each test with an empty registry: callbacks
// cannot be unregistered and would otherwise leak between tests.
//
// Test-only by design: the dirs package registers its variable
// re-derivation as a callback, and a public reset could wipe it and
// desync the dirs variables from RootDir. dirs is not in this test
// binary's closure, so resetting here is safe.
func ResetCallbacks() {
	callbacks = nil
}
