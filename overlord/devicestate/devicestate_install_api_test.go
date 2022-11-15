// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
	. "gopkg.in/check.v1"
)

type deviceMgrInstallAPISuite struct {
	deviceMgrBaseSuite
	*seedtest.TestingSeed20
}

var _ = Suite(&deviceMgrInstallAPISuite{})

func (s *deviceMgrInstallAPISuite) SetUpTest(c *C) {
	classic := true
	s.deviceMgrBaseSuite.setupBaseTest(c, classic)

	// We uncompress a gadget with grub, and prefer not to mock in this case
	bootloader.Force(nil)

	restore := devicestate.MockSystemForPreseeding(func() (string, error) {
		return "fake system label", nil
	})
	s.AddCleanup(restore)

	s.TestingSeed20 = &seedtest.TestingSeed20{}
	s.SeedDir = dirs.SnapSeedDir

	s.state.Lock()
	defer s.state.Unlock()
	s.state.Set("seeded", true)
}

func unpackSnap(snapBlob, targetDir string) error {
	if out, err := exec.Command("unsquashfs", "-d", targetDir, "-f", snapBlob).CombinedOutput(); err != nil {
		return fmt.Errorf("cannot unsquashfs: %v", osutil.OutputErr(out, err))
	}
	return nil
}

func (s *deviceMgrInstallAPISuite) setupSystemSeed(c *C, sysLabel, gadgetYaml string, isClassic bool) *asserts.Model {
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["snapd"], nil, snap.R(1), "canonical", s.StoreSigning.Database)
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["pc-kernel=22"],
		[][]string{{"kernel.efi", ""}}, snap.R(1), "canonical", s.StoreSigning.Database)
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["core22"], nil, snap.R(1), "canonical", s.StoreSigning.Database)
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["pc=22"],
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			{"pc-boot.img", ""}, {"pc-core.img", ""}, {"grubx64.efi", ""},
			{"shim.efi.signed", ""}, {"grub.conf", ""}},
		snap.R(1), "canonical", s.StoreSigning.Database)

	model := map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core22",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              s.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              s.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "20",
			},
			map[string]interface{}{
				"name": "snapd",
				"id":   s.AssertedSnapID("snapd"),
				"type": "snapd",
			},
			map[string]interface{}{
				"name": "core22",
				"id":   s.AssertedSnapID("core22"),
				"type": "base",
			},
		},
	}
	if isClassic {
		model["classic"] = "true"
		model["distribution"] = "ubuntu"
	}

	return s.MakeSeed(c, sysLabel, "my-brand", "my-model", model, nil)
}

type finishStepOpts struct {
	encrypted bool
	isClassic bool
}

