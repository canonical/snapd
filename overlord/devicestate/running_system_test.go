// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2026 Canonical Ltd
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

package devicestate_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	fdeBackend "github.com/snapcore/snapd/overlord/fdestate/backend"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/snapstate/snapstatetest"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type runningSystemInfoSuite struct {
	modelAndGadgetInfoSuite
}

var _ = Suite(&runningSystemInfoSuite{})

func (s *runningSystemInfoSuite) SetUpTest(c *C) {
	s.modelAndGadgetInfoSuite.SetUpTest(c)
}

func (s *runningSystemInfoSuite) TestRunningSystemAndGadgetAndEncryptionInfoHappyPath(c *C) {
	fakeModel := s.makeMockUC20SeedWithGadgetYaml(c, "some-label", mockGadgetUCYaml, false, nil)
	func() {
		s.state.Lock()
		defer s.state.Unlock()

		devicestatetest.SetDevice(s.state, &auth.DeviceState{
			Brand:  fakeModel.BrandID(),
			Model:  fakeModel.Model(),
			Serial: "didididi",
		})

		assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
		assertstatetest.AddMany(s.state, fakeModel)
	}()

	si := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	gadgetSnapInfo := snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, si, [][]string{
		{"meta/gadget.yaml", mockGadgetUCYaml},
	})

	func() {
		s.state.Lock()
		defer s.state.Unlock()

		s.state.Set("seeded-systems", []devicestate.SeededSystem{
			{
				System:  "some-label",
				Model:   fakeModel.Model(),
				BrandID: fakeModel.BrandID(),
			},
		})
	}()

	devicestate.SetSystemMode(s.mgr, "run")

	modeenv := boot.Modeenv{
		Mode:    "run",
		Model:   fakeModel.Model(),
		BrandID: fakeModel.BrandID(),
	}
	err := modeenv.WriteTo("")
	c.Assert(err, IsNil)

	defer devicestate.MockSnapstateGadgetInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		return gadgetSnapInfo, nil
	})()

	expectedCheckContext := &secboot.PreinstallCheckContext{}
	defer devicestate.MockSecbootPostinstallCheck(func(ctx context.Context, bootChain []bootloader.BootFile) (*secboot.PreinstallCheckContext, []secboot.PreinstallErrorDetails, error) {
		c.Assert(bootChain, HasLen, 2)
		c.Check(bootChain[0].Path, Equals, "/some/path/first.efi")
		c.Check(bootChain[1].Path, Equals, "/some/path/second.efi")
		return expectedCheckContext, nil, nil
	})()

	defer devicestate.MockFdestateGetRunBootChain(func() ([]bootloader.BootFile, error) {
		return []bootloader.BootFile{
			{Snap: "", Path: "/some/path/first.efi", Role: bootloader.RoleRecovery},
			{Snap: "", Path: "/some/path/second.efi", Role: bootloader.RoleRunMode},
		}, nil
	})()

	system, gotGadgetInfo, encInfo, err := s.mgr.RunningSystemAndGadgetAndEncryptionInfo()
	c.Assert(err, IsNil)
	c.Assert(system, NotNil)
	c.Assert(gotGadgetInfo, NotNil)
	c.Assert(encInfo, NotNil)

	c.Check(system.Label, Equals, "some-label")

	c.Check(encInfo.Available, Equals, true)
	c.Check(encInfo.Type, Equals, device.EncryptionTypeLUKS)
	c.Check(encInfo.Disabled, Equals, false)
}

func (s *runningSystemInfoSuite) TestRunningSystemAndGadgetAndEncryptionInfoNoRunningSystem(c *C) {
	fakeModel := s.makeMockUC20SeedWithGadgetYaml(c, "some-label", mockGadgetUCYaml, false, nil)
	func() {
		s.state.Lock()
		defer s.state.Unlock()

		devicestatetest.SetDevice(s.state, &auth.DeviceState{
			Brand:  fakeModel.BrandID(),
			Model:  fakeModel.Model(),
			Serial: "didididi",
		})

		assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
		assertstatetest.AddMany(s.state, fakeModel)
	}()

	_, err := gadget.InfoFromGadgetYaml([]byte(mockGadgetUCYaml), fakeModel)
	c.Assert(err, IsNil)
	si := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	gadgetSnapInfo := snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, si, [][]string{
		{"meta/gadget.yaml", mockGadgetUCYaml},
	})

	devicestate.SetSystemMode(s.mgr, "run")

	defer devicestate.MockSnapstateGadgetInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		return gadgetSnapInfo, nil
	})()

	system, gadgetInfo, encInfo, err := s.mgr.RunningSystemAndGadgetAndEncryptionInfo()
	c.Assert(err, ErrorMatches, `no current system for mode run`)
	c.Assert(system, IsNil)
	c.Assert(gadgetInfo, IsNil)
	c.Assert(encInfo, IsNil)
}

func (s *runningSystemInfoSuite) TestRunningSystemAndGadgetAndEncryptionInfoFdestateGetRunBootChainError(c *C) {
	fakeModel := s.makeMockUC20SeedWithGadgetYaml(c, "some-label", mockGadgetUCYaml, false, nil)
	func() {
		s.state.Lock()
		defer s.state.Unlock()

		devicestatetest.SetDevice(s.state, &auth.DeviceState{
			Brand:  fakeModel.BrandID(),
			Model:  fakeModel.Model(),
			Serial: "didididi",
		})

		assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
		assertstatetest.AddMany(s.state, fakeModel)
	}()

	_, err := gadget.InfoFromGadgetYaml([]byte(mockGadgetUCYaml), fakeModel)
	c.Assert(err, IsNil)
	si := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	gadgetSnapInfo := snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, si, [][]string{
		{"meta/gadget.yaml", mockGadgetUCYaml},
	})

	devicestate.SetSystemMode(s.mgr, "run")

	func() {
		s.state.Lock()
		defer s.state.Unlock()

		s.state.Set("seeded-systems", []devicestate.SeededSystem{
			{
				System:  "some-label",
				Model:   fakeModel.Model(),
				BrandID: fakeModel.BrandID(),
			},
		})
	}()

	modeenv := boot.Modeenv{
		Mode:    "run",
		Model:   fakeModel.Model(),
		BrandID: fakeModel.BrandID(),
	}
	err = modeenv.WriteTo("")
	c.Assert(err, IsNil)

	defer devicestate.MockSnapstateGadgetInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		return gadgetSnapInfo, nil
	})()

	defer devicestate.MockFdestateGetRunBootChain(func() ([]bootloader.BootFile, error) {
		return nil, fmt.Errorf("fdestate get boot chain failed")
	})()

	system, gadgetInfo, encInfo, err := s.mgr.RunningSystemAndGadgetAndEncryptionInfo()
	c.Assert(err, ErrorMatches, `fdestate get boot chain failed`)
	c.Assert(system, IsNil)
	c.Assert(gadgetInfo, IsNil)
	c.Assert(encInfo, IsNil)
}

