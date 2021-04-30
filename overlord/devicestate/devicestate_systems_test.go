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
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
)

type mockedSystemSeed struct {
	label string
	model *asserts.Model
	brand *asserts.Account
}

type deviceMgrSystemsBaseSuite struct {
	deviceMgrBaseSuite

	logbuf            *bytes.Buffer
	mockedSystemSeeds []mockedSystemSeed
	ss                *seedtest.SeedSnaps
}

type deviceMgrSystemsSuite struct {
	deviceMgrSystemsBaseSuite
}

var _ = Suite(&deviceMgrSystemsSuite{})
var _ = Suite(&deviceMgrSystemsCreateSuite{})

func (s *deviceMgrSystemsBaseSuite) SetUpTest(c *C) {
	s.deviceMgrBaseSuite.SetUpTest(c)

	s.brands.Register("other-brand", brandPrivKey3, map[string]interface{}{
		"display-name": "other publisher",
	})
	s.state.Lock()
	defer s.state.Unlock()
	s.ss = &seedtest.SeedSnaps{
		StoreSigning: s.storeSigning,
		Brands:       s.brands,
	}

	s.makeModelAssertionInState(c, "canonical", "pc-20", map[string]interface{}{
		"architecture": "amd64",
		// UC20
		"grade": "dangerous",
		"base":  "core20",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              s.ss.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              s.ss.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "core20",
				"id":   s.ss.AssertedSnapID("core20"),
				"type": "base",
			},
			map[string]interface{}{
				"name": "snapd",
				"id":   s.ss.AssertedSnapID("snapd"),
				"type": "snapd",
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

	// all tests should be in run mode by default, if they need to be in
	// different modes they should set that individually
	devicestate.SetSystemMode(s.mgr, "run")

	// state after mark-seeded ran
	modeenv := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "",
	}
	err := modeenv.WriteTo("")
	s.state.Set("seeded", true)

	c.Assert(err, IsNil)

	logbuf, restore := logger.MockLogger()
	s.logbuf = logbuf
	s.AddCleanup(restore)
}

func (s *deviceMgrSystemsSuite) SetUpTest(c *C) {
	s.deviceMgrSystemsBaseSuite.SetUpTest(c)

	// now create a minimal uc20 seed dir with snaps/assertions
	seed20 := &seedtest.TestingSeed20{
		SeedSnaps: *s.ss,
		SeedDir:   dirs.SnapSeedDir,
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
var defaultSystemActions []devicestate.SystemAction = []devicestate.SystemAction{
	{Title: "Install", Mode: "install"},
}
var currentSystemActions []devicestate.SystemAction = []devicestate.SystemAction{
	{Title: "Reinstall", Mode: "install"},
	{Title: "Recover", Mode: "recover"},
	{Title: "Run normally", Mode: "run"},
}

func (s *deviceMgrSystemsSuite) TestListSeedSystemsNoCurrent(c *C) {
	systems, err := s.mgr.Systems()
	c.Assert(err, IsNil)
	c.Assert(systems, HasLen, 3)
	c.Check(systems, DeepEquals, []*devicestate.System{{
		Current: false,
		Label:   s.mockedSystemSeeds[0].label,
		Model:   s.mockedSystemSeeds[0].model,
		Brand:   s.mockedSystemSeeds[0].brand,
		Actions: defaultSystemActions,
	}, {
		Current: false,
		Label:   s.mockedSystemSeeds[1].label,
		Model:   s.mockedSystemSeeds[1].model,
		Brand:   s.mockedSystemSeeds[1].brand,
		Actions: defaultSystemActions,
	}, {
		Current: false,
		Label:   s.mockedSystemSeeds[2].label,
		Model:   s.mockedSystemSeeds[2].model,
		Brand:   s.mockedSystemSeeds[2].brand,
		Actions: defaultSystemActions,
	}})
}

func (s *deviceMgrSystemsSuite) TestListSeedSystemsCurrent(c *C) {
	s.state.Lock()
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:  s.mockedSystemSeeds[1].label,
			Model:   s.mockedSystemSeeds[1].model.Model(),
			BrandID: s.mockedSystemSeeds[1].brand.AccountID(),
		},
	})
	s.state.Unlock()

	systems, err := s.mgr.Systems()
	c.Assert(err, IsNil)
	c.Assert(systems, HasLen, 3)
	c.Check(systems, DeepEquals, []*devicestate.System{{
		Current: false,
		Label:   s.mockedSystemSeeds[0].label,
		Model:   s.mockedSystemSeeds[0].model,
		Brand:   s.mockedSystemSeeds[0].brand,
		Actions: defaultSystemActions,
	}, {
		// this seed was used for installing the running system
		Current: true,
		Label:   s.mockedSystemSeeds[1].label,
		Model:   s.mockedSystemSeeds[1].model,
		Brand:   s.mockedSystemSeeds[1].brand,
		Actions: currentSystemActions,
	}, {
		Current: false,
		Label:   s.mockedSystemSeeds[2].label,
		Model:   s.mockedSystemSeeds[2].model,
		Brand:   s.mockedSystemSeeds[2].brand,
		Actions: defaultSystemActions,
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
		Actions: defaultSystemActions,
	}, {
		Current: false,
		Label:   s.mockedSystemSeeds[2].label,
		Model:   s.mockedSystemSeeds[2].model,
		Brand:   s.mockedSystemSeeds[2].brand,
		Actions: defaultSystemActions,
	}})
}

