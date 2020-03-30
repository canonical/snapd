// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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

package devicestate_test

import (
	"errors"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type mockedSystemSeed struct {
	label string
	model *asserts.Model
	brand *asserts.Account
}

type deviceMgrSystemsSuite struct {
	deviceMgrBaseSuite

	mockedSystemSeeds []mockedSystemSeed
}

var _ = Suite(&deviceMgrSystemsSuite{})

func (s *deviceMgrSystemsSuite) SetUpTest(c *C) {
	s.deviceMgrBaseSuite.SetUpTest(c)

	s.brands.Register("other-brand", brandPrivKey3, map[string]interface{}{
		"display-name": "other publisher",
	})

	s.state.Lock()
	defer s.state.Unlock()
	s.makeModelAssertionInState(c, "canonical", "pc-20", map[string]interface{}{
		"architecture": "amd64",
		// UC20
		"grade": "dangerous",
		"base":  "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              snaptest.AssertedSnapID("oc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              snaptest.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	})
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-20",
		Serial: "serialserialserial",
	})
	assertstest.AddMany(s.storeSigning.Database, s.brands.AccountsAndKeys("my-brand")...)
	assertstest.AddMany(s.storeSigning.Database, s.brands.AccountsAndKeys("other-brand")...)

	// now create a minimal uc20 seed dir with snaps/assertions
	seed20 := &seedtest.TestingSeed20{
		SeedSnaps: seedtest.SeedSnaps{
			StoreSigning: s.storeSigning,
			Brands:       s.brands,
		},

		SeedDir: dirs.SnapSeedDir,
	}

	restore := seed.MockTrusted(s.storeSigning.Trusted)
	s.AddCleanup(restore)

	myBrandAcc := s.brands.Account("my-brand")
	otherBrandAcc := s.brands.Account("other-brand")

	// add essential snaps
	seed20.MakeAssertedSnap(c, "name: snapd\nversion: 1\ntype: snapd", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)
	seed20.MakeAssertedSnap(c, "name: pc\nversion: 1\ntype: gadget\nbase: core20", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)
	seed20.MakeAssertedSnap(c, "name: pc-kernel\nversion: 1\ntype: kernel", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)
	seed20.MakeAssertedSnap(c, "name: core20\nversion: 1\ntype: base", nil, snap.R(1), "canonical", seed20.StoreSigning.Database)

	model1 := seed20.MakeSeed(c, "20191119", "my-brand", "my-model", map[string]interface{}{
		"display-name": "my fancy model",
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              seed20.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              seed20.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			}},
	}, nil)
	model2 := seed20.MakeSeed(c, "20200318", "my-brand", "my-model-2", map[string]interface{}{
		"display-name": "same brand different model",
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              seed20.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              seed20.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			}},
	}, nil)
	model3 := seed20.MakeSeed(c, "other-20200318", "other-brand", "other-model", map[string]interface{}{
		"display-name": "different brand different model",
		"architecture": "amd64",
		"base":         "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              seed20.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              seed20.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			}},
	}, nil)

	s.mockedSystemSeeds = []mockedSystemSeed{{
		label: "20191119",
		model: model1,
		brand: myBrandAcc,
	}, {
		label: "20200318",
		model: model2,
		brand: myBrandAcc,
	}, {
		label: "other-20200318",
		model: model3,
		brand: otherBrandAcc,
	}}

}

func (s *deviceMgrSystemsSuite) TestListNoSystems(c *C) {
	dirs.SetRootDir(c.MkDir())

	systems, err := s.mgr.Systems()
	c.Assert(err, Equals, devicestate.ErrNoSystems)
	c.Assert(systems, HasLen, 0)

	err = os.MkdirAll(filepath.Join(dirs.SnapSeedDir, "systems"), 0755)
	c.Assert(err, IsNil)

	systems, err = s.mgr.Systems()
	c.Assert(err, Equals, devicestate.ErrNoSystems)
	c.Assert(systems, HasLen, 0)
}

func (s *deviceMgrSystemsSuite) TestListSystemsNotPossible(c *C) {
	if os.Geteuid() == 0 {
		c.Skip("this test cannot run as root")
	}
	err := os.Chmod(filepath.Join(dirs.SnapSeedDir, "systems"), 0000)
	c.Assert(err, IsNil)
	defer os.Chmod(filepath.Join(dirs.SnapSeedDir, "systems"), 0755)

	// stdlib swallows up the errors when opening the target directory
	systems, err := s.mgr.Systems()
	c.Assert(err, Equals, devicestate.ErrNoSystems)
	c.Assert(systems, HasLen, 0)
}

