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

package seedwriter_test

import (
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/seed/seedwriter"
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

func (s *manifestSuite) writeManifest(c *C, contents string) string {
	manifestFile := filepath.Join(s.root, "seed.manifest")
	err := os.WriteFile(manifestFile, []byte(contents), 0644)
	c.Assert(err, IsNil)
	return manifestFile
}

func (s *manifestSuite) checkManifest(c *C, manifest *seedwriter.Manifest, revsAllowed, revsSeeded map[string]*seedwriter.ManifestSnapRevision, vsAllowed, vsSeeded map[string]*seedwriter.ManifestValidationSet) {
	expected := seedwriter.MockManifest(revsAllowed, revsSeeded, vsAllowed, vsSeeded)
	c.Check(manifest, DeepEquals, expected)
}

func (s *manifestSuite) TestReadManifestFullHappy(c *C) {
	// Include two entries that end on .snap as ubuntu-image
	// once produced entries looking like this
	manifestFile := s.writeManifest(c, `# test line should not match
canonical/base-set=2
canonical/opt-set 5
core22 275
pc 128
snapd 16681
one-snap x6
`)
	manifest, err := seedwriter.ReadManifest(manifestFile)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, map[string]*seedwriter.ManifestSnapRevision{
		"core22":   {SnapName: "core22", Revision: snap.R(275)},
		"pc":       {SnapName: "pc", Revision: snap.R(128)},
		"snapd":    {SnapName: "snapd", Revision: snap.R(16681)},
		"one-snap": {SnapName: "one-snap", Revision: snap.R(-6)},
	}, nil, map[string]*seedwriter.ManifestValidationSet{
		"canonical/base-set": {AccountID: "canonical", Name: "base-set", Sequence: 2, Pinned: true},
		"canonical/opt-set":  {AccountID: "canonical", Name: "opt-set", Sequence: 5},
	}, nil)
}

func (s *manifestSuite) TestReadManifestParseFails(c *C) {
	tests := []struct {
		contents string
		err      string
	}{
		{"my/validation/set 4\n", `cannot parse validation set "my/validation/set": expected a single account/name`},
		{"my/validation/set=4\n", `cannot parse validation set "my/validation/set=4": expected a single account/name`},
		{"&&asakwrjrew/awodoa 4\n", `cannot parse validation set "&&asakwrjrew/awodoa": invalid account ID "&&asakwrjrew"`},
		{"&&asakwrjrew/awodoa asdaskod\n", `cannot parse validation set "&&asakwrjrew/awodoa": invalid account ID "&&asakwrjrew"`},
		{"foo/set name\n", `invalid validation-set sequence: "name"`},
		{"core\n", `cannot parse line: "core"`},
		{"core 0\n", `invalid snap revision: "0"`},
		{"core\n", `cannot parse line: "core"`},
		{" test\n", `line cannot start with any spaces: " test"`},
		{"core 14 14\n", `cannot parse line: "core 14 14"`},
	}

	for _, t := range tests {
		manifestFile := s.writeManifest(c, t.contents)
		_, err := seedwriter.ReadManifest(manifestFile)
		c.Check(err, ErrorMatches, t.err)
	}
}

func (s *manifestSuite) TestReadManifestNoFile(c *C) {
	snapRevs, err := seedwriter.ReadManifest("noexists.manifest")
	c.Assert(err, NotNil)
	c.Check(snapRevs, IsNil)
	c.Check(err, ErrorMatches, `open noexists.manifest: no such file or directory`)
}

func (s *manifestSuite) testWriteManifest(c *C, revisions map[string]*seedwriter.ManifestSnapRevision, vss map[string]*seedwriter.ManifestValidationSet) string {
	manifestFile := filepath.Join(s.root, "seed.manifest")
	manifest := seedwriter.MockManifest(nil, revisions, nil, vss)
	err := manifest.Write(manifestFile)
	c.Assert(err, IsNil)
	return manifestFile
}

func (s *manifestSuite) TestWriteManifestNoFile(c *C) {
	filePath := s.testWriteManifest(c, nil, nil)
	c.Check(osutil.FileExists(filePath), Equals, false)
}

