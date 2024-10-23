// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package fdestate_test

import (
	"bytes"
	"crypto"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func TestFDE(t *testing.T) { TestingT(t) }

type fdeMgrSuite struct {
	testutil.BaseTest

	logbuf  *bytes.Buffer
	rootdir string
	st      *state.State
	runner  *state.TaskRunner
}

var _ = Suite(&fdeMgrSuite{})

func (s *fdeMgrSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.st = state.New(nil)
	s.runner = state.NewTaskRunner(s.st)

	buf, restore := logger.MockLogger()
	s.AddCleanup(restore)
	s.logbuf = buf

	c.Assert(os.Setenv("SNAPD_DEBUG", "1"), IsNil)
	s.AddCleanup(func() {
		os.Unsetenv("SNAPD_DEBUG")
	})

	s.AddCleanup(fdestate.MockBackendResealKeyForBootChains(
		func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
			panic("BackendResealKeyForBootChains not mocked")
		}))
	s.AddCleanup(fdestate.MockDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		panic("MockDMCryptUUIDFromMountPoint is not mocked")
	}))
	s.AddCleanup(fdestate.MockGetPrimaryKeyHMAC(func(devicePath string, alg crypto.Hash) ([]byte, []byte, error) {
		panic("GetPrimaryKeyHMAC is not mocked")
	}))
	s.AddCleanup(fdestate.MockVerifyPrimaryKeyHMAC(func(devicePath string, alg crypto.Hash, salt, digest []byte) (bool, error) {
		panic("VerifyPrimaryKeyHMAC is not mocked")
	}))
	s.AddCleanup(fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			panic("BackendResealKeysForSignaturesDBUpdate not mocked")
		}))
}

func (s *fdeMgrSuite) TearDownTest(c *C) {
	c.Assert(s.logbuf, NotNil)
	c.Logf("logs:\n%s\n", s.logbuf.String())
	s.BaseTest.TearDownTest(c)
}

func (s *fdeMgrSuite) runnerIterationLocked(c *C) {
	err := func() error {
		s.st.Unlock()
		defer s.st.Lock()
		return s.runner.Ensure()
	}()
	c.Assert(err, IsNil)
}

type instrumentedUnlocker struct {
	state    *state.State
	unlocked int
	relocked int
}

func (u *instrumentedUnlocker) Unlock() (relock func()) {
	u.state.Unlock()
	u.unlocked += 1
	return u.Relock
}

func (u *instrumentedUnlocker) Relock() {
	u.state.Lock()
	u.relocked += 1
}

func (s *fdeMgrSuite) startedManager(c *C) *fdestate.FDEManager {
	defer fdestate.MockDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case dirs.SnapdStateDir(dirs.GlobalRootDir):
			return "aaa", nil
		case dirs.SnapSaveDir:
			return "bbb", nil
		}
		panic(fmt.Sprintf("missing mocked mount point %q", mountpoint))
	})()

	defer fdestate.MockGetPrimaryKeyHMAC(func(devicePath string, alg crypto.Hash) ([]byte, []byte, error) {
		c.Assert(devicePath, Equals, "/dev/disk/by-uuid/aaa")
		c.Check(alg, Equals, crypto.Hash(crypto.SHA256))
		return []byte{1, 2, 3, 4}, []byte{5, 6, 7, 8}, nil
	})()

	defer fdestate.MockVerifyPrimaryKeyHMAC(func(devicePath string, alg crypto.Hash, salt, digest []byte) (bool, error) {
		c.Assert(devicePath, Equals, "/dev/disk/by-uuid/bbb")
		c.Check(alg, Equals, crypto.Hash(crypto.SHA256))
		c.Check(salt, DeepEquals, []byte{1, 2, 3, 4})
		c.Check(digest, DeepEquals, []byte{5, 6, 7, 8})
		return true, nil
	})()

	manager := fdestate.Manager(s.st, s.runner)
	c.Assert(manager.StartUp(), IsNil)
	return manager
}

