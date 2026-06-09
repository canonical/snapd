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
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/snap/ltschannel"
)

type policySuite struct {
	brands *assertstest.SigningAccounts
}

var _ = Suite(&policySuite{})

func (s *policySuite) SetUpTest(c *C) {
	brandKey, _ := assertstest.GenerateKey(752)
	store := assertstest.NewStoreStack("store", nil)
	s.brands = assertstest.NewSigningAccounts(store)
	s.brands.Register("my-brand", brandKey, nil)
}

func (s *policySuite) coreModel(c *C, base, gadget, kernel string) *asserts.Model {
	return s.brands.Model("my-brand", "my-model", map[string]any{
		"architecture": "amd64",
		"base":         base,
		"gadget":       gadget,
		"kernel":       kernel,
	})
}

func (s *policySuite) classicModel(c *C) *asserts.Model {
	return s.brands.Model("my-brand", "my-model", map[string]any{
		"architecture": "amd64",
		"classic":      "true",
	})
}

func (s *policySuite) hybridClassicModel(c *C, base string) *asserts.Model {
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

// Managed boot base returns the LTS track allow-list and applies=true.
func (s *policySuite) TestManagedBootBase(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{18: {"18", "18-fips"}})
	defer restore()

	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	tracks, applies, err := ltschannel.SnapdLTSTracksForModel(uc18)
	c.Assert(err, IsNil)
	c.Check(applies, Equals, true)
	c.Check(tracks, DeepEquals, map[string]string{
		"latest":       "18",
		"18":           "18",
		"18-fips":      "18-fips",
		"fips-updates": "18-fips",
	})
}

// Boot base that is not yet onboarded -> not applicable, no error.
func (s *policySuite) TestUnmanagedBootBasePassthrough(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{18: {"18"}})
	defer restore()

	uc22 := s.coreModel(c, "core22", "pc=22", "pc-kernel=22")
	tracks, applies, err := ltschannel.SnapdLTSTracksForModel(uc22)
	c.Assert(err, IsNil)
	c.Check(applies, Equals, false)
	c.Check(tracks, IsNil)
}

// Empty production map -> any UC model is unmanaged.
func (s *policySuite) TestEmptyMapPassthrough(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{})
	defer restore()

	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	tracks, applies, err := ltschannel.SnapdLTSTracksForModel(uc18)
	c.Assert(err, IsNil)
	c.Check(applies, Equals, false)
	c.Check(tracks, IsNil)
}

// UC16 -> hard error, defense in depth (independent of map contents).
func (s *policySuite) TestUC16Rejected(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{16: {"16"}})
	defer restore()

	uc16 := s.coreModel(c, "core", "pc", "pc-kernel")
	tracks, applies, err := ltschannel.SnapdLTSTracksForModel(uc16)
	c.Check(err, ErrorMatches, "cannot use unsupported Ubuntu Core 16 model")
	c.Check(applies, Equals, false)
	c.Check(tracks, IsNil)
}

// Classic (non-hybrid) default: out of scope -> passthrough, no error.
func (s *policySuite) TestClassicOutOfScope(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{18: {"18"}})
	defer restore()

	tracks, applies, err := ltschannel.SnapdLTSTracksForModel(s.classicModel(c))
	c.Assert(err, IsNil)
	c.Check(applies, Equals, false)
	c.Check(tracks, IsNil)
}

// Hybrid classic default: out of scope -> passthrough.
func (s *policySuite) TestHybridClassicOutOfScope(c *C) {
	restore := ltschannel.MockSnapdLTSTrackMap(map[int][]string{22: {"22"}})
	defer restore()

	tracks, applies, err := ltschannel.SnapdLTSTracksForModel(s.hybridClassicModel(c, "core22"))
	c.Assert(err, IsNil)
	c.Check(applies, Equals, false)
	c.Check(tracks, IsNil)
}

// Hybrid classic in scope + managed boot base -> applies.
func (s *policySuite) TestHybridClassicInScopeManaged(c *C) {
	restoreMap := ltschannel.MockSnapdLTSTrackMap(map[int][]string{22: {"22"}})
	defer restoreMap()
	restoreScope := ltschannel.MockSnapdLTSDeviceKindScope(true, false, true)
	defer restoreScope()

	tracks, applies, err := ltschannel.SnapdLTSTracksForModel(s.hybridClassicModel(c, "core22"))
	c.Assert(err, IsNil)
	c.Check(applies, Equals, true)
	c.Check(tracks, DeepEquals, map[string]string{
		"latest": "22",
		"22":     "22",
	})
}

// Ubuntu Core scope disabled -> UC model passthrough even if managed.
func (s *policySuite) TestUbuntuCoreScopeDisabled(c *C) {
	restoreMap := ltschannel.MockSnapdLTSTrackMap(map[int][]string{18: {"18"}})
	defer restoreMap()
	restoreScope := ltschannel.MockSnapdLTSDeviceKindScope(false, false, false)
	defer restoreScope()

	uc18 := s.coreModel(c, "core18", "pc=18", "pc-kernel=18")
	tracks, applies, err := ltschannel.SnapdLTSTracksForModel(uc18)
	c.Assert(err, IsNil)
	c.Check(applies, Equals, false)
	c.Check(tracks, IsNil)
}

// Classic in scope with no/non-core base -> CoreVersion errors -> passthrough.
// Exercises the model.CoreVersion() error branch that the SnapdLTSChannel
// tests never hit (they fail at the device-kind gate first).
func (s *policySuite) TestClassicInScopeNonCoreBase(c *C) {
	restoreMap := ltschannel.MockSnapdLTSTrackMap(map[int][]string{18: {"18"}})
	defer restoreMap()
	restoreScope := ltschannel.MockSnapdLTSDeviceKindScope(true, true, false)
	defer restoreScope()

	tracks, applies, err := ltschannel.SnapdLTSTracksForModel(s.classicModel(c))
	c.Assert(err, IsNil)
	c.Check(applies, Equals, false)
	c.Check(tracks, IsNil)
}