func (s *deviceMgrInstallAPISuite) mockSystemSeedWithLabel(c *C, label string, isClassic bool) (gadgetSnapPath, kernelSnapPath string, ginfo *gadget.Info, mountCmd *testutil.MockCmd) {
	// Mock partitioned disk
	gadgetYaml := gadgettest.SingleVolumeClassicWithModesGadgetYaml
	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	ginfo, _, _, restore, err := gadgettest.MockGadgetPartitionedDisk(gadgetYaml, gadgetRoot)
	c.Assert(err, IsNil)
	s.AddCleanup(restore)

	// now create a label with snaps/assertions
	s.SetupAssertSigning("canonical")
	s.Brands.Register("my-brand", brandPrivKey, map[string]interface{}{
		"verification": "verified",
	})
	// TODO This should be "gadgetYaml" instead of SingleVolumeUC20GadgetYaml,
	// but we have to do it this way as otherwise snap pack will complain
	// while validating, as it does not have information about the model at
	// that time. When that is fixed this must change to gadgetYaml.
	model := s.setupSystemSeed(c, label, gadgettest.SingleVolumeUC20GadgetYaml, isClassic)
	c.Check(model, NotNil)

	// Create fake seed that will return information from the label we created
	// (TODO: needs to be in sync with setupSystemSeed, fix that)
	kernelSnapPath = filepath.Join(s.SeedDir, "snaps", "pc-kernel_1.snap")
	baseSnapPath := filepath.Join(s.SeedDir, "snaps", "core22_1.snap")
	gadgetSnapPath = filepath.Join(s.SeedDir, "snaps", "pc_1.snap")
	restore = devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		return &fakeSeed{
			essentialSnaps: []*seed.Snap{
				{
					Path:          kernelSnapPath,
					SideInfo:      &snap.SideInfo{RealName: "pc-kernel", Revision: snap.R(1), SnapID: s.SeedSnaps.AssertedSnapID("pc-kernel")},
					EssentialType: snap.TypeKernel,
				},
				{
					Path:          baseSnapPath,
					SideInfo:      &snap.SideInfo{RealName: "core22", Revision: snap.R(1), SnapID: s.SeedSnaps.AssertedSnapID("core22")},
					EssentialType: snap.TypeBase,
				},
				{
					Path:          gadgetSnapPath,
					SideInfo:      &snap.SideInfo{RealName: "pc", Revision: snap.R(1), SnapID: s.SeedSnaps.AssertedSnapID("pc")},
					EssentialType: snap.TypeGadget,
				},
			},
			model: model,
		}, nil
	})
	s.AddCleanup(restore)

	// Mock calls to systemd-mount, which is used to mount snaps from the system label
	mountCmd = testutil.MockCommand(c, "systemd-mount", "")
	s.AddCleanup(func() { mountCmd.Restore() })

	return gadgetSnapPath, kernelSnapPath, ginfo, mountCmd
}

