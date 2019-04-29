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

package gadget_test

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
)

type gadgetYamlTestSuite struct {
	dir            string
	gadgetYamlPath string
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
`)

var mockMultiVolumeGadgetYaml = []byte(`
device-tree: frobinator-3000.dtb
device-tree-origin: kernel
volumes:
  frobinator-image:
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
  u-boot-frobinator:
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

var mockVolumeUpdateGadgetYaml = []byte(`
volumes:
  bootloader:
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
        update:
          edition: 5
          preserve:
           - env.txt
           - config.txt
`)

var gadgetYamlPC = []byte(`
volumes:
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
      - name: EFI System
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        filesystem: vfat
        filesystem-label: system-boot
        size: 50M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
          - source: grub.cfg
            target: EFI/ubuntu/grub.cfg
`)

var gadgetYamlRPi = []byte(`
device-tree: bcm2709-rpi-2-b
volumes:
  pi:
    schema: mbr
    bootloader: u-boot
    structure:
      - type: 0C
        filesystem: vfat
        filesystem-label: system-boot
        size: 128M
        content:
          - source: boot-assets/
            target: /
`)

func TestRun(t *testing.T) { TestingT(t) }

func mustParseGadgetSize(c *C, s string) gadget.Size {
	gs, err := gadget.ParseSize(s)
	c.Assert(err, IsNil)
	return gs
}

func mustParseGadgetRelativeOffset(c *C, s string) *gadget.RelativeOffset {
	grs, err := gadget.ParseRelativeOffset(s)
	c.Assert(err, IsNil)
	c.Assert(grs, NotNil)
	return grs
}

func (s *gadgetYamlTestSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.dir = c.MkDir()
	c.Assert(os.MkdirAll(filepath.Join(s.dir, "meta"), 0755), IsNil)
	s.gadgetYamlPath = filepath.Join(s.dir, "meta", "gadget.yaml")
}

func (s *gadgetYamlTestSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlMissing(c *C) {
	_, err := gadget.ReadInfo("bogus-path", false)
	c.Assert(err, ErrorMatches, ".*meta/gadget.yaml: no such file or directory")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicOptional(c *C) {
	// no meta/gadget.yaml
	gi, err := gadget.ReadInfo(s.dir, true)
	c.Assert(err, IsNil)
	c.Check(gi, NotNil)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicEmptyIsValid(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, nil, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, true)
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &gadget.Info{})
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicOnylDefaultsIsValid(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockClassicGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, true)
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &gadget.Info{
		Defaults: map[string]map[string]interface{}{
			"system": {"something": true},
			// keep this comment so that gofmt 1.10+ does not
			// realign this, thus breaking our gofmt 1.9 checks
			"otheridididididididididididididi": {"foo": map[string]interface{}{"bar": "baz"}},
		},
	})
}

func asSizePtr(size gadget.Size) *gadget.Size {
	gsz := gadget.Size(size)
	return &gsz
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlValid(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, false)
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &gadget.Info{
		Defaults: map[string]map[string]interface{}{
			"system": {"something": true},
		},
		Connections: []gadget.Connection{
			{Plug: gadget.ConnectionPlug{SnapID: "snapid1", Plug: "plg1"}, Slot: gadget.ConnectionSlot{SnapID: "snapid2", Slot: "slot"}},
			{Plug: gadget.ConnectionPlug{SnapID: "snapid3", Plug: "process-control"}, Slot: gadget.ConnectionSlot{SnapID: "system", Slot: "process-control"}},
			{Plug: gadget.ConnectionPlug{SnapID: "snapid4", Plug: "pctl4"}, Slot: gadget.ConnectionSlot{SnapID: "system", Slot: "process-control"}},
		},
		Volumes: map[string]gadget.Volume{
			"volumename": {
				Schema:     gadget.MBR,
				Bootloader: "u-boot",
				ID:         "0C",
				Structure: []gadget.VolumeStructure{
					{
						Label:       "system-boot",
						Offset:      asSizePtr(12345),
						OffsetWrite: mustParseGadgetRelativeOffset(c, "777"),
						Size:        88888,
						Type:        "0C",
						Filesystem:  "vfat",
						Content: []gadget.VolumeContent{
							{
								Source: "subdir/",
								Target: "/",
								Unpack: false,
							},
							{
								Source: "foo",
								Target: "/",
								Unpack: false,
							},
						},
					},
				},
			},
		},
	})
}

