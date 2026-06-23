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
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/ltschannel"
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
		rules[track] = track
		if strings.HasSuffix(track, "-fips") {
			rules["fips-updates"] = track
		}
	}
	return map[int]map[string]string{bootBase: rules}
}

const uc18CandidateInfo = `VERSION=2.99
SNAPD_LTS_TRACKS='{"18":{"latest":"18","18":"18","fips-updates":"18-fips","18-fips":"18-fips"}}'`

func (s *ltsSuite) snapdContainer(c *C, info string) snap.Container {
	snapdPath := snaptest.MakeTestSnapWithFiles(c, `name: snapd
type: snapd
version: 1.0`, [][]string{{"/usr/lib/snapd/info", info}})
	snapf, err := snapfile.Open(snapdPath)
	c.Assert(err, IsNil)
	return snapf
}

func (s *ltsSuite) TestSystemBootBaseAllowedClassic(c *C) {
	_, err := ltschannel.SystemBootBaseAllowed(s.classicModel(c))
	c.Assert(err, ErrorMatches, "policy does not allow classic system")
}

func (s *ltsSuite) TestSystemBootBaseAllowedHybridClassic(c *C) {
	_, err := ltschannel.SystemBootBaseAllowed(s.hybridClassicModel(c, "core22"))
	c.Assert(err, ErrorMatches, "policy does not allow hybrid classic system")
}

func (s *ltsSuite) TestSystemBootBaseAllowedUbuntuCoreDisabled(c *C) {
	restore := ltschannel.MockSystemAllowed(false, false, false)
	defer restore()

	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	_, err := ltschannel.SystemBootBaseAllowed(uc18)
	c.Assert(err, ErrorMatches, "policy does not allow ubuntu core system")
}

func (s *ltsSuite) TestSystemBootBaseAllowedUC18(c *C) {
	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	bootBase, err := ltschannel.SystemBootBaseAllowed(uc18)
	c.Assert(err, IsNil)
	c.Check(bootBase, Equals, 18)
}

func (s *ltsSuite) TestSystemBootBaseAllowedUC16HardError(c *C) {
	uc16 := s.coreModel(c, "core", "pc", "pc-kernel")
	_, err := ltschannel.SystemBootBaseAllowed(uc16)
	c.Assert(err, ErrorMatches, "cannot use unsupported Ubuntu Core 16 model")
}

func (s *ltsSuite) TestSystemBootBaseAllowedClassicInScope(c *C) {
	restore := ltschannel.MockSystemAllowed(true, true, false)
	defer restore()

	_, err := ltschannel.SystemBootBaseAllowed(s.classicModel(c))
	c.Assert(err, ErrorMatches, "classic boot base not currently supported")
}

func (s *ltsSuite) TestSystemBootBaseAllowedHybridClassicInScope(c *C) {
	restore := ltschannel.MockSystemAllowed(true, false, true)
	defer restore()

	_, err := ltschannel.SystemBootBaseAllowed(s.hybridClassicModel(c, "core22"))
	c.Assert(err, ErrorMatches, "classic boot base not currently supported")
}

func (s *ltsSuite) TestSnapdLTSChannelUC18Remap(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(ltsTrackMap(18, "18", "18-fips"))
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
		resolved, err := ltschannel.SnapdLTSChannel(model, t.channel, nil)
		c.Assert(err, IsNil, Commentf("channel %q", t.channel))
		c.Check(resolved, Equals, t.want, Commentf("channel %q", t.channel))
	}
}

func (s *ltsSuite) TestSnapdLTSChannelUC18Identity(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(ltsTrackMap(18, "18", "18-fips"))
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	for _, channel := range []string{
		"18/stable",
		"18/candidate",
		"18-fips/stable",
		"18-fips/beta",
	} {
		resolved, err := ltschannel.SnapdLTSChannel(model, channel, nil)
		c.Assert(err, IsNil, Commentf("channel %q", channel))
		c.Check(resolved, Equals, channel, Commentf("channel %q", channel))
	}
}

