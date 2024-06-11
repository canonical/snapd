// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package configcore_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/asserts/sysdb"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/bootloadertest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/configstate/configcore"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/hookstate/hooktest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store/storetest"
	"github.com/snapcore/snapd/sysconfig"
	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"
)

type kernelSuite struct {
	configcoreSuite
	storetest.Store

	overlord *overlord.Overlord

	StoreSigning *assertstest.StoreStack
	Brands       *assertstest.SigningAccounts
}

var _ = Suite(&kernelSuite{})

var (
	brandPrivKey, _ = assertstest.GenerateKey(752)
)

func (s *kernelSuite) SetUpTest(c *C) {
	s.configcoreSuite.SetUpTest(c)

	var err error

	// File mocking
	err = os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/etc/"), 0755)
	c.Assert(err, IsNil)

	// Bootloader mocking
	bl := bootloadertest.Mock("mock", "")
	bootloader.Force(bl)
	s.AddCleanup(func() { bootloader.Force(nil) })
	bootVars := make(map[string]string)
	bootVars["snap_kernel"] = "pc-kernel_1.snap"
	err = bl.SetBootVars(bootVars)
	c.Assert(err, IsNil)

	// mock the modeenv file
	modeenv := boot.Modeenv{
		// base is base1
		Base: "core20_1.snap",
		// no try base
		TryBase: "",
		// base status is default
		BaseStatus: boot.DefaultStatus,
		// gadget is gadget1
		Gadget: "pc_1.snap",
		// current kernels is just kern1
		CurrentKernels: []string{"pc-kernel_1.snap"},
		// operating mode is run
		Mode: "run",
		// RecoverySystem is unset, as it should be during run mode
		RecoverySystem: "",
	}
	err = modeenv.WriteTo("")
	c.Assert(err, IsNil)

	s.AddCleanup(osutil.MockMountInfo(""))

	// Force backend building so EnsureBefore works
	s.overlord = overlord.MockWithState(nil)
	s.state = s.overlord.State()

	s.StoreSigning = assertstest.NewStoreStack("my-brand", nil)
	s.AddCleanup(sysdb.InjectTrusted(s.StoreSigning.Trusted))

	s.Brands = assertstest.NewSigningAccounts(s.StoreSigning)
	s.Brands.Register("my-brand", brandPrivKey, nil)

	db, err := asserts.OpenDatabase(&asserts.DatabaseConfig{
		Backstore:       asserts.NewMemoryBackstore(),
		Trusted:         s.StoreSigning.Trusted,
		OtherPredefined: s.StoreSigning.Generic,
	})
	c.Assert(err, IsNil)
	s.state.Lock()
	s.state.Set("seeded", true)
	assertstate.ReplaceDB(s.state, db)
	s.state.Unlock()

	s.mockEarlyConfig()

	// we need a device context for this option
	hookMgr, err := hookstate.Manager(s.state, s.overlord.TaskRunner())
	c.Assert(err, IsNil)
	err = configstate.Init(s.state, hookMgr)
	c.Assert(err, IsNil)
	deviceMgr, err := devicestate.Manager(s.state, hookMgr, s.overlord.TaskRunner(), nil)
	c.Assert(err, IsNil)

	s.overlord.AddManager(hookMgr)
	s.overlord.AddManager(deviceMgr)
	s.overlord.AddManager(s.overlord.TaskRunner())

	s.mockEarlyConfig()
}

func (s *kernelSuite) mockEarlyConfig() {
	devicestate.EarlyConfig = func(*state.State, func() (
		sysconfig.Device, *gadget.Info, error)) error {
		return nil
	}
	s.AddCleanup(func() { devicestate.EarlyConfig = nil })
}

func (s *kernelSuite) mockClassicBasicModelWithoutSnaps() {
	s.state.Lock()
	defer s.state.Unlock()

	extras := map[string]interface{}{
		"architecture": "amd64",
		"classic":      "true",
	}
	model := s.Brands.Model("my-brand", "pc", extras)

	assertstatetest.AddMany(s.state, model)
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  model.BrandID(),
		Model:  model.Model(),
		Serial: "serialserial",
	})
}

func (s *kernelSuite) mockModelWithModeenv(grade string, isClassic bool) {
	s.state.Lock()
	defer s.state.Unlock()

	// model setup
	extras := map[string]interface{}{
		"architecture": "amd64",
		"base":         "core20",
		"grade":        grade,
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              "pYVQrBcKmBa0mZ4CCN7ExT6jH8rY1hza",
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              "UqFziVZDHLSyO3TqSWgNBoAdHbLI4dAH",
				"type":            "gadget",
				"default-channel": "20",
			},
		},
	}
	if isClassic {
		extras["classic"] = "true"
		extras["distribution"] = "ubuntu"
	}
	model := s.Brands.Model("my-brand", "pc", extras)

	assertstatetest.AddMany(s.state, model)

	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  model.BrandID(),
		Model:  model.Model(),
		Serial: "serialserial",
	})
}

const gadgetSnapYaml = `
name: pc
type: gadget
`

