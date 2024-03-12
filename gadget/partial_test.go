// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package gadget_test

import (
	"fmt"
	"os"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	. "gopkg.in/check.v1"
)

func (s *gadgetYamlTestSuite) readGadgetVols(c *C) map[string]*gadget.Volume {
	info, err := gadget.ReadInfoAndValidate(s.dir, uc20Mod, nil)
	c.Assert(err, IsNil)
	return info.Volumes
}

func (s *gadgetYamlTestSuite) TestApplyInstallerVolumesToGadgetDeviceAndPartialSchema(c *C) {
	var yaml = []byte(`
volumes:
  vol0:
    partial: [schema]
    bootloader: u-boot
    structure:
      - name: ubuntu-seed
        filesystem: vfat
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-seed
      - name: ubuntu-boot
        filesystem: ext4
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-boot
      - name: ubuntu-save
        size: 1M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-save
      - name: ubuntu-data
        size: 1000M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-data
`)
	err := os.WriteFile(s.gadgetYamlPath, yaml, 0644)
	c.Assert(err, IsNil)

	installerVols := map[string]*gadget.Volume{
		"vol0": {
			Name:   "vol0",
			Schema: "gpt",
			Structure: []gadget.VolumeStructure{
				{
					Name:   "ubuntu-seed",
					Device: "/dev/vda1",
				},
				{
					Name:   "ubuntu-boot",
					Device: "/dev/vda2",
				},
				{
					Name:   "ubuntu-save",
					Device: "/dev/vda3",
				},
				{
					Name:   "ubuntu-data",
					Device: "/dev/vda4",
				},
			},
		},
	}

	// New schema and devices are set
	gVols := s.readGadgetVols(c)
	mergedVols, err := gadget.ApplyInstallerVolumesToGadget(installerVols, gVols)
	c.Assert(err, IsNil)
	c.Assert(mergedVols["vol0"].Schema, Equals, "gpt")
	for i, vs := range mergedVols["vol0"].Structure {
		c.Assert(vs.Device, Equals, fmt.Sprintf("/dev/vda%d", i+1))
	}

	// Invalid schema is detected
	installerVols["vol0"].Schema = "nextbigthing"
	mergedVols, err = gadget.ApplyInstallerVolumesToGadget(installerVols, gVols)
	c.Assert(err.Error(), Equals,
		`finalized volume "vol0" is wrong: invalid schema "nextbigthing"`)
	c.Assert(mergedVols, IsNil)

	// No schema set case
	installerVols["vol0"].Schema = ""
	mergedVols, err = gadget.ApplyInstallerVolumesToGadget(installerVols, gVols)
	c.Assert(err.Error(), Equals, `installer did not provide schema for volume "vol0"`)
	c.Assert(mergedVols, IsNil)
}

func (s *gadgetYamlTestSuite) TestApplyInstallerVolumesToGadgetPartialFilesystem(c *C) {
	var yaml = []byte(`
volumes:
  vol0:
    partial: [filesystem]
    bootloader: u-boot
    structure:
      - name: ubuntu-seed
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-seed
      - name: ubuntu-boot
        filesystem: ext4
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-boot
      - name: ubuntu-save
        size: 1M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-save
      - name: ubuntu-data
        size: 1000M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-data
`)
	err := os.WriteFile(s.gadgetYamlPath, yaml, 0644)
	c.Assert(err, IsNil)

	installerVols := map[string]*gadget.Volume{
		"vol0": {
			Name:   "vol0",
			Schema: "gpt",
			Structure: []gadget.VolumeStructure{
				{
					Name:       "ubuntu-seed",
					Filesystem: "vfat",
				},
				{
					Name: "ubuntu-boot",
				},
				{
					Name:       "ubuntu-save",
					Filesystem: "ext4",
				},
				{
					Name:       "ubuntu-data",
					Filesystem: "ext4",
				},
			},
		},
	}

	gVols := s.readGadgetVols(c)
	mergedVols, err := gadget.ApplyInstallerVolumesToGadget(installerVols, gVols)
	c.Assert(err, IsNil)
	c.Assert(mergedVols["vol0"].Structure[0].Filesystem, Equals, "vfat")
	c.Assert(mergedVols["vol0"].Structure[2].Filesystem, Equals, "ext4")
	c.Assert(mergedVols["vol0"].Structure[3].Filesystem, Equals, "ext4")

	installerVols["vol0"].Structure[0].Filesystem = ""
	mergedVols, err = gadget.ApplyInstallerVolumesToGadget(installerVols, gVols)
	c.Assert(err.Error(), Equals, `installer did not provide filesystem for structure "ubuntu-seed" in volume "vol0"`)
	c.Assert(mergedVols, IsNil)

	installerVols["vol0"].Structure[0].Filesystem = "ext44"
	mergedVols, err = gadget.ApplyInstallerVolumesToGadget(installerVols, gVols)
	c.Assert(err.Error(), Equals, `finalized volume "vol0" is wrong: invalid structure #0 ("ubuntu-seed"): invalid filesystem "ext44"`)
	c.Assert(mergedVols, IsNil)
}

