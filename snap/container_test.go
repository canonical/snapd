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

package snap_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapdir"
)

type FileSuite struct{}

var _ = Suite(&FileSuite{})

func (s *FileSuite) TestFileOpenForSnapDir(c *C) {
	sd := c.MkDir()
	snapYaml := filepath.Join(sd, "meta", "snap.yaml")
	err := os.MkdirAll(filepath.Dir(snapYaml), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(snapYaml, []byte(`name: foo`), 0644)
	c.Assert(err, IsNil)

	f, err := snap.Open(sd)
	c.Assert(err, IsNil)
	c.Assert(f, FitsTypeOf, &snapdir.SnapDir{})
}

func (s *FileSuite) TestFileOpenForSnapDirErrors(c *C) {
	_, err := snap.Open(c.MkDir())
	c.Assert(err, FitsTypeOf, snap.NotSnapError{})
	c.Assert(err, ErrorMatches, `"/.*" is not a snap or snapdir`)
}
