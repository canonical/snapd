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

const uc18CandidateInfo = `VERSION=2.99
SNAPD_LTS_TRACKS='{"18":{"latest":"18","fips-updates":"18-fips"}}'`

func (s *ltsSuite) snapdContainer(c *C, info string) snap.Container {
	snapdPath := snaptest.MakeTestSnapWithFiles(c, `name: snapd
type: snapd
version: 1.0`, [][]string{{"/usr/lib/snapd/info", info}})
	snapf, err := snapfile.Open(snapdPath)
	c.Assert(err, IsNil)
	return snapf
}

func (s *ltsSuite) TestSystemBootBaseAllowedClassic(c *C) {
	_, err := ltstrack.SystemBootBaseAllowed(s.classicModel(c))
	c.Assert(err, ErrorMatches, "policy does not allow classic system")
}

func (s *ltsSuite) TestSystemBootBaseAllowedHybridClassic(c *C) {
	_, err := ltstrack.SystemBootBaseAllowed(s.hybridClassicModel(c, "core22"))
	c.Assert(err, ErrorMatches, "policy does not allow hybrid classic system")
}

func (s *ltsSuite) TestSystemBootBaseAllowedUbuntuCoreDisabled(c *C) {
	restore := ltstrack.MockSystemAllowed(false, false, false)
	defer restore()

	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	_, err := ltstrack.SystemBootBaseAllowed(uc18)
	c.Assert(err, ErrorMatches, "policy does not allow ubuntu core system")
}

func (s *ltsSuite) TestSystemBootBaseAllowedUC18(c *C) {
	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	bootBase, err := ltstrack.SystemBootBaseAllowed(uc18)
	c.Assert(err, IsNil)
	c.Check(bootBase, Equals, 18)
}

func (s *ltsSuite) TestSystemBootBaseAllowedUC16HardError(c *C) {
	uc16 := s.coreModel(c, "core", "pc", "pc-kernel")
	_, err := ltstrack.SystemBootBaseAllowed(uc16)
	c.Assert(err, ErrorMatches, "cannot use unsupported Ubuntu Core 16 model")
}

func (s *ltsSuite) TestSystemBootBaseAllowedClassicInScope(c *C) {
	restore := ltstrack.MockSystemAllowed(true, true, false)
	defer restore()

	_, err := ltstrack.SystemBootBaseAllowed(s.classicModel(c))
	c.Assert(err, ErrorMatches, "classic boot base not currently supported")
}