func (s *fdeMgrSuite) TestGetManagerFromState(c *C) {
	st := s.st
	manager := s.startedManager(c)

	st.Lock()
	defer st.Unlock()
	foundManager := fdestate.FdeMgr(st)
	c.Check(foundManager, Equals, manager)

	var fdeSt fdestate.FdeState
	err := st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)
	primaryKey, hasPrimaryKey := fdeSt.PrimaryKeys[0]
	c.Assert(hasPrimaryKey, Equals, true)
	c.Check(crypto.Hash(primaryKey.Digest.Algorithm), Equals, crypto.Hash(crypto.SHA256))
	c.Check(primaryKey.Digest.Salt, DeepEquals, []byte{1, 2, 3, 4})
	c.Check(primaryKey.Digest.Digest, DeepEquals, []byte{5, 6, 7, 8})
}

type mockModel struct {
}

func (m *mockModel) Series() string {
	return "mock-series"
}

func (m *mockModel) BrandID() string {
	return "mock-brand"
}

func (m *mockModel) Model() string {
	return "mock-model"
}

func (m *mockModel) Classic() bool {
	return false
}

func (m *mockModel) Grade() asserts.ModelGrade {
	return asserts.ModelSigned
}

func (m *mockModel) SignKeyID() string {
	return "mock-key"
}

func (s *fdeMgrSuite) TestUpdateState(c *C) {
	st := s.st
	manager := s.startedManager(c)

	st.Lock()
	defer st.Unlock()
	foundManager := fdestate.FdeMgr(st)
	c.Check(foundManager, Equals, manager)

	models := []secboot.ModelForSealing{
		&mockModel{},
	}

	fdestate.UpdateParameters(st, "run+recover", "container-role", []string{"run"}, models, secboot.SerializedPCRProfile(`"serialized-profile"`))

	var fdeSt fdestate.FdeState
	err := st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)
	runRecoverRole, hasRunRecoverRole := fdeSt.KeyslotRoles["run+recover"]
	c.Assert(hasRunRecoverRole, Equals, true)
	containerRole, hasContainerRole := runRecoverRole.Parameters["container-role"]
	c.Assert(hasContainerRole, Equals, true)

	c.Assert(containerRole.Models, HasLen, 1)
	c.Check(containerRole.Models[0].Model(), Equals, "mock-model")
	c.Check(containerRole.BootModes, DeepEquals, []string{"run"})
	c.Check(containerRole.TPM2PCRProfile, DeepEquals, secboot.SerializedPCRProfile(`"serialized-profile"`))
}

func (s *fdeMgrSuite) TestUpdateReseal(c *C) {
	st := s.st
	manager := s.startedManager(c)

	st.Lock()
	defer st.Unlock()
	foundManager := fdestate.FdeMgr(st)
	c.Check(foundManager, Equals, manager)

	unlocker := &instrumentedUnlocker{state: st}
	params := &boot.ResealKeyForBootChainsParams{}
	resealed := 0

	defer fdestate.MockBackendResealKeyForBootChains(func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		c.Check(unlocker.unlocked, Equals, 1)
		c.Check(unlocker.relocked, Equals, 0)
		c.Check(method, Equals, device.SealingMethodFDESetupHook)
		c.Check(rootdir, Equals, dirs.GlobalRootDir)
		c.Check(params, Equals, params)
		c.Check(expectReseal, Equals, expectReseal)
		updateState("run+recover", "container-role", []string{"run"}, []secboot.ModelForSealing{&mockModel{}}, []byte(`"serialized-profile"`))
		resealed += 1
		return nil
	})()

	err := boot.ResealKeyForBootChains(unlocker.Unlock, device.SealingMethodFDESetupHook, dirs.GlobalRootDir, params, false)
	c.Assert(err, IsNil)
	c.Check(unlocker.unlocked, Equals, 1)
	c.Check(unlocker.relocked, Equals, 1)
	c.Check(resealed, Equals, 1)

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)
	runRecoverRole, hasRunRecoverRole := fdeSt.KeyslotRoles["run+recover"]
	c.Assert(hasRunRecoverRole, Equals, true)
	containerRole, hasContainerRole := runRecoverRole.Parameters["container-role"]
	c.Assert(hasContainerRole, Equals, true)

	c.Assert(containerRole.Models, HasLen, 1)
	c.Check(containerRole.Models[0].Model(), Equals, "mock-model")
	c.Check(containerRole.BootModes, DeepEquals, []string{"run"})
	c.Check(containerRole.TPM2PCRProfile, DeepEquals, secboot.SerializedPCRProfile(`"serialized-profile"`))
}

