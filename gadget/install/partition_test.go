// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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

package install_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/testutil"
)

func TestInstall(t *testing.T) { TestingT(t) }

type partitionTestSuite struct {
	testutil.BaseTest

	dir        string
	gadgetRoot string
	cmdPartx   *testutil.MockCmd
}

var _ = Suite(&partitionTestSuite{})

func (s *partitionTestSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()
	s.gadgetRoot = filepath.Join(c.MkDir(), "gadget")

	s.cmdPartx = testutil.MockCommand(c, "partx", "")
	s.AddCleanup(s.cmdPartx.Restore)

	cmdSfdisk := testutil.MockCommand(c, "sfdisk", `echo "sfdisk was not mocked"; exit 1`)
	s.AddCleanup(cmdSfdisk.Restore)
	cmdLsblk := testutil.MockCommand(c, "lsblk", `echo "lsblk was not mocked"; exit 1`)
	s.AddCleanup(cmdLsblk.Restore)

	cmdUdevadm := testutil.MockCommand(c, "udevadm", `echo "udevadm was not mocked"; exit 1`)
	s.AddCleanup(cmdUdevadm.Restore)
}

const (
	scriptPartitionsNone = iota
	scriptPartitionsBios
	scriptPartitionsBiosSeed
	scriptPartitionsBiosSeedData
)

func makeMockDiskMappingIncludingPartitions(num int) *disks.MockDiskMapping {
	disk := &disks.MockDiskMapping{
		DevNum:              "42:0",
		DiskSizeInBytes:     (8388574 + 34) * 512,
		DiskUsableSectorEnd: 8388574 + 1,
		DiskSchema:          "gpt",
		ID:                  "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
		SectorSizeBytes:     512,
		Structure:           []disks.Partition{},
		DevNode:             "/dev/node",
	}

	if num >= scriptPartitionsBios {
		disk.Structure = append(disk.Structure, disks.Partition{
			KernelDeviceNode: "/dev/node1",
			StartInBytes:     2048 * 512,
			SizeInBytes:      2048 * 512,
			PartitionType:    "21686148-6449-6E6F-744E-656564454649",
			PartitionUUID:    "2E59D969-52AB-430B-88AC-F83873519F6F",
			PartitionLabel:   "BIOS Boot",
			Major:            42,
			Minor:            1,
			DiskIndex:        1,
		})
	}

	if num >= scriptPartitionsBiosSeed {
		disk.Structure = append(disk.Structure, disks.Partition{
			KernelDeviceNode: "/dev/node2",
			StartInBytes:     4096 * 512,
			SizeInBytes:      2457600 * 512,
			PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			PartitionUUID:    "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
			PartitionLabel:   "Recovery",
			Major:            42,
			Minor:            2,
			DiskIndex:        2,
			FilesystemType:   "vfat",
			FilesystemUUID:   "A644-B807",
			FilesystemLabel:  "ubuntu-seed",
		})
	}

	if num >= scriptPartitionsBiosSeedData {
		disk.Structure = append(disk.Structure, disks.Partition{
			KernelDeviceNode: "/dev/node3",
			StartInBytes:     2461696 * 512,
			SizeInBytes:      2457600 * 512,
			PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			PartitionUUID:    "F940029D-BFBB-4887-9D44-321E85C63866",
			PartitionLabel:   "Writable",
			Major:            42,
			Minor:            3,
			DiskIndex:        3,
			FilesystemType:   "ext4",
			FilesystemUUID:   "8781-433a",
			FilesystemLabel:  "ubuntu-data",
		})
	}

	return disk
}

var mockOnDiskStructureWritable = gadget.OnDiskStructure{
	Node:             "/dev/node3",
	Name:             "Writable",
	Type:             "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
	PartitionFSLabel: "ubuntu-data",
	PartitionFSType:  "ext4",
	StartOffset:      1260388352,
	// Note the DiskIndex appears to be the same as the YamlIndex, but this is
	// because YamlIndex starts at 0 and DiskIndex starts at 1, and there is a
	// yaml structure (the MBR) that does not appear on disk
	DiskIndex: 3,
	// expanded to fill the disk
	Size: 2*quantity.SizeGiB + 845*quantity.SizeMiB + 1031680,
}