func (s *runningSystemInfoSuite) TestRunningSystemAndGadgetAndEncryptionInfoEncCheckFails(c *C) {
	fakeModel := s.makeMockUC20SeedWithGadgetYaml(c, "some-label", mockGadgetUCYaml, false, nil)
	func() {
		s.state.Lock()
		defer s.state.Unlock()

		devicestatetest.SetDevice(s.state, &auth.DeviceState{
			Brand:  fakeModel.BrandID(),
			Model:  fakeModel.Model(),
			Serial: "didididi",
		})

		assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
		assertstatetest.AddMany(s.state, fakeModel)
	}()

	_, err := gadget.InfoFromGadgetYaml([]byte(mockGadgetUCYaml), fakeModel)
	c.Assert(err, IsNil)
	si := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	gadgetSnapInfo := snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, si, [][]string{
		{"meta/gadget.yaml", mockGadgetUCYaml},
	})

	devicestate.SetSystemMode(s.mgr, "run")

	func() {
		s.state.Lock()
		defer s.state.Unlock()

		s.state.Set("seeded-systems", []devicestate.SeededSystem{
			{
				System:  "some-label",
				Model:   fakeModel.Model(),
				BrandID: fakeModel.BrandID(),
			},
		})
	}()

	modeenv := boot.Modeenv{
		Mode:    "run",
		Model:   fakeModel.Model(),
		BrandID: fakeModel.BrandID(),
	}
	err = modeenv.WriteTo("")
	c.Assert(err, IsNil)

	defer devicestate.MockSnapstateGadgetInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		return gadgetSnapInfo, nil
	})()

	expectedCheckContext := &secboot.PreinstallCheckContext{}
	defer devicestate.MockSecbootPostinstallCheck(func(ctx context.Context, bootChain []bootloader.BootFile) (*secboot.PreinstallCheckContext, []secboot.PreinstallErrorDetails, error) {
		return expectedCheckContext, nil, fmt.Errorf("tpm not available")
	})()

	defer devicestate.MockFdestateGetRunBootChain(func() ([]bootloader.BootFile, error) {
		return []bootloader.BootFile{
			{Snap: "", Path: "/some/path/first.efi", Role: bootloader.RoleRecovery},
			{Snap: "", Path: "/some/path/second.efi", Role: bootloader.RoleRunMode},
		}, nil
	})()

	system, gadgetInfo, encInfo, err := s.mgr.RunningSystemAndGadgetAndEncryptionInfo()
	c.Assert(err, IsNil)
	c.Assert(system, NotNil)
	c.Assert(gadgetInfo, NotNil)
	c.Assert(encInfo, NotNil)

	c.Check(encInfo.Available, Equals, false)
	c.Check(encInfo.UnavailableErr, ErrorMatches, `tpm not available`)
}

func (s *runningSystemInfoSuite) TestApplyActionOnRunningSystemAndGadgetAndEncryptionInfoHappyPath(c *C) {
	fakeModel := s.makeMockUC20SeedWithGadgetYaml(c, "some-label", mockGadgetUCYaml, false, nil)
	func() {
		s.state.Lock()
		defer s.state.Unlock()

		devicestatetest.SetDevice(s.state, &auth.DeviceState{
			Brand:  fakeModel.BrandID(),
			Model:  fakeModel.Model(),
			Serial: "didididi",
		})
	}()

	si := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	gadgetSnapInfo := snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, si, [][]string{
		{"meta/gadget.yaml", mockGadgetUCYaml},
	})

	devicestate.SetSystemMode(s.mgr, "run")

	func() {
		s.state.Lock()
		defer s.state.Unlock()

		s.state.Set("seeded-systems", []devicestate.SeededSystem{
			{
				System:  "some-label",
				Model:   fakeModel.Model(),
				BrandID: fakeModel.BrandID(),
			},
		})

		assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
		assertstatetest.AddMany(s.state, fakeModel)
	}()

	modeenv := boot.Modeenv{
		Mode:    "run",
		Model:   fakeModel.Model(),
		BrandID: fakeModel.BrandID(),
	}
	err := modeenv.WriteTo("")
	c.Assert(err, IsNil)

	defer devicestate.MockSnapstateGadgetInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		return gadgetSnapInfo, nil
	})()

	mockActionCheckContext := &secboot.PreinstallCheckContext{}

	defer devicestate.MockSecbootPostinstallCheck(func(ctx context.Context, bootChain []bootloader.BootFile) (*secboot.PreinstallCheckContext, []secboot.PreinstallErrorDetails, error) {
		return mockActionCheckContext, nil, nil
	})()

	defer devicestate.MockFdestateGetRunBootChain(func() ([]bootloader.BootFile, error) {
		return []bootloader.BootFile{
			{Snap: "", Path: "/some/path/first.efi", Role: bootloader.RoleRecovery},
			{Snap: "", Path: "/some/path/second.efi", Role: bootloader.RoleRunMode},
		}, nil
	})()

	_, _, _, err = s.mgr.RunningSystemAndGadgetAndEncryptionInfo()
	c.Assert(err, IsNil)

	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		c.Assert(pcc, Equals, mockActionCheckContext)
		c.Assert(action.Action, Equals, "test-action")
		return nil, nil
	})()

	preinstallAction := &secboot.PreinstallAction{
		Action: "test-action",
	}
	system, gadgetInfo, encInfo, err := s.mgr.ApplyActionOnRunningSystemAndGadgetAndEncryptionInfo(preinstallAction)
	c.Assert(err, IsNil)
	c.Assert(system, NotNil)
	c.Assert(gadgetInfo, NotNil)
	c.Assert(encInfo, NotNil)

	c.Check(encInfo.Available, Equals, true)
}

func (s *runningSystemInfoSuite) TestApplyActionOnRunningSystemAndGadgetAndEncryptionInfoNilAction(c *C) {
	_, _, _, err := s.mgr.ApplyActionOnRunningSystemAndGadgetAndEncryptionInfo(nil)
	c.Assert(err, ErrorMatches, `cannot apply empty action`)
}