func (s *fdeMgrSuite) TestEFIDBXNoSealedKeys(c *C) {
	// no sealed keys in the system, all operations are NOP

	st := s.st
	s.startedManager(c)

	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		panic("unexpected call")
	})()

	err := fdestate.EFISecureBootDBManagerStartup(st)
	c.Assert(err, IsNil)

	err = fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	c.Assert(err, IsNil)

	func() {
		st.Lock()
		defer st.Unlock()
		// make sure nothing was added to the state
		var fdeSt fdestate.FdeState
		err = st.Get("fde", &fdeSt)
		c.Assert(err, IsNil)
		c.Check(fdeSt.PendingExternalOperations, HasLen, 0)
	}()

	err = fdestate.EFISecureBootDBUpdateCleanup(st)
	c.Assert(err, IsNil)
}

func (s *fdeMgrSuite) TestEFIDBXStartupClean(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	s.startedManager(c)

	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		panic("unexpected call")
	})()

	err := fdestate.EFISecureBootDBManagerStartup(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(fdeSt.PendingExternalOperations, HasLen, 0)
}

func (s *fdeMgrSuite) TestEFIDBXPrepareHappy(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	s.startedManager(c)

	model := s.mockBootAssetsStateForModeenv(c)

	resealCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		resealCalls++
		c.Check(updateState, NotNil)
		c.Check(params.RunModeBootChains, HasLen, 1)
		c.Check(update, DeepEquals, []byte("payload"))

		// normally executed by the backend code
		c.Assert(updateState("run", "default", []string{"run"}, []secboot.ModelForSealing{model}, []byte("PCR-profile")),
			IsNil)
		return nil
	})()

	err := fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(resealCalls, Equals, 1)
	c.Check(fdeSt.PendingExternalOperations, HasLen, 1)
	c.Check(fdeSt.PendingExternalOperations[0], DeepEquals, fdestate.ExternalOperation{
		Kind:     "fde-efi-secureboot-db-update",
		ChangeID: "1",
		Context:  []byte(`{"payload":"cGF5bG9hZA=="}`),
	})
	c.Check(fdeSt.KeyslotRoles, DeepEquals, map[string]fdestate.KeyslotRoleInfo{
		"recover": {
			PrimaryKeyID:                   0,
			Parameters:                     nil,
			TPM2PCRPolicyRevocationCounter: 0x1880002,
		},
		"run": {
			PrimaryKeyID: 0, Parameters: map[string]fdestate.KeyslotRoleParameters{
				"default": {
					Models:         []fdestate.Model{fdestate.NewModel(model)},
					BootModes:      []string{"run"},
					TPM2PCRProfile: secboot.SerializedPCRProfile([]byte("PCR-profile")),
				},
			},
			TPM2PCRPolicyRevocationCounter: 0x1880001,
		},
		"run+recover": {
			PrimaryKeyID:                   0,
			Parameters:                     nil,
			TPM2PCRPolicyRevocationCounter: 0x1880001,
		},
	})

	// execute a single iteration of task runner, to have the task state switched to doing
	err = func() error {
		st.Unlock()
		defer st.Lock()
		return s.runner.Ensure()
	}()
	c.Assert(err, IsNil)

	// and we have change in the state
	chgs := st.Changes()
	c.Assert(chgs, HasLen, 1)
	chg := chgs[0]
	c.Check(chg.IsReady(), Equals, false)
	tsks := chg.Tasks()
	c.Assert(tsks, HasLen, 1)
	tsk := tsks[0]
	// task runner did not run yet
	c.Check(tsk.Status(), Equals, state.DoingStatus)
}

