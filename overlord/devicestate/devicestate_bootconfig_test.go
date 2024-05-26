// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/restart"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type deviceMgrBootconfigSuite struct {
	deviceMgrBaseSuite

	managedbl      *bootloadertest.MockTrustedAssetsBootloader
	gadgetSnapInfo *snap.Info
}

var _ = Suite(&deviceMgrBootconfigSuite{})

func (s *deviceMgrBootconfigSuite) mockGadget(c *C, yaml string) {
	s.state.Lock()
	defer s.state.Unlock()

	devicestate.SetBootOkRan(s.mgr, true)
	si := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{si}),
		Current:  si.Revision,
		Active:   true,
	})
	s.state.Set("seeded", true)

	s.gadgetSnapInfo = snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, si, [][]string{
		{"meta/gadget.yaml", yaml},
	})
}

func (s *deviceMgrBootconfigSuite) SetUpTest(c *C) {
	classic := false
	s.deviceMgrBaseSuite.setupBaseTest(c, classic)

	s.managedbl = bootloadertest.Mock("mock", c.MkDir()).WithTrustedAssets()
	bootloader.Force(s.managedbl)
	s.managedbl.StaticCommandLine = "console=ttyS0 console=tty1 panic=-1"
	s.managedbl.CandidateStaticCommandLine = "console=ttyS0 console=tty1 panic=-1 candidate"

	s.mockGadget(c, gadgetYaml)

	s.state.Lock()
	defer s.state.Unlock()

	// minimal mocking to reach the mocked bootloader API call
	modeenv := boot.Modeenv{
		Mode:           "run",
		RecoverySystem: "",
		CurrentKernelCommandLines: []string{
			"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		},
	}
	mylog.Check(modeenv.WriteTo(""))

}

func (s *deviceMgrBootconfigSuite) setupUC20Model(c *C) *asserts.Model {
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model-20",
		Serial: "didididi",
	})
	return s.makeModelAssertionInState(c, "canonical", "pc-model-20", mockCore20ModelHeaders)
}

func (s *deviceMgrBootconfigSuite) setupUC20ModelWithGrade(c *C, grade string) *asserts.Model {
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model-20",
		Serial: "didididi",
	})
	headers := make(map[string]interface{})
	for k, v := range mockCore20ModelHeaders {
		headers[k] = v
	}
	headers["grade"] = grade
	return s.makeModelAssertionInState(c, "canonical", "pc-model-20", headers)
}

func (s *deviceMgrBootconfigSuite) setupClassicWithModesModel(c *C) *asserts.Model {
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "classic-with-modes",
		Serial: "didididi",
	})
	return s.makeModelAssertionInState(c, "canonical", "classic-with-modes",
		map[string]interface{}{
			"architecture": "amd64",
			"classic":      "true",
			"distribution": "ubuntu",
			"base":         "core22",
			"snaps": []interface{}{
				map[string]interface{}{
					"name": "pc-linux",
					"id":   "pclinuxdidididididididididididid",
					"type": "kernel",
				},
				map[string]interface{}{
					"name": "pc",
					"id":   "pcididididididididididididididid",
					"type": "gadget",
				},
			},
		})
}

type testBootConfigUpdateOpts struct {
	updateAttempted     bool
	updateApplied       bool
	cmdlineAppend       string
	cmdlineAppendDanger string
}

