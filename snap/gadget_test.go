// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package snap_test

import (
	"io/ioutil"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

type gadgetYamlTestSuite struct {
}

var _ = Suite(&gadgetYamlTestSuite{})

var mockGadgetSnapYaml = `
name: canonical-pc
type: gadget
`

var mockGadgetYaml = []byte(`
bootloader: u-boot
volumes:
  volumename:
    schema: mbr
    id:     id,guid
    structure:
      - label: system-boot
        offset: 12345
        offset-write: 777
        size: 88888
        type: id,guid
        id:   id,guid
        filesystem: vfat
        content:
          - source: subdir/
            target: /
            unpack: false
          - image: foo.img
            offset: 4321
            offset-write: 8888
            size: 88888
            unpack: false
`)

func (s *gadgetYamlTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *gadgetYamlTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlMissing(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	_, err := snap.ReadGadgetInfo(info)
	c.Assert(err, ErrorMatches, ".*meta/gadget.yaml: no such file or directory")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlValid(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := snap.ReadGadgetInfo(info)
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &snap.GadgetInfo{
		Bootloader: "u-boot",
		Volumes: map[string]snap.Volume{
			"volumename": snap.Volume{
				Schema: "mbr",
				ID:     "id,guid",
				Structure: []snap.Structure{
					{
						Label:       "system-boot",
						Offset:      12345,
						OffsetWrite: 777,
						Size:        88888,
						Type:        "id,guid",
						ID:          "id,guid",
						Filesystem:  "vfat",
						Content: []snap.Content{
							{
								Source: "subdir/",
								Target: "/",
								Unpack: false,
							},
							{
								Image:       "foo.img",
								Offset:      4321,
								OffsetWrite: 8888,
								Size:        88888,
								Unpack:      false,
							},
						},
					},
				},
			},
		},
	})
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlEmptydBootloader(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	mockGadgetYamlBroken := []byte(`
bootloader: 
`)

	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockGadgetYamlBroken, 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadGadgetInfo(info)
	c.Assert(err, ErrorMatches, "cannot read gadget snap details: bootloader cannot be empty")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlInvalidBootloader(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	mockGadgetYamlBroken := []byte(`
bootloader: silo
`)

	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockGadgetYamlBroken, 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadGadgetInfo(info)
	c.Assert(err, ErrorMatches, "cannot read gadget snap details: bootloader must be either grub or u-boot")
}
