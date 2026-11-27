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
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/logger"
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

// tracks18Chain adds an onward pair. 18 is an input, so it is not only a target.
var tracks18Chain = snap.UbuntuCoreTracks{
	"18": {"latest": "18", "fips-updates": "18-fips", "18": "24"},
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
		for _, requested := range []string{"", "18/edge"} {
			resolved, err := uctrack.Resolve(t.model, "latest/stable", requested, tracks18Latest)
			c.Check(err, ErrorMatches, t.err, Commentf("%s requested %q", t.label, requested))
			c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true, Commentf("%s requested %q", t.label, requested))
			c.Check(resolved, Equals, "", Commentf("%s requested %q", t.label, requested))
		}
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
		for _, requested := range []string{"", "18/edge"} {
			resolved, err := uctrack.Resolve(model, channel, requested, tracks18Latest)
			c.Assert(err, ErrorMatches, `cannot use Ubuntu Core tracks: no track map for boot base 22`, Commentf("channel %q requested %q", channel, requested))
			c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true, Commentf("channel %q requested %q", channel, requested))
			c.Check(resolved, Equals, "", Commentf("channel %q requested %q", channel, requested))
		}
	}
}

func (s *ucSuite) TestResolveEmptyOrNilTrackMapNotApplicable(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	for _, tracks := range []snap.UbuntuCoreTracks{{}, nil} {
		resolved, err := uctrack.Resolve(model, "latest/stable", "18/stable", tracks)
		c.Assert(err, ErrorMatches, `cannot use Ubuntu Core tracks: no track map for boot base 18`)
		c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true)
		c.Check(resolved, Equals, "")
	}
}

func (s *ucSuite) TestResolveUnknownTrackingTrackNotApplicable(c *C) {
	// The tracking track is neither an input nor a target, so the request is
	// not considered. Resolve returns no channel.
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	for _, t := range []struct {
		tracking  string
		requested string
		track     string
	}{
		{"preview/stable", "", "preview"},
		{"preview/stable", "18/edge", "preview"},
		{"preview/stable", "latest/edge", "preview"},
		{"20/stable", "18/edge", "20"},
		{"24/edge", "latest/edge", "24"},
	} {
		resolved, err := uctrack.Resolve(model, t.tracking, t.requested, tracks18)
		c.Assert(err, ErrorMatches, `cannot use Ubuntu Core tracks: no track `+t.track+` for boot base 18`, Commentf("tracking %q requested %q", t.tracking, t.requested))
		c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true, Commentf("tracking %q requested %q", t.tracking, t.requested))
		c.Check(resolved, Equals, "", Commentf("tracking %q requested %q", t.tracking, t.requested))
	}
}

func (s *ucSuite) TestResolveBootBaseKeyWithNoPairsNotApplicable(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")
	tracks := snap.UbuntuCoreTracks{"18": {}}

	resolved, err := uctrack.Resolve(model, "latest/stable", "", tracks)
	c.Assert(err, ErrorMatches, `cannot use Ubuntu Core tracks: no track latest for boot base 18`)
	c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, true)
	c.Check(resolved, Equals, "")
}

func (s *ucSuite) TestResolveEmptyRequestedChannelRemapsTracking(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	for _, t := range []struct {
		tracking string
		want     string
	}{
		// An omitted track means latest. The risk is kept. A transition drops the branch.
		{"latest/stable", "18/stable"},
		{"latest/candidate", "18/candidate"},
		{"latest/beta", "18/beta"},
		{"stable", "18/stable"},
		{"candidate", "18/candidate"},
		{"beta", "18/beta"},
		{"latest", "18/stable"},
		{"edge", "18/edge"},
		// A transition drops the branch.
		{"latest/stable/mybranch", "18/stable"},
		{"latest/beta/hotfix", "18/beta"},
		{"fips-updates/stable", "18-fips/stable"},
		{"fips-updates/candidate", "18-fips/candidate"},
	} {
		resolved, err := uctrack.Resolve(model, t.tracking, "", tracks18)
		c.Assert(err, IsNil, Commentf("tracking %q", t.tracking))
		c.Check(resolved, Equals, t.want, Commentf("tracking %q", t.tracking))
	}
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

	// A risk-only request has an omitted track, which means latest. latest is
	// only a target here, so the requested risk and branch are kept.
	resolved, err = uctrack.Resolve(model, "18/stable", "edge", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "edge")

	resolved, err = uctrack.Resolve(model, "18/stable", "latest/beta/hotfix", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "beta/hotfix")

	// 18 is an input, so the request follows 18 to latest and drops the branch.
	resolved, err = uctrack.Resolve(model, "18/stable", "18/stable", tracks)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "stable")
}

