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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
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

var mockClassicGadgetCoreDefaultsYaml = []byte(`
defaults:
  99T7MUlRhtI3U0QFgl5mXXESAiSwt776:
    ssh:
      disable: true
`)

var mockClassicGadgetMultilineDefaultsYaml = []byte(`
defaults:
  system:
    something: true
  otheridididididididididididididi:
    foosnap:
      multiline: |
        foo
        bar
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

var gadgetYamlUC20PC = []byte(`
volumes:
  pc:
    # bootloader configuration is shipped and managed by snapd
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
        update:
          edition: 1
        content:
          - image: pc-boot.img
      - name: BIOS Boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        offset-write: mbr+92
        update:
          edition: 2
        content:
          - image: pc-core.img
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
        update:
          edition: 2
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        # whats the appropriate size?
        size: 750M
        update:
          edition: 1
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
      - name: ubuntu-save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 16M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
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

var gadgetYamlLk = []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: lk
    structure:
      - name: BOOTIMG1
        size: 25165824
        role: system-boot-image
        type: 27
        content:
          - image: boot.img
      - name: BOOTIMG2
        size: 25165824
        role: system-boot-image
        type: 27
      - name: snapbootsel
        size: 131072
        role: system-boot-select
        type: B2
        content:
          - image: snapbootsel.bin
      - name: snapbootselbak
        size: 131072
        role: system-boot-select
        type: B2
        content:
          - image: snapbootsel.bin
      - name: writable
        type: 83
        filesystem: ext4
        filesystem-label: writable
        size: 500M
        role: system-data
`)

var gadgetYamlLkUC20 = []byte(`
device-tree-origin: kernel
volumes:
  dragonboard:
    schema: gpt
    bootloader: lk
    structure:
      - name: cdt
        offset: 17408
        size: 2048
        type: A19F205F-CCD8-4B6D-8F1E-2D9BC24CFFB1
        content:
            - image: blobs/sbc_1.0_8016.bin
      - name: sbl1
        offset: 19456
        size: 1048576
        content:
            - image: blobs/sbl1.mbn
        type: DEA0BA2C-CBDD-4805-B4F9-F428251C3E98
      - name: rpm
        offset: 1068032
        size: 1048576
        content:
            - image: blobs/rpm.mbn
        type: 098DF793-D712-413D-9D4E-89D711772228
      - name: tz
        offset: 2116608
        size: 1048576
        content:
            - image: blobs/tz.mbn
        type: A053AA7F-40B8-4B1C-BA08-2F68AC71A4F4
      - name: hyp
        offset: 3165184
        size: 1048576
        content:
            - image: blobs/hyp.mbn
        type: E1A6A689-0C8D-4CC6-B4E8-55A4320FBD8A
      - name: sec
        offset: 5242880
        size: 1048576
        type: 303E6AC3-AF15-4C54-9E9B-D9A8FBECF401
      - name: aboot
        offset: 6291456
        size: 2097152
        content:
            - image: blobs/emmc_appsboot.mbn
        type: 400FFDCD-22E0-47E7-9A23-F16ED9382388
      - name: snaprecoverysel
        offset: 8388608
        size: 131072
        passthrough:
          role: system-seed-select
        content:
            - image: snaprecoverysel.bin
        type: B214D5E4-D442-45E6-B8C6-01BDCD82D396
      - name: snaprecoveryselbak
        offset: 8519680
        size: 131072
        passthrough:
          role: system-seed-select
        content:
            - image: snaprecoverysel.bin
        type: B214D5E4-D442-45E6-B8C6-01BDCD82D396
      - name: snapbootsel
        offset: 8650752
        size: 131072
        role: system-boot-select
        content:
            - image: blobs/snapbootsel.bin
        type: B214D5E4-D442-45E6-B8C6-01BDCD82D396
      - name: snapbootselbak
        offset: 8781824
        size: 131072
        role: system-boot-select
        content:
            - image: blobs/snapbootsel.bin
        type: B214D5E4-D442-45E6-B8C6-01BDCD82D396
      - name: boot_ra
        offset: 9437184
        size: 31457280
        type: 20117F86-E985-4357-B9EE-374BC1D8487D
        passthrough:
          role: system-seed-image
      - name: boot_rb
        offset: 40894464
        size: 31457280
        type: 20117F86-E985-4357-B9EE-374BC1D8487D
        passthrough:
          role: system-seed-image
      - name: boot_a
        offset: 72351744
        size: 31457280
        type: 20117F86-E985-4357-B9EE-374BC1D8487D
        role: system-boot-image
      - name: boot_b
        offset: 103809024
        size: 31457280
        type: 20117F86-E985-4357-B9EE-374BC1D8487D
        role: system-boot-image
      - name: ubuntu-boot
        offset: 135266304
        filesystem: ext4
        size: 10485760
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-boot
      - name: ubuntu-seed
        offset: 145752064
        filesystem: ext4
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-seed
      - name: ubuntu-data
        offset: 670040064
        filesystem: ext4
        size: 1G
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-data
`)

var gadgetYamlLkLegacy = []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: lk
    structure:
      - name: BOOTIMG1
        size: 25165824
        role: bootimg
        type: 27
        content:
          - image: boot.img
      - name: BOOTIMG2
        size: 25165824
        role: bootimg
        type: 27
      - name: snapbootsel
        size: 131072
        role: bootselect
        type: B2
        content:
          - image: snapbootsel.bin
      - name: snapbootselbak
        size: 131072
        role: bootselect
        type: B2
        content:
          - image: snapbootsel.bin
      - name: writable
        type: 83
        filesystem: ext4
        filesystem-label: writable
        size: 500M
        role: system-data
`)