func createOnDiskStructureSave(enclosing *gadget.Volume) *gadget.OnDiskStructure {
	return &gadget.OnDiskStructure{
		Node:             "/dev/node3",
		Name:             "Save",
		Size:             128 * quantity.SizeMiB,
		Type:             "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
		PartitionFSLabel: "ubuntu-save",
		PartitionFSType:  "ext4",
		StartOffset:      1260388352,
		// Note the DiskIndex appears to be the same as the YamlIndex, but this is
		// because YamlIndex starts at 0 and DiskIndex starts at 1, and there is a
		// yaml structure (the MBR) that does not appear on disk
		DiskIndex: 3,
	}
}

func createOnDiskStructureWritableAfterSave(enclosing *gadget.Volume) *gadget.OnDiskStructure {
	return &gadget.OnDiskStructure{
		Node: "/dev/node4",
		Name: "Writable",
		// expanded to fill the disk
		Size:             2*quantity.SizeGiB + 717*quantity.SizeMiB + 1031680,
		Type:             "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
		PartitionFSLabel: "ubuntu-data",
		PartitionFSType:  "ext4",
		StartOffset:      1394606080,
		// Note the DiskIndex appears to be the same as the YamlIndex, but this is
		// because YamlIndex starts at 0 and DiskIndex starts at 1, and there is a
		// yaml structure (the MBR) that does not appear on disk
		DiskIndex: 4,
	}
}

type uc20Model struct{}

func (c uc20Model) Classic() bool             { return false }
func (c uc20Model) Grade() asserts.ModelGrade { return asserts.ModelSigned }

var uc20Mod = uc20Model{}

func (s *partitionTestSuite) TestBuildPartitionList(c *C) {
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": makeMockDiskMappingIncludingPartitions(scriptPartitionsBiosSeed),
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()
	mylog.Check(gadgettest.MakeMockGadget(s.gadgetRoot, gptGadgetContentWithSave))

	pv := mylog.Check2(gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod))


	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))


	// the expected expanded writable partition size is:
	// start offset = (2M + 1200M), expanded size in sectors = (8388575*512 - start offset)/512
	sfdiskInput, create := mylog.Check3(install.BuildPartitionList(dl, pv.Volume, nil))

	c.Assert(sfdiskInput.String(), Equals,
		`/dev/node3 : start=     2461696, size=      262144, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, name="Save"
/dev/node4 : start=     2723840, size=     5664735, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, name="Writable"
`)
	c.Check(create, NotNil)
	c.Assert(create, DeepEquals, []*gadget.OnDiskAndGadgetStructurePair{
		{
			DiskStructure:   createOnDiskStructureSave(pv.Volume),
			GadgetStructure: &pv.Volume.Structure[3],
		},
		{
			DiskStructure:   createOnDiskStructureWritableAfterSave(pv.Volume),
			GadgetStructure: &pv.Volume.Structure[4],
		},
	})
}

func (s *partitionTestSuite) TestBuildPartitionListPartsNotInGadget(c *C) {
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": makeMockDiskMappingIncludingPartitions(scriptPartitionsBiosSeed),
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()
	mylog.

		// This gadget does not specify the bios partition, but it is on the disk
		Check(gadgettest.MakeMockGadget(s.gadgetRoot, gptGadgetContentWithGap))

	pv := mylog.Check2(gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod))


	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))


	// the expected expanded writable partition size is: start
	// offset = (2M + 1200M), expanded size in sectors =
	// (8388575*512 - start offset)/512
	sfdiskInput, create := mylog.Check3(install.BuildPartitionList(dl, pv.Volume,
		&install.CreateOptions{}))

	c.Assert(sfdiskInput.String(), Equals,
		`/dev/node3 : start=     2461696, size=      262144, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, name="Save"
/dev/node4 : start=     2723840, size=     5664735, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, name="Writable"
`)
	c.Check(create, NotNil)
	c.Assert(create, DeepEquals, []*gadget.OnDiskAndGadgetStructurePair{
		{
			DiskStructure:   createOnDiskStructureSave(pv.Volume),
			GadgetStructure: &pv.Volume.Structure[1],
		},
		{
			DiskStructure:   createOnDiskStructureWritableAfterSave(pv.Volume),
			GadgetStructure: &pv.Volume.Structure[2],
		},
	})
}

