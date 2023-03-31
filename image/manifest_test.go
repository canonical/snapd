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

func (s *manifestSuite) checkManifest(c *C, manifest *image.SeedManifest, allowed, used []image.SeedManifestEntry) {
	expected := image.NewSeedManifest()
	for _, a := range allowed {
		if sr, ok := a.(*image.SeedManifestSnapRevision); ok {
			err := expected.SetAllowedSnapRevision(sr.SnapName, sr.Revision.N)
			c.Assert(err, IsNil)
		} else if vs, ok := a.(*image.SeedManifestValidationSet); ok {
			err := expected.SetAllowedValidationSet(vs.AccountID, vs.Name, vs.Sequence, vs.Pinned)
			c.Assert(err, IsNil)
		}
	}
	for _, a := range used {
		if sr, ok := a.(*image.SeedManifestSnapRevision); ok {
			err := expected.MarkSnapRevisionUsed(sr.SnapName, sr.Revision.N)
			c.Assert(err, IsNil)
		} else if vs, ok := a.(*image.SeedManifestValidationSet); ok {
			err := expected.MarkValidationSetUsed(vs.AccountID, vs.Name, vs.Sequence, vs.Pinned)
			c.Assert(err, IsNil)
		}
	}
	c.Check(manifest, DeepEquals, expected)
}

func (s *manifestSuite) TestReadSeedManifestFullHappy(c *C) {
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
	s.checkManifest(c, manifest, []image.SeedManifestEntry{
		&image.SeedManifestSnapRevision{SnapName: "core22", Revision: snap.R(275)},
		&image.SeedManifestSnapRevision{SnapName: "pc", Revision: snap.R(128)},
		&image.SeedManifestSnapRevision{SnapName: "snapd", Revision: snap.R(16681)},
		&image.SeedManifestSnapRevision{SnapName: "one-snap", Revision: snap.R(-6)},
		&image.SeedManifestValidationSet{AccountID: "canonical", Name: "base-set", Sequence: 2, Pinned: true},
		&image.SeedManifestValidationSet{AccountID: "canonical", Name: "opt-set", Sequence: 5},
	}, nil)
}

