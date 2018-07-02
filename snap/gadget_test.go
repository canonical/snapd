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
	"strings"

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
defaults:
  system:
    something: true

connections:
  - plug: snapid1:plg1
    slot: snapid2:slot
  - plug: snapid3:process-control
  - plug: snapid4:pctl4
    slot: system:process-control

volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
    id:     id,guid
    structure:
      - filesystem-label: system-boot
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

var mockMultiVolumeGadgetYaml = []byte(`
device-tree: frobinator-3000.dtb
device-tree-origin: kernel
volumes:
  frobinator-3000-image:
    bootloader: u-boot
    schema: mbr
    structure:
      - name: system-boot
        type: 0C
        filesystem: vfat
        filesystem-label: system-boot
        size: 128M
        role: system-boot
        content:
          - source: splash.bmp
            target: .
      - name: writable
        type: 83
        filesystem: ext4
        filesystem-label: writable
        size: 380M
        role: system-data
  u-boot-frobinator-3000:
    structure:
      - name: u-boot
        type: bare
        size: 623000
        offset: 0
        content:
          - image: u-boot.imz
`)

var mockClassicGadgetYaml = []byte(`
defaults:
  system:
    something: true
  otheridididididididididididididi:
    foo:
      bar: baz
`)

func (s *gadgetYamlTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
}

func (s *gadgetYamlTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *gadgetYamlTestSuite) TestReadGadgetNotAGadget(c *C) {
	info := snaptest.MockInfo(c, `
name: other
version: 0
`, &snap.SideInfo{Revision: snap.R(42)})
	_, err := snap.ReadGadgetInfo(info, false)
	c.Assert(err, ErrorMatches, "cannot read gadget snap details: not a gadget snap")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlMissing(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	_, err := snap.ReadGadgetInfo(info, false)
	c.Assert(err, ErrorMatches, ".*meta/gadget.yaml: no such file or directory")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicOptional(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	gi, err := snap.ReadGadgetInfo(info, true)
	c.Assert(err, IsNil)
	c.Check(gi, NotNil)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicEmptyIsValid(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), nil, 0644)
	c.Assert(err, IsNil)

	ginfo, err := snap.ReadGadgetInfo(info, true)
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &snap.GadgetInfo{})
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicOnylDefaultsIsValid(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockClassicGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := snap.ReadGadgetInfo(info, true)
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &snap.GadgetInfo{
		Defaults: map[string]map[string]interface{}{
			"system": {"something": true},
			"otheridididididididididididididi": {"foo": map[string]interface{}{"bar": "baz"}},
		},
	})
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlValid(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := snap.ReadGadgetInfo(info, false)
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &snap.GadgetInfo{
		Defaults: map[string]map[string]interface{}{
			"system": {"something": true},
		},
		Connections: []snap.GadgetConnection{
			{Plug: snap.GadgetConnectionPlug{SnapID: "snapid1", Plug: "plg1"}, Slot: snap.GadgetConnectionSlot{SnapID: "snapid2", Slot: "slot"}},
			{Plug: snap.GadgetConnectionPlug{SnapID: "snapid3", Plug: "process-control"}, Slot: snap.GadgetConnectionSlot{SnapID: "system", Slot: "process-control"}},
			{Plug: snap.GadgetConnectionPlug{SnapID: "snapid4", Plug: "pctl4"}, Slot: snap.GadgetConnectionSlot{SnapID: "system", Slot: "process-control"}},
		},
		Volumes: map[string]snap.GadgetVolume{
			"volumename": {
				Schema:     "mbr",
				Bootloader: "u-boot",
				ID:         "id,guid",
				Structure: []snap.VolumeStructure{
					{
						Label:       "system-boot",
						Offset:      "12345",
						OffsetWrite: "777",
						Size:        "88888",
						Type:        "id,guid",
						ID:          "id,guid",
						Filesystem:  "vfat",
						Content: []snap.VolumeContent{
							{
								Source: "subdir/",
								Target: "/",
								Unpack: false,
							},
							{
								Image:       "foo.img",
								Offset:      "4321",
								OffsetWrite: "8888",
								Size:        "88888",
								Unpack:      false,
							},
						},
					},
				},
			},
		},
	})
}