func TestRun(t *testing.T) { TestingT(t) }

func mustParseGadgetSize(c *C, s string) quantity.Size {
	gs, err := quantity.ParseSize(s)
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

type modelConstraints struct {
	classic    bool
	systemSeed bool
}

func (m *modelConstraints) Classic() bool {
	return m.classic
}

func (m *modelConstraints) Grade() asserts.ModelGrade {
	if m.systemSeed {
		return asserts.ModelSigned
	}
	return asserts.ModelGradeUnset
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlMissing(c *C) {
	// if constraints are nil, we allow a missing yaml
	_, err := gadget.ReadInfo("bogus-path", nil)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo("bogus-path", &modelConstraints{})
	c.Assert(err, ErrorMatches, ".*meta/gadget.yaml: no such file or directory")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicOptional(c *C) {
	// no meta/gadget.yaml
	gi, err := gadget.ReadInfo(s.dir, &modelConstraints{classic: true})
	c.Assert(err, IsNil)
	c.Check(gi, NotNil)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicEmptyIsValid(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, nil, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, &modelConstraints{classic: true})
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &gadget.Info{})
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicOnylDefaultsIsValid(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockClassicGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, &modelConstraints{classic: true})
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

func (s *gadgetYamlTestSuite) TestFlatten(c *C) {
	cfg := map[string]interface{}{
		"foo":         "bar",
		"some.option": true,
		"sub": map[string]interface{}{
			"option1": true,
			"option2": map[string]interface{}{
				"deep": "2",
			},
		},
	}
	out := map[string]interface{}{}
	gadget.Flatten("", cfg, out)
	c.Check(out, DeepEquals, map[string]interface{}{
		"foo":              "bar",
		"some.option":      true,
		"sub.option1":      true,
		"sub.option2.deep": "2",
	})
}

func (s *gadgetYamlTestSuite) TestCoreConfigDefaults(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockClassicGadgetCoreDefaultsYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, &modelConstraints{classic: true})
	c.Assert(err, IsNil)
	defaults := gadget.SystemDefaults(ginfo.Defaults)
	c.Check(defaults, DeepEquals, map[string]interface{}{
		"ssh.disable": true,
	})

	yaml := string(mockClassicGadgetCoreDefaultsYaml) + `
  system:
    something: true
`

	err = ioutil.WriteFile(s.gadgetYamlPath, []byte(yaml), 0644)
	c.Assert(err, IsNil)
	ginfo, err = gadget.ReadInfo(s.dir, &modelConstraints{classic: true})
	c.Assert(err, IsNil)

	defaults = gadget.SystemDefaults(ginfo.Defaults)
	c.Check(defaults, DeepEquals, map[string]interface{}{
		"something": true,
	})
}

func (s *gadgetYamlTestSuite) TestReadGadgetDefaultsMultiline(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockClassicGadgetMultilineDefaultsYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, &modelConstraints{classic: true})
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &gadget.Info{
		Defaults: map[string]map[string]interface{}{
			"system": {"something": true},
			// keep this comment so that gofmt 1.10+ does not
			// realign this, thus breaking our gofmt 1.9 checks
			"otheridididididididididididididi": {"foosnap": map[string]interface{}{"multiline": "foo\nbar\n"}},
		},
	})
}

