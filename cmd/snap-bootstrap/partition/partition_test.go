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
package partition_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/partition"
	"github.com/snapcore/snapd/gadget"
)

func TestPartition(t *testing.T) { TestingT(t) }

type partitionTestSuite struct{}

var _ = Suite(&partitionTestSuite{})

var mockDeviceStructureBiosBoot = partition.DeviceStructure{
	Node: "/dev/node1",
	LaidOutStructure: gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name: "BIOS Boot",
			Size: 1 * 1024 * 1024,
			Type: "DA,21686148-6449-6E6F-744E-656564454649",
			Content: []gadget.VolumeContent{
				{
					Image: "pc-core.img",
				},
			},
		},
		StartOffset: 0,
		Index:       1,
	},
}

var mockDeviceStructureSystemSeed = partition.DeviceStructure{
	Node: "/dev/node2",
	LaidOutStructure: gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "Recovery",
			Size:       1258291200,
			Type:       "EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			Role:       "system-seed",
			Filesystem: "vfat",
			Content: []gadget.VolumeContent{
				{
					Source: "grubx64.efi",
					Target: "EFI/boot/grubx64.efi",
				},
			},
		},
		StartOffset: 2097152,
		Index:       2,
	},
}

var mockDeviceStructureWritable = partition.DeviceStructure{
	Node: "/dev/node3",
	LaidOutStructure: gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "Writable",
			Size:       1258291200,
			Type:       "83,0FC63DAF-8483-4772-8E79-3D69D8477DE4",
			Role:       "system-data",
			Filesystem: "ext4",
		},
		StartOffset: 1260388352,
		Index:       3,
	},
}

func makeMockGadget(gadgetRoot, gadgetContent string) error {
	if err := os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetContent), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "pc-boot.img"), []byte("pc-boot.img content"), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "pc-core.img"), []byte("pc-core.img content"), 0644); err != nil {
		return err
	}
	if err := ioutil.WriteFile(filepath.Join(gadgetRoot, "grubx64.efi"), []byte("grubx64.efi content"), 0644); err != nil {
		return err
	}

	return nil
}