func (s *ucSuite) TestResolveNilModel(c *C) {
	_, err := uctrack.Resolve(nil, "latest/stable", "", tracks18Latest)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")
	c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, false)
}

func (s *ucSuite) TestResolveInvalidTrackingChannel(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	_, err := uctrack.Resolve(model, "foo/bar/baz/quux", "", tracks18Latest)
	c.Check(err, ErrorMatches, `internal error: cannot parse tracking channel: channel name has too many components: foo/bar/baz/quux`)
	c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, false)
}

func (s *ucSuite) TestResolveInvalidRequestedChannel(c *C) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")

	resolved, err := uctrack.Resolve(model, "latest/stable", "foo/bar/baz/quux", tracks18Latest)
	c.Check(err, ErrorMatches, `internal error: cannot parse requested channel: channel name has too many components: foo/bar/baz/quux`)
	c.Check(errors.Is(err, uctrack.ErrNotApplicable), Equals, false)
	c.Check(resolved, Equals, "")
}

// logNone means the resolution is not logged. logIgnored means the requested
// track is neither an input nor a target. logRewritten means the tracking
// track is already a target and the requested track is an input.
const (
	logNone logKind = iota
	logIgnored
	logRewritten
)

type logKind int

type resolveCase struct {
	id        string
	tracking  string
	requested string
	want      string
	log       logKind
}

func (s *ucSuite) checkResolveCases(c *C, tracks snap.UbuntuCoreTracks, cases []resolveCase) {
	model := s.coreModel("core18", "pc=18", "pc-kernel=18")
	logbuf, restore := logger.MockLogger()
	defer restore()

	for _, t := range cases {
		logbuf.Reset()
		resolved, err := uctrack.Resolve(model, t.tracking, t.requested, tracks)
		comment := Commentf("%s tracking %q requested %q", t.id, t.tracking, t.requested)
		c.Assert(err, IsNil, comment)
		c.Check(resolved, Equals, t.want, comment)
		switch t.log {
		case logNone:
			c.Check(logbuf.String(), Equals, "", comment)
		case logIgnored:
			c.Check(logbuf.String(), Matches, fmt.Sprintf(`(?s).*ignoring requested channel "%s", using "%s".*`, t.requested, t.want), comment)
		case logRewritten:
			c.Check(logbuf.String(), Matches, fmt.Sprintf(`(?s).*rewrote requested channel "%s" to "%s".*`, t.requested, t.want), comment)
		}
	}
}

