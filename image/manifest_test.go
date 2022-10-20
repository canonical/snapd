// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package image_test

import (
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type manifestSuite struct {
	testutil.BaseTest
	root string
}

var _ = Suite(&manifestSuite{})

func (s *manifestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.root = c.MkDir()
}

func (s *manifestSuite) writeSeedManifest(c *C, contents string) string {
	manifestFile := filepath.Join(s.root, "seed.manifest")
	err := ioutil.WriteFile(manifestFile, []byte(contents), 0644)
	c.Assert(err, IsNil)
	return manifestFile
}

func (s *manifestSuite) TestReadSeedManifestFull(c *C) {
	manifestFile := s.writeSeedManifest(c, `# test line should not match
core22 275.snap
pc 128.snap
snapd 16681.snap
dontmatch 99595
`)
	snapRevs, err := image.ReadSeedManifest(manifestFile)
	c.Assert(err, IsNil)
	c.Check(snapRevs, DeepEquals, map[string]int{
		"core22": 275,
		"pc":     128,
		"snapd":  16681,
	})
}

func (s *manifestSuite) TestReadSeedManifestInvalidRevision(c *C) {
	manifestFile := s.writeSeedManifest(c, `# test line should not match
core22 0.snap
`)
	_, err := image.ReadSeedManifest(manifestFile)
	c.Assert(err, ErrorMatches, `cannot use revision 0 for snap "core22": revision must be higher than 0`)
}

func (s *manifestSuite) TestReadSeedManifestNoFile(c *C) {
	snapRevs, err := image.ReadSeedManifest("noexists.manifest")
	c.Assert(err, NotNil)
	c.Check(snapRevs, IsNil)
	c.Check(err, ErrorMatches, `cannot read seed manifest: open noexists.manifest: no such file or directory`)
}

func (s *manifestSuite) testWriteSeedManifest(c *C, revisions map[string]int) string {
	manifestFile := filepath.Join(s.root, "seed.manifest")
	err := image.WriteSeedManifest(manifestFile, revisions)
	c.Assert(err, IsNil)
	return manifestFile
}

func (s *manifestSuite) TestWriteSeedManifestNoFile(c *C) {
	filePath := s.testWriteSeedManifest(c, map[string]int{})
	c.Check(osutil.FileExists(filePath), Equals, false)
}

func (s *manifestSuite) TestWriteSeedManifest(c *C) {
	filePath := s.testWriteSeedManifest(c, map[string]int{"core": 1, "test": 14})
	contents, err := ioutil.ReadFile(filePath)
	c.Assert(err, IsNil)
	c.Check(string(contents), Equals, `core 1.snap
test 14.snap
`)
}

func (s *manifestSuite) TestWriteSeedManifestInvalidRevision(c *C) {
	err := image.WriteSeedManifest("", map[string]int{"core": 0})
	c.Assert(err, ErrorMatches, `invalid revision 0 given for snap "core", revision must be a positive value`)
}
