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
	"context"
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
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