func (s *fdeMgrSuite) TestEFIDBXPrepareConflict(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	s.startedManager(c)

	s.mockBootAssetsStateForModeenv(c)

	resealCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		resealCalls++
		return nil
	})()

	err := fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(resealCalls, Equals, 1)
	c.Check(fdeSt.PendingExternalOperations, HasLen, 1)

	// running prepare again will cause a conflicts
	err = func() error {
		st.Unlock()
		defer st.Lock()
		return fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	}()
	c.Assert(err, ErrorMatches, "cannot add a new external operation when conflicting actions are in progress")

}

func (s *fdeMgrSuite) TestEFIDBXUpdateAndCleanupRunningAction(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	s.startedManager(c)

	model := s.mockBootAssetsStateForModeenv(c)

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			// normally executed by the backend code
			c.Assert(updateState("run", "default", []string{"run"}, []secboot.ModelForSealing{model}, []byte("PCR-profile-dbx-update")),
				IsNil)
			return nil
		})()

	defer fdestate.MockBackendResealKeyForBootChains(
		func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
			resealForBootChainsCalls++
			// normally executed by the backend code
			c.Assert(updateState("run", "default", []string{"run"}, []secboot.ModelForSealing{model}, []byte("PCR-profile-boot-chains")),
				IsNil)
			return nil
		})()

	err := fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 0)
	c.Check(fdeSt.PendingExternalOperations, HasLen, 1)
	c.Check(fdeSt.KeyslotRoles["run"], DeepEquals, fdestate.KeyslotRoleInfo{
		PrimaryKeyID: 0,
		Parameters: map[string]fdestate.KeyslotRoleParameters{
			"default": {
				Models:         []fdestate.Model{fdestate.NewModel(model)},
				BootModes:      []string{"run"},
				TPM2PCRProfile: secboot.SerializedPCRProfile([]byte("PCR-profile-dbx-update")),
			},
		},
		TPM2PCRPolicyRevocationCounter: 0x1880001,
	})

	// execute a single iteration of task runner, to have the task state switched to doing
	s.runnerIterationLocked(c)

	// and we have change in the state
	chgs := st.Changes()
	c.Assert(chgs, HasLen, 1)
	chg := chgs[0]
	c.Check(chg.IsReady(), Equals, false)

	// cleanup completes the change
	err = func() error {
		st.Unlock()
		defer st.Lock()
		return fdestate.EFISecureBootDBUpdateCleanup(st)
	}()
	c.Assert(err, IsNil)

	// post cleanup inspect
	var fdeStAfter fdestate.FdeState
	err = st.Get("fde", &fdeStAfter)
	c.Assert(err, IsNil)

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 1)
	c.Check(fdeStAfter.PendingExternalOperations, HasLen, 0)
	c.Check(fdeStAfter.KeyslotRoles["run"], DeepEquals, fdestate.KeyslotRoleInfo{
		PrimaryKeyID: 0,
		Parameters: map[string]fdestate.KeyslotRoleParameters{
			"default": {
				Models:         []fdestate.Model{fdestate.NewModel(model)},
				BootModes:      []string{"run"},
				TPM2PCRProfile: secboot.SerializedPCRProfile([]byte("PCR-profile-boot-chains")),
			},
		},
		TPM2PCRPolicyRevocationCounter: 0x1880001,
	})

	// and another iteration
	s.runnerIterationLocked(c)

	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)
}

