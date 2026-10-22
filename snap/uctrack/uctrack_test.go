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

type modelCase struct {
	label string
	model *asserts.Model
	err   string
}

func (s *ucSuite) outOfScopeModels() []modelCase {
	return []modelCase{
		{"classic", s.classicModel(), "cannot use Ubuntu Core tracks on a classic system"},
		{"hybrid classic", s.hybridClassicModel("core22"), "cannot use Ubuntu Core tracks on a hybrid classic system"},
		{"bare base", s.coreModel("bare", "pc", "pc-kernel"), `cannot use Ubuntu Core tracks: "bare" is not a core boot base`},
		{"non-core base", s.coreModel("alt-base", "pc", "pc-kernel"), `cannot use Ubuntu Core tracks: "alt-base" is not a core boot base`},
		// UC16 uses the core snap as both base and snapd.
		{"uc16 with no base", s.coreModel("", "pc", "pc-kernel"), "cannot use Ubuntu Core tracks: unsupported Ubuntu Core 16 model"},
		{"uc16 core snap", s.coreModel("core", "pc", "pc-kernel"), "cannot use Ubuntu Core tracks: unsupported Ubuntu Core 16 model"},
		{"uc16", s.coreModel("core16", "pc", "pc-kernel"), "cannot use Ubuntu Core tracks: unsupported Ubuntu Core 16 model"},
	}
}

func (s *ucSuite) TestSystemBootBaseApplicableReturnsVersion(c *C) {
	bootBase, err := uctrack.SystemBootBaseApplicable(s.coreModel("core18", "pc=18", "pc-kernel=18"))
	c.Assert(err, IsNil)
	c.Check(bootBase, Equals, 18)
}

func (s *ucSuite) TestSystemBootBaseApplicableNilModel(c *C) {
	_, err := uctrack.SystemBootBaseApplicable(nil)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")
}

func (s *ucSuite) TestSystemBootBaseApplicableNotApplicable(c *C) {
	for _, t := range s.outOfScopeModels() {
		_, err := uctrack.SystemBootBaseApplicable(t.model)
		c.Check(err, ErrorMatches, t.err, Commentf("%s", t.label))
		c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true, Commentf("%s", t.label))
	}
}

func (s *ucSuite) TestResolveBootBaseNotApplicable(c *C) {
	for _, t := range s.outOfScopeModels() {
		resolved, err := uctrack.Resolve(t.model, "latest/stable", "", tracks18Latest)
		c.Check(err, ErrorMatches, t.err, Commentf("%s", t.label))
		c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true, Commentf("%s", t.label))
		c.Check(resolved, Equals, "", Commentf("%s", t.label))
	}
}

func (s *ucSuite) TestResolveEmptyTrackingChannelNotApplicable(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	// The requested channel is not consulted when nothing is being tracked,
	// including a channel that would not parse.
	for _, requested := range []string{"", "stable", "latest/stable", "fips-updates/stable", "20/stable", "other/stable", "foo/bar/baz/quux"} {
		resolved, err := uctrack.Resolve(model, "", requested, tracks18)
		c.Assert(err, ErrorMatches, "cannot use Ubuntu Core tracks: empty tracking channel", Commentf("requested %q", requested))
		c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true, Commentf("requested %q", requested))
		c.Check(resolved, Equals, "", Commentf("requested %q", requested))
	}
}

func (s *ucSuite) TestResolveBootBaseWithoutTrackMapNotApplicable(c *C) {
	// Boot base 22 has no entry. The map only onboards 18.
	model := s.coreModel("core22", "pc=22", "pc-kernel=22")

	for _, channel := range []string{"latest/stable", "22/stable", "stable"} {
		resolved, err := uctrack.Resolve(model, channel, "", tracks18Latest)
		c.Assert(err, ErrorMatches, `cannot use Ubuntu Core tracks: no track map for boot base 22`, Commentf("channel %q", channel))
		c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true, Commentf("channel %q", channel))
		c.Check(resolved, Equals, "", Commentf("channel %q", channel))
	}
}

func (s *ucSuite) TestResolveEmptyOrNilTrackMapNotApplicable(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	for _, tracks := range []snap.UbuntuCoreTracks{{}, nil} {
		resolved, err := uctrack.Resolve(model, "latest/stable", "", tracks)
		c.Assert(err, ErrorMatches, `cannot use Ubuntu Core tracks: no track map for boot base 18`)
		c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true)
		c.Check(resolved, Equals, "")
	}
}

