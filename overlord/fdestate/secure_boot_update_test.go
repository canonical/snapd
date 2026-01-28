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
	"errors"
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

func (s *fdeMgrSuite) testEFISecurebootNoSealedKeysForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	// no sealed keys in the system, all operations are NOP
	_, err := device.SealedKeysMethod(dirs.GlobalRootDir)
	// make sure the state is true
	c.Assert(err, Equals, device.ErrNoSealedKeys)

	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		panic("unexpected call")
	})()

	st := s.st
	// make sure there is no fde state
	func() {
		st.Lock()
		defer st.Unlock()
		// make sure nothing was added to the state
		var fdeSt fdestate.FdeState
		err = st.Get("fde", &fdeSt)
		c.Assert(errors.Is(err, state.ErrNoState), Equals, true)
	}()

	err = fdestate.EFISecurebootDBManagerStartup(st)
	c.Assert(err, IsNil)

	err = fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
	c.Assert(err, IsNil)

	err = fdestate.EFISecurebootDBUpdateCleanup(st)
	c.Assert(err, IsNil)

}

func (s *fdeMgrSuite) TestEFISecurebootNoSealedKeysPK(c *C) {
	s.testEFISecurebootNoSealedKeysForKind(c, fdestate.EFISecurebootPK)
}

func (s *fdeMgrSuite) TestEFISecurebootNoSealedKeysKEK(c *C) {
	s.testEFISecurebootNoSealedKeysForKind(c, fdestate.EFISecurebootKEK)
}

func (s *fdeMgrSuite) TestEFISecurebootNoSealedKeysDB(c *C) {
	s.testEFISecurebootNoSealedKeysForKind(c, fdestate.EFISecurebootDB)
}

func (s *fdeMgrSuite) TestEFISecurebootNoSealedKeysDBX(c *C) {
	s.testEFISecurebootNoSealedKeysForKind(c, fdestate.EFISecurebootDBX)
}

func (s *fdeMgrSuite) TestEFISecurebootStartupClean(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	s.startedManager(c, onClassic)

	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		panic("unexpected call")
	})()

	err := fdestate.EFISecurebootDBManagerStartup(st)
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(fdeSt.PendingExternalOperations, HasLen, 0)
}

func (s *fdeMgrSuite) testEFISecurebootPrepareHappyForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)
	fdemgr.DeviceInitialized()

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	resealCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		resealCalls++
		c.Check(mgr, NotNil)
		c.Check(params.Options.RevokeOldKeys, Equals, false)
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

	err := fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(resealCalls, Equals, 1)
	c.Assert(fdeSt.PendingExternalOperations, HasLen, 1)
	c.Check(fdeSt.PendingExternalOperations[0], DeepEquals, fdestate.ExternalOperation{
		Kind:     "fde-efi-secureboot-db-update",
		ChangeID: "1",
		Context: []byte(
			fmt.Sprintf(`{"payload":"cGF5bG9hZA==","sealing-method":"tpm","db":%d}`, kind)),
		Status: fdestate.DoingStatus,
	})
	c.Check(fdeSt.KeyslotRoles, DeepEquals, map[string]fdestate.KeyslotRoleInfo{
		"recover": {
			PrimaryKeyID:                   0,
			Parameters:                     nil,
			TPM2PCRPolicyRevocationCounter: 42,
		},
		"run": {
			PrimaryKeyID: 0, Parameters: map[string]fdestate.KeyslotRoleParameters{
				"default": {
					Models:         []*fdestate.Model{fdestate.NewModel(model)},
					BootModes:      []string{"run"},
					TPM2PCRProfile: secboot.SerializedPCRProfile([]byte("PCR-profile")),
				},
			},
			TPM2PCRPolicyRevocationCounter: 41,
		},
		"run+recover": {
			PrimaryKeyID:                   0,
			Parameters:                     nil,
			TPM2PCRPolicyRevocationCounter: 41,
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

func (s *fdeMgrSuite) TestEFISecurebootPrepareHappyPK(c *C) {
	s.testEFISecurebootPrepareHappyForKind(c, fdestate.EFISecurebootPK)
}
func (s *fdeMgrSuite) TestEFISecurebootPrepareHappyKEK(c *C) {
	s.testEFISecurebootPrepareHappyForKind(c, fdestate.EFISecurebootKEK)
}
func (s *fdeMgrSuite) TestEFISecurebootPrepareHappyDB(c *C) {
	s.testEFISecurebootPrepareHappyForKind(c, fdestate.EFISecurebootDB)
}
func (s *fdeMgrSuite) TestEFISecurebootPrepareHappyDBX(c *C) {
	s.testEFISecurebootPrepareHappyForKind(c, fdestate.EFISecurebootDBX)
}

func (s *fdeMgrSuite) testEFISecurebootPrepareConflictSelfForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)
	fdemgr.DeviceInitialized()

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	resealCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		resealCalls++
		c.Check(params.Options.RevokeOldKeys, Equals, false)
		return nil
	})()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

	err := fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(resealCalls, Equals, 1)
	c.Assert(fdeSt.PendingExternalOperations, HasLen, 1)

	// running prepare again will cause a conflicts
	err = func() error {
		st.Unlock()
		defer st.Lock()
		return fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
	}()
	c.Assert(err, DeepEquals, &snapstate.ChangeConflictError{
		ChangeKind: "fde-efi-secureboot-db-update",
		Message: fmt.Sprintf(
			"cannot start a new %s update when conflicting actions are in progress",
			kind.String(),
		),
	})
}

