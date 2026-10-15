// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) Canonical Ltd
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
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/testutil"
)

type gadgetYamlUFSSuite struct {
	testutil.BaseTest

	dir            string
	gadgetYamlPath string
}

var _ = Suite(&gadgetYamlUFSSuite{})

var mockUFSGadgetYaml = []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
    id:     0C
    structure:
      - filesystem-label: system-boot
        offset: 12345
        offset-write: 777
        size: 88888
        type: 0C
        filesystem: vfat
        content:
          - source: subdir/
            target: /
            unpack: false
          - source: foo
            target: /
  my-ufs:
    schema: ufs
    structure:
      - name: boot-lu-a
        content:
          - image: bootlu-a-filename
      - name: boot-lu-b
        content:
          - image: bootlu-b-filename
volume-assignments:
  - assignment-name: ufs-device
    assignment:
      volumename:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
      my-ufs:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
`)

func (s *gadgetYamlUFSSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.dir = c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(s.dir, "meta"), 0755), IsNil)
	s.gadgetYamlPath = filepath.Join(s.dir, "meta", "gadget.yaml")
}

func (s *gadgetYamlUFSSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *gadgetYamlUFSSuite) TestReadGadgetYamlUFSNoID(c *C) {
	err := os.WriteFile(s.gadgetYamlPath, []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
  my-ufs:
    schema: ufs
    id: test
volume-assignments:
  - assignment-name: ufs-device
    assignment:
      volumename:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
      my-ufs:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
`), 0644)
	c.Assert(err, IsNil)

	info, err := gadget.ReadInfo(s.dir, coreMod)
	c.Assert(err, IsNil)

	err = gadget.Validate(info, nil, nil)
	c.Assert(err, ErrorMatches, `invalid volume "my-ufs": cannot set "id" for UFS schemas`)
}

func (s *gadgetYamlUFSSuite) TestReadGadgetYamlUFSNoBootloader(c *C) {
	err := os.WriteFile(s.gadgetYamlPath, []byte(`
volumes:
  volumename:
    schema: mbr
  my-ufs:
    schema: ufs
    bootloader: u-boot
volume-assignments:
  - assignment-name: ufs-device
    assignment:
      volumename:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
      my-ufs:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
`), 0644)
	c.Assert(err, IsNil)

	info, err := gadget.ReadInfo(s.dir, coreMod)
	c.Assert(err, IsNil)

	err = gadget.Validate(info, nil, nil)
	c.Assert(err, ErrorMatches, `invalid volume "my-ufs": cannot set "bootloader" for UFS schemas`)
}

func (s *gadgetYamlUFSSuite) TestReadGadgetYamlUFSNoPartial(c *C) {
	err := os.WriteFile(s.gadgetYamlPath, []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
  my-ufs:
    schema: ufs
    partial: [size]
volume-assignments:
  - assignment-name: ufs-device
    assignment:
      volumename:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
      my-ufs:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
`), 0644)
	c.Assert(err, IsNil)

	info, err := gadget.ReadInfo(s.dir, coreMod)
	c.Assert(err, IsNil)

	err = gadget.Validate(info, nil, nil)
	c.Assert(err, ErrorMatches, `invalid volume "my-ufs": cannot set "partial" content for UFS schemas`)
}

func (s *gadgetYamlUFSSuite) TestReadGadgetYamlOffsetNotSupportedForBoot(c *C) {
	for _, t := range []string{"boot-lu-a", "boot-lu-b"} {
		err := os.WriteFile(s.gadgetYamlPath, []byte(fmt.Sprintf(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
    id:     0C
    structure:
      - filesystem-label: system-boot
        offset: 12345
        offset-write: 777
        size: 88888
        type: 0C
        filesystem: vfat
        content:
          - source: subdir/
            target: /
            unpack: false
  my-ufs:
    schema: ufs
    structure:
      - name: %s
        content:
          - image: bootlu-a-filename
            offset: 1000
volume-assignments:
  - assignment-name: ufs-device
    assignment:
      volumename:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
      my-ufs:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
`, t)), 0644)
		c.Assert(err, IsNil)

		_, err = gadget.ReadInfo(s.dir, coreMod)
		c.Assert(err, ErrorMatches, `.*cannot specify size or offset for content`)
	}
}

