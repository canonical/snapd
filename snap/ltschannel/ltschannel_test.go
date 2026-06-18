// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package ltschannel_test

import (
	"errors"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/snap/ltschannel"
)

func Test(t *testing.T) { TestingT(t) }

type ltsSuite struct {
	brands *assertstest.SigningAccounts
}

var _ = Suite(&ltsSuite{})

func (s *ltsSuite) SetUpTest(c *C) {
	brandKey, _ := assertstest.GenerateKey(752)
	store := assertstest.NewStoreStack("store", nil)
	s.brands = assertstest.NewSigningAccounts(store)
	s.brands.Register("my-brand", brandKey, nil)
}

func (s *ltsSuite) coreModel(c *C, base, gadget, kernel string) *asserts.Model {
	return s.brands.Model("my-brand", "my-model", map[string]any{
		"architecture": "amd64",
		"base":         base,
		"gadget":       gadget,
		"kernel":       kernel,
	})
}

func (s *ltsSuite) classicModel(c *C) *asserts.Model {
	return s.brands.Model("my-brand", "my-model", map[string]any{
		"architecture": "amd64",
		"classic":      "true",
	})
}

func (s *ltsSuite) hybridClassicModel(c *C) *asserts.Model {
	return assertstest.FakeAssertion(map[string]any{
		"type":           "model",
		"authority-id":   "my-brand",
		"brand-id":       "my-brand",
		"model":          "my-model",
		"series":         "16",
		"architecture":   "amd64",
		"classic":        "true",
		"distribution":   "ubuntu",
		"base":           "core22",
		"timestamp":      "2018-01-01T08:00:00+00:00",
		"snaps": []any{
			map[string]any{
				"name": "pc-kernel",
				"id":   "pclinuxdidididididididididididid",
				"type": "kernel",
			},
			map[string]any{
				"name": "pc",
				"id":   "pcididididididididididididididid",
				"type": "gadget",
			},
		},
	}).(*asserts.Model)
}

func (s *ltsSuite) TestSnapdLTSChannelUC18Remap(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{18: {"18", "18-fips"}})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	for _, t := range []struct {
		channel string
		want    string
	}{
		// latest variant -> 18 track, risk preserved
		{"latest/stable", "18/stable"},
		{"latest/candidate", "18/candidate"},
		{"latest/beta", "18/beta"},
		{"stable", "18/stable"},
		{"candidate", "18/candidate"},
		{"beta", "18/beta"},
		// fips-updates variant -> 18-fips track
		{"fips-updates/stable", "18-fips/stable"},
		{"fips-updates/candidate", "18-fips/candidate"},
	} {
		resolved, err := ltschannel.SnapdLTSChannel(model, t.channel)
		c.Assert(err, IsNil, Commentf("channel %q", t.channel))
		c.Check(resolved, Equals, t.want, Commentf("channel %q", t.channel))
	}
}

func (s *ltsSuite) TestSnapdLTSChannelUC18Identity(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{18: {"18", "18-fips"}})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	for _, channel := range []string{
		"18/stable",
		"18/candidate",
		"18-fips/stable",
		"18-fips/beta",
	} {
		resolved, err := ltschannel.SnapdLTSChannel(model, channel)
		c.Assert(err, IsNil, Commentf("channel %q", channel))
		c.Check(resolved, Equals, channel, Commentf("channel %q", channel))
	}
}

func (s *ltsSuite) TestSnapdLTSChannelPassthrough(c *C) {
	// Boot base 22 is unmanaged (not in the production map yet).
	model := s.coreModel(c, "core22", "pc=22", "pc-kernel=22")

	for _, channel := range []string{"latest/stable", "22/stable", "stable"} {
		resolved, err := ltschannel.SnapdLTSChannel(model, channel)
		c.Assert(err, IsNil, Commentf("channel %q", channel))
		c.Check(resolved, Equals, channel, Commentf("channel %q", channel))
	}
}

func (s *ltsSuite) TestSnapdLTSChannelMockEmptyMapPassthrough(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	resolved, err := ltschannel.SnapdLTSChannel(model, "latest/stable")
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "latest/stable")
}

func (s *ltsSuite) TestSnapdLTSChannelBranchDropped(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{18: {"18"}})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	resolved, err := ltschannel.SnapdLTSChannel(model, "latest/stable/mybranch")
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")
}

func (s *ltsSuite) TestSnapdLTSChannelErrors(c *C) {
	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{18: {"18"}})
	defer restore()

	_, err := ltschannel.SnapdLTSChannel(nil, "latest/stable")
	c.Check(err, ErrorMatches, "cannot use nil model")

	_, err = ltschannel.SnapdLTSChannel(uc18, "foo/bar/baz/quux")
	c.Check(err, ErrorMatches, `cannot parse input channel: .*`)

	// Unknown track on a managed boot base errors.
	_, err = ltschannel.SnapdLTSChannel(uc18, "20/stable")
	c.Check(err, ErrorMatches, `cannot resolve LTS channel for track "20"`)
	c.Check(errors.Is(err, ltschannel.ErrNoLTSTrack), Equals, true)
	var nolts *ltschannel.NoLTSTrackError
	c.Assert(errors.As(err, &nolts), Equals, true)
	c.Check(nolts.Track, Equals, "20")
}