func (s *ltsSuite) TestSystemBootBaseAllowedHybridClassicInScope(c *C) {
	restore := ltstrack.MockSystemAllowed(true, false, true)
	defer restore()

	_, err := ltstrack.SystemBootBaseAllowed(s.hybridClassicModel(c, "core22"))
	c.Assert(err, ErrorMatches, "classic boot base not currently supported")
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

func (s *ltsSuite) TestResolveUnmanagedBootBaseErrors(c *C) {
	// Boot base 22 is unmanaged (not in the production map yet).
	model := s.coreModel(c, "core22", "pc=22", "pc-kernel=22")

	for _, channel := range []string{"latest/stable", "22/stable", "stable"} {
		_, err := ltstrack.Resolve(model, channel, nil)
		c.Assert(err, ErrorMatches, `no LTS track map for boot base 22 from running snapd version .*`, Commentf("channel %q", channel))
		c.Check(errors.Is(err, ltstrack.ErrLTSBaseNotManaged), Equals, true, Commentf("channel %q", channel))
	}
}

func (s *ltsSuite) TestResolveMockEmptyMapErrors(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	_, err := ltstrack.Resolve(model, "latest/stable", nil)
	c.Assert(err, ErrorMatches, `no LTS track map for boot base 18 from running snapd version 2.75`)
	c.Check(errors.Is(err, ltstrack.ErrLTSBaseNotManaged), Equals, true)
}

func (s *ltsSuite) TestResolveBranchDropped(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	resolved, err := ltstrack.Resolve(model, "latest/stable/mybranch", nil)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")
}

func (s *ltsSuite) TestLTSTypedErrors(c *C) {
	for _, tc := range []struct {
		err      error
		msg      string
		sentinel error
		other    []error
	}{
		{
			err:      &ltstrack.LTSInternalError{Msg: "internal situation A"},
			msg:      "internal error: internal situation A",
			sentinel: ltstrack.ErrLTSInternal,
			other:    []error{ltstrack.ErrLTSNotAllowed, ltstrack.ErrLTSBaseNotManaged, ltstrack.ErrLTSNoTrack},
		},
		{
			err:      &ltstrack.LTSNotAllowedError{Msg: "not allowed situation B"},
			msg:      "not allowed situation B",
			sentinel: ltstrack.ErrLTSNotAllowed,
			other:    []error{ltstrack.ErrLTSInternal, ltstrack.ErrLTSBaseNotManaged, ltstrack.ErrLTSNoTrack},
		},
		{
			err:      &ltstrack.LTSBaseNotManagedError{Msg: "base not managed situation C"},
			msg:      "base not managed situation C",
			sentinel: ltstrack.ErrLTSBaseNotManaged,
			other:    []error{ltstrack.ErrLTSInternal, ltstrack.ErrLTSNotAllowed, ltstrack.ErrLTSNoTrack},
		},
		{
			err:      &ltstrack.LTSNoTrackError{Msg: "no track situation D"},
			msg:      "no track situation D",
			sentinel: ltstrack.ErrLTSNoTrack,
			other:    []error{ltstrack.ErrLTSInternal, ltstrack.ErrLTSNotAllowed, ltstrack.ErrLTSBaseNotManaged},
		},
	} {
		c.Check(tc.err.Error(), Equals, tc.msg, Commentf("%v", tc.sentinel))
		c.Check(errors.Is(tc.err, tc.sentinel), Equals, true, Commentf("%v", tc.sentinel))
		for _, other := range tc.other {
			c.Check(errors.Is(tc.err, other), Equals, false, Commentf("%v vs %v", tc.sentinel, other))
		}
		switch tc.sentinel {
		case ltstrack.ErrLTSInternal:
			var internal *ltstrack.LTSInternalError
			c.Assert(errors.As(tc.err, &internal), Equals, true)
			c.Check(internal.Msg, Equals, "internal situation A")
		case ltstrack.ErrLTSNotAllowed:
			var notAllowed *ltstrack.LTSNotAllowedError
			c.Assert(errors.As(tc.err, &notAllowed), Equals, true)
			c.Check(notAllowed.Msg, Equals, tc.msg)
		case ltstrack.ErrLTSBaseNotManaged:
			var notManaged *ltstrack.LTSBaseNotManagedError
			c.Assert(errors.As(tc.err, &notManaged), Equals, true)
			c.Check(notManaged.Msg, Equals, tc.msg)
		case ltstrack.ErrLTSNoTrack:
			var noTrack *ltstrack.LTSNoTrackError
			c.Assert(errors.As(tc.err, &noTrack), Equals, true)
			c.Check(noTrack.Msg, Equals, tc.msg)
		}
	}
}

func (s *ltsSuite) TestResolveErrors(c *C) {
	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	restore := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	_, err := ltstrack.Resolve(nil, "latest/stable", nil)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")
	c.Check(errors.Is(err, ltstrack.ErrLTSInternal), Equals, true)

	_, err = ltstrack.Resolve(uc18, "foo/bar/baz/quux", nil)
	c.Check(err, ErrorMatches, `internal error: cannot parse input channel: .*`)
	c.Check(errors.Is(err, ltstrack.ErrLTSInternal), Equals, true)

	// Unknown track on a managed boot base errors.
	_, err = ltstrack.Resolve(uc18, "20/stable", nil)
	c.Check(err, ErrorMatches, `no LTS track for boot base 18 for input track "20" from running snapd version 2.75`)
	c.Check(errors.Is(err, ltstrack.ErrLTSNoTrack), Equals, true)
	var noTrack *ltstrack.LTSNoTrackError
	c.Assert(errors.As(err, &noTrack), Equals, true)
	c.Check(noTrack.Msg, Equals, `no LTS track for boot base 18 for input track "20" from running snapd version 2.75`)
}

func (s *ltsSuite) TestResolveOutOfScopeNotAllowed(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	// Classic and hybrid classic models are not allowed by default.
	_, err := ltstrack.Resolve(s.classicModel(c), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "policy does not allow classic system")
	c.Check(errors.Is(err, ltstrack.ErrLTSNotAllowed), Equals, true)

	_, err = ltstrack.Resolve(s.hybridClassicModel(c, "core22"), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "policy does not allow hybrid classic system")
	c.Check(errors.Is(err, ltstrack.ErrLTSNotAllowed), Equals, true)
}

func (s *ltsSuite) TestResolveUC16Rejected(c *C) {
	uc16 := s.coreModel(c, "core", "pc", "pc-kernel")
	_, err := ltstrack.Resolve(uc16, "latest/stable", nil)
	c.Check(err, ErrorMatches, "cannot use unsupported Ubuntu Core 16 model")
	c.Check(errors.Is(err, ltstrack.ErrLTSNotAllowed), Equals, true)
}

func (s *ltsSuite) TestResolveScopeFlags(c *C) {
	restoreMap := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restoreMap()

	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	// Flip scope: Ubuntu Core off, hybrid classic on.
	restore := ltstrack.MockSystemAllowed(false, false, true)
	defer restore()

	// Ubuntu Core now not allowed.
	_, err := ltstrack.Resolve(uc18, "latest/stable", nil)
	c.Assert(err, ErrorMatches, "policy does not allow ubuntu core system")
	c.Check(errors.Is(err, ltstrack.ErrLTSNotAllowed), Equals, true)

	// Hybrid classic now allowed by flags but classic boot base is not supported yet.
	_, err = ltstrack.Resolve(s.hybridClassicModel(c, "core22"), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "classic boot base not currently supported")
	c.Check(errors.Is(err, ltstrack.ErrLTSNotAllowed), Equals, true)
}

func (s *ltsSuite) TestResolveHybridClassicInScopeNotAllowed(c *C) {
	restoreMap := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(22, "22"))
	defer restoreMap()
	restoreScope := ltstrack.MockSystemAllowed(true, false, true)
	defer restoreScope()

	_, err := ltstrack.Resolve(s.hybridClassicModel(c, "core22"), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "classic boot base not currently supported")
	c.Check(errors.Is(err, ltstrack.ErrLTSNotAllowed), Equals, true)
}