func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictSelfPK(c *C) {
	s.testEFISecurebootPrepareConflictSelfForKind(c, fdestate.EFISecurebootPK)
}
func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictSelfKEK(c *C) {
	s.testEFISecurebootPrepareConflictSelfForKind(c, fdestate.EFISecurebootKEK)
}
func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictSelfDB(c *C) {
	s.testEFISecurebootPrepareConflictSelfForKind(c, fdestate.EFISecurebootDB)
}
func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictSelfDBX(c *C) {
	s.testEFISecurebootPrepareConflictSelfForKind(c, fdestate.EFISecurebootDBX)
}

func (s *fdeMgrSuite) testEFISecurebootConflictFDEChangeForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	s.startedManager(c, onClassic)

	st.Lock()
	// mock conflicting FDE change
	chg := st.NewChange("some-fde-change", "")
	tsk := st.NewTask("fde-op", "")
	chg.AddTask(tsk)
	st.Unlock()

	err := fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
	c.Assert(err, ErrorMatches, "FDE change in progress, no other FDE changes allowed until this is done")
}

func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictFDEChangePK(c *C) {
	s.testEFISecurebootConflictFDEChangeForKind(c, fdestate.EFISecurebootPK)
}

func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictFDEChangeKEK(c *C) {
	s.testEFISecurebootConflictFDEChangeForKind(c, fdestate.EFISecurebootKEK)
}

func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictFDEChangeDB(c *C) {
	s.testEFISecurebootConflictFDEChangeForKind(c, fdestate.EFISecurebootDB)
}

func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictFDEChangeDBX(c *C) {
	s.testEFISecurebootConflictFDEChangeForKind(c, fdestate.EFISecurebootDBX)
}

func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictOperationNotInDoingYet(c *C) {
	// attempting to run cleanup or startup when the operation has not yet
	// reached Doing status raises a conflict
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)
	fdemgr.DeviceInitialized()

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	resealCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		resealCalls++
		c.Check(params.Options.RevokeOldKeys, Equals, false)
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
	err := fdestate.EFISecurebootDBUpdateCleanup(st)
	c.Assert(err, DeepEquals, &snapstate.ChangeConflictError{
		ChangeKind: "fde-efi-secureboot-db-update",
		Message:    "cannot perform Secureboot Key Database update 'cleanup' action when conflicting actions are in progress",
	})

	err = fdestate.EFISecurebootDBManagerStartup(st)
	c.Assert(err, DeepEquals, &snapstate.ChangeConflictError{
		ChangeKind: "fde-efi-secureboot-db-update",
		Message:    "cannot perform Secureboot Key Database 'startup' action when conflicting actions are in progress",
	})
}

func (s *fdeMgrSuite) testEFISecurebootPrepareConflictSnapChangesForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)
	fdemgr.DeviceInitialized()

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

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
	err = fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
	c.Assert(err, DeepEquals, &snapstate.ChangeConflictError{
		ChangeKind: "kernel-snap-remove",
		Snap:       "pc-kernel",
		ChangeID:   "1",
	})
}

