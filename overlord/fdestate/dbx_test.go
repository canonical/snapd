// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func (s *fdeMgrSuite) TestEFIDBXNoSealedKeys(c *C) {
	// no sealed keys in the system, all operations are NOP

	st := s.st
	onClassic := true
	s.startedManager(c, onClassic)

	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
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
	onClassic := true
	s.startedManager(c, onClassic)

	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
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
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model)

	resealCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		resealCalls++
		c.Check(mgr, NotNil)
		c.Check(params.RunModeBootChains, HasLen, 1)
		c.Check(update, DeepEquals, []byte("payload"))

		// normally executed by the backend code
		c.Check(mgr.Update("run", "default", &backend.SealingParameters{
			BootModes:     []string{"run"},
			Models:        []secboot.ModelForSealing{model},
			TpmPCRProfile: []byte("PCR-profile"),
		}), IsNil)
		return nil
	})()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

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
		Context:  []byte(`{"payload":"cGF5bG9hZA==","sealing-method":"tpm"}`),
		Status:   fdestate.DoingStatus,
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
					Models:         []*fdestate.Model{fdestate.NewModel(model)},
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
	c.Assert(tsks, HasLen, 2)
	c.Check(tsks[0].Kind(), Equals, "efi-secureboot-db-update-prepare")
	c.Check(tsks[0].Status(), Equals, state.DoneStatus)
	c.Check(tsks[1].Kind(), Equals, "efi-secureboot-db-update")
	c.Check(tsks[1].Status(), Equals, state.DoStatus)
	c.Check(tsks[1].WaitTasks(), DeepEquals, []*state.Task{tsks[0]})
}

func (s *fdeMgrSuite) TestEFIDBXPrepareConflictSelf(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model)

	resealCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		resealCalls++
		return nil
	})()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

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
	c.Assert(err, DeepEquals, &snapstate.ChangeConflictError{
		ChangeKind: "fde-efi-secureboot-db-update",
		Message:    "cannot start a new DBX update when conflicting actions are in progress",
	})
}

func (s *fdeMgrSuite) TestEFIDBXPrepareConflictOperationNotInDoingYet(c *C) {
	// attempting to run cleanup or startup when the operation has not yet
	// reached Doing status raises a conflict
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model)

	resealCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		resealCalls++
		return nil
	})()

	st.Lock()
	defer st.Unlock()

	// mock an operation which has just been added in prepare, but the initial reseal has not yet completed
	c.Assert(fdestate.AddExternalOperation(st, &fdestate.ExternalOperation{
		ChangeID: "1234",
		Kind:     "fde-efi-secureboot-db-update",
		Status:   fdestate.PreparingStatus,
	}), IsNil)

	st.Unlock()
	defer st.Lock()
	err := fdestate.EFISecureBootDBUpdateCleanup(st)
	c.Assert(err, DeepEquals, &snapstate.ChangeConflictError{
		ChangeKind: "fde-efi-secureboot-db-update",
		Message:    "cannot perform DBX update 'cleanup' action when conflicting actions are in progress",
	})

	err = fdestate.EFISecureBootDBManagerStartup(st)
	c.Assert(err, DeepEquals, &snapstate.ChangeConflictError{
		ChangeKind: "fde-efi-secureboot-db-update",
		Message:    "cannot perform DBX update 'startup' action when conflicting actions are in progress",
	})
}

