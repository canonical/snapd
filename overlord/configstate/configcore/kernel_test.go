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
	"io/ioutil"
	"os"
	"path/filepath"

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
	"github.com/snapcore/snapd/overlord/state"
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
	mockEtcEnvironment := filepath.Join(dirs.GlobalRootDir, "/etc/environment")
	err = ioutil.WriteFile(mockEtcEnvironment, []byte{}, 0644)
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

func (s *kernelSuite) mockModel(st *state.State, grade string) {
	s.state.Lock()
	defer s.state.Unlock()

	// model setup
	model := s.Brands.Model("my-brand", "pc", map[string]interface{}{
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
	})

	assertstatetest.AddMany(s.state, model)

	devicestatetest.SetDevice(st, &auth.DeviceState{
		Brand:  model.BrandID(),
		Model:  model.Model(),
		Serial: "serialserial",
	})
}

func (s *kernelSuite) testConfigureKernelCmdlineHappy(c *C, option, cmdline, modelGrade string) {
	s.mockModel(s.state, modelGrade)
	doHandlerCalls := 0

	s.overlord.TaskRunner().AddHandler("update-gadget-cmdline",
		func(task *state.Task, tomb *tomb.Tomb) error {
			doHandlerCalls++

			var trCmdlineOpt string
			s.state.Lock()
			defer s.state.Unlock()
			tr := config.NewTransaction(s.state)
			err := tr.Get("core", option, &trCmdlineOpt)
			if err != nil {
				return err
			}
			if trCmdlineOpt != cmdline {
				return fmt.Errorf("cmdline is %q, expected %q",
					trCmdlineOpt, cmdline)
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
	hookCtx.Set("patch", map[string]interface{}{
		option: cmdline,
	})
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
	<-chg.Ready()
	s.overlord.Stop()

	c.Assert(doHandlerCalls, Equals, 1)

	s.state.Lock()
	changes := s.state.Changes()
	c.Assert(len(changes), Equals, 2)
	var soCh, aecCh *state.Change
	for _, ch := range changes {
		switch ch.Kind() {
		case "system-option":
			soCh = ch
		case "apply-extra-cmdline":
			aecCh = ch
		default:
			c.Fatal("unexpected change kind")
		}
	}
	// If the handler of these tasks failed it will be an error status
	c.Assert(soCh.Status(), Equals, state.DoneStatus)
	c.Assert(aecCh.Status(), Equals, state.DoneStatus)
	aecTsks := aecCh.Tasks()
	c.Assert(len(aecTsks), Equals, 1)
	var systemOption bool
	c.Assert(aecTsks[0].Get("system-option", &systemOption), IsNil)
	c.Assert(systemOption, Equals, true)
	s.state.Unlock()
}

func (s *kernelSuite) TestConfigureKernelCmdlineDangGrade(c *C) {
	s.testConfigureKernelCmdlineHappy(c, configcore.OptionKernelCmdlineAppend, "par=val param", "dangerous")
}

func (s *kernelSuite) TestConfigureKernelCmdlineSignedGrade(c *C) {
	s.testConfigureKernelCmdlineHappy(c, configcore.OptionKernelCmdlineAppend, "par=val param", "signed")
}

func (s *kernelSuite) TestConfigureKernelCmdlineDangGradeDangCmdline(c *C) {
	s.testConfigureKernelCmdlineHappy(c, configcore.OptionKernelDangerousCmdlineAppend, "par=val param", "dangerous")
}

func (s *kernelSuite) TestConfigureKernelCmdlineSignedGradeDangCmdline(c *C) {
	s.mockModel(s.state, "signed")

	cmdline := "param1=val1"
	s.state.Lock()
	ts := s.state.NewTask("hook-task", "system hook task")
	chg := s.state.NewChange("system-option", "...")
	chg.AddTask(ts)
	rt := configcore.NewRunTransaction(config.NewTransaction(s.state), ts)
	s.state.Unlock()

	rt.Set("core", configcore.OptionKernelDangerousCmdlineAppend, cmdline)

	err := configcore.Run(coreDev, rt)
	c.Assert(err, ErrorMatches, "cannot use system.kernel.dangerous-cmdline-append for non-dangerous model")
}