func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictSnapChangesPK(c *C) {
	s.testEFISecurebootPrepareConflictSnapChangesForKind(c, fdestate.EFISecurebootPK)
}
func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictSnapChangesKEK(c *C) {
	s.testEFISecurebootPrepareConflictSnapChangesForKind(c, fdestate.EFISecurebootKEK)
}
func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictSnapChangesDB(c *C) {
	s.testEFISecurebootPrepareConflictSnapChangesForKind(c, fdestate.EFISecurebootDB)
}
func (s *fdeMgrSuite) TestEFISecurebootPrepareConflictSnapChangesDBX(c *C) {
	s.testEFISecurebootPrepareConflictSnapChangesForKind(c, fdestate.EFISecurebootDBX)
}

func (s *fdeMgrSuite) testEFISecurebootUpdateAndCleanupRunningActionForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)
	fdemgr.DeviceInitialized()

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, false)
			// normally executed by the backend code
			c.Assert(mgr.Update("run", "default", &backend.SealingParameters{
				BootModes:     []string{"run"},
				Models:        []secboot.ModelForSealing{model},
				TpmPCRProfile: []byte("PCR-profile-dbx-update"),
			}), IsNil)
			return nil
		})()

	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
			resealForBootChainsCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, true)
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

	err := fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 0)
	c.Assert(fdeSt.PendingExternalOperations, HasLen, 1)
	c.Check(fdeSt.KeyslotRoles["run"], DeepEquals, fdestate.KeyslotRoleInfo{
		PrimaryKeyID: 0,
		Parameters: map[string]fdestate.KeyslotRoleParameters{
			"default": {
				Models:         []*fdestate.Model{fdestate.NewModel(model)},
				BootModes:      []string{"run"},
				TPM2PCRProfile: secboot.SerializedPCRProfile([]byte("PCR-profile-dbx-update")),
			},
		},
		TPM2PCRPolicyRevocationCounter: 41,
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
	err = fdestate.EFISecurebootDBUpdateCleanup(st)
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
		TPM2PCRPolicyRevocationCounter: 41,
	})

	c.Check(chg.IsReady(), Equals, true)
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

func (s *fdeMgrSuite) TestEFISecurebootUpdateAndCleanupRunningActionPK(c *C) {
	s.testEFISecurebootUpdateAndCleanupRunningActionForKind(c, fdestate.EFISecurebootPK)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateAndCleanupRunningActionKEK(c *C) {
	s.testEFISecurebootUpdateAndCleanupRunningActionForKind(c, fdestate.EFISecurebootKEK)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateAndCleanupRunningActionDB(c *C) {
	s.testEFISecurebootUpdateAndCleanupRunningActionForKind(c, fdestate.EFISecurebootDB)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateAndCleanupRunningActionDBX(c *C) {
	s.testEFISecurebootUpdateAndCleanupRunningActionForKind(c, fdestate.EFISecurebootDBX)
}

func (s *fdeMgrSuite) testEFISecurebootUpdateAndUnexpectedStartupActionForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)
	fdemgr.DeviceInitialized()

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, false)
			// normally executed by the backend code
			c.Assert(mgr.Update("run", "default", &backend.SealingParameters{
				BootModes:     []string{"run"},
				Models:        []secboot.ModelForSealing{model},
				TpmPCRProfile: []byte("PCR-profile-dbx-update"),
			}), IsNil)
			return nil
		})()

	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
			resealForBootChainsCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, true)
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

	err := fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
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
		TPM2PCRPolicyRevocationCounter: 41,
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

	// first reach a known steady state
	iterateUnlockedStateWaitingFor(st, func() bool {
		return tsks[0].Status() == state.DoneStatus
	})

	// keep kicking the ensure loop, until we reach the desired state
	doneC := make(chan struct{})
	go func() {
		iterateUnlockedStateWaitingFor(st, func() bool {
			status := tsks[0].Status()
			return status == state.UndoingStatus || status == state.UndoneStatus
		})
		close(doneC)
	}()

	// startup aborts the change and reseals with current boot chains, waits for
	// change to be complete
	err = fdestate.EFISecurebootDBManagerStartup(st)
	c.Assert(err, IsNil)

	// wait for helper to complete
	<-doneC

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
		c.Check(fdeStAfter.PendingExternalOperations[0].Status, Equals, fdestate.ErrorStatus)
	} else if l > 1 {
		c.Fatalf("unexpected number of operations in the state: %v", l)
	}
	c.Check(fdeStAfter.KeyslotRoles["run"], DeepEquals, fdestate.KeyslotRoleInfo{
		PrimaryKeyID: 0,
		Parameters: map[string]fdestate.KeyslotRoleParameters{
			"default": {
				Models:         []*fdestate.Model{fdestate.NewModel(model)},
				BootModes:      []string{"run"},
				TPM2PCRProfile: secboot.SerializedPCRProfile([]byte("PCR-profile-boot-chains-startup")),
			},
		},
		TPM2PCRPolicyRevocationCounter: 41,
	})

	// change has an error now
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	updateKindstr := kind.String()
	c.Check(chg.Err(), ErrorMatches,
		"cannot perform the following tasks:\n"+
			fmt.Sprintf(
				"- Reseal after external EFI %s update .'startup' action invoked while an operation is in progress.",
				updateKindstr,
			),
	)
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