const gadgetYaml = `
volumes:
  pc:
    bootloader: grub
kernel-cmdline:
  allow:
    - par=val
    - param
    - star=*
`

func (s *kernelSuite) mockGadget(c *C) {
	pcSideInfo := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(1),
		SnapID:   "UqFziVZDHLSyO3TqSWgNBoAdHbLI4dAH",
	}
	files := [][]string{{"meta/gadget.yaml", gadgetYaml}}
	snaptest.MockSnapWithFiles(c, gadgetSnapYaml, pcSideInfo, files)

	s.state.Lock()
	defer s.state.Unlock()
	snapstate.Set(s.state, "pc", &snapstate.SnapState{
		SnapType: "gadget",
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{pcSideInfo}),
		Current:  pcSideInfo.Revision,
		Active:   true,
	})
}

type cmdlineOption struct {
	name    string
	cmdline string
}

func (s *kernelSuite) testConfigureKernelCmdlineHappy(c *C, option []cmdlineOption, modelGrade string, isClassic bool) {
	s.mockModelWithModeenv(modelGrade, isClassic)
	s.mockGadget(c)
	doHandlerCalls := 0
	extraChange := true
	expectedHandlerCalls := 1
	expectedChanges := 2
	if modelGrade != "dangerous" && len(option) == 1 && option[0].name == "system.kernel.dangerous-cmdline-append" {
		extraChange = false
		expectedHandlerCalls = 0
		expectedChanges = 1
	}

	s.overlord.TaskRunner().AddHandler("update-gadget-cmdline",
		func(task *state.Task, tomb *tomb.Tomb) error {
			doHandlerCalls++

			s.state.Lock()
			defer s.state.Unlock()

			// Check we have set only the needed options
			for _, optName := range []string{
				"system.kernel.cmdline-append",
				"system.kernel.dangerous-cmdline-append"} {
				shouldHaveBeenSet := false
				cmdline := ""
				for _, opt := range option {
					if optName == opt.name {
						shouldHaveBeenSet = true
						cmdline = opt.cmdline
					}
				}

				key := strings.Split(optName, ".")[2]
				var taskCmdline string
				if shouldHaveBeenSet {
					err := task.Get(key, &taskCmdline)
					c.Check(err, IsNil)
					c.Check(taskCmdline, Equals, cmdline)
				} else {
					err := task.Get(key, &taskCmdline)
					errStr := fmt.Sprintf("no state entry for key %q", key)
					c.Check(err, ErrorMatches, errStr)
				}
			}

			return nil
		},
		func(task *state.Task, tomb *tomb.Tomb) error { return nil })

	var err error

	s.state.Lock()
	ts := s.state.NewTask("run-hook", "system hook task")
	hsup := &hookstate.HookSetup{
		Hook: "configure",
		Snap: "core",
	}
	ts.Set("hook-setup", &hsup)
	hookCtx, err := hookstate.NewContext(ts, ts.State(), hsup,
		hooktest.NewMockHandler(), "")
	c.Assert(err, IsNil)
	s.state.Unlock()

	hookCtx.Lock()
	patchVals := make(map[string]interface{})
	for _, opt := range option {
		patchVals[opt.name] = opt.cmdline
	}
	hookCtx.Set("patch", patchVals)
	hookCtx.Unlock()

	s.state.Lock()
	chg := s.state.NewChange("system-option", "...")
	chg.AddTask(ts)
	s.state.EnsureBefore(0)
	s.state.Unlock()

	// We need loop instead of settle because we create an
	// ancillary change when the option is set.
	c.Assert(s.overlord.StartUp(), IsNil)
	s.overlord.Loop()
	select {
	case <-chg.Ready():
	case <-time.After(2 * time.Minute):
		c.Fatal("waiting for too long")
	}
	s.overlord.Stop()

	c.Assert(doHandlerCalls, Equals, expectedHandlerCalls)

	s.state.Lock()
	defer s.state.Unlock()

	changes := s.state.Changes()
	c.Assert(len(changes), Equals, expectedChanges)
	var soCh, aecCh *state.Change
	for _, ch := range changes {
		switch ch.Kind() {
		case "system-option":
			soCh = ch
		case "apply-cmdline-append":
			aecCh = ch
		default:
			c.Fatal("unexpected change kind")
		}
	}
	// If the handler of these tasks failed it will be an error status
	c.Assert(soCh.Status(), Equals, state.DoneStatus)
	if extraChange {
		c.Assert(aecCh.Status(), Equals, state.DoneStatus)
		aecTsks := aecCh.Tasks()
		c.Assert(len(aecTsks), Equals, 1)
	} else {
		c.Assert(aecCh, IsNil)
	}

	var taskCmdline string
	tr := config.NewTransaction(s.state)
	for _, opt := range option {
		c.Assert(tr.Get("core", opt.name, &taskCmdline), IsNil)
		c.Assert(taskCmdline, Equals, opt.cmdline)
	}
}