func (s *partitionTestSuite) TestBuildPartitionListOnlyCreatablePartitions(c *C) {
	// drop the "BIOS Boot" partition from the mock disk so that we only have
	// ubuntu-seed (at normal location for the second partition, as if the first
	// partition just vanished from the disk)
	mockDisk := makeMockDiskMappingIncludingPartitions(scriptPartitionsBiosSeed)
	mockDisk.Structure = mockDisk.Structure[1:]
	mockDisk.Structure[0].DiskIndex = 1
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": mockDisk,
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()
	mylog.Check(gadgettest.MakeMockGadget(s.gadgetRoot, gptGadgetContentWithSave))

	pv := mylog.Check2(gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod))


	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))


	_, _ = mylog.Check3(install.BuildPartitionList(dl, pv.Volume, nil))
	c.Assert(err, ErrorMatches, `gadget and boot device /dev/node partition table not compatible: cannot find gadget structure "BIOS Boot" on disk`)
}

func (s *partitionTestSuite) TestBuildPartitionListExistingPartsInSizeRange(c *C) {
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": makeMockDiskMappingIncludingPartitions(scriptPartitionsBiosSeed),
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()
	mylog.

		// The gadget has size rage of [1000, 1400]MiB for the seed partition,
		// and the actual size on disk is 1200MiB. The partition on disk should
		// match the one in the gadget and we will created save and data
		// partitions right after it.
		Check(gadgettest.MakeMockGadget(s.gadgetRoot, gptGadgetContentWithRangeForSeed))

	pv := mylog.Check2(gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod))


	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))


	// the expected expanded writable partition size is:
	// start offset = (2M + 1200M), expanded size in sectors = (8388575*512 - start offset)/512
	sfdiskInput, create := mylog.Check3(install.BuildPartitionList(dl, pv.Volume, nil))

	c.Assert(sfdiskInput.String(), Equals,
		`/dev/node3 : start=     2461696, size=      262144, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, name="Save"
/dev/node4 : start=     2723840, size=     5664735, type=0FC63DAF-8483-4772-8E79-3D69D8477DE4, name="Writable"
`)
	c.Check(create, NotNil)
	c.Assert(create, DeepEquals, []*gadget.OnDiskAndGadgetStructurePair{
		{
			DiskStructure:   createOnDiskStructureSave(pv.Volume),
			GadgetStructure: &pv.Volume.Structure[3],
		},
		{
			DiskStructure:   createOnDiskStructureWritableAfterSave(pv.Volume),
			GadgetStructure: &pv.Volume.Structure[4],
		},
	})
}

func (s *partitionTestSuite) TestCreatePartitions(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", "")
	defer cmdSfdisk.Restore()

	m := map[string]*disks.MockDiskMapping{
		"/dev/node": makeMockDiskMappingIncludingPartitions(scriptPartitionsBiosSeed),
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	cmdUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer cmdUdevadm.Restore()

	calls := 0
	restore = install.MockEnsureNodesExist(func(nodes []string, timeout time.Duration) error {
		calls++
		c.Assert(nodes, HasLen, 1)
		c.Assert(nodes[0], Equals, "/dev/node3")
		return nil
	})
	defer restore()
	mylog.Check(gadgettest.MakeMockGadget(s.gadgetRoot, gadgetContent))

	pv := mylog.Check2(gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod))


	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))

	opts := &install.CreateOptions{
		GadgetRootDir: s.gadgetRoot,
	}
	created := mylog.Check2(install.TestCreateMissingPartitions(dl, pv.Volume, opts))

	c.Assert(created, DeepEquals, []*gadget.OnDiskAndGadgetStructurePair{
		{
			DiskStructure:   &mockOnDiskStructureWritable,
			GadgetStructure: &pv.Volume.Structure[3],
		},
	})

	c.Assert(calls, Equals, 1)

	// Check partition table write
	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--append", "--no-reread", "/dev/node"},
	})

	// Check partition table update
	c.Assert(s.cmdPartx.Calls(), DeepEquals, [][]string{
		{"partx", "-u", "/dev/node"},
	})

	c.Assert(cmdUdevadm.Calls(), DeepEquals, [][]string{
		{"udevadm", "settle", "--timeout=180"},
	})
}