func (s *runningSystemInfoSuite) TestApplyActionOnRunningSystemAndGadgetAndEncryptionInfoNoPriorCheck(c *C) {
	fakeModel := s.makeMockUC20SeedWithGadgetYaml(c, "some-label", mockGadgetUCYaml, false, nil)
	func() {
		s.state.Lock()
		defer s.state.Unlock()

		devicestatetest.SetDevice(s.state, &auth.DeviceState{
			Brand:  fakeModel.BrandID(),
			Model:  fakeModel.Model(),
			Serial: "didididi",
		})

		assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
		assertstatetest.AddMany(s.state, fakeModel)
	}()

	si := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	gadgetSnapInfo := snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, si, [][]string{
		{"meta/gadget.yaml", mockGadgetUCYaml},
	})

	devicestate.SetSystemMode(s.mgr, "run")

	func() {
		s.state.Lock()
		defer s.state.Unlock()

		s.state.Set("seeded-systems", []devicestate.SeededSystem{
			{
				System:  "some-label",
				Model:   fakeModel.Model(),
				BrandID: fakeModel.BrandID(),
			},
		})
	}()

	modeenv := boot.Modeenv{
		Mode:    "run",
		Model:   fakeModel.Model(),
		BrandID: fakeModel.BrandID(),
	}
	err := modeenv.WriteTo("")
	c.Assert(err, IsNil)

	defer devicestate.MockSnapstateGadgetInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		return gadgetSnapInfo, nil
	})()

	preinstallAction := &secboot.PreinstallAction{
		Action: "test-action",
	}

	system, gotGadgetInfo, encInfo, err := s.mgr.ApplyActionOnRunningSystemAndGadgetAndEncryptionInfo(preinstallAction)
	c.Assert(err, ErrorMatches, `cannot run check action without prior check`)
	c.Assert(system, IsNil)
	c.Assert(gotGadgetInfo, IsNil)
	c.Assert(encInfo, IsNil)
}

func (s *runningSystemInfoSuite) TestApplyActionOnRunningSystemAndGadgetAndEncryptionInfoCheckActionFails(c *C) {
	fakeModel := s.makeMockUC20SeedWithGadgetYaml(c, "some-label", mockGadgetUCYaml, false, nil)
	func() {
		s.state.Lock()
		defer s.state.Unlock()

		devicestatetest.SetDevice(s.state, &auth.DeviceState{
			Brand:  fakeModel.BrandID(),
			Model:  fakeModel.Model(),
			Serial: "didididi",
		})
	}()

	_, err := gadget.InfoFromGadgetYaml([]byte(mockGadgetUCYaml), fakeModel)
	c.Assert(err, IsNil)
	si := &snap.SideInfo{
		RealName: "pc",
		Revision: snap.R(33),
		SnapID:   "foo-id",
	}
	gadgetSnapInfo := snaptest.MockSnapWithFiles(c, pcGadgetSnapYaml, si, [][]string{
		{"meta/gadget.yaml", mockGadgetUCYaml},
	})

	devicestate.SetSystemMode(s.mgr, "run")

	func() {
		s.state.Lock()
		defer s.state.Unlock()

		s.state.Set("seeded-systems", []devicestate.SeededSystem{
			{
				System:  "some-label",
				Model:   fakeModel.Model(),
				BrandID: fakeModel.BrandID(),
			},
		})

		assertstatetest.AddMany(s.state, s.brands.AccountsAndKeys("my-brand")...)
		assertstatetest.AddMany(s.state, fakeModel)
	}()

	modeenv := boot.Modeenv{
		Mode:    "run",
		Model:   fakeModel.Model(),
		BrandID: fakeModel.BrandID(),
	}
	err = modeenv.WriteTo("")
	c.Assert(err, IsNil)

	defer devicestate.MockSnapstateGadgetInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		return gadgetSnapInfo, nil
	})()

	mockActionCheckContext := &secboot.PreinstallCheckContext{}

	defer devicestate.MockSecbootPostinstallCheck(func(ctx context.Context, bootChain []bootloader.BootFile) (*secboot.PreinstallCheckContext, []secboot.PreinstallErrorDetails, error) {
		return mockActionCheckContext, nil, nil
	})()

	defer devicestate.MockFdestateGetRunBootChain(func() ([]bootloader.BootFile, error) {
		return []bootloader.BootFile{
			{Snap: "", Path: "/some/path/first.efi", Role: bootloader.RoleRecovery},
			{Snap: "", Path: "/some/path/second.efi", Role: bootloader.RoleRunMode},
		}, nil
	})()

	_, _, _, err = s.mgr.RunningSystemAndGadgetAndEncryptionInfo()
	c.Assert(err, IsNil)

	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		return nil, fmt.Errorf("action failed")
	})()

	preinstallAction := &secboot.PreinstallAction{
		Action: "test-action",
	}
	system, gadgetInfo, encInfo, err := s.mgr.ApplyActionOnRunningSystemAndGadgetAndEncryptionInfo(preinstallAction)
	c.Assert(err, IsNil)
	c.Assert(system, NotNil)
	c.Assert(gadgetInfo, NotNil)
	c.Assert(encInfo, NotNil)

	c.Check(encInfo.Available, Equals, false)
	c.Check(encInfo.UnavailableErr, ErrorMatches, `action failed`)
}

func (s *runningSystemInfoSuite) TestGenerateReprovisionRecoveryKeyHappy(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	rkeyVal := keys.RecoveryKey{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	defer devicestate.MockFdestateGenerateRecoveryKey(func(st *state.State) (rkey keys.RecoveryKey, keyID string, err error) {
		return rkeyVal, "test-key-id", nil
	})()

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (rkey keys.RecoveryKey, err error) {
		c.Check(keyID, Equals, "test-key-id")
		return rkeyVal, nil
	})()

	rkey, err := devicestate.GenerateReprovisionRecoveryKey(st)
	c.Assert(err, IsNil)
	c.Check(rkey, DeepEquals, rkeyVal)

	cached := st.Cached(devicestate.ReprovisionSetupDataKey{})
	c.Assert(cached, NotNil)
	data, ok := cached.(*devicestate.ReprovisionSetupDataType)
	c.Assert(ok, Equals, true)

	cachedKeyID := devicestate.GetCachedReprovisionRecoveryKeyID(data)
	c.Check(cachedKeyID, Equals, "test-key-id")
}

func (s *runningSystemInfoSuite) TestGenerateReprovisionRecoveryKeyUpdatesCache(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	rkey1 := keys.RecoveryKey{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	defer devicestate.MockFdestateGenerateRecoveryKey(func(st *state.State) (rkey keys.RecoveryKey, keyID string, err error) {
		return rkey1, "key-1", nil
	})()

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (rkey keys.RecoveryKey, err error) {
		return rkey1, nil
	})()

	rkey, err := devicestate.GenerateReprovisionRecoveryKey(st)
	c.Assert(err, IsNil)
	c.Check(rkey, DeepEquals, rkey1)

	// Call again with a different key to test the update path
	rkey2 := keys.RecoveryKey{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	defer devicestate.MockFdestateGenerateRecoveryKey(func(st *state.State) (rkey keys.RecoveryKey, keyID string, err error) {
		return rkey2, "key-2", nil
	})()

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (rkey keys.RecoveryKey, err error) {
		return rkey2, nil
	})()

	rkeyNew, err := devicestate.GenerateReprovisionRecoveryKey(st)
	c.Assert(err, IsNil)
	c.Assert(rkeyNew, DeepEquals, rkey2)

	cached := st.Cached(devicestate.ReprovisionSetupDataKey{})
	c.Assert(cached, NotNil)
	data, ok := cached.(*devicestate.ReprovisionSetupDataType)
	c.Assert(ok, Equals, true)
	cachedKeyID := devicestate.GetCachedReprovisionRecoveryKeyID(data)
	c.Check(cachedKeyID, Equals, "key-2")
}

