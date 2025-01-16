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
	"bytes"
	"crypto"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snapdenv"
	"github.com/snapcore/snapd/testutil"
)

func TestFDE(t *testing.T) { TestingT(t) }

type fdeMgrSuite struct {
	testutil.BaseTest

	logbuf  *bytes.Buffer
	rootdir string
	st      *state.State
	runner  *state.TaskRunner
	o       *overlord.Overlord
}

var _ = Suite(&fdeMgrSuite{})

func (s *fdeMgrSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.AddCleanup(release.MockOnClassic(true))

	s.rootdir = c.MkDir()
	dirs.SetRootDir(s.rootdir)
	s.AddCleanup(func() { dirs.SetRootDir("") })

	s.o = overlord.Mock()

	s.st = s.o.State()
	s.runner = s.o.TaskRunner()

	s.st.Lock()
	repo := interfaces.NewRepository()
	ifacerepo.Replace(s.st, repo)
	s.st.Unlock()

	buf, restore := logger.MockLogger()
	s.AddCleanup(restore)
	s.logbuf = buf

	c.Assert(os.Setenv("SNAPD_DEBUG", "1"), IsNil)
	s.AddCleanup(func() {
		os.Unsetenv("SNAPD_DEBUG")
	})

	s.AddCleanup(fdestate.MockBackendResealKeyForBootChains(
		func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
			panic("BackendResealKeyForBootChains not mocked")
		}))
	s.AddCleanup(fdestate.MockDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		panic("MockDMCryptUUIDFromMountPoint is not mocked")
	}))
	s.AddCleanup(fdestate.MockGetPrimaryKeyDigest(func(devicePath string, alg crypto.Hash) ([]byte, []byte, error) {
		panic("GetPrimaryKeyDigest is not mocked")
	}))
	s.AddCleanup(fdestate.MockVerifyPrimaryKeyDigest(func(devicePath string, alg crypto.Hash, salt, digest []byte) (bool, error) {
		panic("VerifyPrimaryKeyDigest is not mocked")
	}))
	s.AddCleanup(fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(mgr backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			panic("BackendResealKeysForSignaturesDBUpdate not mocked")
		}))
	s.AddCleanup(fdestate.MockSecbootGetPCRHandle(func(devicePath, keySlot, keyFile string) (uint32, error) {
		panic("secbootGetPCRHandle is not mocked")
	}))

	m := boot.Modeenv{
		Mode: boot.ModeRun,
	}
	err := m.WriteTo(dirs.GlobalRootDir)
	c.Assert(err, IsNil)

}

func (s *fdeMgrSuite) TearDownTest(c *C) {
	c.Assert(s.logbuf, NotNil)
	c.Logf("logs:\n%s\n", s.logbuf.String())
	s.BaseTest.TearDownTest(c)
}

func (s *fdeMgrSuite) mockDeviceInState(model *asserts.Model) {
	s.st.Lock()
	defer s.st.Unlock()

	s.AddCleanup(snapstatetest.MockDeviceContext(&snapstatetest.TrivialDeviceContext{
		DeviceModel: model,
	}))
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

func (s *fdeMgrSuite) startedManager(c *C, onClassic bool) *fdestate.FDEManager {
	defer fdestate.MockDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case dirs.GlobalRootDir:
			c.Check(onClassic, Equals, true)
			return "aaa", nil
		case filepath.Join(dirs.GlobalRootDir, "writable"):
			c.Check(onClassic, Equals, false)
			return "aaa", nil
		case dirs.SnapSaveDir:
			return "bbb", nil
		}
		panic(fmt.Sprintf("missing mocked mount point %q", mountpoint))
	})()

	defer fdestate.MockGetPrimaryKeyDigest(func(devicePath string, alg crypto.Hash) ([]byte, []byte, error) {
		c.Assert(devicePath, Equals, "/dev/disk/by-uuid/aaa")
		c.Check(alg, Equals, crypto.Hash(crypto.SHA256))
		return []byte{1, 2, 3, 4}, []byte{5, 6, 7, 8}, nil
	})()

	defer fdestate.MockVerifyPrimaryKeyDigest(func(devicePath string, alg crypto.Hash, salt, digest []byte) (bool, error) {
		c.Assert(devicePath, Equals, "/dev/disk/by-uuid/bbb")
		c.Check(alg, Equals, crypto.Hash(crypto.SHA256))
		c.Check(salt, DeepEquals, []byte{1, 2, 3, 4})
		c.Check(digest, DeepEquals, []byte{5, 6, 7, 8})
		return true, nil
	})()

	defer fdestate.MockSecbootGetPCRHandle(func(devicePath, keySlot, keyFile string) (uint32, error) {
		switch devicePath {
		case "/dev/disk/by-uuid/aaa":
			switch keySlot {
			case "default":
				c.Check(keyFile, Equals, device.DataSealedKeyUnder(dirs.SnapSaveDir))
				return 41, nil
			case "default-fallback":
				c.Check(keyFile, Equals, device.FallbackDataSealedKeyUnder(dirs.SnapSaveDir))
				return 42, nil
			default:
				c.Errorf("unexpected keyslot %s", keySlot)
			}
		case "/dev/disk/by-uuid/bbb":
			c.Check(keySlot, Equals, "default-fallback")
			c.Check(keyFile, Equals, device.FallbackDataSealedKeyUnder(dirs.SnapSaveDir))
			return 42, nil
		default:
			c.Errorf("unexpected device path %s", devicePath)
		}
		return 0, fmt.Errorf("unexpected")
	})()

	manager, err := fdestate.Manager(s.st, s.runner)
	c.Assert(err, IsNil)
	c.Assert(manager.StartUp(), IsNil)
	return manager
}

