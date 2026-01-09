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

type gadgetYamlEMMCSuite struct {
	testutil.BaseTest

	dir            string
	gadgetYamlPath string
}

var _ = Suite(&gadgetYamlEMMCSuite{})

var mockEMMCGadgetYaml = []byte(`
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
  my-emmc:
    schema: emmc
    structure:
      - name: boot0
        content:
          - image: boot0filename
      - name: boot1
        content:
          - image: boot1filename
`)

func (s *gadgetYamlEMMCSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)

	dirs.SetRootDir(c.MkDir())
	s.dir = c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(s.dir, "meta"), 0o755), IsNil)
	s.gadgetYamlPath = filepath.Join(s.dir, "meta", "gadget.yaml")
}

func (s *gadgetYamlEMMCSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *gadgetYamlEMMCSuite) TestReadGadgetYamlEMMCNoID(c *C) {
	err := os.WriteFile(s.gadgetYamlPath, []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
  my-emmc:
    schema: emmc
    id: test
`), 0644)
	c.Assert(err, IsNil)

	info, err := gadget.ReadInfo(s.dir, coreMod)
	c.Assert(err, IsNil)

	err = gadget.Validate(info, nil, nil)
	c.Assert(err, ErrorMatches, `invalid volume "my-emmc": cannot set "id" for eMMC schemas`)
}

func (s *gadgetYamlEMMCSuite) TestReadGadgetYamlEMMCNoBootloader(c *C) {
	err := os.WriteFile(s.gadgetYamlPath, []byte(`
volumes:
  volumename:
    schema: mbr
  my-emmc:
    schema: emmc
    bootloader: u-boot
`), 0644)
	c.Assert(err, IsNil)

	info, err := gadget.ReadInfo(s.dir, coreMod)
	c.Assert(err, IsNil)

	err = gadget.Validate(info, nil, nil)
	c.Assert(err, ErrorMatches, `invalid volume "my-emmc": cannot set "bootloader" for eMMC schemas`)
}

func (s *gadgetYamlEMMCSuite) TestReadGadgetYamlEMMCNoPartial(c *C) {
	err := os.WriteFile(s.gadgetYamlPath, []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
  my-emmc:
    schema: emmc
    partial: [size]
`), 0644)
	c.Assert(err, IsNil)

	info, err := gadget.ReadInfo(s.dir, coreMod)
	c.Assert(err, IsNil)

	err = gadget.Validate(info, nil, nil)
	c.Assert(err, ErrorMatches, `invalid volume "my-emmc": cannot set "partial" content for eMMC schemas`)
}

func (s *gadgetYamlEMMCSuite) TestReadGadgetYamlOffsetNotSupportedForBoot(c *C) {
	for _, t := range []string{"boot0", "boot1"} {
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
  my-emmc:
    schema: emmc
    structure:
      - name: %s
        content:
          - image: boot0filename
            offset: 1000
`, t)), 0644)
		c.Assert(err, IsNil)

		_, err = gadget.ReadInfo(s.dir, coreMod)
		c.Assert(err, ErrorMatches, `.*cannot specify size or offset for content`)
	}
}

func (s *gadgetYamlEMMCSuite) TestReadGadgetYamlSourceIsNotSupported(c *C) {
	for _, t := range []string{"boot0", "boot1"} {
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
  my-emmc:
    schema: emmc
    structure:
      - name: %s
        content:
          - source: hello.bin
`, t)), 0644)
		c.Assert(err, IsNil)

		_, err = gadget.ReadInfo(s.dir, coreMod)
		c.Assert(err, ErrorMatches, `.*cannot use non-image content for hardware partitions`)
	}
}