func (s *runningSystemInfoSuite) TestGenerateReprovisionRecoveryKeyGenerationError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	defer devicestate.MockFdestateGenerateRecoveryKey(func(st *state.State) (rkey keys.RecoveryKey, keyID string, err error) {
		return keys.RecoveryKey{}, "", fmt.Errorf("generation failed")
	})()

	_, err := devicestate.GenerateReprovisionRecoveryKey(st)
	c.Assert(err, ErrorMatches, `generation failed`)
}

type handlersReprovisionSuite struct {
	deviceMgrBaseSuite

	dataKeys *mockContainer
	saveKeys *mockContainer
}

var _ = Suite(&handlersReprovisionSuite{})

func (s *handlersReprovisionSuite) SetUpTest(c *C) {
	const classic = true
	s.setupBaseTest(c, classic)

	s.dataKeys = &mockContainer{
		keys: map[string]*mockKey{
			"default":          {isRecovery: false, key: []byte("old-data")},
			"default-fallback": {isRecovery: false, key: []byte("old-data-fallback")},
			"default-recovery": {isRecovery: true, key: []byte("old-data-recovery")},
		},
	}

	s.saveKeys = &mockContainer{
		keys: map[string]*mockKey{
			"default":          {isRecovery: false, key: []byte("old-save")},
			"default-fallback": {isRecovery: false, key: []byte("old-save-fallback")},
			"default-recovery": {isRecovery: true, key: []byte("old-save-recovery")},
		},
	}

	dataContainer := &mockEncryptedContainer{devPath: "/dev/data", containerRole: "system-data"}
	saveContainer := &mockEncryptedContainer{devPath: "/dev/save", containerRole: "system-save"}
	s.AddCleanup(devicestate.MockFdestateGetEncryptedContainers(func(st *state.State) (containers []fdeBackend.EncryptedContainer, err error) {
		return []fdeBackend.EncryptedContainer{dataContainer, saveContainer}, nil
	}))

	s.AddCleanup(devicestate.MockSecbootListContainerRecoveryKeyNames(func(disk string) ([]string, error) {
		var ret []string
		switch disk {
		case "/dev/data":
			for name, key := range s.dataKeys.keys {
				if key.isRecovery {
					ret = append(ret, name)
				}
			}
		case "/dev/save":
			for name, key := range s.saveKeys.keys {
				if key.isRecovery {
					ret = append(ret, name)
				}
			}
		default:
			c.Errorf("unexpected disk")
			return nil, fmt.Errorf("unexpected disk")
		}
		return ret, nil
	}))

	s.AddCleanup(devicestate.MockSecbootDeleteContainerKey(func(disk string, name string) error {
		switch disk {
		case "/dev/data":
			delete(s.dataKeys.keys, name)
		case "/dev/save":
			delete(s.saveKeys.keys, name)
		default:
			c.Errorf("unexpected disk")
			return fmt.Errorf("unexpected disk")
		}
		return nil
	}))

	s.AddCleanup(devicestate.MockSecbootListContainerUnlockKeyNames(func(disk string) ([]string, error) {
		var ret []string
		switch disk {
		case "/dev/data":
			for name, key := range s.dataKeys.keys {
				if !key.isRecovery {
					ret = append(ret, name)
				}
			}
		case "/dev/save":
			for name, key := range s.saveKeys.keys {
				if !key.isRecovery {
					ret = append(ret, name)
				}
			}
		default:
			c.Errorf("unexpected disk")
			return nil, fmt.Errorf("unexpected disk")
		}
		return ret, nil
	}))

	s.AddCleanup(devicestate.MockSecbootRenameContainerKey(func(disk string, from string, to string) error {
		var diskKeys *mockContainer

		switch disk {
		case "/dev/data":
			diskKeys = s.dataKeys
		case "/dev/save":
			diskKeys = s.saveKeys
		default:
			c.Errorf("unexpected disk")
			return fmt.Errorf("unexpected disk")
		}

		if _, hadKey := diskKeys.keys[to]; hadKey {
			return fmt.Errorf("key already exists")
		}

		key, hasKey := diskKeys.keys[from]
		if !hasKey {
			return fmt.Errorf("key did not exist")
		}

		diskKeys.keys[to] = key
		delete(diskKeys.keys, from)

		return nil
	}))

	s.AddCleanup(devicestate.MockSecbootAddBootstrapKeyOnExistingDisk(func(node string, newKey keys.EncryptionKey) error {
		switch node {
		case "/dev/data":
			s.dataKeys.keys["bootstrap-key"] = &mockKey{isRecovery: false, key: []byte("temporary-data-key")}
		case "/dev/save":
			s.saveKeys.keys["bootstrap-key"] = &mockKey{isRecovery: false, key: []byte("temporary-save-key")}
		default:
			c.Errorf("unexpected disk")
			return fmt.Errorf("unexpected disk")
		}
		return nil
	}))

	s.AddCleanup(devicestate.MockSecbootCreateBootstrappedContainer(func(key secboot.DiskUnlockKey, devicePath string) secboot.BootstrappedContainer {
		switch devicePath {
		case "/dev/data":
			return &mockBootstrappedContainer{container: s.dataKeys}
		case "/dev/save":
			return &mockBootstrappedContainer{container: s.saveKeys}
		default:
			c.Errorf("unexpected disk")
			return nil
		}
	}))

	c.Assert(os.MkdirAll(dirs.SnapFDEDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapFDEDir, "ubuntu-save.key"), []byte("save-key"), 0644), IsNil)

}

func (s *handlersReprovisionSuite) setupModel(c *C) {
	devicestatetest.SetDevice(s.state, &auth.DeviceState{
		Brand:  "canonical",
		Model:  "hybrid",
		Serial: "serialserialserial",
	})
	s.makeModelAssertionInState(c, "canonical", "hybrid",
		map[string]any{
			"architecture": "amd64",
			"classic":      "true",
			"distribution": "ubuntu",
			"base":         "core26",
			"snaps": []any{
				map[string]any{
					"name": "pc-kernel",
					"id":   "pckernelidididididididididididid",
					"type": "kernel",
				},
				map[string]any{
					"name": "pc",
					"id":   "pcididididididididididididididid",
					"type": "gadget",
				},
				map[string]any{
					"name": "core26",
					"id":   "core26ididididididididididididid",
					"type": "base",
				},
			},
		})
}

type mockEncryptedContainer struct {
	devPath       string
	containerRole string
}

func (m *mockEncryptedContainer) DevPath() string {
	return m.devPath
}

func (m *mockEncryptedContainer) ContainerRole() string {
	return m.containerRole
}

func (m *mockEncryptedContainer) LegacyKeys() map[string]string {
	return nil
}

type mockKey struct {
	isRecovery bool
	key        []byte
	token      []byte
}

type mockContainer struct {
	keys map[string]*mockKey
}

type mockKeyDataWriter struct {
	container *mockContainer
	slotName  string
	buf       bytes.Buffer
}

