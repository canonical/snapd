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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	. "gopkg.in/check.v1"
	"gopkg.in/tomb.v2"

	"github.com/snapcore/snapd/arch/archtest"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/interfaces"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/ifacestate/ifacerepo"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
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

	s.AddCleanup(archtest.MockArchitecture("amd64"))

	s.o = overlord.Mock()

	s.st = s.o.State()
	s.runner = s.o.TaskRunner()
	s.o.AddManager(s.runner)

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
		func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
			panic("BackendResealKeyForBootChains not mocked")
		}))
	s.AddCleanup(fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
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
	s.AddCleanup(fdestate.MockSecbootGetPCRHandle(func(devicePath, keySlot, keyFile string, hintExpectFDEHook bool) (uint32, error) {
		panic("secbootGetPCRHandle is not mocked")
	}))

	mountinfo := `26 27 8:3 / %s/var/lib/snapd/save rw,relatime shared:7 - ext4 /dev/fakedevice0p1 rw,data=ordered`
	s.AddCleanup(osutil.MockMountInfo(fmt.Sprintf(mountinfo, dirs.GlobalRootDir)))

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

func (s *fdeMgrSuite) mockDeviceInState(model *asserts.Model, sysMode string) {
	s.st.Lock()
	defer s.st.Unlock()

	s.AddCleanup(snapstatetest.MockDeviceContext(&snapstatetest.TrivialDeviceContext{
		DeviceModel: model,
		SysMode:     sysMode,
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

func (s *fdeMgrSuite) startedManagerNoEncryptedDisks(c *C, onClassic bool) *fdestate.FDEManager {
	s.mockDeviceInState(&asserts.Model{}, "run")

	defer fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		// disks.ErrNoDmUUID results in getEncryptedContainers
		// not returning any encrypted containers
		return "", disks.ErrNoDmUUID
	})()

	manager, err := fdestate.Manager(s.st, s.runner)
	c.Assert(err, IsNil)
	s.o.AddManager(manager)
	c.Assert(manager.StartUp(), IsNil)
	c.Assert(s.logbuf.String(), testutil.Contains, "WARNING: no primary key was found")
	return manager
}

func (s *fdeMgrSuite) startedManager(c *C, onClassic bool) *fdestate.FDEManager {
	s.mockDeviceInState(&asserts.Model{}, "run")

	defer fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case dirs.GlobalRootDir:
			return "aaa", nil
		case filepath.Join(dirs.GlobalRootDir, "writable"):
			return "aaa", nil
		case filepath.Join(dirs.GlobalRootDir, "run/mnt/data"):
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

	err := os.MkdirAll(filepath.Dir(device.DataSealedKeyUnder(boot.InitramfsBootEncryptionKeyDir)), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(device.DataSealedKeyUnder(boot.InitramfsBootEncryptionKeyDir), []byte{}, 0644)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Dir(device.FallbackDataSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(device.FallbackDataSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir), []byte{}, 0644)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Dir(device.FallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir)), 0755)
	c.Assert(err, IsNil)
	err = os.WriteFile(device.FallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir), []byte{}, 0644)
	c.Assert(err, IsNil)

	defer fdestate.MockSecbootGetPCRHandle(func(devicePath, keySlot, keyFile string, hintExpectFDEHook bool) (uint32, error) {
		c.Check(hintExpectFDEHook, Equals, false)
		switch devicePath {
		case "/dev/disk/by-uuid/aaa":
			switch keySlot {
			case "default":
				c.Check(keyFile, Equals, device.DataSealedKeyUnder(boot.InitramfsBootEncryptionKeyDir))
				return 41, nil
			case "default-fallback":
				c.Check(keyFile, Equals, device.FallbackDataSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir))
				return 42, nil
			default:
				c.Errorf("unexpected keyslot %s", keySlot)
			}
		case "/dev/disk/by-uuid/bbb":
			c.Check(keySlot, Equals, "default-fallback")
			c.Check(keyFile, Equals, device.FallbackSaveSealedKeyUnder(boot.InitramfsSeedEncryptionKeyDir))
			return 42, nil
		default:
			c.Errorf("unexpected device path %s", devicePath)
		}
		return 0, fmt.Errorf("unexpected")
	})()

	manager, err := fdestate.Manager(s.st, s.runner)
	c.Assert(err, IsNil)
	s.o.AddManager(manager)
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

	defer fdestate.MockBackendResealKeyForBootChains(func(manager backend.FDEStateManager, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams) error {
		c.Check(unlocker.unlocked, Equals, 0)
		c.Check(unlocker.relocked, Equals, 0)
		// Simulate the unlocking to calculate the profile
		relock := manager.Unlock()
		relock()
		c.Check(method, Equals, device.SealingMethodFDESetupHook)
		c.Check(rootdir, Equals, dirs.GlobalRootDir)
		c.Check(params, Equals, params)
		c.Check(params.Options.ExpectReseal, Equals, false)
		manager.Update("run+recover", "container-role", &backend.SealingParameters{
			BootModes:     []string{"run"},
			Models:        []secboot.ModelForSealing{&mockModel{}},
			TpmPCRProfile: []byte(`"serialized-profile"`),
		})
		resealed += 1
		return nil
	})()

	err := boot.ResealKeyForBootChains(unlocker.Unlock, device.SealingMethodFDESetupHook, dirs.GlobalRootDir, params)
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
	s.mockDeviceInState(&asserts.Model{}, "run")

	defer fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case filepath.Join(dirs.GlobalRootDir, "run/mnt/data"):
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

	defer fdestate.MockSecbootGetPCRHandle(func(devicePath, keySlot, keyFile string, hintExpectFDEHook bool) (uint32, error) {
		return 41, nil
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
		expectedError:  "cannot initialize FDE state: .*: mock error data",
	})
}