func (s *ltsSuite) TestSnapdLTSChannelUnmanagedBootBaseErrors(c *C) {
	// Boot base 22 is unmanaged (not in the production map yet).
	model := s.coreModel(c, "core22", "pc=22", "pc-kernel=22")

	for _, channel := range []string{"latest/stable", "22/stable", "stable"} {
		_, err := ltschannel.SnapdLTSChannel(model, channel, nil)
		c.Assert(err, ErrorMatches, `no LTS track map for boot base 22 from running snapd version .*`, Commentf("channel %q", channel))
		c.Check(errors.Is(err, ltschannel.ErrLTSBaseNotManaged), Equals, true, Commentf("channel %q", channel))
	}
}

func (s *ltsSuite) TestSnapdLTSChannelMockEmptyMapErrors(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	_, err := ltschannel.SnapdLTSChannel(model, "latest/stable", nil)
	c.Assert(err, ErrorMatches, `no LTS track map for boot base 18 from running snapd version 2.75`)
	c.Check(errors.Is(err, ltschannel.ErrLTSBaseNotManaged), Equals, true)
}

func (s *ltsSuite) TestSnapdLTSChannelBranchDropped(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	resolved, err := ltschannel.SnapdLTSChannel(model, "latest/stable/mybranch", nil)
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
			err:      &ltschannel.LTSInternalError{Msg: "internal situation A"},
			msg:      "internal error: internal situation A",
			sentinel: ltschannel.ErrLTSInternal,
			other:    []error{ltschannel.ErrLTSNotAllowed, ltschannel.ErrLTSBaseNotManaged, ltschannel.ErrLTSNoTrack},
		},
		{
			err:      &ltschannel.LTSNotAllowedError{Msg: "not allowed situation B"},
			msg:      "not allowed situation B",
			sentinel: ltschannel.ErrLTSNotAllowed,
			other:    []error{ltschannel.ErrLTSInternal, ltschannel.ErrLTSBaseNotManaged, ltschannel.ErrLTSNoTrack},
		},
		{
			err:      &ltschannel.LTSBaseNotManagedError{Msg: "base not managed situation C"},
			msg:      "base not managed situation C",
			sentinel: ltschannel.ErrLTSBaseNotManaged,
			other:    []error{ltschannel.ErrLTSInternal, ltschannel.ErrLTSNotAllowed, ltschannel.ErrLTSNoTrack},
		},
		{
			err:      &ltschannel.LTSNoTrackError{Msg: "no track situation D"},
			msg:      "no track situation D",
			sentinel: ltschannel.ErrLTSNoTrack,
			other:    []error{ltschannel.ErrLTSInternal, ltschannel.ErrLTSNotAllowed, ltschannel.ErrLTSBaseNotManaged},
		},
	} {
		c.Check(tc.err.Error(), Equals, tc.msg, Commentf("%v", tc.sentinel))
		c.Check(errors.Is(tc.err, tc.sentinel), Equals, true, Commentf("%v", tc.sentinel))
		for _, other := range tc.other {
			c.Check(errors.Is(tc.err, other), Equals, false, Commentf("%v vs %v", tc.sentinel, other))
		}
		switch tc.sentinel {
		case ltschannel.ErrLTSInternal:
			var internal *ltschannel.LTSInternalError
			c.Assert(errors.As(tc.err, &internal), Equals, true)
			c.Check(internal.Msg, Equals, "internal situation A")
		case ltschannel.ErrLTSNotAllowed:
			var notAllowed *ltschannel.LTSNotAllowedError
			c.Assert(errors.As(tc.err, &notAllowed), Equals, true)
			c.Check(notAllowed.Msg, Equals, tc.msg)
		case ltschannel.ErrLTSBaseNotManaged:
			var notManaged *ltschannel.LTSBaseNotManagedError
			c.Assert(errors.As(tc.err, &notManaged), Equals, true)
			c.Check(notManaged.Msg, Equals, tc.msg)
		case ltschannel.ErrLTSNoTrack:
			var noTrack *ltschannel.LTSNoTrackError
			c.Assert(errors.As(tc.err, &noTrack), Equals, true)
			c.Check(noTrack.Msg, Equals, tc.msg)
		}
	}
}

