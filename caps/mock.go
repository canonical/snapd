// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package caps

import (
	"path/filepath"
)

type evalSymlinksFn func(string) (string, error)

// evalSymlinks is either filepath.EvalSymlinks or a mocked function for
// applicable for testing.
var evalSymlinks = filepath.EvalSymlinks

// Mock EvalSymlinks function for the purpose of the capability package.
func MockEvalSymlinks(fn evalSymlinksFn) {
	evalSymlinks = fn
}

// IgnoreSymbolicLinks is a no-op version of filepath.EvalSymlinks.
func IgnoreSymbolicLinks(path string) (string, error) {
	return path, nil
}

// RestoreEvalSymlinks restores the real behavior of filepath.EvalSymlinks.
func RestoreEvalSymlinks() {
	evalSymlinks = filepath.EvalSymlinks
}