func (s *fdeMgrSuite) TestEFIDBXUpdateAndUnexpectedStartupAction(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	s.startedManager(c)

	model := s.mockBootAssetsStateForModeenv(c)

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			// normally executed by the backend code
			c.Assert(updateState("run", "default", []string{"run"}, []secboot.ModelForSealing{model}, []byte("PCR-profile-dbx-update")),
				IsNil)
			return nil
		})()

	defer fdestate.MockBackendResealKeyForBootChains(
		func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
			resealForBootChainsCalls++
			// normally executed by the backend code
			c.Assert(updateState("run", "default", []string{"run"}, []secboot.ModelForSealing{model}, []byte("PCR-profile-boot-chains-startup")),
				IsNil)
			return nil
		})()

	err := fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 0)
	c.Check(fdeSt.PendingExternalOperations, HasLen, 1)
	c.Check(fdeSt.KeyslotRoles["run"], DeepEquals, fdestate.KeyslotRoleInfo{
		PrimaryKeyID: 0,
		Parameters: map[string]fdestate.KeyslotRoleParameters{
			"default": {
				Models:         []fdestate.Model{fdestate.NewModel(model)},
				BootModes:      []string{"run"},
				TPM2PCRProfile: secboot.SerializedPCRProfile([]byte("PCR-profile-dbx-update")),
			},
		},
		TPM2PCRPolicyRevocationCounter: 0x1880001,
	})

	// execute a single iteration of task runner, to have the task state switched to doing
	s.runnerIterationLocked(c)

	// and we have change in the state
	chgs := st.Changes()
	c.Assert(chgs, HasLen, 1)
	chg := chgs[0]
	c.Check(chg.IsReady(), Equals, false)
	tsks := chg.Tasks()
	c.Assert(tsks, HasLen, 1)
	tsk := tsks[0]

	// startup aborts the change and reseals with current boot chains
	err = func() error {
		st.Unlock()
		defer st.Lock()
		return fdestate.EFISecureBootDBManagerStartup(st)
	}()
	c.Assert(err, IsNil)

	// post cleanup inspect
	var fdeStAfter fdestate.FdeState
	err = st.Get("fde", &fdeStAfter)
	c.Assert(err, IsNil)

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 1)
	c.Check(fdeStAfter.PendingExternalOperations, HasLen, 0)
	c.Check(fdeStAfter.KeyslotRoles["run"], DeepEquals, fdestate.KeyslotRoleInfo{
		PrimaryKeyID: 0,
		Parameters: map[string]fdestate.KeyslotRoleParameters{
			"default": {
				Models:         []fdestate.Model{fdestate.NewModel(model)},
				BootModes:      []string{"run"},
				TPM2PCRProfile: secboot.SerializedPCRProfile([]byte("PCR-profile-boot-chains-startup")),
			},
		},
		TPM2PCRPolicyRevocationCounter: 0x1880001,
	})

	// and another iteration
	s.runnerIterationLocked(c)

	// change has an error now
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n"+
		"- External EFI secure boot key database update .startup action invoked while a change is in progress.")
	c.Check(tsk.Status(), Equals, state.ErrorStatus)
}

func (s *fdeMgrSuite) TestEFIDBXUpdateResealFailedAborts(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	s.startedManager(c)

	s.mockBootAssetsStateForModeenv(c)

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			return fmt.Errorf("mock error")
		})()

	err := fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	c.Assert(err, ErrorMatches, "cannot reseal keys for DBX update: mock error")

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 0)
	c.Check(fdeSt.PendingExternalOperations, HasLen, 0)

	// execute a single iteration of task runner, to have the change state updated
	s.runnerIterationLocked(c)

	// and we have change in the state, but it is in an error status already
	chgs := st.Changes()
	c.Assert(chgs, HasLen, 1)
	chg := chgs[0]
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n"+
		"- External EFI secure boot key database update .initial reseal failed: mock error.")
}

func (s *fdeMgrSuite) TestEFIDBXCleanupNoChange(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	s.startedManager(c)

	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		panic("unexpected call")
	})()

	err := fdestate.EFISecureBootDBUpdateCleanup(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(fdeSt.PendingExternalOperations, HasLen, 0)
}