func asSizePtr(size quantity.Size) *quantity.Size {
	gsz := quantity.Size(size)
	return &gsz
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlValid(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, coreConstraints)
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
		Volumes: map[string]*gadget.Volume{
			"volumename": {
				Schema:     "mbr",
				Bootloader: "u-boot",
				ID:         "0C",
				Structure: []gadget.VolumeStructure{
					{
						Label:       "system-boot",
						Role:        "system-boot", // implicit
						Offset:      asSizePtr(12345),
						OffsetWrite: mustParseGadgetRelativeOffset(c, "777"),
						Size:        88888,
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
		},
	})
}

func (s *gadgetYamlTestSuite) TestReadMultiVolumeGadgetYamlValid(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockMultiVolumeGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, nil)
	c.Assert(err, IsNil)
	c.Check(ginfo.Volumes, HasLen, 2)
	c.Assert(ginfo, DeepEquals, &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"frobinator-image": {
				Schema:     "mbr",
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
								UnresolvedSource: "splash.bmp",
								Target:           ".",
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
				Schema: "gpt",
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

	_, err = gadget.ReadInfo(s.dir, nil)
	c.Assert(err, ErrorMatches, "bootloader must be one of grub, u-boot, android-boot or lk")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlEmptyBootloader(c *C) {
	mockGadgetYamlBroken := []byte(`
volumes:
 name:
  bootloader:
`)

	err := ioutil.WriteFile(s.gadgetYamlPath, mockGadgetYamlBroken, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, &modelConstraints{classic: false})
	c.Assert(err, ErrorMatches, "bootloader not declared in any volume")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlMissingBootloader(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, nil, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, &modelConstraints{classic: false})
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

	_, err = gadget.ReadInfo(s.dir, nil)
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

		_, err = gadget.ReadInfo(s.dir, nil)
		c.Check(err, ErrorMatches, t.expectedErr)
	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlVolumeUpdate(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockVolumeUpdateGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, coreConstraints)
	c.Check(err, IsNil)
	c.Assert(ginfo, DeepEquals, &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"bootloader": {
				Schema:     "mbr",
				Bootloader: "u-boot",
				ID:         "0C",
				Structure: []gadget.VolumeStructure{
					{
						Label:       "system-boot",
						Role:        "system-boot", // implicit
						Offset:      asSizePtr(12345),
						OffsetWrite: mustParseGadgetRelativeOffset(c, "777"),
						Size:        88888,
						Type:        "0C",
						Filesystem:  "vfat",
						Content: []gadget.VolumeContent{{
							UnresolvedSource: "subdir/",
							Target:           "/",
							Unpack:           false,
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

	_, err = gadget.ReadInfo(s.dir, nil)
	c.Check(err, ErrorMatches, `cannot parse gadget metadata: "edition" must be a positive number, not "borked"`)

	broken = bytes.Replace(mockVolumeUpdateGadgetYaml, []byte("edition: 5"), []byte("edition: -5"), 1)
	err = ioutil.WriteFile(s.gadgetYamlPath, broken, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, nil)
	c.Check(err, ErrorMatches, `cannot parse gadget metadata: "edition" must be a positive number, not "-5"`)
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
		{"1234M", &gadget.RelativeOffset{Offset: 1234 * quantity.SizeMiB}, ""},
		{"4096M", &gadget.RelativeOffset{Offset: 4096 * quantity.SizeMiB}, ""},
		{"0", &gadget.RelativeOffset{}, ""},
		{"mbr+0", &gadget.RelativeOffset{RelativeTo: "mbr"}, ""},
		{"foo+1234M", &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1234 * quantity.SizeMiB}, ""},
		{"foo+1G", &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1 * quantity.SizeGiB}, ""},
		{"foo+1G", &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1 * quantity.SizeGiB}, ""},
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

var classicModelConstraints = []gadget.Model{
	nil,
	&modelConstraints{classic: false, systemSeed: false},
	&modelConstraints{classic: true, systemSeed: false},
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlPCHappy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlPC, 0644)
	c.Assert(err, IsNil)

	for _, constraints := range classicModelConstraints {
		_, err = gadget.ReadInfo(s.dir, constraints)
		c.Assert(err, IsNil)
	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlRPiHappy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlRPi, 0644)
	c.Assert(err, IsNil)

	for _, constraints := range classicModelConstraints {
		_, err = gadget.ReadInfo(s.dir, constraints)
		c.Assert(err, IsNil)
	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlLkHappy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlLk, 0644)
	c.Assert(err, IsNil)

	for _, constraints := range classicModelConstraints {
		_, err = gadget.ReadInfo(s.dir, constraints)
		c.Assert(err, IsNil)
	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlLkUC20Happy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlLkUC20, 0644)
	c.Assert(err, IsNil)

	uc20Model := &modelConstraints{
		systemSeed: true,
		classic:    false,
	}

	_, err = gadget.ReadInfo(s.dir, uc20Model)
	c.Assert(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlLkLegacyHappy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlLkLegacy, 0644)
	c.Assert(err, IsNil)

	for _, constraints := range classicModelConstraints {
		_, err = gadget.ReadInfo(s.dir, constraints)
		c.Assert(err, IsNil)
	}
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
		{"0C", "", "mbr"},
		// GPT UUID
		{"21686148-6449-6E6F-744E-656564454649", "", "gpt"},
		// GPT UUID (lowercase)
		{"21686148-6449-6e6f-744e-656564454649", "", "gpt"},
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
		{"21686148-6449-6E6F-744E-656564454649", `invalid type "21686148-6449-6E6F-744E-656564454649": GUID structure type with non-GPT schema "mbr"`, "mbr"},
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
		{"EF,AAAA686148-6449-6E6F-744E-656564454649", `invalid type "EF,AAAA686148-6449-6E6F-744E-656564454649": invalid format of hybrid type`, "gpt"},
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

func mustParseStructureNoImplicit(c *C, s string) *gadget.VolumeStructure {
	var v gadget.VolumeStructure
	err := yaml.Unmarshal([]byte(s), &v)
	c.Assert(err, IsNil)
	return &v
}

func mustParseStructure(c *C, s string) *gadget.VolumeStructure {
	vs := mustParseStructureNoImplicit(c, s)
	gadget.SetImplicitForVolumeStructure(vs, 0)
	return vs
}

func (s *gadgetYamlTestSuite) TestValidateRole(c *C) {
	uuidType := `
type: 21686148-6449-6E6F-744E-656564454649
size: 1023
`
	bareType := `
type: bare
`
	mbrTooLarge := bareType + `
role: mbr
size: 467`
	mbrBadOffset := bareType + `
role: mbr
size: 446
offset: 123`
	mbrBadID := bareType + `
role: mbr
id: 123
size: 446`
	mbrBadFilesystem := bareType + `
role: mbr
size: 446
filesystem: vfat`
	mbrNoneFilesystem := `
type: bare
role: mbr
filesystem: none
size: 446`
	typeConflictsRole := `
type: bare
role: system-data
size: 1M`
	validSystemBoot := uuidType + `
role: system-boot
`
	validSystemSeed := uuidType + `
role: system-seed
`
	validSystemSave := uuidType + `
role: system-save
size: 5M
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
	legacyTypeMatchingRole := `
type: mbr
role: mbr
size: 446`
	legacyTypeConflictsRole := `
type: mbr
role: system-data
size: 446`
	legacyTypeAsMBRTooLarge := `
type: mbr
size: 447`
	vol := &gadget.Volume{}
	mbrVol := &gadget.Volume{Schema: "mbr"}
	for i, tc := range []struct {
		s   *gadget.VolumeStructure
		v   *gadget.Volume
		err string
	}{
		{mustParseStructureNoImplicit(c, validSystemBoot), vol, ""},
		// empty, ok too
		{mustParseStructureNoImplicit(c, emptyRole), vol, ""},
		// invalid role name
		{mustParseStructureNoImplicit(c, bogusRole), vol, `invalid role "foobar": unsupported role`},
		// the system-seed role
		{mustParseStructureNoImplicit(c, validSystemSeed), vol, ""},
		// system-save role
		{mustParseStructureNoImplicit(c, validSystemSave), vol, ""},
		// mbr
		{mustParseStructureNoImplicit(c, mbrTooLarge), mbrVol, `invalid role "mbr": mbr structures cannot be larger than 446 bytes`},
		{mustParseStructureNoImplicit(c, mbrBadOffset), mbrVol, `invalid role "mbr": mbr structure must start at offset 0`},
		{mustParseStructureNoImplicit(c, mbrBadID), mbrVol, `invalid role "mbr": mbr structure must not specify partition ID`},
		{mustParseStructureNoImplicit(c, mbrBadFilesystem), mbrVol, `invalid role "mbr": mbr structures must not specify a file system`},
		// filesystem: none is ok for MBR
		{mustParseStructureNoImplicit(c, mbrNoneFilesystem), mbrVol, ""},
		// legacy, type: mbr treated like role: mbr
		{mustParseStructureNoImplicit(c, legacyMBR), mbrVol, ""},
		{mustParseStructureNoImplicit(c, legacyTypeMatchingRole), mbrVol, ""},
		{mustParseStructureNoImplicit(c, legacyTypeAsMBRTooLarge), mbrVol, `invalid implicit role "mbr": mbr structures cannot be larger than 446 bytes`},
		{mustParseStructureNoImplicit(c, legacyTypeConflictsRole), vol, `invalid role "system-data": conflicting legacy type: "mbr"`},
		// conflicting type/role
		{mustParseStructureNoImplicit(c, typeConflictsRole), vol, `invalid role "system-data": conflicting type: "bare"`},
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
		{"gpt", ""},
		{"mbr", ""},
		// implicit GPT
		{"", ""},
		// invalid
		{"some", `invalid schema "some"`},
	} {
		c.Logf("tc: %v %+v", i, tc.s)

		err := gadget.ValidateVolume("name", &gadget.Volume{Schema: tc.s}, nil)
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

		err := gadget.ValidateVolume(tc.s, &gadget.Volume{}, nil)
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
	}, nil)
	c.Assert(err, ErrorMatches, `structure name "duplicate" is not unique`)
}

func (s *gadgetYamlTestSuite) TestValidateVolumeDuplicateFsLabel(c *C) {
	err := gadget.ValidateVolume("name", &gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Label: "foo", Type: "21686148-6449-6E6F-744E-656564454123", Size: quantity.SizeMiB},
			{Label: "foo", Type: "21686148-6449-6E6F-744E-656564454649", Size: quantity.SizeMiB},
		},
	}, nil)
	c.Assert(err, ErrorMatches, `filesystem label "foo" is not unique`)

	// writable isn't special
	for _, x := range []struct {
		systemSeed bool
		label      string
		errMsg     string
	}{
		{false, "writable", `filesystem label "writable" is not unique`},
		{false, "ubuntu-data", `filesystem label "ubuntu-data" is not unique`},
		{true, "writable", `filesystem label "writable" is not unique`},
		{true, "ubuntu-data", `filesystem label "ubuntu-data" is not unique`},
	} {
		for _, constraints := range []*modelConstraints{
			{classic: false, systemSeed: x.systemSeed},
			{classic: true, systemSeed: x.systemSeed},
		} {
			err = gadget.ValidateVolume("name", &gadget.Volume{
				Structure: []gadget.VolumeStructure{{
					Name:  "data1",
					Role:  gadget.SystemData,
					Label: x.label,
					Type:  "21686148-6449-6E6F-744E-656564454123",
					Size:  quantity.SizeMiB,
				}, {
					Name:  "data2",
					Role:  gadget.SystemData,
					Label: x.label,
					Type:  "21686148-6449-6E6F-744E-656564454649",
					Size:  quantity.SizeMiB,
				}},
			}, constraints)
			c.Assert(err, ErrorMatches, x.errMsg)
		}
	}

	// nor is system-boot
	err = gadget.ValidateVolume("name", &gadget.Volume{
		Structure: []gadget.VolumeStructure{{
			Name:  "boot1",
			Label: "system-boot",
			Type:  "EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			Size:  quantity.SizeMiB,
		}, {
			Name:  "boot2",
			Label: "system-boot",
			Type:  "EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
			Size:  quantity.SizeMiB,
		}},
	}, nil)
	c.Assert(err, ErrorMatches, `filesystem label "system-boot" is not unique`)
}

func (s *gadgetYamlTestSuite) TestValidateVolumeErrorsWrapped(c *C) {
	err := gadget.ValidateVolume("name", &gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Type: "bare", Size: 1024},
			{Type: "bogus", Size: 1024},
		},
	}, nil)
	c.Assert(err, ErrorMatches, `invalid structure #1: invalid type "bogus": invalid format`)

	err = gadget.ValidateVolume("name", &gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Type: "bare", Size: 1024},
			{Type: "bogus", Size: 1024, Name: "foo"},
		},
	}, nil)
	c.Assert(err, ErrorMatches, `invalid structure #1 \("foo"\): invalid type "bogus": invalid format`)

	err = gadget.ValidateVolume("name", &gadget.Volume{
		Structure: []gadget.VolumeStructure{
			{Type: "bare", Name: "foo", Size: 1024, Content: []gadget.VolumeContent{{UnresolvedSource: "foo"}}},
		},
	}, nil)
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

	err := ioutil.WriteFile(s.gadgetYamlPath, []byte(gadgetYamlBadStructureName), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, nil)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #1 \("other-name"\) refers to an unknown structure "bad-name"`)

	err = ioutil.WriteFile(s.gadgetYamlPath, []byte(gadgetYamlBadContentName), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, nil)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #1 \("other-name"\), content #0 \("pc-core.img"\) refers to an unknown structure "bad-name"`)

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

func (s *gadgetYamlTestSuite) TestValidateLayoutOverlapPreceding(c *C) {
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

	_, err = gadget.ReadInfo(s.dir, nil)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #1 \("other-name"\) overlaps with the preceding structure #0 \("mbr"\)`)
}

func (s *gadgetYamlTestSuite) TestValidateLayoutOverlapOutOfOrder(c *C) {
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

	_, err = gadget.ReadInfo(s.dir, nil)
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

	_, err = gadget.ReadInfo(s.dir, nil)
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

	_, err = gadget.ReadInfo(s.dir, nil)
	c.Check(err, ErrorMatches, `invalid volume "pc": structure #1 \("mbr"\) has "mbr" role and must start at offset 0`)
}

type gadgetTestSuite struct{}

var _ = Suite(&gadgetTestSuite{})

func (s *gadgetTestSuite) TestEffectiveFilesystemLabel(c *C) {
	// no label, and no role set
	vs := gadget.VolumeStructure{Role: ""}
	c.Check(vs.EffectiveFilesystemLabel(), Equals, "")

	// explicitly set label
	vs = gadget.VolumeStructure{Label: "my-label"}
	c.Check(vs.EffectiveFilesystemLabel(), Equals, "my-label")

	// inferred based on role
	vs = gadget.VolumeStructure{Role: gadget.SystemData, Label: "unused-label"}
	c.Check(vs.EffectiveFilesystemLabel(), Equals, "writable")
	vs = gadget.VolumeStructure{Role: gadget.SystemData}
	c.Check(vs.EffectiveFilesystemLabel(), Equals, "writable")

	// only system-data role is special
	vs = gadget.VolumeStructure{Role: gadget.SystemBoot}
	c.Check(vs.EffectiveFilesystemLabel(), Equals, "")
}

func (s *gadgetYamlTestSuite) TestGadgetConsistencyWithoutConstraints(c *C) {
	for i, tc := range []struct {
		role  string
		label string
		err   string
	}{
		// when constraints are nil, the system-seed role and ubuntu-data label on the
		// system-data structure should be consistent
		{"system-seed", "", ""},
		{"system-seed", "writable", ".* system-data structure must not have a label"},
		{"system-seed", "ubuntu-data", ".* system-data structure must not have a label"},
		{"", "", ""},
		{"", "writable", ""},
		{"", "ubuntu-data", `.* must have an implicit label or "writable", not "ubuntu-data"`},
	} {
		c.Logf("tc: %v %v %v", i, tc.role, tc.label)
		b := &bytes.Buffer{}

		fmt.Fprintf(b, `
volumes:
  pc:
    bootloader: grub
    schema: mbr
    structure:`)

		if tc.role == "system-seed" {
			fmt.Fprintf(b, `
      - name: Recovery
        size: 10M
        type: 83
        role: system-seed`)
		}

		fmt.Fprintf(b, `
      - name: Data
        size: 10M
        type: 83
        role: system-data
        filesystem-label: %s`, tc.label)

		err := ioutil.WriteFile(s.gadgetYamlPath, b.Bytes(), 0644)
		c.Assert(err, IsNil)

		_, err = gadget.ReadInfo(s.dir, nil)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)

		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *gadgetYamlTestSuite) TestGadgetConsistencyWithConstraints(c *C) {
	bloader := `
volumes:
  pc:
    bootloader: grub
    schema: mbr
    structure:`

	for i, tc := range []struct {
		addSeed     bool
		dataLabel   string
		requireSeed bool
		addSave     bool
		saveLabel   string
		err         string
	}{
		// when constraints are nil, the system-seed role and ubuntu-data label on the
		// system-data structure should be consistent
		{addSeed: true, requireSeed: true},
		{addSeed: true, err: `.* model does not support the system-seed role`},
		{addSeed: true, dataLabel: "writable", requireSeed: true,
			err: ".* system-data structure must not have a label"},
		{addSeed: true, dataLabel: "writable",
			err: `.* model does not support the system-seed role`},
		{addSeed: true, dataLabel: "ubuntu-data", requireSeed: true,
			err: ".* system-data structure must not have a label"},
		{addSeed: true, dataLabel: "ubuntu-data",
			err: `.* model does not support the system-seed role`},
		{dataLabel: "writable", requireSeed: true,
			err: `.* model requires system-seed structure, but none was found`},
		{dataLabel: "writable"},
		{dataLabel: "ubuntu-data", requireSeed: true,
			err: `.* model requires system-seed structure, but none was found`},
		{dataLabel: "ubuntu-data", err: `.* must have an implicit label or "writable", not "ubuntu-data"`},
		{addSave: true, err: `.* system-save requires system-seed and system-data structures`},
		{addSeed: true, requireSeed: true, addSave: true, saveLabel: "foo",
			err: `.* system-save structure must not have a label`},
	} {
		c.Logf("tc: %v %v %v %v", i, tc.addSeed, tc.dataLabel, tc.requireSeed)
		b := &bytes.Buffer{}

		fmt.Fprintf(b, bloader)
		if tc.addSeed {
			fmt.Fprintf(b, `
      - name: Recovery
        size: 10M
        type: 83
        role: system-seed`)
		}

		fmt.Fprintf(b, `
      - name: Data
        size: 10M
        type: 83
        role: system-data
        filesystem-label: %s`, tc.dataLabel)
		if tc.addSave {
			fmt.Fprintf(b, `
      - name: Save
        size: 10M
        type: 83
        role: system-save`)
			if tc.saveLabel != "" {
				fmt.Fprintf(b, `
        filesystem-label: %s`, tc.saveLabel)

			}
		}

		err := ioutil.WriteFile(s.gadgetYamlPath, b.Bytes(), 0644)
		c.Assert(err, IsNil)

		constraints := &modelConstraints{
			classic:    false,
			systemSeed: tc.requireSeed,
		}

		_, err = gadget.ReadInfo(s.dir, constraints)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}

	// test error with no volumes
	err := ioutil.WriteFile(s.gadgetYamlPath, []byte(bloader), 0644)
	c.Assert(err, IsNil)
	constraints := &modelConstraints{
		systemSeed: true,
	}
	_, err = gadget.ReadInfo(s.dir, constraints)
	c.Assert(err, ErrorMatches, ".*: model requires system-seed partition, but no system-seed or system-data partition found")
}

func (s *gadgetYamlTestSuite) TestGadgetReadInfoVsFromMeta(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlPC, 0644)
	c.Assert(err, IsNil)

	constraints := &modelConstraints{
		classic: false,
	}

	giRead, err := gadget.ReadInfo(s.dir, constraints)
	c.Check(err, IsNil)

	giMeta, err := gadget.InfoFromGadgetYaml(gadgetYamlPC, constraints)
	c.Check(err, IsNil)

	c.Assert(giRead, DeepEquals, giMeta)
}

var (
	classicConstraints = &modelConstraints{
		classic: true,
	}
	coreConstraints = &modelConstraints{
		classic: false,
	}
	uc20Constraints = &modelConstraints{
		classic:    false,
		systemSeed: true,
	}
)

func (s *gadgetYamlTestSuite) TestGadgetImplicitSchema(c *C) {
	var minimal = []byte(`
volumes:
   minimal:
     bootloader: grub
`)

	tests := map[string][]byte{
		"minimal": minimal,
		"pc":      gadgetYamlPC,
	}

	for volName, yaml := range tests {
		giMeta, err := gadget.InfoFromGadgetYaml(yaml, nil)
		c.Assert(err, IsNil)

		vol := giMeta.Volumes[volName]
		c.Check(vol.Schema, Equals, "gpt")
	}
}

func (s *gadgetYamlTestSuite) TestGadgetImplicitRoleMBR(c *C) {
	var minimal = []byte(`
volumes:
   minimal:
     bootloader: grub
     structure:
       - name: mbr
         type: mbr
         size: 440
`)

	tests := map[string][]byte{
		"minimal": minimal,
		"pc":      gadgetYamlPC,
	}

	for volName, yaml := range tests {
		giMeta, err := gadget.InfoFromGadgetYaml(yaml, nil)
		c.Assert(err, IsNil)

		vs := giMeta.Volumes[volName].Structure[0]
		c.Check(vs.Role, Equals, "mbr")
	}
}

func (s *gadgetYamlTestSuite) TestGadgetImplicitRoleLegacySystemBoot(c *C) {
	minimal := []byte(`
volumes:
   minimal:
     bootloader: grub
     structure:
       - name: boot
         filesystem-label: system-boot
         type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
         size: 1G
`)

	explicit := []byte(`
volumes:
   explicit:
     bootloader: grub
     schema: mbr
     structure:
       - name: boot
         filesystem-label: system-boot
         role: bootselect
         type: EF
         size: 1G
`)

	data := []byte(`
volumes:
   data:
     bootloader: grub
     schema: mbr
     structure:
       - name: dat
         filesystem-label: system-data
         type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
         size: 1G
`)

	tests := []struct {
		name      string
		structure string
		yaml      []byte
		model     gadget.Model
		role      string
	}{
		{"pc", "EFI System", gadgetYamlPC, coreConstraints, gadget.SystemBoot},
		// XXX later {gadgetYamlUC20PC, uc20Constraints},
		{"minimal", "boot", minimal, nil, ""},
		{"minimal", "boot", minimal, coreConstraints, gadget.SystemBoot},
		// XXX later {minimal, uc20Constraints},
		{"explicit", "boot", explicit, coreConstraints, "bootselect"},
		{"data", "dat", data, coreConstraints, ""},
	}

	for _, t := range tests {
		giMeta, err := gadget.InfoFromGadgetYaml(t.yaml, t.model)
		c.Assert(err, IsNil)

		foundStruct := false
		vol := giMeta.Volumes[t.name]
		for _, vs := range vol.Structure {
			if vs.Name != t.structure {
				continue
			}
			foundStruct = true
			c.Check(vs.Role, Equals, t.role)
		}
		c.Check(foundStruct, Equals, true)
	}
}

func (s *gadgetYamlTestSuite) TestGadgetFromMetaEmpty(c *C) {
	// this is ok for classic
	giClassic, err := gadget.InfoFromGadgetYaml([]byte(""), classicConstraints)
	c.Check(err, IsNil)
	c.Assert(giClassic, DeepEquals, &gadget.Info{})

	// but not so much for core
	giCore, err := gadget.InfoFromGadgetYaml([]byte(""), coreConstraints)
	c.Check(err, ErrorMatches, "bootloader not declared in any volume")
	c.Assert(giCore, IsNil)
}

func (s *gadgetYamlTestSuite) TestPositionedVolumeFromGadgetMultiVolume(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockMultiVolumeGadgetYaml, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.PositionedVolumeFromGadget(s.dir)
	c.Assert(err, ErrorMatches, "cannot position multiple volumes yet")
}

func (s *gadgetYamlTestSuite) TestPositionedVolumeFromGadgetHappy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlPC, 0644)
	c.Assert(err, IsNil)
	for _, fn := range []string{"pc-boot.img", "pc-core.img"} {
		err = ioutil.WriteFile(filepath.Join(s.dir, fn), nil, 0644)
		c.Assert(err, IsNil)
	}

	lv, err := gadget.PositionedVolumeFromGadget(s.dir)
	c.Assert(err, IsNil)
	c.Assert(lv.Volume.Bootloader, Equals, "grub")
	// mbr, bios-boot, efi-system
	c.Assert(lv.LaidOutStructure, HasLen, 3)
}

func (s *gadgetYamlTestSuite) TestStructureBareFilesystem(c *C) {
	bareType := `
type: bare
size: 1M`
	mbr := `
role: mbr
size: 446`
	mbrLegacy := `
type: mbr
size: 446`
	fs := `
type: 21686148-6449-6E6F-744E-656564454649
filesystem: vfat`
	rawFsNoneExplicit := `
type: 21686148-6449-6E6F-744E-656564454649
filesystem: none
size: 1M`
	raw := `
type: 21686148-6449-6E6F-744E-656564454649
size: 1M`
	for i, tc := range []struct {
		s           *gadget.VolumeStructure
		hasFs       bool
		isPartition bool
	}{
		{mustParseStructure(c, bareType), false, false},
		{mustParseStructure(c, mbr), false, false},
		{mustParseStructure(c, mbrLegacy), false, false},
		{mustParseStructure(c, fs), true, true},
		{mustParseStructure(c, rawFsNoneExplicit), false, true},
		{mustParseStructure(c, raw), false, true},
	} {
		c.Logf("tc: %v %+v", i, tc.s)
		c.Check(tc.s.HasFilesystem(), Equals, tc.hasFs)
		c.Check(tc.s.IsPartition(), Equals, tc.isPartition)
	}
}

var mockSnapYaml = `name: pc
type: gadget
version: 1.0
`

func (s *gadgetYamlTestSuite) TestReadGadgetYamlFromSnapFileMissing(c *C) {
	snapPath := snaptest.MakeTestSnapWithFiles(c, string(mockSnapYaml), nil)
	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)

	// if constraints are nil, we allow a missing gadget.yaml
	_, err = gadget.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfoFromSnapFile(snapf, &modelConstraints{})
	c.Assert(err, ErrorMatches, ".*meta/gadget.yaml: no such file or directory")
}

var minimalMockGadgetYaml = `
volumes:
 pc:
  bootloader: grub
`

func (s *gadgetYamlTestSuite) TestReadGadgetYamlFromSnapFileValid(c *C) {
	snapPath := snaptest.MakeTestSnapWithFiles(c, mockSnapYaml, [][]string{
		{"meta/gadget.yaml", string(minimalMockGadgetYaml)},
	})
	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"pc": {
				Bootloader: "grub",
				Schema:     "gpt",
			},
		},
	})
}

