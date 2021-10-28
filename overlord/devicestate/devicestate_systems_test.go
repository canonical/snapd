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
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

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
	"github.com/snapcore/snapd/overlord/restart"
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
	model             *asserts.Model
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

	s.model = s.makeModelAssertionInState(c, "canonical", "pc-20", map[string]interface{}{
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

		Model:          s.model.Model(),
		BrandID:        s.model.BrandID(),
		Grade:          string(s.model.Grade()),
		ModelSignKeyID: s.model.SignKeyID(),
	}
	err := modeenv.WriteTo("")
	s.state.Set("seeded", true)

	c.Assert(err, IsNil)

	logbuf, restore := logger.MockLogger()
	s.logbuf = logbuf
	s.AddCleanup(restore)

	nopHandler := func(task *state.Task, _ *tomb.Tomb) error { return nil }
	s.o.TaskRunner().AddHandler("fake-download", nopHandler, nil)
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

func (s *deviceMgrSystemsSuite) TestListSeedSystemsCurrentSingleSeeded(c *C) {
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

func (s *deviceMgrSystemsSuite) TestListSeedSystemsCurrentManySeeded(c *C) {
	// during a remodel, a new seeded system is prepended to the list
	s.state.Lock()
	s.state.Set("seeded-systems", []devicestate.SeededSystem{
		{
			System:  s.mockedSystemSeeds[2].label,
			Model:   s.mockedSystemSeeds[2].model.Model(),
			BrandID: s.mockedSystemSeeds[2].brand.AccountID(),
		},
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
		// this seed was used to install the system in the past
		Current: false,
		Label:   s.mockedSystemSeeds[1].label,
		Model:   s.mockedSystemSeeds[1].model,
		Brand:   s.mockedSystemSeeds[1].brand,
		Actions: defaultSystemActions,
	}, {
		// this seed was seeded most recently
		Current: true,
		Label:   s.mockedSystemSeeds[2].label,
		Model:   s.mockedSystemSeeds[2].model,
		Brand:   s.mockedSystemSeeds[2].brand,
		Actions: currentSystemActions,
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
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
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
		c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
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
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
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
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
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
		c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
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
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
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
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
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

func (s *deviceMgrSystemsSuite) TestRecordSeededSystem(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	now := time.Now()
	modelTs := now.AddDate(-1, 0, 0)

	sys := devicestate.SeededSystem{
		System: "1234",

		Model:     "my-model",
		BrandID:   "my-brand",
		Revision:  1,
		Timestamp: modelTs,

		SeedTime: now,
	}
	err := devicestate.RecordSeededSystem(s.mgr, s.state, &sys)
	c.Assert(err, IsNil)

	expectedSeededOneSys := []map[string]interface{}{
		{
			"system":    "1234",
			"model":     "my-model",
			"brand-id":  "my-brand",
			"revision":  float64(1),
			"timestamp": modelTs.Format(time.RFC3339Nano),
			"seed-time": now.Format(time.RFC3339Nano),
		},
	}
	var seededSystemsFromState []map[string]interface{}
	err = s.state.Get("seeded-systems", &seededSystemsFromState)
	c.Assert(err, IsNil)
	c.Assert(seededSystemsFromState, DeepEquals, expectedSeededOneSys)
	// adding the system again does nothing
	err = devicestate.RecordSeededSystem(s.mgr, s.state, &sys)
	c.Assert(err, IsNil)
	err = s.state.Get("seeded-systems", &seededSystemsFromState)
	c.Assert(err, IsNil)
	c.Assert(seededSystemsFromState, DeepEquals, expectedSeededOneSys)
	// adding the system again, even with changed seed time, still does nothing
	sysWithNewSeedTime := sys
	sysWithNewSeedTime.SeedTime = now.Add(time.Hour)
	err = devicestate.RecordSeededSystem(s.mgr, s.state, &sysWithNewSeedTime)
	c.Assert(err, IsNil)
	err = s.state.Get("seeded-systems", &seededSystemsFromState)
	c.Assert(err, IsNil)
	c.Assert(seededSystemsFromState, DeepEquals, expectedSeededOneSys)

	rev3Ts := modelTs.AddDate(0, 1, 0)
	// most common case, a new revision and timestamp
	sysRev3 := sys
	sysRev3.Revision = 3
	sysRev3.Timestamp = rev3Ts

	err = devicestate.RecordSeededSystem(s.mgr, s.state, &sysRev3)
	c.Assert(err, IsNil)
	err = s.state.Get("seeded-systems", &seededSystemsFromState)
	c.Assert(err, IsNil)
	expectedWithNewRev := []map[string]interface{}{
		{
			// new entry is added at the beginning
			"system":    "1234",
			"model":     "my-model",
			"brand-id":  "my-brand",
			"revision":  float64(3),
			"timestamp": rev3Ts.Format(time.RFC3339Nano),
			"seed-time": now.Format(time.RFC3339Nano),
		}, {
			"system":    "1234",
			"model":     "my-model",
			"brand-id":  "my-brand",
			"revision":  float64(1),
			"timestamp": modelTs.Format(time.RFC3339Nano),
			"seed-time": now.Format(time.RFC3339Nano),
		},
	}
	c.Assert(seededSystemsFromState, DeepEquals, expectedWithNewRev)
	// trying to add again does nothing
	err = devicestate.RecordSeededSystem(s.mgr, s.state, &sysRev3)
	c.Assert(err, IsNil)
	err = s.state.Get("seeded-systems", &seededSystemsFromState)
	c.Assert(err, IsNil)
	c.Assert(seededSystemsFromState, DeepEquals, expectedWithNewRev)

	modelNewTs := modelTs
	// and a case of new model
	sysNew := devicestate.SeededSystem{
		System: "9999",

		Model:     "my-new-model",
		BrandID:   "my-new-brand",
		Revision:  1,
		Timestamp: modelNewTs,

		SeedTime: now,
	}
	err = devicestate.RecordSeededSystem(s.mgr, s.state, &sysNew)
	c.Assert(err, IsNil)
	err = s.state.Get("seeded-systems", &seededSystemsFromState)
	c.Assert(err, IsNil)
	expectedWithNewModel := []map[string]interface{}{
		{
			// and another one got added at the beginning
			"system":    "9999",
			"model":     "my-new-model",
			"brand-id":  "my-new-brand",
			"revision":  float64(1),
			"timestamp": modelNewTs.Format(time.RFC3339Nano),
			"seed-time": now.Format(time.RFC3339Nano),
		}, {
			"system":    "1234",
			"model":     "my-model",
			"brand-id":  "my-brand",
			"revision":  float64(3),
			"timestamp": rev3Ts.Format(time.RFC3339Nano),
			"seed-time": now.Format(time.RFC3339Nano),
		}, {
			"system":    "1234",
			"model":     "my-model",
			"brand-id":  "my-brand",
			"revision":  float64(1),
			"timestamp": modelTs.Format(time.RFC3339Nano),
			"seed-time": now.Format(time.RFC3339Nano),
		},
	}
	c.Assert(seededSystemsFromState, DeepEquals, expectedWithNewModel)
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
		"label":            "1234",
		"directory":        filepath.Join(boot.InitramfsUbuntuSeedDir, "systems/1234"),
		"snap-setup-tasks": nil,
	})

	var otherTaskID string
	err = tskFinalize.Get("recovery-system-setup-task", &otherTaskID)
	c.Assert(err, IsNil)
	c.Assert(otherTaskID, Equals, tskCreate.ID())
}

func (s *deviceMgrSystemsCreateSuite) TestDeviceManagerCreateRecoverySystemTasksWhenDirExists(c *C) {
	devicestate.SetBootOkRan(s.mgr, true)

	c.Assert(os.MkdirAll(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems/1234"), 0755), IsNil)

	s.state.Lock()
	defer s.state.Unlock()
	chg, err := devicestate.CreateRecoverySystem(s.state, "1234")
	c.Assert(err, ErrorMatches, `recovery system "1234" already exists`)
	c.Check(chg, IsNil)
}

func (s *deviceMgrSystemsCreateSuite) TestDeviceManagerCreateRecoverySystemNotSeeded(c *C) {
	devicestate.SetBootOkRan(s.mgr, true)

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", nil)

	chg, err := devicestate.CreateRecoverySystem(s.state, "1234")
	c.Assert(err, ErrorMatches, `cannot create new recovery systems until fully seeded`)
	c.Check(chg, IsNil)
}

func (s *deviceMgrSystemsCreateSuite) makeSnapInState(c *C, name string, rev snap.Revision) *snap.Info {
	snapID := s.ss.AssertedSnapID(name)
	if rev.Unset() || rev.Local() {
		snapID = ""
	}
	si := &snap.SideInfo{
		RealName: name,
		SnapID:   snapID,
		Revision: rev,
	}
	info := snaptest.MakeSnapFileAndDir(c, snapYamls[name], snapFiles[name], si)
	// asserted?
	if !rev.Unset() && !rev.Local() {
		s.setupSnapDecl(c, info, "canonical")
		s.setupSnapRevision(c, info, "canonical", rev)
	}
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

		Model:          s.model.Model(),
		BrandID:        s.model.BrandID(),
		Grade:          string(s.model.Grade()),
		ModelSignKeyID: s.model.SignKeyID(),
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
	// a reboot is expected
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})

	validateCore20Seed(c, "1234", s.model, s.storeSigning.Trusted)
	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "try",
	})
	modeenvAfterCreate, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterCreate, testutil.JsonEquals, boot.Modeenv{
		Mode:                   "run",
		Base:                   "core20_3.snap",
		CurrentKernels:         []string{"pc-kernel_2.snap"},
		CurrentRecoverySystems: []string{"othersystem", "1234"},
		GoodRecoverySystems:    []string{"othersystem"},

		Model:          s.model.Model(),
		BrandID:        s.model.BrandID(),
		Grade:          string(s.model.Grade()),
		ModelSignKeyID: s.model.SignKeyID(),
		// try model is unset as its measured properties are identical
		// to current
	})
	// verify that new files are tracked correctly
	expectedFilesLog := &bytes.Buffer{}
	// new snap files are logged in this order
	for _, fname := range []string{"snapd_4.snap", "pc-kernel_2.snap", "core20_3.snap", "pc_1.snap"} {
		fmt.Fprintln(expectedFilesLog, filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps", fname))
	}
	c.Check(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", "1234", "snapd-new-file-log"),
		testutil.FileEquals, expectedFilesLog.String())

	// these things happen on snapd startup
	restart.MockPending(s.state, restart.RestartUnset)
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
	c.Check(modeenvAfterFinalize, testutil.JsonEquals, boot.Modeenv{
		Mode:                   "run",
		Base:                   "core20_3.snap",
		CurrentKernels:         []string{"pc-kernel_2.snap"},
		CurrentRecoverySystems: []string{"othersystem", "1234"},
		GoodRecoverySystems:    []string{"othersystem", "1234"},

		Model:          s.model.Model(),
		BrandID:        s.model.BrandID(),
		Grade:          string(s.model.Grade()),
		ModelSignKeyID: s.model.SignKeyID(),
	})
	// no more calls to the bootloader past creating the system
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	c.Check(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", "1234", "snapd-new-file-log"), testutil.FileAbsent)
}

func (s *deviceMgrSystemsCreateSuite) TestDeviceManagerCreateRecoverySystemRemodelDownloadingSnapsHappy(c *C) {
	devicestate.SetBootOkRan(s.mgr, true)

	fooSnap := snaptest.MakeTestSnapWithFiles(c, "name: foo\nversion: 1.0\nbase: core20", nil)
	barSnap := snaptest.MakeTestSnapWithFiles(c, "name: bar\nversion: 1.0\nbase: core20", nil)
	s.state.Lock()
	// fake downloads are a nop
	tSnapsup1 := s.state.NewTask("fake-download", "dummy task carrying snap setup")
	tSnapsup2 := s.state.NewTask("fake-download", "dummy task carrying snap setup")
	// both snaps are asserted
	snapsupFoo := snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "foo", SnapID: s.ss.AssertedSnapID("foo"), Revision: snap.R(99)},
		SnapPath: fooSnap,
	}
	s.setupSnapDeclForNameAndID(c, "foo", s.ss.AssertedSnapID("foo"), "canonical")
	s.setupSnapRevisionForFileAndID(c, fooSnap, s.ss.AssertedSnapID("foo"), "canonical", snap.R(99))
	snapsupBar := snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "bar", SnapID: s.ss.AssertedSnapID("bar"), Revision: snap.R(100)},
		SnapPath: barSnap,
	}
	s.setupSnapDeclForNameAndID(c, "bar", s.ss.AssertedSnapID("bar"), "canonical")
	s.setupSnapRevisionForFileAndID(c, barSnap, s.ss.AssertedSnapID("bar"), "canonical", snap.R(100))
	// when download completes, the files will be at /var/lib/snapd/snap
	c.Assert(os.MkdirAll(filepath.Dir(snapsupFoo.MountFile()), 0755), IsNil)
	c.Assert(os.Rename(fooSnap, snapsupFoo.MountFile()), IsNil)
	c.Assert(os.MkdirAll(filepath.Dir(snapsupBar.MountFile()), 0755), IsNil)
	c.Assert(os.Rename(barSnap, snapsupBar.MountFile()), IsNil)
	tSnapsup1.Set("snap-setup", snapsupFoo)
	tSnapsup2.Set("snap-setup", snapsupBar)

	tss, err := devicestate.CreateRecoverySystemTasks(s.state, "1234", []string{tSnapsup1.ID(), tSnapsup2.ID()})
	c.Assert(err, IsNil)
	tsks := tss.Tasks()
	c.Check(tsks, HasLen, 2)
	tskCreate := tsks[0]
	tskFinalize := tsks[1]
	c.Assert(tskCreate.Summary(), Matches, `Create recovery system with label "1234"`)
	c.Check(tskFinalize.Summary(), Matches, `Finalize recovery system with label "1234"`)
	var systemSetupData map[string]interface{}
	err = tskCreate.Get("recovery-system-setup", &systemSetupData)
	c.Assert(err, IsNil)
	c.Assert(systemSetupData, DeepEquals, map[string]interface{}{
		"label":            "1234",
		"directory":        filepath.Join(boot.InitramfsUbuntuSeedDir, "systems/1234"),
		"snap-setup-tasks": []interface{}{tSnapsup1.ID(), tSnapsup2.ID()},
	})
	tss.WaitFor(tSnapsup1)
	tss.WaitFor(tSnapsup2)
	// add the dummy tasks to the change
	chg := s.state.NewChange("create-recovery-system", "create recovery system")
	chg.AddTask(tSnapsup1)
	chg.AddTask(tSnapsup2)
	chg.AddAll(tss)

	// downloads are only accepted if the tasks are executed as part of
	// remodel, so procure a new model
	newModel := s.brands.Model("canonical", "pc-20", map[string]interface{}{
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
				"name":     "foo",
				"id":       s.ss.AssertedSnapID("foo"),
				"presence": "required",
			},
			map[string]interface{}{
				"name":     "bar",
				"presence": "required",
			},
		},
		"revision": "2",
	})
	chg.Set("new-model", string(asserts.Encode(newModel)))

	s.mockStandardSnapsModeenvAndBootloaderState(c)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(tskCreate.Status(), Equals, state.DoneStatus)
	c.Assert(tskFinalize.Status(), Equals, state.DoingStatus)
	// a reboot is expected
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})

	validateCore20Seed(c, "1234", newModel, s.storeSigning.Trusted, "foo", "bar")
	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "try",
	})
	modeenvAfterCreate, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterCreate, testutil.JsonEquals, boot.Modeenv{
		Mode:                   "run",
		Base:                   "core20_3.snap",
		CurrentKernels:         []string{"pc-kernel_2.snap"},
		CurrentRecoverySystems: []string{"othersystem", "1234"},
		GoodRecoverySystems:    []string{"othersystem"},

		Model:          s.model.Model(),
		BrandID:        s.model.BrandID(),
		Grade:          string(s.model.Grade()),
		ModelSignKeyID: s.model.SignKeyID(),
		// try model is unset as its measured properties are identical
	})
	// verify that new files are tracked correctly
	expectedFilesLog := &bytes.Buffer{}
	// new snap files are logged in this order
	for _, fname := range []string{
		"snapd_4.snap", "pc-kernel_2.snap", "core20_3.snap", "pc_1.snap",
		"foo_99.snap", "bar_100.snap",
	} {
		fmt.Fprintln(expectedFilesLog, filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps", fname))
	}
	c.Check(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", "1234", "snapd-new-file-log"),
		testutil.FileEquals, expectedFilesLog.String())

	// these things happen on snapd startup
	restart.MockPending(s.state, restart.RestartUnset)
	s.state.Set("tried-systems", []string{"1234"})
	s.bootloader.SetBootVars(map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
	s.bootloader.SetBootVarsCalls = 0

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Check(chg.IsReady(), Equals, true)
	c.Assert(tskCreate.Status(), Equals, state.DoneStatus)
	c.Assert(tskFinalize.Status(), Equals, state.DoneStatus)

	// this would be part of a remodel so some state is cleaned up only at the end of remodel change
	var triedSystemsAfterFinalize []string
	err = s.state.Get("tried-systems", &triedSystemsAfterFinalize)
	c.Assert(err, IsNil)
	c.Check(triedSystemsAfterFinalize, DeepEquals, []string{"1234"})

	modeenvAfterFinalize, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterFinalize, testutil.JsonEquals, boot.Modeenv{
		Mode:           "run",
		Base:           "core20_3.snap",
		CurrentKernels: []string{"pc-kernel_2.snap"},
		// the system is kept in the current list
		CurrentRecoverySystems: []string{"othersystem", "1234"},
		// but not promoted to good systems yet
		GoodRecoverySystems: []string{"othersystem"},

		Model:          s.model.Model(),
		BrandID:        s.model.BrandID(),
		Grade:          string(s.model.Grade()),
		ModelSignKeyID: s.model.SignKeyID(),
	})
	// no more calls to the bootloader past creating the system
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	c.Check(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems", "1234", "snapd-new-file-log"), testutil.FileAbsent)
}

func (s *deviceMgrSystemsCreateSuite) TestDeviceManagerCreateRecoverySystemRemodelDownloadingMissingSnap(c *C) {
	devicestate.SetBootOkRan(s.mgr, true)

	fooSnap := snaptest.MakeTestSnapWithFiles(c, "name: foo\nversion: 1.0\nbase: core20", nil)
	s.state.Lock()
	defer s.state.Unlock()
	// fake downloads are a nop
	tSnapsup1 := s.state.NewTask("fake-download", "dummy task carrying snap setup")
	// both snaps are asserted
	snapsupFoo := snapstate.SnapSetup{
		SideInfo: &snap.SideInfo{RealName: "foo", SnapID: s.ss.AssertedSnapID("foo"), Revision: snap.R(99)},
		SnapPath: fooSnap,
	}
	tSnapsup1.Set("snap-setup", snapsupFoo)

	tss, err := devicestate.CreateRecoverySystemTasks(s.state, "1234missingdownload", []string{tSnapsup1.ID()})
	c.Assert(err, IsNil)
	tsks := tss.Tasks()
	c.Check(tsks, HasLen, 2)
	tskCreate := tsks[0]
	tskFinalize := tsks[1]
	c.Assert(tskCreate.Summary(), Matches, `Create recovery system with label "1234missingdownload"`)
	c.Check(tskFinalize.Summary(), Matches, `Finalize recovery system with label "1234missingdownload"`)
	var systemSetupData map[string]interface{}
	err = tskCreate.Get("recovery-system-setup", &systemSetupData)
	c.Assert(err, IsNil)
	c.Assert(systemSetupData, DeepEquals, map[string]interface{}{
		"label":            "1234missingdownload",
		"directory":        filepath.Join(boot.InitramfsUbuntuSeedDir, "systems/1234missingdownload"),
		"snap-setup-tasks": []interface{}{tSnapsup1.ID()},
	})
	tss.WaitFor(tSnapsup1)
	// add the dummy task to the change
	chg := s.state.NewChange("create-recovery-system", "create recovery system")
	chg.AddTask(tSnapsup1)
	chg.AddAll(tss)

	// downloads are only accepted if the tasks are executed as part of
	// remodel, so procure a new model
	newModel := s.brands.Model("canonical", "pc-20", map[string]interface{}{
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
			// we have a download task for snap foo, but not for bar
			map[string]interface{}{
				"name":     "bar",
				"presence": "required",
			},
		},
		"revision": "2",
	})
	chg.Set("new-model", string(asserts.Encode(newModel)))

	s.mockStandardSnapsModeenvAndBootloaderState(c)

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), ErrorMatches, `(?s).*cannot create a recovery system.*internal error: non-essential but required snap "bar" not present.`)
	c.Assert(tskCreate.Status(), Equals, state.ErrorStatus)
	c.Assert(tskFinalize.Status(), Equals, state.HoldStatus)
	// a reboot is expected
	c.Check(s.restartRequests, HasLen, 0)
	// single bootloader call to clear any recovery system variables
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 1)
	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
	// system directory was removed
	c.Check(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems/1234missingdownload"), testutil.FileAbsent)
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

	snaptest.PopulateDir(filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps"), [][]string{
		{"core20_10.snap", "canary"},
		{"some-snap_1.snap", "canary"},
	})

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	c.Assert(chg.Err(), IsNil)
	c.Assert(tskCreate.Status(), Equals, state.DoneStatus)
	c.Assert(tskFinalize.Status(), Equals, state.DoingStatus)
	// a reboot is expected
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
	// sanity check asserted snaps location
	c.Check(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems/1234undo"), testutil.FilePresent)
	p, err := filepath.Glob(filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/*"))
	c.Assert(err, IsNil)
	c.Check(p, DeepEquals, []string{
		filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/core20_10.snap"),
		filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/core20_3.snap"),
		filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/pc-kernel_2.snap"),
		filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/pc_1.snap"),
		filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/snapd_4.snap"),
		filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/some-snap_1.snap"),
	})
	// do more extensive validation
	validateCore20Seed(c, "1234undo", s.model, s.storeSigning.Trusted)
	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "1234undo",
		"recovery_system_status": "try",
	})
	modeenvAfterCreate, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterCreate, testutil.JsonEquals, boot.Modeenv{
		Mode:                   "run",
		Base:                   "core20_3.snap",
		CurrentKernels:         []string{"pc-kernel_2.snap"},
		CurrentRecoverySystems: []string{"othersystem", "1234undo"},
		GoodRecoverySystems:    []string{"othersystem"},

		Model:          s.model.Model(),
		BrandID:        s.model.BrandID(),
		Grade:          string(s.model.Grade()),
		ModelSignKeyID: s.model.SignKeyID(),
	})

	// these things happen on snapd startup
	restart.MockPending(s.state, restart.RestartUnset)
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
	c.Check(modeenvAfterFinalize, testutil.JsonEquals, boot.Modeenv{
		Mode:                   "run",
		Base:                   "core20_3.snap",
		CurrentKernels:         []string{"pc-kernel_2.snap"},
		CurrentRecoverySystems: []string{"othersystem"},
		GoodRecoverySystems:    []string{"othersystem"},

		Model:          s.model.Model(),
		BrandID:        s.model.BrandID(),
		Grade:          string(s.model.Grade()),
		ModelSignKeyID: s.model.SignKeyID(),
	})
	// no more calls to the bootloader
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	// system directory was removed
	c.Check(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems/1234undo"), testutil.FileAbsent)
	// only the canary files are left now
	p, err = filepath.Glob(filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/*"))
	c.Assert(err, IsNil)
	c.Check(p, DeepEquals, []string{
		filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/core20_10.snap"),
		filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/some-snap_1.snap"),
	})
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
	// a reboot is expected
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})

	validateCore20Seed(c, "1234", s.model, s.storeSigning.Trusted)
	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "1234",
		"recovery_system_status": "try",
	})
	modeenvAfterCreate, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterCreate, testutil.JsonEquals, boot.Modeenv{
		Mode:                   "run",
		Base:                   "core20_3.snap",
		CurrentKernels:         []string{"pc-kernel_2.snap"},
		CurrentRecoverySystems: []string{"othersystem", "1234"},
		GoodRecoverySystems:    []string{"othersystem"},

		Model:          s.model.Model(),
		BrandID:        s.model.BrandID(),
		Grade:          string(s.model.Grade()),
		ModelSignKeyID: s.model.SignKeyID(),
	})

	// these things happen on snapd startup
	restart.MockPending(s.state, restart.RestartUnset)
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

	c.Assert(chg.Err(), ErrorMatches, `(?s)cannot perform the following tasks.* Finalize recovery system with label "1234" \(cannot promote recovery system "1234": system has not been successfully tried\)`)
	c.Check(chg.IsReady(), Equals, true)
	c.Assert(tskCreate.Status(), Equals, state.UndoneStatus)
	c.Assert(tskFinalize.Status(), Equals, state.ErrorStatus)

	var triedSystemsAfter []string
	err = s.state.Get("tried-systems", &triedSystemsAfter)
	c.Assert(err, IsNil)
	c.Assert(triedSystemsAfter, HasLen, 0)

	modeenvAfterFinalize, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterFinalize, testutil.JsonEquals, boot.Modeenv{
		Mode:                   "run",
		Base:                   "core20_3.snap",
		CurrentKernels:         []string{"pc-kernel_2.snap"},
		CurrentRecoverySystems: []string{"othersystem"},
		GoodRecoverySystems:    []string{"othersystem"},

		Model:          s.model.Model(),
		BrandID:        s.model.BrandID(),
		Grade:          string(s.model.Grade()),
		ModelSignKeyID: s.model.SignKeyID(),
	})
	// no more calls to the bootloader
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 0)
	// seed directory was removed
	c.Check(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems/1234"), testutil.FileAbsent)
	// all common snaps were cleaned up
	p, err := filepath.Glob(filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/*"))
	c.Assert(err, IsNil)
	c.Check(p, HasLen, 0)
}

func (s *deviceMgrSystemsCreateSuite) TestDeviceManagerCreateRecoverySystemErrCleanup(c *C) {
	devicestate.SetBootOkRan(s.mgr, true)

	s.state.Lock()
	chg, err := devicestate.CreateRecoverySystem(s.state, "1234error")
	c.Assert(err, IsNil)
	c.Assert(chg, NotNil)
	tsks := chg.Tasks()
	c.Check(tsks, HasLen, 2)
	tskCreate := tsks[0]
	tskFinalize := tsks[1]

	s.mockStandardSnapsModeenvAndBootloaderState(c)
	s.bootloader.SetBootVarsCalls = 0

	s.bootloader.SetErrFunc = func() error {
		c.Logf("boot calls: %v", s.bootloader.SetBootVarsCalls)
		// for simplicity error out only when we try to set the recovery
		// system variables in bootenv (and not in the cleanup path)
		if s.bootloader.SetBootVarsCalls == 1 {
			return fmt.Errorf("mock bootloader error")
		}
		return nil
	}

	snaptest.PopulateDir(filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps"), [][]string{
		{"core20_10.snap", "canary"},
		{"some-snap_1.snap", "canary"},
	})

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Err(), ErrorMatches, `(?s)cannot perform the following tasks.* \(cannot attempt booting into recovery system "1234error": mock bootloader error\)`)
	c.Check(chg.IsReady(), Equals, true)
	c.Assert(tskCreate.Status(), Equals, state.ErrorStatus)
	c.Assert(tskFinalize.Status(), Equals, state.HoldStatus)

	c.Check(s.restartRequests, HasLen, 0)
	// sanity check asserted snaps location
	c.Check(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems/1234error"), testutil.FileAbsent)
	p, err := filepath.Glob(filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/*"))
	c.Assert(err, IsNil)
	c.Check(p, DeepEquals, []string{
		filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/core20_10.snap"),
		filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/some-snap_1.snap"),
	})
	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
	modeenvAfterCreate, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterCreate, testutil.JsonEquals, boot.Modeenv{
		Mode:                   "run",
		Base:                   "core20_3.snap",
		CurrentKernels:         []string{"pc-kernel_2.snap"},
		CurrentRecoverySystems: []string{"othersystem"},
		GoodRecoverySystems:    []string{"othersystem"},

		Model:          s.model.Model(),
		BrandID:        s.model.BrandID(),
		Grade:          string(s.model.Grade()),
		ModelSignKeyID: s.model.SignKeyID(),
	})
}

func (s *deviceMgrSystemsCreateSuite) TestDeviceManagerCreateRecoverySystemReboot(c *C) {
	devicestate.SetBootOkRan(s.mgr, true)

	s.state.Lock()
	chg, err := devicestate.CreateRecoverySystem(s.state, "1234reboot")
	c.Assert(err, IsNil)
	c.Assert(chg, NotNil)
	tsks := chg.Tasks()
	c.Check(tsks, HasLen, 2)
	tskCreate := tsks[0]
	tskFinalize := tsks[1]

	s.mockStandardSnapsModeenvAndBootloaderState(c)
	s.bootloader.SetBootVarsCalls = 0

	setBootVarsOk := true
	s.bootloader.SetErrFunc = func() error {
		c.Logf("boot calls: %v", s.bootloader.SetBootVarsCalls)
		if setBootVarsOk {
			return nil
		}
		return fmt.Errorf("unexpected call")
	}

	snaptest.PopulateDir(filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps"), [][]string{
		{"core20_10.snap", "canary"},
		{"some-snap_1.snap", "canary"},
	})

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()

	// so far so good
	c.Assert(chg.Err(), IsNil)
	c.Assert(tskCreate.Status(), Equals, state.DoneStatus)
	c.Assert(tskFinalize.Status(), Equals, state.DoingStatus)
	// a reboot is expected
	c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
	c.Check(s.bootloader.SetBootVarsCalls, Equals, 2)
	s.restartRequests = nil

	c.Check(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems/1234reboot"), testutil.FilePresent)
	// since we can't inject a panic into the task and recover from it in
	// the tests, reset the task states to as state which we would have if
	// the system unexpectedly reboots before the task is marked as done
	tskCreate.SetStatus(state.DoStatus)
	tskFinalize.SetStatus(state.DoStatus)
	restart.MockPending(s.state, restart.RestartUnset)
	// we may have rebooted just before the task was marked as done, in
	// which case tried systems would be populated
	s.state.Set("tried-systems", []string{"1234undo"})
	s.bootloader.SetBootVars(map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
	setBootVarsOk = false

	s.state.Unlock()
	s.settle(c)
	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.Err(), ErrorMatches, `(?s)cannot perform the following tasks.* \(cannot create a recovery system with label "1234reboot" for pc-20: system "1234reboot" already exists\)`)
	c.Assert(tskCreate.Status(), Equals, state.ErrorStatus)
	c.Assert(tskFinalize.Status(), Equals, state.HoldStatus)
	c.Check(s.restartRequests, HasLen, 0)

	// recovery system was removed
	c.Check(filepath.Join(boot.InitramfsUbuntuSeedDir, "systems/1234reboot"), testutil.FileAbsent)
	// and so were the new snaps
	p, err := filepath.Glob(filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/*"))
	c.Assert(err, IsNil)
	c.Check(p, DeepEquals, []string{
		filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/core20_10.snap"),
		filepath.Join(boot.InitramfsUbuntuSeedDir, "snaps/some-snap_1.snap"),
	})
	m, err := s.bootloader.GetBootVars("try_recovery_system", "recovery_system_status")
	c.Assert(err, IsNil)
	c.Check(m, DeepEquals, map[string]string{
		"try_recovery_system":    "",
		"recovery_system_status": "",
	})
	modeenvAfterCreate, err := boot.ReadModeenv("")
	c.Assert(err, IsNil)
	c.Check(modeenvAfterCreate, testutil.JsonEquals, boot.Modeenv{
		Mode:                   "run",
		Base:                   "core20_3.snap",
		CurrentKernels:         []string{"pc-kernel_2.snap"},
		CurrentRecoverySystems: []string{"othersystem"},
		GoodRecoverySystems:    []string{"othersystem"},

		Model:          s.model.Model(),
		BrandID:        s.model.BrandID(),
		Grade:          string(s.model.Grade()),
		ModelSignKeyID: s.model.SignKeyID(),
	})
	var triedSystems []string
	s.state.Get("tried-systems", &triedSystems)
	c.Check(triedSystems, HasLen, 0)
}

type systemSnapTrackingSuite struct {
	deviceMgrSystemsBaseSuite
}

var _ = Suite(&systemSnapTrackingSuite{})

func (s *systemSnapTrackingSuite) TestSnapFileTracking(c *C) {
	otherDir := c.MkDir()
	systemDir := filepath.Join(boot.InitramfsUbuntuSeedDir, "systems/1234")
	flog := filepath.Join(otherDir, "files-log")

	snaptest.PopulateDir(systemDir, [][]string{
		{"this-will-be-removed", "canary"},
		{"this-one-too", "canary"},
		{"this-one-stays", "canary"},
		{"snaps/to-be-removed", "canary"},
		{"snaps/this-one-stays", "canary"},
	})

	// complain loudly if the file is under unexpected location
	err := devicestate.LogNewSystemSnapFile(flog, filepath.Join(otherDir, "some-file"))
	c.Assert(err, ErrorMatches, `internal error: unexpected recovery system snap location ".*/some-file"`)
	c.Check(flog, testutil.FileAbsent)

	expectedContent := &bytes.Buffer{}

	for _, p := range []string{
		filepath.Join(systemDir, "this-will-be-removed"),
		filepath.Join(systemDir, "this-one-too"),
		filepath.Join(systemDir, "does-not-exist"),
		filepath.Join(systemDir, "snaps/to-be-removed"),
	} {
		err = devicestate.LogNewSystemSnapFile(flog, p)
		c.Check(err, IsNil)
		fmt.Fprintln(expectedContent, p)
		// logged content is accumulated
		c.Check(flog, testutil.FileEquals, expectedContent.String())
	}

	// add some empty spaces to log file, which should get ignored when purging
	f, err := os.OpenFile(flog, os.O_APPEND, 0644)
	c.Assert(err, IsNil)
	defer f.Close()
	fmt.Fprintln(f, "    ")
	fmt.Fprintln(f, "")
	// and double some entries
	fmt.Fprintln(f, filepath.Join(systemDir, "this-will-be-removed"))

	err = devicestate.PurgeNewSystemSnapFiles(flog)
	c.Assert(err, IsNil)

	// those are removed
	for _, p := range []string{
		filepath.Join(systemDir, "this-will-be-removed"),
		filepath.Join(systemDir, "this-one-too"),
		filepath.Join(systemDir, "snaps/to-be-removed"),
	} {
		c.Check(p, testutil.FileAbsent)
	}
	c.Check(filepath.Join(systemDir, "this-one-stays"), testutil.FileEquals, "canary")
	c.Check(filepath.Join(systemDir, "snaps/this-one-stays"), testutil.FileEquals, "canary")
}

func (s *systemSnapTrackingSuite) TestSnapFilePurgeWhenNoLog(c *C) {
	otherDir := c.MkDir()
	flog := filepath.Join(otherDir, "files-log")
	// purge is still happy even if log file does not exist
	err := devicestate.PurgeNewSystemSnapFiles(flog)
	c.Assert(err, IsNil)
}
