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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/snap"
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

// tracks18 onboards boot base 18, as a full onboard would.
var tracks18 = snap.UbuntuCoreTracks{
	"18": {"latest": "18", "fips-updates": "18-fips"},
}

// tracks18Latest onboards boot base 18 for the latest track only.
var tracks18Latest = snap.UbuntuCoreTracks{
	"18": {"latest": "18"},
}

func (s *ucSuite) coreModel(base, gadget, kernel string) *asserts.Model {
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

func (s *ucSuite) classicModel() *asserts.Model {
	return s.brands.Model("my-brand", "my-model", map[string]any{
		"architecture": "amd64",
		"classic":      "true",
	})
}

func (s *ucSuite) hybridClassicModel(base string) *asserts.Model {
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

func (s *ucSuite) TestResolveUC18Remap(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

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
		{"latest", "18/stable"},
		{"edge", "18/edge"},
		// fips-updates variant -> 18-fips track
		{"fips-updates/stable", "18-fips/stable"},
		{"fips-updates/candidate", "18-fips/candidate"},
	} {
		resolved, err := uctrack.Resolve(model, t.channel, tracks18)
		c.Assert(err, IsNil, Commentf("channel %q", t.channel))
		c.Check(resolved, Equals, t.want, Commentf("channel %q", t.channel))
	}
}

func (s *ucSuite) TestResolveUC18Identity(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	for _, channel := range []string{
		"18/stable",
		"18/candidate",
		"18-fips/stable",
		"18-fips/beta",
	} {
		resolved, err := uctrack.Resolve(model, channel, tracks18)
		c.Assert(err, IsNil, Commentf("channel %q", channel))
		c.Check(resolved, Equals, channel, Commentf("channel %q", channel))
	}
}

func (s *ucSuite) TestResolveExplicitKeyWinsOverIdentity(c *C) {
	// A later onboard can remap a track onward with an explicit key.
	tracks := snap.UbuntuCoreTracks{
		"18": {"latest": "24", "18": "24"},
	}
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	resolved, err := uctrack.Resolve(model, "latest/stable", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/stable")

	resolved, err = uctrack.Resolve(model, "18/stable", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/stable")

	resolved, err = uctrack.Resolve(model, "24/edge", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/edge")
}

func (s *ucSuite) TestResolveUncoveredBootBaseErrors(c *C) {
	// Boot base 22 is not covered (not in the map; 18 is onboarded).
	model := s.coreModel("core22", "pc=22", "pc-kernel=22")

	for _, channel := range []string{"latest/stable", "22/stable", "stable"} {
		_, err := uctrack.Resolve(model, channel, tracks18Latest)
		c.Assert(err, ErrorMatches, `cannot find Ubuntu Core track map for boot base 22`, Commentf("channel %q", channel))
		c.Check(errors.Is(err, uctrack.ErrBootBaseNotCovered), Equals, true, Commentf("channel %q", channel))
	}
}

func (s *ucSuite) TestResolveBranchDropped(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")
	resolved, err := uctrack.Resolve(model, "latest/stable/mybranch", tracks18Latest)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")
}

func (s *ucSuite) TestResolveLatestTargetCollapses(c *C) {
	// "latest" is a valid target, but it is the default track and so is
	// rendered implicitly. The store reads the result as latest/stable.
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")
	resolved, err := uctrack.Resolve(model, "18/stable", snap.UbuntuCoreTracks{
		"18": {"18": "latest"},
	})
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "stable")
}

func (s *ucSuite) TestResolveErrors(c *C) {
	uc18 := s.coreModel("core18", "pc=18", "pc-kernel=18")

	_, err := uctrack.Resolve(nil, "latest/stable", tracks18Latest)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")

	_, err = uctrack.Resolve(uc18, "foo/bar/baz/quux", tracks18Latest)
	c.Check(err, ErrorMatches, `cannot parse input channel: .*`)

	// Unknown track on a covered boot base errors.
	_, err = uctrack.Resolve(uc18, "20/stable", tracks18Latest)
	c.Check(err, ErrorMatches, `cannot find Ubuntu Core track 20 for boot base 18`)
	c.Check(errors.Is(err, uctrack.ErrNoTrack), Equals, true)
}

func (s *ucSuite) TestResolveOutOfScopeNotApplicable(c *C) {
	for _, t := range []struct {
		model *asserts.Model
		err   string
	}{
		{s.classicModel(), "cannot use Ubuntu Core tracks on a classic system"},
		{s.hybridClassicModel("core22"), "cannot use Ubuntu Core tracks on a hybrid classic system"},
		{s.coreModel("bare", "pc", "pc-kernel"), `cannot use Ubuntu Core tracks: "bare" is not a core boot base`},
		{s.coreModel("alt-base", "pc", "pc-kernel"), `cannot use Ubuntu Core tracks: "alt-base" is not a core boot base`},
		// UC16 has no separate snapd snap to apply tracks to
		{s.coreModel("", "pc", "pc-kernel"), "cannot use Ubuntu Core tracks: unsupported Ubuntu Core 16 model"},
		{s.coreModel("core", "pc", "pc-kernel"), "cannot use Ubuntu Core tracks: unsupported Ubuntu Core 16 model"},
		{s.coreModel("core16", "pc", "pc-kernel"), "cannot use Ubuntu Core tracks: unsupported Ubuntu Core 16 model"},
	} {
		_, err := uctrack.Resolve(t.model, "latest/stable", tracks18Latest)
		c.Check(err, ErrorMatches, t.err, Commentf("base %q", t.model.Base()))
		c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true, Commentf("base %q", t.model.Base()))
	}
}

func (s *ucSuite) TestResolveUsesProvidedMap(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	// an empty or absent map covers no boot base at all
	for _, tracks := range []snap.UbuntuCoreTracks{{}, nil} {
		_, err := uctrack.Resolve(model, "latest/stable", tracks)
		c.Assert(err, ErrorMatches, `cannot find Ubuntu Core track map for boot base 18`)
		c.Check(errors.Is(err, uctrack.ErrBootBaseNotCovered), Equals, true)
	}

	resolved, err := uctrack.Resolve(model, "latest/stable", tracks18)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")

	resolved, err = uctrack.Resolve(model, "fips-updates/candidate", tracks18)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18-fips/candidate")
}
