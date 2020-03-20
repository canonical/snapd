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
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type mockedSystemSeed struct {
	label string
	model *asserts.Model
	brand *asserts.Account
}

type deviceMgrSystemsSuite struct {
	testutil.BaseTest

	mgr *devicestate.DeviceManager

	mockedSystemSeeds []mockedSystemSeed
}

var _ = Suite(&deviceMgrSystemsSuite{})

func (s *deviceMgrSystemsSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.AddCleanup(func() { dirs.SetRootDir("/") })

	o := overlord.Mock()
	state := o.State()

	hookMgr, err := hookstate.Manager(state, o.TaskRunner())
	c.Assert(err, IsNil)
	mgr, err := devicestate.Manager(state, hookMgr, o.TaskRunner(), nil)
	c.Assert(err, IsNil)
	s.mgr = mgr

	// now create a minimal uc20 seed dir with snaps/assertions
	seed20 := &seedtest.TestingSeed20{SeedDir: dirs.SnapSeedDir}
	seed20.SetupAssertSigning("canonical")

	restore := seed.MockTrusted(seed20.StoreSigning.Trusted)
	s.AddCleanup(restore)

	seed20.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"display-name": "fancy model publisher",
		"validation":   "certified",
	})
	seed20.Brands.Register("other-brand", brandPrivKey2, map[string]interface{}{
		"display-name": "other publisher",
	})
	myBrandAcc := seed20.Brands.Account("my-brand")
	otherBrandAcc := seed20.Brands.Account("other-brand")

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

func (s *deviceMgrSystemsSuite) TestListSeedSystems(c *C) {
	systems, err := s.mgr.Systems()
	c.Assert(err, IsNil)
	c.Assert(systems, HasLen, 3)
	c.Check(systems, DeepEquals, []devicestate.System{{
		Current: false,
		Label:   s.mockedSystemSeeds[0].label,
		Model:   s.mockedSystemSeeds[0].model,
		Brand:   s.mockedSystemSeeds[0].brand,
	}, {
		Current: false,
		Label:   s.mockedSystemSeeds[1].label,
		Model:   s.mockedSystemSeeds[1].model,
		Brand:   s.mockedSystemSeeds[1].brand,
	}, {
		Current: false,
		Label:   s.mockedSystemSeeds[2].label,
		Model:   s.mockedSystemSeeds[2].model,
		Brand:   s.mockedSystemSeeds[2].brand,
	}})
}

func (s *deviceMgrSystemsSuite) TestBrokenSeedSystems(c *C) {
	// break the first seed
	err := os.Remove(filepath.Join(dirs.SnapSeedDir, "systems", s.mockedSystemSeeds[0].label, "model"))
	c.Assert(err, IsNil)

	systems, err := s.mgr.Systems()
	c.Assert(err, IsNil)
	c.Assert(systems, HasLen, 2)
	c.Check(systems, DeepEquals, []devicestate.System{{
		Current: false,
		Label:   s.mockedSystemSeeds[1].label,
		Model:   s.mockedSystemSeeds[1].model,
		Brand:   s.mockedSystemSeeds[1].brand,
	}, {
		Current: false,
		Label:   s.mockedSystemSeeds[2].label,
		Model:   s.mockedSystemSeeds[2].model,
		Brand:   s.mockedSystemSeeds[2].brand,
	}})
}