func (s *deviceMgrSystemsSuite) TestRequestModeInstallHappyForAny(c *C) {
	// no current system
	err := s.mgr.RequestSystemAction("20191119", devicestate.SystemAction{Mode: "install", Title: "Install"})
	c.Assert(err, IsNil)

	m, err := s.bootloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"snapd_recovery_system": "20191119",
		"snapd_recovery_mode":   "install",
	})
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})
	c.Check(s.logbuf.String(), Matches, `.*: restarting into system "20191119" for action "Install"\n`)
}

func (s *deviceMgrSystemsSuite) TestRequestSameModeSameSystem(c *C) {
	s.state.Lock()
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:  s.mockedSystemSeeds[0].label,
			Model:   s.mockedSystemSeeds[0].model.Model(),
			BrandID: s.mockedSystemSeeds[0].brand.AccountID(),
		},
	})
	s.state.Unlock()

	label := s.mockedSystemSeeds[0].label

	happyModes := []string{"run"}
	sadModes := []string{"install", "recover"}

	for _, mode := range append(happyModes, sadModes...) {
		s.logbuf.Reset()

		c.Logf("checking mode: %q", mode)
		// non run modes use modeenv
		modeenv := boot.Modeenv{
			Mode: mode,
		}
		if mode != "run" {
			modeenv.RecoverySystem = s.mockedSystemSeeds[0].label
		}
		err := modeenv.WriteTo("")
		c.Assert(err, IsNil)

		devicestate.SetSystemMode(s.mgr, mode)
		err = s.bootloader.SetBootVars(map[string]string{
			"snapd_recovery_mode":   mode,
			"snapd_recovery_system": label,
		})
		c.Assert(err, IsNil)
		err = s.mgr.RequestSystemAction(label, devicestate.SystemAction{Mode: mode})
		if strutil.ListContains(sadModes, mode) {
			c.Assert(err, Equals, devicestate.ErrUnsupportedAction)
		} else {
			c.Assert(err, IsNil)
		}
		// bootloader vars shouldn't change
		m, err := s.bootloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
		c.Assert(err, IsNil)
		c.Check(m, DeepEquals, map[string]string{
			"snapd_recovery_mode":   mode,
			"snapd_recovery_system": label,
		})
		// should never restart
		c.Check(s.restartRequests, HasLen, 0)
		// no log output
		c.Check(s.logbuf.String(), Equals, "")
	}
}

func (s *deviceMgrSystemsSuite) TestRequestSeedingSameConflict(c *C) {
	label := s.mockedSystemSeeds[0].label

	devicestate.SetSystemMode(s.mgr, "run")

	s.state.Lock()
	s.state.Set("seeded", nil)
	s.state.Unlock()

	for _, mode := range []string{"run", "install", "recover"} {
		s.logbuf.Reset()

		c.Logf("checking mode: %q", mode)
		modeenv := boot.Modeenv{
			Mode:           mode,
			RecoverySystem: s.mockedSystemSeeds[0].label,
		}
		err := modeenv.WriteTo("")
		c.Assert(err, IsNil)

		err = s.bootloader.SetBootVars(map[string]string{
			"snapd_recovery_mode":   "",
			"snapd_recovery_system": label,
		})
		c.Assert(err, IsNil)
		err = s.mgr.RequestSystemAction(label, devicestate.SystemAction{Mode: mode})
		c.Assert(err, ErrorMatches, "cannot request system action, system is seeding")
		// no log output
		c.Check(s.logbuf.String(), Equals, "")
	}
}

func (s *deviceMgrSystemsSuite) TestRequestSeedingDifferentNoConflict(c *C) {
	label := s.mockedSystemSeeds[0].label
	otherLabel := s.mockedSystemSeeds[1].label

	devicestate.SetSystemMode(s.mgr, "run")

	modeenv := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: label,
	}
	err := modeenv.WriteTo("")
	c.Assert(err, IsNil)

	s.state.Lock()
	s.state.Set("seeded", nil)
	s.state.Unlock()

	// we can only go to install mode of other system when one is currently
	// being seeded
	err = s.bootloader.SetBootVars(map[string]string{
		"snapd_recovery_mode":   "",
		"snapd_recovery_system": label,
	})
	c.Assert(err, IsNil)
	err = s.mgr.RequestSystemAction(otherLabel, devicestate.SystemAction{Mode: "install"})
	c.Assert(err, IsNil)
	m, err := s.bootloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"snapd_recovery_system": otherLabel,
		"snapd_recovery_mode":   "install",
	})
	c.Check(s.logbuf.String(), Matches, fmt.Sprintf(`.*: restarting into system "%s" for action "Install"\n`, otherLabel))
}