func (s *gadgetYamlEMMCSuite) TestReadGadgetYamlImageMustBeSet(c *C) {
	for _, t := range []string{"boot0", "boot1"} {
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
  my-emmc:
    schema: emmc
    structure:
      - name: %s
        content:
          - unpack: true
`, t)), 0644)
		c.Assert(err, IsNil)

		_, err = gadget.ReadInfo(s.dir, coreMod)
		c.Assert(err, ErrorMatches, `.*missing image file name`)
	}
}

func (s *gadgetYamlEMMCSuite) TestReadGadgetYamlHappy(c *C) {
	err := os.WriteFile(s.gadgetYamlPath, mockEMMCGadgetYaml, 0o644)
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
			"my-emmc": {
				Name:   "my-emmc",
				Schema: "emmc",
				Structure: []gadget.VolumeStructure{
					{
						VolumeName: "my-emmc",
						Name:       "boot0",
						Offset:     asOffsetPtr(0),
						Content: []gadget.VolumeContent{
							{
								Image: "boot0filename",
							},
						},
						YamlIndex: 0,
					}, {
						VolumeName: "my-emmc",
						Name:       "boot1",
						Offset:     asOffsetPtr(0),
						Content: []gadget.VolumeContent{
							{
								Image: "boot1filename",
							},
						},
						YamlIndex: 1,
					},
				},
			},
		},
	}
	gadget.SetEnclosingVolumeInStructs(expected.Volumes)

	c.Check(ginfo, DeepEquals, expected)
}

func (s *gadgetYamlEMMCSuite) TestUpdateApplyHappy(c *C) {
	err := os.WriteFile(s.gadgetYamlPath, mockEMMCGadgetYaml, 0o644)
	c.Assert(err, IsNil)

	oldInfo, err := gadget.ReadInfo(s.dir, coreMod)
	c.Assert(err, IsNil)
	oldRootDir := c.MkDir()
	makeSizedFile(c, filepath.Join(oldRootDir, "boot0filename"), 1*quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(oldRootDir, "boot1filename"), 1*quantity.SizeMiB, nil)
	oldData := gadget.GadgetData{Info: oldInfo, RootDir: oldRootDir}

	newInfo, err := gadget.ReadInfo(s.dir, coreMod)
	c.Assert(err, IsNil)
	// pretend we have an update
	newInfo.Volumes["my-emmc"].Structure[1].Update.Edition = 1

	newRootDir := c.MkDir()
	makeSizedFile(c, filepath.Join(newRootDir, "boot0filename"), 1*quantity.SizeMiB, nil)
	makeSizedFile(c, filepath.Join(newRootDir, "boot1filename"), 2*quantity.SizeMiB, nil)
	newData := gadget.GadgetData{Info: newInfo, RootDir: newRootDir}

	rollbackDir := c.MkDir()

	restore := gadget.MockVolumeStructureToLocationMap(func(_ gadget.Model, _, newVolumes map[string]*gadget.Volume) (map[string]map[int]gadget.StructureLocation, map[string]map[int]*gadget.OnDiskStructure, error) {
		return map[string]map[int]gadget.StructureLocation{
				"volumename": {
					0: {
						Device:         "/dev/emmcblk0",
						Offset:         quantity.OffsetMiB,
						RootMountPoint: "/run/mnt/ubuntu-boot",
					},
				},
				"my-emmc": {
					0: {
						Device: "/dev/emmcblk0boot0",
					},
					1: {
						Device: "/dev/emmcblk0boot1",
					},
				},
			}, map[string]map[int]*gadget.OnDiskStructure{
				"volumename": gadget.OnDiskStructsFromGadget(newVolumes["volumename"]),
				"my-emmc":    gadget.OnDiskStructsFromGadget(newVolumes["my-emmc"]),
			}, nil
	})
	defer restore()

	muo := &mockUpdateProcessObserver{}
	updaterForStructureCalls := 0
	restore = gadget.MockUpdaterForStructure(func(loc gadget.StructureLocation, fromPs, ps *gadget.LaidOutStructure, rootDir, rollbackDir string, observer gadget.ContentUpdateObserver) (gadget.Updater, error) {
		fmt.Println("update-for-structure", loc, ps, fromPs)
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