func (s *ltsSuite) TestSnapdLTSChannelErrors(c *C) {
	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	restore := ltschannel.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	_, err := ltschannel.SnapdLTSChannel(nil, "latest/stable", nil)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")
	c.Check(errors.Is(err, ltschannel.ErrLTSInternal), Equals, true)

	_, err = ltschannel.SnapdLTSChannel(uc18, "foo/bar/baz/quux", nil)
	c.Check(err, ErrorMatches, `internal error: cannot parse input channel: .*`)
	c.Check(errors.Is(err, ltschannel.ErrLTSInternal), Equals, true)

	// Unknown track on a managed boot base errors.
	_, err = ltschannel.SnapdLTSChannel(uc18, "20/stable", nil)
	c.Check(err, ErrorMatches, `no LTS track for boot base 18 for input track "20" from running snapd version 2.75`)
	c.Check(errors.Is(err, ltschannel.ErrLTSNoTrack), Equals, true)
	var noTrack *ltschannel.LTSNoTrackError
	c.Assert(errors.As(err, &noTrack), Equals, true)
	c.Check(noTrack.Msg, Equals, `no LTS track for boot base 18 for input track "20" from running snapd version 2.75`)
}

func (s *ltsSuite) TestSnapdLTSChannelOutOfScopeNotAllowed(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	// Classic and hybrid classic models are not allowed by default.
	_, err := ltschannel.SnapdLTSChannel(s.classicModel(c), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "policy does not allow classic system")
	c.Check(errors.Is(err, ltschannel.ErrLTSNotAllowed), Equals, true)

	_, err = ltschannel.SnapdLTSChannel(s.hybridClassicModel(c, "core22"), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "policy does not allow hybrid classic system")
	c.Check(errors.Is(err, ltschannel.ErrLTSNotAllowed), Equals, true)
}

func (s *ltsSuite) TestSnapdLTSChannelUC16Rejected(c *C) {
	uc16 := s.coreModel(c, "core", "pc", "pc-kernel")
	_, err := ltschannel.SnapdLTSChannel(uc16, "latest/stable", nil)
	c.Check(err, ErrorMatches, "cannot use unsupported Ubuntu Core 16 model")
	c.Check(errors.Is(err, ltschannel.ErrLTSNotAllowed), Equals, true)
}

func (s *ltsSuite) TestSnapdLTSChannelScopeFlags(c *C) {
	restoreMap := ltschannel.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restoreMap()

	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	// Flip scope: Ubuntu Core off, hybrid classic on.
	restore := ltschannel.MockSystemAllowed(false, false, true)
	defer restore()

	// Ubuntu Core now not allowed.
	_, err := ltschannel.SnapdLTSChannel(uc18, "latest/stable", nil)
	c.Assert(err, ErrorMatches, "policy does not allow ubuntu core system")
	c.Check(errors.Is(err, ltschannel.ErrLTSNotAllowed), Equals, true)

	// Hybrid classic now allowed by flags but classic boot base is not supported yet.
	_, err = ltschannel.SnapdLTSChannel(s.hybridClassicModel(c, "core22"), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "classic boot base not currently supported")
	c.Check(errors.Is(err, ltschannel.ErrLTSNotAllowed), Equals, true)
}

func (s *ltsSuite) TestSnapdLTSChannelHybridClassicInScopeNotAllowed(c *C) {
	restoreMap := ltschannel.MockSnapdLTSTrackMap(ltsTrackMap(22, "22"))
	defer restoreMap()
	restoreScope := ltschannel.MockSystemAllowed(true, false, true)
	defer restoreScope()

	_, err := ltschannel.SnapdLTSChannel(s.hybridClassicModel(c, "core22"), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "classic boot base not currently supported")
	c.Check(errors.Is(err, ltschannel.ErrLTSNotAllowed), Equals, true)
}