func (s *manifestSuite) TestWriteManifest(c *C) {
	filePath := s.testWriteManifest(c,
		map[string]*seedwriter.ManifestSnapRevision{
			"core": {SnapName: "core", Revision: snap.R(12)},
			"test": {SnapName: "test", Revision: snap.R(-4)},
		},
		map[string]*seedwriter.ManifestValidationSet{
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
	contents, err := os.ReadFile(filePath)
	c.Assert(err, IsNil)
	c.Check(string(contents), Equals, `canonical/base-set=4
canonical/opt-set 1
core 12
test x4
`)
}

func (s *manifestSuite) TestManifestSetAllowedSnapRevisionInvalidRevision(c *C) {
	manifest := seedwriter.NewManifest()
	err := manifest.SetAllowedSnapRevision("core", snap.R(0))
	c.Assert(err, ErrorMatches, `snap revision for "core" in manifest cannot be 0 \(unset\)`)
}

func (s *manifestSuite) TestManifestSetAllowedSnapRevision(c *C) {
	manifest := seedwriter.NewManifest()
	err := manifest.SetAllowedSnapRevision("core", snap.R(14))
	c.Assert(err, IsNil)

	// Test that it's correctly returned
	c.Check(manifest.AllowedSnapRevision("core"), DeepEquals, snap.R(14))
}

func (s *manifestSuite) TestManifestSetAllowedSnapRevisionTwice(c *C) {
	// Adding two different allowed revisions, in this case the second
	// call will be a no-op.
	manifest := seedwriter.NewManifest()
	err := manifest.SetAllowedSnapRevision("core", snap.R(14))
	c.Assert(err, IsNil)
	err = manifest.SetAllowedSnapRevision("core", snap.R(28))
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, map[string]*seedwriter.ManifestSnapRevision{
		"core": {SnapName: "core", Revision: snap.R(14)},
	}, nil, nil, nil)
}

func (s *manifestSuite) TestManifestMarkSnapRevisionSeededWithAllowedHappy(c *C) {
	manifest := seedwriter.NewManifest()
	err := manifest.SetAllowedSnapRevision("core", snap.R(14))
	c.Assert(err, IsNil)
	err = manifest.SetAllowedSnapRevision("pc", snap.R(1))
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionSeeded("core", snap.R(14))
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, map[string]*seedwriter.ManifestSnapRevision{
		"core": {SnapName: "core", Revision: snap.R(14)},
		"pc":   {SnapName: "pc", Revision: snap.R(1)},
	}, map[string]*seedwriter.ManifestSnapRevision{
		"core": {SnapName: "core", Revision: snap.R(14)},
	}, nil, nil)
}

func (s *manifestSuite) TestManifestMarkSnapRevisionSeededNoMatchingAllowed(c *C) {
	manifest := seedwriter.NewManifest()
	err := manifest.SetAllowedSnapRevision("core", snap.R(14))
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionSeeded("my-snap", snap.R(1))
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, map[string]*seedwriter.ManifestSnapRevision{
		"core": {SnapName: "core", Revision: snap.R(14)},
	}, map[string]*seedwriter.ManifestSnapRevision{
		"my-snap": {SnapName: "my-snap", Revision: snap.R(1)},
	}, nil, nil)
}

func (s *manifestSuite) TestManifestMarkSnapRevisionSeededTwice(c *C) {
	manifest := seedwriter.NewManifest()
	err := manifest.MarkSnapRevisionSeeded("my-snap", snap.R(1))
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionSeeded("my-snap", snap.R(5))
	c.Assert(err, ErrorMatches, `cannot mark \"my-snap\" \(5\) as seeded, it has already been marked seeded for revision 1`)
}

func (s *manifestSuite) TestManifestMarkSnapRevisionSeededWrongRevision(c *C) {
	manifest := seedwriter.NewManifest()
	err := manifest.SetAllowedSnapRevision("core", snap.R(14))
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionSeeded("core", snap.R(1))
	c.Assert(err, ErrorMatches, `snap "core" \(1\) does not match the allowed revision 14`)
}

func (s *manifestSuite) TestManifestSetAllowedValidationSet(c *C) {
	manifest := seedwriter.NewManifest()
	err := manifest.SetAllowedValidationSet("canonical", "base-set", 4, true)
	c.Assert(err, IsNil)
	err = manifest.SetAllowedValidationSet("canonical", "opt-set", 2, false)
	c.Assert(err, IsNil)

	// Check the allowed validation-sets returned
	c.Check(manifest.AllowedValidationSets(), DeepEquals, []*seedwriter.ManifestValidationSet{
		{AccountID: "canonical", Name: "base-set", Sequence: 4, Pinned: true},
		{AccountID: "canonical", Name: "opt-set", Sequence: 2},
	})
}

