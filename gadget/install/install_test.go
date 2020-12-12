// -*- Mode: Go; indent-tabs-mode: t -*-
// +build !nosecboot

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
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/testutil"
)

type installSuite struct {
	testutil.BaseTest

	dir string
}

var _ = Suite(&installSuite{})

// XXX: write a very high level integration like test here that
// mocks the world (sfdisk,lsblk,mkfs,...)? probably silly as
// each part inside bootstrap is tested and we have a spread test

func (s *installSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	s.dir = c.MkDir()
	dirs.SetRootDir(s.dir)
	s.AddCleanup(func() { dirs.SetRootDir("/") })
}

func (s *installSuite) TestInstallRunError(c *C) {
	sys, err := install.Run(nil, "", "", install.Options{}, nil)
	c.Assert(err, ErrorMatches, "cannot use empty gadget root directory")
	c.Check(sys, IsNil)
}

const mockGadgetYaml = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
`

const mockExtraStructure = `
      - name: Writable
        role: system-data
        filesystem-label: writable
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

var mockDeviceLayout = gadget.OnDiskVolume{
	Structure: []gadget.OnDiskStructure{
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Name: "mbr",
					Size: 440,
				},
				StartOffset: 0,
			},
			Node: "/dev/node1",
		},
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Name: "BIOS Boot",
					Size: 1 * quantity.SizeMiB,
				},
				StartOffset: 1 * quantity.OffsetMiB,
			},
			Node: "/dev/node2",
		},
	},
	ID:         "anything",
	Device:     "/dev/node",
	Schema:     "gpt",
	Size:       2 * quantity.SizeGiB,
	SectorSize: 512,
}

func (s *installSuite) TestLayoutCompatibility(c *C) {
	// same contents (the locally created structure should be ignored)
	gadgetLayout := layoutFromYaml(c, mockGadgetYaml, nil)
	err := install.EnsureLayoutCompatibility(gadgetLayout, &mockDeviceLayout)
	c.Assert(err, IsNil)

	// missing structure (that's ok)
	gadgetLayoutWithExtras := layoutFromYaml(c, mockGadgetYaml+mockExtraStructure, nil)
	err = install.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &mockDeviceLayout)
	c.Assert(err, IsNil)

	deviceLayoutWithExtras := mockDeviceLayout
	deviceLayoutWithExtras.Structure = append(deviceLayoutWithExtras.Structure,
		gadget.OnDiskStructure{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Name:  "Extra partition",
					Size:  10 * quantity.SizeMiB,
					Label: "extra",
				},
				StartOffset: 2 * quantity.OffsetMiB,
			},
			Node: "/dev/node3",
		},
	)
	// extra structure (should fail)
	err = install.EnsureLayoutCompatibility(gadgetLayout, &deviceLayoutWithExtras)
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node3.* in gadget`)

	// layout is not compatible if the device is too small
	smallDeviceLayout := mockDeviceLayout
	smallDeviceLayout.Size = 100 * quantity.SizeMiB
	// sanity check
	c.Check(gadgetLayoutWithExtras.Size > smallDeviceLayout.Size, Equals, true)
	err = install.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &smallDeviceLayout)
	c.Assert(err, ErrorMatches, `device /dev/node \(100 MiB\) is too small to fit the requested layout \(1\.17 GiB\)`)
}

func (s *installSuite) TestMBRLayoutCompatibility(c *C) {
	const mockMBRGadgetYaml = `volumes:
  pc:
    schema: mbr
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
`
	var mockMBRDeviceLayout = gadget.OnDiskVolume{
		Structure: []gadget.OnDiskStructure{
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						// partition names have no
						// meaning in MBR schema
						Name: "other",
						Size: 440,
					},
					StartOffset: 0,
				},
				Node: "/dev/node1",
			},
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						// partition names have no
						// meaning in MBR schema
						Name: "different BIOS Boot",
						Size: 1 * quantity.SizeMiB,
					},
					StartOffset: 1 * quantity.OffsetMiB,
				},
				Node: "/dev/node2",
			},
		},
		ID:         "anything",
		Device:     "/dev/node",
		Schema:     "dos",
		Size:       2 * quantity.SizeGiB,
		SectorSize: 512,
	}
	gadgetLayout := layoutFromYaml(c, mockMBRGadgetYaml, nil)
	err := install.EnsureLayoutCompatibility(gadgetLayout, &mockMBRDeviceLayout)
	c.Assert(err, IsNil)
	// structure is missing from disk
	gadgetLayoutWithExtras := layoutFromYaml(c, mockMBRGadgetYaml+mockExtraStructure, nil)
	err = install.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &mockMBRDeviceLayout)
	c.Assert(err, IsNil)
	// add it now
	deviceLayoutWithExtras := mockMBRDeviceLayout
	deviceLayoutWithExtras.Structure = append(deviceLayoutWithExtras.Structure,
		gadget.OnDiskStructure{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					// name is ignored with MBR schema
					Name:       "Extra partition",
					Size:       1200 * quantity.SizeMiB,
					Label:      "extra",
					Filesystem: "ext4",
					Type:       "83",
				},
				StartOffset: 2 * quantity.OffsetMiB,
			},
			Node: "/dev/node3",
		},
	)
	err = install.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayoutWithExtras)
	c.Assert(err, IsNil)
	// add another structure that's not part of the gadget
	deviceLayoutWithExtras.Structure = append(deviceLayoutWithExtras.Structure,
		gadget.OnDiskStructure{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					// name is ignored with MBR schema
					Name: "Extra extra partition",
					Size: 1 * quantity.SizeMiB,
				},
				StartOffset: 1202 * quantity.OffsetMiB,
			},
			Node: "/dev/node4",
		},
	)
	err = install.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayoutWithExtras)
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node4 \(starting at 1260388352\) in gadget: start offsets do not match \(disk: 1260388352 \(1.17 GiB\) and gadget: 2097152 \(2 MiB\)\)`)
}