func (s *ucSuite) TestResolveSimpleMap(c *C) {
	s.checkResolveCases(c, tracks18, []resolveCase{
		{"S1", "latest/stable", "", "18/stable", logNone},
		{"S2", "latest/stable", "latest", "18/stable", logNone},
		{"S3", "latest/stable", "stable", "18/stable", logNone},
		{"S4", "latest/stable", "latest/stable", "18/stable", logNone},
		{"S5", "latest/beta/hotfix", "", "18/beta", logNone},
		{"S6", "latest/stable", "latest/edge", "18/edge", logNone},
		{"S7", "latest/stable", "edge", "18/edge", logNone},
		{"S8", "latest/stable", "fips-updates/beta", "18-fips/beta", logNone},
		{"S9", "latest/beta", "fips-updates/stable", "18-fips/stable", logNone},
		{"S10", "latest/stable", "latest/beta/hotfix", "18/beta", logNone},
		{"S11", "latest/stable", "fips-updates/beta/hotfix", "18-fips/beta", logNone},
		{"S12", "latest/stable", "18/edge", "18/edge", logNone},
		{"S13", "latest/stable", "18/edge/hotfix", "18/edge/hotfix", logNone},
		{"S14", "latest/beta", "18/stable", "18/stable", logNone},
		{"S15", "latest/stable", "18", "18/stable", logNone},
		{"S16", "latest/stable", "18-fips/stable", "18-fips/stable", logNone},
		{"S17", "latest/beta", "preview/edge", "18/beta", logIgnored},
		{"S18", "latest/beta/hotfix", "preview/edge", "18/beta", logIgnored},
		{"S19", "latest/stable", "24/stable", "18/stable", logIgnored},
		{"S20", "fips-updates/candidate", "", "18-fips/candidate", logNone},
		{"S21", "fips-updates/candidate", "latest/edge", "18/edge", logNone},
		{"S22", "18/stable", "", "18/stable", logNone},
		{"S23", "18/edge/hotfix", "", "18/edge/hotfix", logNone},
		{"S24", "18/stable", "18/edge", "18/edge", logNone},
		{"S25", "18/stable", "18/beta/hotfix", "18/beta/hotfix", logNone},
		{"S26", "18/edge/hotfix", "18/stable", "18/stable", logNone},
		{"S27", "18/stable", "18-fips/beta", "18-fips/beta", logNone},
		{"S28", "18/stable", "latest/edge", "18/edge", logRewritten},
		{"S29", "18/stable", "edge", "18/edge", logRewritten},
		{"S30", "18/stable", "latest/beta/hotfix", "18/beta", logRewritten},
		{"S31", "18/edge/hotfix", "latest/stable", "18/stable", logRewritten},
		{"S32", "18/stable", "fips-updates/beta", "18-fips/beta", logRewritten},
		{"S33", "18/stable", "preview/edge", "18/stable", logIgnored},
		{"S34", "18/edge/hotfix", "preview/stable", "18/edge/hotfix", logIgnored},
		{"S35", "18/stable", "24/stable", "18/stable", logIgnored},
		{"S36", "18-fips/stable", "", "18-fips/stable", logNone},
		{"S37", "18-fips/beta", "18/edge", "18/edge", logNone},
	})
}

func (s *ucSuite) TestResolveChainMap(c *C) {
	s.checkResolveCases(c, tracks18Chain, []resolveCase{
		{"C1", "latest/stable", "", "18/stable", logNone},
		{"C2", "latest/beta/hotfix", "", "18/beta", logNone},
		{"C3", "latest/stable", "18/edge", "24/edge", logNone},
		{"C4", "latest/stable", "18/edge/hotfix", "24/edge", logNone},
		{"C5", "latest/stable", "24/edge", "24/edge", logNone},
		{"C6", "latest/stable", "24/edge/hotfix", "24/edge/hotfix", logNone},
		{"C7", "latest/stable", "latest/edge", "18/edge", logNone},
		{"C8", "18/stable", "", "24/stable", logNone},
		{"C9", "18/beta/hotfix", "", "24/beta", logNone},
		{"C10", "18/stable", "18/edge", "24/edge", logNone},
		{"C11", "18/stable", "24/edge", "24/edge", logNone},
		{"C12", "18/stable", "latest/edge", "18/edge", logNone},
		{"C13", "18/beta", "fips-updates/beta", "18-fips/beta", logNone},
		{"C14", "18/stable", "preview/edge", "24/stable", logIgnored},
		{"C15", "24/stable", "", "24/stable", logNone},
		{"C16", "24/edge/hotfix", "", "24/edge/hotfix", logNone},
		{"C17", "24/stable", "18/stable", "24/stable", logRewritten},
		{"C18", "24/stable", "18/beta/hotfix", "24/beta", logRewritten},
		{"C19", "24/stable", "latest/edge", "18/edge", logRewritten},
		{"C20", "24/stable", "fips-updates/beta", "18-fips/beta", logRewritten},
		{"C21", "24/stable", "24/edge", "24/edge", logNone},
		{"C22", "24/stable", "18-fips/stable", "18-fips/stable", logNone},
		{"C23", "24/stable", "preview/edge", "24/stable", logIgnored},
		{"C24", "24/beta/hotfix", "latest/stable", "18/stable", logRewritten},
	})
}