func (s *fdeMgrSuite) TestEFISecurebootUpdateAndUnexpectedStartupActionPK(c *C) {
	s.testEFISecurebootUpdateAndUnexpectedStartupActionForKind(c, fdestate.EFISecurebootPK)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateAndUnexpectedStartupActionKEK(c *C) {
	s.testEFISecurebootUpdateAndUnexpectedStartupActionForKind(c, fdestate.EFISecurebootKEK)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateAndUnexpectedStartupActionDB(c *C) {
	s.testEFISecurebootUpdateAndUnexpectedStartupActionForKind(c, fdestate.EFISecurebootDB)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateAndUnexpectedStartupActionDBX(c *C) {
	s.testEFISecurebootUpdateAndUnexpectedStartupActionForKind(c, fdestate.EFISecurebootDBX)
}

func (s *fdeMgrSuite) testEFISecurebootUpdateAbortForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	// simulate a case when prepare is requested, but neither cleanup nor
	// startup is called, the change will wait till it is auto aborted

	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)
	fdemgr.DeviceInitialized()

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	resealForDBUpdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUpdateCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, false)
			return nil
		})()

	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
			resealForBootChainsCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, true)
			return nil
		})()

	// the loop is running now
	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

	err := fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	var fdeSt fdestate.FdeState
	err = st.Get("fde", &fdeSt)
	c.Assert(err, IsNil)

	c.Check(resealForDBUpdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 0)
	c.Assert(fdeSt.PendingExternalOperations, HasLen, 1)
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
	c.Check(chg.IsReady(), Equals, false)
	chg.Abort()
	st.EnsureBefore(0)
	st.Unlock()

	// iterate the state as much as needed to reach the desired state
	iterateUnlockedStateWaitingFor(st, func() bool {
		status := tsks[0].Status()
		return status == state.UndoingStatus || status == state.UndoneStatus
	})

	c.Logf("-- wait ready")
	<-chg.Ready()
	c.Logf("-- ready")

	st.Lock()
	defer st.Unlock()

	// change has been undone
	c.Check(chg.IsReady(), Equals, true)
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
	if l := len(fdeStAfter.PendingExternalOperations); l == 1 {
		c.Check(fdeStAfter.PendingExternalOperations[0].Status, Equals, fdestate.ErrorStatus)
	} else if l != 0 {
		c.Fatalf("unexpected number of pending external operations: %v", l)
	}

	st.Unlock()
	// wait for change to become clean
	iterateUnlockedStateWaitingFor(st, chg.IsClean)
	st.Lock()

	var fdeStAfterCleanup fdestate.FdeState
	err = st.Get("fde", &fdeStAfterCleanup)
	c.Assert(err, IsNil)
	c.Check(fdeStAfterCleanup.PendingExternalOperations, HasLen, 0)
}

