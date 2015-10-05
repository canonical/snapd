// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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

package husk_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"launchpad.net/snappy/dirs"
	"launchpad.net/snappy/pkg/husk"
)

func ExampleHusk() {
	d, _ := ioutil.TempDir("", "test-xyzzy-")
	defer os.RemoveAll(d)
	dirs.SetRootDir(d)
	os.MkdirAll(filepath.Join(dirs.SnapDataDir, "foo.bar", "0.1"), 0755)
	os.MkdirAll(filepath.Join(dirs.SnapDataDir, "foo.bar", "0.2"), 0755)
	os.MkdirAll(filepath.Join(dirs.SnapDataDir, "foo.bar", "0.5"), 0755)
	os.MkdirAll(filepath.Join(dirs.SnapDataDir, "baz", "0.4"), 0755)
	os.MkdirAll(filepath.Join(dirs.SnapDataDir, "qux", "0.5"), 0755)
	os.MkdirAll(filepath.Join(dirs.SnapOemDir, "qux", "0.5"), 0755)

	husks := husk.All()

	for _, k := range []string{"foo.bar", "baz", "qux"} {
		h := husks[k]
		fmt.Printf("Found %d versions for %s, type %q: %s\n",
			len(h.Versions), h.QualifiedName(), h.Type, h.Versions)
	}
	// Output:
	// Found 3 versions for foo.bar, type "app": [0.5 0.2 0.1]
	// Found 1 versions for baz, type "framework": [0.4]
	// Found 1 versions for qux, type "oem": [0.5]
}