// TODO:UC20 update once we can list actions
var defaultActions []devicestate.SystemAction = []devicestate.SystemAction{
	{Title: "reinstall", Mode: "install"},
}

func (s *deviceMgrSystemsSuite) TestListSeedSystems(c *C) {
	systems, err := s.mgr.Systems()
	c.Assert(err, IsNil)
	c.Assert(systems, HasLen, 3)
	c.Check(systems, DeepEquals, []*devicestate.System{{
		Current: false,
		Label:   s.mockedSystemSeeds[0].label,
		Model:   s.mockedSystemSeeds[0].model,
		Brand:   s.mockedSystemSeeds[0].brand,
		Actions: defaultActions,
	}, {
		Current: false,
		Label:   s.mockedSystemSeeds[1].label,
		Model:   s.mockedSystemSeeds[1].model,
		Brand:   s.mockedSystemSeeds[1].brand,
		Actions: defaultActions,
	}, {
		Current: false,
		Label:   s.mockedSystemSeeds[2].label,
		Model:   s.mockedSystemSeeds[2].model,
		Brand:   s.mockedSystemSeeds[2].brand,
		Actions: defaultActions,
	}})
}

func (s *deviceMgrSystemsSuite) TestBrokenSeedSystems(c *C) {
	// break the first seed
	err := os.Remove(filepath.Join(dirs.SnapSeedDir, "systems", s.mockedSystemSeeds[0].label, "model"))
	c.Assert(err, IsNil)

	systems, err := s.mgr.Systems()
	c.Assert(err, IsNil)
	c.Assert(systems, HasLen, 2)
	c.Check(systems, DeepEquals, []*devicestate.System{{
		Current: false,
		Label:   s.mockedSystemSeeds[1].label,
		Model:   s.mockedSystemSeeds[1].model,
		Brand:   s.mockedSystemSeeds[1].brand,
		Actions: defaultActions,
	}, {
		Current: false,
		Label:   s.mockedSystemSeeds[2].label,
		Model:   s.mockedSystemSeeds[2].model,
		Brand:   s.mockedSystemSeeds[2].brand,
		Actions: defaultActions,
	}})
}

func (s *deviceMgrSystemsSuite) TestRequestModeHappy(c *C) {
	err := s.mgr.RequestSystemAction("20191119", devicestate.SystemAction{Mode: "install"})
	c.Assert(err, IsNil)

	m, err := s.bootloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"snapd_recovery_system": "20191119",
		"snapd_recovery_mode":   "install",
	})
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystem})
}

func (s *deviceMgrSystemsSuite) TestRequestModeErrInBoot(c *C) {
	s.bootloader.SetErr = errors.New("no can do")
	err := s.mgr.RequestSystemAction("20191119", devicestate.SystemAction{Mode: "install"})
	c.Assert(err, ErrorMatches, `cannot boot into system "20191119" in mode "install": no can do`)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrSystemsSuite) TestRequestModeNotFound(c *C) {
	err := s.mgr.RequestSystemAction("not-found", devicestate.SystemAction{Mode: "install"})
	c.Assert(err, NotNil)
	c.Assert(os.IsNotExist(err), Equals, true)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrSystemsSuite) TestRequestModeBadMode(c *C) {
	err := s.mgr.RequestSystemAction("20191119", devicestate.SystemAction{Mode: "unknown-mode"})
	c.Assert(err, Equals, devicestate.ErrUnsupportedAction)
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrSystemsSuite) TestRequestModeBroken(c *C) {
	// break the first seed
	err := os.Remove(filepath.Join(dirs.SnapSeedDir, "systems", s.mockedSystemSeeds[0].label, "model"))
	c.Assert(err, IsNil)

	err = s.mgr.RequestSystemAction("20191119", devicestate.SystemAction{Mode: "install"})
	c.Assert(err, ErrorMatches, "cannot load seed system: cannot load assertions: .*")
	c.Check(s.restartRequests, HasLen, 0)
}

func (s *deviceMgrSystemsSuite) TestRequestModeNonUC20(c *C) {
	s.setPCModelInState(c)
	err := s.mgr.RequestSystemAction("20191119", devicestate.SystemAction{Mode: "install"})
	c.Assert(err, ErrorMatches, `cannot boot into system "20191119" in mode "install": system boot mode is unsupported`)
	c.Check(s.restartRequests, HasLen, 0)
}