func (s *fdeMgrSuite) TestEFISecurebootUpdateAbortPK(c *C) {
	s.testEFISecurebootUpdateAbortForKind(c, fdestate.EFISecurebootPK)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateAbortKEK(c *C) {
	s.testEFISecurebootUpdateAbortForKind(c, fdestate.EFISecurebootKEK)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateAbortDB(c *C) {
	s.testEFISecurebootUpdateAbortForKind(c, fdestate.EFISecurebootDB)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateAbortDBX(c *C) {
	s.testEFISecurebootUpdateAbortForKind(c, fdestate.EFISecurebootDBX)
}

func (s *fdeMgrSuite) testEFISecurebootUpdateResealFailedAbortsForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)
	fdemgr.DeviceInitialized()

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, false)
			return fmt.Errorf("mock error")
		})()
	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
			resealForBootChainsCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, true)
			return nil
		})()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

	err := fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
	c.Assert(err, ErrorMatches, "(?sm).*cannot perform initial reseal of keys for Secureboot Key Database update: mock error.*")

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
	updateKindstr := kind.String()
	c.Check(chg.Err(), ErrorMatches,
		"cannot perform the following tasks:\n"+
			fmt.Sprintf(
				"- Prepare for external EFI %s update "+
					".cannot perform initial reseal of keys for Secureboot Key Database update: mock error.",
				updateKindstr,
			),
	)
}

func (s *fdeMgrSuite) TestEFISecurebootUpdateResealFailedAbortsPK(c *C) {
	s.testEFISecurebootUpdateResealFailedAbortsForKind(c, fdestate.EFISecurebootPK)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateResealFailedAbortsKEK(c *C) {
	s.testEFISecurebootUpdateResealFailedAbortsForKind(c, fdestate.EFISecurebootKEK)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateResealFailedAbortsDB(c *C) {
	s.testEFISecurebootUpdateResealFailedAbortsForKind(c, fdestate.EFISecurebootDB)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateResealFailedAbortsDBX(c *C) {
	s.testEFISecurebootUpdateResealFailedAbortsForKind(c, fdestate.EFISecurebootDBX)
}

func (s *fdeMgrSuite) testEFISecurebootUpdatePostUpdateResealFailedForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	// mock an error in a reseal which happens in the 'do' handler after snapd
	// has been notified of a completed update
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)
	fdemgr.DeviceInitialized()

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, false)
			return nil
		})()
	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
			resealForBootChainsCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, true)
			return fmt.Errorf("mock error")
		})()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

	err := fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 0)

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

	// first reach a known steady state
	iterateUnlockedStateWaitingFor(st, func() bool {
		return tsks[0].Status() == state.DoneStatus
	})

	// keep kicking the ensure loop, until we reach the desired state
	doneC := make(chan struct{})
	go func() {
		iterateUnlockedStateWaitingFor(st, func() bool {
			status := tsks[0].Status()
			// the reseal in update task fails, so we're expecting
			// the prepare task to be undone
			return status == state.UndoneStatus || status == state.UndoingStatus
		})
		close(doneC)
	}()

	// blocks internally waiting for change to complete
	err = fdestate.EFISecurebootDBUpdateCleanup(st)
	c.Assert(err, IsNil)

	// wait for helper to complete
	<-doneC

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
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	updateKindstr := kind.String()
	c.Check(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n"+
		fmt.Sprintf(
			// error logged in the task
			"- Reseal after external EFI %s update .cannot complete post update reseal: mock error.\n"+
				// actual error
				"- Reseal after external EFI %s update .mock error.",
			updateKindstr, updateKindstr,
		),
	)
}