type gadgetCompatibilityTestSuite struct{}

var _ = Suite(&gadgetCompatibilityTestSuite{})

func (s *gadgetCompatibilityTestSuite) TestGadgetIsCompatibleSelf(c *C) {
	giPC1, err := gadget.InfoFromGadgetYaml(gadgetYamlPC, coreConstraints)
	c.Assert(err, IsNil)
	giPC2, err := gadget.InfoFromGadgetYaml(gadgetYamlPC, coreConstraints)
	c.Assert(err, IsNil)

	err = gadget.IsCompatible(giPC1, giPC2)
	c.Check(err, IsNil)
}

func (s *gadgetCompatibilityTestSuite) TestGadgetIsCompatibleBadVolume(c *C) {
	var mockYaml = []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
    id: 0C
`)

	var mockOtherYaml = []byte(`
volumes:
  volumename-other:
    schema: mbr
    bootloader: u-boot
    id: 0C
`)
	var mockManyYaml = []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
    id: 0C
  volumename-many:
    schema: mbr
    id: 0C
`)
	var mockBadIDYaml = []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
    id: 0D
`)
	var mockSchemaYaml = []byte(`
volumes:
  volumename:
    schema: gpt
    bootloader: u-boot
    id: 0C
`)
	var mockBootloaderYaml = []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: grub
    id: 0C
`)
	var mockBadStructureSizeYaml = []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: grub
    id: 0C
    structure:
      - name: bad-size
        size: 99999
        type: 0C