func (s *partitionTestSuite) TestCreatePartitionsNonRolePartitions(c *C) {
	cmdSfdisk := testutil.MockCommand(c, "sfdisk", "")
	defer cmdSfdisk.Restore()

	m := map[string]*disks.MockDiskMapping{
		"/dev/node": makeMockDiskMappingIncludingPartitions(scriptPartitionsNone),
	}
	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	cmdUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer cmdUdevadm.Restore()

	calls := 0
	restore = install.MockEnsureNodesExist(func(nodes []string, timeout time.Duration) error {
		calls++
		c.Assert(nodes, HasLen, 3)
		c.Assert(nodes[0], Equals, "/dev/node1")
		c.Assert(nodes[1], Equals, "/dev/node2")
		c.Assert(nodes[2], Equals, "/dev/node3")
		return nil
	})
	defer restore()
	mylog.Check(gadgettest.MakeMockGadget(s.gadgetRoot, gadgetContent))

	pv := mylog.Check2(gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod))


	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))

	opts := &install.CreateOptions{
		GadgetRootDir:              s.gadgetRoot,
		CreateAllMissingPartitions: true,
	}
	created := mylog.Check2(install.TestCreateMissingPartitions(dl, pv.Volume, opts))

	c.Assert(created, HasLen, 3)
	c.Assert(calls, Equals, 1)
}

func (s *partitionTestSuite) TestRemovePartitionsTrivial(c *C) {
	// no locally created partitions
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": makeMockDiskMappingIncludingPartitions(scriptPartitionsBios),
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()
	mylog.Check(gadgettest.MakeMockGadget(s.gadgetRoot, gadgetContent))

	gInfo := mylog.Check2(gadget.ReadInfoAndValidate(s.gadgetRoot, uc20Mod, nil))


	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))

	mylog.Check(install.RemoveCreatedPartitions(s.gadgetRoot, gInfo.Volumes["pc"], dl))

}

func (s *partitionTestSuite) TestRemovePartitions(c *C) {
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": {
			DevNum:  "42:0",
			DevNode: "/dev/node",
			// assume GPT backup header section is 34 sectors long
			DiskSizeInBytes:     (8388574 + 34) * 512,
			DiskUsableSectorEnd: 8388574 + 1,
			DiskSchema:          "gpt",
			ID:                  "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
			SectorSizeBytes:     512,
			Structure: []disks.Partition{
				// all 3 partitions present
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     2048 * 512,
					SizeInBytes:      2048 * 512,
					PartitionType:    "21686148-6449-6E6F-744E-656564454649",
					PartitionUUID:    "2E59D969-52AB-430B-88AC-F83873519F6F",
					PartitionLabel:   "BIOS Boot",
					Major:            42,
					Minor:            1,
					DiskIndex:        1,
				},
				{
					KernelDeviceNode: "/dev/node2",
					StartInBytes:     4096 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					PartitionUUID:    "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
					PartitionLabel:   "Recovery",
					Major:            42,
					Minor:            2,
					DiskIndex:        2,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-seed",
				},
				{
					KernelDeviceNode: "/dev/node3",
					StartInBytes:     2461696 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					PartitionUUID:    "F940029D-BFBB-4887-9D44-321E85C63866",
					PartitionLabel:   "Writable",
					Major:            42,
					Minor:            3,
					DiskIndex:        3,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8781-433a",
					FilesystemLabel:  "ubuntu-data",
				},
			},
		},
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	cmdSfdisk := testutil.MockCommand(c, "sfdisk", "")
	defer cmdSfdisk.Restore()

	cmdUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer cmdUdevadm.Restore()

	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))

	mylog.Check(gadgettest.MakeMockGadget(s.gadgetRoot, gadgetContent))

	gInfo := mylog.Check2(gadget.ReadInfoAndValidate(s.gadgetRoot, uc20Mod, nil))

	mylog.Check(install.RemoveCreatedPartitions(s.gadgetRoot, gInfo.Volumes["pc"], dl))


	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--no-reread", "--delete", "/dev/node", "3"},
	})

	c.Assert(s.cmdPartx.Calls(), DeepEquals, [][]string{
		{"partx", "-u", "/dev/node"},
	})

	// check that the OnDiskVolume was updated as expected
	c.Assert(dl.Structure, DeepEquals, []gadget.OnDiskStructure{
		{
			Name:        "BIOS Boot",
			Size:        1024 * 1024,
			Type:        "21686148-6449-6E6F-744E-656564454649",
			StartOffset: 1024 * 1024,
			DiskIndex:   1,
			Node:        "/dev/node1",
		},
		{
			PartitionFSLabel: "ubuntu-seed",
			Name:             "Recovery",
			Type:             "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			PartitionFSType:  "vfat",
			StartOffset:      1024*1024 + 1024*1024,
			DiskIndex:        2,
			Node:             "/dev/node2",
			Size:             2457600 * 512,
		},
	})
}

