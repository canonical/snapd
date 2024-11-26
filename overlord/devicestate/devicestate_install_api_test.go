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
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/overlord/devicestate"
	installLogic "github.com/snapcore/snapd/overlord/install"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/seed/seedtest"
	"github.com/snapcore/snapd/seed/seedwriter"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/naming"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

type deviceMgrInstallSuite struct {
	deviceMgrBaseSuite
	*seedtest.TestingSeed20
}

func (s *deviceMgrInstallSuite) SetUpTest(c *C) {
	s.TestingSeed20 = &seedtest.TestingSeed20{}
	s.SeedDir = dirs.SnapSeedDir
}

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

func (s *deviceMgrInstallSuite) setupSystemSeed(c *C, sysLabel, gadgetYaml string, isClassic bool, kModsRevs map[string]snap.Revision) (*asserts.Model, map[string]interface{}) {
	s.StoreSigning = assertstest.NewStoreStack("can0nical", nil)

	s.Brands = assertstest.NewSigningAccounts(s.StoreSigning)
	s.Brands.Register("my-brand", brandPrivKey, nil)

	// now create a minimal seed dir with snaps/assertions
	testSeed := &seedtest.TestingSeed20{
		SeedSnaps: seedtest.SeedSnaps{
			StoreSigning: s.StoreSigning,
			Brands:       s.Brands,
		},
		SeedDir: dirs.SnapSeedDir,
	}

	restore := seed.MockTrusted(testSeed.StoreSigning.Trusted)
	s.AddCleanup(restore)

	assertstest.AddMany(s.StoreSigning.Database, s.Brands.AccountsAndKeys("my-brand")...)

	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["snapd"], nil, snap.R(1), "my-brand", s.StoreSigning.Database)
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["core24"], nil, snap.R(1), "my-brand", s.StoreSigning.Database)
	s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["pc=24"],
		[][]string{
			{"meta/gadget.yaml", gadgetYaml},
			{"pc-boot.img", ""}, {"pc-core.img", ""}, {"grubx64.efi", ""},
			{"shim.efi.signed", ""}, {"grub.conf", ""}},
		snap.R(1), "my-brand", s.StoreSigning.Database)
	if len(kModsRevs) > 0 {
		s.MakeAssertedSnapWithComps(c,
			seedtest.SampleSnapYaml["pc-kernel=24+kmods"],
			[][]string{{"kernel.efi", ""}}, snap.R(1), kModsRevs, "my-brand", s.StoreSigning.Database)
	} else {
		s.MakeAssertedSnap(c, seedtest.SampleSnapYaml["pc-kernel=24"],
			[][]string{{"kernel.efi", ""}}, snap.R(1), "my-brand", s.StoreSigning.Database)
	}

	s.MakeAssertedSnapWithComps(c, seedtest.SampleSnapYaml["optional24"], nil, snap.R(1), nil, "my-brand", s.StoreSigning.Database)

	var kmods map[string]interface{}
	if len(kModsRevs) > 0 {
		kmods = map[string]interface{}{
			"kcomp1": "required",
			"kcomp2": "required",
		}
	}
	model := map[string]interface{}{
		"display-name": "my model",
		"architecture": "amd64",
		"base":         "core24",
		"grade":        "dangerous",
		"snaps": []interface{}{
			map[string]interface{}{
				"name":            "pc-kernel",
				"id":              s.AssertedSnapID("pc-kernel"),
				"type":            "kernel",
				"default-channel": "24",
				"components":      kmods,
			},
			map[string]interface{}{
				"name":            "pc",
				"id":              s.AssertedSnapID("pc"),
				"type":            "gadget",
				"default-channel": "24",
			},
			map[string]interface{}{
				"name": "snapd",
				"id":   s.AssertedSnapID("snapd"),
				"type": "snapd",
			},
			map[string]interface{}{
				"name": "core24",
				"id":   s.AssertedSnapID("core24"),
				"type": "base",
			},
			map[string]interface{}{
				"name": "optional24",
				"id":   s.AssertedSnapID("optional24"),
				"components": map[string]interface{}{
					"comp1": "optional",
				},
			},
		},
	}
	if isClassic {
		model["classic"] = "true"
		model["distribution"] = "ubuntu"
	}

	return s.MakeSeed(c, sysLabel, "my-brand", "my-model", model, []*seedwriter.OptionsSnap{
		{
			Name:       "optional24",
			Components: []seedwriter.OptionsComponent{{Name: "comp1"}},
		},
	}), model
}

