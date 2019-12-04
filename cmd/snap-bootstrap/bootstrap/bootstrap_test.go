// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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
package bootstrap_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
)

func TestBootstrap(t *testing.T) { TestingT(t) }

type bootstrapSuite struct {
	dir string
}

var _ = Suite(&bootstrapSuite{})

// XXX: write a very high level integration like test here that
// mocks the world (sfdisk,lsblk,mkfs,...)? probably silly as
// each part inside bootstrap is tested and we have a spread test

func (s *bootstrapSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
	dirs.SetRootDir(s.dir)
}

func (s *bootstrapSuite) TestBootstrapRunError(c *C) {
	err := bootstrap.Run("", "", nil)
	c.Assert(err, ErrorMatches, "cannot use empty gadget root directory")
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

var mockDeviceLayout = partition.DeviceLayout{
	Structure: []partition.DeviceStructure{
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
					Size: 1 * gadget.SizeMiB,
				},
				StartOffset: 1 * gadget.SizeMiB,
			},
			Node: "/dev/node2",
		},
	},
	ID:         "anything",
	Device:     "/dev/node",
	Schema:     "gpt",
	Size:       0x500000,
	SectorSize: 512,
}

func (s *bootstrapSuite) TestLayoutCompatibility(c *C) {
	// same contents
	gadgetLayout := layoutFromYaml(c, mockGadgetYaml)
	err := bootstrap.EnsureLayoutCompatibility(gadgetLayout, &mockDeviceLayout)
	c.Assert(err, IsNil)

	// missing structure (that's ok)
	gadgetLayoutWithExtras := layoutFromYaml(c, mockGadgetYaml+mockExtraStructure)
	err = bootstrap.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &mockDeviceLayout)
	c.Assert(err, IsNil)

	deviceLayoutWithExtras := mockDeviceLayout
	deviceLayoutWithExtras.Structure = append(deviceLayoutWithExtras.Structure,
		partition.DeviceStructure{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Name:  "Extra partition",
					Size:  10 * gadget.SizeMiB,
					Label: "extra",
				},
				StartOffset: 2 * gadget.SizeMiB,
			},
			Node: "/dev/node3",
		},
	)
	// extra structure (should fail)
	err = bootstrap.EnsureLayoutCompatibility(gadgetLayout, &deviceLayoutWithExtras)
	c.Assert(err, ErrorMatches, `cannot find disk partition "extra".* in gadget`)
}

func (s *bootstrapSuite) TestSchemaCompatibility(c *C) {
	gadgetLayout := layoutFromYaml(c, mockGadgetYaml)
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
		err := bootstrap.EnsureLayoutCompatibility(gadgetLayout, &deviceLayout)
		if tc.e == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.e)
		}
	}
	c.Logf("-----")
}

func (s *bootstrapSuite) TestIDCompatibility(c *C) {
	gadgetLayout := layoutFromYaml(c, mockGadgetYaml)
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
		err := bootstrap.EnsureLayoutCompatibility(gadgetLayout, &deviceLayout)
		if tc.e == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.e)
		}
	}
	c.Logf("-----")
}

func layoutFromYaml(c *C, gadgetYaml string) *gadget.LaidOutVolume {
	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	err := os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetYaml), 0644)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(gadgetRoot)
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

func (s *bootstrapSuite) setupMockSysfs(c *C) {
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

func (s *bootstrapSuite) TestDeviceFromRoleHappy(c *C) {
	s.setupMockSysfs(c)
	lv := layoutFromYaml(c, mockUC20GadgetYaml)

	device, err := bootstrap.DeviceFromRole(lv, gadget.SystemSeed)
	c.Assert(err, IsNil)
	c.Check(device, Matches, ".*/dev/fakedevice0")
}

func (s *bootstrapSuite) TestDeviceFromRoleErrorNoMatchingSysfs(c *C) {
	// note no sysfs mocking
	lv := layoutFromYaml(c, mockUC20GadgetYaml)

	_, err := bootstrap.DeviceFromRole(lv, gadget.SystemSeed)
	c.Assert(err, ErrorMatches, `cannot find device for role "system-seed": device not found`)
}

func (s *bootstrapSuite) TestDeviceFromRoleErrorNoRole(c *C) {
	s.setupMockSysfs(c)
	lv := layoutFromYaml(c, mockGadgetYaml)

	_, err := bootstrap.DeviceFromRole(lv, gadget.SystemSeed)
	c.Assert(err, ErrorMatches, "cannot find role system-seed in gadget")
}