func (s *partitionTestSuite) TestRemovePartitionsWithDeviceRescan(c *C) {
	devPath := filepath.Join(s.dir, "/sys/foo/")
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": {
			DevNum:  "42:0",
			DevNode: "/dev/node",
			DevPath: devPath,
			// assume GPT backup header section is 34 sectors long
			DiskSizeInBytes:     (8388574 + 34) * 512,
			DiskUsableSectorEnd: 8388574 + 1,
			DiskSchema:          "gpt",
			ID:                  "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
			SectorSizeBytes:     512,
			Structure: []disks.Partition{
				// all 3 partitions present
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     2048 * 512,
					SizeInBytes:      2048 * 512,
					PartitionType:    "21686148-6449-6E6F-744E-656564454649",
					PartitionUUID:    "2E59D969-52AB-430B-88AC-F83873519F6F",
					PartitionLabel:   "BIOS Boot",
					Major:            42,
					Minor:            1,
					DiskIndex:        1,
				},
				{
					KernelDeviceNode: "/dev/node2",
					StartInBytes:     4096 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					PartitionUUID:    "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
					PartitionLabel:   "Recovery",
					Major:            42,
					Minor:            2,
					DiskIndex:        2,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-seed",
				},
				{
					KernelDeviceNode: "/dev/node3",
					StartInBytes:     2461696 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					PartitionUUID:    "F940029D-BFBB-4887-9D44-321E85C63866",
					PartitionLabel:   "Writable",
					Major:            42,
					Minor:            3,
					DiskIndex:        3,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8781-433a",
					FilesystemLabel:  "ubuntu-data",
				},
			},
		},
	}
	mylog.

		// make empty device rescan file
		Check(os.MkdirAll(filepath.Join(devPath, "device"), 0755))

	mylog.Check(os.WriteFile(filepath.Join(devPath, "device", "rescan"), nil, 0755))


	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	cmdSfdisk := testutil.MockCommand(c, "sfdisk", "")
	defer cmdSfdisk.Restore()

	cmdUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer cmdUdevadm.Restore()

	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))

	mylog.Check(gadgettest.MakeMockGadget(s.gadgetRoot, gadgetContent))

	mylog.

		// add the file to indicate we should do the device/rescan trick
		Check(os.WriteFile(filepath.Join(s.gadgetRoot, "meta", "force-partition-table-reload-via-device-rescan"), nil, 0755))


	gInfo := mylog.Check2(gadget.ReadInfoAndValidate(s.gadgetRoot, uc20Mod, nil))

	mylog.Check(install.RemoveCreatedPartitions(s.gadgetRoot, gInfo.Volumes["pc"], dl))


	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--no-reread", "--delete", "/dev/node", "3"},
	})

	// didn't call partx
	c.Assert(s.cmdPartx.Calls(), HasLen, 0)

	// but we did write to the sysfs file
	c.Assert(filepath.Join(devPath, "device", "rescan"), testutil.FileEquals, "1\n")

	// check that the OnDiskVolume was updated as expected
	c.Assert(dl.Structure, DeepEquals, []gadget.OnDiskStructure{
		{
			Name:        "BIOS Boot",
			Type:        "21686148-6449-6E6F-744E-656564454649",
			StartOffset: 1024 * 1024,
			DiskIndex:   1,
			Node:        "/dev/node1",
			Size:        1024 * 1024,
		},
		{
			PartitionFSLabel: "ubuntu-seed",
			Name:             "Recovery",
			Type:             "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			PartitionFSType:  "vfat",
			StartOffset:      1024*1024 + 1024*1024,
			DiskIndex:        2,
			Node:             "/dev/node2",
			Size:             2457600 * 512,
		},
	})
}