func (s *fdeMgrSuite) TestEFIDBXPrepareConflictSnapChanges(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model)

	defer testutil.Mock(&snapstate.EnforcedValidationSets, func(st *state.State, extraVss ...*asserts.ValidationSet) (*snapasserts.ValidationSets, error) {
		return nil, nil
	})()

	st.Lock()
	defer st.Unlock()
	snapstate.Set(st, model.Kernel(), &snapstate.SnapState{
		Active: true,
		Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{
			{RealName: model.Kernel(), Revision: snap.R(1)},
			{RealName: model.Kernel(), Revision: snap.R(2)},
		}),
		Current:  snap.R(2),
		SnapType: "kernel",
	})

	chg := st.NewChange("kernel-snap-remove", "...")
	rmTasks, err := snapstate.Remove(st, model.Kernel(), snap.R(1), nil)
	c.Assert(err, IsNil)
	c.Assert(rmTasks, NotNil)
	chg.AddAll(rmTasks)

	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		c.Fatalf("unexpected call")
		return fmt.Errorf("unexpected call")
	})()

	st.Unlock()
	defer st.Lock()
	err = fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	c.Assert(err, DeepEquals, &snapstate.ChangeConflictError{
		ChangeKind: "kernel-snap-remove",
		Snap:       "pc-kernel",
		ChangeID:   "1",
	})
}

func (s *fdeMgrSuite) TestEFIDBXUpdateAndCleanupRunningAction(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model)

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			// normally executed by the backend code
			c.Assert(mgr.Update("run", "default", &backend.SealingParameters{
				BootModes:     []string{"run"},
				Models:        []secboot.ModelForSealing{model},
				TpmPCRProfile: []byte("PCR-profile-dbx-update"),
			}), IsNil)
			return nil
		})()

	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
			resealForBootChainsCalls++
			// normally executed by the backend code
			c.Assert(mgr.Update("run", "default", &backend.SealingParameters{
				BootModes:     []string{"run"},
				Models:        []secboot.ModelForSealing{model},
				TpmPCRProfile: []byte("PCR-profile-boot-chains"),
			}), IsNil)
			return nil
		})()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

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
				Models:         []*fdestate.Model{fdestate.NewModel(model)},
				BootModes:      []string{"run"},
				TPM2PCRProfile: secboot.SerializedPCRProfile([]byte("PCR-profile-dbx-update")),
			},
		},
		TPM2PCRPolicyRevocationCounter: 0x1880001,
	})

	// execute a single iteration of task runner, to have the task state switched to doing
	//s.runnerIterationLocked(c)

	// and we have change in the state
	chgs := st.Changes()
	c.Assert(chgs, HasLen, 1)
	chg := chgs[0]
	c.Check(chg.IsReady(), Equals, false)

	st.Unlock()
	defer st.Lock()

	// cleanup completes the change, and waits internally for change to become ready
	err = fdestate.EFISecureBootDBUpdateCleanup(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()
	// post cleanup inspect
	var fdeStAfter fdestate.FdeState
	err = st.Get("fde", &fdeStAfter)
	c.Assert(err, IsNil)

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 1)
	// task cleanup may have run
	if l := len(fdeStAfter.PendingExternalOperations); l == 1 {
		c.Check(fdeStAfter.PendingExternalOperations[0].Status, Equals, fdestate.DoneStatus)
	} else if l != 0 {
		c.Fatalf("unexpected number of pending external operations: %v", l)
	}

	c.Check(fdeStAfter.KeyslotRoles["run"], DeepEquals, fdestate.KeyslotRoleInfo{
		PrimaryKeyID: 0,
		Parameters: map[string]fdestate.KeyslotRoleParameters{
			"default": {
				Models:         []*fdestate.Model{fdestate.NewModel(model)},
				BootModes:      []string{"run"},
				TPM2PCRProfile: secboot.SerializedPCRProfile([]byte("PCR-profile-boot-chains")),
			},
		},
		TPM2PCRPolicyRevocationCounter: 0x1880001,
	})

	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.IsClean(), Equals, false)
	c.Check(chg.Status(), Equals, state.DoneStatus)
	c.Check(chg.Err(), IsNil)

	st.Unlock()
	// wait for change to become clean
	iterateUnlockedStateWaitingFor(st, chg.IsClean)
	st.Lock()

	var fdeStAfterCleanup fdestate.FdeState
	err = st.Get("fde", &fdeStAfterCleanup)
	c.Assert(err, IsNil)
	// operation has been dropped from the state in cleanup
	c.Check(fdeStAfterCleanup.PendingExternalOperations, HasLen, 0)
}