func (s *ltsSuite) TestResolveClassicInScopeNotAllowed(c *C) {
	restoreMap := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restoreMap()
	restoreScope := ltstrack.MockSystemAllowed(true, true, false)
	defer restoreScope()

	_, err := ltstrack.Resolve(s.classicModel(c), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "classic boot base not currently supported")
	c.Check(errors.Is(err, ltstrack.ErrLTSNotAllowed), Equals, true)
}

func (s *ltsSuite) TestResolveCandidateRemap(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidate := s.snapdContainer(c, uc18CandidateInfo)

	resolved, err := ltstrack.Resolve(model, "latest/stable", candidate)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")

	resolved, err = ltstrack.Resolve(model, "18/stable", candidate)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")

	resolved, err = ltstrack.Resolve(model, "fips-updates/candidate", candidate)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18-fips/candidate")

	resolved, err = ltstrack.Resolve(model, "18-fips/beta", candidate)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18-fips/beta")
}

func (s *ltsSuite) TestResolveCandidateUsesMapNotRunning(c *C) {
	// Running loader has no UC18 onboarded; nil candidate errors.
	restore := ltstrack.MockSnapdLTSTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	_, err := ltstrack.Resolve(model, "latest/stable", nil)
	c.Assert(err, ErrorMatches, `no LTS track map for boot base 18 from running snapd version 2.75`)
	c.Check(errors.Is(err, ltstrack.ErrLTSBaseNotManaged), Equals, true)

	candidate := s.snapdContainer(c, uc18CandidateInfo)
	resolved, err := ltstrack.Resolve(model, "latest/stable", candidate)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")
}

func (s *ltsSuite) TestResolveCandidateWithoutMapErrors(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidate := s.snapdContainer(c, "VERSION=2.99\n")

	_, err := ltstrack.Resolve(model, "latest/stable", candidate)
	c.Assert(err, ErrorMatches, `no LTS track map for boot base 18 from candidate snapd version 2.99`)
	c.Check(errors.Is(err, ltstrack.ErrLTSBaseNotManaged), Equals, true)
}

func (s *ltsSuite) TestResolveCandidateUnmanagedBootBaseErrors(c *C) {
	model := s.coreModel(c, "core22", "pc=22", "pc-kernel=22")
	candidate := s.snapdContainer(c, uc18CandidateInfo)

	_, err := ltstrack.Resolve(model, "latest/stable", candidate)
	c.Assert(err, ErrorMatches, `no LTS track map for boot base 22 from candidate snapd version 2.99`)
	c.Check(errors.Is(err, ltstrack.ErrLTSBaseNotManaged), Equals, true)
}

func (s *ltsSuite) TestResolveCandidateErrors(c *C) {
	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidate := s.snapdContainer(c, uc18CandidateInfo)

	_, err := ltstrack.Resolve(nil, "latest/stable", candidate)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")
	c.Check(errors.Is(err, ltstrack.ErrLTSInternal), Equals, true)

	_, err = ltstrack.Resolve(model, "20/stable", candidate)
	c.Check(err, ErrorMatches, `no LTS track for boot base 18 for input track "20" from candidate snapd version 2.99`)
	c.Check(errors.Is(err, ltstrack.ErrLTSNoTrack), Equals, true)
}

func (s *ltsSuite) TestResolveCandidateUsesCandidateMap(c *C) {
	restore := ltstrack.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	// Running map would remap latest to 18; candidate map takes precedence.
	candidate := s.snapdContainer(c, `VERSION=2.70
SNAPD_LTS_TRACKS='{"18":{"latest":"20"}}'`)

	resolved, err := ltstrack.Resolve(model, "latest/stable", candidate)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "20/stable")
}