func (s *gadgetYamlTestSuite) TestReadMultiVolumeGadgetYamlValid(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockMultiVolumeGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, false)
	c.Assert(err, IsNil)
	c.Check(ginfo.Volumes, HasLen, 2)
	c.Assert(ginfo, DeepEquals, &gadget.Info{
		Volumes: map[string]gadget.Volume{
			"frobinator-image": {
				Schema:     gadget.MBR,
				Bootloader: "u-boot",
				Structure: []gadget.VolumeStructure{
					{
						Name:       "system-boot",
						Role:       "system-boot",
						Label:      "system-boot",
						Size:       mustParseGadgetSize(c, "128M"),
						Filesystem: "vfat",
						Type:       "0C",
						Content: []gadget.VolumeContent{
							{
								Source: "splash.bmp",
								Target: ".",
							},
						},
					},
					{
						Role:       "system-data",
						Name:       "writable",
						Label:      "writable",
						Type:       "83",
						Filesystem: "ext4",
						Size:       mustParseGadgetSize(c, "380M"),
					},
				},
			},
			"u-boot-frobinator": {
				Structure: []gadget.VolumeStructure{
					{
						Name:   "u-boot",
						Type:   "bare",
						Size:   623000,
						Offset: asSizePtr(0),
						Content: []gadget.VolumeContent{
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
	mockGadgetYamlBroken := []byte(`
volumes:
 name:
  bootloader: silo
`)

	err := ioutil.WriteFile(s.gadgetYamlPath, mockGadgetYamlBroken, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Assert(err, ErrorMatches, "bootloader must be one of grub, u-boot or android-boot")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlEmptyBootloader(c *C) {
	mockGadgetYamlBroken := []byte(`
volumes:
 name:
  bootloader:
`)

	err := ioutil.WriteFile(s.gadgetYamlPath, mockGadgetYamlBroken, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Assert(err, ErrorMatches, "bootloader not declared in any volume")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlMissingBootloader(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, nil, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Assert(err, ErrorMatches, "bootloader not declared in any volume")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlInvalidDefaultsKey(c *C) {
	mockGadgetYamlBroken := []byte(`
defaults:
 foo:
  x: 1
`)

	err := ioutil.WriteFile(s.gadgetYamlPath, mockGadgetYamlBroken, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Assert(err, ErrorMatches, `default stanza not keyed by "system" or snap-id: foo`)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlInvalidConnection(c *C) {
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

		err := ioutil.WriteFile(s.gadgetYamlPath, []byte(mockGadgetYamlBroken), 0644)
		c.Assert(err, IsNil)

		_, err = gadget.ReadInfo(s.dir, false)
		c.Check(err, ErrorMatches, t.expectedErr)
	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlVolumeUpdate(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockVolumeUpdateGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, false)
	c.Check(err, IsNil)
	c.Assert(ginfo, DeepEquals, &gadget.Info{
		Volumes: map[string]gadget.Volume{
			"bootloader": {
				Schema:     gadget.MBR,
				Bootloader: "u-boot",
				ID:         "0C",
				Structure: []gadget.VolumeStructure{
					{
						Label:       "system-boot",
						Offset:      asSizePtr(12345),
						OffsetWrite: mustParseGadgetRelativeOffset(c, "777"),
						Size:        88888,
						Type:        "0C",
						Filesystem:  "vfat",
						Content: []gadget.VolumeContent{{
							Source: "subdir/",
							Target: "/",
							Unpack: false,
						}},
						Update: gadget.VolumeUpdate{
							Edition: 5,
							Preserve: []string{
								"env.txt",
								"config.txt",
							},
						},
					},
				},
			},
		},
	})
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlVolumeUpdateUnhappy(c *C) {
	broken := bytes.Replace(mockVolumeUpdateGadgetYaml, []byte("edition: 5"), []byte("edition: borked"), 1)
	err := ioutil.WriteFile(s.gadgetYamlPath, broken, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Check(err, ErrorMatches, `cannot parse gadget metadata: "edition" must be a positive number, not "borked"`)

	broken = bytes.Replace(mockVolumeUpdateGadgetYaml, []byte("edition: 5"), []byte("edition: -5"), 1)
	err = ioutil.WriteFile(s.gadgetYamlPath, broken, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Check(err, ErrorMatches, `cannot parse gadget metadata: "edition" must be a positive number, not "-5"`)
}

func (s *gadgetYamlTestSuite) TestUnmarshalGadgetSize(c *C) {
	type foo struct {
		Size gadget.Size `yaml:"size"`
	}

	for i, tc := range []struct {
		s   string
		sz  gadget.Size
		err string
	}{
		{"1234", 1234, ""},
		{"1234M", 1234 * gadget.SizeMiB, ""},
		{"1234G", 1234 * gadget.SizeGiB, ""},
		{"0", 0, ""},
		{"a0M", 0, `cannot parse size "a0M": no numerical prefix.*`},
		{"-123", 0, `cannot parse size "-123": size cannot be negative`},
		{"123a", 0, `cannot parse size "123a": invalid suffix "a"`},
	} {
		c.Logf("tc: %v", i)

		var f foo
		err := yaml.Unmarshal([]byte(fmt.Sprintf("size: %s", tc.s)), &f)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
			c.Check(f.Size, Equals, tc.sz)
		}
	}
}

func (s *gadgetYamlTestSuite) TestUnmarshalGadgetRelativeOffset(c *C) {
	type foo struct {
		OffsetWrite gadget.RelativeOffset `yaml:"offset-write"`
	}

	for i, tc := range []struct {
		s   string
		sz  *gadget.RelativeOffset
		err string
	}{
		{"1234", &gadget.RelativeOffset{Offset: 1234}, ""},
		{"1234M", &gadget.RelativeOffset{Offset: 1234 * gadget.SizeMiB}, ""},
		{"4096M", &gadget.RelativeOffset{Offset: 4096 * gadget.SizeMiB}, ""},
		{"0", &gadget.RelativeOffset{}, ""},
		{"mbr+0", &gadget.RelativeOffset{RelativeTo: gadget.MBR}, ""},
		{"foo+1234M", &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1234 * gadget.SizeMiB}, ""},
		{"foo+1G", &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1 * gadget.SizeGiB}, ""},
		{"foo+1G", &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1 * gadget.SizeGiB}, ""},
		{"foo+4097M", nil, `cannot parse relative offset "foo\+4097M": offset above 4G limit`},
		{"foo+", nil, `cannot parse relative offset "foo\+": missing offset`},
		{"foo+++12", nil, `cannot parse relative offset "foo\+\+\+12": cannot parse offset "\+\+12": .*`},
		{"+12", nil, `cannot parse relative offset "\+12": missing volume name`},
		{"a0M", nil, `cannot parse relative offset "a0M": cannot parse offset "a0M": no numerical prefix.*`},
		{"-123", nil, `cannot parse relative offset "-123": cannot parse offset "-123": size cannot be negative`},
		{"123a", nil, `cannot parse relative offset "123a": cannot parse offset "123a": invalid suffix "a"`},
	} {
		c.Logf("tc: %v", i)

		var f foo
		err := yaml.Unmarshal([]byte(fmt.Sprintf("offset-write: %s", tc.s)), &f)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
			c.Assert(tc.sz, NotNil, Commentf("test case %v data must be not-nil", i))
			c.Check(f.OffsetWrite, Equals, *tc.sz)
		}
	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlPCHappy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlPC, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Assert(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlRPiHappy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlRPi, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Assert(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestValidateStructureType(c *C) {
	for i, tc := range []struct {
		s      string
		err    string
		schema string
	}{
		// legacy
		{"mbr", "", ""},
		// special case
		{"bare", "", ""},
		// plain MBR type
		{"0C", "", gadget.MBR},
		// GPT UUID
		{"21686148-6449-6E6F-744E-656564454649", "", gadget.GPT},
		// GPT UUID (lowercase)
		{"21686148-6449-6e6f-744e-656564454649", "", gadget.GPT},
		// hybrid ID
		{"EF,21686148-6449-6E6F-744E-656564454649", "", ""},
		// hybrid ID (UUID lowercase)
		{"EF,21686148-6449-6e6f-744e-656564454649", "", ""},
		// hybrid, partially lowercase UUID
		{"EF,aa686148-6449-6e6f-744E-656564454649", "", ""},
		// GPT UUID, partially lowercase
		{"aa686148-6449-6e6f-744E-656564454649", "", ""},
		// no type specified
		{"", `invalid type "": type is not specified`, ""},
		// plain MBR type without mbr schema
		{"0C", `invalid type "0C": MBR structure type with non-MBR schema ""`, ""},
		// GPT UUID with non GPT schema
		{"21686148-6449-6E6F-744E-656564454649", `invalid type "21686148-6449-6E6F-744E-656564454649": GUID structure type with non-GPT schema "mbr"`, gadget.MBR},
		// invalid
		{"1234", `invalid type "1234": invalid format`, ""},
		// outside of hex range
		{"FG", `invalid type "FG": invalid format`, ""},
		{"GG686148-6449-6E6F-744E-656564454649", `invalid type "GG686148-6449-6E6F-744E-656564454649": invalid format`, ""},
		// too long
		{"AA686148-6449-6E6F-744E-656564454649123", `invalid type "AA686148-6449-6E6F-744E-656564454649123": invalid format`, ""},
		// hybrid, missing MBR type
		{",AA686148-6449-6E6F-744E-656564454649", `invalid type ",AA686148-6449-6E6F-744E-656564454649": invalid format of hybrid type`, ""},
		// hybrid, missing GPT UUID
		{"EF,", `invalid type "EF,": invalid format of hybrid type`, ""},
		// hybrid, MBR type too long
		{"EFC,AA686148-6449-6E6F-744E-656564454649", `invalid type "EFC,AA686148-6449-6E6F-744E-656564454649": invalid format of hybrid type`, ""},
		// hybrid, GPT UUID too long
		{"EF,AAAA686148-6449-6E6F-744E-656564454649", `invalid type "EF,AAAA686148-6449-6E6F-744E-656564454649": invalid format of hybrid type`, ""},
		// GPT schema with non GPT type
		{"EF,AAAA686148-6449-6E6F-744E-656564454649", `invalid type "EF,AAAA686148-6449-6E6F-744E-656564454649": invalid format of hybrid type`, gadget.GPT},
	} {
		c.Logf("tc: %v %q", i, tc.s)

		err := gadget.ValidateVolumeStructure(&gadget.VolumeStructure{Type: tc.s, Size: 123}, &gadget.Volume{Schema: tc.schema})
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func mustParseStructure(c *C, s string) *gadget.VolumeStructure {
	var v gadget.VolumeStructure
	err := yaml.Unmarshal([]byte(s), &v)
	c.Assert(err, IsNil)
	return &v
}

func (s *gadgetYamlTestSuite) TestValidateRole(c *C) {
	uuidType := `
type: 21686148-6449-6E6F-744E-656564454649
size: 1023
`
	bareType := `
type: mbr
size: 446
`
	invalidSystemDataLabel := uuidType + `
role: system-data
filesystem-label: foobar
size: 123M
`
	mbrTooLarge := bareType + `
role: mbr
size: 467`
	mbrBadOffset := bareType + `
role: mbr
offset: 123`
	mbrBadID := bareType + `
role: mbr
id: 123
size: 446`
	mbrBadFilesystem := bareType + `
role: mbr
filesystem: vfat`
	mbrNoneFilesystem := `
type: bare
role: mbr
filesystem: none
size: 446`
	typeAsMBRTooLarge := `
type: mbr
size: 447`
	typeConflictsRole := `
type: bare
role: system-data
size: 1M`
	validSystemBoot := uuidType + `
role: system-boot
`
	emptyRole := uuidType + `
role: system-boot
size: 123M
`
	bogusRole := uuidType + `
role: foobar
size: 123M
`
	legacyMBR := `
type: mbr
size: 446`
	vol := &gadget.Volume{}
	mbrVol := &gadget.Volume{Schema: gadget.MBR}
	for i, tc := range []struct {
		s   *gadget.VolumeStructure
		v   *gadget.Volume
		err string
	}{
		{mustParseStructure(c, validSystemBoot), vol, ""},
		// empty, ok too
		{mustParseStructure(c, emptyRole), vol, ""},
		// invalid role name
		{mustParseStructure(c, bogusRole), vol, `invalid role "foobar": unsupported role`},
		// system-data, but improper label
		{mustParseStructure(c, invalidSystemDataLabel), vol, `invalid role "system-data": role of this kind must have an implicit label or "writable", not "foobar"`},
		// mbr
		{mustParseStructure(c, mbrTooLarge), mbrVol, `invalid role "mbr": mbr structures cannot be larger than 446 bytes`},
		{mustParseStructure(c, mbrBadOffset), mbrVol, `invalid role "mbr": mbr structure must start at offset 0`},
		{mustParseStructure(c, mbrBadID), mbrVol, `invalid role "mbr": mbr structure must not specify partition ID`},
		{mustParseStructure(c, mbrBadFilesystem), mbrVol, `invalid role "mbr": mbr structures must not specify a file system`},
		// filesystem: none is ok for MBR
		{mustParseStructure(c, mbrNoneFilesystem), mbrVol, ""},
		// legacy, type: mbr treated like role: mbr
		{mustParseStructure(c, legacyMBR), mbrVol, ""},
		{mustParseStructure(c, typeAsMBRTooLarge), mbrVol, `invalid implicit role "mbr": mbr structures cannot be larger than 446 bytes`},
		// conflicting type/role
		{mustParseStructure(c, typeConflictsRole), vol, `invalid role "system-data": conflicting type: "bare"`},
	} {
		c.Logf("tc: %v %+v", i, tc.s)

		err := gadget.ValidateVolumeStructure(tc.s, tc.v)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *gadgetYamlTestSuite) TestValidateFilesystem(c *C) {
	for i, tc := range []struct {
		s   string
		err string
	}{
		{"vfat", ""},
		{"ext4", ""},
		{"none", ""},
		{"btrfs", `invalid filesystem "btrfs"`},
	} {
		c.Logf("tc: %v %+v", i, tc.s)

		err := gadget.ValidateVolumeStructure(&gadget.VolumeStructure{Filesystem: tc.s, Type: "21686148-6449-6E6F-744E-656564454649", Size: 123}, &gadget.Volume{})
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *gadgetYamlTestSuite) TestValidateVolumeSchema(c *C) {
	for i, tc := range []struct {
		s   string
		err string
	}{
		{gadget.GPT, ""},
		{gadget.MBR, ""},
		// implicit GPT
		{"", ""},
		// invalid
		{"some", `invalid schema "some"`},
	} {
		c.Logf("tc: %v %+v", i, tc.s)

		err := gadget.ValidateVolume("name", &gadget.Volume{Schema: tc.s})
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *gadgetYamlTestSuite) TestValidateVolumeName(c *C) {

	for i, tc := range []struct {
		s   string
		err string
	}{
		{"valid", ""},
		{"still-valid", ""},
		{"123volume", ""},
		{"volume123", ""},
		{"PC", ""},
		{"PC123", ""},
		{"UPCASE", ""},
		// invalid
		{"-valid", "invalid name"},
		{"in+valid", "invalid name"},
		{"with whitespace", "invalid name"},
		{"", "invalid name"},
	} {
		c.Logf("tc: %v %+v", i, tc.s)

		err := gadget.ValidateVolume(tc.s, &gadget.Volume{})
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *gadgetYamlTestSuite) TestValidateVolumeDuplicateStructures(c *C) {
	err := gadget.ValidateVolume("name", &gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Name: "duplicate", Type: "bare", Size: 1024},
			{Name: "duplicate", Type: "21686148-6449-6E6F-744E-656564454649", Size: 2048},
		},
	})
	c.Assert(err, ErrorMatches, `structure name "duplicate" is not unique`)
}

func (s *gadgetYamlTestSuite) TestValidateVolumeErrorsWrapped(c *C) {
	err := gadget.ValidateVolume("name", &gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Type: "bare", Size: 1024},
			{Type: "bogus", Size: 1024},
		},
	})
	c.Assert(err, ErrorMatches, `invalid structure #1: invalid type "bogus": invalid format`)

	err = gadget.ValidateVolume("name", &gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Type: "bare", Size: 1024},
			{Type: "bogus", Size: 1024, Name: "foo"},
		},
	})
	c.Assert(err, ErrorMatches, `invalid structure #1 \("foo"\): invalid type "bogus": invalid format`)

	err = gadget.ValidateVolume("name", &gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Type: "bare", Name: "foo", Size: 1024, Content: []gadget.VolumeContent{{Source: "foo"}}},
		},
	})
	c.Assert(err, ErrorMatches, `invalid structure #0 \("foo"\): invalid content #0: cannot use non-image content for bare file system`)
}

func (s *gadgetYamlTestSuite) TestValidateStructureContent(c *C) {
	bareOnlyOk := `
type: bare
size: 1M
content:
  - image: foo.img
`
	bareMixed := `
type: bare
size: 1M
content:
  - image: foo.img
  - source: foo
    target: bar
`
	bareMissing := `
type: bare
size: 1M
content:
  - offset: 123
`
	fsOk := `
type: 21686148-6449-6E6F-744E-656564454649
filesystem: ext4
size: 1M
content:
  - source: foo
    target: bar
`
	fsMixed := `
type: 21686148-6449-6E6F-744E-656564454649
filesystem: ext4
size: 1M
content:
  - source: foo
    target: bar
  - image: foo.img
`
	fsMissing := `
type: 21686148-6449-6E6F-744E-656564454649
filesystem: ext4
size: 1M
content:
  - source: foo
`

	for i, tc := range []struct {
		s   *gadget.VolumeStructure
		v   *gadget.Volume
		err string
	}{
		{mustParseStructure(c, bareOnlyOk), nil, ""},
		{mustParseStructure(c, bareMixed), nil, `invalid content #1: cannot use non-image content for bare file system`},
		{mustParseStructure(c, bareMissing), nil, `invalid content #0: missing image file name`},
		{mustParseStructure(c, fsOk), nil, ""},
		{mustParseStructure(c, fsMixed), nil, `invalid content #1: cannot use image content for non-bare file system`},
		{mustParseStructure(c, fsMissing), nil, `invalid content #0: missing source or target`},
	} {
		c.Logf("tc: %v %+v", i, tc.s)

		err := gadget.ValidateVolumeStructure(tc.s, &gadget.Volume{})
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *gadgetYamlTestSuite) TestValidateStructureAndContentRelativeOffset(c *C) {
	gadgetYamlHeader := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: my-name-is
        type: mbr
        size: 440
        content:
          - image: pc-boot.img`

	gadgetYamlBadStructureName := gadgetYamlHeader + `
      - name: other-name
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: bad-name+92
        content:
          - image: pc-core.img
`
	gadgetYamlBadContentName := gadgetYamlHeader + `
      - name: other-name
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: my-name-is+92
        content:
          - image: pc-core.img
            offset-write: bad-name+123
`
	gadgetYamlBadOffsetWriteCrossingSize := gadgetYamlHeader + `
      - name: other-name
        type: bare
        size: 1M
        offset: 1M
        offset-write: my-name-is+438
        content:
          - image: pc-core.img
`
	gadgetYamlBadContentOffsetWriteCrossingSize := gadgetYamlHeader + `
      - name: other-name
        type: bare
        size: 1M
        offset: 1M
        content:
          - image: pc-core.img
            offset-write: my-name-is+438
`

	gadgetYamlHeaderSmall := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: too-small
        type: bare
        size: 3
`
	gadgetYamlBadContentOffsetWriteTooSmall := gadgetYamlHeaderSmall + `
      - name: other-name
        type: bare
        size: 1M
        offset: 1M
        content:
          - image: pc-core.img
            offset-write: too-small+0
`
	gadgetYamlBadOffsetWriteTooSmall := gadgetYamlHeaderSmall + `
      - name: other-name
        type: bare
        size: 1M
        offset: 1M
        offset-write: too-small+0
        content:
          - image: pc-core.img
`

	err := ioutil.WriteFile(s.gadgetYamlPath, []byte(gadgetYamlBadStructureName), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #1 \("other-name"\) refers to an unknown structure "bad-name"`)

	err = ioutil.WriteFile(s.gadgetYamlPath, []byte(gadgetYamlBadContentName), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #1 \("other-name"\), content #0 \("pc-core.img"\) refers to an unknown structure "bad-name"`)

	err = ioutil.WriteFile(s.gadgetYamlPath, []byte(gadgetYamlBadOffsetWriteCrossingSize), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #1 \("other-name"\) offset-write crosses structure #0 \("my-name-is"\) size`)

	err = ioutil.WriteFile(s.gadgetYamlPath, []byte(gadgetYamlBadOffsetWriteTooSmall), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #1 \("other-name"\) offset-write crosses structure #0 \("too-small"\) size`)

	err = ioutil.WriteFile(s.gadgetYamlPath, []byte(gadgetYamlBadContentOffsetWriteCrossingSize), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #1 \("other-name"\), content #0 \("pc-core.img"\) offset-write crosses structure "my-name-is" size`)

	err = ioutil.WriteFile(s.gadgetYamlPath, []byte(gadgetYamlBadContentOffsetWriteTooSmall), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #1 \("other-name"\), content #0 \("pc-core.img"\) offset-write crosses structure "too-small" size`)

}

func (s *gadgetYamlTestSuite) TestValidateStructureUpdatePreserveOnlyForFs(c *C) {
	gv := &gadget.Volume{}

	err := gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:   "bare",
		Update: gadget.VolumeUpdate{Preserve: []string{"foo"}},
		Size:   512,
	}, gv)
	c.Check(err, ErrorMatches, "preserving files during update is not supported for non-filesystem structures")

	err = gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:   "21686148-6449-6E6F-744E-656564454649",
		Update: gadget.VolumeUpdate{Preserve: []string{"foo"}},
		Size:   512,
	}, gv)
	c.Check(err, ErrorMatches, "preserving files during update is not supported for non-filesystem structures")

	err = gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:       "21686148-6449-6E6F-744E-656564454649",
		Filesystem: "vfat",
		Update:     gadget.VolumeUpdate{Preserve: []string{"foo"}},
		Size:       512,
	}, gv)
	c.Check(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestValidateStructureUpdatePreserveDuplicates(c *C) {
	gv := &gadget.Volume{}

	err := gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:       "21686148-6449-6E6F-744E-656564454649",
		Filesystem: "vfat",
		Update:     gadget.VolumeUpdate{Edition: 1, Preserve: []string{"foo", "bar"}},
		Size:       512,
	}, gv)
	c.Check(err, IsNil)

	err = gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:       "21686148-6449-6E6F-744E-656564454649",
		Filesystem: "vfat",
		Update:     gadget.VolumeUpdate{Edition: 1, Preserve: []string{"foo", "bar", "foo"}},
		Size:       512,
	}, gv)
	c.Check(err, ErrorMatches, `duplicate "preserve" entry "foo"`)
}

func (s *gadgetYamlTestSuite) TestValidateStructureSizeRequired(c *C) {

	gv := &gadget.Volume{}

	err := gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:   "bare",
		Update: gadget.VolumeUpdate{Preserve: []string{"foo"}},
	}, gv)
	c.Check(err, ErrorMatches, "missing size")

	err = gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:       "21686148-6449-6E6F-744E-656564454649",
		Filesystem: "vfat",
		Update:     gadget.VolumeUpdate{Preserve: []string{"foo"}},
	}, gv)
	c.Check(err, ErrorMatches, "missing size")

	err = gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:       "21686148-6449-6E6F-744E-656564454649",
		Filesystem: "vfat",
		Size:       mustParseGadgetSize(c, "123M"),
		Update:     gadget.VolumeUpdate{Preserve: []string{"foo"}},
	}, gv)
	c.Check(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestValidatePositioningOverlapPreceding(c *C) {
	overlappingGadgetYaml := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
        content:
          - image: pc-boot.img
      - name: other-name
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 200
        content:
          - image: pc-core.img
`
	err := ioutil.WriteFile(s.gadgetYamlPath, []byte(overlappingGadgetYaml), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #1 \("other-name"\) overlaps with the preceding structure #0 \("mbr"\)`)
}

func (s *gadgetYamlTestSuite) TestValidatePositioningOverlapOutOfOrder(c *C) {
	outOfOrderGadgetYaml := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: overlaps-with-foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 200
        content:
          - image: pc-core.img
      - name: foo
        type: DA,21686148-6449-6E6F-744E-656564454648
        size: 1M
        offset: 100
        filesystem: vfat
`
	err := ioutil.WriteFile(s.gadgetYamlPath, []byte(outOfOrderGadgetYaml), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #0 \("overlaps-with-foo"\) overlaps with the preceding structure #1 \("foo"\)`)
}

func (s *gadgetYamlTestSuite) TestValidateCrossStructureMBRFixedOffset(c *C) {
	gadgetYaml := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: other-name
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 500
        content:
          - image: pc-core.img
      - name: mbr
        type: mbr
        size: 440
        offset: 0
        content:
          - image: pc-boot.img
`
	err := ioutil.WriteFile(s.gadgetYamlPath, []byte(gadgetYaml), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Check(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestValidateCrossStructureMBRDefaultOffsetInvalid(c *C) {
	gadgetYaml := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: other-name
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 500
        content:
          - image: pc-core.img
      - name: mbr
        type: mbr
        size: 440
        content:
          - image: pc-boot.img
`
	err := ioutil.WriteFile(s.gadgetYamlPath, []byte(gadgetYaml), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, false)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #1 \("mbr"\) has "mbr" role and must start at offset 0`)
}