func (s *ltsSuite) TestSnapdLTSChannelOutOfScopePassthrough(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{18: {"18"}})
	defer restore()

	// Classic and hybrid classic models are out of scope by default; their
	// channel is returned unchanged regardless of any LTS policy entry that
	// would otherwise apply (e.g. UC18 mapping above is irrelevant).
	resolved, err := ltschannel.SnapdLTSChannel(s.classicModel(c), "latest/stable")
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "latest/stable")

	resolved, err = ltschannel.SnapdLTSChannel(s.hybridClassicModel(c), "latest/stable")
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "latest/stable")
}

func (s *ltsSuite) TestSnapdLTSChannelUC16Rejected(c *C) {
	uc16 := s.coreModel(c, "core", "pc", "pc-kernel")
	_, err := ltschannel.SnapdLTSChannel(uc16, "latest/stable")
	c.Check(err, ErrorMatches, "cannot use unsupported Ubuntu Core 16 model")
}

func (s *ltsSuite) TestSnapdLTSChannelScopeFlags(c *C) {
	restoreMap := ltschannel.MockSnapdLTSTrackMap(map[int][]string{18: {"18"}})
	defer restoreMap()

	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	// Flip scope: Ubuntu Core off, hybrid classic on.
	restore := ltschannel.MockSnapdLTSDeviceKindScope(false, false, true)
	defer restore()

	// Ubuntu Core now out of scope -> passthrough even though UC18 is in the
	// policy map.
	resolved, err := ltschannel.SnapdLTSChannel(uc18, "latest/stable")
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "latest/stable")

	// Hybrid classic now in scope -> resolution applies; UC22 hybrid model is
	// unmanaged so still passthrough.
	resolved, err = ltschannel.SnapdLTSChannel(s.hybridClassicModel(c), "latest/stable")
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "latest/stable")
}

// uc18CandidateTrackMap is the shape returned by snap.ParseSnapdLTSTracks from a
// typical candidate snapd info file (not the test-helper []string form).
func uc18CandidateTrackMap() map[int]map[string]string {
	return map[int]map[string]string{
		18: {
			"latest":       "18",
			"18":           "18",
			"fips-updates": "18-fips",
			"18-fips":      "18-fips",
		},
	}
}

func (s *ltsSuite) TestSnapdLTSChannelWithTrackMapRemap(c *C) {
	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidateMap := uc18CandidateTrackMap()

	resolved, err := ltschannel.SnapdLTSChannelWithTrackMap(model, "latest/stable", candidateMap)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")

	resolved, err = ltschannel.SnapdLTSChannelWithTrackMap(model, "fips-updates/candidate", candidateMap)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18-fips/candidate")
}

func (s *ltsSuite) TestSnapdLTSChannelWithTrackMapUsesExplicitMapNotRunning(c *C) {
	// Running loader has no UC18 onboarded; SnapdLTSChannel would pass through.
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	resolved, err := ltschannel.SnapdLTSChannel(model, "latest/stable")
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "latest/stable")

	// Candidate inspect uses the squashfs map regardless of the running loader.
	resolved, err = ltschannel.SnapdLTSChannelWithTrackMap(model, "latest/stable", uc18CandidateTrackMap())
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")
}

func (s *ltsSuite) TestSnapdLTSChannelWithTrackMapEmptyCandidatePassthrough(c *C) {
	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	for _, candidateMap := range []map[int]map[string]string{nil, {}} {
		resolved, err := ltschannel.SnapdLTSChannelWithTrackMap(model, "latest/stable", candidateMap)
		c.Assert(err, IsNil, Commentf("map %v", candidateMap))
		c.Check(resolved, Equals, "latest/stable", Commentf("map %v", candidateMap))
	}
}

func (s *ltsSuite) TestSnapdLTSChannelWithTrackMapUnmanagedBootBase(c *C) {
	model := s.coreModel(c, "core22", "pc=22", "pc-kernel=22")
	candidateMap := uc18CandidateTrackMap()

	resolved, err := ltschannel.SnapdLTSChannelWithTrackMap(model, "latest/stable", candidateMap)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "latest/stable")
}

func (s *ltsSuite) TestSnapdLTSChannelWithTrackMapErrors(c *C) {
	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidateMap := uc18CandidateTrackMap()

	_, err := ltschannel.SnapdLTSChannelWithTrackMap(nil, "latest/stable", candidateMap)
	c.Check(err, ErrorMatches, "cannot use nil model")

	_, err = ltschannel.SnapdLTSChannelWithTrackMap(model, "20/stable", candidateMap)
	c.Check(err, ErrorMatches, `cannot resolve LTS channel for track "20"`)
	c.Check(errors.Is(err, ltschannel.ErrNoLTSTrack), Equals, true)
}