func (s *fdeMgrSuite) TestEFIDBXUpdateAndUnexpectedStartupAction(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model)

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			// normally executed by the backend code
			c.Assert(mgr.Update("run", "default", &backend.SealingParameters{
				BootModes:     []string{"run"},
				Models:        []secboot.ModelForSealing{model},
				TpmPCRProfile: []byte("PCR-profile-dbx-update"),
			}), IsNil)
			return nil
		})()

	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
			resealForBootChainsCalls++
			// normally executed by the backend code
			c.Assert(mgr.Update("run", "default", &backend.SealingParameters{
				BootModes:     []string{"run"},
				Models:        []secboot.ModelForSealing{model},
				TpmPCRProfile: []byte("PCR-profile-boot-chains-startup"),
			}), IsNil)
			return nil
		})()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

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
				Models:         []*fdestate.Model{fdestate.NewModel(model)},
				BootModes:      []string{"run"},
				TPM2PCRProfile: secboot.SerializedPCRProfile([]byte("PCR-profile-dbx-update")),
			},
		},
		TPM2PCRPolicyRevocationCounter: 0x1880001,
	})

	// and we have change in the state
	chgs := st.Changes()
	c.Assert(chgs, HasLen, 1)
	chg := chgs[0]
	c.Check(chg.IsReady(), Equals, false)
	tsks := chg.Tasks()
	c.Assert(tsks, HasLen, 2)
	tsk := tsks[1]
	c.Assert(tsk.Kind(), Equals, "efi-secureboot-db-update")
	c.Assert(tsk.Status(), Equals, state.DoStatus)

	st.Unlock()
	defer st.Lock()

	// startup aborts the change and reseals with current boot chains, waits for
	// change to be complete
	err = fdestate.EFISecureBootDBManagerStartup(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()
	// post cleanup inspect
	var fdeStAfter fdestate.FdeState
	err = st.Get("fde", &fdeStAfter)
	c.Assert(err, IsNil)

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 1)
	c.Assert(fdeStAfter.PendingExternalOperations, HasLen, 1)
	c.Check(fdeStAfter.PendingExternalOperations[0].Status, Equals, fdestate.ErrorStatus)
	c.Check(fdeStAfter.KeyslotRoles["run"], DeepEquals, fdestate.KeyslotRoleInfo{
		PrimaryKeyID: 0,
		Parameters: map[string]fdestate.KeyslotRoleParameters{
			"default": {
				Models:         []*fdestate.Model{fdestate.NewModel(model)},
				BootModes:      []string{"run"},
				TPM2PCRProfile: secboot.SerializedPCRProfile([]byte("PCR-profile-boot-chains-startup")),
			},
		},
		TPM2PCRPolicyRevocationCounter: 0x1880001,
	})

	// change has an error now
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.IsClean(), Equals, false)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n"+
		"- Reseal after external EFI DBX update .'startup' action invoked while an operation is in progress.")
	c.Check(tsk.Status(), Equals, state.ErrorStatus)

	// this should return immediately, as the operation has completed
	st.Unlock()
	c.Logf("-- wait ready")
	<-chg.Ready()
	st.Lock()

	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.IsReady(), Equals, true)

	st.Unlock()
	// wait for change to become clean
	iterateUnlockedStateWaitingFor(st, chg.IsClean)
	st.Lock()

	var fdeStAfterCleanup fdestate.FdeState
	err = st.Get("fde", &fdeStAfterCleanup)
	c.Assert(err, IsNil)
	// operation has been dropped from the state during cleanup
	c.Check(fdeStAfterCleanup.PendingExternalOperations, HasLen, 0)
}