type finishStepOpts struct {
	encrypted          bool
	installClassic     bool
	hasPartial         bool
	hasSystemSeed      bool
	hasKernelModsComps bool
	optionalContainers *seed.OptionalContainers
}

type mockSystemSeedWithLabelOpts struct {
	isClassic       bool
	hasSystemSeed   bool
	hasPartial      bool
	preseedArtifact bool
	testCompsMode   bool
	kModsRevs       map[string]snap.Revision
	types           []snap.Type
}

func (s *deviceMgrInstallSuite) mockSystemSeedWithLabel(c *C, label string, seedCopyFn func(string, seed.CopyOptions, timings.Measurer) error, opts mockSystemSeedWithLabelOpts) (gadgetSnapPath, kernelSnapPath string, kCompsPaths []string, ginfo *gadget.Info, mountCmd *testutil.MockCmd, rawModel map[string]interface{}) {
	// Mock partitioned disk
	gadgetYaml := gadgettest.SingleVolumeUC20GadgetYaml
	if opts.isClassic {
		if opts.hasSystemSeed {
			gadgetYaml = gadgettest.SingleVolumeClassicWithModesAndSystemSeedGadgetYaml
		} else {
			gadgetYaml = gadgettest.SingleVolumeClassicWithModesGadgetYaml
		}
	}
	seedGadget := gadgetYaml
	if opts.hasPartial {
		// This is the gadget provided by the installer, that must have
		// filled the partial information.
		gadgetYaml = gadgettest.SingleVolumeClassicWithModesFilledPartialGadgetYaml
		// This is the partial gadget, with parts not filled
		seedGadget = gadgettest.SingleVolumeClassicWithModesPartialGadgetYaml
	}
	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	ginfo, _, _, restore, err := gadgettest.MockGadgetPartitionedDisk(gadgetYaml, gadgetRoot)
	c.Assert(err, IsNil)
	s.AddCleanup(restore)

	// now create a label with snaps/assertions
	model, rawModel := s.setupSystemSeed(c, label, seedGadget, opts.isClassic, opts.kModsRevs)
	c.Check(model, NotNil)

	// Create fake seed that will return information from the label we created
	// (TODO: needs to be in sync with setupSystemSeed, fix that)
	kernelSnapPath = filepath.Join(s.SeedDir, "snaps", "pc-kernel_1.snap")
	baseSnapPath := filepath.Join(s.SeedDir, "snaps", "core24_1.snap")
	gadgetSnapPath = filepath.Join(s.SeedDir, "snaps", "pc_1.snap")

	var kernComps []seed.Component
	if len(opts.kModsRevs) > 0 {
		kernComps = []seed.Component{
			{
				Path: filepath.Join(s.SeedDir, "snaps", "pc-kernel+kcomp1_"+opts.kModsRevs["kcomp1"].String()+".comp"),
				CompSideInfo: snap.ComponentSideInfo{
					Component: naming.NewComponentRef("pc-kernel", "kcomp1"),
					Revision:  opts.kModsRevs["kcomp1"]},
			}}
		kCompsPaths = []string{kernComps[0].Path}
		if !opts.testCompsMode {
			kernComps = append(kernComps, seed.Component{
				Path: filepath.Join(s.SeedDir, "snaps", "pc-kernel+kcomp2_"+opts.kModsRevs["kcomp2"].String()+".comp"),
				CompSideInfo: snap.ComponentSideInfo{
					Component: naming.NewComponentRef("pc-kernel", "kcomp2"),
					Revision:  opts.kModsRevs["kcomp2"]},
			})
			kCompsPaths = append(kCompsPaths, kernComps[1].Path)
		}
	}
	essentialSnaps := make([]*seed.Snap, 0, len(opts.types))
	for _, typ := range opts.types {
		switch typ {
		case snap.TypeKernel:
			essentialSnaps = append(essentialSnaps, &seed.Snap{
				Path: kernelSnapPath,
				SideInfo: &snap.SideInfo{RealName: "pc-kernel",
					Revision: snap.R(1), SnapID: s.SeedSnaps.AssertedSnapID("pc-kernel")},
				EssentialType: snap.TypeKernel,
				Components:    kernComps,
			})
		case snap.TypeBase:
			essentialSnaps = append(essentialSnaps, &seed.Snap{
				Path: baseSnapPath,
				SideInfo: &snap.SideInfo{RealName: "core24",
					Revision: snap.R(1), SnapID: s.SeedSnaps.AssertedSnapID("core24")},
				EssentialType: snap.TypeBase,
			})
		case snap.TypeGadget:
			essentialSnaps = append(essentialSnaps, &seed.Snap{
				Path: gadgetSnapPath,
				SideInfo: &snap.SideInfo{RealName: "pc",
					Revision: snap.R(1), SnapID: s.SeedSnaps.AssertedSnapID("pc")},
				EssentialType: snap.TypeGadget,
			})
		}
	}

	restore = devicestate.MockSeedOpen(func(seedDir, label string) (seed.Seed, error) {
		return &fakeSeedCopier{
			copyFn: seedCopyFn,
			optionalContainers: seed.OptionalContainers{
				Snaps:      []string{"optional24"},
				Components: map[string][]string{"optional24": {"comp1"}},
			},
			fakeSeed: fakeSeed{
				essentialSnaps:  essentialSnaps,
				model:           model,
				preseedArtifact: opts.preseedArtifact,
			},
		}, nil
	})
	s.AddCleanup(restore)

	// Mock calls to systemd-mount, which is used to mount snaps from the system label
	mountCmd = testutil.MockCommand(c, "systemd-mount", "")
	s.AddCleanup(func() { mountCmd.Restore() })

	return gadgetSnapPath, kernelSnapPath, kCompsPaths, ginfo, mountCmd, rawModel
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

type fakeSeedCopier struct {
	fakeSeed
	optionalContainers seed.OptionalContainers
	copyFn             func(seedDir string, opts seed.CopyOptions, tm timings.Measurer) error
}

func (s *fakeSeedCopier) Copy(seedDir string, opts seed.CopyOptions, tm timings.Measurer) error {
	return s.copyFn(seedDir, opts, tm)
}

func (s *fakeSeedCopier) OptionalContainers() (seed.OptionalContainers, error) {
	return s.optionalContainers, nil
}

// TODO encryption case for the finish step is not tested yet, it needs more mocking
func (s *deviceMgrInstallAPISuite) testInstallFinishStep(c *C, opts finishStepOpts) {
	if opts.hasSystemSeed && !opts.installClassic {
		c.Fatal("explicitly setting hasSystemSeed is only supported with installClassic")
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
		kModsRevs = map[string]snap.Revision{"kcomp1": snap.R(77), "kcomp2": snap.R(77)}
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
			IsCore:           false,
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

	// Insert encryption data when enabled
	if opts.encrypted {
		// Mock TPM and sealing
		restore := installLogic.MockSecbootCheckTPMKeySealingSupported(func(tpmMode secboot.TPMProvisionMode) error {
			c.Check(tpmMode, Equals, secboot.TPMProvisionFull)
			return nil
		})
		s.AddCleanup(restore)
		restore = boot.MockSealKeyToModeenv(func(key, saveKey keys.EncryptionKey, model *asserts.Model, modeenv *boot.Modeenv, flags boot.MockSealKeyToModeenvFlags) error {
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
			return nil
		})
		s.AddCleanup(restore)

		// Insert encryption set-up data in state cache
		restore = devicestate.MockEncryptionSetupDataInCache(s.state, label)
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

	if !opts.installClassic {
		c.Check(seedCopyCalled, Equals, true)
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

func (s *deviceMgrInstallAPISuite) TestInstallClassicFinishEncryptionAndSystemSeedHappy(c *C) {
	s.testInstallFinishStep(c, finishStepOpts{
		encrypted:      true,
		installClassic: true,
		hasSystemSeed:  true,
	})
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

func (s *deviceMgrInstallAPISuite) testInstallSetupStorageEncryption(c *C, hasTPM bool) {
	// Mock label
	label := "classic"
	isClassic := true
	seedCopyFn := func(seedDir string, opts seed.CopyOptions, tm timings.Measurer) error {
		return fmt.Errorf("unexpected copy call")
	}
	seedOpts := mockSystemSeedWithLabelOpts{
		isClassic:     isClassic,
		hasSystemSeed: false,
		hasPartial:    false,
		types:         []snap.Type{snap.TypeKernel, snap.TypeBase, snap.TypeGadget},
	}
	gadgetSnapPath, kernelSnapPath, _, ginfo, mountCmd, _ := s.mockSystemSeedWithLabel(
		c, label, seedCopyFn, seedOpts)

	// Simulate system with TPM
	if hasTPM {
		restore := installLogic.MockSecbootCheckTPMKeySealingSupported(func(tpmMode secboot.TPMProvisionMode) error {
			c.Check(tpmMode, Equals, secboot.TPMProvisionFull)
			return nil
		})
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
		{"systemd-mount", kernelSnapPath, kernelDir},
		{"systemd-mount", gadgetSnapPath, gadgetDir},
		{"systemd-mount", "--umount", kernelDir},
		{"systemd-mount", "--umount", gadgetDir},
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