func (s *deviceMgrBootconfigSuite) testBootConfigUpdateRun(c *C, opts testBootConfigUpdateOpts, errMatch string) {
	restore := release.MockOnClassic(false)
	defer restore()

	// Override the gadget from SetupTest to add allowed arguments
	yaml := gadgetYaml + `
kernel-cmdline:
  allow:
    - par1=val
    - par2
    - append1=val
    - append2
`
	s.mockGadget(c, yaml)

	s.state.Lock()

	// Set options that influence the final kernel command line
	tr := config.NewTransaction(s.state)
	mylog.Check(tr.Set("core", "system.kernel.cmdline-append", opts.cmdlineAppend))

	tr.Set("core", "system.kernel.dangerous-cmdline-append", opts.cmdlineAppendDanger)

	tr.Commit()

	tsk := s.state.NewTask("update-managed-boot-config", "update boot config")
	tsk.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &s.gadgetSnapInfo.SideInfo,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("sample", "...")

	chg.AddTask(tsk)
	chg.Set("system-restart-immediate", true)

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	restarting, rt := restart.Pending(s.state)
	if errMatch == "" {
		if opts.updateAttempted && opts.updateApplied {
			// Expect the change to be in wait status at this point, as a restart
			// will have been requested
			c.Check(tsk.Status(), Equals, state.WaitStatus)
			c.Check(chg.Status(), Equals, state.WaitStatus)
			// Restart and re-run to completion
			s.mockRestartAndSettle(c, s.state, chg)
		}
		c.Assert(chg.IsReady(), Equals, true)
		c.Check(chg.Err(), IsNil)
		c.Check(tsk.Status(), Equals, state.DoneStatus)
	} else {
		c.Assert(chg.IsReady(), Equals, true)
		c.Check(chg.Err(), ErrorMatches, errMatch)
		c.Check(tsk.Status(), Equals, state.ErrorStatus)
	}
	if opts.updateAttempted {
		c.Assert(s.managedbl.UpdateCalls, Equals, 1)
		if errMatch == "" && opts.updateApplied {
			// we log on success
			log := tsk.Log()
			c.Assert(log, HasLen, 2)
			c.Check(log[0], Matches, ".* updated boot config assets")
			c.Check(log[1], Matches, ".* INFO Task set to wait until a system restart allows to continue")
			// update was applied, thus a restart was requested
			c.Check(s.restartRequests, DeepEquals, []restart.RestartType{restart.RestartSystemNow})
			c.Check(restarting, Equals, true)
			c.Check(rt, Equals, restart.RestartSystemNow)
		} else {
			// update was not applied or failed
			c.Check(s.restartRequests, HasLen, 0)
		}
	} else {
		c.Assert(s.managedbl.UpdateCalls, Equals, 0)
	}
}

func (s *deviceMgrBootconfigSuite) testBootConfigUpdateRunClassic(c *C, opts testBootConfigUpdateOpts, errMatch string) {
	restore := release.MockOnClassic(true)
	defer restore()

	// Override the gadget from SetupTest to add allowed arguments
	yaml := gadgetYaml + `
kernel-cmdline:
  allow:
    - par1=val
    - par2
    - append1=val
    - append2
`
	s.mockGadget(c, yaml)

	s.state.Lock()

	// Set options that influence the final kernel command line
	tr := config.NewTransaction(s.state)
	mylog.Check(tr.Set("core", "system.kernel.cmdline-append", opts.cmdlineAppend))

	tr.Set("core", "system.kernel.dangerous-cmdline-append", opts.cmdlineAppendDanger)

	tr.Commit()

	tsk := s.state.NewTask("update-managed-boot-config", "update boot config")
	tsk.Set("snap-setup", &snapstate.SnapSetup{
		SideInfo: &s.gadgetSnapInfo.SideInfo,
		Type:     snap.TypeGadget,
	})
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(tsk)
	chg.Set("system-restart-immediate", true)

	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	if errMatch == "" {
		if opts.updateAttempted && opts.updateApplied {
			// Expect the change to be in wait status at this point, as a restart
			// will have been requested
			c.Check(tsk.Status(), Equals, state.WaitStatus)
			c.Check(chg.Status(), Equals, state.WaitStatus)
			// Restart and re-run to completion
			s.mockRestartAndSettle(c, s.state, chg)
		}
		c.Assert(chg.IsReady(), Equals, true)
		c.Check(chg.Err(), IsNil)
		c.Check(tsk.Status(), Equals, state.DoneStatus)
	} else {
		c.Assert(chg.IsReady(), Equals, true)
		c.Check(chg.Err(), ErrorMatches, errMatch)
		c.Check(tsk.Status(), Equals, state.ErrorStatus)
	}
	if opts.updateAttempted {
		c.Assert(s.managedbl.UpdateCalls, Equals, 1)
		if errMatch == "" && opts.updateApplied {
			// we log on success
			log := tsk.Log()
			c.Assert(log, HasLen, 2)
			c.Check(log[0], Matches, ".* updated boot config assets")
			c.Check(log[1], Matches, ".* INFO Task set to wait until a system restart allows to continue")
		}
		// There must be no restart request
		c.Check(s.restartRequests, HasLen, 0)
	} else {
		c.Assert(s.managedbl.UpdateCalls, Equals, 0)
	}
}