`)
	for _, tc := range []struct {
		gadgetYaml []byte
		err        string
	}{
		{mockOtherYaml, `cannot find entry for volume "volumename" in updated gadget info`},
		{mockManyYaml, "gadgets with multiple volumes are unsupported"},
		{mockBadStructureSizeYaml, `cannot lay out the new volume: cannot lay out volume, structure #0 \("bad-size"\) size is not a multiple of sector size 512`},
		{mockBadIDYaml, "incompatible layout change: incompatible ID change from 0C to 0D"},
		{mockSchemaYaml, "incompatible layout change: incompatible schema change from mbr to gpt"},
		{mockBootloaderYaml, "incompatible layout change: incompatible bootloader change from u-boot to grub"},
	} {
		c.Logf("trying: %v\n", string(tc.gadgetYaml))
		gi, err := gadget.InfoFromGadgetYaml(mockYaml, coreConstraints)
		c.Assert(err, IsNil)
		giNew, err := gadget.InfoFromGadgetYaml(tc.gadgetYaml, coreConstraints)
		c.Assert(err, IsNil)
		err = gadget.IsCompatible(gi, giNew)
		if tc.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, tc.err)
		}

	}
}

func (s *gadgetCompatibilityTestSuite) TestGadgetIsCompatibleBadStructure(c *C) {
	var baseYaml = `
volumes:
  volumename:
    schema: gpt
    bootloader: grub
    id: 0C
    structure:`
	var mockYaml = baseYaml + `
      - name: legit
        size: 2M
        type: 00000000-0000-0000-0000-0000deadbeef
        filesystem: ext4
        filesystem-label: fs-legit
`
	var mockBadStructureTypeYaml = baseYaml + `
      - name: legit
        size: 2M
        type: 00000000-0000-0000-0000-0000deadcafe
        filesystem: ext4
        filesystem-label: fs-legit
`
	var mockBadFsYaml = baseYaml + `
      - name: legit
        size: 2M
        type: 00000000-0000-0000-0000-0000deadbeef
        filesystem: vfat
        filesystem-label: fs-legit
`
	var mockBadOffsetYaml = baseYaml + `
      - name: legit
        size: 2M
        type: 00000000-0000-0000-0000-0000deadbeef
        filesystem: ext4
        offset: 1M
        filesystem-label: fs-legit
`
	var mockBadLabelYaml = baseYaml + `
      - name: legit
        size: 2M
        type: 00000000-0000-0000-0000-0000deadbeef
        filesystem: ext4
        filesystem-label: fs-non-legit
`
	var mockGPTBadNameYaml = baseYaml + `
      - name: non-legit
        size: 2M
        type: 00000000-0000-0000-0000-0000deadbeef
        filesystem: ext4
        filesystem-label: fs-legit
`

	for i, tc := range []struct {
		gadgetYaml string
		err        string
	}{
		{mockYaml, ``},
		{mockBadStructureTypeYaml, `incompatible layout change: incompatible structure #0 \("legit"\) change: cannot change structure type from "00000000-0000-0000-0000-0000deadbeef" to "00000000-0000-0000-0000-0000deadcafe"`},
		{mockBadFsYaml, `incompatible layout change: incompatible structure #0 \("legit"\) change: cannot change filesystem from "ext4" to "vfat"`},
		{mockBadOffsetYaml, `incompatible layout change: incompatible structure #0 \("legit"\) change: cannot change structure offset from unspecified to 1048576`},
		{mockBadLabelYaml, `incompatible layout change: incompatible structure #0 \("legit"\) change: cannot change filesystem label from "fs-legit" to "fs-non-legit"`},
		{mockGPTBadNameYaml, `incompatible layout change: incompatible structure #0 \("non-legit"\) change: cannot change structure name from "legit" to "non-legit"`},
	} {
		c.Logf("trying: %d %v\n", i, string(tc.gadgetYaml))
		gi, err := gadget.InfoFromGadgetYaml([]byte(mockYaml), coreConstraints)
		c.Assert(err, IsNil)
		giNew, err := gadget.InfoFromGadgetYaml([]byte(tc.gadgetYaml), coreConstraints)
		c.Assert(err, IsNil)
		err = gadget.IsCompatible(gi, giNew)
		if tc.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, tc.err)
		}

	}
}

func (s *gadgetCompatibilityTestSuite) TestGadgetIsCompatibleStructureNameMBR(c *C) {
	var baseYaml = `
volumes:
  volumename:
    schema: mbr
    bootloader: grub
    id: 0C
    structure:`
	var mockYaml = baseYaml + `
      - name: legit
        size: 2M
        type: 0A
`
	var mockMBRNameOkYaml = baseYaml + `
      - name: non-legit
        size: 2M
        type: 0A
`

	gi, err := gadget.InfoFromGadgetYaml([]byte(mockYaml), coreConstraints)
	c.Assert(err, IsNil)
	giNew, err := gadget.InfoFromGadgetYaml([]byte(mockMBRNameOkYaml), coreConstraints)
	c.Assert(err, IsNil)
	err = gadget.IsCompatible(gi, giNew)
	c.Check(err, IsNil)
}