func (s *fdeMgrSuite) TestStatetInitMountResolveError_NoSaveFails(c *C) {
	s.testMountResolveError(c, mountResolveTestCase{
		saveResolveErr: fmt.Errorf("mock error save"),
		expectedError:  "cannot initialize FDE state: .*: mock error save",
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

func (s *fdeMgrSuite) TestGetEncryptedContainers(c *C) {
	dataPath := filepath.Join(dirs.GlobalRootDir, "path/to/data")

	err := os.MkdirAll(filepath.Dir(dataPath), 0755)
	c.Assert(err, IsNil)

	onClassic := false
	mgr := s.startedManager(c, onClassic)

	model := &asserts.Model{}
	s.mockDeviceInState(model, "run")

	defer fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case dataPath:
			return "aaa", nil
		case dirs.SnapSaveDir:
			return "bbb", nil
		}
		panic(fmt.Sprintf("missing mocked mount point %q", mountpoint))
	})()

	defer fdestate.MockBootHostUbuntuDataForMode(func(mode string, mod gadget.Model) ([]string, error) {
		c.Check(mode, Equals, "run")
		c.Check(mod, Equals, model)
		return []string{dataPath}, nil
	})()

	disks, err := mgr.GetEncryptedContainers()
	c.Assert(err, IsNil)
	c.Check(disks, DeepEquals, []backend.EncryptedContainer{
		fdestate.EncryptedContainer(
			"aaa",
			"system-data",
			map[string]string{
				"default":          filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-boot/device/fde/ubuntu-data.sealed-key"),
				"default-fallback": filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-data.recovery.sealed-key"),
			},
		),
		fdestate.EncryptedContainer(
			"bbb",
			"system-save",
			map[string]string{
				"default-fallback": filepath.Join(dirs.GlobalRootDir, "run/mnt/ubuntu-seed/device/fde/ubuntu-save.recovery.sealed-key"),
			},
		),
	})
}

type mockRecoveryKeyCache struct {
	addRecoveryKey    func(keyID string, rkeyInfo backend.CachedRecoverKey) (err error)
	getRecoveryKey    func(keyID string) (rkeyInfo backend.CachedRecoverKey, err error)
	deleteRecoveryKey func(keyID string) error
}

