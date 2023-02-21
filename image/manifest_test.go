// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

func (s *manifestSuite) checkManifest(c *C, manifest *image.SeedManifest, rules map[string]snap.Revision, vss []*image.SeedManifestValidationSet) {
	expected := image.SeedManifestFromSnapRevisions(rules)
	for _, vs := range vss {
		expected.MarkValidationSetUsed(vs.AccountID, vs.Name, vs.Sequence, vs.Pinned)
	}
	c.Check(manifest, DeepEquals, expected)
}

func (s *manifestSuite) TestReadSeedManifestFullHAppy(c *C) {
	// Include two entries that end on .snap as ubuntu-image
	// once produced entries looking like this
	manifestFile := s.writeSeedManifest(c, `# test line should not match
canonical/base-set=2
canonical/opt-set 5
core22 275
pc 128
snapd 16681
one-snap x6
`)
	manifest, err := image.ReadSeedManifest(manifestFile)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, map[string]snap.Revision{
		"core22":   snap.R(275),
		"pc":       snap.R(128),
		"snapd":    snap.R(16681),
		"one-snap": snap.R(-6),
	}, []*image.SeedManifestValidationSet{
		{
			AccountID: "canonical",
			Name:      "base-set",
			Sequence:  2,
			Pinned:    true,
		},
		{
			AccountID: "canonical",
			Name:      "opt-set",
			Sequence:  5,
		},
	})
}

func (s *manifestSuite) TestReadSeedManifestParseFails(c *C) {
	tests := []struct {
		contents string
		err      string
	}{
		{"my/invalid&name 33\n", `invalid snap name: "my/invalid&name"`},
		{"core 0\n", `invalid snap revision: "0"`},
		{"core\n", `line is illegally formatted: "core"`},
		{" test\n", `line cannot start with any spaces: " test"`},
		{"core 14 14\n", `line is illegally formatted: "core 14 14"`},
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
	manifest := image.SeedManifestFromSnapRevisions(revisions)
	for sn, rev := range revisions {
		manifest.SetAllowedSnapRevision(sn, rev.N)
	}
	err := manifest.Write(manifestFile)
	c.Assert(err, IsNil)
	return manifestFile
}

func (s *manifestSuite) TestWriteSeedManifestNoFile(c *C) {
	filePath := s.testWriteSeedManifest(c, map[string]snap.Revision{})
	c.Check(osutil.FileExists(filePath), Equals, false)
}

func (s *manifestSuite) TestWriteSeedManifest(c *C) {
	filePath := s.testWriteSeedManifest(c, map[string]snap.Revision{"core": snap.R(12), "test": snap.R(-4)})
	contents, err := ioutil.ReadFile(filePath)
	c.Assert(err, IsNil)
	c.Check(string(contents), Equals, `core 12
test x4
`)
}

func (s *manifestSuite) TestSeedManifestSetAllowedSnapRevisionInvalidRevision(c *C) {
	manifest := &image.SeedManifest{}
	err := manifest.SetAllowedSnapRevision("core", 0)
	c.Assert(err, ErrorMatches, `cannot add a rule for a zero-value revision`)
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionUsedRuleHappy(c *C) {
	manifest := &image.SeedManifest{}
	err := manifest.SetAllowedSnapRevision("core", 14)
	c.Assert(err, IsNil)
	err = manifest.SetAllowedSnapRevision("pc", 1)
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionUsed("core", 14)
	c.Assert(err, IsNil)
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionUsedNoRule(c *C) {
	manifest := &image.SeedManifest{}
	err := manifest.SetAllowedSnapRevision("core", 14)
	c.Assert(err, IsNil)
	err = manifest.SetAllowedSnapRevision("pc", 1)
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionUsed("my-snap", 1)
	c.Assert(err, IsNil)
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionUsedWrongRevision(c *C) {
	manifest := &image.SeedManifest{}
	err := manifest.SetAllowedSnapRevision("core", 14)
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionUsed("core", 1)
	c.Assert(err, ErrorMatches, `revision does not match the value specified by revisions rules \(1 != 14\)`)
}
