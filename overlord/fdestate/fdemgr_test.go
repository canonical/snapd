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
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/fdestate"
	"github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
)

func TestFDE(t *testing.T) { TestingT(t) }

type fdeMgrSuite struct{}

var _ = Suite(&fdeMgrSuite{})

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

func (s *fdeMgrSuite) TestGetManagerFromState(c *C) {
	st := state.New(nil)

	defer fdestate.MockDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case dirs.GlobalRootDir:
			return "aaa", nil
		case dirs.SnapSaveDir:
			return "bbb", nil
		}
		return "", fmt.Errorf("unknown mount point")
	})()

	defer fdestate.MockGetPrimaryKeyHash(func(devicePath string, alg crypto.Hash) ([]byte, []byte, error) {
		c.Check(devicePath, Equals, "/dev/disk/by-uuid/aaa")
		c.Check(alg, Equals, crypto.Hash(crypto.SHA256))
		return []byte{1, 2, 3, 4}, []byte{5, 6, 7, 8}, nil
	})()

	defer fdestate.MockVerifyPrimaryKeyHash(func(devicePath string, alg crypto.Hash, salt, digest []byte) (bool, error) {
		c.Check(devicePath, Equals, "/dev/disk/by-uuid/bbb")
		c.Check(alg, Equals, crypto.Hash(crypto.SHA256))
		c.Check(salt, DeepEquals, []byte{1, 2, 3, 4})
		c.Check(digest, DeepEquals, []byte{5, 6, 7, 8})
		return true, nil
	})()

	runner := state.NewTaskRunner(st)
	manager := fdestate.Manager(st, runner)
	c.Assert(manager.StartUp(), IsNil)
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
	st := state.New(nil)

	defer fdestate.MockDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case dirs.GlobalRootDir:
			return "aaa", nil
		case dirs.SnapSaveDir:
			return "bbb", nil
		}
		return "", fmt.Errorf("unknown mount point")
	})()

	defer fdestate.MockGetPrimaryKeyHash(func(devicePath string, alg crypto.Hash) ([]byte, []byte, error) {
		c.Check(devicePath, Equals, "/dev/disk/by-uuid/aaa")
		c.Check(alg, Equals, crypto.Hash(crypto.SHA256))
		return []byte{1, 2, 3, 4}, []byte{5, 6, 7, 8}, nil
	})()

	defer fdestate.MockVerifyPrimaryKeyHash(func(devicePath string, alg crypto.Hash, salt, digest []byte) (bool, error) {
		c.Check(devicePath, Equals, "/dev/disk/by-uuid/bbb")
		c.Check(alg, Equals, crypto.Hash(crypto.SHA256))
		c.Check(salt, DeepEquals, []byte{1, 2, 3, 4})
		c.Check(digest, DeepEquals, []byte{5, 6, 7, 8})
		return true, nil
	})()

	runner := state.NewTaskRunner(st)
	manager := fdestate.Manager(st, runner)
	c.Assert(manager.StartUp(), IsNil)
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
	st := state.New(nil)

	defer fdestate.MockDMCryptUUIDFromMountPoint(func(mountpoint string) (string, error) {
		switch mountpoint {
		case dirs.GlobalRootDir:
			return "aaa", nil
		case dirs.SnapSaveDir:
			return "bbb", nil
		}
		return "", fmt.Errorf("unknown mount point")
	})()

	defer fdestate.MockGetPrimaryKeyHash(func(devicePath string, alg crypto.Hash) ([]byte, []byte, error) {
		c.Check(devicePath, Equals, "/dev/disk/by-uuid/aaa")
		c.Check(alg, Equals, crypto.Hash(crypto.SHA256))
		return []byte{1, 2, 3, 4}, []byte{5, 6, 7, 8}, nil
	})()

	defer fdestate.MockVerifyPrimaryKeyHash(func(devicePath string, alg crypto.Hash, salt, digest []byte) (bool, error) {
		c.Check(devicePath, Equals, "/dev/disk/by-uuid/bbb")
		c.Check(alg, Equals, crypto.Hash(crypto.SHA256))
		c.Check(salt, DeepEquals, []byte{1, 2, 3, 4})
		c.Check(digest, DeepEquals, []byte{5, 6, 7, 8})
		return true, nil
	})()

	runner := state.NewTaskRunner(st)
	manager := fdestate.Manager(st, runner)
	c.Assert(manager.StartUp(), IsNil)
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