func (s *gadgetYamlUFSSuite) TestReadGadgetYamlSourceIsNotSupported(c *C) {
	for _, t := range []string{"boot-lu-a", "boot-lu-b"} {
		err := os.WriteFile(s.gadgetYamlPath, []byte(fmt.Sprintf(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
    id:     0C
    structure:
      - filesystem-label: system-boot
        offset: 12345
        offset-write: 777
        size: 88888
        type: 0C
        filesystem: vfat
        content:
          - source: subdir/
            target: /
            unpack: false
  my-ufs:
    schema: ufs
    structure:
      - name: %s
        content:
          - source: hello.bin
volume-assignments:
  - assignment-name: ufs-device
    assignment:
      volumename:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
      my-ufs:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
`, t)), 0644)
		c.Assert(err, IsNil)

		_, err = gadget.ReadInfo(s.dir, coreMod)
		c.Assert(err, ErrorMatches, `.*cannot use non-image content for hardware partitions`)
	}
}

func (s *gadgetYamlUFSSuite) TestReadGadgetYamlImageMustBeSet(c *C) {
	for _, t := range []string{"boot-lu-a", "boot-lu-b"} {
		err := os.WriteFile(s.gadgetYamlPath, []byte(fmt.Sprintf(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
    id:     0C
    structure:
      - filesystem-label: system-boot
        offset: 12345
        offset-write: 777
        size: 88888
        type: 0C
        filesystem: vfat
        content:
          - source: subdir/
            target: /
            unpack: false
  my-ufs:
    schema: ufs
    structure:
      - name: %s
        content:
          - unpack: true
volume-assignments:
  - assignment-name: ufs-device
    assignment:
      volumename:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
      my-ufs:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
`, t)), 0644)
		c.Assert(err, IsNil)

		_, err = gadget.ReadInfo(s.dir, coreMod)
		c.Assert(err, ErrorMatches, `.*missing image file name`)
	}
}

func (s *gadgetYamlUFSSuite) TestReadGadgetYamlHappy(c *C) {
	err := os.WriteFile(s.gadgetYamlPath, mockUFSGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, coreMod)
	c.Assert(err, IsNil)
	expected := &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"volumename": {
				Name:       "volumename",
				Schema:     "mbr",
				Bootloader: "u-boot",
				ID:         "0C",
				Structure: []gadget.VolumeStructure{
					{
						VolumeName:  "volumename",
						Label:       "system-boot",
						Role:        "system-boot", // implicit
						Offset:      asOffsetPtr(12345),
						OffsetWrite: mustParseGadgetRelativeOffset(c, "777"),
						Size:        88888,
						MinSize:     88888,
						Type:        "0C",
						Filesystem:  "vfat",
						Content: []gadget.VolumeContent{
							{
								UnresolvedSource: "subdir/",
								Target:           "/",
								Unpack:           false,
							},
							{
								UnresolvedSource: "foo",
								Target:           "/",
								Unpack:           false,
							},
						},
					},
				},
			},
			"my-ufs": {
				Name:   "my-ufs",
				Schema: "ufs",
				Structure: []gadget.VolumeStructure{
					{
						VolumeName: "my-ufs",
						Name:       "boot-lu-a",
						Offset:     asOffsetPtr(0),
						Content: []gadget.VolumeContent{
							{
								Image: "bootlu-a-filename",
							},
						},
						YamlIndex: 0,
					}, {
						VolumeName: "my-ufs",
						Name:       "boot-lu-b",
						Offset:     asOffsetPtr(0),
						Content: []gadget.VolumeContent{
							{
								Image: "bootlu-b-filename",
							},
						},
						YamlIndex: 1,
					},
				},
			},
		},
		VolumeAssignments: []*gadget.VolumeAssignment{
			{
				Name: "ufs-device",
				Assignments: map[string]*gadget.DeviceAssignment{
					"volumename": {Device: "/dev/disk/by-path/platform-ufshci-scsi-0:0:0:2"},
					"my-ufs":     {Device: "/dev/disk/by-path/platform-ufshci-scsi-0:0:0:2"},
				},
			},
		},
	}
	gadget.SetEnclosingVolumeInStructs(expected.Volumes)

	c.Check(ginfo, DeepEquals, expected)
}