func (s *fdeMgrSuite) TestEFIDBXUpdateAbort(c *C) {
	// simulate a case when prepare is requested, but neither cleanup nor
	// startup is called, the change will wait till it is auto aborted

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model)

	resealForDBUpdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUpdateCalls++
			return nil
		})()

	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
			resealForBootChainsCalls++
			return nil
		})()

	// the loop is running now
	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

	err := fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(resealForDBUpdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 0)
	c.Check(fdeSt.PendingExternalOperations, HasLen, 1)
	c.Check(fdeSt.PendingExternalOperations[0].Status, Equals, fdestate.DoingStatus)

	// and we have change in the state
	chgs := st.Changes()
	c.Assert(chgs, HasLen, 1)
	chg := chgs[0]
	c.Check(chg.IsReady(), Equals, false)
	tsks := chg.Tasks()
	c.Assert(tsks, HasLen, 2)
	c.Assert(tsks[0].Kind(), Equals, "efi-secureboot-db-update-prepare")
	c.Assert(tsks[1].Kind(), Equals, "efi-secureboot-db-update")

	st.Unlock()
	defer st.Lock()

	iterateUnlockedStateWaitingFor(st, func() bool {
		return tsks[0].Status() == state.DoneStatus
	})

	st.Lock()
	chg.Abort()
	st.EnsureBefore(0)
	st.Unlock()

	c.Logf("-- wait ready")
	<-chg.Ready()
	c.Logf("-- ready")

	st.Lock()
	defer st.Unlock()

	// change has been undone
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.IsClean(), Equals, false)
	c.Check(chg.Status(), Equals, state.UndoneStatus)
	c.Check(tsks[0].Status(), Equals, state.UndoneStatus)
	c.Check(tsks[1].Status(), Equals, state.HoldStatus)

	// post cleanup inspect
	var fdeStAfter fdestate.FdeState
	err = st.Get("fde", &fdeStAfter)
	c.Assert(err, IsNil)

	// no more reseal for update calls
	c.Check(resealForDBUpdateCalls, Equals, 1)
	// but one post-update reseal
	c.Check(resealForBootChainsCalls, Equals, 1)
	c.Check(fdeStAfter.PendingExternalOperations, HasLen, 1)
	c.Check(fdeStAfter.PendingExternalOperations[0].Status, Equals, fdestate.ErrorStatus)

	st.Unlock()
	// wait for change to become clean
	iterateUnlockedStateWaitingFor(st, chg.IsClean)
	st.Lock()

	var fdeStAfterCleanup fdestate.FdeState
	err = st.Get("fde", &fdeStAfterCleanup)
	c.Assert(err, IsNil)
	c.Check(fdeStAfterCleanup.PendingExternalOperations, HasLen, 0)
}

func (s *fdeMgrSuite) TestEFIDBXUpdateResealFailedAborts(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model)

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			return fmt.Errorf("mock error")
		})()
	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
			resealForBootChainsCalls++
			return nil
		})()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

	err := fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	c.Assert(err, ErrorMatches, "(?sm).*cannot perform initial reseal of keys for DBX update: mock error.*")

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 0)
	// depending on whether cleanup ran, the there can either be one or no
	// operations in the state
	if l := len(fdeSt.PendingExternalOperations); l == 1 {
		c.Check(fdeSt.PendingExternalOperations[0].Status, Equals, fdestate.ErrorStatus)
	} else if l > 1 {
		c.Fatalf("unexpected number of operations in the state: %v", l)
	}

	// and we have change in the state, but it is in an error status already
	chgs := st.Changes()
	c.Assert(chgs, HasLen, 1)
	chg := chgs[0]
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n"+
		"- Prepare for external EFI DBX update .cannot perform initial reseal of keys for DBX update: mock error.")
}