func (s *installSuite) TestLayoutCompatibilityWithCreatedPartitions(c *C) {
	gadgetLayoutWithExtras := layoutFromYaml(c, mockGadgetYaml+mockExtraStructure, nil)
	deviceLayout := mockDeviceLayout

	// device matches gadget except for the filesystem type
	deviceLayout.Structure = append(deviceLayout.Structure,
		gadget.OnDiskStructure{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Name:       "Writable",
					Size:       1200 * quantity.SizeMiB,
					Label:      "writable",
					Filesystem: "something_else",
				},
				StartOffset: 2 * quantity.OffsetMiB,
			},
			Node: "/dev/node3",
		},
	)
	err := install.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayout)
	c.Assert(err, IsNil)

	// we are going to manipulate last structure, which has system-data role
	c.Assert(gadgetLayoutWithExtras.Structure[len(deviceLayout.Structure)-1].Role, Equals, gadget.SystemData)

	// change the role for the laid out volume to not be a partition role that
	// is created at install time (note that the duplicated seed role here is
	// technically incorrect, you can't have duplicated roles, but this
	// demonstrates that a structure that otherwise fits the bill but isn't a
	// role that is created during install will fail the filesystem match check)
	gadgetLayoutWithExtras.Structure[len(deviceLayout.Structure)-1].Role = gadget.SystemSeed

	// now we fail to find the /dev/node3 structure from the gadget on disk
	err = install.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayout)
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node3 \(starting at 2097152\) in gadget: filesystems do not match and the partition is not creatable at install`)

	// undo the role change
	gadgetLayoutWithExtras.Structure[len(deviceLayout.Structure)-1].Role = gadget.SystemData

	// change the gadget size to be bigger than the on disk size
	gadgetLayoutWithExtras.Structure[len(deviceLayout.Structure)-1].Size = 10000000 * quantity.SizeMiB

	// now we fail to find the /dev/node3 structure from the gadget on disk because the gadget says it must be bigger
	err = install.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayout)
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node3 \(starting at 2097152\) in gadget: on disk size is smaller than gadget size`)

	// change the gadget size to be smaller than the on disk size and the role to be one that is not expanded
	gadgetLayoutWithExtras.Structure[len(deviceLayout.Structure)-1].Size = 1 * quantity.SizeMiB
	gadgetLayoutWithExtras.Structure[len(deviceLayout.Structure)-1].Role = gadget.SystemBoot

	// now we fail because the gadget says it should be smaller and it can't be expanded
	err = install.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayout)
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node3 \(starting at 2097152\) in gadget: on disk size is larger than gadget size \(and the role should not be expanded\)`)

	// but a smaller partition on disk for SystemData role is okay
	gadgetLayoutWithExtras.Structure[len(deviceLayout.Structure)-1].Role = gadget.SystemData
	err = install.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayout)
	c.Assert(err, IsNil)
}

func (s *installSuite) TestSchemaCompatibility(c *C) {
	gadgetLayout := layoutFromYaml(c, mockGadgetYaml, nil)
	deviceLayout := mockDeviceLayout

	error_msg := "disk partitioning.* doesn't match gadget.*"

	for i, tc := range []struct {
		gs string
		ds string
		e  string
	}{
		{"", "dos", error_msg},
		{"", "gpt", ""},
		{"", "xxx", error_msg},
		{"mbr", "dos", ""},
		{"mbr", "gpt", error_msg},
		{"mbr", "xxx", error_msg},
		{"gpt", "dos", error_msg},
		{"gpt", "gpt", ""},
		{"gpt", "xxx", error_msg},
		// XXX: "mbr,gpt" is currently unsupported
		{"mbr,gpt", "dos", error_msg},
		{"mbr,gpt", "gpt", error_msg},
		{"mbr,gpt", "xxx", error_msg},
	} {
		c.Logf("%d: %q %q\n", i, tc.gs, tc.ds)
		gadgetLayout.Volume.Schema = tc.gs
		deviceLayout.Schema = tc.ds
		err := install.EnsureLayoutCompatibility(gadgetLayout, &deviceLayout)
		if tc.e == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.e)
		}
	}
	c.Logf("-----")
}

func (s *installSuite) TestIDCompatibility(c *C) {
	gadgetLayout := layoutFromYaml(c, mockGadgetYaml, nil)
	deviceLayout := mockDeviceLayout

	error_msg := "disk ID.* doesn't match gadget volume ID.*"

	for i, tc := range []struct {
		gid string
		did string
		e   string
	}{
		{"", "", ""},
		{"", "123", ""},
		{"123", "345", error_msg},
		{"123", "123", ""},
	} {
		c.Logf("%d: %q %q\n", i, tc.gid, tc.did)
		gadgetLayout.Volume.ID = tc.gid
		deviceLayout.ID = tc.did
		err := install.EnsureLayoutCompatibility(gadgetLayout, &deviceLayout)
		if tc.e == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.e)
		}
	}
	c.Logf("-----")
}

func layoutFromYaml(c *C, gadgetYaml string, model gadget.Model) *gadget.LaidOutVolume {
	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	err := os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetYaml), 0644)
	c.Assert(err, IsNil)
	pv, err := gadget.LaidOutVolumeFromGadget(gadgetRoot, model)
	c.Assert(err, IsNil)
	return pv
}

const mockUC20GadgetYaml = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 750M
`

