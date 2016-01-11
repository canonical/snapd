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
	"github.com/ubuntu-core/snappy/testutil"
)

type evalSymlinksFn func(string) (string, error)

// MockEvalSymlinks mocks the path/filepath.EvalSymlinks function for the
// purpose of the capability package. The original function is automatically
// restored at the end of the test.
func MockEvalSymlinks(s *testutil.BaseTest, fn evalSymlinksFn) {
	oldEvalSymlinks := evalSymlinks
	evalSymlinks = fn
	s.AddCleanup(func() { evalSymlinks = oldEvalSymlinks })
}

// IgnoreSymbolicLinks is a no-op version of filepath.EvalSymlinks.
func IgnoreSymbolicLinks(path string) (string, error) {
	return path, nil
}