func (s *deviceMgrSystemsSuite) testRequestModeWithRestart(c *C, toModes []string, label string) {
	for _, mode := range toModes {
		c.Logf("checking mode: %q", mode)
		err := s.mgr.RequestSystemAction(label, devicestate.SystemAction{Mode: mode})
		c.Assert(err, IsNil)
		m, err := s.bootloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
		c.Assert(err, IsNil)
		c.Check(m, DeepEquals, map[string]string{
			"snapd_recovery_system": label,
			"snapd_recovery_mode":   mode,
		})
		c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})
		s.restartRequests = nil
		s.bootloader.BootVars = map[string]string{}

		// TODO: also test correct action string logging
		c.Check(s.logbuf.String(), Matches, fmt.Sprintf(`.*: restarting into system "%s" for action ".*"\n`, label))
		s.logbuf.Reset()
	}
}

func (s *deviceMgrSystemsSuite) TestRequestModeRunInstallForRecover(c *C) {
	// we are in recover mode here
	devicestate.SetSystemMode(s.mgr, "recover")
	// non run modes use modeenv
	modeenv := boot.Modeenv{
		Mode:           "recover",
		RecoverySystem: s.mockedSystemSeeds[0].label,
	}
	err := modeenv.WriteTo("")
	c.Assert(err, IsNil)

	s.state.Lock()
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:  s.mockedSystemSeeds[0].label,
			Model:   s.mockedSystemSeeds[0].model.Model(),
			BrandID: s.mockedSystemSeeds[0].brand.AccountID(),
		},
	})
	s.state.Unlock()

	s.testRequestModeWithRestart(c, []string{"install", "run"}, s.mockedSystemSeeds[0].label)
}

func (s *deviceMgrSystemsSuite) TestRequestModeInstallRecoverForCurrent(c *C) {
	devicestate.SetSystemMode(s.mgr, "run")
	// non run modes use modeenv
	modeenv := boot.Modeenv{
		Mode: "run",
	}
	err := modeenv.WriteTo("")
	c.Assert(err, IsNil)

	s.state.Lock()
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:  s.mockedSystemSeeds[0].label,
			Model:   s.mockedSystemSeeds[0].model.Model(),
			BrandID: s.mockedSystemSeeds[0].brand.AccountID(),
		},
	})
	s.state.Unlock()

	s.testRequestModeWithRestart(c, []string{"install", "recover"}, s.mockedSystemSeeds[0].label)
}

func (s *deviceMgrSystemsSuite) TestRequestModeErrInBoot(c *C) {
	s.bootloader.SetErr = errors.New("no can do")
	err := s.mgr.RequestSystemAction("20191119", devicestate.SystemAction{Mode: "install"})
	c.Assert(err, ErrorMatches, `cannot set device to boot into system "20191119" in mode "install": no can do`)
	c.Check(s.restartRequests, HasLen, 0)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *deviceMgrSystemsSuite) TestRequestModeNotFound(c *C) {
	err := s.mgr.RequestSystemAction("not-found", devicestate.SystemAction{Mode: "install"})
	c.Assert(err, NotNil)
	c.Assert(os.IsNotExist(err), Equals, true)
	c.Check(s.restartRequests, HasLen, 0)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *deviceMgrSystemsSuite) TestRequestModeBadMode(c *C) {
	err := s.mgr.RequestSystemAction("20191119", devicestate.SystemAction{Mode: "unknown-mode"})
	c.Assert(err, Equals, devicestate.ErrUnsupportedAction)
	c.Check(s.restartRequests, HasLen, 0)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *deviceMgrSystemsSuite) TestRequestModeBroken(c *C) {
	// break the first seed
	err := os.Remove(filepath.Join(dirs.SnapSeedDir, "systems", s.mockedSystemSeeds[0].label, "model"))
	c.Assert(err, IsNil)

	err = s.mgr.RequestSystemAction("20191119", devicestate.SystemAction{Mode: "install"})
	c.Assert(err, ErrorMatches, "cannot load seed system: cannot load assertions: .*")
	c.Check(s.restartRequests, HasLen, 0)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *deviceMgrSystemsSuite) TestRequestModeNonUC20(c *C) {
	s.setPCModelInState(c)
	err := s.mgr.RequestSystemAction("20191119", devicestate.SystemAction{Mode: "install"})
	c.Assert(err, ErrorMatches, `cannot set device to boot into system "20191119" in mode "install": system mode is unsupported`)
	c.Check(s.restartRequests, HasLen, 0)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *deviceMgrSystemsSuite) TestRequestActionNoLabel(c *C) {
	err := s.mgr.RequestSystemAction("", devicestate.SystemAction{Mode: "install"})
	c.Assert(err, ErrorMatches, "internal error: system label is unset")
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *deviceMgrSystemsSuite) TestRequestModeForNonCurrent(c *C) {
	s.state.Lock()
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:  s.mockedSystemSeeds[0].label,
			Model:   s.mockedSystemSeeds[0].model.Model(),
			BrandID: s.mockedSystemSeeds[0].brand.AccountID(),
		},
	})

	s.state.Unlock()
	s.setPCModelInState(c)
	// request mode reserved for current system
	err := s.mgr.RequestSystemAction(s.mockedSystemSeeds[1].label, devicestate.SystemAction{Mode: "run"})
	c.Assert(err, Equals, devicestate.ErrUnsupportedAction)
	err = s.mgr.RequestSystemAction(s.mockedSystemSeeds[1].label, devicestate.SystemAction{Mode: "recover"})
	c.Assert(err, Equals, devicestate.ErrUnsupportedAction)
	c.Check(s.restartRequests, HasLen, 0)
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *deviceMgrSystemsSuite) TestRequestInstallForOther(c *C) {
	devicestate.SetSystemMode(s.mgr, "run")
	// non run modes use modeenv
	modeenv := boot.Modeenv{
		Mode: "run",
	}
	err := modeenv.WriteTo("")
	c.Assert(err, IsNil)

	s.state.Lock()
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:  s.mockedSystemSeeds[0].label,
			Model:   s.mockedSystemSeeds[0].model.Model(),
			BrandID: s.mockedSystemSeeds[0].brand.AccountID(),
		},
	})
	s.state.Unlock()
	// reinstall from different system seed is ok
	s.testRequestModeWithRestart(c, []string{"install"}, s.mockedSystemSeeds[1].label)
}