func (s *fdeMgrSuite) testGetManagerFromState(c *C, onClassic bool) {
	st := s.st
	s.AddCleanup(release.MockOnClassic(onClassic))
	dirs.SetRootDir(s.rootdir)

	manager := s.startedManager(c, onClassic)

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

	runRole, hasRunRole := fdeSt.KeyslotRoles["run"]
	c.Assert(hasRunRole, Equals, true)
	c.Check(runRole.PrimaryKeyID, Equals, 0)
	c.Check(runRole.TPM2PCRPolicyRevocationCounter, Equals, uint32(41))

	runRecoverRole, hasRunRecoverRole := fdeSt.KeyslotRoles["run+recover"]
	c.Assert(hasRunRecoverRole, Equals, true)
	c.Check(runRecoverRole.PrimaryKeyID, Equals, 0)
	c.Check(runRecoverRole.TPM2PCRPolicyRevocationCounter, Equals, uint32(41))

	recoverRole, hasRecoverRole := fdeSt.KeyslotRoles["recover"]
	c.Assert(hasRecoverRole, Equals, true)
	c.Check(recoverRole.PrimaryKeyID, Equals, 0)
	c.Check(recoverRole.TPM2PCRPolicyRevocationCounter, Equals, uint32(42))
}

func (s *fdeMgrSuite) TestGetManagerFromStateClassic(c *C) {
	const onClassic = true
	s.testGetManagerFromState(c, onClassic)
}

func (s *fdeMgrSuite) TestGetManagerFromStateCore(c *C) {
	const onClassic = false
	s.testGetManagerFromState(c, onClassic)
}

type mockModel struct {
	otherName string
}

func (m *mockModel) Series() string {
	return "mock-series"
}

func (m *mockModel) BrandID() string {
	return "mock-brand"
}