func (s *gadgetYamlTestSuite) TestApplyInstallerVolumesToGadgetPartialSize(c *C) {
	var yaml = []byte(`
volumes:
  vol0:
    partial: [size]
    bootloader: u-boot
    schema: gpt
    structure:
      - name: ubuntu-seed
        filesystem: ext4
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-seed
      - name: ubuntu-boot
        filesystem: ext4
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-boot
      - name: ubuntu-save
        min-size: 1M
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-save
      - name: ubuntu-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-data
`)
	err := os.WriteFile(s.gadgetYamlPath, yaml, 0644)
	c.Assert(err, IsNil)

	installerVols := map[string]*gadget.Volume{
		"vol0": {
			Name:   "vol0",
			Schema: "gpt",
			Structure: []gadget.VolumeStructure{
				{
					Name: "ubuntu-seed",
				},
				{
					Name: "ubuntu-boot",
				},
				{
					Name:   "ubuntu-save",
					Offset: asOffsetPtr(1001 * quantity.OffsetMiB),
					Size:   2 * quantity.SizeMiB,
				},
				{
					Name:   "ubuntu-data",
					Offset: asOffsetPtr(1003 * quantity.OffsetMiB),
					Size:   2000 * quantity.SizeMiB,
				},
			},
		},
	}

	gVols := s.readGadgetVols(c)
	mergedVols, err := gadget.ApplyInstallerVolumesToGadget(installerVols, gVols)
	c.Assert(err, IsNil)
	c.Assert(*mergedVols["vol0"].Structure[2].Offset, Equals, 1001*quantity.OffsetMiB)
	c.Assert(*mergedVols["vol0"].Structure[3].Offset, Equals, 1003*quantity.OffsetMiB)
	c.Assert(mergedVols["vol0"].Structure[2].Size, Equals, 2*quantity.SizeMiB)
	c.Assert(mergedVols["vol0"].Structure[3].Size, Equals, 2000*quantity.SizeMiB)

	installerVols["vol0"].Structure[2].Offset = nil
	mergedVols, err = gadget.ApplyInstallerVolumesToGadget(installerVols, gVols)
	c.Assert(err.Error(), Equals, `installer did not provide offset for structure "ubuntu-save" in volume "vol0"`)
	c.Assert(mergedVols, IsNil)

	installerVols["vol0"].Structure[2].Offset = asOffsetPtr(1001 * quantity.OffsetMiB)
	installerVols["vol0"].Structure[2].Size = 0
	mergedVols, err = gadget.ApplyInstallerVolumesToGadget(installerVols, gVols)
	c.Assert(err.Error(), Equals, `installer did not provide size for structure "ubuntu-save" in volume "vol0"`)
	c.Assert(mergedVols, IsNil)

	installerVols["vol0"].Structure[2].Size = 500 * quantity.SizeKiB
	mergedVols, err = gadget.ApplyInstallerVolumesToGadget(installerVols, gVols)
	c.Assert(err.Error(), Equals, `finalized volume "vol0" is wrong: invalid structure #2 ("ubuntu-save"): min-size (1048576) is bigger than size (512000)`)
	c.Assert(mergedVols, IsNil)
}

func (s *gadgetYamlTestSuite) TestApplyInstallerVolumesToGadgetBadInstallerVol(c *C) {
	var yaml = []byte(`
volumes:
  vol0:
    partial: [filesystem]
    bootloader: u-boot
    structure:
      - name: ubuntu-seed
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-seed
      - name: ubuntu-boot
        filesystem: ext4
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-boot
      - name: ubuntu-save
        size: 1M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-save
      - name: ubuntu-data
        size: 1000M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-data
`)
	err := os.WriteFile(s.gadgetYamlPath, yaml, 0644)
	c.Assert(err, IsNil)

	installerVols := map[string]*gadget.Volume{
		"foo": {
			Name:   "foo",
			Schema: "gpt",
		},
	}
	gVols := s.readGadgetVols(c)
	mergedVols, err := gadget.ApplyInstallerVolumesToGadget(installerVols, gVols)
	c.Assert(err.Error(), Equals, `installer did not provide information for volume "vol0"`)
	c.Assert(mergedVols, IsNil)

	installerVols = map[string]*gadget.Volume{
		"vol0": {
			Name:   "vol0",
			Schema: "gpt",
		},
	}
	mergedVols, err = gadget.ApplyInstallerVolumesToGadget(installerVols, gVols)
	c.Assert(err.Error(), Equals, `cannot find structure "ubuntu-seed"`)
	c.Assert(mergedVols, IsNil)
}