func (m *mockKeyDataWriter) Write(p []byte) (n int, err error) {
	return m.buf.Write(p)
}

func (m *mockKeyDataWriter) Commit() error {
	m.container.keys[m.slotName].token = m.buf.Bytes()
	return nil
}

type mockBootstrappedContainer struct {
	container *mockContainer
}

func (m *mockBootstrappedContainer) AddKey(slotName string, newKey []byte) error {
	if _, has := m.container.keys[slotName]; has {
		return fmt.Errorf("already has key")
	}
	m.container.keys[slotName] = &mockKey{isRecovery: false, key: newKey}
	return nil
}

func (m *mockBootstrappedContainer) AddRecoveryKey(slotName string, rkey keys.RecoveryKey) error {
	if _, has := m.container.keys[slotName]; has {
		return fmt.Errorf("already has key")
	}
	m.container.keys[slotName] = &mockKey{isRecovery: true, key: rkey[:]}
	return nil
}

func (m *mockBootstrappedContainer) GetTokenWriter(slotName string) (secboot.KeyDataWriter, error) {
	return &mockKeyDataWriter{container: m.container, slotName: slotName}, nil
}

func (m *mockBootstrappedContainer) RemoveBootstrapKey() error {
	if _, has := m.container.keys["bootstrap-key"]; !has {
		return fmt.Errorf("missing bootstrap key")
	}
	delete(m.container.keys, "bootstrap-key")
	return nil
}

func (m *mockBootstrappedContainer) RegisterKeyAsUsed(primaryKey []byte, unlockKey []byte) {
}

func (s *handlersReprovisionSuite) testDoReprovisionHappy(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockSecbootGetPCRHandleFromToken(func(disk string, name string) (uint32, error) {
		switch disk {
		case "/dev/data":
			switch name {
			case "default":
				// Called during cleanup from previous unclean reprovision
				return 0, nil
			case "default-fallback":
				// Called during cleanup from previous unclean reprovision
				return 42, nil
			case "snapd-reprovision-default":
				return 0, nil
			case "snapd-reprovision-default-fallback":
				return 42, nil
			default:
				c.Errorf("unexpected key %s:%s", disk, name)
				return 0, fmt.Errorf("unexpected key")
			}
		case "/dev/save":
			switch name {
			case "default":
				// Called during cleanup from previous unclean reprovision
				return 42, nil
			case "default-fallback":
				// Called during cleanup from previous unclean reprovision
				return 42, nil
			case "snapd-reprovision-default":
				return 42, nil
			case "snapd-reprovision-default-fallback":
				return 42, nil
			default:
				c.Errorf("unexpected key %s:%s", disk, name)
				return 0, fmt.Errorf("unexpected key")
			}
		default:
			c.Errorf("unexpected disk")
			return 0, fmt.Errorf("unexpected disk")
		}
	})()

	defer devicestate.MockSecbootReleasePCRResourceHandle(func(nv uint32) error {
		c.Check(nv, Equals, uint32(42))
		return nil
	})()

	defer devicestate.MockSecbootTestProtectorKey(func(ctx context.Context, disk string, keyName string, key []byte) (bool, error) {
		switch keyName {
		case "snapd-reprovision-default":
			// this is the happy case, so if we find an old key, we say it is working
			return true, nil
		default:
			c.Errorf("unexpected")
			return false, fmt.Errorf("unexpected")
		}
	})()

	preinstallCheckActionsCalls := 0
	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		preinstallCheckActionsCalls++
		return []secboot.PreinstallErrorDetails{}, nil
	})()

	saveCheckResultsCalls := 0
	defer devicestate.MockSecbootSaveCheckResult(func(pcc *secboot.PreinstallCheckContext, filename string) error {
		saveCheckResultsCalls++
		return nil
	})()

	defer devicestate.MockSecbootCheckResult(func(pcc *secboot.PreinstallCheckContext) (*secboot.PreinstallCheckResult, error) {
		return &secboot.PreinstallCheckResult{}, nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		sideInfo := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: "pc-kernel-id"}
		snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "kernel",
		})
		yml := `
name: pc-kernel
version: 1.0
`
		return snaptest.MockSnap(c, yml, sideInfo), nil
	})()

	defer devicestate.MockKeysNewProtectorKey(func() (keys.ProtectorKey, error) {
		return keys.ProtectorKey([]byte("new-protector")), nil
	})()

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		c.Check(primaryKey, IsNil)
		c.Check(k, DeepEquals, keys.ProtectorKey([]byte("new-protector")))
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		c.Check(key, Equals, plainKey)
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockHookKeyProtectorFactory(func(m *devicestate.DeviceManager, s *snap.Info) (secboot.KeyProtectorFactory, error) {
		return nil, secboot.ErrNoKeyProtector
	})()

	bootMakeRunnableReprovisionCalls := 0
	defer devicestate.MockBootMakeRunnableReprovision(func(model *asserts.Model, protector secboot.KeyProtectorFactory, encryption *boot.EncryptionSetup) error {
		bootMakeRunnableReprovisionCalls++

		c.Check(encryption.PrimaryKey(), DeepEquals, []byte("new-primary-key"))

		c.Check(protector, IsNil)

		// Simulate what is expected
		s.dataKeys.keys["default"] = &mockKey{
			isRecovery: false,
			key:        []byte("new-data-default"),
			token:      []byte("new-data-default-token"),
		}
		s.dataKeys.keys["default-fallback"] = &mockKey{
			isRecovery: false,
			key:        []byte("new-data-fallback"),
			token:      []byte("new-data-fallback-token"),
		}
		s.saveKeys.keys["default-fallback"] = &mockKey{
			isRecovery: false,
			key:        []byte("new-save-fallback"),
			token:      []byte("new-save-fallback-token"),
		}
		delete(s.dataKeys.keys, "bootstrap-key")
		delete(s.saveKeys.keys, "bootstrap-key")

		return nil
	})()

	modeenv := &boot.Modeenv{
		Mode: "run",
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	st.Set("fde", "not-modified")

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, IsNil)

	c.Check(preinstallCheckActionsCalls, Equals, 1)
	c.Check(saveCheckResultsCalls, Equals, 1)
	c.Check(bootMakeRunnableReprovisionCalls, Equals, 1)

	c.Check(s.dataKeys.keys, DeepEquals, map[string]*mockKey{
		"default": {
			isRecovery: false,
			key:        []byte("new-data-default"),
			token:      []byte("new-data-default-token"),
		},
		"default-fallback": {
			isRecovery: false,
			key:        []byte("new-data-fallback"),
			token:      []byte("new-data-fallback-token"),
		},
		"default-recovery": {
			isRecovery: true,
			key:        []byte{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4},
		},
	})
	c.Check(s.saveKeys.keys, DeepEquals, map[string]*mockKey{
		"default": {
			isRecovery: false,
			key:        []byte("new-save-default"),
			token:      []byte("new-save-default-token"),
		},
		"default-fallback": {
			isRecovery: false,
			key:        []byte("new-save-fallback"),
			token:      []byte("new-save-fallback-token"),
		},
		"default-recovery": {
			isRecovery: true,
			key:        []byte{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4},
		},
	})

	newSaveKey, err := os.ReadFile(filepath.Join(dirs.SnapFDEDir, "ubuntu-save.key"))
	c.Assert(err, IsNil)
	c.Assert(newSaveKey, DeepEquals, []byte("new-protector"))

	var newState any
	err = st.Get("fde", &newState)
	c.Check(err, testutil.ErrorIs, state.ErrNoState)
}