const gadgetContentDifferentOrder = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
        content:
          - image: pc-boot.img
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
        content:
          - image: pc-core.img
      - name: Writable
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
      - name: Recovery
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
`

func (s *partitionTestSuite) TestRemovePartitionsNonAdjacent(c *C) {
	m := map[string]*disks.MockDiskMapping{
		"/dev/node": {
			DevNum:  "42:0",
			DevNode: "/dev/node",
			// assume GPT backup header section is 34 sectors long
			DiskSizeInBytes:     (8388574 + 34) * 512,
			DiskUsableSectorEnd: 8388574 + 1,
			DiskSchema:          "gpt",
			ID:                  "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
			SectorSizeBytes:     512,
			Structure: []disks.Partition{
				// all 3 partitions present
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     1024 * 1024,
					SizeInBytes:      2048 * 512,
					PartitionType:    "21686148-6449-6E6F-744E-656564454649",
					PartitionUUID:    "2E59D969-52AB-430B-88AC-F83873519F6F",
					PartitionLabel:   "BIOS Boot",
					Major:            42,
					Minor:            1,
					DiskIndex:        1,
				},
				{
					KernelDeviceNode: "/dev/node2",
					StartInBytes:     1024*1024 + 1024*1024,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					PartitionUUID:    "F940029D-BFBB-4887-9D44-321E85C63866",
					PartitionLabel:   "Writable",
					Major:            42,
					Minor:            2,
					DiskIndex:        2,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8781-433a",
					FilesystemLabel:  "ubuntu-data",
				},
				{
					KernelDeviceNode: "/dev/node3",
					StartInBytes:     1024*1024 + 1024*1024 + 2457600*512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					PartitionUUID:    "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
					PartitionLabel:   "Recovery",
					Major:            42,
					Minor:            3,
					DiskIndex:        3,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-seed",
				},
			},
		},
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	cmdSfdisk := testutil.MockCommand(c, "sfdisk", "")
	defer cmdSfdisk.Restore()

	cmdUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer cmdUdevadm.Restore()

	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("/dev/node"))

	mylog.Check(gadgettest.MakeMockGadget(s.gadgetRoot, gadgetContentDifferentOrder))

	gInfo := mylog.Check2(gadget.ReadInfoAndValidate(s.gadgetRoot, uc20Mod, nil))

	mylog.Check(install.RemoveCreatedPartitions(s.gadgetRoot, gInfo.Volumes["pc"], dl))


	c.Assert(cmdSfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", "--no-reread", "--delete", "/dev/node", "2"},
	})

	// check that the OnDiskVolume was updated as expected
	c.Assert(dl.Structure, DeepEquals, []gadget.OnDiskStructure{
		{
			Name:        "BIOS Boot",
			Type:        "21686148-6449-6E6F-744E-656564454649",
			StartOffset: 1024 * 1024,
			DiskIndex:   1,
			Node:        "/dev/node1",
			Size:        1024 * 1024,
		},
		{
			PartitionFSLabel: "ubuntu-seed",
			Name:             "Recovery",
			Type:             "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			PartitionFSType:  "vfat",
			StartOffset:      1024*1024 + 1024*1024 + 2457600*512,
			Node:             "/dev/node3",
			DiskIndex:        3,
			Size:             2457600 * 512,
		},
	})
}

func (s *partitionTestSuite) TestEnsureNodesExist(c *C) {
	const mockUdevadmScript = `err=%q; echo "$err"; [ -n "$err" ] && exit 1 || exit 0`
	for _, tc := range []struct {
		utErr string
		err   string
	}{
		{utErr: "", err: ""},
		{utErr: "some error", err: "some error"},
	} {
		c.Logf("utErr:%q err:%q", tc.utErr, tc.err)

		node := filepath.Join(c.MkDir(), "node")
		mylog.Check(os.WriteFile(node, nil, 0644))


		cmdUdevadm := testutil.MockCommand(c, "udevadm", fmt.Sprintf(mockUdevadmScript, tc.utErr))
		defer cmdUdevadm.Restore()

		nodes := []string{node}
		mylog.Check(install.EnsureNodesExist(nodes, 10*time.Millisecond))
		if tc.err == "" {

		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}

		c.Assert(cmdUdevadm.Calls(), DeepEquals, [][]string{
			{"udevadm", "trigger", "--settle", node},
		})
	}
}

func (s *partitionTestSuite) TestEnsureNodesExistTimeout(c *C) {
	cmdUdevadm := testutil.MockCommand(c, "udevadm", "")
	defer cmdUdevadm.Restore()

	node := filepath.Join(c.MkDir(), "node")
	nodes := []string{node}
	t := time.Now()
	timeout := 1 * time.Second
	mylog.Check(install.EnsureNodesExist(nodes, timeout))
	c.Assert(err, ErrorMatches, fmt.Sprintf("device %s not available", node))
	c.Assert(time.Since(t) >= timeout, Equals, true)
	c.Assert(cmdUdevadm.Calls(), HasLen, 0)
}

const gptGadgetContentWithSave = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
        content:
          - image: pc-boot.img
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
        content:
          - image: pc-core.img
      - name: Recovery
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
      - name: Save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 128M
      - name: Writable
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

const gptGadgetContentWithGap = `volumes:
  pc:
    bootloader: grub
    partial: [ structure ]
    structure:
      - name: Recovery
        offset: 2M
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
      - name: Save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 128M
      - name: Writable
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