func (s *ucSuite) TestResolveUnknownTrackingTrackNotApplicable(c *C) {
	// Boot base 18 is covered, but track 20 is neither a key nor a target.
	// The requested channel is not considered once the tracking lookup fails.
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	for _, requested := range []string{"", "latest/stable"} {
		resolved, err := uctrack.Resolve(model, "20/stable", requested, tracks18Latest)
		c.Assert(err, ErrorMatches, `cannot use Ubuntu Core tracks: no track 20 for boot base 18`, Commentf("requested %q", requested))
		c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true, Commentf("requested %q", requested))
		c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, false, Commentf("requested %q", requested))
		c.Check(resolved, Equals, "", Commentf("requested %q", requested))
	}
}

func (s *ucSuite) TestResolveEmptyRequestedChannelRemapsTracking(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	for _, t := range []struct {
		tracking string
		want     string
	}{
		// An omitted track means latest. The risk and branch are kept.
		{"latest/stable", "18/stable"},
		{"latest/candidate", "18/candidate"},
		{"latest/beta", "18/beta"},
		{"stable", "18/stable"},
		{"candidate", "18/candidate"},
		{"beta", "18/beta"},
		{"latest", "18/stable"},
		{"edge", "18/edge"},
		{"latest/stable/mybranch", "18/stable/mybranch"},
		{"fips-updates/stable", "18-fips/stable"},
		{"fips-updates/candidate", "18-fips/candidate"},
	} {
		resolved, err := uctrack.Resolve(model, t.tracking, "", tracks18)
		c.Assert(err, IsNil, Commentf("tracking %q", t.tracking))
		c.Check(resolved, Equals, t.want, Commentf("tracking %q", t.tracking))
	}
}

func (s *ucSuite) TestResolveRequestedChannelOnResolvedTrack(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	for _, t := range []struct {
		tracking  string
		requested string
		want      string
	}{
		// The requested risk and branch are kept. The track is the resolved
		// tracking track, not a second lookup.
		{"latest/stable", "18/stable", "18/stable"},
		{"latest/stable", "18/edge", "18/edge"},
		{"latest/stable", "18/edge/hotfix", "18/edge/hotfix"},
		{"18/stable", "18/edge", "18/edge"},
		{"18/stable", "18/stable", "18/stable"},
		{"18/candidate", "18/candidate", "18/candidate"},
		{"18-fips/stable", "18-fips/stable", "18-fips/stable"},
		{"18-fips/beta", "18-fips/beta", "18-fips/beta"},
		{"fips-updates/stable", "18-fips/edge", "18-fips/edge"},
	} {
		resolved, err := uctrack.Resolve(model, t.tracking, t.requested, tracks18)
		c.Assert(err, IsNil, Commentf("tracking %q requested %q", t.tracking, t.requested))
		c.Check(resolved, Equals, t.want, Commentf("tracking %q requested %q", t.tracking, t.requested))
	}
}

func (s *ucSuite) TestResolveRequestedTrackIsNotLookedUpAgain(c *C) {
	// latest resolves to 18, and an explicit key would send 18 on to 24.
	// A requested track of 18 already matches the resolved track, so it is
	// copied and not sent through the map again.
	tracks := snap.UbuntuCoreTracks{
		"18": {"latest": "18", "18": "24"},
	}
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	resolved, err := uctrack.Resolve(model, "latest/stable", "18/edge", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/edge")

	// The secondary hop is not the target of latest, and repeating latest is
	// not rewritten onto 18.
	_, err = uctrack.Resolve(model, "latest/stable", "24/edge", tracks)
	c.Assert(err, ErrorMatches, `cannot use requested track "24": resolved track is "18"`)
	c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, true)

	_, err = uctrack.Resolve(model, "latest/stable", "latest/stable", tracks)
	c.Assert(err, ErrorMatches, `cannot use requested track "latest": resolved track is "18"`)
	c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, true)
}