func (s *mockRecoveryKeyCache) AddKey(keyID string, rkeyInfo backend.CachedRecoverKey) (err error) {
	if s.addRecoveryKey == nil {
		panic("AddKey is not implemented")
	}
	return s.addRecoveryKey(keyID, rkeyInfo)
}

func (s *mockRecoveryKeyCache) Key(keyID string) (rkeyInfo backend.CachedRecoverKey, err error) {
	if s.getRecoveryKey == nil {
		panic("Key is not implemented")
	}
	return s.getRecoveryKey(keyID)
}

func (s *mockRecoveryKeyCache) RemoveKey(keyID string) error {
	if s.deleteRecoveryKey == nil {
		panic("RemoveKey is not implemented")
	}
	return s.deleteRecoveryKey(keyID)
}

func (s *fdeMgrSuite) TestGenerateRecoveryKey(c *C) {
	now := time.Now()
	defer fdestate.MockTimeNow(func() time.Time {
		return now
	})()

	expectedKeys := []struct {
		id         string
		key        keys.RecoveryKey
		expiration time.Time
	}{
		{
			id:         "F1DBNCCKlM",
			key:        keys.RecoveryKey{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y', '-', '1'},
			expiration: now.Add(5 * time.Minute),
		},
		{
			id:         "2JId82xFLN",
			key:        keys.RecoveryKey{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y', '-', '2'},
			expiration: now.Add(5 * time.Minute),
		},
		{
			id:         "Jk1rFMJeuo",
			key:        keys.RecoveryKey{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y', '-', '3'},
			expiration: now.Add(5 * time.Minute),
		},
	}

	getCalled, addCalled := 0, 0
	mockStore := &mockRecoveryKeyCache{
		getRecoveryKey: func(keyID string) (rkeyInfo backend.CachedRecoverKey, err error) {
			defer func() { getCalled++ }()
			switch getCalled {
			case 0:
				c.Check(keyID, Equals, expectedKeys[0].id)
				// simulate collision, key exists
				return backend.CachedRecoverKey{
					Key:        expectedKeys[0].key,
					Expiration: expectedKeys[0].expiration,
				}, nil
			case 1:
				c.Check(keyID, Equals, expectedKeys[1].id)
				return backend.CachedRecoverKey{}, backend.ErrNoRecoveryKey
			case 2:
				c.Check(keyID, Equals, expectedKeys[2].id)
				return backend.CachedRecoverKey{}, backend.ErrNoRecoveryKey
			default:
				c.Error("unexpected call")
			}
			return backend.CachedRecoverKey{}, backend.ErrNoRecoveryKey
		},
		addRecoveryKey: func(keyID string, rkeyInfo backend.CachedRecoverKey) (err error) {
			defer func() { addCalled++ }()
			switch addCalled {
			case 0:
				c.Check(keyID, Equals, expectedKeys[1].id)
				c.Check(rkeyInfo.Key, DeepEquals, expectedKeys[1].key)
				c.Check(rkeyInfo.Expiration, DeepEquals, expectedKeys[1].expiration)
				return nil
			case 1:
				c.Check(keyID, Equals, expectedKeys[2].id)
				c.Check(rkeyInfo.Key, DeepEquals, expectedKeys[2].key)
				c.Check(rkeyInfo.Expiration, DeepEquals, expectedKeys[2].expiration)
				return nil
			default:
				c.Error("unexpected call")
			}
			return nil
		},
	}
	defer fdestate.MockBackendNewInMemoryRecoveryKeyCache(func() backend.RecoveryKeyCache {
		return mockStore
	})()

	nextKeyIdx := 0
	defer fdestate.MockKeysNewRecoveryKey(func() (keys.RecoveryKey, error) {
		expected := expectedKeys[nextKeyIdx]
		nextKeyIdx++
		return expected.key, nil
	})()

	// initialize fde manager
	_, err := fdestate.Manager(s.st, s.runner)
	c.Assert(err, IsNil)

	s.st.Lock()
	defer s.st.Unlock()

	rkey, keyID, err := fdestate.GenerateRecoveryKey(s.st)
	c.Assert(err, IsNil)
	c.Check(addCalled, Equals, 1)
	c.Check(getCalled, Equals, 2)              // twice due to collision
	c.Check(keyID, Equals, expectedKeys[1].id) // first key collided with exisiting key
	c.Check(rkey, DeepEquals, expectedKeys[1].key)

	rkey, keyID, err = fdestate.GenerateRecoveryKey(s.st)
	c.Assert(err, IsNil)
	c.Check(addCalled, Equals, 2)
	c.Check(getCalled, Equals, 3)
	c.Check(keyID, Equals, expectedKeys[2].id)
	c.Check(rkey, DeepEquals, expectedKeys[2].key)
}

func (s *fdeMgrSuite) TestGenerateRecoveryKeyMaxRetriesError(c *C) {
	called := 0
	mockStore := &mockRecoveryKeyCache{
		getRecoveryKey: func(keyID string) (rkeyInfo backend.CachedRecoverKey, err error) {
			called++
			return backend.CachedRecoverKey{}, nil
		},
	}
	defer fdestate.MockBackendNewInMemoryRecoveryKeyCache(func() backend.RecoveryKeyCache {
		return mockStore
	})()

	defer fdestate.MockKeysNewRecoveryKey(func() (keys.RecoveryKey, error) {
		return keys.RecoveryKey{'1', '2'}, nil
	})()

	// initialize fde manager
	_, err := fdestate.Manager(s.st, s.runner)
	c.Assert(err, IsNil)

	s.st.Lock()
	defer s.st.Unlock()
	_, _, err = fdestate.GenerateRecoveryKey(s.st)
	c.Assert(err, ErrorMatches, "internal error: cannot generate recovery key: max retries reached")
	c.Check(called, Equals, 10)
}

func (s *fdeMgrSuite) TestGetRecoveryKey(c *C) {
	mockRecoveryKeyInfo := backend.CachedRecoverKey{
		Key: [16]byte{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y', '-', '1'},
		// not expired
		Expiration: time.Now().Add(time.Minute),
	}
	mockRecoveryKeyInfoExpired := backend.CachedRecoverKey{
		Key: [16]byte{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y', '-', '2'},
		// not expired
		Expiration: time.Now().Add(-time.Minute),
	}

	getCalled, deleteCalled := 0, 0
	mockStore := &mockRecoveryKeyCache{
		getRecoveryKey: func(keyID string) (rkeyInfo backend.CachedRecoverKey, err error) {
			getCalled++
			switch keyID {
			case "1":
				return mockRecoveryKeyInfo, nil
			case "2":
				return mockRecoveryKeyInfoExpired, nil
			default:
				panic("unexpected key-id")
			}
		},
		deleteRecoveryKey: func(keyID string) error {
			deleteCalled++
			switch keyID {
			case "1", "2":
				return nil
			default:
				panic("unexpected key-id")
			}
		},
	}
	defer fdestate.MockBackendNewInMemoryRecoveryKeyCache(func() backend.RecoveryKeyCache {
		return mockStore
	})()

	// initialize fde manager
	_, err := fdestate.Manager(s.st, s.runner)
	c.Assert(err, IsNil)

	s.st.Lock()
	defer s.st.Unlock()

	rkey, err := fdestate.GetRecoveryKey(s.st, "1")
	c.Assert(err, IsNil)
	c.Check(rkey, DeepEquals, keys.RecoveryKey{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y', '-', '1'})
	c.Check(getCalled, Equals, 1)
	c.Check(deleteCalled, Equals, 1)

	rkey, err = fdestate.GetRecoveryKey(s.st, "2")
	c.Assert(err, ErrorMatches, "recovery key has expired")
	c.Check(rkey, DeepEquals, keys.RecoveryKey{})
	c.Check(getCalled, Equals, 2)
	c.Check(deleteCalled, Equals, 2)
}

func (s *fdeMgrSuite) testCheckRecoveryKey(c *C, defaultContainerRoles bool) {
	dataPath := filepath.Join(dirs.GlobalRootDir, "path/to/data")

	err := os.MkdirAll(filepath.Dir(dataPath), 0755)
	c.Assert(err, IsNil)

	onClassic := false
	mgr := s.startedManager(c, onClassic)

	model := &asserts.Model{}
	s.mockDeviceInState(model, "run")

	defer fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case dataPath:
			return "aaa", nil
		case dirs.SnapSaveDir:
			return "bbb", nil
		}
		panic(fmt.Sprintf("missing mocked mount point %q", mountpoint))
	})()

	defer fdestate.MockBootHostUbuntuDataForMode(func(mode string, mod gadget.Model) ([]string, error) {
		c.Check(mode, Equals, "run")
		c.Check(mod, Equals, model)
		return []string{dataPath}, nil
	})()

	var foundDevPaths []string
	defer fdestate.MockSecbootCheckRecoveryKey(func(devicePath string, rkey keys.RecoveryKey) error {
		c.Check(rkey, DeepEquals, keys.RecoveryKey{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y'})
		foundDevPaths = append(foundDevPaths, devicePath)
		return nil
	})()

	var containerRoles []string
	if !defaultContainerRoles {
		containerRoles = []string{"system-data"}
	}
	err = mgr.CheckRecoveryKey(keys.RecoveryKey{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y'}, containerRoles)
	c.Assert(err, IsNil)

	if defaultContainerRoles {
		c.Check(foundDevPaths, DeepEquals, []string{"/dev/disk/by-uuid/aaa", "/dev/disk/by-uuid/bbb"})
	} else {
		// system-data only
		c.Check(foundDevPaths, DeepEquals, []string{"/dev/disk/by-uuid/aaa"})
	}
}

func (s *fdeMgrSuite) TestCheckRecoveryKey(c *C) {
	const defaultContainerRoles = false
	s.testCheckRecoveryKey(c, defaultContainerRoles)
}

func (s *fdeMgrSuite) TestCheckRecoveryKeyDefaultContainerRole(c *C) {
	const defaultContainerRoles = true
	s.testCheckRecoveryKey(c, defaultContainerRoles)
}

func (s *fdeMgrSuite) TestCheckRecoveryKeyMissingContainerRole(c *C) {
	dataPath := filepath.Join(dirs.GlobalRootDir, "path/to/data")

	err := os.MkdirAll(filepath.Dir(dataPath), 0755)
	c.Assert(err, IsNil)

	onClassic := false
	mgr := s.startedManager(c, onClassic)

	model := &asserts.Model{}
	s.mockDeviceInState(model, "run")

	defer fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case dataPath:
			return "aaa", nil
		case dirs.SnapSaveDir:
			return "bbb", nil
		}
		panic(fmt.Sprintf("missing mocked mount point %q", mountpoint))
	})()

	defer fdestate.MockBootHostUbuntuDataForMode(func(mode string, mod gadget.Model) ([]string, error) {
		c.Check(mode, Equals, "run")
		c.Check(mod, Equals, model)
		return []string{dataPath}, nil
	})()

	err = mgr.CheckRecoveryKey(keys.RecoveryKey{}, []string{"missing-container-role"})
	c.Assert(err, ErrorMatches, `encrypted container role "missing-container-role" does not exist`)
}

func (s *fdeMgrSuite) TestCheckRecoveryKeyError(c *C) {
	dataPath := filepath.Join(dirs.GlobalRootDir, "path/to/data")

	err := os.MkdirAll(filepath.Dir(dataPath), 0755)
	c.Assert(err, IsNil)

	onClassic := false
	mgr := s.startedManager(c, onClassic)

	model := &asserts.Model{}
	s.mockDeviceInState(model, "run")

	defer fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case dataPath:
			return "aaa", nil
		case dirs.SnapSaveDir:
			return "bbb", nil
		}
		panic(fmt.Sprintf("missing mocked mount point %q", mountpoint))
	})()

	defer fdestate.MockBootHostUbuntuDataForMode(func(mode string, mod gadget.Model) ([]string, error) {
		c.Check(mode, Equals, "run")
		c.Check(mod, Equals, model)
		return []string{dataPath}, nil
	})()

	defer fdestate.MockSecbootCheckRecoveryKey(func(devicePath string, rkey keys.RecoveryKey) error {
		c.Check(devicePath, Equals, "/dev/disk/by-uuid/aaa")
		return errors.New("boom!")
	})()

	err = mgr.CheckRecoveryKey(keys.RecoveryKey{}, []string{"system-data"})
	c.Assert(err, ErrorMatches, `recovery key failed for "system-data": boom!`)
}

type mockKeyData struct {
	authMode     device.AuthMode
	platformName string
	roles        []string

	changePassphrase func(oldPassphrase, newPassphrase string) error
	writeTokenAtomic func(devicePath, slotName string) error
}

func (k *mockKeyData) AuthMode() device.AuthMode {
	return k.authMode
}

func (k *mockKeyData) PlatformName() string {
	return k.platformName
}

func (k *mockKeyData) Roles() []string {
	return k.roles
}

func (k *mockKeyData) ChangePassphrase(oldPassphrase, newPassphrase string) error {
	if k.changePassphrase != nil {
		return k.changePassphrase(oldPassphrase, newPassphrase)
	}
	return nil
}

func (k *mockKeyData) WriteTokenAtomic(devicePath, slotName string) error {
	if k.writeTokenAtomic != nil {
		return k.writeTokenAtomic(devicePath, slotName)
	}
	return nil
}

func (s *fdeMgrSuite) TestKeyslotKeyDataLazyLoad(c *C) {
	called := 0
	defer fdestate.MockSecbootReadContainerKeyData(func(devicePath, slotName string) (secboot.KeyData, error) {
		called++
		c.Check(devicePath, Equals, "/dev/some-device")
		c.Check(slotName, Equals, "some-slot")
		return &mockKeyData{
			authMode:     device.AuthModePassphrase,
			platformName: "tpm2",
			roles:        []string{"run+recover"},
		}, nil
	})()

	keyslot := fdestate.Keyslot{
		Name: "some-slot",
		Type: fdestate.KeyslotTypePlatform,
	}
	keyslot.SetDevPath("/dev/some-device")

	for i := 0; i < 10; i++ {
		_, err := keyslot.KeyData()
		c.Assert(err, IsNil)
	}
	kd, err := keyslot.KeyData()
	c.Assert(err, IsNil)
	c.Check(kd.AuthMode(), Equals, device.AuthModePassphrase)
	c.Check(kd.PlatformName(), Equals, "tpm2")
	c.Check(kd.Roles(), DeepEquals, []string{"run+recover"})
	// lazy loaded once, then reused
	c.Check(called, Equals, 1)
}

func (s *fdeMgrSuite) TestKeyslotKeyDataErrors(c *C) {
	keyslot := fdestate.Keyslot{
		Name: "some-slot",
		Type: fdestate.KeyslotTypeRecovery,
	}
	keyslot.SetDevPath("/dev/some-device")

	_, err := keyslot.KeyData()
	c.Assert(err, ErrorMatches, `internal error: Keyslot.KeyData\(\) is only available for KeyslotTypePlatform, found "recovery"`)

	keyslot.Type = fdestate.KeyslotTypePlatform
	defer fdestate.MockSecbootReadContainerKeyData(func(devicePath, slotName string) (secboot.KeyData, error) {
		return nil, errors.New("boom!")
	})()
	_, err = keyslot.KeyData()
	c.Assert(err, ErrorMatches, `cannot read key data for "some-slot" from "/dev/some-device": boom!`)
}

func (s *fdeMgrSuite) testGetKeyslots(c *C, allKeyslots bool) {
	s.mockDeviceInState(&asserts.Model{}, "run")

	defer fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case filepath.Join(dirs.GlobalRootDir, "run/mnt/data"):
			return "aaa", nil
		case dirs.SnapSaveDir:
			return "bbb", nil
		}
		panic(fmt.Sprintf("missing mocked mount point %q", mountpoint))
	})()

	defer fdestate.MockSecbootListContainerRecoveryKeyNames(func(devicePath string) ([]string, error) {
		switch devicePath {
		case "/dev/disk/by-uuid/aaa":
			return []string{"recovery-aaa"}, nil
		case "/dev/disk/by-uuid/bbb":
			return []string{}, nil
		default:
			return nil, fmt.Errorf("unexpected devicePath %q", devicePath)
		}
	})()

	defer fdestate.MockSecbootListContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		switch devicePath {
		case "/dev/disk/by-uuid/aaa":
			return []string{"unlock-aaa"}, nil
		case "/dev/disk/by-uuid/bbb":
			return []string{"unlock-bbb"}, nil
		default:
			return nil, fmt.Errorf("unexpected devicePath %q", devicePath)
		}
	})()

	const onClassic = true
	s.startedManager(c, onClassic)

	var keyslotRefs []fdestate.KeyslotRef
	if !allKeyslots {
		keyslotRefs = []fdestate.KeyslotRef{
			{ContainerRole: "system-data", Name: "unlock-aaa"},
			{ContainerRole: "system-data", Name: "recovery-aaa"},
			// should be marked as missing
			{ContainerRole: "system-data", Name: "recovery-ccc"},
		}
	}

	s.st.Lock()
	defer s.st.Unlock()

	keyslots, missing, err := fdestate.GetKeyslots(s.st, keyslotRefs)
	c.Assert(err, IsNil)

	// sort for test consistency
	sort.Slice(keyslots, func(i, j int) bool {
		if keyslots[i].ContainerRole == keyslots[j].ContainerRole {
			return keyslots[i].Name < keyslots[j].Name
		}
		return keyslots[i].ContainerRole < keyslots[j].ContainerRole
	})

	if allKeyslots {
		c.Check(keyslots, HasLen, 3)

		c.Check(keyslots[0].ContainerRole, Equals, "system-data")
		c.Check(keyslots[0].Name, Equals, "recovery-aaa")
		c.Check(keyslots[0].Type, Equals, fdestate.KeyslotTypeRecovery)
		c.Check(keyslots[1].ContainerRole, Equals, "system-data")
		c.Check(keyslots[1].Name, Equals, "unlock-aaa")
		c.Check(keyslots[1].Type, Equals, fdestate.KeyslotTypePlatform)
		c.Check(keyslots[2].ContainerRole, Equals, "system-save")
		c.Check(keyslots[2].Name, Equals, "unlock-bbb")
		c.Check(keyslots[2].Type, Equals, fdestate.KeyslotTypePlatform)

		c.Check(missing, HasLen, 0)
	} else {
		c.Check(keyslots, HasLen, 2)

		c.Check(keyslots[0].ContainerRole, Equals, "system-data")
		c.Check(keyslots[0].Name, Equals, "recovery-aaa")
		c.Check(keyslots[0].Type, Equals, fdestate.KeyslotTypeRecovery)
		c.Check(keyslots[1].ContainerRole, Equals, "system-data")
		c.Check(keyslots[1].Name, Equals, "unlock-aaa")
		c.Check(keyslots[1].Type, Equals, fdestate.KeyslotTypePlatform)

		c.Check(missing, DeepEquals, []fdestate.KeyslotRef{{ContainerRole: "system-data", Name: "recovery-ccc"}})
	}
}