func (s *gadgetYamlUFSSuite) TestUpdateApplyHappy(c *C) {
	err := os.WriteFile(s.gadgetYamlPath, mockUFSGadgetYaml, 0644)
	c.Assert(err, IsNil)
	// the assigned device must exist for the volume-assignment to match
	byPathDir := filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-path")
	c.Assert(os.MkdirAll(byPathDir, 0755), IsNil)
	c.Assert(os.WriteFile(filepath.Join(byPathDir, "platform-ufshci-scsi-0:0:0:2"), nil, 0644), IsNil)

	oldInfo, err := gadget.ReadInfo(s.dir, coreMod)
	c.Assert(err, IsNil)
	oldRootDir := c.MkDir()
	makeSizedFile(c, filepath.Join(oldRootDir, "bootlu-a-filename"), 1*quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(oldRootDir, "bootlu-b-filename"), 1*quantity.SizeMiB, nil)
	oldData := gadget.GadgetData{Info: oldInfo, RootDir: oldRootDir}

	newInfo, err := gadget.ReadInfo(s.dir, coreMod)
	c.Assert(err, IsNil)
	// pretend we have an update
	newInfo.Volumes["my-ufs"].Structure[1].Update.Edition = 1

	newRootDir := c.MkDir()
	makeSizedFile(c, filepath.Join(newRootDir, "bootlu-a-filename"), 1*quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(newRootDir, "bootlu-b-filename"), 2*quantity.SizeMiB, nil)
	newData := gadget.GadgetData{Info: newInfo, RootDir: newRootDir}

	rollbackDir := c.MkDir()

	restore := gadget.MockVolumeStructureToLocationMap(func(_ gadget.Model, _, newVolumes map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"volumename": {
					0: {
						Device:         "/dev/sdc",
						Offset:         quantity.OffsetMiB,
						RootMountPoint: "/run/mnt/ubuntu-boot",
					},
				},
				"my-ufs": {
					0: {
						Device: "/dev/sda",
					},
					1: {
						Device: "/dev/sdb",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"volumename": gadget.OnDiskStructsFromGadget(newVolumes["volumename"]),
				"my-ufs":     gadget.OnDiskStructsFromGadget(newVolumes["my-ufs"]),
			}, nil
	})
	defer restore()

	muo := &mockUpdateProcessObserver{}
	updaterForStructureCalls := 0
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, fromPs, ps *gadget.LaidOutStructure, rootDir, rollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		updaterForStructureCalls++
		mu := &mockUpdater{}

		return mu, nil
	})
	defer restore()

	// go go go
	err = gadget.Update(uc16Model, oldData, newData, rollbackDir, nil, muo)
	c.Assert(err, IsNil)
	c.Assert(updaterForStructureCalls, Equals, 1)
}

func (s *gadgetYamlUFSSuite) TestReadGadgetYamlUFSRequiresAssignment(c *C) {
	const volumes = `
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
  my-ufs:
    schema: ufs
    structure:
      - name: boot-lu-a
        content:
          - image: bootlu-a-filename
`
	for _, tc := range []struct {
		assignments string
		err         string
	}{{
		assignments: "",
		err:         `volume "my-ufs" with schema "ufs" requires volume-assignments`,
	}, {
		// a variant for a device without UFS can leave the UFS volume out
		assignments: `
volume-assignments:
  - assignment-name: emmc-device
    assignment:
      volumename:
        device: /dev/disk/by-path/platform-11230000.mmc
  - assignment-name: ufs-device
    assignment:
      volumename:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
      my-ufs:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
`,
	}, {
		assignments: `
volume-assignments:
  - assignment-name: ufs-device
    assignment:
      volumename:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
      my-ufs:
        device: /dev/disk/by-path/platform-ufshci-scsi-0:0:0:2
`,
	}} {
		err := os.WriteFile(s.gadgetYamlPath, []byte(volumes+tc.assignments), 0644)
		c.Assert(err, IsNil)

		_, err = gadget.ReadInfo(s.dir, coreMod)
		if tc.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, tc.err)
		}
	}
}
