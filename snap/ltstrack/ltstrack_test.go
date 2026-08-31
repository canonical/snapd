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

package ltstrack_test

import (
	"errors"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/ltstrack"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
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
	headers := map[string]any{
		"architecture": "amd64",
		"gadget":       gadget,
		"kernel":       kernel,
	}
	if base != "" {
		headers["base"] = base
	}
	return s.brands.Model("my-brand", "my-model", headers)
}

func (s *ltsSuite) classicModel(c *C) *asserts.Model {
	return s.brands.Model("my-brand", "my-model", map[string]any{
		"architecture": "amd64",
		"classic":      "true",
	})
}

func (s *ltsSuite) hybridClassicModel(c *C, base string) *asserts.Model {
	return assertstest.FakeAssertion(map[string]any{
		"type":         "model",
		"authority-id": "my-brand",
		"brand-id":     "my-brand",
		"model":        "my-model",
		"series":       "16",
		"architecture": "amd64",
		"classic":      "true",
		"distribution": "ubuntu",
		"base":         base,
		"timestamp":    "2018-01-01T08:00:00+00:00",
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

func ltsTrackMap(bootBase int, tracks ...string) map[int]map[string]string {
	if len(tracks) == 0 {
		return map[int]map[string]string{}
	}
	rules := map[string]string{
		"latest": tracks[0],
	}
	for _, track := range tracks {
		if strings.HasSuffix(track, "-fips") {
			rules["fips-updates"] = track
		}
	}
	return map[int]map[string]string{bootBase: rules}
}

const uc18CandidateSnapdInfo = `VERSION=2.99
SNAPD_LTS_TRACKS='{"18":{"latest":"18","fips-updates":"18-fips"}}'`

func (s *ltsSuite) snapdContainer(c *C, info string) snap.Container {
	snapdPath := snaptest.MakeTestSnapWithFiles(c, `name: snapd
type: snapd
version: 1.0`, [][]string{{"/usr/lib/snapd/info", info}})
	snapf, err := snapfile.Open(snapdPath)
	c.Assert(err, IsNil)
	return snapf
}

func (s *ltsSuite) TestSystemBootBaseApplicableClassic(c *C) {
	_, err := ltstrack.SystemBootBaseApplicable(s.classicModel(c))
	c.Assert(err, ErrorMatches, "cannot use LTS tracks on a classic system")
}

func (s *ltsSuite) TestSystemBootBaseApplicableHybridClassic(c *C) {
	_, err := ltstrack.SystemBootBaseApplicable(s.hybridClassicModel(c, "core22"))
	c.Assert(err, ErrorMatches, "cannot use LTS tracks on a hybrid classic system")
}

func (s *ltsSuite) TestSystemBootBaseApplicableUC18(c *C) {
	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	bootBase, err := ltstrack.SystemBootBaseApplicable(uc18)
	c.Assert(err, IsNil)
	c.Check(bootBase, Equals, 18)
}

func (s *ltsSuite) TestSystemBootBaseApplicableUC16HardError(c *C) {
	for _, base := range []string{"", "core", "core16"} {
		uc16 := s.coreModel(c, base, "pc", "pc-kernel")
		_, err := ltstrack.SystemBootBaseApplicable(uc16)
		c.Assert(err, ErrorMatches, "cannot use LTS tracks: unsupported Ubuntu Core 16 model", Commentf("base %q", base))
		c.Check(errors.Is(err, ltstrack.ErrNotApplicable), Equals, true, Commentf("base %q", base))
	}
}

func (s *ltsSuite) TestResolveUC18Remap(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18", "18-fips"))
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
		resolved, err := ltstrack.Resolve(model, t.channel, nil)
		c.Assert(err, IsNil, Commentf("channel %q", t.channel))
		c.Check(resolved, Equals, t.want, Commentf("channel %q", t.channel))
	}
}

func (s *ltsSuite) TestResolveUC18Identity(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18", "18-fips"))
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	for _, channel := range []string{
		"18/stable",
		"18/candidate",
		"18-fips/stable",
		"18-fips/beta",
	} {
		resolved, err := ltstrack.Resolve(model, channel, nil)
		c.Assert(err, IsNil, Commentf("channel %q", channel))
		c.Check(resolved, Equals, channel, Commentf("channel %q", channel))
	}
}

func (s *ltsSuite) TestResolveExplicitKeyWinsOverIdentity(c *C) {
	// A later onboard can remap an LTS track onward with an explicit key.
	restore := ltstrack.MockSnapdLTSTrackMap(map[int]map[string]string{
		18: {"latest": "24", "18": "24"},
	})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	resolved, err := ltstrack.Resolve(model, "latest/stable", nil)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/stable")

	resolved, err = ltstrack.Resolve(model, "18/stable", nil)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/stable")

	resolved, err = ltstrack.Resolve(model, "24/edge", nil)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/edge")
}

func (s *ltsSuite) TestResolveUncoveredBootBaseErrors(c *C) {
	// Boot base 22 is not covered (not in the mocked map; 18 is onboarded).
	restore := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	model := s.coreModel(c, "core22", "pc=22", "pc-kernel=22")

	for _, channel := range []string{"latest/stable", "22/stable", "stable"} {
		_, err := ltstrack.Resolve(model, channel, nil)
		c.Assert(err, ErrorMatches, `cannot find LTS track map for boot base 22 from running snapd 2.75`, Commentf("channel %q", channel))
		c.Check(errors.Is(err, ltstrack.ErrBootBaseNotCovered), Equals, true, Commentf("channel %q", channel))
	}
}

func (s *ltsSuite) TestResolveMockEmptyMapErrors(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	_, err := ltstrack.Resolve(model, "latest/stable", nil)
	c.Assert(err, ErrorMatches, `cannot find LTS track map for boot base 18 from running snapd 2.75`)
	c.Check(errors.Is(err, ltstrack.ErrBootBaseNotCovered), Equals, true)
}

func (s *ltsSuite) TestResolveBranchDropped(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	resolved, err := ltstrack.Resolve(model, "latest/stable/mybranch", nil)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")
}

func (s *ltsSuite) TestResolveErrors(c *C) {
	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	restore := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	_, err := ltstrack.Resolve(nil, "latest/stable", nil)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")

	_, err = ltstrack.Resolve(uc18, "foo/bar/baz/quux", nil)
	c.Check(err, ErrorMatches, `cannot parse input channel: .*`)

	// Unknown track on a covered boot base errors.
	_, err = ltstrack.Resolve(uc18, "20/stable", nil)
	c.Check(err, ErrorMatches, `cannot find LTS track for input track 20 for boot base 18 from running snapd 2.75`)
	c.Check(errors.Is(err, ltstrack.ErrNoTrack), Equals, true)
}

func (s *ltsSuite) TestResolveOutOfScopeNotApplicable(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	// Classic and hybrid classic models are not applicable.
	_, err := ltstrack.Resolve(s.classicModel(c), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "cannot use LTS tracks on a classic system")
	c.Check(errors.Is(err, ltstrack.ErrNotApplicable), Equals, true)

	_, err = ltstrack.Resolve(s.hybridClassicModel(c, "core22"), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "cannot use LTS tracks on a hybrid classic system")
	c.Check(errors.Is(err, ltstrack.ErrNotApplicable), Equals, true)
}

func (s *ltsSuite) TestResolveUC16Rejected(c *C) {
	for _, base := range []string{"", "core", "core16"} {
		uc16 := s.coreModel(c, base, "pc", "pc-kernel")
		_, err := ltstrack.Resolve(uc16, "latest/stable", nil)
		c.Check(err, ErrorMatches, "cannot use LTS tracks: unsupported Ubuntu Core 16 model", Commentf("base %q", base))
		c.Check(errors.Is(err, ltstrack.ErrNotApplicable), Equals, true, Commentf("base %q", base))
	}
}

func (s *ltsSuite) TestResolveCandidateSnapdRemap(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidateSnapd := s.snapdContainer(c, uc18CandidateSnapdInfo)

	resolved, err := ltstrack.Resolve(model, "latest/stable", candidateSnapd)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")

	resolved, err = ltstrack.Resolve(model, "18/stable", candidateSnapd)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")

	resolved, err = ltstrack.Resolve(model, "fips-updates/candidate", candidateSnapd)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18-fips/candidate")

	resolved, err = ltstrack.Resolve(model, "18-fips/beta", candidateSnapd)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18-fips/beta")
}

func (s *ltsSuite) TestResolveCandidateSnapdUsesMapNotThis(c *C) {
	// This process's map has no UC18 onboarded; nil candidateSnapd errors.
	restore := ltstrack.MockSnapdLTSTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	_, err := ltstrack.Resolve(model, "latest/stable", nil)
	c.Assert(err, ErrorMatches, `cannot find LTS track map for boot base 18 from running snapd 2.75`)
	c.Check(errors.Is(err, ltstrack.ErrBootBaseNotCovered), Equals, true)

	candidateSnapd := s.snapdContainer(c, uc18CandidateSnapdInfo)
	resolved, err := ltstrack.Resolve(model, "latest/stable", candidateSnapd)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")
}

func (s *ltsSuite) TestResolveCandidateSnapdWithoutMapErrors(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidateSnapd := s.snapdContainer(c, "VERSION=2.99\n")

	_, err := ltstrack.Resolve(model, "latest/stable", candidateSnapd)
	c.Assert(err, ErrorMatches, `cannot find LTS track map for boot base 18 from candidate snapd snap 2.99`)
	c.Check(errors.Is(err, ltstrack.ErrBootBaseNotCovered), Equals, true)
}

func (s *ltsSuite) TestResolveCandidateSnapdUncoveredBootBaseErrors(c *C) {
	model := s.coreModel(c, "core22", "pc=22", "pc-kernel=22")
	candidateSnapd := s.snapdContainer(c, uc18CandidateSnapdInfo)

	_, err := ltstrack.Resolve(model, "latest/stable", candidateSnapd)
	c.Assert(err, ErrorMatches, `cannot find LTS track map for boot base 22 from candidate snapd snap 2.99`)
	c.Check(errors.Is(err, ltstrack.ErrBootBaseNotCovered), Equals, true)
}

func (s *ltsSuite) TestResolveCandidateSnapdErrors(c *C) {
	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidateSnapd := s.snapdContainer(c, uc18CandidateSnapdInfo)

	_, err := ltstrack.Resolve(nil, "latest/stable", candidateSnapd)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")

	_, err = ltstrack.Resolve(model, "20/stable", candidateSnapd)
	c.Check(err, ErrorMatches, `cannot find LTS track for input track 20 for boot base 18 from candidate snapd snap 2.99`)
	c.Check(errors.Is(err, ltstrack.ErrNoTrack), Equals, true)

	bad := s.snapdContainer(c, "VERSION=2.99\nSNAPD_LTS_TRACKS='{bad'\n")
	_, err = ltstrack.Resolve(model, "latest/stable", bad)
	c.Check(err, ErrorMatches, `cannot retrieve LTS track map from candidate snapd snap 2.99: cannot parse SNAPD_LTS_TRACKS:.*`)

	// A full channel as the LTS target must not be rewritten into track/risk/risk.
	full := s.snapdContainer(c, `VERSION=2.99
SNAPD_LTS_TRACKS='{"18":{"latest":"18/stable"}}'`)
	_, err = ltstrack.Resolve(model, "latest/stable", full)
	c.Check(err, ErrorMatches, `cannot retrieve LTS track map from candidate snapd snap 2.99: cannot parse SNAPD_LTS_TRACKS: LTS target "18/stable" for boot base 18 is not a track-only channel`)
}

func (s *ltsSuite) TestResolveCandidateSnapdMapTakesPrecedence(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	// This process's map would remap latest to 18; candidateSnapd map takes precedence.
	candidateSnapd := s.snapdContainer(c, `VERSION=2.70
SNAPD_LTS_TRACKS='{"18":{"latest":"20"}}'`)

	resolved, err := ltstrack.Resolve(model, "latest/stable", candidateSnapd)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "20/stable")
}