func (s *deviceMgrSystemsSuite) TestRequestAction1618(c *C) {
	s.setPCModelInState(c)
	// system mode is unset in 16/18
	devicestate.SetSystemMode(s.mgr, "")
	// no modeenv either
	err := os.Remove(dirs.SnapModeenvFileUnder(dirs.GlobalRootDir))
	c.Assert(err, IsNil)

	s.state.Lock()
	s.state.Set("seeded-systems", nil)
	s.state.Set("seeded", nil)
	s.state.Unlock()
	// a label exists
	err = s.mgr.RequestSystemAction(s.mockedSystemSeeds[0].label, devicestate.SystemAction{Mode: "install"})
	c.Assert(err, ErrorMatches, "cannot set device to boot .*: system mode is unsupported")

	s.state.Lock()
	s.state.Set("seeded", true)
	s.state.Unlock()

	// even with system mode explicitly set, the action is not executed
	devicestate.SetSystemMode(s.mgr, "run")

	err = s.mgr.RequestSystemAction(s.mockedSystemSeeds[0].label, devicestate.SystemAction{Mode: "install"})
	c.Assert(err, ErrorMatches, "cannot set device to boot .*: system mode is unsupported")

	devicestate.SetSystemMode(s.mgr, "")
	// also no UC20 style system seeds
	for _, m := range s.mockedSystemSeeds {
		os.RemoveAll(filepath.Join(dirs.SnapSeedDir, "systems", m.label))
	}

	err = s.mgr.RequestSystemAction(s.mockedSystemSeeds[0].label, devicestate.SystemAction{Mode: "install"})
	c.Assert(err, ErrorMatches, ".*/seed/systems/20191119: no such file or directory")
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *deviceMgrSystemsSuite) TestRebootNoLabelNoModeHappy(c *C) {
	err := s.mgr.Reboot("", "")
	c.Assert(err, IsNil)

	m, err := s.bootloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
	c.Assert(err, IsNil)
	// requested restart
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})
	// but no bootloader changes
	c.Check(m, DeepEquals, map[string]string{
		"snapd_recovery_system": "",
		"snapd_recovery_mode":   "",
	})
	c.Check(s.logbuf.String(), Matches, `.*: rebooting system\n`)
}

func (s *deviceMgrSystemsSuite) TestRebootLabelAndModeHappy(c *C) {
	s.state.Lock()
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:  s.mockedSystemSeeds[0].label,
			Model:   s.mockedSystemSeeds[0].model.Model(),
			BrandID: s.mockedSystemSeeds[0].brand.AccountID(),
		},
	})
	s.state.Unlock()

	err := s.mgr.Reboot("20191119", "install")
	c.Assert(err, IsNil)

	m, err := s.bootloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"snapd_recovery_system": "20191119",
		"snapd_recovery_mode":   "install",
	})
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})
	c.Check(s.logbuf.String(), Matches, `.*: rebooting into system "20191119" in "install" mode\n`)
}

func (s *deviceMgrSystemsSuite) TestRebootModeOnlyHappy(c *C) {
	s.state.Lock()
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:  s.mockedSystemSeeds[0].label,
			Model:   s.mockedSystemSeeds[0].model.Model(),
			BrandID: s.mockedSystemSeeds[0].brand.AccountID(),
		},
	})
	s.state.Unlock()

	for _, mode := range []string{"recover", "install"} {
		s.restartRequests = nil
		s.bootloader.BootVars = make(map[string]string)
		s.logbuf.Reset()

		err := s.mgr.Reboot("", mode)
		c.Assert(err, IsNil)

		m, err := s.bootloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
		c.Assert(err, IsNil)
		c.Check(m, DeepEquals, map[string]string{
			"snapd_recovery_system": s.mockedSystemSeeds[0].label,
			"snapd_recovery_mode":   mode,
		})
		c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})
		c.Check(s.logbuf.String(), Matches, fmt.Sprintf(`.*: rebooting into system "20191119" in "%s" mode\n`, mode))
	}
}