func (s *fdeMgrSuite) TestEFIDBXUpdatePostUpdateResealFailed(c *C) {
	// mock an error in a reseal which happens in the 'do' handler after snapd
	// has been notified of a completed update
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model)

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			return nil
		})()
	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
			resealForBootChainsCalls++
			return fmt.Errorf("mock error")
		})()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

	err := fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 0)

	st.Unlock()
	defer st.Lock()

	// cleanup triggers post update reseal
	err = fdestate.EFISecureBootDBUpdateCleanup(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 1)

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)
	// depending on whether cleanup ran, the there can either be one or no
	// operations in the state
	if l := len(fdeSt.PendingExternalOperations); l == 1 {
		c.Check(fdeSt.PendingExternalOperations[0].Status, Equals, fdestate.ErrorStatus)
	} else if l > 1 {
		c.Fatalf("unexpected number of operations in the state: %v", l)
	}

	// and we have change in the state, but it is in an error status already
	chgs := st.Changes()
	c.Assert(chgs, HasLen, 1)
	chg := chgs[0]
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n"+
		// error logged in the task
		"- Reseal after external EFI DBX update .cannot complete post update reseal: mock error.\n"+
		// actual error
		"- Reseal after external EFI DBX update .mock error.")
}

func (s *fdeMgrSuite) TestEFIDBXUpdateUndoResealFails(c *C) {
	// mock an error in a reseal which happens in the 'undo' path after snapd
	// has been notified of a restart in the external DBX manager process
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model)

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			return nil
		})()

	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
			resealForBootChainsCalls++
			return fmt.Errorf("mock error")
		})()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

	err := fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 0)

	st.Unlock()
	defer st.Lock()

	// 'external' DBX manger restarted
	err = fdestate.EFISecureBootDBManagerStartup(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()
	// post cleanup inspect
	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 1)

	// task cleanup may have run
	if l := len(fdeSt.PendingExternalOperations); l == 1 {
		c.Check(fdeSt.PendingExternalOperations[0].Status, Equals, fdestate.ErrorStatus)
	} else if l > 1 {
		c.Fatalf("unexpected number of operations in the state: %v", l)
	}

	// and we have change in the state, but it is in an error status already
	chgs := st.Changes()
	c.Assert(chgs, HasLen, 1)
	chg := chgs[0]
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	c.Check(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n"+
		// undo failure
		"- Prepare for external EFI DBX update .cannot complete reseal in undo: mock error.\n"+
		"- Reseal after external EFI DBX update .'startup' action invoked while an operation is in progress.")
}

func (s *fdeMgrSuite) TestEFIDBXCleanupNoChange(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	s.startedManager(c, onClassic)

	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
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
	onClassic := true
	s.startedManager(c, onClassic)

	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("fde-efi-secureboot-db-update", "EFI secure boot key database update 1")
	tsk := st.NewTask("efi-secureboot-db-update", "External EFI secure boot key database update")
	chg.AddTask(tsk)

	op := &fdestate.ExternalOperation{
		Kind:     "fde-efi-secureboot-db-update",
		ChangeID: chg.ID(),
		Status:   fdestate.DoingStatus,
	}
	c.Assert(fdestate.AddExternalOperation(st, op), IsNil)

	c.Check(fdestate.IsEFISecurebootDBUpdateBlocked(tsk), Equals, true)

	// execute a single iteration of task runner
	s.runnerIterationLocked(c)
	c.Check(tsk.Status(), Equals, state.DoStatus)

	// now unblock it
	op.SetStatus(fdestate.CompletingStatus)
	c.Assert(fdestate.UpdateExternalOperation(st, op), IsNil)

	c.Check(fdestate.IsEFISecurebootDBUpdateBlocked(tsk), Equals, false)

	// execute a single iteration of task runner
	s.runnerIterationLocked(c)
	c.Check(tsk.Status(), Equals, state.DoingStatus)

	st.Unlock()
	iterateUnlockedStateWaitingFor(st, chg.IsReady)
	st.Lock()
}

