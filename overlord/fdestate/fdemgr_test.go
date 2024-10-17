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
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/boot/boottest"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
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

	s.AddCleanup(fdestate.MockBackendResealKeysForSignaturesDBUpdate(
		func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
			panic("BackendResealKeysForSignaturesDBUpdate not mocked")
		}))
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

	s.mockBootAssetsStateForModeenv(c)

	resealCalls := 0
	defer fdestate.MockBackendResealKeysForSignaturesDBUpdate(func(updateState backend.StateUpdater, method device.SealingMethod, rootdir string, params *boot.ResealKeyForBootChainsParams, update []byte) error {
		resealCalls++
		c.Check(update, DeepEquals, []byte("payload"))
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

func (s *fdeMgrSuite) mockBootAssetsStateForModeenv(c *C) {
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
}