const gptGadgetContentWithMinSize = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
        content:
          - image: pc-boot.img
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
        content:
          - image: pc-core.img
      - name: Recovery
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
      - name: Save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        min-size: 128M
        size: 256M
      - name: Writable
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

const gptGadgetContentWithRangeForSeed = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
        content:
          - image: pc-boot.img
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
      - name: Recovery
        role: system-seed
        filesystem: vfat
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        min-size: 1000M
        size: 1400M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
      - name: Save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 128M
      - name: Writable
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

func (s *partitionTestSuite) TestCreatedDuringInstallGPT(c *C) {
	m := map[string]*disks.MockDiskMapping{
		"node": {
			DevNum:              "42:0",
			DiskSizeInBytes:     (8388574 + 34) * 512,
			DiskUsableSectorEnd: 8388574 + 1,
			DiskSchema:          "gpt",
			ID:                  "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
			SectorSizeBytes:     512,
			DevNode:             "/dev/node",
			Structure: []disks.Partition{
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     2048 * 512,
					SizeInBytes:      2048 * 512,
					PartitionType:    "21686148-6449-6E6F-744E-656564454649",
					PartitionUUID:    "2E59D969-52AB-430B-88AC-F83873519F6F",
					PartitionLabel:   "BIOS Boot",
					Major:            42,
					Minor:            1,
					DiskIndex:        1,
				},
				{
					KernelDeviceNode: "/dev/node2",
					StartInBytes:     4096 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					PartitionUUID:    "44C3D5C3-CAE1-4306-83E8-DF437ACDB32F",
					PartitionLabel:   "ubuntu-seed",
					Major:            42,
					Minor:            2,
					DiskIndex:        2,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-seed",
				},
				{
					KernelDeviceNode: "/dev/node3",
					StartInBytes:     2461696 * 512,
					SizeInBytes:      262144 * 512,
					PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					PartitionUUID:    "F940029D-BFBB-4887-9D44-321E85C63866",
					PartitionLabel:   "ubuntu-boot",
					Major:            42,
					Minor:            3,
					DiskIndex:        3,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8781-433a",
					FilesystemLabel:  "ubuntu-boot",
				},
				{
					KernelDeviceNode: "/dev/node4",
					StartInBytes:     2723840 * 512,
					SizeInBytes:      2457600 * 512,
					PartitionType:    "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					PartitionUUID:    "G940029D-BFBB-4887-9D44-321E85C63866",
					PartitionLabel:   "ubuntu-data",
					Major:            42,
					Minor:            4,
					DiskIndex:        4,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8123-433a",
					FilesystemLabel:  "ubuntu-data",
				},
			},
		},
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()
	mylog.Check(gadgettest.MakeMockGadget(s.gadgetRoot, gptGadgetContentWithSave))

	pv := mylog.Check2(gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod))


	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("node"))


	list := install.CreatedDuringInstall(pv.Volume, dl)
	// only save and writable should show up
	c.Check(list, DeepEquals, []string{"/dev/node3", "/dev/node4"})
	mylog.

		// min-size for ubuntu-save for this gadget will match the third partition size
		// (but size wouldn't)
		Check(gadgettest.MakeMockGadget(s.gadgetRoot, gptGadgetContentWithMinSize))

	pv = mylog.Check2(gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod))


	dl = mylog.Check2(gadget.OnDiskVolumeFromDevice("node"))


	list = install.CreatedDuringInstall(pv.Volume, dl)
	// only save and writable should show up
	c.Check(list, DeepEquals, []string{"/dev/node3", "/dev/node4"})
}