func (s *fdeMgrSuite) TestEFISecurebootUpdatePostUpdateResealFailedPK(c *C) {
	s.testEFISecurebootUpdatePostUpdateResealFailedForKind(c, fdestate.EFISecurebootPK)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdatePostUpdateResealFailedKEK(c *C) {
	s.testEFISecurebootUpdatePostUpdateResealFailedForKind(c, fdestate.EFISecurebootKEK)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdatePostUpdateResealFailedDB(c *C) {
	s.testEFISecurebootUpdatePostUpdateResealFailedForKind(c, fdestate.EFISecurebootDB)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdatePostUpdateResealFailedDBX(c *C) {
	s.testEFISecurebootUpdatePostUpdateResealFailedForKind(c, fdestate.EFISecurebootDBX)
}

func (s *fdeMgrSuite) testEFISecurebootUpdateUndoResealFailsForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	// mock an error in a reseal which happens in the 'undo' path after snapd
	// has been notified of a restart in the external Secureboot Key Database manager process
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)
	fdemgr.DeviceInitialized()

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, false)
			return nil
		})()

	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
			resealForBootChainsCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, true)
			return fmt.Errorf("mock error")
		})()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

	err := fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
	c.Assert(err, IsNil)

	st.Lock()
	defer st.Unlock()

	chgs := st.Changes()
	c.Assert(chgs, HasLen, 1)
	chg := chgs[0]
	c.Check(chg.IsReady(), Equals, false)
	tsks := chg.Tasks()
	c.Assert(tsks, HasLen, 2)
	c.Assert(tsks[0].Kind(), Equals, "efi-secureboot-db-update-prepare")
	c.Assert(tsks[1].Kind(), Equals, "efi-secureboot-db-update")

	c.Check(resealForDBUPdateCalls, Equals, 1)
	c.Check(resealForBootChainsCalls, Equals, 0)

	st.Unlock()
	defer st.Lock()

	// first reach a known steady state
	iterateUnlockedStateWaitingFor(st, func() bool {
		return tsks[0].Status() == state.DoneStatus
	})

	// keep kicking the ensure loop, until we reach the desired state
	doneC := make(chan struct{})
	go func() {
		iterateUnlockedStateWaitingFor(st, func() bool {
			status := tsks[0].Status()
			// the reseal in undo of the prepare task fails, so we're expecting
			// an error status
			return status == state.ErrorStatus
		})
		close(doneC)
	}()

	// 'external' DBX manger restarted, blocks internally waiting for the change to complete
	err = fdestate.EFISecurebootDBManagerStartup(st)
	c.Assert(err, IsNil)

	// wait for helper to complete
	<-doneC

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
	c.Check(chg.IsReady(), Equals, true)
	c.Check(chg.Status(), Equals, state.ErrorStatus)
	updateKindstr := kind.String()
	c.Check(chg.Err(), ErrorMatches, "cannot perform the following tasks:\n"+
		// undo failure
		fmt.Sprintf(
			"- Prepare for external EFI %s update .cannot complete reseal in undo: mock error.\n"+
				"- Reseal after external EFI %s update .'startup' action invoked while an operation is in progress.",
			updateKindstr, updateKindstr,
		),
	)
}