func (s *fdeMgrSuite) TestGetKeyslots(c *C) {
	const allKeyslots = false
	s.testGetKeyslots(c, allKeyslots)
}

func (s *fdeMgrSuite) TestGetKeyslotsAll(c *C) {
	const allKeyslots = true
	s.testGetKeyslots(c, allKeyslots)
}

func (s *fdeMgrSuite) TestGetKeyslotsErrors(c *C) {
	s.mockDeviceInState(&asserts.Model{}, "run")

	defer fdestate.MockDisksDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case filepath.Join(dirs.GlobalRootDir, "run/mnt/data"):
			return "aaa", nil
		case dirs.SnapSaveDir:
			return "bbb", nil
		}
		panic(fmt.Sprintf("missing mocked mount point %q", mountpoint))
	})()

	const onClassic = true
	manager := s.startedManager(c, onClassic)

	defer fdestate.MockSecbootListContainerRecoveryKeyNames(func(devicePath string) ([]string, error) {
		return nil, errors.New("boom!")
	})()
	defer fdestate.MockSecbootListContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		return nil, nil
	})()

	_, _, err := manager.GetKeyslots(nil)
	c.Assert(err, ErrorMatches, `cannot obtain recovery keys for "/dev/disk/by-uuid/aaa": boom!`)

	defer fdestate.MockSecbootListContainerRecoveryKeyNames(func(devicePath string) ([]string, error) {
		return nil, nil
	})()
	defer fdestate.MockSecbootListContainerUnlockKeyNames(func(devicePath string) ([]string, error) {
		return nil, errors.New("boom!")
	})()

	_, _, err = manager.GetKeyslots(nil)
	c.Assert(err, ErrorMatches, `cannot obtain platform keys for "/dev/disk/by-uuid/aaa": boom!`)
}