func (s *handlersReprovisionSuite) TestDoReprovisionHappy(c *C) {
	s.testDoReprovisionHappy(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionHappyWithFailedPreviousReprovision(c *C) {
	s.dataKeys.keys["snapd-reprovision-default"] = &mockKey{isRecovery: false, key: []byte("old-data-failed")}
	s.dataKeys.keys["snapd-reprovision-default-fallback"] = &mockKey{isRecovery: false, key: []byte("old-data-fallback-failed")}
	s.dataKeys.keys["snapd-reprovision-default-recovery"] = &mockKey{isRecovery: true, key: []byte("old-data-recovery-failed")}

	// We do not add this one, it makes it a reprovision that was not completed:
	// s.saveKeys.keys["snapd-reprovision-default"] = &mockKey{isRecovery: false, key: []byte("old-save-failed")}

	s.saveKeys.keys["snapd-reprovision-default-fallback"] = &mockKey{isRecovery: false, key: []byte("old-save-fallback-failed")}
	s.saveKeys.keys["snapd-reprovision-default-recovery"] = &mockKey{isRecovery: true, key: []byte("old-save-recovery-failed")}

	s.testDoReprovisionHappy(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionHappyWithNotCleanedPreviousReprovision(c *C) {
	s.dataKeys.keys["snapd-reprovision-default"] = &mockKey{isRecovery: false, key: []byte("old-data-success")}
	s.dataKeys.keys["snapd-reprovision-default-fallback"] = &mockKey{isRecovery: false, key: []byte("old-data-fallback-success")}
	s.dataKeys.keys["snapd-reprovision-default-recovery"] = &mockKey{isRecovery: true, key: []byte("old-data-recovery-success")}

	// This has been completed, but not cleaned up
	s.saveKeys.keys["snapd-reprovision-default"] = &mockKey{isRecovery: false, key: []byte("old-save-success")}

	s.saveKeys.keys["snapd-reprovision-default-fallback"] = &mockKey{isRecovery: false, key: []byte("old-save-fallback-success")}
	s.saveKeys.keys["snapd-reprovision-default-recovery"] = &mockKey{isRecovery: true, key: []byte("old-save-recovery-success")}

	s.testDoReprovisionHappy(c)
}

func (s *handlersReprovisionSuite) verifyRollback(c *C) {
	// Verify rollback
	c.Check(s.dataKeys.keys, DeepEquals, map[string]*mockKey{
		"default": {
			isRecovery: false,
			key:        []byte("old-data"),
		},
		"default-fallback": {
			isRecovery: false,
			key:        []byte("old-data-fallback"),
		},
		"default-recovery": {
			isRecovery: true,
			key:        []byte("old-data-recovery"),
		},
	})
	c.Check(s.saveKeys.keys, DeepEquals, map[string]*mockKey{
		"default": {
			isRecovery: false,
			key:        []byte("old-save"),
		},
		"default-fallback": {
			isRecovery: false,
			key:        []byte("old-save-fallback"),
		},
		"default-recovery": {
			isRecovery: true,
			key:        []byte("old-save-recovery"),
		},
	})
}

func (s *handlersReprovisionSuite) TestDoReprovisionSaveProtectorKeyFailure(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockSecbootGetPCRHandleFromToken(func(disk string, name string) (uint32, error) {
		switch disk {
		case "/dev/data":
			switch name {
			case "default":
				// Called during cleanup from previous unclean reprovision
				return 0, nil
			case "default-fallback":
				// Called during cleanup from previous unclean reprovision
				return 42, nil
			case "snapd-reprovision-default":
				return 0, nil
			case "snapd-reprovision-default-fallback":
				return 42, nil
			default:
				c.Errorf("unexpected key %s:%s", disk, name)
				return 0, fmt.Errorf("unexpected key")
			}
		case "/dev/save":
			switch name {
			case "default":
				// Called during cleanup from previous unclean reprovision
				return 42, nil
			case "default-fallback":
				// Called during cleanup from previous unclean reprovision
				return 42, nil
			case "snapd-reprovision-default":
				return 42, nil
			case "snapd-reprovision-default-fallback":
				return 42, nil
			default:
				c.Errorf("unexpected key %s:%s", disk, name)
				return 0, fmt.Errorf("unexpected key")
			}
		default:
			c.Errorf("unexpected disk")
			return 0, fmt.Errorf("unexpected disk")
		}
	})()

	defer devicestate.MockSecbootReleasePCRResourceHandle(func(nv uint32) error {
		c.Check(nv, Equals, uint32(42))
		return nil
	})()

	defer devicestate.MockSecbootTestProtectorKey(func(ctx context.Context, disk string, keyName string, key []byte) (bool, error) {
		switch keyName {
		case "snapd-reprovision-default":
			// this is the happy case, so if we find an old key, we say it is working
			return true, nil
		default:
			c.Errorf("unexpected")
			return false, fmt.Errorf("unexpected")
		}
	})()

	preinstallCheckActionsCalls := 0
	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		preinstallCheckActionsCalls++
		return []secboot.PreinstallErrorDetails{}, nil
	})()

	saveCheckResultsCalls := 0
	defer devicestate.MockSecbootSaveCheckResult(func(pcc *secboot.PreinstallCheckContext, filename string) error {
		saveCheckResultsCalls++
		return nil
	})()

	defer devicestate.MockSecbootCheckResult(func(pcc *secboot.PreinstallCheckContext) (*secboot.PreinstallCheckResult, error) {
		return &secboot.PreinstallCheckResult{}, nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		sideInfo := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: "pc-kernel-id"}
		snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "kernel",
		})
		yml := `
name: pc-kernel
version: 1.0
`
		return snaptest.MockSnap(c, yml, sideInfo), nil
	})()

	defer devicestate.MockKeysNewProtectorKey(func() (keys.ProtectorKey, error) {
		return keys.ProtectorKey([]byte("new-protector")), nil
	})()

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		c.Check(primaryKey, IsNil)
		c.Check(k, DeepEquals, keys.ProtectorKey([]byte("new-protector")))
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		c.Check(key, Equals, plainKey)
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockHookKeyProtectorFactory(func(m *devicestate.DeviceManager, s *snap.Info) (secboot.KeyProtectorFactory, error) {
		return nil, secboot.ErrNoKeyProtector
	})()

	bootMakeRunnableReprovisionCalls := 0
	defer devicestate.MockBootMakeRunnableReprovision(func(model *asserts.Model, protector secboot.KeyProtectorFactory, encryption *boot.EncryptionSetup) error {
		bootMakeRunnableReprovisionCalls++

		c.Check(encryption.PrimaryKey(), DeepEquals, []byte("new-primary-key"))

		c.Check(protector, IsNil)

		// Simulate what is expected
		s.dataKeys.keys["default"] = &mockKey{
			isRecovery: false,
			key:        []byte("new-data-default"),
			token:      []byte("new-data-default-token"),
		}
		s.dataKeys.keys["default-fallback"] = &mockKey{
			isRecovery: false,
			key:        []byte("new-data-fallback"),
			token:      []byte("new-data-fallback-token"),
		}
		s.saveKeys.keys["default-fallback"] = &mockKey{
			isRecovery: false,
			key:        []byte("new-save-fallback"),
			token:      []byte("new-save-fallback-token"),
		}
		delete(s.dataKeys.keys, "bootstrap-key")
		delete(s.saveKeys.keys, "bootstrap-key")

		return nil
	})()

	modeenv := &boot.Modeenv{
		Mode: "run",
	}
	c.Assert(modeenv.WriteTo(""), IsNil)

	defer devicestate.MockKeysSaveProtectorKey(func(key keys.ProtectorKey, path string) error {
		c.Check(path, Equals, filepath.Join(dirs.GlobalRootDir, "/var/lib/snapd/device/fde/ubuntu-save.key"))
		return fmt.Errorf("boom")
	})()

	st.Set("fde", "not-modified")

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "cannot save the system-save key: boom")
	s.verifyRollback(c)

	var newState any
	err = st.Get("fde", &newState)
	c.Assert(err, IsNil)
	c.Check(newState, Equals, "not-modified")
}

