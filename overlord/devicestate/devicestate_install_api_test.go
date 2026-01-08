// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build !nosecboot

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
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/device"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/devicestate"
	installLogic "github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/systemd"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type deviceMgrInstallAPISuite struct {
	deviceMgrInstallSuite
}

var _ = Suite(&deviceMgrInstallAPISuite{})

func (s *deviceMgrInstallAPISuite) SetUpTest(c *C) {
	classic := true
	s.deviceMgrBaseSuite.setupBaseTest(c, classic)
	s.deviceMgrInstallSuite.SetUpTest(c)

	// We uncompress a gadget with grub, and prefer not to mock in this case
	bootloader.Force(nil)

	restore := devicestate.MockSystemForPreseeding(func() (string, error) {
		return "fake system label", nil
	})
	s.AddCleanup(restore)

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

type finishStepOpts struct {
	encrypted          bool
	installClassic     bool
	hasPartial         bool
	hasSystemSeed      bool
	hasKernelModsComps bool
	hasRecoveryKey     bool
	optionalContainers *seed.OptionalContainers
	volumesAuth        *device.VolumesAuthOptions
}

func mockDiskVolume(opts finishStepOpts) *gadget.OnDiskVolume {
	if opts.hasPartial {
		return &mockFilledPartialDiskVolume
	}

	labelPostfix := ""
	dataPartsFs := "ext4"
	if opts.encrypted {
		labelPostfix = "-enc"
		dataPartsFs = "crypto_LUKS"
	}

	if opts.hasSystemSeed {
		return mockClassicWithSystemSeedDiskVolume(dataPartsFs, labelPostfix)
	}

	return &gadget.OnDiskVolume{
		Structure: []gadget.OnDiskStructure{
			// Note that mbr is not a partition so it is not returned
			{
				Node:        "/dev/vda1",
				Name:        "BIOS Boot",
				Size:        1 * quantity.SizeMiB,
				StartOffset: 1 * quantity.OffsetMiB,
			},
			{
				Node:            "/dev/vda2",
				Name:            "EFI System partition",
				Size:            99 * quantity.SizeMiB,
				StartOffset:     2 * quantity.OffsetMiB,
				PartitionFSType: "vfat",
			},
			{
				Node:            "/dev/vda3",
				Name:            "ubuntu-boot",
				Size:            750 * quantity.SizeMiB,
				StartOffset:     1202 * quantity.OffsetMiB,
				PartitionFSType: "ext4",
			},
			{
				Node:             "/dev/vda4",
				Name:             "ubuntu-save",
				Size:             16 * quantity.SizeMiB,
				StartOffset:      1952 * quantity.OffsetMiB,
				PartitionFSType:  dataPartsFs,
				PartitionFSLabel: "ubuntu-save" + labelPostfix,
			},
			{
				Node:             "/dev/vda5",
				Name:             "ubuntu-data",
				Size:             4 * quantity.SizeGiB,
				StartOffset:      1968 * quantity.OffsetMiB,
				PartitionFSType:  dataPartsFs,
				PartitionFSLabel: "ubuntu-data" + labelPostfix,
			},
		},
		ID:         "anything",
		Device:     "/dev/vda",
		Schema:     "gpt",
		Size:       6 * quantity.SizeGiB,
		SectorSize: 512,

		// ( 2 GB / 512 B sector size ) - 33 typical GPT header backup sectors +
		// 1 sector to get the exclusive end
		UsableSectorsEnd: uint64((6*quantity.SizeGiB/512)-33) + 1,
	}
}

func mockClassicWithSystemSeedDiskVolume(dataPartsFs string, labelPostfix string) *gadget.OnDiskVolume {
	return &gadget.OnDiskVolume{
		Structure: []gadget.OnDiskStructure{
			// Note that mbr is not a partition so it is not returned
			{
				Node:        "/dev/vda1",
				Name:        "BIOS Boot",
				Size:        1 * quantity.SizeMiB,
				StartOffset: 1 * quantity.OffsetMiB,
			},
			{
				Node:            "/dev/vda2",
				Name:            "ubuntu-seed",
				Size:            1200 * quantity.SizeMiB,
				StartOffset:     2 * quantity.OffsetMiB,
				PartitionFSType: "vfat",
			},
			{
				Node:            "/dev/vda3",
				Name:            "ubuntu-boot",
				Size:            750 * quantity.SizeMiB,
				StartOffset:     1202 * quantity.OffsetMiB,
				PartitionFSType: "ext4",
			},
			{
				Node:             "/dev/vda4",
				Name:             "ubuntu-save",
				Size:             16 * quantity.SizeMiB,
				StartOffset:      1952 * quantity.OffsetMiB,
				PartitionFSType:  dataPartsFs,
				PartitionFSLabel: "ubuntu-save" + labelPostfix,
			},
			{
				Node:             "/dev/vda5",
				Name:             "ubuntu-data",
				Size:             4 * quantity.SizeGiB,
				StartOffset:      1968 * quantity.OffsetMiB,
				PartitionFSType:  dataPartsFs,
				PartitionFSLabel: "ubuntu-data" + labelPostfix,
			},
		},
		ID:         "anything",
		Device:     "/dev/vda",
		Schema:     "gpt",
		Size:       6 * quantity.SizeGiB,
		SectorSize: 512,

		// ( 2 GB / 512 B sector size ) - 33 typical GPT header backup sectors +
		// 1 sector to get the exclusive end
		UsableSectorsEnd: uint64((6*quantity.SizeGiB/512)-33) + 1,
	}

}

func mockCoreDiskVolume(opts finishStepOpts) *gadget.OnDiskVolume {
	labelPostfix := ""
	dataPartsFs := "ext4"
	if opts.encrypted {
		labelPostfix = "-enc"
		dataPartsFs = "crypto_LUKS"
	}
	var diskVolume = gadget.OnDiskVolume{
		Structure: []gadget.OnDiskStructure{
			// Note that mbr is not a partition so it is not returned
			{
				Node:        "/dev/vda1",
				Name:        "BIOS Boot",
				Size:        1 * quantity.SizeMiB,
				StartOffset: 1 * quantity.OffsetMiB,
			},
			{
				Node:            "/dev/vda2",
				Name:            "ubuntu-seed",
				Size:            1200 * quantity.SizeMiB,
				StartOffset:     2 * quantity.OffsetMiB,
				PartitionFSType: "vfat",
			},
			{
				Node:            "/dev/vda3",
				Name:            "ubuntu-boot",
				Size:            750 * quantity.SizeMiB,
				StartOffset:     1202 * quantity.OffsetMiB,
				PartitionFSType: "ext4",
			},
			{
				Node:             "/dev/vda4",
				Name:             "ubuntu-save",
				Size:             16 * quantity.SizeMiB,
				StartOffset:      1952 * quantity.OffsetMiB,
				PartitionFSType:  dataPartsFs,
				PartitionFSLabel: "ubuntu-save" + labelPostfix,
			},
			{
				Node:             "/dev/vda5",
				Name:             "ubuntu-data",
				Size:             1 * quantity.SizeGiB,
				StartOffset:      1968 * quantity.OffsetMiB,
				PartitionFSType:  dataPartsFs,
				PartitionFSLabel: "ubuntu-data" + labelPostfix,
			},
		},
		ID:         "anything",
		Device:     "/dev/vda",
		Schema:     "gpt",
		Size:       6 * quantity.SizeGiB,
		SectorSize: 512,

		// ( 2 GB / 512 B sector size ) - 33 typical GPT header backup sectors +
		// 1 sector to get the exclusive end
		UsableSectorsEnd: uint64((6*quantity.SizeGiB/512)-33) + 1,
	}
	return &diskVolume
}

var mockFilledPartialDiskVolume = gadget.OnDiskVolume{
	Structure: []gadget.OnDiskStructure{
		// Note that mbr is not a partition so it is not returned
		{
			Node:            "/dev/vda1",
			Name:            "ubuntu-seed",
			Size:            1200 * quantity.SizeMiB,
			StartOffset:     2 * quantity.OffsetMiB,
			PartitionFSType: "vfat",
		},
		{
			Node:            "/dev/vda2",
			Name:            "ubuntu-boot",
			Size:            750 * quantity.SizeMiB,
			StartOffset:     1202 * quantity.OffsetMiB,
			PartitionFSType: "ext4",
		},
		{
			Node:             "/dev/vda3",
			Name:             "ubuntu-save",
			Size:             16 * quantity.SizeMiB,
			StartOffset:      1952 * quantity.OffsetMiB,
			PartitionFSType:  "crypto_LUKS",
			PartitionFSLabel: "ubuntu-save-enc",
		},
		{
			Node:             "/dev/vda4",
			Name:             "ubuntu-data",
			Size:             4 * quantity.SizeGiB,
			StartOffset:      1968 * quantity.OffsetMiB,
			PartitionFSType:  "crypto_LUKS",
			PartitionFSLabel: "ubuntu-data-enc",
		},
	},
	ID:         "anything",
	Device:     "/dev/vda",
	Schema:     "gpt",
	Size:       6 * quantity.SizeGiB,
	SectorSize: 512,

	// ( 2 GB / 512 B sector size ) - 33 typical GPT header backup sectors +
	// 1 sector to get the exclusive end
	UsableSectorsEnd: uint64((6*quantity.SizeGiB/512)-33) + 1,
}

// TODO encryption case for the finish step is not tested yet, it needs more mocking
func (s *deviceMgrInstallAPISuite) testInstallFinishStep(c *C, opts finishStepOpts) {
	if opts.hasSystemSeed && !opts.installClassic {
		c.Fatal("explicitly setting hasSystemSeed is only supported with installClassic")
	}

	if opts.hasRecoveryKey && !opts.encrypted {
		c.Fatal("explicitly setting hasRecoveryKey is only supported with encrypted")
	}

	// The installer API is used on classic images only for the moment
	restore := release.MockOnClassic(true)
	s.AddCleanup(restore)

	// only amd64/arm64 have trusted boot assets
	oldArch := arch.DpkgArchitecture()
	defer arch.SetArchitecture(arch.ArchitectureType(oldArch))
	arch.SetArchitecture("amd64")

	// Mock label
	label := "core"
	if opts.installClassic {
		label = "classic"
	}

	recoveryKeyID := ""
	if opts.hasRecoveryKey {
		recoveryKeyID = "7"
		restore = devicestate.MockFdestateGetRecoveryKey(func(st *state.State, keyID string) (rkey keys.RecoveryKey, err error) {
			c.Check(keyID, Equals, "7")
			return keys.RecoveryKey{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y', '-', '7'}, nil
		})
		s.AddCleanup(restore)
	}

	seedCopyFn := func(seedDir string, opts seed.CopyOptions, tm timings.Measurer) error {
		return fmt.Errorf("unexpected copy call")
	}
	seedCopyCalled := false
	if !opts.installClassic || opts.hasSystemSeed {
		seedCopyFn = func(seedDir string, copyOpts seed.CopyOptions, tm timings.Measurer) error {
			c.Check(seedDir, Equals, filepath.Join(dirs.RunDir, "mnt/ubuntu-seed"))
			c.Check(copyOpts.Label, Equals, label)
			c.Check(copyOpts.OptionalContainers, DeepEquals, opts.optionalContainers)
			seedCopyCalled = true
			return nil
		}
	}

	var kModsRevs map[string]snap.Revision
	if opts.hasKernelModsComps {
		kModsRevs = map[string]snap.Revision{"kcomp1": snap.R(77), "kcomp2": snap.R(77), "kcomp3": snap.R(77)}
	}
	seedOpts := mockSystemSeedWithLabelOpts{
		isClassic:     opts.installClassic,
		hasSystemSeed: opts.hasSystemSeed,
		hasPartial:    opts.hasPartial,
		kModsRevs:     kModsRevs,
		types:         []snap.Type{snap.TypeKernel, snap.TypeBase, snap.TypeGadget},
	}
	gadgetSnapPath, kernelSnapPath, kCompsPaths, ginfo, mountCmd, _ := s.mockSystemSeedWithLabel(
		c, label, seedCopyFn, seedOpts)

	// Unpack gadget snap from seed where it would have been mounted
	gadgetDir := filepath.Join(dirs.SnapRunDir, "snap-content/gadget")
	err := os.MkdirAll(gadgetDir, 0755)
	c.Assert(err, IsNil)
	err = unpackSnap(filepath.Join(s.SeedDir, "snaps/pc_1.snap"), gadgetDir)
	c.Assert(err, IsNil)

	kernelMountDir := filepath.Join(dirs.SnapRunDir, "snap-content/kernel")
	kcomp1MountDir := filepath.Join(dirs.SnapRunDir, "snap-content/pc-kernel+kcomp1")
	kcomp2MountDir := filepath.Join(dirs.SnapRunDir, "snap-content/pc-kernel+kcomp2")

	// Mock writing of contents
	writeContentCalls := 0
	restore = devicestate.MockInstallWriteContent(func(onVolumes map[string]*gadget.Volume, allLaidOutVols map[string]*gadget.LaidOutVolume, encSetupData *install.EncryptionSetupData, kSnapInfo *install.KernelSnapInfo, observer gadget.ContentObserver, perfTimings timings.Measurer) ([]*gadget.OnDiskVolume, error) {
		writeContentCalls++
		vol := onVolumes["pc"]
		for sIdx, vs := range vol.Structure {
			c.Check(vs.Device, Equals, fmt.Sprintf("/dev/vda%d", sIdx+1))
		}
		if opts.encrypted {
			c.Check(encSetupData, NotNil)

			writeChange := &gadget.ContentChange{
				// file that contains the data of the installed file
				After: filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/boot/grubx64.efi"),
				// there is no original file in place
				Before: "",
			}
			// We "observe" grub from boot partition
			action, err := observer.Observe(gadget.ContentWrite, gadget.SystemBoot,
				filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/"),
				"EFI/boot/grubx64.efi", writeChange)
			c.Check(err, IsNil)
			c.Check(action, Equals, gadget.ChangeApply)
		} else {
			c.Check(encSetupData, IsNil)
		}
		modulesComps := []install.KernelModulesComponentInfo{}
		if opts.hasKernelModsComps {
			modulesComps = []install.KernelModulesComponentInfo{
				{
					Name:       "kcomp1",
					Revision:   snap.R(77),
					MountPoint: kcomp1MountDir,
				},
				{
					Name:       "kcomp2",
					Revision:   snap.R(77),
					MountPoint: kcomp2MountDir,
				},
			}
		}
		c.Check(kSnapInfo, DeepEquals, &install.KernelSnapInfo{
			Name:             "pc-kernel",
			Revision:         snap.R(1),
			MountPoint:       kernelMountDir,
			IsCore:           !opts.installClassic,
			ModulesComps:     modulesComps,
			NeedsDriversTree: true,
		})
		return nil, nil
	})
	s.AddCleanup(restore)

	seedDir := filepath.Join(dirs.RunDir, "mnt/ubuntu-seed")

	// Mock mounting of partitions
	mountVolsCalls := 0
	restore = devicestate.MockInstallMountVolumes(func(onVolumes map[string]*gadget.Volume, encSetupData *install.EncryptionSetupData) (seedMntDir string, unmount func() error, err error) {
		mountVolsCalls++
		return seedDir, func() error { return nil }, nil
	})
	s.AddCleanup(restore)

	// Mock saving of traits
	saveStorageTraitsCalls := 0
	restore = devicestate.MockInstallSaveStorageTraits(func(model gadget.Model, allVols map[string]*gadget.Volume, encryptSetupData *install.EncryptionSetupData) error {
		saveStorageTraitsCalls++
		// This is a good point to check if things have been filled
		if opts.hasPartial {
			for _, v := range allVols {
				c.Check(v.Partial, DeepEquals, []gadget.PartialProperty{gadget.PartialStructure})
				c.Check(v.Schema != "", Equals, true)
				for _, vs := range v.Structure {
					c.Check(vs.Filesystem != "", Equals, true)
					c.Check(vs.Size != 0, Equals, true)
				}
			}
		}
		return nil
	})
	s.AddCleanup(restore)

	restore = devicestate.MockMatchDisksToGadgetVolumes(func(gVols map[string]*gadget.Volume, volCompatOpts *gadget.VolumeCompatibilityOptions) (map[string]map[int]*gadget.OnDiskStructure, error) {
		volToGadgetToDiskStruct := map[string]map[int]*gadget.OnDiskStructure{}
		for name, vol := range gVols {
			var diskVolume *gadget.OnDiskVolume
			if opts.installClassic {
				diskVolume = mockDiskVolume(opts)
			} else {
				diskVolume = mockCoreDiskVolume(opts)
			}
			gadgetToDiskMap, err := gadget.EnsureVolumeCompatibility(
				vol, diskVolume, volCompatOpts)
			if err != nil {
				return nil, err
			}
			volToGadgetToDiskStruct[name] = gadgetToDiskMap
		}

		return volToGadgetToDiskStruct, nil
	})
	s.AddCleanup(restore)

	var checkContext *secboot.PreinstallCheckContext
	var checkResult *secboot.PreinstallCheckResult

	// Insert encryption data when enabled
	if opts.encrypted {
		// Mock sealing, not required to mock encryption check because install finish step uses encryption information from cache
		restore = boot.MockSealKeyToModeenv(func(
			key, saveKey secboot.BootstrappedContainer,
			primaryKey []byte,
			volumesAuth *device.VolumesAuthOptions,
			checkResult *secboot.PreinstallCheckResult,
			model *asserts.Model,
			modeenv *boot.Modeenv,
			flags boot.MockSealKeyToModeenvFlags,
		) error {
			c.Check(model.Classic(), Equals, opts.installClassic)
			// Note that we cannot compare the full structure and we check
			// separately bits as the types for these are not exported.
			c.Check(len(modeenv.CurrentTrustedBootAssets), Equals, 1)
			c.Check(modeenv.CurrentTrustedBootAssets["grubx64.efi"], DeepEquals,
				[]string{"0c63a75b845e4f7d01107d852e4c2485c51a50aaaa94fc61995e71bbee983a2ac3713831264adb47fb6bd1e058d5f004"})
			c.Check(len(modeenv.CurrentTrustedRecoveryBootAssets), Equals, 2)
			c.Check(modeenv.CurrentTrustedRecoveryBootAssets["bootx64.efi"], DeepEquals, []string{"0c63a75b845e4f7d01107d852e4c2485c51a50aaaa94fc61995e71bbee983a2ac3713831264adb47fb6bd1e058d5f004"})
			c.Check(modeenv.CurrentTrustedRecoveryBootAssets["grubx64.efi"], DeepEquals, []string{"0c63a75b845e4f7d01107d852e4c2485c51a50aaaa94fc61995e71bbee983a2ac3713831264adb47fb6bd1e058d5f004"})
			c.Check(len(modeenv.CurrentKernelCommandLines), Equals, 1)
			// exact cmdline depends on arch, see
			// bootloader/assets/grub.go:init()
			c.Check(modeenv.CurrentKernelCommandLines[0], testutil.Contains, "snapd_recovery_mode=run")
			// Check that volume authentication options where propagated
			c.Check(volumesAuth, Equals, opts.volumesAuth)

			if opts.hasSystemSeed && opts.installClassic {
				c.Check(checkResult, Equals, preinstallCheckResult)
			}
			return nil
		})
		s.AddCleanup(restore)

		// Insert encryption set-up data in state cache
		if opts.hasSystemSeed && opts.installClassic {
			// hybrid classic
			checkContext = preinstallCheckContext
			restore = installLogic.MockSecbootSaveCheckResult(func(pcc *secboot.PreinstallCheckContext, filename string) error {
				var err error
				if pcc != preinstallCheckContext {
					return fmt.Errorf("test error: MockSecbootSaveCheckResult received unexpected check context")
				}
				if !strings.HasSuffix(filename, "run/mnt/ubuntu-save/device/fde/preinstall") {
					return fmt.Errorf("test error: MockSecbootSaveCheckResult received unexpected filename %s", filename)
				}
				dir := filepath.Dir(filename)
				if err = os.MkdirAll(dir, 0755); err != nil {
					return fmt.Errorf("test error: MockSecbootSaveCheckResult failed to create dir %s", dir)
				}
				if err = osutil.AtomicWriteFile(filename, []byte{}, 0600, 0); err != nil {
					return fmt.Errorf("test error: MockSecbootSaveCheckResult failed to create file %s", filename)
				}
				return nil
			})
			s.AddCleanup(restore)

			checkResult = preinstallCheckResult
			restore = installLogic.MockSecbootCheckResult(func(pcc *secboot.PreinstallCheckContext) (*secboot.PreinstallCheckResult, error) {
				if pcc != preinstallCheckContext {
					return nil, fmt.Errorf("test error: MockSecbootCheckResult received unexpected check context")
				}

				return checkResult, nil
			})
			s.AddCleanup(restore)
		}
		restore = devicestate.MockEncryptionSetupDataInCache(s.state, label, recoveryKeyID, opts.volumesAuth, checkContext)
		s.AddCleanup(restore)

		// Write expected boot assets needed when creating bootchain
		seedBootDir := filepath.Join(dirs.RunDir, "mnt/ubuntu-seed/EFI/boot/")
		c.Assert(os.MkdirAll(seedBootDir, 0755), IsNil)

		for _, p := range []string{
			filepath.Join(seedBootDir, "bootx64.efi"),
			filepath.Join(seedBootDir, "grubx64.efi"),
		} {
			c.Assert(os.WriteFile(p, []byte{}, 0755), IsNil)
		}

		bootDir := filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/boot/")
		c.Assert(os.MkdirAll(bootDir, 0755), IsNil)
		c.Assert(os.WriteFile(filepath.Join(bootDir, "grubx64.efi"), []byte{}, 0755), IsNil)

		s.AddCleanup(secboot.MockCreateBootstrappedContainer(func(key secboot.DiskUnlockKey, devicePath string) secboot.BootstrappedContainer {
			return secboot.CreateMockBootstrappedContainer()
		}))
	}

	if opts.hasSystemSeed {
		devicestate.MockBootMakeRecoverySystemBootable(func(model *asserts.Model, rootdir string, relativeRecoverySystemDir string, bootWith *boot.RecoverySystemBootableSet) error {
			c.Check(model.Classic(), Equals, true)
			c.Check(rootdir, Equals, filepath.Join(dirs.RunDir, "mnt/ubuntu-seed"))
			c.Check(relativeRecoverySystemDir, Equals, filepath.Join("systems", label))
			c.Check(bootWith.KernelPath, Equals, filepath.Join(dirs.RunDir, "mnt/ubuntu-seed/snaps/pc-kernel_1.snap"))
			c.Check(bootWith.GadgetSnapOrDir, Equals, filepath.Join(s.SeedDir, "snaps/pc_1.snap"))
			return nil
		})
	}

	s.state.Lock()
	defer s.state.Unlock()

	// Create change
	chg := s.state.NewChange("install-step-finish", "finish setup of run system")
	finishTask := s.state.NewTask("install-finish", "install API finish step")
	finishTask.Set("system-label", label)
	// Set devices as an installer would
	for _, vol := range ginfo.Volumes {
		for sIdx := range vol.Structure {
			vol.Structure[sIdx].Device = fmt.Sprintf("/dev/vda%d", sIdx+1)
		}
	}
	finishTask.Set("on-volumes", ginfo.Volumes)
	if opts.optionalContainers != nil {
		finishTask.Set("optional-install", *opts.optionalContainers)
	}

	chg.AddTask(finishTask)

	// now let the change run - some checks will happen in the mocked functions
	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()
	c.Assert(chg.Err(), IsNil)

	// Checks now
	mountCalls := [][]string{
		{"systemd-mount", kernelSnapPath, kernelMountDir},
		{"systemd-mount", gadgetSnapPath, gadgetDir}}
	if opts.hasKernelModsComps {
		mountCalls = append(mountCalls,
			[]string{"systemd-mount", kCompsPaths[0], kcomp1MountDir},
			[]string{"systemd-mount", kCompsPaths[1], kcomp2MountDir})
	}
	mountCalls = append(mountCalls,
		[]string{"systemd-mount", "--umount", kernelMountDir},
		[]string{"systemd-mount", "--umount", gadgetDir})
	if opts.hasKernelModsComps {
		mountCalls = append(mountCalls,
			[]string{"systemd-mount", "--umount", kcomp1MountDir},
			[]string{"systemd-mount", "--umount", kcomp2MountDir})
	}
	c.Check(mountCmd.Calls(), DeepEquals, mountCalls)
	c.Check(writeContentCalls, Equals, 1)
	c.Check(mountVolsCalls, Equals, 1)
	c.Check(saveStorageTraitsCalls, Equals, 1)

	if !opts.installClassic || opts.hasSystemSeed {
		c.Check(seedCopyCalled, Equals, true)
	}

	// on hybrid systems (classic systems that'll have a seed), we expect a bind
	// mount from the seed that is mounted /run/mnt/ubuntu-seed from the
	// initramfs
	unitFile := systemd.EscapeUnitNamePath(dirs.SnapSeedDir) + ".mount"
	unitPath := filepath.Join(
		boot.InstallUbuntuDataDir,
		"etc/systemd/system",
		unitFile,
	)
	if opts.installClassic && opts.hasSystemSeed {
		unitContents, err := os.ReadFile(unitPath)
		c.Assert(err, IsNil)

		contents := string(unitContents)
		c.Check(strings.Contains(contents, fmt.Sprintf("Where=%s", dirs.SnapSeedDir)), Equals, true)
		c.Check(strings.Contains(contents, fmt.Sprintf("What=%s", boot.InitramfsUbuntuSeedDir)), Equals, true)
		c.Check(strings.Contains(contents, "Options=bind"), Equals, true)
		c.Check(strings.Contains(contents, "Type=none"), Equals, true)
		c.Check(strings.Contains(contents, "Before=snapd.mounts.target"), Equals, true)
		c.Check(strings.Contains(contents, "WantedBy=snapd.mounts.target"), Equals, true)

		unitSymlinkPath := filepath.Join(boot.InstallUbuntuDataDir, "etc/systemd/system/snapd.mounts.target.wants", unitFile)
		info, err := os.Lstat(unitSymlinkPath)
		c.Assert(err, IsNil)

		c.Check(info.Mode()&os.ModeSymlink != 0, Equals, true)
		linkTarget, err := os.Readlink(unitSymlinkPath)
		c.Assert(err, IsNil)

		c.Check(linkTarget, Equals, filepath.Join(dirs.GlobalRootDir, "etc/systemd/system", unitFile))
	} else {
		c.Check(unitPath, testutil.FileAbsent)
	}

	snapdVarDir := "mnt/ubuntu-data/system-data/var/lib/snapd"
	if opts.installClassic {
		snapdVarDir = "mnt/ubuntu-data/var/lib/snapd"
	}
	expectedFiles := []string{
		filepath.Join(seedDir, "EFI/ubuntu/grub.cfg"),
		filepath.Join(seedDir, "EFI/ubuntu/grubenv"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/ubuntu/grub.cfg"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/ubuntu/grubenv"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/ubuntu/pc-kernel_1.snap/kernel.efi"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/EFI/ubuntu/kernel.efi"),
		filepath.Join(dirs.RunDir, "mnt/ubuntu-boot/device/model"),
		filepath.Join(dirs.RunDir, snapdVarDir, "modeenv"),
		filepath.Join(dirs.RunDir, snapdVarDir, "snaps/core24_1.snap"),
		filepath.Join(dirs.RunDir, snapdVarDir, "snaps/pc_1.snap"),
		filepath.Join(dirs.RunDir, snapdVarDir, "snaps/pc-kernel_1.snap"),
	}
	if opts.encrypted {
		expectedFiles = append(expectedFiles, dirs.RunDir,
			filepath.Join(dirs.RunDir, snapdVarDir, "device/fde/marker"),
			filepath.Join(dirs.RunDir, snapdVarDir, "device/fde/ubuntu-save.key"),
			filepath.Join(dirs.RunDir, "mnt/ubuntu-save/device/fde/marker"))

		if opts.hasSystemSeed && opts.installClassic {
			// hybrid classic
			expectedFiles = append(expectedFiles, filepath.Join(dirs.RunDir, "mnt/ubuntu-save/device/fde/preinstall"))
		}
	}
	if opts.hasKernelModsComps {
		expectedFiles = append(expectedFiles,
			filepath.Join(dirs.RunDir, snapdVarDir, "snaps/pc-kernel+kcomp1_77.comp"),
			filepath.Join(dirs.RunDir, snapdVarDir, "snaps/pc-kernel+kcomp2_77.comp"),
		)
	}
	for _, f := range expectedFiles {
		c.Check(f, testutil.FilePresent)
	}

	if opts.hasRecoveryKey {
		encSetupData := devicestate.GetEncryptionSetupDataFromCache(s.state, label)
		bootstrappedContainersForRole := install.BootstrappedContainersForRole(encSetupData)
		c.Assert(bootstrappedContainersForRole, HasLen, 2)

		dataBootstrappedContainer := bootstrappedContainersForRole[gadget.SystemData].(*secboot.MockBootstrappedContainer)
		c.Check(dataBootstrappedContainer.Slots["default-recovery"], DeepEquals, []byte{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y', '-', '7', 0, 0, 0, 0, 0, 0})

		saveBootstrappedContainer := bootstrappedContainersForRole[gadget.SystemSave].(*secboot.MockBootstrappedContainer)
		c.Check(saveBootstrappedContainer.Slots["default-recovery"], DeepEquals, []byte{'r', 'e', 'c', 'o', 'v', 'e', 'r', 'y', '-', '7', 0, 0, 0, 0, 0, 0})
	}
}

func (s *deviceMgrInstallAPISuite) TestInstallClassicFinishNoEncryptionHappy(c *C) {
	s.testInstallFinishStep(c, finishStepOpts{encrypted: false, installClassic: true})
}

func (s *deviceMgrInstallAPISuite) TestInstallClassicFinishEncryptionHappy(c *C) {
	s.testInstallFinishStep(c, finishStepOpts{encrypted: true, installClassic: true})
}

func (s *deviceMgrInstallAPISuite) TestInstallClassicFinishNoEncryptionWithKModsHappy(c *C) {
	s.testInstallFinishStep(c, finishStepOpts{
		encrypted: false, installClassic: true, hasKernelModsComps: true})
}

func (s *deviceMgrInstallAPISuite) TestInstallClassicFinishEncryptionWithPassphraseAuthHappy(c *C) {
	volumesAuth := &device.VolumesAuthOptions{Mode: device.AuthModePassphrase, Passphrase: "test"}
	s.testInstallFinishStep(c, finishStepOpts{encrypted: true, installClassic: true, volumesAuth: volumesAuth})
}

func (s *deviceMgrInstallAPISuite) TestInstallClassicFinishEncryptionWithRecoveryKeyHappy(c *C) {
	s.testInstallFinishStep(c, finishStepOpts{encrypted: true, installClassic: true, hasRecoveryKey: true})
}

func (s *deviceMgrInstallAPISuite) TestInstallClassicFinishEncryptionAndSystemSeedHappy(c *C) {
	s.testInstallFinishStep(c, finishStepOpts{encrypted: true, installClassic: true, hasSystemSeed: true})
}

func (s *deviceMgrInstallAPISuite) TestInstallClassicFinishEncryptionPartialHappy(c *C) {
	s.testInstallFinishStep(c, finishStepOpts{encrypted: true, installClassic: true, hasPartial: true})
}

func (s *deviceMgrInstallAPISuite) TestInstallCoreFinishNoEncryptionHappy(c *C) {
	s.testInstallFinishStep(c, finishStepOpts{encrypted: false, installClassic: false})
}

func (s *deviceMgrInstallAPISuite) TestInstallCoreFinishEncryptionHappy(c *C) {
	s.testInstallFinishStep(c, finishStepOpts{encrypted: true, installClassic: false})
}

func (s *deviceMgrInstallAPISuite) TestInstallCoreFinishEncryptionWithPassphraseAuthHappy(c *C) {
	volumesAuth := &device.VolumesAuthOptions{Mode: device.AuthModePassphrase, Passphrase: "test"}
	s.testInstallFinishStep(c, finishStepOpts{encrypted: true, installClassic: false, volumesAuth: volumesAuth})
}

func (s *deviceMgrInstallAPISuite) TestInstallCoreFinishEncryptionWithRecoveryKeyHappy(c *C) {
	s.testInstallFinishStep(c, finishStepOpts{encrypted: true, installClassic: false, hasRecoveryKey: true})
}

func (s *deviceMgrInstallAPISuite) TestInstallCoreFinishWithOptionalContainers(c *C) {
	s.testInstallFinishStep(c, finishStepOpts{
		encrypted:      true,
		installClassic: false,
		optionalContainers: &seed.OptionalContainers{
			Snaps:      []string{"optional24"},
			Components: map[string][]string{"optional24": {"comp1"}},
		},
	})
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

func (s *deviceMgrInstallAPISuite) testInstallSetupStorageEncryption(c *C, isSupportedHybrid, hasTPM bool, mockVolumesAuth *device.VolumesAuthOptions) {
	// Mock label
	label := "classic"
	isClassic := true
	seedCopyFn := func(seedDir string, opts seed.CopyOptions, tm timings.Measurer) error {
		return fmt.Errorf("unexpected copy call")
	}
	var snapdVersionByType map[snap.Type]string
	if mockVolumesAuth != nil {
		// Passphrase auth requires snapd 2.68 as a minimum in target install system
		snapdVersionByType = map[snap.Type]string{
			snap.TypeSnapd:  "2.68",
			snap.TypeKernel: "2.68",
		}
	} else {
		// mock other versions to cover more cases
		snapdVersionByType = map[snap.Type]string{
			snap.TypeSnapd:  "2.67",
			snap.TypeKernel: "2.66",
		}
	}
	seedOpts := mockSystemSeedWithLabelOpts{
		isClassic:          isClassic,
		hasSystemSeed:      false,
		hasPartial:         false,
		types:              []snap.Type{snap.TypeSnapd, snap.TypeKernel, snap.TypeBase, snap.TypeGadget},
		snapdVersionByType: snapdVersionByType,
	}
	gadgetSnapPath, kernelSnapPath, _, ginfo, mountCmd, _ := s.mockSystemSeedWithLabel(
		c, label, seedCopyFn, seedOpts)

	callCnt := mockHelperForEncryptionAvailabilityCheck(s, c, isSupportedHybrid, hasTPM)

	// Mock encryption of partitions
	encrytpPartCalls := 0
	restore := devicestate.MockInstallEncryptPartitions(func(
		onVolumes map[string]*gadget.Volume,
		volumesAuth *device.VolumesAuthOptions,
		encryptionType device.EncryptionType,
		checkContext *secboot.PreinstallCheckContext,
		model *asserts.Model,
		gadgetRoot,
		kernelRoot string,
		perfTimings timings.Measurer,
	) (*install.EncryptionSetupData, error) {
		encrytpPartCalls++
		c.Check(encryptionType, Equals, device.EncryptionTypeLUKS)
		if isSupportedHybrid {
			c.Check(checkContext, DeepEquals, preinstallCheckContext)
		} else {
			c.Check(checkContext, IsNil)
		}
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
		c.Check(volumesAuth, Equals, mockVolumesAuth)
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
	if mockVolumesAuth != nil {
		encryptTask.Set("volumes-auth-required", true)
		s.state.Cache(devicestate.VolumesAuthOptionsKeyByLabel(label), mockVolumesAuth)
	}
	chg.AddTask(encryptTask)

	// now let the change run - some checks will happen in the mocked functions
	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	// Make sure that if anything was stored in cache it is removed after test is run
	s.AddCleanup(func() {
		devicestate.CleanUpEncryptionSetupDataInCache(s.state, label)
	})

	s.state.Lock()
	defer s.state.Unlock()

	// ensure the expected encryption availability check was used
	if isSupportedHybrid {
		c.Assert(callCnt, DeepEquals, &callCounter{checkCnt: 1, checkActionCnt: 0, sealingSupportedCnt: 0})
	} else {
		c.Assert(callCnt, DeepEquals, &callCounter{checkCnt: 0, checkActionCnt: 0, sealingSupportedCnt: 1})
	}

	// Checks now
	if !hasTPM {
		c.Check(chg.Err(), ErrorMatches, `.*
.*encryption unavailable on this device: not encrypting device storage as checking TPM gave: .*`)
		return
	}

	if mockVolumesAuth != nil {
		// TODO:FDEM: PIN and passphrase support is temporarily disabled
		// during install even with supported snapd versions.
		expectedErr := fmt.Sprintf("cannot perform the following tasks:\n.*%q authentication mode is not supported by target system.*", mockVolumesAuth.Mode)
		c.Assert(chg.Err(), ErrorMatches, expectedErr)
		return
	} else {
		c.Assert(chg.Err(), IsNil)
	}
	gadgetDir := filepath.Join(dirs.SnapRunDir, "snap-content/gadget")
	kernelDir := filepath.Join(dirs.SnapRunDir, "snap-content/kernel")
	c.Check(mountCmd.Calls(), DeepEquals, [][]string{
		{"systemd-mount", kernelSnapPath, kernelDir},
		{"systemd-mount", gadgetSnapPath, gadgetDir},
		{"systemd-mount", "--umount", kernelDir},
		{"systemd-mount", "--umount", gadgetDir},
	})
	c.Check(encrytpPartCalls, Equals, 1)
	// Check that some data has been stored in the change
	apiData := make(map[string]any)
	c.Check(chg.Get("api-data", &apiData), IsNil)
	_, ok := apiData["encrypted-devices"]
	c.Check(ok, Equals, true)
	// Check that state has been stored in the cache
	c.Check(devicestate.GetEncryptionSetupDataFromCache(s.state, label), NotNil)
	// Cached auth options are cleaned
	c.Check(s.state.Cached(devicestate.VolumesAuthOptionsKeyByLabel(label)), IsNil)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionSupportedHybridHappy(c *C) {
	// supported hybrid system uses specialized encryption availability check
	const hasTPM = true
	const isSupportedHybrid = true
	var volumesAuth *device.VolumesAuthOptions = nil
	s.testInstallSetupStorageEncryption(c, isSupportedHybrid, hasTPM, volumesAuth)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionNotSupportedHybridHappy(c *C) {
	// unsupported hybrid system uses general encryption availability check
	const hasTPM = true
	const isSupportedHybrid = false
	var volumesAuth *device.VolumesAuthOptions = nil
	s.testInstallSetupStorageEncryption(c, isSupportedHybrid, hasTPM, volumesAuth)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionSupportedHybridHappyWithPassphrase(c *C) {
	// supported hybrid system uses specialized encryption availability check
	const hasTPM = true
	const isSupportedHybrid = true
	volumesAuth := &device.VolumesAuthOptions{Mode: device.AuthModePassphrase, Passphrase: "test"}
	s.testInstallSetupStorageEncryption(c, isSupportedHybrid, hasTPM, volumesAuth)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionSupportedHybridHappyWithPIN(c *C) {
	// supported hybrid system uses specialized encryption availability check
	const hasTPM = true
	const isSupportedHybrid = true
	volumesAuth := &device.VolumesAuthOptions{Mode: device.AuthModePIN, PIN: "1234"}
	s.testInstallSetupStorageEncryption(c, isSupportedHybrid, hasTPM, volumesAuth)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionNotSupportedHybridHappyWithVolumesAuth(c *C) {
	// unsupported hybrid system uses general encryption availability check
	const hasTPM = true
	const isSupportedHybrid = false
	volumesAuth := &device.VolumesAuthOptions{Mode: device.AuthModePassphrase, Passphrase: "test"}
	s.testInstallSetupStorageEncryption(c, isSupportedHybrid, hasTPM, volumesAuth)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionSupportedHybridNoCrypto(c *C) {
	// supported hybrid system uses specialized encryption availability check
	const hasTPM = false
	const isSupportedHybrid = true
	var volumesAuth *device.VolumesAuthOptions = nil
	s.testInstallSetupStorageEncryption(c, isSupportedHybrid, hasTPM, volumesAuth)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionNotSupportedHybridNoCrypto(c *C) {
	// unsupported hybrid system uses general encryption availability check
	const hasTPM = false
	const isSupportedHybrid = false
	var volumesAuth *device.VolumesAuthOptions = nil
	s.testInstallSetupStorageEncryption(c, isSupportedHybrid, hasTPM, volumesAuth)
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

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionMissingVolumesAuthOptions(c *C) {
	// Mock label
	label := "classic"
	seedCopyFn := func(seedDir string, opts seed.CopyOptions, tm timings.Measurer) error {
		return fmt.Errorf("unexpected copy call")
	}
	seedOpts := mockSystemSeedWithLabelOpts{
		isClassic:     true,
		hasSystemSeed: false,
		hasPartial:    false,
	}
	_, _, _, ginfo, _, _ := s.mockSystemSeedWithLabel(c, label, seedCopyFn, seedOpts)

	s.state.Lock()
	defer s.state.Unlock()

	// Create change
	chg := s.state.NewChange("install-step-setup-storage-encryption",
		"Setup storage encryption")
	encryptTask := s.state.NewTask("install-setup-storage-encryption",
		"install API set-up encryption step")
	encryptTask.Set("system-label", label)
	encryptTask.Set("on-volumes", ginfo.Volumes)
	// Set volumes auth as required without corresponding cached options
	// mimicing unexpected restart of snapd.
	encryptTask.Set("volumes-auth-required", true)
	chg.AddTask(encryptTask)

	// now let the change run - some checks will happen in the mocked functions
	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Checks now
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:
- install API set-up encryption step \(volumes authentication is required but cannot find corresponding cached options\)`)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionBadVolumesAuthOptionsType(c *C) {
	// Mock label
	label := "classic"
	seedCopyFn := func(seedDir string, opts seed.CopyOptions, tm timings.Measurer) error {
		return fmt.Errorf("unexpected copy call")
	}
	seedOpts := mockSystemSeedWithLabelOpts{
		isClassic:     true,
		hasSystemSeed: false,
		hasPartial:    false,
	}
	_, _, _, ginfo, _, _ := s.mockSystemSeedWithLabel(c, label, seedCopyFn, seedOpts)

	s.state.Lock()
	defer s.state.Unlock()

	// Create change
	chg := s.state.NewChange("install-step-setup-storage-encryption",
		"Setup storage encryption")
	encryptTask := s.state.NewTask("install-setup-storage-encryption",
		"install API set-up encryption step")
	encryptTask.Set("system-label", label)
	encryptTask.Set("on-volumes", ginfo.Volumes)
	encryptTask.Set("volumes-auth-required", true)
	s.state.Cache(devicestate.VolumesAuthOptionsKeyByLabel(label), "bad-type")
	chg.AddTask(encryptTask)

	// now let the change run - some checks will happen in the mocked functions
	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// Checks now
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:
- install API set-up encryption step \(internal error: wrong data type under volumesAuthOptionsKey\)`)
	// Cached auth options are cleaned
	c.Check(s.state.Cached(devicestate.VolumesAuthOptionsKeyByLabel(label)), IsNil)
}

func (s *deviceMgrInstallAPISuite) testInstallSetupStorageEncryptionPassphraseAuthUnsupportedSnap(c *C, snapdVersionByType map[snap.Type]string) {
	// Mock label
	label := "classic"
	seedCopyFn := func(seedDir string, opts seed.CopyOptions, tm timings.Measurer) error {
		return fmt.Errorf("unexpected copy call")
	}
	seedOpts := mockSystemSeedWithLabelOpts{
		isClassic:          true,
		hasSystemSeed:      false,
		hasPartial:         false,
		types:              []snap.Type{snap.TypeSnapd, snap.TypeKernel, snap.TypeBase, snap.TypeGadget},
		snapdVersionByType: snapdVersionByType,
	}

	_, _, _, ginfo, _, _ := s.mockSystemSeedWithLabel(c, label, seedCopyFn, seedOpts)

	s.state.Lock()
	defer s.state.Unlock()

	callCnt := mockHelperForEncryptionAvailabilityCheck(s, c, true, true)

	restore := devicestate.MockInstallEncryptPartitions(func(
		onVolumes map[string]*gadget.Volume,
		volumesAuth *device.VolumesAuthOptions,
		encryptionType device.EncryptionType,
		checkContext *secboot.PreinstallCheckContext,
		model *asserts.Model,
		gadgetRoot,
		kernelRoot string,
		perfTimings timings.Measurer,
	) (*install.EncryptionSetupData, error) {
		return &install.EncryptionSetupData{}, nil
	})
	s.AddCleanup(restore)

	// Create change
	chg := s.state.NewChange("install-step-setup-storage-encryption",
		"Setup storage encryption")
	encryptTask := s.state.NewTask("install-setup-storage-encryption",
		"install API set-up encryption step")
	encryptTask.Set("system-label", label)
	encryptTask.Set("on-volumes", ginfo.Volumes)
	encryptTask.Set("volumes-auth-required", true)
	mockVolumesAuth := &device.VolumesAuthOptions{Mode: device.AuthModePassphrase, Passphrase: "1234"}
	s.state.Cache(devicestate.VolumesAuthOptionsKeyByLabel(label), mockVolumesAuth)
	chg.AddTask(encryptTask)

	// now let the change run - some checks will happen in the mocked functions
	s.state.Unlock()
	defer s.state.Lock()

	s.settle(c)

	s.state.Lock()
	defer s.state.Unlock()

	// ensure the expected encryption availability check was used
	c.Assert(callCnt, DeepEquals, &callCounter{checkCnt: 1, checkActionCnt: 0, sealingSupportedCnt: 0})

	// Checks now
	c.Check(chg.Err(), ErrorMatches, `cannot perform the following tasks:
- install API set-up encryption step \(\"passphrase\" authentication mode is not supported by target system\)`)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionPassphraseAuthUnsupportedSnapd(c *C) {
	snapdVersionByType := map[snap.Type]string{
		snap.TypeSnapd:  "2.67",
		snap.TypeKernel: "2.68",
	}
	s.testInstallSetupStorageEncryptionPassphraseAuthUnsupportedSnap(c, snapdVersionByType)
}

func (s *deviceMgrInstallAPISuite) TestInstallSetupStorageEncryptionPassphraseAuthUnsupportedKernel(c *C) {
	snapdVersionByType := map[snap.Type]string{
		snap.TypeSnapd:  "2.68",
		snap.TypeKernel: "2.67",
	}
	s.testInstallSetupStorageEncryptionPassphraseAuthUnsupportedSnap(c, snapdVersionByType)
}

func (s *deviceMgrInstallAPISuite) TestInstallPreseedConflictWithOngoingChange(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	chg := s.state.NewChange("install-step-preseed", "Preseeding...")
	chg.AddTask(s.state.NewTask("install-preseed", "Preseed task"))

	_, err := devicestate.InstallPreseed(s.state, "20191119", c.MkDir())
	c.Assert(err, NotNil, Commentf("expected an error when preseeding with concurrent change"))

	var conflictErr *snapstate.ChangeConflictError
	c.Assert(errors.As(err, &conflictErr), Equals, true)

	c.Check(conflictErr.Message, Matches, "installation preseeding in progress, no other installation steps allowed until it is done")
	c.Check(conflictErr.ChangeKind, Equals, "install-step-preseed")
	c.Check(conflictErr.ChangeID, Equals, chg.ID())
}
