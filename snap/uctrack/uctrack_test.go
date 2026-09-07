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

package uctrack_test

import (
	"errors"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/snap/uctrack"
)

func Test(t *testing.T) { TestingT(t) }

type ucSuite struct {
	brands *assertstest.SigningAccounts
}

var _ = Suite(&ucSuite{})

func (s *ucSuite) SetUpTest(c *C) {
	brandKey, _ := assertstest.GenerateKey(752)
	store := assertstest.NewStoreStack("store", nil)
	s.brands = assertstest.NewSigningAccounts(store)
	s.brands.Register("my-brand", brandKey, nil)
}

func (s *ucSuite) coreModel(c *C, base, gadget, kernel string) *asserts.Model {
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

func (s *ucSuite) classicModel(c *C) *asserts.Model {
	return s.brands.Model("my-brand", "my-model", map[string]any{
		"architecture": "amd64",
		"classic":      "true",
	})
}

func (s *ucSuite) hybridClassicModel(c *C, base string) *asserts.Model {
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

func ucTrackMap(bootBase int, tracks ...string) map[int]map[string]string {
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
SNAPD_UC_TRACKS='{"18":{"latest":"18","fips-updates":"18-fips"}}'`

func (s *ucSuite) snapdContainer(c *C, info string) snap.Container {
	snapdPath := snaptest.MakeTestSnapWithFiles(c, `name: snapd
type: snapd
version: 1.0`, [][]string{{"/usr/lib/snapd/info", info}})
	snapf, err := snapfile.Open(snapdPath)
	c.Assert(err, IsNil)
	return snapf
}

func (s *ucSuite) TestSystemBootBaseApplicableClassic(c *C) {
	_, err := uctrack.SystemBootBaseApplicable(s.classicModel(c))
	c.Assert(err, ErrorMatches, "cannot use UC tracks on a classic system")
}

func (s *ucSuite) TestSystemBootBaseApplicableHybridClassic(c *C) {
	_, err := uctrack.SystemBootBaseApplicable(s.hybridClassicModel(c, "core22"))
	c.Assert(err, ErrorMatches, "cannot use UC tracks on a hybrid classic system")
}

func (s *ucSuite) TestSystemBootBaseApplicableUC18(c *C) {
	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	bootBase, err := uctrack.SystemBootBaseApplicable(uc18)
	c.Assert(err, IsNil)
	c.Check(bootBase, Equals, 18)
}

func (s *ucSuite) TestSystemBootBaseApplicableUC16HardError(c *C) {
	for _, base := range []string{"", "core", "core16"} {
		uc16 := s.coreModel(c, base, "pc", "pc-kernel")
		_, err := uctrack.SystemBootBaseApplicable(uc16)
		c.Assert(err, ErrorMatches, "cannot use UC tracks: unsupported Ubuntu Core 16 model", Commentf("base %q", base))
		c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true, Commentf("base %q", base))
	}
}

func (s *ucSuite) TestResolveUC18Remap(c *C) {
	restore := uctrack.MockSnapdUCTrackMap(ucTrackMap(18, "18", "18-fips"))
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
		resolved, err := uctrack.Resolve(model, t.channel, nil)
		c.Assert(err, IsNil, Commentf("channel %q", t.channel))
		c.Check(resolved, Equals, t.want, Commentf("channel %q", t.channel))
	}
}

func (s *ucSuite) TestResolveUC18Identity(c *C) {
	restore := uctrack.MockSnapdUCTrackMap(ucTrackMap(18, "18", "18-fips"))
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	for _, channel := range []string{
		"18/stable",
		"18/candidate",
		"18-fips/stable",
		"18-fips/beta",
	} {
		resolved, err := uctrack.Resolve(model, channel, nil)
		c.Assert(err, IsNil, Commentf("channel %q", channel))
		c.Check(resolved, Equals, channel, Commentf("channel %q", channel))
	}
}

func (s *ucSuite) TestResolveExplicitKeyWinsOverIdentity(c *C) {
	// A later onboard can remap a track onward with an explicit key.
	restore := uctrack.MockSnapdUCTrackMap(map[int]map[string]string{
		18: {"latest": "24", "18": "24"},
	})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	resolved, err := uctrack.Resolve(model, "latest/stable", nil)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/stable")

	resolved, err = uctrack.Resolve(model, "18/stable", nil)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/stable")

	resolved, err = uctrack.Resolve(model, "24/edge", nil)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/edge")
}

func (s *ucSuite) TestResolveUncoveredBootBaseErrors(c *C) {
	// Boot base 22 is not covered (not in the mocked map; 18 is onboarded).
	restore := uctrack.MockSnapdUCTrackMap(ucTrackMap(18, "18"))
	defer restore()

	model := s.coreModel(c, "core22", "pc=22", "pc-kernel=22")

	for _, channel := range []string{"latest/stable", "22/stable", "stable"} {
		_, err := uctrack.Resolve(model, channel, nil)
		c.Assert(err, ErrorMatches, `cannot find UC track map for boot base 22 from running snapd 2.75`, Commentf("channel %q", channel))
		c.Check(errors.Is(err, uctrack.ErrBootBaseNotCovered), Equals, true, Commentf("channel %q", channel))
	}
}

func (s *ucSuite) TestResolveMockEmptyMapErrors(c *C) {
	restore := uctrack.MockSnapdUCTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	_, err := uctrack.Resolve(model, "latest/stable", nil)
	c.Assert(err, ErrorMatches, `cannot find UC track map for boot base 18 from running snapd 2.75`)
	c.Check(errors.Is(err, uctrack.ErrBootBaseNotCovered), Equals, true)
}

func (s *ucSuite) TestResolveBranchDropped(c *C) {
	restore := uctrack.MockSnapdUCTrackMap(ucTrackMap(18, "18"))
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	resolved, err := uctrack.Resolve(model, "latest/stable/mybranch", nil)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")
}

func (s *ucSuite) TestResolveErrors(c *C) {
	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	restore := uctrack.MockSnapdUCTrackMap(ucTrackMap(18, "18"))
	defer restore()

	_, err := uctrack.Resolve(nil, "latest/stable", nil)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")

	_, err = uctrack.Resolve(uc18, "foo/bar/baz/quux", nil)
	c.Check(err, ErrorMatches, `cannot parse input channel: .*`)

	// Unknown track on a covered boot base errors.
	_, err = uctrack.Resolve(uc18, "20/stable", nil)
	c.Check(err, ErrorMatches, `cannot find UC track for input track 20 for boot base 18 from running snapd 2.75`)
	c.Check(errors.Is(err, uctrack.ErrNoTrack), Equals, true)
}

func (s *ucSuite) TestResolveOutOfScopeNotApplicable(c *C) {
	restore := uctrack.MockSnapdUCTrackMap(ucTrackMap(18, "18"))
	defer restore()

	// Classic and hybrid classic models are not applicable.
	_, err := uctrack.Resolve(s.classicModel(c), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "cannot use UC tracks on a classic system")
	c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true)

	_, err = uctrack.Resolve(s.hybridClassicModel(c, "core22"), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "cannot use UC tracks on a hybrid classic system")
	c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true)
}

func (s *ucSuite) TestResolveUC16Rejected(c *C) {
	for _, base := range []string{"", "core", "core16"} {
		uc16 := s.coreModel(c, base, "pc", "pc-kernel")
		_, err := uctrack.Resolve(uc16, "latest/stable", nil)
		c.Check(err, ErrorMatches, "cannot use UC tracks: unsupported Ubuntu Core 16 model", Commentf("base %q", base))
		c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true, Commentf("base %q", base))
	}
}

func (s *ucSuite) TestResolveCandidateSnapdRemap(c *C) {
	restore := uctrack.MockSnapdUCTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidateSnapd := s.snapdContainer(c, uc18CandidateSnapdInfo)

	resolved, err := uctrack.Resolve(model, "latest/stable", candidateSnapd)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")

	resolved, err = uctrack.Resolve(model, "18/stable", candidateSnapd)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")

	resolved, err = uctrack.Resolve(model, "fips-updates/candidate", candidateSnapd)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18-fips/candidate")

	resolved, err = uctrack.Resolve(model, "18-fips/beta", candidateSnapd)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18-fips/beta")
}

func (s *ucSuite) TestResolveCandidateSnapdUsesMapNotThis(c *C) {
	// This process's map has no UC18 onboarded; nil candidateSnapd errors.
	restore := uctrack.MockSnapdUCTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	_, err := uctrack.Resolve(model, "latest/stable", nil)
	c.Assert(err, ErrorMatches, `cannot find UC track map for boot base 18 from running snapd 2.75`)
	c.Check(errors.Is(err, uctrack.ErrBootBaseNotCovered), Equals, true)

	candidateSnapd := s.snapdContainer(c, uc18CandidateSnapdInfo)
	resolved, err := uctrack.Resolve(model, "latest/stable", candidateSnapd)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")
}

func (s *ucSuite) TestResolveCandidateSnapdWithoutMapErrors(c *C) {
	restore := uctrack.MockSnapdUCTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidateSnapd := s.snapdContainer(c, "VERSION=2.99\n")

	_, err := uctrack.Resolve(model, "latest/stable", candidateSnapd)
	c.Assert(err, ErrorMatches, `cannot find UC track map for boot base 18 from candidate snapd snap 2.99`)
	c.Check(errors.Is(err, uctrack.ErrBootBaseNotCovered), Equals, true)
}

func (s *ucSuite) TestResolveCandidateSnapdUncoveredBootBaseErrors(c *C) {
	model := s.coreModel(c, "core22", "pc=22", "pc-kernel=22")
	candidateSnapd := s.snapdContainer(c, uc18CandidateSnapdInfo)

	_, err := uctrack.Resolve(model, "latest/stable", candidateSnapd)
	c.Assert(err, ErrorMatches, `cannot find UC track map for boot base 22 from candidate snapd snap 2.99`)
	c.Check(errors.Is(err, uctrack.ErrBootBaseNotCovered), Equals, true)
}

func (s *ucSuite) TestResolveCandidateSnapdErrors(c *C) {
	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidateSnapd := s.snapdContainer(c, uc18CandidateSnapdInfo)

	_, err := uctrack.Resolve(nil, "latest/stable", candidateSnapd)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")

	_, err = uctrack.Resolve(model, "20/stable", candidateSnapd)
	c.Check(err, ErrorMatches, `cannot find UC track for input track 20 for boot base 18 from candidate snapd snap 2.99`)
	c.Check(errors.Is(err, uctrack.ErrNoTrack), Equals, true)

	bad := s.snapdContainer(c, "VERSION=2.99\nSNAPD_UC_TRACKS='{bad'\n")
	_, err = uctrack.Resolve(model, "latest/stable", bad)
	c.Check(err, ErrorMatches, `cannot retrieve UC track map from candidate snapd snap 2.99: cannot parse SNAPD_UC_TRACKS:.*`)

	// A full channel as the target track must not be rewritten into track/risk/risk.
	full := s.snapdContainer(c, `VERSION=2.99
SNAPD_UC_TRACKS='{"18":{"latest":"18/stable"}}'`)
	_, err = uctrack.Resolve(model, "latest/stable", full)
	c.Check(err, ErrorMatches, `cannot retrieve UC track map from candidate snapd snap 2.99: cannot parse SNAPD_UC_TRACKS: target track "18/stable" for boot base 18 is not a track-only channel`)
}

func (s *ucSuite) TestResolveCandidateSnapdMapTakesPrecedence(c *C) {
	restore := uctrack.MockSnapdUCTrackMap(ucTrackMap(18, "18"))
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	// This process's map would remap latest to 18; candidateSnapd map takes precedence.
	candidateSnapd := s.snapdContainer(c, `VERSION=2.70
SNAPD_UC_TRACKS='{"18":{"latest":"20"}}'`)

	resolved, err := uctrack.Resolve(model, "latest/stable", candidateSnapd)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "20/stable")
}