func (s *fdeMgrSuite) TestEFISecurebootUpdateUndoResealFailsPK(c *C) {
	s.testEFISecurebootUpdateUndoResealFailsForKind(c, fdestate.EFISecurebootPK)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateUndoResealFailsKEK(c *C) {
	s.testEFISecurebootUpdateUndoResealFailsForKind(c, fdestate.EFISecurebootKEK)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateUndoResealFailsDB(c *C) {
	s.testEFISecurebootUpdateUndoResealFailsForKind(c, fdestate.EFISecurebootDB)
}
func (s *fdeMgrSuite) TestEFISecurebootUpdateUndoResealFailsDBX(c *C) {
	s.testEFISecurebootUpdateUndoResealFailsForKind(c, fdestate.EFISecurebootDBX)
}

func (s *fdeMgrSuite) TestEFISecurebootCleanupNoChange(c *C) {
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	s.startedManager(c, onClassic)

	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		panic("unexpected call")
	})()

	err := fdestate.EFISecurebootDBUpdateCleanup(st)
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

func (s *fdeMgrSuite) TestEFISecurebootBlockedTasks(c *C) {
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
	st.Unlock()
	iterateUnlockedStateWaitingFor(st, func() bool {
		return tsk.Status() == state.DoStatus
	})
	st.Lock()

	// now unblock it
	op.SetStatus(fdestate.CompletingStatus)
	c.Assert(fdestate.UpdateExternalOperation(st, op), IsNil)

	c.Check(fdestate.IsEFISecurebootDBUpdateBlocked(tsk), Equals, false)

	s.runnerIterationLocked(c)

	// the change is able to complete now
	st.Unlock()
	iterateUnlockedStateWaitingFor(st, chg.IsReady)
	st.Lock()
}

func (s *fdeMgrSuite) testEFISecurebootOperationAddWaitForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	// add 2 changes, and exercise the notification mechanism
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	s.startedManager(c, onClassic)

	st.Lock()
	defer st.Unlock()

	op1, err := fdestate.AddEFISecurebootDBUpdateChange(
		st,
		device.SealingMethodTPM,
		kind,
		[]byte("payload 1"),
	)
	c.Assert(err, IsNil)

	op2, err := fdestate.AddEFISecurebootDBUpdateChange(
		st,
		device.SealingMethodTPM,
		kind,
		[]byte("payload 2"),
	)
	c.Assert(err, IsNil)

	sync1PreparedC := fdestate.SecurebootUpdatePreparedOKChan(st, op1.ChangeID)
	sync2PreparedC := fdestate.SecurebootUpdatePreparedOKChan(st, op2.ChangeID)

	syncC := make(chan struct{})
	defer close(syncC)
	doneC := make(chan struct{})

	go func() {
		<-syncC
		st.Lock()
		fdestate.NotifySecurebootUpdatePrepareDoneOK(st, op1.ChangeID)
		st.Unlock()

		<-syncC
		st.Lock()
		fdestate.NotifySecurebootUpdatePrepareDoneOK(st, op2.ChangeID)
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

func (s *fdeMgrSuite) TestEFISecurebootOperationAddWaitPK(c *C) {
	s.testEFISecurebootOperationAddWaitForKind(c, fdestate.EFISecurebootPK)
}
func (s *fdeMgrSuite) TestEFISecurebootOperationAddWaitKEK(c *C) {
	s.testEFISecurebootOperationAddWaitForKind(c, fdestate.EFISecurebootKEK)
}
func (s *fdeMgrSuite) TestEFISecurebootOperationAddWaitDB(c *C) {
	s.testEFISecurebootOperationAddWaitForKind(c, fdestate.EFISecurebootDB)
}
func (s *fdeMgrSuite) TestEFISecurebootOperationAddWaitDBX(c *C) {
	s.testEFISecurebootOperationAddWaitForKind(c, fdestate.EFISecurebootDBX)
}

func (s *fdeMgrSuite) TestEFISecurebootUpdateAffectedSnaps(c *C) {
	// add 2 changes, and exercise the notification mechanism
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	s.startedManager(c, onClassic)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	st.Lock()
	defer st.Unlock()

	tsk := st.NewTask("foo", "foo task")

	names, err := fdestate.SecurebootUpdateAffectedSnaps(tsk)
	c.Assert(err, IsNil)
	c.Check(names, DeepEquals, []string{
		"pc",        // gadget
		"pc-kernel", // kernel
		"core20",    // base
	})
}

func (s *fdeMgrSuite) testEFISecurebootConflictingSnapsForKind(
	c *C,
	kind fdestate.EFISecurebootKeyDatabase,
) {
	// mock an error in a reseal which happens in the 'undo' path after snapd
	// has been notified of a restart in the external Secureboot Key Database manager process
	c.Assert(device.StampSealedKeys(dirs.GlobalRootDir, device.SealingMethodTPM), IsNil)

	st := s.st
	onClassic := true
	fdemgr := s.startedManager(c, onClassic)
	s.o.AddManager(fdemgr)
	s.o.AddManager(s.o.TaskRunner())
	c.Assert(s.o.StartUp(), IsNil)
	fdemgr.DeviceInitialized()

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	resealForDBUPdateCalls := 0
	resealForBootChainsCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			resealForDBUPdateCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, false)
			return nil
		})()

	defer fdestate.MockBackendResealKeyForBootChains(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
			resealForBootChainsCalls++
			c.Check(params.Options.RevokeOldKeys, Equals, true)
			return fmt.Errorf("mock error")
		})()

	st.Lock()
	st.Set("seeded", true)
	st.Unlock()

	c.Logf("overlord loop start")
	s.o.Loop()
	defer s.o.Stop()

	err := fdestate.EFISecurebootDBUpdatePrepare(st, kind, []byte("payload"))
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

func (s *fdeMgrSuite) TestEFISecurebootConflictingSnapsPK(c *C) {
	s.testEFISecurebootConflictingSnapsForKind(c, fdestate.EFISecurebootPK)
}
func (s *fdeMgrSuite) TestEFISecurebootConflictingSnapsKEK(c *C) {
	s.testEFISecurebootConflictingSnapsForKind(c, fdestate.EFISecurebootKEK)
}
func (s *fdeMgrSuite) TestEFISecurebootConflictingSnapsDB(c *C) {
	s.testEFISecurebootConflictingSnapsForKind(c, fdestate.EFISecurebootDB)
}
func (s *fdeMgrSuite) TestEFISecurebootConflictingSnapsDBX(c *C) {
	s.testEFISecurebootConflictingSnapsForKind(c, fdestate.EFISecurebootDBX)
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