func (s *deviceMgrSystemsSuite) TestRebootFromRecoverToRun(c *C) {
	modeenv := boot.Modeenv{
		Mode:           "recover",
		RecoverySystem: s.mockedSystemSeeds[0].label,
	}
	err := modeenv.WriteTo("")
	c.Assert(err, IsNil)

	devicestate.SetSystemMode(s.mgr, "recover")
	err = s.bootloader.SetBootVars(map[string]string{
		"snapd_recovery_mode":   "recover",
		"snapd_recovery_system": s.mockedSystemSeeds[0].label,
	})
	c.Assert(err, IsNil)

	s.state.Lock()
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:  s.mockedSystemSeeds[0].label,
			Model:   s.mockedSystemSeeds[0].model.Model(),
			BrandID: s.mockedSystemSeeds[0].brand.AccountID(),
		},
	})
	s.state.Unlock()

	err = s.mgr.Reboot("", "run")
	c.Assert(err, IsNil)

	m, err := s.bootloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"snapd_recovery_mode":   "run",
		"snapd_recovery_system": s.mockedSystemSeeds[0].label,
	})
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})
	c.Check(s.logbuf.String(), Matches, fmt.Sprintf(`.*: rebooting into system "%s" in "run" mode\n`, s.mockedSystemSeeds[0].label))
}

func (s *deviceMgrSystemsSuite) TestRebootAlreadyInRunMode(c *C) {
	devicestate.SetSystemMode(s.mgr, "run")

	s.state.Lock()
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:  s.mockedSystemSeeds[0].label,
			Model:   s.mockedSystemSeeds[0].model.Model(),
			BrandID: s.mockedSystemSeeds[0].brand.AccountID(),
		},
	})
	s.state.Unlock()

	// we are already in "run" mode so this should just reboot
	err := s.mgr.Reboot("", "run")
	c.Assert(err, IsNil)

	m, err := s.bootloader.GetBootVars("snapd_recovery_mode", "snapd_recovery_system")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"snapd_recovery_mode":   "",
		"snapd_recovery_system": "",
	})
	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})
	c.Check(s.logbuf.String(), Matches, `.*: rebooting system\n`)
}

func (s *deviceMgrSystemsSuite) TestRebootUnhappy(c *C) {
	s.state.Lock()
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:  s.mockedSystemSeeds[0].label,
			Model:   s.mockedSystemSeeds[0].model.Model(),
			BrandID: s.mockedSystemSeeds[0].brand.AccountID(),
		},
	})
	s.state.Unlock()

	errUnsupportedActionStr := devicestate.ErrUnsupportedAction.Error()
	for _, tc := range []struct {
		systemLabel, mode string
		expectedErr       string
	}{
		{"", "unknown-mode", errUnsupportedActionStr},
		{"unknown-system", "run", `stat /.*: no such file or directory`},
		{"unknown-system", "unknown-mode", `stat /.*: no such file or directory`},
	} {
		s.restartRequests = nil
		s.bootloader.BootVars = make(map[string]string)

		err := s.mgr.Reboot(tc.systemLabel, tc.mode)
		c.Assert(err, ErrorMatches, tc.expectedErr)

		c.Check(s.restartRequests, HasLen, 0)
		c.Check(s.logbuf.String(), Equals, "")
	}
	c.Check(s.logbuf.String(), Equals, "")
}

func (s *deviceMgrSystemsSuite) TestDeviceManagerEnsureTriedSystemSuccessfuly(c *C) {
	err := s.bootloader.SetBootVars(map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)
	devicestate.SetBootOkRan(s.mgr, true)

	modeenv := boot.Modeenv{
		Mode: boot.ModeRun,
		// the system is in CurrentRecoverySystems
		CurrentRecoverySystems: []string{"29112019", "1234"},
	}
	err = modeenv.WriteTo("")
	c.Assert(err, IsNil)

	// system is considered successful, bootenv is cleared, the label is
	// recorded in tried-systems
	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})

	var triedSystems []string
	s.state.Lock()
	err = s.state.Get("tried-systems", &triedSystems)
	c.Assert(err, IsNil)
	c.Check(triedSystems, DeepEquals, []string{"1234"})
	// also logged
	c.Check(s.logbuf.String(), testutil.Contains, `tried recovery system "1234" was successful`)
	s.state.Unlock()

	// reset and run again, we need to populate boot variables again
	err = s.bootloader.SetBootVars(map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)
	devicestate.SetTriedSystemsRan(s.mgr, false)

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)
	s.state.Lock()
	defer s.state.Unlock()
	err = s.state.Get("tried-systems", &triedSystems)
	c.Assert(err, IsNil)
	// the system was already there, no duplicate got appended
	c.Assert(triedSystems, DeepEquals, []string{"1234"})
}