func (s *installSuite) setupMockSysfs(c *C) {
	err := os.MkdirAll(filepath.Join(s.dir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(s.dir, "/dev/fakedevice0p1"), nil, 0644)
	c.Assert(err, IsNil)
	err = os.Symlink("../../fakedevice0p1", filepath.Join(s.dir, "/dev/disk/by-partlabel/ubuntu-seed"))
	c.Assert(err, IsNil)

	// make parent device
	err = ioutil.WriteFile(filepath.Join(s.dir, "/dev/fakedevice0"), nil, 0644)
	c.Assert(err, IsNil)
	// and fake /sys/block structure
	err = os.MkdirAll(filepath.Join(s.dir, "/sys/block/fakedevice0/fakedevice0p1"), 0755)
	c.Assert(err, IsNil)
}

func (s *installSuite) TestDeviceFromRoleHappy(c *C) {
	s.setupMockSysfs(c)
	lv := layoutFromYaml(c, mockUC20GadgetYaml, uc20Mod)

	device, err := install.DeviceFromRole(lv, gadget.SystemSeed)
	c.Assert(err, IsNil)
	c.Check(device, Matches, ".*/dev/fakedevice0")
}

func (s *installSuite) TestDeviceFromRoleErrorNoMatchingSysfs(c *C) {
	// note no sysfs mocking
	lv := layoutFromYaml(c, mockUC20GadgetYaml, uc20Mod)

	_, err := install.DeviceFromRole(lv, gadget.SystemSeed)
	c.Assert(err, ErrorMatches, `cannot find device for role "system-seed": device not found`)
}

func (s *installSuite) TestDeviceFromRoleErrorNoRole(c *C) {
	s.setupMockSysfs(c)
	lv := layoutFromYaml(c, mockGadgetYaml, nil)

	_, err := install.DeviceFromRole(lv, gadget.SystemSeed)
	c.Assert(err, ErrorMatches, "cannot find role system-seed in gadget")
}