func (s *ltsSuite) TestSnapdLTSChannelClassicInScopeNotAllowed(c *C) {
	restoreMap := ltschannel.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restoreMap()
	restoreScope := ltschannel.MockSystemAllowed(true, true, false)
	defer restoreScope()

	_, err := ltschannel.SnapdLTSChannel(s.classicModel(c), "latest/stable", nil)
	c.Assert(err, ErrorMatches, "classic boot base not currently supported")
	c.Check(errors.Is(err, ltschannel.ErrLTSNotAllowed), Equals, true)
}

func (s *ltsSuite) TestSnapdLTSChannelCandidateRemap(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidate := s.snapdContainer(c, uc18CandidateInfo)

	resolved, err := ltschannel.SnapdLTSChannel(model, "latest/stable", candidate)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")

	resolved, err = ltschannel.SnapdLTSChannel(model, "fips-updates/candidate", candidate)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18-fips/candidate")
}

func (s *ltsSuite) TestSnapdLTSChannelCandidateUsesMapNotRunning(c *C) {
	// Running loader has no UC18 onboarded; nil candidate errors.
	restore := ltschannel.MockSnapdLTSTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")

	_, err := ltschannel.SnapdLTSChannel(model, "latest/stable", nil)
	c.Assert(err, ErrorMatches, `no LTS track map for boot base 18 from running snapd version 2.75`)
	c.Check(errors.Is(err, ltschannel.ErrLTSBaseNotManaged), Equals, true)

	candidate := s.snapdContainer(c, uc18CandidateInfo)
	resolved, err := ltschannel.SnapdLTSChannel(model, "latest/stable", candidate)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "18/stable")
}

func (s *ltsSuite) TestSnapdLTSChannelCandidateWithoutMapErrors(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int]map[string]string{})
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidate := s.snapdContainer(c, "VERSION=2.99\n")

	_, err := ltschannel.SnapdLTSChannel(model, "latest/stable", candidate)
	c.Assert(err, ErrorMatches, `no LTS track map for boot base 18 from candidate snapd version 2.99`)
	c.Check(errors.Is(err, ltschannel.ErrLTSBaseNotManaged), Equals, true)
}

func (s *ltsSuite) TestSnapdLTSChannelCandidateUnmanagedBootBaseErrors(c *C) {
	model := s.coreModel(c, "core22", "pc=22", "pc-kernel=22")
	candidate := s.snapdContainer(c, uc18CandidateInfo)

	_, err := ltschannel.SnapdLTSChannel(model, "latest/stable", candidate)
	c.Assert(err, ErrorMatches, `no LTS track map for boot base 22 from candidate snapd version 2.99`)
	c.Check(errors.Is(err, ltschannel.ErrLTSBaseNotManaged), Equals, true)
}

func (s *ltsSuite) TestSnapdLTSChannelCandidateErrors(c *C) {
	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	candidate := s.snapdContainer(c, uc18CandidateInfo)

	_, err := ltschannel.SnapdLTSChannel(nil, "latest/stable", candidate)
	c.Check(err, ErrorMatches, "internal error: cannot use nil model")
	c.Check(errors.Is(err, ltschannel.ErrLTSInternal), Equals, true)

	_, err = ltschannel.SnapdLTSChannel(model, "20/stable", candidate)
	c.Check(err, ErrorMatches, `no LTS track for boot base 18 for input track "20" from candidate snapd version 2.99`)
	c.Check(errors.Is(err, ltschannel.ErrLTSNoTrack), Equals, true)
}

func (s *ltsSuite) TestSnapdLTSChannelCandidateUsesCandidateMap(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(ltsTrackMap(18, "18"))
	defer restore()

	model := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	// Running map would remap latest to 18; candidate map takes precedence.
	candidate := s.snapdContainer(c, `VERSION=2.70
SNAPD_LTS_TRACKS='{"18":{"latest":"20","20":"20"}}'`)

	resolved, err := ltschannel.SnapdLTSChannel(model, "latest/stable", candidate)
	c.Assert(err, IsNil)
	c.Check(resolved, Equals, "20/stable")
}