func (s *ucSuite) TestResolveExplicitMapKeyWinsOverTargetIdentity(c *C) {
	// A later onboard can remap a track onward with an explicit key.
	// An input that is only a target, such as 24, stays where it is.
	tracks := snap.UbuntuCoreTracks{
		"18": {"latest": "24", "18": "24"},
	}
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	resolved, err := uctrack.Resolve(model, "latest/stable", "", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/stable")

	resolved, err = uctrack.Resolve(model, "18/stable", "", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/stable")

	resolved, err = uctrack.Resolve(model, "24/edge", "", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/edge")

	resolved, err = uctrack.Resolve(model, "18/stable", "24/edge", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "24/edge")

	// Repeating the current channel names the old track, so it is not rewritten.
	_, err = uctrack.Resolve(model, "latest/stable", "latest/stable", tracks)
	c.Assert(err, ErrorMatches, `cannot use requested track "latest": resolved track is "24"`)
	c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, true)

	_, err = uctrack.Resolve(model, "18/stable", "18/stable", tracks)
	c.Assert(err, ErrorMatches, `cannot use requested track "18": resolved track is "24"`)
	c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, true)

	_, err = uctrack.Resolve(model, "18/stable", "18/edge", tracks)
	c.Assert(err, ErrorMatches, `cannot use requested track "18": resolved track is "24"`)
	c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, true)
}

func (s *ucSuite) TestResolveLatestTargetRendersImplicitTrack(c *C) {
	// "latest" is a valid target, but it is the default track and so is
	// rendered implicitly. The store reads "stable" as latest/stable.
	tracks := snap.UbuntuCoreTracks{
		"18": {"18": "latest"},
	}
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	resolved, err := uctrack.Resolve(model, "18/stable", "", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "stable")

	// A risk-only requested channel has an omitted track, which means latest.
	resolved, err = uctrack.Resolve(model, "18/stable", "edge", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "edge")

	resolved, err = uctrack.Resolve(model, "18/stable", "latest/beta/hotfix", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "beta/hotfix")

	// The request names 18, and the target is latest, so it is not rewritten
	// to stable.
	_, err = uctrack.Resolve(model, "18/stable", "18/stable", tracks)
	c.Assert(err, ErrorMatches, `cannot use requested track "18": resolved track is "latest"`)
	c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, true)
}

func (s *ucSuite) TestResolveNilModel(c *C) {
	_, err := uctrack.Resolve(nil, "latest/stable", "", tracks18Latest)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")
}

func (s *ucSuite) TestResolveInvalidTrackingChannel(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	_, err := uctrack.Resolve(model, "foo/bar/baz/quux", "", tracks18Latest)
	c.Check(err, ErrorMatches, `internal error: cannot parse tracking channel: channel name has too many components: foo/bar/baz/quux`)
}

func (s *ucSuite) TestResolveInvalidRequestedChannel(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	_, err := uctrack.Resolve(model, "latest/stable", "foo/bar/baz/quux", tracks18Latest)
	c.Check(err, ErrorMatches, `internal error: cannot parse requested channel: channel name has too many components: foo/bar/baz/quux`)
	c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, false)
	c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, false)
}

func (s *ucSuite) TestResolveRequestedTrackDiffersFromResolved(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	for _, t := range []struct {
		tracking       string
		requested      string
		requestedTrack string
		resolvedTrack  string
	}{
		// Repeating the tracking channel is a request for that track. It is not
		// rewritten onto the target. An omitted track means latest.
		{"latest/stable", "latest/stable", "latest", "18"},
		{"latest/stable", "stable", "latest", "18"},
		{"latest", "latest/stable", "latest", "18"},
		{"latest/stable/mybranch", "stable/mybranch", "latest", "18"},
		{"fips-updates/stable", "fips-updates/stable", "fips-updates", "18-fips"},
		// A different risk or branch on the old track is also a switch.
		{"latest/stable", "latest/edge", "latest", "18"},
		{"latest/stable", "stable/mybranch", "latest", "18"},
		{"latest/stable", "fips-updates/stable", "fips-updates", "18"},
		{"latest/stable", "other/stable", "other", "18"},
		{"latest/stable", "20/stable", "20", "18"},
		{"fips-updates/stable", "18/stable", "18", "18-fips"},
	} {
		resolved, err := uctrack.Resolve(model, t.tracking, t.requested, tracks18)
		c.Assert(err, ErrorMatches, `cannot use requested track "`+t.requestedTrack+`": resolved track is "`+t.resolvedTrack+`"`, Commentf("tracking %q requested %q", t.tracking, t.requested))
		c.Check(errors.Is(err, uctrack.ErrRequestedChannelProhibited), Equals, true, Commentf("tracking %q requested %q", t.tracking, t.requested))
		c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, false, Commentf("tracking %q requested %q", t.tracking, t.requested))
		c.Check(resolved, Equals, "", Commentf("tracking %q requested %q", t.tracking, t.requested))
	}
}