func (s *gadgetYamlTestSuite) TestReadMultiVolumeGadgetYamlValid(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockMultiVolumeGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := snap.ReadGadgetInfo(info, false)
	c.Assert(err, IsNil)
	c.Check(ginfo.Volumes, HasLen, 2)
	c.Assert(ginfo, DeepEquals, &snap.GadgetInfo{
		Volumes: map[string]snap.GadgetVolume{
			"frobinator-3000-image": {
				Schema:     "mbr",
				Bootloader: "u-boot",
				Structure: []snap.VolumeStructure{
					{
						Label:      "system-boot",
						Size:       "128M",
						Filesystem: "vfat",
						Type:       "0C",
						Content: []snap.VolumeContent{
							{
								Source: "splash.bmp",
								Target: ".",
							},
						},
					},
					{
						Label:      "writable",
						Type:       "83",
						Filesystem: "ext4",
						Size:       "380M",
					},
				},
			},
			"u-boot-frobinator-3000": {
				Structure: []snap.VolumeStructure{
					{
						Type:   "bare",
						Size:   "623000",
						Offset: "0",
						Content: []snap.VolumeContent{
							{
								Image: "u-boot.imz",
							},
						},
					},
				},
			},
		},
	})
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlInvalidBootloader(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	mockGadgetYamlBroken := []byte(`
volumes:
 name:
  bootloader: silo
`)

	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockGadgetYamlBroken, 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadGadgetInfo(info, false)
	c.Assert(err, ErrorMatches, "cannot read gadget snap details: bootloader must be one of grub, u-boot or android-boot")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlEmptydBootloader(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	mockGadgetYamlBroken := []byte(`
volumes:
 name:
  bootloader:
`)

	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockGadgetYamlBroken, 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadGadgetInfo(info, false)
	c.Assert(err, ErrorMatches, "cannot read gadget snap details: bootloader not declared in any volume")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlMissingBootloader(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})

	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), nil, 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadGadgetInfo(info, false)
	c.Assert(err, ErrorMatches, "cannot read gadget snap details: bootloader not declared in any volume")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlInvalidDefaultsKey(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	mockGadgetYamlBroken := []byte(`
defaults:
 foo:
  x: 1
`)

	err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockGadgetYamlBroken, 0644)
	c.Assert(err, IsNil)

	_, err = snap.ReadGadgetInfo(info, false)
	c.Assert(err, ErrorMatches, `default stanza not keyed by "system" or snap-id: foo`)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlInvalidConnection(c *C) {
	info := snaptest.MockSnap(c, mockGadgetSnapYaml, &snap.SideInfo{Revision: snap.R(42)})
	mockGadgetYamlBroken := `
connections:
 - @INVALID@
`
	tests := []struct {
		invalidConn string
		expectedErr string
	}{
		{``, `gadget connection plug cannot be empty`},
		{`foo:bar baz:quux`, `(?s).*unmarshal errors:.*`},
		{`plug: foo:`, `.*mapping values are not allowed in this context`},
		{`plug: ":"`, `.*in gadget connection plug: expected "\(<snap-id>\|system\):name" not ":"`},
		{`slot: "foo:"`, `.*in gadget connection slot: expected "\(<snap-id>\|system\):name" not "foo:"`},
		{`slot: foo:bar`, `gadget connection plug cannot be empty`},
	}

	for _, t := range tests {
		mockGadgetYamlBroken := strings.Replace(mockGadgetYamlBroken, "@INVALID@", t.invalidConn, 1)

		err := ioutil.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), []byte(mockGadgetYamlBroken), 0644)
		c.Assert(err, IsNil)

		_, err = snap.ReadGadgetInfo(info, false)
		c.Check(err, ErrorMatches, t.expectedErr)
	}
}