// TODO encryption case for the finish step is not tested yet, it needs more mocking
func (s *deviceMgrInstallAPISuite) testInstallFinishStep(c *C, opts finishStepOpts) {
	// TODO UC case when supported
	restore := release.MockOnClassic(opts.isClassic)
	s.AddCleanup(restore)

	// Mock label
	label := "classic"
	gadgetSnapPath, kernelSnapPath, ginfo, mountCmd := s.mockSystemSeedWithLabel(c, label, opts.isClassic)

	// Unpack gadget snap from seed where it would have been mounted
	gadgetDir := filepath.Join(dirs.SnapRunDir, "snap-content/gadget")
	err := os.MkdirAll(gadgetDir, 0755)
	c.Assert(err, IsNil)
	err = unpackSnap(filepath.Join(s.SeedDir, "snaps/pc_1.snap"), gadgetDir)
	c.Assert(err, IsNil)

	// Mock writing of contents
	writeContentCalls := 0
	restore = devicestate.MockInstallWriteContent(func(onVolumes map[string]*gadget.Volume, allLaidOutVols map[string]*gadget.LaidOutVolume, encSetupData *install.EncryptionSetupData, observer gadget.ContentObserver, perfTimings timings.Measurer) ([]*gadget.OnDiskVolume, error) {
		writeContentCalls++
		if opts.encrypted {
			c.Check(encSetupData, NotNil)
		} else {
			c.Check(encSetupData, IsNil)
		}
		return nil, nil
	})
	s.AddCleanup(restore)

	// Note that ESP must be mounted in the same place as a seed partition
	// so MarkRecoveryCapableSystem is happy when searching for a bootloader.
	// TODO Should this be changed?
	espDir := filepath.Join(dirs.RunDir, "mnt/ubuntu-seed")

	// Mock mounting of partitions
	mountVolsCalls := 0
	restore = devicestate.MockInstallMountVolumes(func(onVolumes map[string]*gadget.Volume, encSetupData *install.EncryptionSetupData) (espMntDir string, unmount func() error, err error) {
		mountVolsCalls++
		return espDir, func() error { return nil }, nil
	})
	s.AddCleanup(restore)
	s.state.Lock()
	defer s.state.Unlock()

	// Mock saving of traits
	saveStorageTraitsCalls := 0
	restore = devicestate.MockInstallSaveStorageTraits(func(model gadget.Model, allLaidOutVols map[string]*gadget.LaidOutVolume, encryptSetupData *install.EncryptionSetupData) error {
		saveStorageTraitsCalls++
		return nil
	})
	s.AddCleanup(restore)

	// Insert encryption data when enabled
	if opts.encrypted {
		restore = devicestate.MockEncryptionSetupDataInCache(s.state, label)
		s.AddCleanup(restore)
	}

	// Create change
	chg := s.state.NewChange("install-step-finish", "finish setup of run system")
	finishTask := s.state.NewTask("install-finish", "install API finish step")
	finishTask.Set("system-label", label)
	finishTask.Set("on-volumes", ginfo.Volumes)

	chg.AddTask(finishTask)

	// now let the change run - some checks will happen in the mocked functions
	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()
	c.Check(chg.Err(), IsNil)

	// Checks now
	kernelDir := filepath.Join(dirs.SnapRunDir, "snap-content/kernel")
	c.Check(mountCmd.Calls(), DeepEquals, [][]string{
		{"systemd-mount", gadgetSnapPath, gadgetDir},
		{"systemd-mount", kernelSnapPath, kernelDir},
		{"systemd-mount", "--umount", gadgetDir},
		{"systemd-mount", "--umount", kernelDir},
	})
	c.Check(writeContentCalls, Equals, 1)
	c.Check(mountVolsCalls, Equals, 1)
	c.Check(saveStorageTraitsCalls, Equals, 1)

	expectedFiles := []string{
		filepath.Join(espDir, "EFI/ubuntu/grub.cfg"),
		filepath.Join(espDir, "EFI/ubuntu/grubenv"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/ubuntu/grub.cfg"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/ubuntu/grubenv"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/ubuntu/pc-kernel_1.snap/kernel.efi"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/ubuntu/kernel.efi"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/device/model"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-data/var/lib/snapd/modeenv"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-data/var/lib/snapd/snaps/core22_1.snap"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-data/var/lib/snapd/snaps/pc_1.snap"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-data/var/lib/snapd/snaps/pc-kernel_1.snap"),
	}
	for _, f := range expectedFiles {
		c.Check(f, testutil.FilePresent)
	}
}

func (s *deviceMgrInstallAPISuite) TestInstallFinishNoEncryptionHappy(c *C) {
	s.testInstallFinishStep(c, finishStepOpts{encrypted: false, isClassic: true})
}

func (s *deviceMgrInstallAPISuite) TestInstallFinishNoLabel(c *C) {
	// Mock partitioned disk, but there will be no label in the system
	gadgetYaml := gadgettest.SingleVolumeClassicWithModesGadgetYaml
	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	ginfo, _, _, restore, err := gadgettest.MockGadgetPartitionedDisk(gadgetYaml, gadgetRoot)
	c.Assert(err, IsNil)
	s.AddCleanup(restore)

	s.state.Lock()
	defer s.state.Unlock()

	// Create change
	label := "classic"
	chg := s.state.NewChange("install-step-finish", "finish setup of run system")
	finishTask := s.state.NewTask("install-finish", "install API finish step")
	finishTask.Set("system-label", label)
	finishTask.Set("on-volumes", ginfo.Volumes)

	chg.AddTask(finishTask)

	// now let the change run - some checks will happen in the mocked functions
	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Checks now
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:
- install API finish step \(cannot load assertions for label "classic": no seed assertions\)`)
}

func (s *deviceMgrInstallAPISuite) testInstallSetupStorageEncryption(c *C, hasTPM bool) {
	// Mock label
	label := "classic"
	isClassic := true
	gadgetSnapPath, kernelSnapPath, ginfo, mountCmd := s.mockSystemSeedWithLabel(c, label, isClassic)

	// Simulate system with TPM
	if hasTPM {
		restore := devicestate.MockSecbootCheckTPMKeySealingSupported(func() error { return nil })
		s.AddCleanup(restore)
	}

	// Mock encryption of partitions
	encrytpPartCalls := 0
	restore := devicestate.MockInstallEncryptPartitions(func(onVolumes map[string]*gadget.Volume, encryptionType secboot.EncryptionType, model *asserts.Model, gadgetRoot, kernelRoot string, perfTimings timings.Measurer) (*install.EncryptionSetupData, error) {
		encrytpPartCalls++
		c.Check(encryptionType, Equals, secboot.EncryptionTypeLUKS)
		saveFound := false
		dataFound := false
		for _, strct := range onVolumes["pc"].Structure {
			switch strct.Role {
			case "system-save":
				saveFound = true
			case "system-data":
				dataFound = true
			}
		}
		c.Check(saveFound, Equals, true)
		c.Check(dataFound, Equals, true)
		return &install.EncryptionSetupData{}, nil
	})
	s.AddCleanup(restore)

	s.state.Lock()
	defer s.state.Unlock()

	// Create change
	chg := s.state.NewChange("install-step-setup-storage-encryption",
		"Setup storage encryption")
	encryptTask := s.state.NewTask("install-setup-storage-encryption",
		"install API set-up encryption step")
	encryptTask.Set("system-label", label)
	encryptTask.Set("on-volumes", ginfo.Volumes)
	chg.AddTask(encryptTask)

	// now let the change run - some checks will happen in the mocked functions
	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	// Make sure that if anything was stored in cache it is removed after test is run
	s.AddCleanup(func() {
		s.state.Lock()
		defer s.state.Unlock()
		devicestate.CleanUpEncryptionSetupDataInCache(s.state, label)
	})

	s.state.Lock()
	defer s.state.Unlock()

	// Checks now
	if !hasTPM {
		c.Check(chg.Err(), ErrorMatches, `.*
.*encryption unavailable on this device: not encrypting device storage as checking TPM gave: .*`)
		return
	}

	c.Check(chg.Err(), IsNil)
	gadgetDir := filepath.Join(dirs.SnapRunDir, "snap-content/gadget")
	kernelDir := filepath.Join(dirs.SnapRunDir, "snap-content/kernel")
	c.Check(mountCmd.Calls(), DeepEquals, [][]string{
		{"systemd-mount", gadgetSnapPath, gadgetDir},
		{"systemd-mount", kernelSnapPath, kernelDir},
		{"systemd-mount", "--umount", gadgetDir},
		{"systemd-mount", "--umount", kernelDir},
	})
	c.Check(encrytpPartCalls, Equals, 1)
	// Check that some data has been stored in the change
	apiData := make(map[string]interface{})
	c.Check(chg.Get("api-data", &apiData), IsNil)
	_, ok := apiData["encrypted-devices"]
	c.Check(ok, Equals, true)
	// Check that state has been stored in the cache
	c.Check(devicestate.CheckEncryptionSetupDataFromCache(s.state, label), IsNil)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionHappy(c *C) {
	s.testInstallSetupStorageEncryption(c, true)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionNoCrypto(c *C) {
	s.testInstallSetupStorageEncryption(c, false)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionNoLabel(c *C) {
	// Mock partitioned disk, but there will be no label in the system
	gadgetYaml := gadgettest.SingleVolumeClassicWithModesGadgetYaml
	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	ginfo, _, _, restore, err := gadgettest.MockGadgetPartitionedDisk(gadgetYaml, gadgetRoot)
	c.Assert(err, IsNil)
	s.AddCleanup(restore)

	s.state.Lock()
	defer s.state.Unlock()

	// Create change
	label := "classic"
	chg := s.state.NewChange("install-step-setup-storage-encryption",
		"Setup storage encryption")
	encryptTask := s.state.NewTask("install-setup-storage-encryption",
		"install API set-up encryption step")
	encryptTask.Set("system-label", label)
	encryptTask.Set("on-volumes", ginfo.Volumes)
	chg.AddTask(encryptTask)

	// now let the change run - some checks will happen in the mocked functions
	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Checks now
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:
- install API set-up encryption step \(cannot load assertions for label "classic": no seed assertions\)`)
}