func (s *deviceMgrSystemsSuite) TestDeviceManagerEnsureTriedSystemMissingInModeenvUnhappy(c *C) {
	err := s.bootloader.SetBootVars(map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)
	devicestate.SetBootOkRan(s.mgr, true)

	modeenv := boot.Modeenv{
		Mode: boot.ModeRun,
		// the system is not in CurrentRecoverySystems
		CurrentRecoverySystems: []string{"29112019"},
	}
	err = modeenv.WriteTo("")
	c.Assert(err, IsNil)

	// system is considered successful, bootenv is cleared, the label is
	// recorded in tried-systems
	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})

	var triedSystems []string
	s.state.Lock()
	err = s.state.Get("tried-systems", &triedSystems)
	c.Assert(err, Equals, state.ErrNoState)
	// also logged
	c.Check(s.logbuf.String(), testutil.Contains, `tried recovery system outcome error: recovery system "1234" was tried, but is not present in the modeenv CurrentRecoverySystems`)
	s.state.Unlock()
}

func (s *deviceMgrSystemsSuite) TestDeviceManagerEnsureTriedSystemBad(c *C) {
	// after reboot, the recovery system status is still try
	err := s.bootloader.SetBootVars(map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "try",
	})
	c.Assert(err, IsNil)
	devicestate.SetBootOkRan(s.mgr, true)

	// thus the system is considered bad, bootenv is cleared, and system is
	// not recorded as successful
	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})

	var triedSystems []string
	s.state.Lock()
	err = s.state.Get("tried-systems", &triedSystems)
	c.Assert(err, Equals, state.ErrNoState)
	c.Check(s.logbuf.String(), testutil.Contains, `tried recovery system "1234" failed`)
	s.state.Unlock()

	// procure an inconsistent state, reset and run again
	err = s.bootloader.SetBootVars(map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "try",
	})
	c.Assert(err, IsNil)
	devicestate.SetTriedSystemsRan(s.mgr, false)

	// clear the log buffer
	s.logbuf.Reset()

	err = s.mgr.Ensure()
	c.Assert(err, IsNil)
	s.state.Lock()
	defer s.state.Unlock()
	err = s.state.Get("tried-systems", &triedSystems)
	c.Assert(err, Equals, state.ErrNoState)
	// bootenv got cleared
	m, err = s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
	c.Check(s.logbuf.String(), testutil.Contains, `tried recovery system outcome error: try recovery system is unset but status is "try"`)
	c.Check(s.logbuf.String(), testutil.Contains, `inconsistent outcome of a tried recovery system`)
}

func (s *deviceMgrSystemsSuite) TestDeviceManagerEnsureTriedSystemManyLabels(c *C) {
	err := s.bootloader.SetBootVars(map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "tried",
	})
	c.Assert(err, IsNil)
	devicestate.SetBootOkRan(s.mgr, true)

	s.state.Lock()
	s.state.Set("tried-systems", []string{"0000", "1111"})
	s.state.Unlock()

	modeenv := boot.Modeenv{
		Mode: boot.ModeRun,
		// the system is in CurrentRecoverySystems
		CurrentRecoverySystems: []string{"29112019", "1234"},
	}
	err = modeenv.WriteTo("")
	c.Assert(err, IsNil)

	// successful system label is appended
	err = s.mgr.Ensure()
	c.Assert(err, IsNil)

	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})

	s.state.Lock()
	defer s.state.Unlock()

	var triedSystems []string
	err = s.state.Get("tried-systems", &triedSystems)
	c.Assert(err, IsNil)
	c.Assert(triedSystems, DeepEquals, []string{"0000", "1111", "1234"})

	c.Check(s.logbuf.String(), testutil.Contains, `tried recovery system "1234" was successful`)
}

type deviceMgrSystemsCreateSuite struct {
	deviceMgrSystemsBaseSuite

	bootloader *bootloadertest.MockRecoveryAwareTrustedAssetsBootloader
}

func (s *deviceMgrSystemsCreateSuite) SetUpTest(c *C) {
	s.deviceMgrSystemsBaseSuite.SetUpTest(c)

	s.bootloader = s.deviceMgrSystemsBaseSuite.bootloader.WithRecoveryAwareTrustedAssets()
	bootloader.Force(s.bootloader)
	s.AddCleanup(func() { bootloader.Force(nil) })
}