// this is an mbr gadget like the pi, but doesn't have the amd64 mbr structure
// so it's probably not representative, but still useful for unit tests here
const mbrGadgetContentWithSave = `volumes:
  pc:
    schema: mbr
    bootloader: grub
    structure:
      - name: Recovery
        role: system-seed
        filesystem: vfat
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        offset: 2M
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
      - name: Boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
      - name: Save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 128M
      - name: Writable
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

func (s *partitionTestSuite) TestCreatedDuringInstallMBR(c *C) {
	const (
		twoMeg                   = 2 * 1024 * 1024
		oneHundredTwentyEightMeg = 128 * 1024 * 1024
		twelveHundredMeg         = 1200 * 1024 * 1024
	)
	m := map[string]*disks.MockDiskMapping{
		"node": {
			DevNum:              "42:0",
			DevNode:             "/dev/node",
			DiskSizeInBytes:     (8388574 + 34) * 512,
			DiskUsableSectorEnd: 8388574 + 1,
			DiskSchema:          "dos",
			ID:                  "9151F25B-CDF0-48F1-9EDE-68CBD616E2CA",
			SectorSizeBytes:     512,
			Structure: []disks.Partition{
				{
					KernelDeviceNode: "/dev/node1",
					StartInBytes:     twoMeg,
					SizeInBytes:      twelveHundredMeg,
					PartitionType:    "0a",
					PartitionLabel:   "Recovery",
					Major:            42,
					Minor:            1,
					DiskIndex:        1,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-seed",
				},
				{
					KernelDeviceNode: "/dev/node2",
					StartInBytes:     twelveHundredMeg + twoMeg,
					SizeInBytes:      twelveHundredMeg,
					PartitionType:    "b",
					PartitionLabel:   "Boot",
					Major:            42,
					Minor:            2,
					DiskIndex:        2,
					FilesystemType:   "vfat",
					FilesystemUUID:   "A644-B807",
					FilesystemLabel:  "ubuntu-boot",
				},
				{
					KernelDeviceNode: "/dev/node3",
					StartInBytes:     twoMeg + twelveHundredMeg + twelveHundredMeg,
					SizeInBytes:      oneHundredTwentyEightMeg,
					PartitionType:    "c",
					PartitionLabel:   "Save",
					Major:            42,
					Minor:            3,
					DiskIndex:        3,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8781-433a",
					FilesystemLabel:  "ubuntu-save",
				},
				{
					KernelDeviceNode: "/dev/node4",
					StartInBytes:     twoMeg + twelveHundredMeg + twelveHundredMeg + oneHundredTwentyEightMeg,
					SizeInBytes:      twelveHundredMeg,
					PartitionType:    "0d",
					PartitionLabel:   "Data",
					Major:            42,
					Minor:            4,
					DiskIndex:        4,
					FilesystemType:   "ext4",
					FilesystemUUID:   "8123-433a",
					FilesystemLabel:  "ubuntu-data",
				},
			},
		},
	}

	restore := disks.MockDeviceNameToDiskMapping(m)
	defer restore()

	dl := mylog.Check2(gadget.OnDiskVolumeFromDevice("node"))

	mylog.Check(gadgettest.MakeMockGadget(s.gadgetRoot, mbrGadgetContentWithSave))

	pv := mylog.Check2(gadgettest.MustLayOutSingleVolumeFromGadget(s.gadgetRoot, "", uc20Mod))


	list := install.CreatedDuringInstall(pv.Volume, dl)
	c.Assert(list, DeepEquals, []string{"/dev/node2", "/dev/node3", "/dev/node4"})
}