func (s *manifestSuite) TestReadSeedManifestParseFails(c *C) {
	tests := []struct {
		contents string
		err      string
	}{
		{"my/validation/set 4\n", `cannot parse validation set "my/validation/set": expected a single account/name`},
		{"my/validation/set=4\n", `cannot parse validation set "my/validation/set=4": expected a single account/name`},
		{"&&asakwrjrew/awodoa 4\n", `cannot parse validation set "&&asakwrjrew/awodoa": invalid account ID "&&asakwrjrew"`},
		{"&&asakwrjrew/awodoa asdaskod\n", `cannot parse validation set "&&asakwrjrew/awodoa": invalid account ID "&&asakwrjrew"`},
		{"foo/set name\n", `invalid formatted validation-set sequence: "name"`},
		{"core\n", `line is illegally formatted: "core"`},
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

func (s *manifestSuite) testWriteSeedManifest(c *C, revisions map[string]snap.Revision, vss []*image.SeedManifestValidationSet) string {
	manifestFile := filepath.Join(s.root, "seed.manifest")
	manifest := image.NewSeedManifest()
	for sn, rev := range revisions {
		manifest.MarkSnapRevisionUsed(sn, rev.N)
	}
	for _, vs := range vss {
		manifest.MarkValidationSetUsed(vs.AccountID, vs.Name, vs.Sequence, vs.Pinned)
	}
	err := manifest.Write(manifestFile)
	c.Assert(err, IsNil)
	return manifestFile
}

func (s *manifestSuite) TestWriteSeedManifestNoFile(c *C) {
	filePath := s.testWriteSeedManifest(c, nil, nil)
	c.Check(osutil.FileExists(filePath), Equals, false)
}

func (s *manifestSuite) TestWriteSeedManifest(c *C) {
	filePath := s.testWriteSeedManifest(c,
		map[string]snap.Revision{
			"core": snap.R(12),
			"test": snap.R(-4),
		},
		[]*image.SeedManifestValidationSet{
			{
				AccountID: "canonical",
				Name:      "base-set",
				Sequence:  4,
				Pinned:    true,
			},
			{
				AccountID: "canonical",
				Name:      "opt-set",
				Sequence:  1,
			},
		},
	)
	contents, err := ioutil.ReadFile(filePath)
	c.Assert(err, IsNil)
	c.Check(string(contents), Equals, `canonical/base-set=4
canonical/opt-set 1
core 12
test x4
`)
}

func (s *manifestSuite) TestSeedManifestSetAllowedSnapRevisionInvalidRevision(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedSnapRevision("core", 0)
	c.Assert(err, ErrorMatches, `cannot add a rule for a zero-value revision`)
}

func (s *manifestSuite) TestSeedManifestSetAllowedSnapRevisionTwice(c *C) {
	// Adding two different allowed revisions, in this case the second
	// call will be a no-op.
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedSnapRevision("core", 14)
	c.Assert(err, IsNil)
	err = manifest.SetAllowedSnapRevision("core", 28)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, []image.SeedManifestEntry{
		&image.SeedManifestSnapRevision{SnapName: "core", Revision: snap.R(14)},
	}, nil)
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionUsedRuleHappy(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedSnapRevision("core", 14)
	c.Assert(err, IsNil)
	err = manifest.SetAllowedSnapRevision("pc", 1)
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionUsed("core", 14)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, []image.SeedManifestEntry{
		&image.SeedManifestSnapRevision{SnapName: "core", Revision: snap.R(14)},
		&image.SeedManifestSnapRevision{SnapName: "pc", Revision: snap.R(1)},
	}, []image.SeedManifestEntry{
		&image.SeedManifestSnapRevision{SnapName: "core", Revision: snap.R(14)},
	})
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionUsedNoRule(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedSnapRevision("core", 14)
	c.Assert(err, IsNil)
	err = manifest.SetAllowedSnapRevision("pc", 1)
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionUsed("my-snap", 1)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, []image.SeedManifestEntry{
		&image.SeedManifestSnapRevision{SnapName: "core", Revision: snap.R(14)},
		&image.SeedManifestSnapRevision{SnapName: "pc", Revision: snap.R(1)},
	}, []image.SeedManifestEntry{
		&image.SeedManifestSnapRevision{SnapName: "my-snap", Revision: snap.R(1)},
	})
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionUsedTwice(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.MarkSnapRevisionUsed("my-snap", 1)
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionUsed("my-snap", 5)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, nil, []image.SeedManifestEntry{
		&image.SeedManifestSnapRevision{SnapName: "my-snap", Revision: snap.R(1)},
	})
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionUsedWrongRevision(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedSnapRevision("core", 14)
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionUsed("core", 1)
	c.Assert(err, ErrorMatches, `revision 1 does not match the allowed revision 14`)
}

func (s *manifestSuite) TestSeedManifestMarkValidationSetUsed(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.MarkValidationSetUsed("canonical", "base-set", 4, true)
	c.Assert(err, IsNil)
	err = manifest.MarkValidationSetUsed("canonical", "opt-set", 2, false)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, nil, []image.SeedManifestEntry{
		&image.SeedManifestValidationSet{AccountID: "canonical", Name: "base-set", Sequence: 4, Pinned: true},
		&image.SeedManifestValidationSet{AccountID: "canonical", Name: "opt-set", Sequence: 2},
	})
}

func (s *manifestSuite) TestSeedManifestSetAllowedValidationSet(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedValidationSet("canonical", "base-set", 4, true)
	c.Assert(err, IsNil)
	err = manifest.SetAllowedValidationSet("canonical", "opt-set", 2, false)
	c.Assert(err, IsNil)

	// Check the allowed validation-sets returned
	c.Check(manifest.ValidationSetsAllowed(), DeepEquals, []*image.SeedManifestValidationSet{
		{AccountID: "canonical", Name: "base-set", Sequence: 4, Pinned: true},
		{AccountID: "canonical", Name: "opt-set", Sequence: 2},
	})
}

func (s *manifestSuite) TestSeedManifestMarkValidationSetUsedTwice(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.MarkValidationSetUsed("canonical", "base-set", 4, true)
	c.Assert(err, IsNil)
	err = manifest.MarkValidationSetUsed("canonical", "base-set", 5, false)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, nil, []image.SeedManifestEntry{
		&image.SeedManifestValidationSet{AccountID: "canonical", Name: "base-set", Sequence: 4, Pinned: true},
	})
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionUsedWrongSequence(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedValidationSet("canonical", "base-set", 1, true)
	c.Assert(err, IsNil)
	err = manifest.MarkValidationSetUsed("canonical", "base-set", 4, true)
	c.Assert(err, ErrorMatches, `sequence of "canonical/base-set" \(4\) does not match the allowed sequence \(1\)`)
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionUsedWrongPinned(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedValidationSet("canonical", "base-set", 1, true)
	c.Assert(err, IsNil)
	err = manifest.MarkValidationSetUsed("canonical", "base-set", 1, false)
	c.Assert(err, ErrorMatches, `pinning of "canonical/base-set" \(false\) does not match the allowed pinning \(true\)`)
}

func (s *manifestSuite) TestSeedManifestMarkValidationSetUsedInvalidSequence(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.MarkValidationSetUsed("canonical", "base-set", 0, true)
	c.Assert(err, ErrorMatches, `cannot mark validation-set "canonical/base-set" used, sequence must be set`)
}