func (s *deviceMgrSystemsCreateSuite) TestDeviceManagerCreateRecoverySystemTasksAndChange(c *C) {
	devicestate.SetBootOkRan(s.mgr, true)

	s.state.Lock()
	defer s.state.Unlock()
	chg, err := devicestate.CreateRecoverySystem(s.state, "1234")
	c.Assert(err, IsNil)
	c.Assert(chg, NotNil)
	tsks := chg.Tasks()
	c.Check(tsks, HasLen, 2)
	tskCreate := tsks[0]
	tskFinalize := tsks[1]
	c.Check(tskCreate.Summary(), Matches, `Create recovery system with label "1234"`)
	c.Check(tskFinalize.Summary(), Matches, `Finalize recovery system with label "1234"`)
	var systemSetupData map[string]interface{}
	err = tskCreate.Get("recovery-system-setup", &systemSetupData)
	c.Assert(err, IsNil)
	c.Assert(systemSetupData, DeepEquals, map[string]interface{}{
		"label": "1234",
		// not set yet
		"directory":        "",
		"new-common-files": nil,
		"snap-setup-tasks": nil,
	})

	var otherTaskID string
	err = tskFinalize.Get("recovery-system-setup-task", &otherTaskID)
	c.Assert(err, IsNil)
	c.Assert(otherTaskID, Equals, tskCreate.ID())
}

func (s *deviceMgrSystemsCreateSuite) makeSnapInState(c *C, name string, rev snap.Revision) *snap.Info {
	snapID := s.ss.AssertedSnapID(name)
	c.Logf("snap %q ID %q rev %v", name, snapID, rev.String())
	if rev.Unset() || rev.Local() {
		snapID = ""
	}
	si := &snap.SideInfo{
		RealName: name,
		SnapID:   snapID,
		Revision: rev,
	}
	// asserted?
	info := snaptest.MakeSnapFileAndDir(c, snapYamls[name], snapFiles[name], si)
	if !rev.Unset() && !rev.Local() {
		s.setupSnapDecl(c, info, "canonical")
		s.setupSnapRevision(c, info, "canonical", rev)
	}
	c.Logf("setting type: %v", info.Type())
	snapstate.Set(s.state, info.InstanceName(), &snapstate.SnapState{
		SnapType: string(info.Type()),
		Active:   true,
		Sequence: []*snap.SideInfo{si},
		Current:  si.Revision,
	})

	return info
}

func (s *deviceMgrSystemsCreateSuite) mockStandardSnapsModeenvAndBootloaderState(c *C) {
	s.makeSnapInState(c, "pc", snap.R(1))
	s.makeSnapInState(c, "pc-kernel", snap.R(2))
	s.makeSnapInState(c, "core20", snap.R(3))
	s.makeSnapInState(c, "snapd", snap.R(4))

	err := s.bootloader.SetBootVars(map[string]string{
		"snap_kernel": "pc-kernel_2.snap",
		"snap_core":   "core20_3.snap",
	})
	c.Assert(err, IsNil)
	modeenv := boot.Modeenv{
		Mode:                   "run",
		Base:                   "core20_3.snap",
		CurrentKernels:         []string{"pc-kernel_2.snap"},
		CurrentRecoverySystems: []string{"othersystem"},
		GoodRecoverySystems:    []string{"othersystem"},
	}
	err = modeenv.WriteTo("")
	c.Assert(err, IsNil)
}

func (s *deviceMgrSystemsCreateSuite) TestDeviceManagerCreateRecoverySystemHappy(c *C) {
	devicestate.SetBootOkRan(s.mgr, true)

	s.state.Lock()
	chg, err := devicestate.CreateRecoverySystem(s.state, "1234")
	c.Assert(err, IsNil)
	c.Assert(chg, NotNil)
	tsks := chg.Tasks()
	c.Check(tsks, HasLen, 2)
	tskCreate := tsks[0]
	tskFinalize := tsks[1]
	c.Assert(tskCreate.Summary(), Matches, `Create recovery system with label "1234"`)
	c.Check(tskFinalize.Summary(), Matches, `Finalize recovery system with label "1234"`)

	s.mockStandardSnapsModeenvAndBootloaderState(c)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(tskCreate.Status(), Equals, state.DoneStatus)
	c.Assert(tskFinalize.Status(), Equals, state.DoingStatus)

	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})

	validateSeed(c, "1234", s.storeSigning.Trusted)
	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "try",
	})
	modeenvAfterCreate, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterCreate.CurrentRecoverySystems, DeepEquals, []string{"othersystem", "1234"})
	c.Check(modeenvAfterCreate.GoodRecoverySystems, DeepEquals, []string{"othersystem"})

	// these things happen on snapd startup
	state.MockRestarting(s.state, state.RestartUnset)
	s.state.Set("tried-systems", []string{"1234"})
	s.bootloader.SetBootVars(map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
	s.bootloader.SetBootVarsCalls = 0

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Err(), IsNil)
	c.Check(chg.IsReady(), Equals, true)
	c.Assert(tskCreate.Status(), Equals, state.DoneStatus)
	c.Assert(tskFinalize.Status(), Equals, state.DoneStatus)

	var triedSystemsAfterFinalize []string
	err = s.state.Get("tried-systems", &triedSystemsAfterFinalize)
	c.Assert(err, Equals, state.ErrNoState)

	modeenvAfterFinalize, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterFinalize.CurrentRecoverySystems, DeepEquals, []string{"othersystem", "1234"})
	c.Check(modeenvAfterFinalize.GoodRecoverySystems, DeepEquals, []string{"othersystem", "1234"})
	// no more calls to the bootloader past creating the system
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
}