func (s *manifestSuite) TestManifestSetAllowedValidationSetTwice(c *C) {
	manifest := seedwriter.NewManifest()
	err := manifest.SetAllowedValidationSet("canonical", "base-set", 4, true)
	c.Assert(err, IsNil)
	err = manifest.SetAllowedValidationSet("canonical", "base-set", 2, false)
	c.Assert(err, IsNil)

	// Check the allowed validation-sets returned
	c.Check(manifest.AllowedValidationSets(), DeepEquals, []*seedwriter.ManifestValidationSet{
		{AccountID: "canonical", Name: "base-set", Sequence: 4, Pinned: true},
	})
}

func (s *manifestSuite) TestManifestSetAllowedValidationSetInvalidSequence(c *C) {
	manifest := seedwriter.NewManifest()
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

func (s *manifestSuite) TestManifestMarkValidationSetSeededUsedHappy(c *C) {
	vsa := s.setupValidationSet(c)

	manifest := seedwriter.NewManifest()
	err := manifest.MarkValidationSetSeeded(vsa, true)
	c.Assert(err, IsNil)
	s.checkManifest(c, manifest, map[string]*seedwriter.ManifestSnapRevision{
		"pc-kernel": {
			SnapName: "pc-kernel",
			Revision: snap.R(1),
		},
		"pc": {
			SnapName: "pc",
			Revision: snap.R(1),
		},
	}, nil, nil, map[string]*seedwriter.ManifestValidationSet{
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
	data, err := os.ReadFile(manifestFile)
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, "canonical/base-set=1\n")

	err = manifest.MarkSnapRevisionSeeded("pc-kernel", snap.R(1))
	c.Assert(err, IsNil)
	err = manifest.MarkSnapRevisionSeeded("pc", snap.R(1))
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

func (s *manifestSuite) TestManifestMarkValidationSetWeirdCases(c *C) {
	vsa := s.setupValidationSetWithNothingToTrack(c)

	manifest := seedwriter.NewManifest()
	err := manifest.MarkValidationSetSeeded(vsa, true)
	c.Assert(err, IsNil)

	// Expect us to track the validation set, but do not expect us to
	// track any snaps from it
	s.checkManifest(c, manifest, nil, nil, nil, map[string]*seedwriter.ManifestValidationSet{
		"canonical/weird-set": {
			AccountID: "canonical",
			Name:      "weird-set",
			Sequence:  1,
			Pinned:    true,
		},
	})
}

func (s *manifestSuite) TestManifestMarkValidationSetSeededUsedTwice(c *C) {
	vsa := s.setupValidationSet(c)

	manifest := seedwriter.NewManifest()
	err := manifest.MarkValidationSetSeeded(vsa, true)
	c.Assert(err, IsNil)
	err = manifest.MarkValidationSetSeeded(vsa, true)
	c.Assert(err, ErrorMatches, `cannot mark validation set "canonical/base-set" as seeded, it has already been marked as such`)
}

func (s *manifestSuite) TestManifestMarkValidationSetSeededWrongSequence(c *C) {
	vsa := s.setupValidationSet(c)

	manifest := seedwriter.NewManifest()
	err := manifest.SetAllowedValidationSet("canonical", "base-set", 4, true)
	c.Assert(err, IsNil)
	err = manifest.MarkValidationSetSeeded(vsa, true)
	c.Assert(err, ErrorMatches, `sequence of "canonical/base-set" \(1\) does not match the allowed sequence \(4\)`)
}

func (s *manifestSuite) TestManifestMarkValidationSetSeededWrongPinned(c *C) {
	vsa := s.setupValidationSet(c)

	manifest := seedwriter.NewManifest()
	err := manifest.SetAllowedValidationSet("canonical", "base-set", 1, true)
	c.Assert(err, IsNil)
	err = manifest.MarkValidationSetSeeded(vsa, false)
	c.Assert(err, ErrorMatches, `pinning of "canonical/base-set" \(false\) does not match the allowed pinning \(true\)`)
}