func (s *fdeMgrSuite) TestFDEBlockedTasks(c *C) {
	st := s.st
	onClassic := true
	s.startedManager(c, onClassic)

	ready := make(chan struct{})
	s.runner.AddHandler("fde-op-1", func(t *state.Task, _ *tomb.Tomb) error {
		<-ready
		return nil
	}, nil)

	s.runner.AddHandler("fde-op-2", func(t *state.Task, _ *tomb.Tomb) error { return nil }, nil)

	st.Lock()
	defer st.Unlock()

	chg1 := st.NewChange("some-change-1", "")
	tsk1 := st.NewTask("fde-op-1", "") // fde- task prefix
	chg1.AddTask(tsk1)

	// wait for keyslot-op to start
	for i := 0; i < 10; i++ {
		s.runnerIterationLocked(c)
		if tsk1.Status() == state.DoingStatus {
			break
		}
	}
	c.Assert(tsk1.Status(), Equals, state.DoingStatus)

	chg2 := st.NewChange("some-change-2", "")
	tsk2 := st.NewTask("fde-op-2", "") // fde- task prefix
	chg2.AddTask(tsk2)

	// try to force "fde-op-2" to run
	for i := 0; i < 10; i++ {
		s.runnerIterationLocked(c)
	}
	// check it hasn't started yet
	c.Check(chg2.Status(), Equals, state.DoStatus)
	c.Check(tsk2.Status(), Equals, state.DoStatus)

	// now unblock it
	close(ready)
	st.Unlock()
	iterateUnlockedStateWaitingFor(st, chg1.IsReady)
	st.Lock()

	s.runnerIterationLocked(c)

	// // the change is able to complete now
	st.Unlock()
	iterateUnlockedStateWaitingFor(st, chg2.IsReady)
	st.Lock()
}

func (s *fdeMgrSuite) TestEFIDBXUpdateTaskAffectedSnaps(c *C) {
	onClassic := true
	s.startedManager(c, onClassic)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	s.st.Lock()
	defer s.st.Unlock()

	tsk := s.st.NewTask("efi-secureboot-db-update", "")
	snaps, err := snapstate.SnapsAffectedByTask(tsk)
	c.Assert(err, IsNil)
	c.Assert(snaps, DeepEquals, []string{"pc", "pc-kernel", "core20"})
}

func (s *fdeMgrSuite) TestAddProtectedKeysTaskAffectedSnaps(c *C) {
	onClassic := true
	s.startedManager(c, onClassic)

	model := s.mockBootAssetsStateForModeenv(c)
	s.mockDeviceInState(model, "run")

	s.st.Lock()
	defer s.st.Unlock()

	tsk := s.st.NewTask("fde-add-protected-keys", "")
	snaps, err := snapstate.SnapsAffectedByTask(tsk)
	c.Assert(err, IsNil)
	c.Assert(snaps, DeepEquals, []string{"pc", "pc-kernel", "core20"})
}