func (s *deviceMgrBootconfigSuite) TestBootConfigUpdateRunSuccess(c *C) {
	s.state.Lock()
	s.setupUC20Model(c)
	s.state.Unlock()

	s.managedbl.Updated = true

	opts := testBootConfigUpdateOpts{
		updateAttempted: true,
		updateApplied:   true,
	}
	s.testBootConfigUpdateRun(c, opts, "")

	m := mylog.Check2(boot.ReadModeenv(""))

	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 candidate",
	})
}

func (s *deviceMgrBootconfigSuite) TestBootConfigUpdateRunSuccessClassicWithModes(c *C) {
	s.state.Lock()
	s.setupClassicWithModesModel(c)
	s.state.Unlock()

	s.managedbl.Updated = true

	opts := testBootConfigUpdateOpts{
		updateAttempted: true,
		updateApplied:   true,
	}
	s.testBootConfigUpdateRunClassic(c, opts, "")

	m := mylog.Check2(boot.ReadModeenv(""))

	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 candidate",
	})
}

func (s *deviceMgrBootconfigSuite) TestBootConfigUpdateRunSuccessClassicWithModesWithAppend(c *C) {
	s.state.Lock()
	s.setupClassicWithModesModel(c)
	s.state.Unlock()

	s.managedbl.Updated = true

	opts := testBootConfigUpdateOpts{
		updateAttempted: true,
		updateApplied:   true,
		cmdlineAppend:   "par1=val par2",
	}
	s.testBootConfigUpdateRunClassic(c, opts, "")

	m := mylog.Check2(boot.ReadModeenv(""))

	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 candidate par1=val par2",
	})
}

func (s *deviceMgrBootconfigSuite) TestBootConfigUpdateWithGadgetExtra(c *C) {
	s.state.Lock()
	s.setupUC20Model(c)
	s.state.Unlock()

	s.managedbl.Updated = true

	// drop the file for gadget
	snaptest.PopulateDir(s.gadgetSnapInfo.MountDir(), [][]string{
		{"cmdline.extra", "args from gadget"},
	})

	// update the modeenv to have the gadget arguments included to mimic the
	// state we would have in the system
	m := mylog.Check2(boot.ReadModeenv(""))

	m.CurrentKernelCommandLines = []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from gadget",
	}
	c.Assert(m.Write(), IsNil)

	opts := testBootConfigUpdateOpts{
		updateAttempted: true,
		updateApplied:   true,
	}
	s.testBootConfigUpdateRun(c, opts, "")

	m = mylog.Check2(boot.ReadModeenv(""))

	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 args from gadget",
		// gadget arguments are picked up for the candidate command line
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 candidate args from gadget",
	})
}

func (s *deviceMgrBootconfigSuite) testBootConfigUpdateRunWithAppend(c *C, grade string) {
	s.state.Lock()
	s.setupUC20ModelWithGrade(c, grade)
	s.state.Unlock()

	s.managedbl.Updated = true

	opts := testBootConfigUpdateOpts{
		updateAttempted: true,
		updateApplied:   true,
		cmdlineAppend:   "par1=val par2",
	}
	s.testBootConfigUpdateRun(c, opts, "")

	m := mylog.Check2(boot.ReadModeenv(""))

	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 candidate par1=val par2",
	})
}

func (s *deviceMgrBootconfigSuite) TestBootConfigUpdateRunWithAppendModelDangerous(c *C) {
	s.testBootConfigUpdateRunWithAppend(c, "dangerous")
}

func (s *deviceMgrBootconfigSuite) TestBootConfigUpdateRunWithAppendModelSigned(c *C) {
	s.testBootConfigUpdateRunWithAppend(c, "signed")
}

func (s *deviceMgrBootconfigSuite) testBootConfigUpdateRunWithDangerousAppend(c *C, grade string) {
	s.state.Lock()
	s.setupUC20ModelWithGrade(c, grade)
	s.state.Unlock()

	s.managedbl.Updated = true

	opts := testBootConfigUpdateOpts{
		updateAttempted:     true,
		updateApplied:       true,
		cmdlineAppendDanger: "par1=val par2",
	}
	s.testBootConfigUpdateRun(c, opts, "")

	m := mylog.Check2(boot.ReadModeenv(""))

	expected := "snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 candidate"
	if grade == "dangerous" {
		expected += " par1=val par2"
	}
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		expected,
	})
}

func (s *deviceMgrBootconfigSuite) TestBootConfigUpdateRunWithDangerousAppendDangerousModel(c *C) {
	s.testBootConfigUpdateRunWithDangerousAppend(c, "dangerous")
}

