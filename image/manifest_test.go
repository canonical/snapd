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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/image"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type manifestSuite struct {
	testutil.BaseTest
	root         string
	storeSigning *assertstest.StoreStack
}

var _ = Suite(&manifestSuite{})

func (s *manifestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.root = c.MkDir()
	s.storeSigning = assertstest.NewStoreStack("canonical", nil)
}

func (s *manifestSuite) writeSeedManifest(c *C, contents string) string {
	manifestFile := filepath.Join(s.root, "seed.manifest")
	err := ioutil.WriteFile(manifestFile, []byte(contents), 0644)
	c.Assert(err, IsNil)
	return manifestFile
}

func (s *manifestSuite) checkManifest(c *C, manifest *image.SeedManifest, revsAllowed, revsSeeded map[string]*image.SeedManifestSnapRevision, vsAllowed, vsSeeded map[string]*image.SeedManifestValidationSet) {
	expected := image.NewSeedManifestForTest(revsAllowed, revsSeeded, vsAllowed, vsSeeded)
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
	s.checkManifest(c, manifest, map[string]*image.SeedManifestSnapRevision{
		"core22":   {SnapName: "core22", Revision: snap.R(275)},
		"pc":       {SnapName: "pc", Revision: snap.R(128)},
		"snapd":    {SnapName: "snapd", Revision: snap.R(16681)},
		"one-snap": {SnapName: "one-snap", Revision: snap.R(-6)},
	}, nil, map[string]*image.SeedManifestValidationSet{
		"canonical/base-set": {AccountID: "canonical", Name: "base-set", Sequence: 2, Pinned: true},
		"canonical/opt-set":  {AccountID: "canonical", Name: "opt-set", Sequence: 5},
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

func (s *manifestSuite) testWriteSeedManifest(c *C, revisions map[string]*image.SeedManifestSnapRevision, vss map[string]*image.SeedManifestValidationSet) string {
	manifestFile := filepath.Join(s.root, "seed.manifest")
	manifest := image.NewSeedManifestForTest(nil, revisions, nil, vss)
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
		map[string]*image.SeedManifestSnapRevision{
			"core": {SnapName: "core", Revision: snap.R(12)},
			"test": {SnapName: "test", Revision: snap.R(-4)},
		},
		map[string]*image.SeedManifestValidationSet{
			"canonical/base-set": {
				AccountID: "canonical",
				Name:      "base-set",
				Sequence:  4,
				Pinned:    true,
			},
			"canonical/opt-set": {
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

func (s *manifestSuite) TestSeedManifestSetAllowedSnapRevision(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedSnapRevision("core", 14)
	c.Assert(err, IsNil)

	// Test that it's correctly returned
	c.Check(manifest.AllowedSnapRevision("core"), DeepEquals, snap.R(14))
}

func (s *manifestSuite) TestSeedManifestSetAllowedSnapRevisionTwice(c *C) {
	// Adding two different allowed revisions, in this case the second
	// call will be a no-op.
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedSnapRevision("core", 14)
	c.Assert(err, IsNil)
	err = manifest.SetAllowedSnapRevision("core", 28)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, map[string]*image.SeedManifestSnapRevision{
		"core": {SnapName: "core", Revision: snap.R(14)},
	}, nil, nil, nil)
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionSeededWithAllowedHappy(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedSnapRevision("core", 14)
	c.Assert(err, IsNil)
	err = manifest.SetAllowedSnapRevision("pc", 1)
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionSeeded("core", 14)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, map[string]*image.SeedManifestSnapRevision{
		"core": {SnapName: "core", Revision: snap.R(14)},
		"pc":   {SnapName: "pc", Revision: snap.R(1)},
	}, map[string]*image.SeedManifestSnapRevision{
		"core": {SnapName: "core", Revision: snap.R(14)},
	}, nil, nil)
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionSeededNoMatchingAllowed(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedSnapRevision("core", 14)
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionSeeded("my-snap", 1)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, map[string]*image.SeedManifestSnapRevision{
		"core": {SnapName: "core", Revision: snap.R(14)},
	}, map[string]*image.SeedManifestSnapRevision{
		"my-snap": {SnapName: "my-snap", Revision: snap.R(1)},
	}, nil, nil)
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionSeededTwice(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.MarkSnapRevisionSeeded("my-snap", 1)
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionSeeded("my-snap", 5)
	c.Assert(err, ErrorMatches, `cannot mark "my-snap" \(5\) as seeded, it has already been marked seeded: "my-snap" \(1\)`)
}

func (s *manifestSuite) TestSeedManifestMarkSnapRevisionSeededWrongRevision(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedSnapRevision("core", 14)
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionSeeded("core", 1)
	c.Assert(err, ErrorMatches, `revision 1 does not match the allowed revision 14`)
}

func (s *manifestSuite) TestSeedManifestSetAllowedValidationSet(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedValidationSet("canonical", "base-set", 4, true)
	c.Assert(err, IsNil)
	err = manifest.SetAllowedValidationSet("canonical", "opt-set", 2, false)
	c.Assert(err, IsNil)

	// Check the allowed validation-sets returned
	c.Check(manifest.AllowedValidationSets(), DeepEquals, []*image.SeedManifestValidationSet{
		{AccountID: "canonical", Name: "base-set", Sequence: 4, Pinned: true},
		{AccountID: "canonical", Name: "opt-set", Sequence: 2},
	})
}

func (s *manifestSuite) TestSeedManifestSetAllowedValidationSetTwice(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedValidationSet("canonical", "base-set", 4, true)
	c.Assert(err, IsNil)
	err = manifest.SetAllowedValidationSet("canonical", "base-set", 2, false)
	c.Assert(err, IsNil)

	// Check the allowed validation-sets returned
	c.Check(manifest.AllowedValidationSets(), DeepEquals, []*image.SeedManifestValidationSet{
		{AccountID: "canonical", Name: "base-set", Sequence: 4, Pinned: true},
	})
}

func (s *manifestSuite) TestSeedManifestSetAllowedValidationSetInvalidSequence(c *C) {
	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedValidationSet("canonical", "base-set", 0, true)
	c.Assert(err, ErrorMatches, `cannot add allowed validation set "canonical/base-set" for a unknown sequence`)
}

func (s *manifestSuite) setupValidationSet(c *C) *asserts.ValidationSet {
	vs, err := s.storeSigning.Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "base-set",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       "123456ididididididididididididid",
				"presence": "required",
				"revision": "1",
			},
			map[string]interface{}{
				"name":     "pc",
				"id":       "mysnapididididididididididididid",
				"presence": "required",
				"revision": "1",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(vs)
	c.Check(err, IsNil)
	return vs.(*asserts.ValidationSet)
}

func (s *manifestSuite) TestSeedManifestMarkValidationSetSeededUsedHappy(c *C) {
	vsa := s.setupValidationSet(c)

	manifest := image.NewSeedManifest()
	err := manifest.MarkValidationSetSeeded(vsa, true)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, map[string]*image.SeedManifestSnapRevision{
		"pc-kernel": {
			SnapName: "pc-kernel",
			Revision: snap.R(1),
		},
		"pc": {
			SnapName: "pc",
			Revision: snap.R(1),
		},
	}, nil, nil, map[string]*image.SeedManifestValidationSet{
		"canonical/base-set": {
			AccountID: "canonical",
			Name:      "base-set",
			Sequence:  1,
			Pinned:    true,
			Snaps:     []string{"pc-kernel", "pc"},
		},
	})

	// Write the seed.manifest and verify pc-kernel and pc is not written
	manifestFile := filepath.Join(s.root, "seed.manifest")
	manifest.Write(manifestFile)

	// Read it back in and verify contents
	data, err := ioutil.ReadFile(manifestFile)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "canonical/base-set=1\n")

	err = manifest.MarkSnapRevisionSeeded("pc-kernel", 1)
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionSeeded("pc", 1)
	c.Assert(err, IsNil)
}

func (s *manifestSuite) setupValidationSetWithNothingToTrack(c *C) *asserts.ValidationSet {
	vs, err := s.storeSigning.Sign(asserts.ValidationSetType, map[string]interface{}{
		"type":         "validation-set",
		"authority-id": "canonical",
		"series":       "16",
		"account-id":   "canonical",
		"name":         "weird-set",
		"sequence":     "1",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":     "pc-kernel",
				"id":       "123456ididididididididididididid",
				"presence": "required",
			},
			map[string]interface{}{
				"name":     "pc",
				"id":       "mysnapididididididididididididid",
				"presence": "invalid",
			},
		},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)
	err = s.storeSigning.Add(vs)
	c.Check(err, IsNil)
	return vs.(*asserts.ValidationSet)
}

func (s *manifestSuite) TestSeedManifestMarkValidationSetWeirdCases(c *C) {
	vsa := s.setupValidationSetWithNothingToTrack(c)

	manifest := image.NewSeedManifest()
	err := manifest.MarkValidationSetSeeded(vsa, true)
	c.Assert(err, IsNil)

	// Expect us to track the validation set, but do not expect us to
	// track any snaps from it
	s.checkManifest(c, manifest, nil, nil, nil, map[string]*image.SeedManifestValidationSet{
		"canonical/weird-set": {
			AccountID: "canonical",
			Name:      "weird-set",
			Sequence:  1,
			Pinned:    true,
		},
	})
}

func (s *manifestSuite) TestSeedManifestMarkValidationSetSeededUsedTwice(c *C) {
	vsa := s.setupValidationSet(c)

	manifest := image.NewSeedManifest()
	err := manifest.MarkValidationSetSeeded(vsa, true)
	c.Assert(err, IsNil)
	err = manifest.MarkValidationSetSeeded(vsa, true)
	c.Assert(err, ErrorMatches, `cannot mark validation set "canonical/base-set" as seeded, it has already been marked as such`)
}

func (s *manifestSuite) TestSeedManifestMarkValidationSetSeededWrongSequence(c *C) {
	vsa := s.setupValidationSet(c)

	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedValidationSet("canonical", "base-set", 4, true)
	c.Assert(err, IsNil)
	err = manifest.MarkValidationSetSeeded(vsa, true)
	c.Assert(err, ErrorMatches, `sequence of "canonical/base-set" \(1\) does not match the allowed sequence \(4\)`)
}

func (s *manifestSuite) TestSeedManifestMarkValidationSetSeededWrongPinned(c *C) {
	vsa := s.setupValidationSet(c)

	manifest := image.NewSeedManifest()
	err := manifest.SetAllowedValidationSet("canonical", "base-set", 1, true)
	c.Assert(err, IsNil)
	err = manifest.MarkValidationSetSeeded(vsa, false)
	c.Assert(err, ErrorMatches, `pinning of "canonical/base-set" \(false\) does not match the allowed pinning \(true\)`)
}