func createMockGrubCfg(baseDir string) error {
	cfg := filepath.Join(baseDir, "EFI/ubuntu/grub.cfg")
	if err := os.MkdirAll(filepath.Dir(cfg), 0755); err != nil {
		return err
	}
	return os.WriteFile(cfg, []byte("# Snapd-Boot-Config-Edition: 1\n"), 0644)
}

func (s *fdeMgrSuite) mockBootAssetsStateForModeenv(c *C) *asserts.Model {
	model := boottest.MakeMockUC20Model()

	rootdir := dirs.GlobalRootDir

	modeenv := &boot.Modeenv{
		Mode: "run",

		// no recovery systems to keep things relatively short
		//
		CurrentTrustedRecoveryBootAssets: map[string][]string{
			"grubx64.efi": {"grub-hash"},
			"bootx64.efi": {"shim-hash"},
		},

		CurrentTrustedBootAssets: map[string][]string{
			"grubx64.efi": {"run-grub-hash"},
		},

		CurrentKernels: []string{"pc-kernel_500.snap"},

		CurrentKernelCommandLines: []string{
			"snapd_recovery_mode=run console=ttyS0 console=tty1 panic=-1",
		},
		Model:          model.Model(),
		BrandID:        model.BrandID(),
		Grade:          string(model.Grade()),
		ModelSignKeyID: model.SignKeyID(),
	}

	c.Assert(modeenv.WriteTo(rootdir), IsNil)

	err := createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-seed"))
	c.Assert(err, IsNil)

	err = createMockGrubCfg(filepath.Join(rootdir, "run/mnt/ubuntu-boot"))
	c.Assert(err, IsNil)

	collectAssetHashes := func(bmaps ...map[string][]string) []string {
		uniqAssetsHashes := map[string]bool{}

		for _, bm := range bmaps {
			for bl, hashes := range bm {
				for _, h := range hashes {
					uniqAssetsHashes[fmt.Sprintf("%s-%s", bl, h)] = true
				}
			}
		}

		l := make([]string, 0, len(uniqAssetsHashes))
		for h := range uniqAssetsHashes {
			l = append(l, h)
		}

		c.Logf("assets: %v", l)
		return l
	}

	// mock asset cache
	boottest.MockAssetsCache(c, rootdir, "grub",
		collectAssetHashes(modeenv.CurrentTrustedBootAssets, modeenv.CurrentTrustedRecoveryBootAssets))

	const gadgetSnapYaml = `name: gadget
version: 1.0
type: gadget
`

	restore := boot.MockSeedReadSystemEssential(func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error) {
		return model, []*seed.Snap{
			boottest.MockNamedKernelSeedSnap(snap.R(1), "pc-kernel"),
			boottest.MockGadgetSeedSnap(c, gadgetSnapYaml, nil),
		}, nil
	})
	s.AddCleanup(restore)

	return model
}

func (s *fdeMgrSuite) TestEFIDBXBlockedTasks(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	s.startedManager(c)

	st.Lock()
	defer st.Unlock()

	chg1 := st.NewChange("fde-efi-secureboot-db-update", "EFI secure boot key database update 1")
	t1 := st.NewTask("efi-secureboot-db-update", "External EFI secure boot key database update")
	chg1.AddTask(t1)

	chg2 := st.NewChange("fde-efi-secureboot-db-update", "EFI secure boot key database update 2")
	t2 := st.NewTask("efi-secureboot-db-update", "External EFI secure boot key database update")
	chg2.AddTask(t2)

	// execute a single iteration of task runner, to have the change state updated
	s.runnerIterationLocked(c)

	opts := []state.Status{t1.Status(), t2.Status()}

	sort.Slice(opts, func(i, j int) bool {
		return opts[i] < opts[j]
	})
	// only one task is running
	c.Check(opts, DeepEquals, []state.Status{state.DoStatus, state.DoingStatus})
}