func (s *deviceMgrBootconfigSuite) TestBootConfigUpdateRunWithDangerousAppendSignedModel(c *C) {
	s.testBootConfigUpdateRunWithDangerousAppend(c, "signed")
}

func (s *deviceMgrBootconfigSuite) testBootConfigUpdateRunWithAppendBothOpts(c *C, grade string) {
	s.state.Lock()
	s.setupUC20ModelWithGrade(c, grade)
	s.state.Unlock()

	s.managedbl.Updated = true

	opts := testBootConfigUpdateOpts{
		updateAttempted:     true,
		updateApplied:       true,
		cmdlineAppend:       "par1=val par2",
		cmdlineAppendDanger: "par3=val par4",
	}
	s.testBootConfigUpdateRun(c, opts, "")

	m := mylog.Check2(boot.ReadModeenv(""))

	expected := "snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1 candidate par1=val par2"
	if grade == "dangerous" {
		expected += " par3=val par4"
	}
	c.Check([]string(m.CurrentKernelCommandLines), DeepEquals, []string{
		"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		expected,
	})
}

func (s *deviceMgrBootconfigSuite) TestBootConfigUpdateRunWithAppendBothOptsDangerousModel(c *C) {
	s.testBootConfigUpdateRunWithAppendBothOpts(c, "dangerous")
}

func (s *deviceMgrBootconfigSuite) TestBootConfigUpdateRunWithAppendBothOptsSignedModel(c *C) {
	s.testBootConfigUpdateRunWithAppendBothOpts(c, "signed")
}

func (s *deviceMgrBootconfigSuite) TestBootConfigUpdateRunButNotUpdated(c *C) {
	s.state.Lock()
	s.setupUC20Model(c)
	s.state.Unlock()

	s.managedbl.CandidateStaticCommandLine = s.managedbl.StaticCommandLine
	s.managedbl.Updated = false

	opts := testBootConfigUpdateOpts{
		updateAttempted: true,
		updateApplied:   false,
	}
	s.testBootConfigUpdateRun(c, opts, "")
}

func (s *deviceMgrBootconfigSuite) TestBootConfigUpdateUpdateErr(c *C) {
	s.state.Lock()
	s.setupUC20Model(c)
	s.state.Unlock()

	s.managedbl.UpdateErr = errors.New("update fail")
	// actually tried to update
	opts := testBootConfigUpdateOpts{
		updateAttempted: true,
		updateApplied:   false,
	}
	s.testBootConfigUpdateRun(c, opts,
		`(?ms).*cannot update boot config assets: update fail\)`)
}

func (s *deviceMgrBootconfigSuite) TestBootConfigNoUC20(c *C) {
	s.state.Lock()
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model",
		Serial: "didididi",
	})
	s.makeModelAssertionInState(c, "canonical", "pc-model", map[string]interface{}{
		"architecture": "amd64",
		"kernel":       "pc-kernel",
		"gadget":       "pc",
		"base":         "core18",
	})
	s.state.Unlock()

	opts := testBootConfigUpdateOpts{
		updateAttempted: false,
		updateApplied:   false,
	}
	s.testBootConfigUpdateRun(c, opts, "")
}

func (s *deviceMgrBootconfigSuite) TestBootConfigRemodelDoNothing(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	s.state.Lock()

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "pc-model-20",
		Serial: "didididi",
	})

	uc20Model := s.setupUC20Model(c)
	// save the hassle and try a trivial remodel
	newModel := s.brands.Model("canonical", "pc-model-20", map[string]interface{}{
		"brand":        "canonical",
		"model":        "pc-model-20",
		"architecture": "amd64",
		"grade":        "dangerous",
		"base":         "core20",
		"snaps":        mockCore20ModelSnaps,
	})
	remodCtx := mylog.Check2(devicestate.RemodelCtx(s.state, uc20Model, newModel))

	// be extra sure
	c.Check(remodCtx.ForRemodeling(), Equals, true)
	tsk := s.state.NewTask("update-managed-boot-config", "update boot config")
	chg := s.state.NewChange("sample", "...")
	chg.AddTask(tsk)
	remodCtx.Init(chg)
	// replace the bootloader with something that always fails
	bootloader.ForceError(errors.New("unexpected call"))
	s.state.Unlock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	c.Assert(chg.IsReady(), Equals, true)
	c.Check(chg.Err(), IsNil)
	c.Check(tsk.Status(), Equals, state.DoneStatus)
}