func (m *mockModel) Model() string {
	if m.otherName != "" {
		return m.otherName
	} else {
		return "mock-model"
	}
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

func (s *fdeMgrSuite) TestUpdate(c *C) {
	st := s.st
	const onClassic = true
	s.AddCleanup(release.MockOnClassic(onClassic))
	dirs.SetRootDir(s.rootdir)

	manager := s.startedManager(c, onClassic)

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
	const onClassic = true
	s.AddCleanup(release.MockOnClassic(onClassic))
	dirs.SetRootDir(s.rootdir)

	manager := s.startedManager(c, onClassic)

	st.Lock()
	defer st.Unlock()
	foundManager := fdestate.FdeMgr(st)
	c.Check(foundManager, Equals, manager)

	unlocker := &instrumentedUnlocker{state: st}
	params := &boot.ResealKeyForBootChainsParams{}
	resealed := 0

	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, expectReseal bool) error {
		c.Check(unlocker.unlocked, Equals, 0)
		c.Check(unlocker.relocked, Equals, 0)
		// Simulate the unlocking to calculate the profile
		relock := manager.Unlock()
		relock()
		c.Check(method, Equals, device.SealingMethodFDESetupHook)
		c.Check(rootdir, Equals, dirs.GlobalRootDir)
		c.Check(params, Equals, params)
		c.Check(expectReseal, Equals, expectReseal)
		manager.Update("run+recover", "container-role", &backend.SealingParameters{
			BootModes:     []string{"run"},
			Models:        []secboot.ModelForSealing{&mockModel{}},
			TpmPCRProfile: []byte(`"serialized-profile"`),
		})
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
		case dirs.GlobalRootDir:
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

	defer fdestate.MockGetPrimaryKeyDigest(func(devicePath string, alg crypto.Hash) ([]byte, []byte, error) {
		if tc.expectedError == "" {
			return nil, nil, fmt.Errorf("unexpected call to get primary key")
		}
		return []byte{1, 2, 3, 4}, []byte{5, 6, 7, 8}, nil
	})()

	defer fdestate.MockVerifyPrimaryKeyDigest(func(devicePath string, alg crypto.Hash, salt, digest []byte) (bool, error) {
		if tc.expectedError == "" {
			return false, fmt.Errorf("unexpected call to get primary key")
		}
		return true, nil
	})()

	manager, err := fdestate.Manager(s.st, s.runner)
	c.Assert(err, IsNil)
	err = manager.StartUp()
	c.Check(err, IsNil)

	functionalErr := manager.IsFunctional()
	if tc.expectedError != "" {
		c.Check(functionalErr, ErrorMatches, tc.expectedError)
	} else {
		c.Check(functionalErr, IsNil)
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
	// but the manager is deemed non functional, so API calls will fail
	c.Assert(manager.IsFunctional(), ErrorMatches, "internal error: FDE manager cannot be used in preseeding mode")
}

func (s *fdeMgrSuite) TestGetParameters(c *C) {
	st := s.st
	const onClassic = true
	s.AddCleanup(release.MockOnClassic(onClassic))
	dirs.SetRootDir(s.rootdir)

	manager := s.startedManager(c, onClassic)

	st.Lock()
	defer st.Unlock()

	c.Assert(manager.IsFunctional(), IsNil)

	models := []secboot.ModelForSealing{
		&mockModel{},
		&mockModel{"other"},
	}

	err := manager.UpdateParameters("recover", "something", []string{"recover"}, models, secboot.SerializedPCRProfile(`serialized-profile-recover`))
	c.Assert(err, IsNil)
	err = manager.UpdateParameters("recover", "all", []string{"recover-all"}, models, secboot.SerializedPCRProfile(`serialized-profile-recover-all`))
	c.Assert(err, IsNil)
	err = manager.UpdateParameters("run", "something", []string{"run"}, models, secboot.SerializedPCRProfile(`serialized-profile-run`))
	c.Assert(err, IsNil)

	hasParameters, foundRunModes, foundModels, foundPCRProfile, err := manager.GetParameters("recover", "something")
	c.Assert(err, IsNil)
	c.Check(hasParameters, Equals, true)
	c.Check(foundRunModes, DeepEquals, []string{"recover"})
	c.Assert(foundModels, HasLen, 2)
	c.Check(foundModels[0].Model(), Equals, "mock-model")
	c.Check(foundModels[1].Model(), Equals, "other")
	c.Check(foundPCRProfile, DeepEquals, []byte(`serialized-profile-recover`))

	hasParameters, foundRunModes, foundModels, foundPCRProfile, err = manager.GetParameters("recover", "something-that-is-not-specific")
	c.Assert(err, IsNil)
	c.Check(hasParameters, Equals, true)
	c.Check(foundRunModes, DeepEquals, []string{"recover-all"})
	c.Assert(foundModels, HasLen, 2)
	c.Check(foundModels[0].Model(), Equals, "mock-model")
	c.Check(foundModels[1].Model(), Equals, "other")
	c.Check(foundPCRProfile, DeepEquals, []byte(`serialized-profile-recover-all`))

	hasParameters, foundRunModes, foundModels, foundPCRProfile, err = manager.GetParameters("run", "something")
	c.Assert(err, IsNil)
	c.Check(hasParameters, Equals, true)
	c.Check(foundRunModes, DeepEquals, []string{"run"})
	c.Assert(foundModels, HasLen, 2)
	c.Check(foundModels[0].Model(), Equals, "mock-model")
	c.Check(foundModels[1].Model(), Equals, "other")
	c.Check(foundPCRProfile, DeepEquals, []byte(`serialized-profile-run`))

	hasParameters, _, _, _, err = manager.GetParameters("run", "something-that-is-not-specific")
	c.Assert(err, IsNil)
	c.Check(hasParameters, Equals, false)
}