func (s *kernelSuite) TestConfigureKernelCmdlineDangerousGrade(c *C) {
	const isClassic = false
	s.testConfigureKernelCmdlineHappy(c,
		[]cmdlineOption{{
			name:    "system.kernel.cmdline-append",
			cmdline: "par=val param"}},
		"dangerous", isClassic)
}

func (s *kernelSuite) TestConfigureKernelCmdlineDangerousGradeClassic(c *C) {
	const isClassic = true
	s.testConfigureKernelCmdlineHappy(c,
		[]cmdlineOption{{
			name:    "system.kernel.cmdline-append",
			cmdline: "par=val param"}},
		"dangerous", isClassic)
}

func (s *kernelSuite) TestConfigureKernelCmdlineSignedGrade(c *C) {
	const isClassic = false
	s.testConfigureKernelCmdlineHappy(c,
		[]cmdlineOption{{
			name:    "system.kernel.cmdline-append",
			cmdline: "par=val param star=val"}},
		"signed", isClassic)
}

func (s *kernelSuite) TestConfigureKernelCmdlineDangerousGradeDangerousCmdline(c *C) {
	const isClassic = false
	s.testConfigureKernelCmdlineHappy(c,
		[]cmdlineOption{{
			name:    "system.kernel.dangerous-cmdline-append",
			cmdline: "par=val param"}},
		"dangerous", isClassic)
}

func (s *kernelSuite) TestConfigureKernelCmdlineBothOptions(c *C) {
	const isClassic = false
	s.testConfigureKernelCmdlineHappy(c,
		[]cmdlineOption{
			{
				name:    "system.kernel.cmdline-append",
				cmdline: "par=val param"},
			{
				name:    "system.kernel.dangerous-cmdline-append",
				cmdline: "dang_par=dang_val dang_param"},
		},
		"dangerous", isClassic)
}

func (s *kernelSuite) TestConfigureKernelCmdlineSignedGradeDangerousCmdline(c *C) {
	const isClassic = false
	s.testConfigureKernelCmdlineHappy(c,
		[]cmdlineOption{{
			name:    "system.kernel.dangerous-cmdline-append",
			cmdline: "par=val param"}},
		"signed", isClassic)
}

func (s *kernelSuite) TestConfigureKernelCmdlineConflict(c *C) {
	isClassic := false
	s.mockModelWithModeenv("dangerous", isClassic)

	cmdline := "param1=val1"
	s.state.Lock()

	tugc := s.state.NewTask("update-gadget-cmdline", "update gadget cmdline")
	chgUpd := s.state.NewChange("optional-kernel-cmdline", "optional kernel cmdline")
	chgUpd.AddTask(tugc)

	ts := s.state.NewTask("hook-task", "system hook task")
	chg := s.state.NewChange("system-option", "...")
	chg.AddTask(ts)
	rt := configcore.NewRunTransaction(config.NewTransaction(s.state), ts)

	s.state.Unlock()

	rt.Set("core", "system.kernel.dangerous-cmdline-append", cmdline)

	err := configcore.Run(core20Dev, rt)
	c.Assert(err, ErrorMatches, "kernel command line already being updated, no additional changes for it allowed meanwhile")
}

func (s *kernelSuite) testConfigureKernelCmdlineSignedGradeNotAllowed(c *C, cmdline string) {
	isClassic := false
	s.mockModelWithModeenv("signed", isClassic)
	s.mockGadget(c)

	s.state.Lock()

	ts := s.state.NewTask("hook-task", "system hook task")
	chg := s.state.NewChange("system-option", "...")
	chg.AddTask(ts)
	rt := configcore.NewRunTransaction(config.NewTransaction(s.state), ts)

	s.state.Unlock()

	rt.Set("core", "system.kernel.cmdline-append", cmdline)

	err := configcore.Run(core20Dev, rt)
	c.Assert(err.Error(), Equals, fmt.Sprintf("%q is not allowed in the kernel command line by the gadget", cmdline))
}

func (s *kernelSuite) TestConfigureKernelCmdlineSignedGradeNotAllowed(c *C) {
	for _, cmdline := range []string{
		"forbidden=val",
		`forbidden1 forbidden2=" with quotes "`,
	} {
		s.testConfigureKernelCmdlineSignedGradeNotAllowed(c, cmdline)
	}
}

func (s *kernelSuite) TestConfigureKernelCmdlineOnClassicFails(c *C) {
	s.mockClassicBasicModelWithoutSnaps()

	cmdline := "param1=val1"
	s.state.Lock()

	tugc := s.state.NewTask("update-gadget-cmdline", "update gadget cmdline")
	chgUpd := s.state.NewChange("optional-kernel-cmdline", "optional kernel cmdline")
	chgUpd.AddTask(tugc)

	ts := s.state.NewTask("hook-task", "system hook task")
	chg := s.state.NewChange("system-option", "...")
	chg.AddTask(ts)
	rt := configcore.NewRunTransaction(config.NewTransaction(s.state), ts)

	s.state.Unlock()

	rt.Set("core", "system.kernel.cmdline-append", cmdline)

	err := configcore.Run(core20Dev, rt)
	c.Assert(err, ErrorMatches, "changing the kernel command line is not supported on a classic system")
}
