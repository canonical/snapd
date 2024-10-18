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
	"crypto"
	"fmt"
	"os"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/testutil"
)

func TestFDE(t *testing.T) { TestingT(t) }

type fdeMgrSuite struct {
	testutil.BaseTest

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

	m := boot.Modeenv{
		Mode: boot.ModeRun,
	}
	err := m.WriteTo(dirs.GlobalRootDir)
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

	manager, err := fdestate.Manager(s.st, s.runner)
	c.Assert(err, IsNil)
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

type mountResolveTestCase struct {
	dataResolveErr error
	saveResolveErr error
	expectedError  string
}

func (s *fdeMgrSuite) testMountResolveError(c *C, tc mountResolveTestCase) {
	defer fdestate.MockDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case dirs.SnapdStateDir(dirs.GlobalRootDir):
			// ubuntu-data
			if tc.dataResolveErr != nil {
				return "", tc.dataResolveErr
			}
			return "aaa", nil
		case dirs.SnapSaveDir:
			if tc.saveResolveErr != nil {
				return "", tc.saveResolveErr
			}
			return "bbb", nil
		}
		panic(fmt.Sprintf("missing mocked mount point %q", mountpoint))
	})()

	defer fdestate.MockGetPrimaryKeyHMAC(func(devicePath string, alg crypto.Hash) ([]byte, []byte, error) {
		if tc.expectedError == "" {
			return nil, nil, fmt.Errorf("unexpected call to get primary key")
		}
		return []byte{1, 2, 3, 4}, []byte{5, 6, 7, 8}, nil
	})()

	defer fdestate.MockVerifyPrimaryKeyHMAC(func(devicePath string, alg crypto.Hash, salt, digest []byte) (bool, error) {
		if tc.expectedError == "" {
			return false, fmt.Errorf("unexpected call to get primary key")
		}
		return true, nil
	})()

	manager, err := fdestate.Manager(s.st, s.runner)
	c.Assert(err, IsNil)
	err = manager.StartUp()
	if tc.expectedError != "" {
		c.Check(err, ErrorMatches, tc.expectedError)
	} else {
		c.Check(err, IsNil)
	}
}

func (s *fdeMgrSuite) TestStateInitMountResolveError_StatePresentNoError(c *C) {
	s.st.Lock()
	s.st.Set("fde", fdestate.FdeState{})
	s.st.Unlock()

	// state initialization happens (and may fail) only when "fde" isn't yet set
	// in the state

	s.testMountResolveError(c, mountResolveTestCase{
		dataResolveErr: fmt.Errorf("mock degraded mode"),
	})
}

func (s *fdeMgrSuite) TestStateInitMountResolveError_NoDataNoSaveNoError(c *C) {
	s.testMountResolveError(c, mountResolveTestCase{
		dataResolveErr: disks.ErrNoDmUUID,
		saveResolveErr: disks.ErrNoDmUUID,
	})
}

func (s *fdeMgrSuite) TestStateInitMountResolveError_NoDataFails(c *C) {
	s.testMountResolveError(c, mountResolveTestCase{
		dataResolveErr: fmt.Errorf("mock error data"),
		expectedError:  "cannot initialize FDE state: cannot resolve data partition mount: mock error data",
	})
}

func (s *fdeMgrSuite) TestStatetInitMountResolveError_NoSaveFails(c *C) {
	s.testMountResolveError(c, mountResolveTestCase{
		saveResolveErr: fmt.Errorf("mock error save"),
		expectedError:  "cannot initialize FDE state: cannot resolve save partition mount: mock error save",
	})
}

func (s *fdeMgrSuite) TestStateInitMountResolveError_Recover(c *C) {
	m := boot.Modeenv{
		Mode:           boot.ModeRecover,
		RecoverySystem: "1234",
	}
	err := m.WriteTo(dirs.GlobalRootDir)
	c.Assert(err, IsNil)

	// neither partition could be mounted
	s.testMountResolveError(c, mountResolveTestCase{
		dataResolveErr: disks.ErrNoDmUUID,
		saveResolveErr: disks.ErrNoDmUUID,
	})
}

func (s *fdeMgrSuite) TestMountResolveError_FactoryReset(c *C) {
	m := boot.Modeenv{
		Mode:           boot.ModeFactoryReset,
		RecoverySystem: "1234",
	}
	err := m.WriteTo(dirs.GlobalRootDir)
	c.Assert(err, IsNil)

	// neither partition could be mounted
	s.testMountResolveError(c, mountResolveTestCase{
		dataResolveErr: disks.ErrNoDmUUID,
		saveResolveErr: disks.ErrNoDmUUID,
	})
}

func (s *fdeMgrSuite) TestManagerUC_16_18(c *C) {
	// no modeenv
	err := os.Remove(dirs.SnapModeenvFileUnder(s.rootdir))
	c.Assert(err, IsNil)

	manager, err := fdestate.Manager(s.st, s.runner)
	c.Assert(err, IsNil)

	// neither startup nor ensure fails
	c.Assert(manager.StartUp(), IsNil)
	c.Assert(manager.Ensure(), IsNil)
}

func (s *fdeMgrSuite) TestManagerPreseeding(c *C) {
	defer snapdenv.MockPreseeding(true)()

	manager, err := fdestate.Manager(s.st, s.runner)
	c.Assert(err, IsNil)

	// neither startup nor ensure fails
	c.Assert(manager.StartUp(), IsNil)
	c.Assert(manager.Ensure(), IsNil)
}
