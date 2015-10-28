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

package lightweight_test

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/pkg/lightweight"
)

// we don't use example tests nearly as often as we should :-)
// https://blog.golang.org/examples if you haven't seen them used before.
// (also: look at it in the 'go doc' output for this package)

func ExamplePartBag() {
	d, _ := ioutil.TempDir("", "test-xyzzy-")
	defer os.RemoveAll(d)
	dirs.SetRootDir(d)
	os.MkdirAll(filepath.Join(dirs.SnapDataDir, "foo.bar", "0.1"), 0755)
	os.MkdirAll(filepath.Join(dirs.SnapDataDir, "foo.bar", "0.2"), 0755)
	os.MkdirAll(filepath.Join(dirs.SnapDataDir, "foo.bar", "0.5"), 0755)
	os.MkdirAll(filepath.Join(dirs.SnapDataDir, "baz", "0.4"), 0755)
	os.MkdirAll(filepath.Join(dirs.SnapDataDir, "qux", "0.5"), 0755)
	os.MkdirAll(filepath.Join(dirs.SnapOemDir, "qux", "0.5"), 0755)

	bags := lightweight.AllPartBags()

	for _, k := range []string{"foo.bar", "baz", "qux"} {
		bag := bags[k]
		fmt.Printf("Found %d versions for %s, type %q: %s\n",
			len(bag.Versions), bag.QualifiedName(), bag.Type, bag.Versions)
	}
	// Output:
	// Found 3 versions for foo.bar, type "app": [0.5 0.2 0.1]
	// Found 1 versions for baz, type "framework": [0.4]
	// Found 1 versions for qux, type "oem": [0.5]
}
