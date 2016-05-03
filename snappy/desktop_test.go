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

package snappy

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/dirs"
	"github.com/ubuntu-core/snappy/osutil"
)

var desktopAppYaml = `
name: foo
version: 1.0
`

var mockDesktopFile = []byte(`
[Desktop Entry]
Name=foo
Icon=${SNAP}/foo.png`)

func (s *SnapTestSuite) TestDesktopFileIsAddedAndRemoved(c *C) {
	yamlFile, err := makeInstalledMockSnap(string(desktopAppYaml), 11)
	c.Assert(err, IsNil)
	snap, err := NewInstalledSnap(yamlFile)
	c.Assert(err, IsNil)

	// create a mock desktop file
	err = os.MkdirAll(filepath.Join(filepath.Dir(yamlFile), "gui"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(filepath.Dir(yamlFile), "gui", "foobar.desktop"), []byte(mockDesktopFile), 0644)
	c.Assert(err, IsNil)

	// ensure that activate creates the desktop file
	err = ActivateSnap(snap, nil)
	c.Assert(err, IsNil)

	mockDesktopFilePath := filepath.Join(dirs.SnapDesktopFilesDir, "foo_foobar.desktop")
	content, err := ioutil.ReadFile(mockDesktopFilePath)
	c.Assert(err, IsNil)
	c.Assert(string(content), Matches, "(?s).*Name=foo\n.*")

	// unlink (deactivate) removes it again
	err = UnlinkSnap(snap.Info(), nil)
	c.Assert(err, IsNil)
	c.Assert(osutil.FileExists(mockDesktopFilePath), Equals, false)
}
