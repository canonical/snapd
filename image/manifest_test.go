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
	"github.com/snapcore/snapd/snap"
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
	// Include two entries that end on .snap as ubuntu-image
	// once produced entries looking like this
	manifestFile := s.writeSeedManifest(c, `# test line should not match
core22 275
pc 128
snapd 16681
one-snap x6
`)
	snapRevs, err := image.ReadSeedManifest(manifestFile)
	c.Assert(err, IsNil)
	c.Check(snapRevs, DeepEquals, map[string]snap.Revision{
		"core22":   snap.R(275),
		"pc":       snap.R(128),
		"snapd":    snap.R(16681),
		"one-snap": snap.R(-6),
	})
}

func (s *manifestSuite) TestReadSeedManifestParseFails(c *C) {
	tests := []struct {
		contents string
		err      string
	}{
		{"my/invalid&name 33\n", `invalid snap name: "my/invalid&name"`},
		{"core 0\n", `invalid snap revision: "0"`},
		{"core\n", `line was illegally formatted: "core"`},
	}

	for _, t := range tests {
		manifestFile := s.writeSeedManifest(c, t.contents)
		_, err := image.ReadSeedManifest(manifestFile)
		c.Check(err, ErrorMatches, t.err)
	}
}

func (s *manifestSuite) TestReadSeedManifestNoFile(c *C) {
	snapRevs, err := image.ReadSeedManifest("noexists.manifest")
	c.Assert(err, NotNil)
	c.Check(snapRevs, IsNil)
	c.Check(err, ErrorMatches, `open noexists.manifest: no such file or directory`)
}

func (s *manifestSuite) testWriteSeedManifest(c *C, revisions map[string]snap.Revision) string {
	manifestFile := filepath.Join(s.root, "seed.manifest")
	err := image.WriteSeedManifest(manifestFile, revisions)
	c.Assert(err, IsNil)
	return manifestFile
}

func (s *manifestSuite) TestWriteSeedManifestNoFile(c *C) {
	filePath := s.testWriteSeedManifest(c, map[string]snap.Revision{})
	c.Check(osutil.FileExists(filePath), Equals, false)
}

func (s *manifestSuite) TestWriteSeedManifest(c *C) {
	filePath := s.testWriteSeedManifest(c, map[string]snap.Revision{"core": {N: 12}, "test": {N: -4}})
	contents, err := ioutil.ReadFile(filePath)
	c.Assert(err, IsNil)
	c.Check(string(contents), Equals, `core 12
test x4
`)
}

func (s *manifestSuite) TestWriteSeedManifestInvalidRevision(c *C) {
	err := image.WriteSeedManifest("", map[string]snap.Revision{"core": {}})
	c.Assert(err, ErrorMatches, `revision must not be 0 for snap "core"`)
}