func (s *fdeMgrSuite) TestEFIDBXOperationAddWait(c *C) {
	// add 2 changes, ant exercise the notification mechanism
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	s.startedManager(c, onClassic)

	st.Lock()
	defer st.Unlock()

	op1, err := fdestate.AddEFISecurebootDBUpdateChange(st, device.SealingMethodTPM, []byte("payload 1"))
	c.Assert(err, IsNil)

	op2, err := fdestate.AddEFISecurebootDBUpdateChange(st, device.SealingMethodTPM, []byte("payload 2"))
	c.Assert(err, IsNil)

	sync1PreparedC := fdestate.DbxUpdatePreparedOKChan(st, op1.ChangeID)
	sync2PreparedC := fdestate.DbxUpdatePreparedOKChan(st, op2.ChangeID)

	syncC := make(chan struct{})
	defer close(syncC)
	doneC := make(chan struct{})

	go func() {
		<-syncC
		st.Lock()
		fdestate.NotifyDBXUpdatePrepareDoneOK(st, op1.ChangeID)
		st.Unlock()

		<-syncC
		st.Lock()
		fdestate.NotifyDBXUpdatePrepareDoneOK(st, op2.ChangeID)
		st.Unlock()

		close(doneC)
	}()

	st.Unlock()
	defer st.Lock()
	syncC <- struct{}{}
	<-sync1PreparedC
	syncC <- struct{}{}
	<-sync2PreparedC
	<-doneC
}

func (s *fdeMgrSuite) TestEFIDBXUpdateAffectedSnaps(c *C) {
	// add 2 changes, ant exercise the notification mechanism
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	s.startedManager(c, onClassic)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model)

	st.Lock()
	defer st.Unlock()

	tsk := st.NewTask("foo", "foo task")

	names, err := fdestate.DbxUpdateAffectedSnaps(tsk)
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{
		"pc",        // gadget
		"pc-kernel", // kernel
		"core20",    // base
	})
}

func (s *fdeMgrSuite) TestEFIDBXConflictingSnaps(c *C) {
	// mock an error in a reseal which happens in the 'undo' path after snapd
	// has been notified of a restart in the external DBX manager process
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model)

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			return nil
		})()

	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
			resealForBootChainsCalls++
			return fmt.Errorf("mock error")
		})()

	st.Lock()
	st.Set("seeded", true)
	st.Unlock()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

	err := fdestate.EFISecureBootDBUpdatePrepare(st, fdestate.EFISecurebootDBX, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 0)

	gadgetSnapYamlContent := fmt.Sprintf(`
name: %s
version: "1.0"
type: gadget
`[1:], model.Gadget())
	kernelSnapYamlContent := fmt.Sprintf(`
name: %s
version: "1.0"
type: kernel
`[1:], model.Kernel())
	baseSnapYamlContent := fmt.Sprintf(`
name: %s
version: "1.0"
type: base
`[1:], model.Base())
	appSnapYamlContent := `
name: apps
version: "1.0"
type: app
`[1:]

	for _, sn := range []struct {
		snapYaml   string
		name       string
		noConflict bool
	}{
		{snapYaml: gadgetSnapYamlContent, name: model.Gadget()},
		{snapYaml: kernelSnapYamlContent, name: model.Kernel()},
		{snapYaml: baseSnapYamlContent, name: model.Base()},
		{snapYaml: appSnapYamlContent, name: "apps", noConflict: true},
	} {
		c.Logf("checking snap %s:\n%s", sn.name, sn.snapYaml)
		path := snaptest.MakeTestSnapWithFiles(c, sn.snapYaml, nil)

		_, _, err = snapstate.InstallPath(st, &snap.SideInfo{
			RealName: sn.name,
		}, path, "", "", snapstate.Flags{}, nil)

		if !sn.noConflict {
			c.Check(err, ErrorMatches, fmt.Sprintf(`snap %q has \"fde-efi-secureboot-db-update\" change in progress`, sn.name))
		} else {
			c.Check(err, IsNil)
		}
	}

}

func iterateUnlockedStateWaitingFor(st *state.State, pred func() bool) {
	ok := false
	for !ok {
		st.Lock()
		ok = pred()
		if !ok {
			st.EnsureBefore(0)
		}
		st.Unlock()
	}
}
