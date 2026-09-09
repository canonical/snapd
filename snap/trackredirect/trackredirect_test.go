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

package trackredirect_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/snap/trackredirect"
)

func Test(t *testing.T) { TestingT(t) }

type trackredirectSuite struct {
	brands *assertstest.SigningAccounts
}

var _ = Suite(&trackredirectSuite{})

func (s *trackredirectSuite) SetUpTest(c *C) {
	brandKey, _ := assertstest.GenerateKey(752)
	store := assertstest.NewStoreStack("store", nil)
	s.brands = assertstest.NewSigningAccounts(store)
	s.brands.Register("my-brand", brandKey, nil)
}

func (s *trackredirectSuite) coreModel(c *C, base, gadget, kernel string) *asserts.Model {
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

func (s *trackredirectSuite) classicModel(c *C) *asserts.Model {
	return s.brands.Model("my-brand", "my-model", map[string]any{
		"architecture": "amd64",
		"classic":      "true",
	})
}

func (s *trackredirectSuite) hybridClassicModel(c *C, base string) *asserts.Model {
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

func ucKey(version string) trackredirect.Key {
	return trackredirect.Key{ID: trackredirect.UbuntuCoreID, Version: version}
}

func trackRedirectMap(bootBase int, tracks ...string) map[string]map[string]map[string]string {
	if len(tracks) == 0 {
		return map[string]map[string]map[string]string{}
	}
	rules := map[string]string{
		"latest": tracks[0],
	}
	for _, track := range tracks {
		if strings.HasSuffix(track, "-fips") {
			rules["fips-updates"] = track
		}
	}
	return map[string]map[string]map[string]string{
		trackredirect.UbuntuCoreID: {strconv.Itoa(bootBase): rules},
	}
}

func (s *trackredirectSuite) TestUbuntuCoreKeyClassic(c *C) {
	_, err := trackredirect.UbuntuCoreKey(s.classicModel(c))
	c.Assert(err, ErrorMatches, "cannot use UC tracks on a classic system")
	c.Check(errors.Is(err, trackredirect.ErrNotApplicable), Equals, true)
}

func (s *trackredirectSuite) TestUbuntuCoreKeyHybridClassic(c *C) {
	_, err := trackredirect.UbuntuCoreKey(s.hybridClassicModel(c, "core22"))
	c.Assert(err, ErrorMatches, "cannot use UC tracks on a hybrid classic system")
	c.Check(errors.Is(err, trackredirect.ErrNotApplicable), Equals, true)
}

func (s *trackredirectSuite) TestUbuntuCoreKeyUC18(c *C) {
	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	key, err := trackredirect.UbuntuCoreKey(uc18)
	c.Assert(err, IsNil)
	c.Check(key, DeepEquals, ucKey("18"))
}

func (s *trackredirectSuite) TestUbuntuCoreKeyUC16(c *C) {
	for _, base := range []string{"", "core", "core16"} {
		uc16 := s.coreModel(c, base, "pc", "pc-kernel")
		_, err := trackredirect.UbuntuCoreKey(uc16)
		c.Assert(err, ErrorMatches, "cannot use UC tracks: unsupported Ubuntu Core 16 model", Commentf("base %q", base))
		c.Check(errors.Is(err, trackredirect.ErrNotApplicable), Equals, true, Commentf("base %q", base))
	}
}

func (s *trackredirectSuite) TestUbuntuCoreKeyNilModel(c *C) {
	_, err := trackredirect.UbuntuCoreKey(nil)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")
}

func (s *trackredirectSuite) TestUbuntuCoreKeyNonCoreBase(c *C) {
	model := s.coreModel(c, "bare", "pc", "pc-kernel")
	_, err := trackredirect.UbuntuCoreKey(model)
	c.Check(err, ErrorMatches, "cannot determine boot base: not a core base")
}

func (s *trackredirectSuite) TestResolveUC18Remap(c *C) {
	trackMap := trackRedirectMap(18, "18", "18-fips")

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
		resolved, err := trackredirect.Resolve(ucKey("18"), t.channel, trackMap)
		c.Assert(err, IsNil, Commentf("channel %q", t.channel))
		c.Check(resolved, Equals, t.want, Commentf("channel %q", t.channel))
	}
}

func (s *trackredirectSuite) TestResolveUC18Identity(c *C) {
	trackMap := trackRedirectMap(18, "18", "18-fips")

	for _, channel := range []string{
		"18/stable",
		"18/candidate",
		"18-fips/stable",
		"18-fips/beta",
	} {
		resolved, err := trackredirect.Resolve(ucKey("18"), channel, trackMap)
		c.Assert(err, IsNil, Commentf("channel %q", channel))
		c.Check(resolved, Equals, channel, Commentf("channel %q", channel))
	}
}

func (s *trackredirectSuite) TestResolveExplicitKeyWinsOverIdentity(c *C) {
	// A later onboard can remap a track onward with an explicit key.
	trackMap := map[string]map[string]map[string]string{
		trackredirect.UbuntuCoreID: {
			"18": {"latest": "24", "18": "24"},
		},
	}

	resolved, err := trackredirect.Resolve(ucKey("18"), "latest/stable", trackMap)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/stable")

	resolved, err = trackredirect.Resolve(ucKey("18"), "18/stable", trackMap)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/stable")

	resolved, err = trackredirect.Resolve(ucKey("18"), "24/edge", trackMap)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/edge")
}

func (s *trackredirectSuite) TestResolveUncoveredVersion(c *C) {
	// Version 22 is not covered (not in the map; 18 is onboarded).
	trackMap := trackRedirectMap(18, "18")

	for _, channel := range []string{"latest/stable", "22/stable", "stable"} {
		_, err := trackredirect.Resolve(ucKey("22"), channel, trackMap)
		c.Assert(err, ErrorMatches, `cannot find track redirects for ubuntu-core 22`, Commentf("channel %q", channel))
		c.Check(errors.Is(err, trackredirect.ErrNotCovered), Equals, true, Commentf("channel %q", channel))
	}
}

func (s *trackredirectSuite) TestResolveEmptyMapErrors(c *C) {
	_, err := trackredirect.Resolve(ucKey("18"), "latest/stable", map[string]map[string]map[string]string{})
	c.Assert(err, ErrorMatches, `cannot find track redirects for ubuntu-core 18`)
	c.Check(errors.Is(err, trackredirect.ErrNotCovered), Equals, true)

	_, err = trackredirect.Resolve(ucKey("18"), "latest/stable", nil)
	c.Assert(err, ErrorMatches, `cannot find track redirects for ubuntu-core 18`)
	c.Check(errors.Is(err, trackredirect.ErrNotCovered), Equals, true)
}

func (s *trackredirectSuite) TestResolveBranchDropped(c *C) {
	resolved, err := trackredirect.Resolve(ucKey("18"), "latest/stable/mybranch", trackRedirectMap(18, "18"))
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")
}

func (s *trackredirectSuite) TestResolveErrors(c *C) {
	trackMap := trackRedirectMap(18, "18")

	_, err := trackredirect.Resolve(ucKey("18"), "foo/bar/baz/quux", trackMap)
	c.Check(err, ErrorMatches, `cannot parse input channel: .*`)

	// Unknown track on a covered version errors.
	_, err = trackredirect.Resolve(ucKey("18"), "20/stable", trackMap)
	c.Check(err, ErrorMatches, `cannot find track redirect for input track 20 for ubuntu-core 18`)
	c.Check(errors.Is(err, trackredirect.ErrNoTrack), Equals, true)
}

func (s *trackredirectSuite) TestResolveEmptyKey(c *C) {
	_, err := trackredirect.Resolve(trackredirect.Key{}, "latest/stable", trackRedirectMap(18, "18"))
	c.Check(err, ErrorMatches, "internal error: cannot resolve track redirects with empty key")

	_, err = trackredirect.Resolve(trackredirect.Key{ID: trackredirect.UbuntuCoreID}, "latest/stable", trackRedirectMap(18, "18"))
	c.Check(err, ErrorMatches, "internal error: cannot resolve track redirects with empty key")

	_, err = trackredirect.Resolve(trackredirect.Key{Version: "18"}, "latest/stable", trackRedirectMap(18, "18"))
	c.Check(err, ErrorMatches, "internal error: cannot resolve track redirects with empty key")
}

func (s *trackredirectSuite) TestResolveIgnoresUnusedID(c *C) {
	trackMap := map[string]map[string]map[string]string{
		trackredirect.UbuntuCoreID: {
			"18": {"latest": "18"},
		},
		"ubuntu": {
			"22.04": {"latest": "22.04"},
		},
	}

	resolved, err := trackredirect.Resolve(ucKey("18"), "latest/stable", trackMap)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")
}

func (s *trackredirectSuite) TestResolveUbuntuID(c *C) {
	trackMap := map[string]map[string]map[string]string{
		"ubuntu": {
			"22.04": {"latest": "22.04"},
		},
	}
	key := trackredirect.Key{ID: "ubuntu", Version: "22.04"}

	resolved, err := trackredirect.Resolve(key, "latest/stable", trackMap)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "22.04/stable")
}