func (s *deviceMgrSystemsCreateSuite) TestDeviceManagerCreateRecoverySystemUndo(c *C) {
	devicestate.SetBootOkRan(s.mgr, true)

	s.state.Lock()
	chg, err := devicestate.CreateRecoverySystem(s.state, "1234undo")
	c.Assert(err, IsNil)
	c.Assert(chg, NotNil)
	tsks := chg.Tasks()
	c.Check(tsks, HasLen, 2)
	tskCreate := tsks[0]
	tskFinalize := tsks[1]
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(tskFinalize)
	chg.AddTask(terr)

	s.mockStandardSnapsModeenvAndBootloaderState(c)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(tskCreate.Status(), Equals, state.DoneStatus)
	c.Assert(tskFinalize.Status(), Equals, state.DoingStatus)

	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})
	validateSeed(c, "1234undo", s.storeSigning.Trusted)
	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "1234undo",
		"recovery_system_status": "try",
	})
	modeenvAfterCreate, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterCreate.CurrentRecoverySystems, DeepEquals, []string{"othersystem", "1234undo"})
	c.Check(modeenvAfterCreate.GoodRecoverySystems, DeepEquals, []string{"othersystem"})

	// these things happen on snapd startup
	state.MockRestarting(s.state, state.RestartUnset)
	s.state.Set("tried-systems", []string{"1234undo"})
	s.bootloader.SetBootVars(map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
	s.bootloader.SetBootVarsCalls = 0

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Err(), ErrorMatches, "(?s)cannot perform the following tasks.* provoking total undo.*")
	c.Check(chg.IsReady(), Equals, true)
	c.Assert(tskCreate.Status(), Equals, state.UndoneStatus)
	c.Assert(tskFinalize.Status(), Equals, state.UndoneStatus)

	var triedSystemsAfter []string
	err = s.state.Get("tried-systems", &triedSystemsAfter)
	c.Assert(err, Equals, state.ErrNoState)

	modeenvAfterFinalize, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterFinalize.CurrentRecoverySystems, DeepEquals, []string{"othersystem"})
	c.Check(modeenvAfterFinalize.GoodRecoverySystems, DeepEquals, []string{"othersystem"})
	// no more calls to the bootloader
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
}

func (s *deviceMgrSystemsCreateSuite) TestDeviceManagerCreateRecoverySystemFinalizeErrsWhenSystemFailed(c *C) {
	devicestate.SetBootOkRan(s.mgr, true)

	s.state.Lock()
	chg, err := devicestate.CreateRecoverySystem(s.state, "1234")
	c.Assert(err, IsNil)
	c.Assert(chg, NotNil)
	tsks := chg.Tasks()
	c.Check(tsks, HasLen, 2)
	tskCreate := tsks[0]
	tskFinalize := tsks[1]
	terr := s.state.NewTask("error-trigger", "provoking total undo")
	terr.WaitFor(tskFinalize)
	chg.AddTask(terr)

	s.mockStandardSnapsModeenvAndBootloaderState(c)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(tskCreate.Status(), Equals, state.DoneStatus)
	c.Assert(tskFinalize.Status(), Equals, state.DoingStatus)

	c.Check(s.restartRequests, DeepEquals, []state.RestartType{state.RestartSystemNow})

	validateSeed(c, "1234", s.storeSigning.Trusted)
	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "try",
	})
	modeenvAfterCreate, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterCreate.CurrentRecoverySystems, DeepEquals, []string{"othersystem", "1234"})
	c.Check(modeenvAfterCreate.GoodRecoverySystems, DeepEquals, []string{"othersystem"})

	// these things happen on snapd startup
	state.MockRestarting(s.state, state.RestartUnset)
	// after reboot the relevant startup code identified that the tried
	// system failed to operate properly
	s.state.Set("tried-systems", []string{})
	s.bootloader.SetBootVars(map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
	s.bootloader.SetBootVarsCalls = 0

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Err(), ErrorMatches, `(?s)cannot perform the following tasks.* Finalize recovery system with label "1234" \(tried recovery system "1234" failed\)`)
	c.Check(chg.IsReady(), Equals, true)
	c.Assert(tskCreate.Status(), Equals, state.UndoneStatus)
	c.Assert(tskFinalize.Status(), Equals, state.ErrorStatus)

	var triedSystemsAfter []string
	err = s.state.Get("tried-systems", &triedSystemsAfter)
	c.Assert(err, IsNil)
	c.Assert(triedSystemsAfter, HasLen, 0)

	modeenvAfterFinalize, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterFinalize.CurrentRecoverySystems, DeepEquals, []string{"othersystem"})
	c.Check(modeenvAfterFinalize.GoodRecoverySystems, DeepEquals, []string{"othersystem"})
	// no more calls to the bootloader
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
}