func (s *handlersReprovisionSuite) TestDoReprovisionMissingCache(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	// Don't set up any cache data - this simulates missing reprovision context
	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "missing reprovision context")
	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionWrongCacheType(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	// Set up wrong cache type
	st.Cache(devicestate.ReprovisionSetupDataKey{}, "wrong-type")

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, `internal error: wrong data type for reprovisionSetupDataKey`)
	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionMissingCheckContext(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	// Create setupData with recovery key but nil checkContext
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", nil))

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "missing post install check context")
	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionMissingRecoveryKey(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{}, fmt.Errorf("wot no key")
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "wot no key")
	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionUnsetRecoveryKey(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Errorf("unexpected")
		return keys.RecoveryKey{}, fmt.Errorf("unexpected")
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("", preinstallCheckContext))

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "missing recovery key")
}

func (s *handlersReprovisionSuite) TestDoReprovisionNewProtectorKeyError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	defer devicestate.MockSecbootGetPCRHandleFromToken(func(disk string, name string) (uint32, error) {
		return 0, nil
	})()

	defer devicestate.MockSecbootReleasePCRResourceHandle(func(nv uint32) error {
		return nil
	})()

	defer devicestate.MockSecbootTestProtectorKey(func(ctx context.Context, disk string, keyName string, key []byte) (bool, error) {
		c.Errorf("unexpected")
		return false, fmt.Errorf("unexpected")
	})()

	defer devicestate.MockKeysNewProtectorKey(func() (keys.ProtectorKey, error) {
		return nil, fmt.Errorf("protector key failed")
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "protector key failed")
	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionCreateProtectedKeyError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	defer devicestate.MockSecbootGetPCRHandleFromToken(func(disk string, name string) (uint32, error) {
		return 0, nil
	})()

	defer devicestate.MockSecbootReleasePCRResourceHandle(func(nv uint32) error {
		return nil
	})()

	defer devicestate.MockSecbootTestProtectorKey(func(ctx context.Context, disk string, keyName string, key []byte) (bool, error) {
		c.Errorf("unexpected")
		return false, fmt.Errorf("unexpected")
	})()

	defer devicestate.MockKeysNewProtectorKey(func() (keys.ProtectorKey, error) {
		return keys.ProtectorKey([]byte("new-protector")), nil
	})()

	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		return nil, nil, nil, fmt.Errorf("protected key creation failed")
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "protected key creation failed")
	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionKernelInfoError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		return nil, fmt.Errorf("kernel info failed")
	})()

	c.Assert(os.MkdirAll(dirs.SnapFDEDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(dirs.SnapFDEDir, "ubuntu-save.key"), []byte("save-key"), 0644), IsNil)

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "cannot get kernel info: kernel info failed")
	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionKeyProtectorError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		sideInfo := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: "pc-kernel-id"}
		snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "kernel",
		})
		yml := `
name: pc-kernel
version: 1.0
`
		return snaptest.MockSnap(c, yml, sideInfo), nil
	})()

	defer devicestate.MockHookKeyProtectorFactory(func(m *devicestate.DeviceManager, s *snap.Info) (secboot.KeyProtectorFactory, error) {
		return nil, fmt.Errorf("key protector failed")
	})()

	defer devicestate.MockSecbootSaveCheckResult(func(pcc *secboot.PreinstallCheckContext, filename string) error {
		return nil
	})()

	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		return []secboot.PreinstallErrorDetails{}, nil
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "key protector failed")
	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionPreinstallCheckError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		sideInfo := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: "pc-kernel-id"}
		snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "kernel",
		})
		yml := `
name: pc-kernel
version: 1.0
`
		return snaptest.MockSnap(c, yml, sideInfo), nil
	})()

	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		return nil, fmt.Errorf("preinstall check failed")
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "preinstall check failed")

	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionPreinstallCheckErrorDetails(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		sideInfo := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: "pc-kernel-id"}
		snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "kernel",
		})
		yml := `
name: pc-kernel
version: 1.0
`
		return snaptest.MockSnap(c, yml, sideInfo), nil
	})()

	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		return []secboot.PreinstallErrorDetails{{}}, nil
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "postinstall check found some issues")

	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionMakeRunnableError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	plainKey := &keys.PlainKey{}
	defer devicestate.MockKeysCreateProtectedKey(func(k keys.ProtectorKey, primaryKey []byte) (*keys.PlainKey, []byte, []byte, error) {
		return plainKey, []byte("new-primary-key"), []byte("new-save-default"), nil
	})()

	defer devicestate.MockKeysPlainKeyWrite(func(key *keys.PlainKey, writer keys.KeyDataWriter) error {
		_, err := writer.Write([]byte("new-save-default-token"))
		c.Assert(err, IsNil)
		err = writer.Commit()
		c.Assert(err, IsNil)
		return nil
	})()

	defer devicestate.MockSnapstateKernelInfo(func(st *state.State, deviceCtx snapstate.DeviceContext) (*snap.Info, error) {
		sideInfo := &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: "pc-kernel-id"}
		snapstate.Set(st, "pc-kernel", &snapstate.SnapState{
			Active:   true,
			Sequence: snapstatetest.NewSequenceFromSnapSideInfos([]*snap.SideInfo{sideInfo}),
			Current:  sideInfo.Revision,
			SnapType: "kernel",
		})
		yml := `
name: pc-kernel
version: 1.0
`
		return snaptest.MockSnap(c, yml, sideInfo), nil
	})()

	defer devicestate.MockSecbootPreinstallCheckAction(func(pcc *secboot.PreinstallCheckContext, ctx context.Context, action *secboot.PreinstallAction) ([]secboot.PreinstallErrorDetails, error) {
		return []secboot.PreinstallErrorDetails{}, nil
	})()

	defer devicestate.MockSecbootSaveCheckResult(func(pcc *secboot.PreinstallCheckContext, filename string) error {
		return nil
	})()

	defer devicestate.MockSecbootCheckResult(func(pcc *secboot.PreinstallCheckContext) (*secboot.PreinstallCheckResult, error) {
		return &secboot.PreinstallCheckResult{}, nil
	})()

	defer devicestate.MockBootMakeRunnableReprovision(func(model *asserts.Model, protector secboot.KeyProtectorFactory, encryption *boot.EncryptionSetup) error {
		return fmt.Errorf("make runnable failed")
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "cannot make system runnable: make runnable failed")

	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionFdestateGetError(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	// Mock fdestateGetEncryptedContainers to return an error (e.g., disk gone)
	defer devicestate.MockFdestateGetEncryptedContainers(func(st *state.State) ([]fdeBackend.EncryptedContainer, error) {
		return nil, fmt.Errorf("storage backend failure")
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "storage backend failure")

	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionMultipleDataDisks(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	// Mock fdestateGetEncryptedContainers to return multiple system-data disks
	defer devicestate.MockFdestateGetEncryptedContainers(func(st *state.State) ([]fdeBackend.EncryptedContainer, error) {
		return []fdeBackend.EncryptedContainer{
			&mockEncryptedContainer{devPath: "/dev/data1", containerRole: "system-data"},
			&mockEncryptedContainer{devPath: "/dev/data2", containerRole: "system-data"},
			&mockEncryptedContainer{devPath: "/dev/save", containerRole: "system-save"},
		}, nil
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "multiple containers found with role system-data")

	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionMultipleSaveDisks(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	// Mock fdestateGetEncryptedContainers to return multiple system-save disks
	defer devicestate.MockFdestateGetEncryptedContainers(func(st *state.State) ([]fdeBackend.EncryptedContainer, error) {
		return []fdeBackend.EncryptedContainer{
			&mockEncryptedContainer{devPath: "/dev/data", containerRole: "system-data"},
			&mockEncryptedContainer{devPath: "/dev/save1", containerRole: "system-save"},
			&mockEncryptedContainer{devPath: "/dev/save2", containerRole: "system-save"},
		}, nil
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "multiple containers found with role system-save")

	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionNoSaveDisk(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	// Mock fdestateGetEncryptedContainers to return only data disk (no save)
	defer devicestate.MockFdestateGetEncryptedContainers(func(st *state.State) ([]fdeBackend.EncryptedContainer, error) {
		return []fdeBackend.EncryptedContainer{
			&mockEncryptedContainer{devPath: "/dev/data", containerRole: "system-data"},
		}, nil
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "no save container found")

	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestDoReprovisionNoDataDisk(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	s.setupModel(c)

	chg := st.NewChange("fde-reprovision", "...")
	t := st.NewTask("fde-reprovision", "reprovision test")
	chg.AddTask(t)

	defer devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (keys.RecoveryKey, error) {
		c.Check(keyID, Equals, "key-id")
		return keys.RecoveryKey{1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4, 1, 2, 3, 4}, nil
	})()
	preinstallCheckContext := &secboot.PreinstallCheckContext{}
	st.Cache(devicestate.ReprovisionSetupDataKey{}, devicestate.MakeReprovisionSetupData("key-id", preinstallCheckContext))

	// Mock fdestateGetEncryptedContainers to return only save disk (no data)
	defer devicestate.MockFdestateGetEncryptedContainers(func(st *state.State) ([]fdeBackend.EncryptedContainer, error) {
		return []fdeBackend.EncryptedContainer{
			&mockEncryptedContainer{devPath: "/dev/save", containerRole: "system-save"},
		}, nil
	})()

	err := func() error {
		st.Unlock()
		defer st.Lock()
		return devicestate.DoReprovision(s.mgr, t)
	}()
	c.Assert(err, ErrorMatches, "no data container found")

	s.verifyRollback(c)
}

func (s *handlersReprovisionSuite) TestReprovisionCreateChange(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	chg, err := devicestate.Reprovision(st)
	c.Assert(err, IsNil)

	c.Check(chg.Err(), IsNil)
	c.Check(chg.Status(), Equals, state.DoStatus)
}

func (s *handlersReprovisionSuite) TestReprovisionCreateChangeAlreadyRunning(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	conflictChg := st.NewChange("fde-reprovision", "...")
	conflictTask := st.NewTask("fde-reprovision", "...")
	conflictChg.AddTask(conflictTask)

	_, err := devicestate.Reprovision(st)

	var confictError *snapstate.ChangeConflictError
	c.Assert(errors.As(err, &confictError), Equals, true)
	c.Check(confictError.ChangeKind, Equals, "fde-reprovision")
	c.Check(confictError.Message, Equals, "reprovision is in progress, no other FDE changes allowed until this is done")
	c.Check(confictError.ChangeID, Equals, conflictChg.ID())
}

func (s *handlersReprovisionSuite) TestReprovisionCreateChangeOtherFDEChangeRunning(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	conflictChg := st.NewChange("fde-change-passphrase", "...")
	conflictTask := st.NewTask("something", "...")
	conflictChg.AddTask(conflictTask)

	_, err := devicestate.Reprovision(st)

	var confictError *snapstate.ChangeConflictError
	c.Assert(errors.As(err, &confictError), Equals, true)
	c.Check(confictError.ChangeKind, Equals, "fde-change-passphrase")
	c.Check(confictError.Message, Equals, "changing passphrase in progress, no other FDE changes allowed until this is done")
	c.Check(confictError.ChangeID, Equals, conflictChg.ID())
}

func (s *handlersReprovisionSuite) TestReprovisionCreateChangeOtherFDETaskRunning(c *C) {
	st := s.state
	st.Lock()
	defer st.Unlock()

	conflictChg := st.NewChange("something", "...")
	conflictTask := st.NewTask("fde-something", "...")
	conflictChg.AddTask(conflictTask)

	_, err := devicestate.Reprovision(st)

	var confictError *snapstate.ChangeConflictError
	c.Assert(errors.As(err, &confictError), Equals, true)
	c.Check(confictError.ChangeKind, Equals, "something")
	c.Check(confictError.Message, Equals, "FDE change in progress, no other FDE changes allowed until this is done")
	c.Check(confictError.ChangeID, Equals, conflictChg.ID())
}
