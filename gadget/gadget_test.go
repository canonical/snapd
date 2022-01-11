// -*- Mode: Go; indent-tabs-mode: t -*-

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
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
)

type gadgetYamlTestSuite struct {
	dir            string
	gadgetYamlPath string
}

var _ = Suite(&gadgetYamlTestSuite{})

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

var mockMultiVolumeUC20GadgetYaml = []byte(`
volumes:
  frobinator-image:
    bootloader: u-boot
    schema: gpt
    structure:
      - name: ubuntu-seed
        filesystem: ext4
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-seed
      - name: ubuntu-save
        size: 10485760
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-save
      - name: ubuntu-boot
        filesystem: ext4
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-boot
      - name: ubuntu-data
        filesystem: ext4
        size: 1G
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
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

var mockMultiVolumeGadgetYaml = []byte(`
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
        role: system-seed-select
        content:
            - image: snaprecoverysel.bin
        type: B214D5E4-D442-45E6-B8C6-01BDCD82D396
      - name: snaprecoveryselbak
        offset: 8519680
        size: 131072
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
        role: system-seed-image
      - name: boot_rb
        offset: 40894464
        size: 31457280
        type: 20117F86-E985-4357-B9EE-374BC1D8487D
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

type modelCharateristics struct {
	classic    bool
	systemSeed bool
}

func (m *modelCharateristics) Classic() bool {
	return m.classic
}

func (m *modelCharateristics) Grade() asserts.ModelGrade {
	if m.systemSeed {
		return asserts.ModelSigned
	}
	return asserts.ModelGradeUnset
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlMissing(c *C) {
	// if model is nil, we allow a missing yaml
	_, err := gadget.ReadInfo("bogus-path", nil)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo("bogus-path", &modelCharateristics{})
	c.Assert(err, ErrorMatches, ".*meta/gadget.yaml: no such file or directory")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicOptional(c *C) {
	// no meta/gadget.yaml
	gi, err := gadget.ReadInfo(s.dir, &modelCharateristics{classic: true})
	c.Assert(err, IsNil)
	c.Check(gi, NotNil)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicEmptyIsValid(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, nil, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, &modelCharateristics{classic: true})
	c.Assert(err, IsNil)
	c.Assert(ginfo, DeepEquals, &gadget.Info{})
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicOnylDefaultsIsValid(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockClassicGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, &modelCharateristics{classic: true})
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

	ginfo, err := gadget.ReadInfo(s.dir, &modelCharateristics{classic: true})
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
	ginfo, err = gadget.ReadInfo(s.dir, &modelCharateristics{classic: true})
	c.Assert(err, IsNil)

	defaults = gadget.SystemDefaults(ginfo.Defaults)
	c.Check(defaults, DeepEquals, map[string]interface{}{
		"something": true,
	})
}

var mockGadgetWithEmptyVolumes = `device-tree-origin: kernel
volumes:
  lun-0:
`

func (s *gadgetYamlTestSuite) TestRegressionGadgetWithEmptyVolume(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, []byte(mockGadgetWithEmptyVolumes), 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, nil)
	c.Assert(err, ErrorMatches, `volume "lun-0" stanza is empty`)
}

func (s *gadgetYamlTestSuite) TestReadGadgetDefaultsMultiline(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockClassicGadgetMultilineDefaultsYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, &modelCharateristics{classic: true})
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

func asOffsetPtr(offs quantity.Offset) *quantity.Offset {
	goff := offs
	return &goff
}

var (
	classicMod = &modelCharateristics{
		classic: true,
	}
	coreMod = &modelCharateristics{
		classic: false,
	}
	uc20Mod = &modelCharateristics{
		classic:    false,
		systemSeed: true,
	}
)

func (s *gadgetYamlTestSuite) TestReadGadgetYamlValid(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockGadgetYaml, 0644)
	c.Assert(err, IsNil)

	ginfo, err := gadget.ReadInfo(s.dir, coreMod)
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
				Name:       "frobinator-image",
				Schema:     "mbr",
				Bootloader: "u-boot",
				Structure: []gadget.VolumeStructure{
					{
						VolumeName: "frobinator-image",
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
						VolumeName: "frobinator-image",
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
				Name:   "u-boot-frobinator",
				Schema: "gpt",
				Structure: []gadget.VolumeStructure{
					{
						VolumeName: "u-boot-frobinator",
						Name:       "u-boot",
						Type:       "bare",
						Size:       623000,
						Offset:     asOffsetPtr(0),
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

	_, err = gadget.ReadInfo(s.dir, &modelCharateristics{classic: false})
	c.Assert(err, ErrorMatches, "bootloader not declared in any volume")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlMissingBootloader(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, nil, 0644)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfo(s.dir, &modelCharateristics{classic: false})
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

	ginfo, err := gadget.ReadInfo(s.dir, coreMod)
	c.Check(err, IsNil)
	c.Assert(ginfo, DeepEquals, &gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"bootloader": {
				Name:       "bootloader",
				Schema:     "mbr",
				Bootloader: "u-boot",
				ID:         "0C",
				Structure: []gadget.VolumeStructure{
					{
						VolumeName:  "bootloader",
						Label:       "system-boot",
						Role:        "system-boot", // implicit
						Offset:      asOffsetPtr(12345),
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
		{"1234M", &gadget.RelativeOffset{Offset: 1234 * quantity.OffsetMiB}, ""},
		{"4096M", &gadget.RelativeOffset{Offset: 4096 * quantity.OffsetMiB}, ""},
		{"0", &gadget.RelativeOffset{}, ""},
		{"mbr+0", &gadget.RelativeOffset{RelativeTo: "mbr"}, ""},
		{"foo+1234M", &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1234 * quantity.OffsetMiB}, ""},
		{"foo+1G", &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1024 * quantity.OffsetMiB}, ""},
		{"foo+1G", &gadget.RelativeOffset{RelativeTo: "foo", Offset: 1024 * quantity.OffsetMiB}, ""},
		{"foo+4097M", nil, `cannot parse relative offset "foo\+4097M": offset above 4G limit`},
		{"foo+", nil, `cannot parse relative offset "foo\+": missing offset`},
		{"foo+++12", nil, `cannot parse relative offset "foo\+\+\+12": cannot parse offset "\+\+12": .*`},
		{"+12", nil, `cannot parse relative offset "\+12": missing volume name`},
		{"a0M", nil, `cannot parse relative offset "a0M": cannot parse offset "a0M": no numerical prefix.*`},
		{"-123", nil, `cannot parse relative offset "-123": cannot parse offset "-123": offset cannot be negative`},
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

var classicModelCharacteristics = []gadget.Model{
	nil,
	&modelCharateristics{classic: false, systemSeed: false},
	&modelCharateristics{classic: true, systemSeed: false},
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlPCHappy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlPC, 0644)
	c.Assert(err, IsNil)

	for _, mod := range classicModelCharacteristics {
		_, err = gadget.ReadInfo(s.dir, mod)
		c.Assert(err, IsNil)
	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlRPiHappy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlRPi, 0644)
	c.Assert(err, IsNil)

	for _, mod := range classicModelCharacteristics {
		_, err = gadget.ReadInfo(s.dir, mod)
		c.Assert(err, IsNil)
	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlLkHappy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlLk, 0644)
	c.Assert(err, IsNil)

	for _, mod := range classicModelCharacteristics {
		_, err = gadget.ReadInfo(s.dir, mod)
		c.Assert(err, IsNil)
	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlLkUC20Happy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlLkUC20, 0644)
	c.Assert(err, IsNil)

	uc20Model := &modelCharateristics{
		systemSeed: true,
		classic:    false,
	}

	_, err = gadget.ReadInfo(s.dir, uc20Model)
	c.Assert(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlLkLegacyHappy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlLkLegacy, 0644)
	c.Assert(err, IsNil)

	for _, mod := range classicModelCharacteristics {
		_, err = gadget.ReadInfo(s.dir, mod)
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
	gadget.SetImplicitForVolumeStructure(vs, 0, make(map[string]bool))
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

		err := gadget.ValidateVolume(&gadget.Volume{Name: "name", Schema: tc.s}, nil)
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

		err := gadget.ValidateVolume(&gadget.Volume{Name: tc.s}, nil)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *gadgetYamlTestSuite) TestValidateVolumeDuplicateStructures(c *C) {
	err := gadget.ValidateVolume(&gadget.Volume{
		Name: "name",
		Structure: []gadget.VolumeStructure{
			{Name: "duplicate", Type: "bare", Size: 1024},
			{Name: "duplicate", Type: "21686148-6449-6E6F-744E-656564454649", Size: 2048},
		},
	}, nil)
	c.Assert(err, ErrorMatches, `structure name "duplicate" is not unique`)
}

func (s *gadgetYamlTestSuite) TestValidateVolumeDuplicateFsLabel(c *C) {
	err := gadget.ValidateVolume(&gadget.Volume{
		Name: "name",
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

		err = gadget.ValidateVolume(&gadget.Volume{
			Name: "name",
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
		}, nil)
		c.Assert(err, ErrorMatches, x.errMsg)
	}

	// nor is system-boot
	err = gadget.ValidateVolume(&gadget.Volume{
		Name: "name",
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
	err := gadget.ValidateVolume(&gadget.Volume{
		Name: "name",
		Structure: []gadget.VolumeStructure{
			{Type: "bare", Size: 1024},
			{Type: "bogus", Size: 1024},
		},
	}, nil)
	c.Assert(err, ErrorMatches, `invalid structure #1: invalid type "bogus": invalid format`)

	err = gadget.ValidateVolume(&gadget.Volume{
		Name: "name",
		Structure: []gadget.VolumeStructure{
			{Type: "bare", Size: 1024},
			{Type: "bogus", Size: 1024, Name: "foo"},
		},
	}, nil)
	c.Assert(err, ErrorMatches, `invalid structure #1 \("foo"\): invalid type "bogus": invalid format`)

	err = gadget.ValidateVolume(&gadget.Volume{
		Name: "name",
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
	sourceEmpty := `
type: 21686148-6449-6E6F-744E-656564454649
filesystem: ext4
size: 1M
content:
  - source:
    target: /
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
		{mustParseStructure(c, fsMissing), nil, `invalid content #0: missing target`},
		{mustParseStructure(c, sourceEmpty), nil, `invalid content #0: missing source`},
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

func (s *gadgetYamlTestSuite) TestReadInfoAndValidateConsistencyWithoutModelCharacteristics(c *C) {
	for i, tc := range []struct {
		role  string
		label string
		err   string
	}{
		// when characteristics are nil, the system-seed role and ubuntu-data label on the
		// system-data structure should be consistent
		{"system-seed", "writable", `.* must have an implicit label or "ubuntu-data", not "writable"`},
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

		_, err = gadget.ReadInfoAndValidate(s.dir, nil, nil)
		c.Check(err, ErrorMatches, tc.err)
	}
}

func (s *gadgetYamlTestSuite) TestReadInfoAndValidateConsistencyWithModelCharacteristics(c *C) {
	bloader := `
volumes:
  pc:
    bootloader: grub
    schema: mbr
    structure:`

	err := ioutil.WriteFile(s.gadgetYamlPath, []byte(bloader), 0644)
	c.Assert(err, IsNil)
	mod := &modelCharateristics{
		systemSeed: true,
	}

	_, err = gadget.ReadInfoAndValidate(s.dir, mod, nil)
	c.Assert(err, ErrorMatches, "model requires system-seed partition, but no system-seed or system-data partition found")
}

func (s *gadgetYamlTestSuite) TestGadgetReadInfoVsFromMeta(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlPC, 0644)
	c.Assert(err, IsNil)

	mod := &modelCharateristics{
		classic: false,
	}

	giRead, err := gadget.ReadInfo(s.dir, mod)
	c.Check(err, IsNil)

	giMeta, err := gadget.InfoFromGadgetYaml(gadgetYamlPC, mod)
	c.Check(err, IsNil)

	c.Assert(giRead, DeepEquals, giMeta)
}

func (s *gadgetYamlTestSuite) TestReadInfoValidatesEmptySource(c *C) {
	var gadgetYamlContent = `
volumes:
  missing:
    bootloader: grub
    structure:
      - name: missing-content-source
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        filesystem: ext4
        content:
          - source: foo
            target: /
          - source:
            target: /

`
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))

	_, err := gadget.ReadInfo(s.dir, nil)
	c.Assert(err, ErrorMatches, `invalid volume "missing": invalid structure #0 \("missing-content-source"\): invalid content #1: missing source`)
}

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

	constr := gadget.LayoutConstraints{NonMBRStartOffset: 1 * quantity.OffsetMiB}

	for volName, yaml := range tests {
		giMeta, err := gadget.InfoFromGadgetYaml(yaml, nil)
		c.Assert(err, IsNil)

		vs := giMeta.Volumes[volName].Structure[0]
		c.Check(vs.Role, Equals, "mbr")

		// also layout the volume and check that when laying out the MBR
		// structure it retains the role of MBR, as validated by IsRoleMBR
		ls, err := gadget.LayoutVolumePartially(giMeta.Volumes[volName], constr)
		c.Assert(err, IsNil)
		c.Check(gadget.IsRoleMBR(ls.LaidOutStructure[0]), Equals, true)
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
		{"pc", "EFI System", gadgetYamlPC, coreMod, gadget.SystemBoot},
		// XXX later {gadgetYamlUC20PC, uc20Mod},
		{"minimal", "boot", minimal, nil, ""},
		{"minimal", "boot", minimal, coreMod, gadget.SystemBoot},
		// XXX later {minimal, uc20Mod},
		{"explicit", "boot", explicit, coreMod, "bootselect"},
		{"data", "dat", data, coreMod, ""},
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

func (s *gadgetYamlTestSuite) TestGadgetImplicitFSLabelUC16(c *C) {
	minimal := []byte(`
volumes:
   minimal:
     bootloader: grub
     structure:
       - name: dat
         role: system-data
         type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
         size: 1G
`)

	explicit := []byte(`
volumes:
   explicit:
     bootloader: grub
     structure:
       - name: dat
         filesystem-label: writable
         role: system-data
         type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
         size: 1G
`)
	tests := []struct {
		name      string
		structure string
		yaml      []byte
		fsLabel   string
	}{
		{"minimal", "dat", minimal, "writable"},
		{"explicit", "dat", explicit, "writable"},
	}

	for _, t := range tests {
		giMeta, err := gadget.InfoFromGadgetYaml(t.yaml, coreMod)
		c.Assert(err, IsNil)

		foundStruct := false
		vol := giMeta.Volumes[t.name]
		for _, vs := range vol.Structure {
			if vs.Name != t.structure {
				continue
			}
			foundStruct = true
			c.Check(vs.Label, Equals, t.fsLabel)
		}
		c.Check(foundStruct, Equals, true)
	}
}

func (s *gadgetYamlTestSuite) TestGadgetImplicitFSLabelUC20(c *C) {
	minimal := []byte(`
volumes:
   minimal:
     bootloader: grub
     structure:
       - name: seed
         role: system-seed
         type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
         size: 1G
       - name: boot
         role: system-boot
         type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
         size: 500M
       - name: dat
         role: system-data
         type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
         size: 1G
       - name: sav
         role: system-save
         type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
         size: 1G
`)

	tests := []struct {
		name      string
		structure string
		yaml      []byte
		fsLabel   string
	}{
		{"minimal", "seed", minimal, "ubuntu-seed"},
		{"minimal", "boot", minimal, "ubuntu-boot"},
		{"minimal", "dat", minimal, "ubuntu-data"},
		{"minimal", "sav", minimal, "ubuntu-save"},
		{"pc", "ubuntu-seed", gadgetYamlUC20PC, "ubuntu-seed"},
		{"pc", "ubuntu-boot", gadgetYamlUC20PC, "ubuntu-boot"},
		{"pc", "ubuntu-data", gadgetYamlUC20PC, "ubuntu-data"},
		{"pc", "ubuntu-save", gadgetYamlUC20PC, "ubuntu-save"},
	}

	for _, t := range tests {
		giMeta, err := gadget.InfoFromGadgetYaml(t.yaml, uc20Mod)
		c.Assert(err, IsNil)

		foundStruct := false
		vol := giMeta.Volumes[t.name]
		for _, vs := range vol.Structure {
			if vs.Name != t.structure {
				continue
			}
			foundStruct = true
			c.Check(vs.Label, Equals, t.fsLabel)
		}
		c.Check(foundStruct, Equals, true)
	}
}

func (s *validateGadgetTestSuite) TestGadgetImplicitFSLabelDuplicate(c *C) {
	const pcYaml = `
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
      - name: EFI System
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        filesystem: vfat
        filesystem-label: system-boot
        size: 50M
      - name: data
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
        role: system-data
`

	tests := []struct {
		yaml  string
		label string
		mod   gadget.Model
		err   string
	}{
		{pcYaml, "foo", coreMod, ""},
		{pcYaml, "writable", coreMod, `.*: filesystem label "writable" is implied by system-data role but was already set elsewhere`},
		{pcYaml, "writable", nil, ""},
		{string(gadgetYamlUC20PC), "ubuntu-data", nil, ""},
		{string(gadgetYamlUC20PC), "ubuntu-data", uc20Mod, `.*: filesystem label "ubuntu-data" is implied by system-data role but was already set elsewhere`},
		{string(gadgetYamlUC20PC), "ubuntu-save", uc20Mod, `.*: filesystem label "ubuntu-save" is implied by system-save role but was already set elsewhere`},
		{string(gadgetYamlUC20PC), "ubuntu-seed", uc20Mod, `.*: filesystem label "ubuntu-seed" is implied by system-seed role but was already set elsewhere`},
		{string(gadgetYamlUC20PC), "ubuntu-boot", uc20Mod, `.*: filesystem label "ubuntu-boot" is implied by system-boot role but was already set elsewhere`},
	}

	for _, t := range tests {
		dup := fmt.Sprintf(`
      - name: dup
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        filesystem-label: %s
        size: 1M

`, t.label)

		yaml := strings.TrimSpace(t.yaml) + dup
		_, err := gadget.InfoFromGadgetYaml([]byte(yaml), t.mod)
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *gadgetYamlTestSuite) TestGadgetFromMetaEmpty(c *C) {
	// this is ok for classic
	giClassic, err := gadget.InfoFromGadgetYaml([]byte(""), classicMod)
	c.Check(err, IsNil)
	c.Assert(giClassic, DeepEquals, &gadget.Info{})

	// but not so much for core
	giCore, err := gadget.InfoFromGadgetYaml([]byte(""), coreMod)
	c.Check(err, ErrorMatches, "bootloader not declared in any volume")
	c.Assert(giCore, IsNil)
}

func (s *gadgetYamlTestSuite) TestLaidOutVolumesFromGadgetMultiVolume(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, mockMultiVolumeUC20GadgetYaml, 0644)
	c.Assert(err, IsNil)

	err = ioutil.WriteFile(filepath.Join(s.dir, "u-boot.imz"), nil, 0644)
	c.Assert(err, IsNil)

	systemLv, all, err := gadget.LaidOutVolumesFromGadget(s.dir, "", uc20Mod)
	c.Assert(err, IsNil)

	c.Assert(all, HasLen, 2)
	c.Assert(all["frobinator-image"], DeepEquals, systemLv)
	zero := quantity.Offset(0)
	c.Assert(all["u-boot-frobinator"].LaidOutStructure, DeepEquals, []gadget.LaidOutStructure{
		{
			VolumeStructure: &gadget.VolumeStructure{
				VolumeName: "u-boot-frobinator",
				Name:       "u-boot",
				Offset:     &zero,
				Size:       quantity.Size(623000),
				Type:       "bare",
				Content: []gadget.VolumeContent{
					{Image: "u-boot.imz"},
				},
			},
			StartOffset: 0,
			LaidOutContent: []gadget.LaidOutContent{
				{
					VolumeContent: &gadget.VolumeContent{
						Image: "u-boot.imz",
					},
				},
			},
		},
	})

	c.Assert(systemLv.Volume.Bootloader, Equals, "u-boot")
	// ubuntu-seed, ubuntu-save, ubuntu-boot and ubuntu-data
	c.Assert(systemLv.LaidOutStructure, HasLen, 4)
}

func (s *gadgetYamlTestSuite) TestLaidOutVolumesFromGadgetHappy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlPC, 0644)
	c.Assert(err, IsNil)
	for _, fn := range []string{"pc-boot.img", "pc-core.img"} {
		err = ioutil.WriteFile(filepath.Join(s.dir, fn), nil, 0644)
		c.Assert(err, IsNil)
	}

	systemLv, all, err := gadget.LaidOutVolumesFromGadget(s.dir, "", coreMod)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Assert(all["pc"], DeepEquals, systemLv)
	c.Assert(systemLv.Volume.Bootloader, Equals, "grub")
	// mbr, bios-boot, efi-system
	c.Assert(systemLv.LaidOutStructure, HasLen, 3)
}

func (s *gadgetYamlTestSuite) TestLaidOutVolumesFromGadgetNeedsModel(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlPC, 0644)
	c.Assert(err, IsNil)
	for _, fn := range []string{"pc-boot.img", "pc-core.img"} {
		err = ioutil.WriteFile(filepath.Join(s.dir, fn), nil, 0644)
		c.Assert(err, IsNil)
	}

	// need the model in order to lay out system volumes due to the verification
	// and other metadata we use with the gadget
	_, _, err = gadget.LaidOutVolumesFromGadget(s.dir, "", nil)
	c.Assert(err, ErrorMatches, "internal error: must have model to lay out system volumes from a gadget")
}

func (s *gadgetYamlTestSuite) TestLaidOutVolumesFromGadgetUC20Happy(c *C) {
	err := ioutil.WriteFile(s.gadgetYamlPath, gadgetYamlUC20PC, 0644)
	c.Assert(err, IsNil)
	for _, fn := range []string{"pc-boot.img", "pc-core.img"} {
		err = ioutil.WriteFile(filepath.Join(s.dir, fn), nil, 0644)
		c.Assert(err, IsNil)
	}

	systemLv, all, err := gadget.LaidOutVolumesFromGadget(s.dir, "", uc20Mod)
	c.Assert(err, IsNil)
	c.Assert(all, HasLen, 1)
	c.Assert(all["pc"], DeepEquals, systemLv)
	c.Assert(systemLv.Volume.Bootloader, Equals, "grub")
	// mbr, bios-boot, ubuntu-seed, ubuntu-save, ubuntu-boot, and ubuntu-data
	c.Assert(systemLv.LaidOutStructure, HasLen, 6)
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

	// if model is nil, we allow a missing gadget.yaml
	_, err = gadget.ReadInfoFromSnapFile(snapf, nil)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfoFromSnapFile(snapf, &modelCharateristics{})
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
				Name:       "pc",
				Bootloader: "grub",
				Schema:     "gpt",
			},
		},
	})
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlFromSnapFileNoVolumesSystemSeed(c *C) {
	snapPath := snaptest.MakeTestSnapWithFiles(c, mockSnapYaml, [][]string{
		{"meta/gadget.yaml", string(minimalMockGadgetYaml)},
	})
	snapf, err := snapfile.Open(snapPath)
	c.Assert(err, IsNil)

	_, err = gadget.ReadInfoFromSnapFile(snapf, &modelCharateristics{systemSeed: true})
	c.Check(err, ErrorMatches, "model requires system-seed partition, but no system-seed or system-data partition found")
}

type gadgetCompatibilityTestSuite struct{}

var _ = Suite(&gadgetCompatibilityTestSuite{})

func (s *gadgetCompatibilityTestSuite) TestGadgetIsCompatibleSelf(c *C) {
	giPC1, err := gadget.InfoFromGadgetYaml(gadgetYamlPC, coreMod)
	c.Assert(err, IsNil)
	giPC2, err := gadget.InfoFromGadgetYaml(gadgetYamlPC, coreMod)
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
	var mockNewStructuresYaml = []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
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
		{mockNewStructuresYaml, `incompatible layout change: incompatible change in the number of structures from 0 to 1`},
		{mockBadIDYaml, "incompatible layout change: incompatible ID change from 0C to 0D"},
		{mockSchemaYaml, "incompatible layout change: incompatible schema change from mbr to gpt"},
		{mockBootloaderYaml, "incompatible layout change: incompatible bootloader change from u-boot to grub"},
	} {
		c.Logf("trying: %v\n", string(tc.gadgetYaml))
		gi, err := gadget.InfoFromGadgetYaml(mockYaml, coreMod)
		c.Assert(err, IsNil)
		giNew, err := gadget.InfoFromGadgetYaml(tc.gadgetYaml, coreMod)
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
		gi, err := gadget.InfoFromGadgetYaml([]byte(mockYaml), coreMod)
		c.Assert(err, IsNil)
		giNew, err := gadget.InfoFromGadgetYaml([]byte(tc.gadgetYaml), coreMod)
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

	gi, err := gadget.InfoFromGadgetYaml([]byte(mockYaml), coreMod)
	c.Assert(err, IsNil)
	giNew, err := gadget.InfoFromGadgetYaml([]byte(mockMBRNameOkYaml), coreMod)
	c.Assert(err, IsNil)
	err = gadget.IsCompatible(gi, giNew)
	c.Check(err, IsNil)
}

const cmdlineMultiLineWithComments = `
# reboot 5 seconds after panic
panic=5
# reserve range
reserve=0x300,32
foo=bar     baz=baz
# random op
                                  random=op
debug
# snapd logging level to debug (does not trip the disallowed argument check)
# or this snapd_ or this snapd.
snapd.debug=1
# this is valid
memmap=100M@2G,100M#3G,1G!1024G
`

func (s *gadgetYamlTestSuite) TestKernelCommandLineBasic(c *C) {
	for _, tc := range []struct {
		files [][]string

		cmdline string
		full    bool
		err     string
	}{{
		files: [][]string{
			{"cmdline.extra", "   foo bar baz just-extra\n"},
		},
		cmdline: "foo bar baz just-extra", full: false,
	}, {
		files: [][]string{
			{"cmdline.full", "    foo bar baz full\n"},
		},
		cmdline: "foo bar baz full", full: true,
	}, {
		files: [][]string{
			{"cmdline.full", cmdlineMultiLineWithComments},
		},
		cmdline: "panic=5 reserve=0x300,32 foo=bar baz=baz random=op debug snapd.debug=1 memmap=100M@2G,100M#3G,1G!1024G",
		full:    true,
	}, {
		files: [][]string{
			{"cmdline.full", ""},
		},
		cmdline: "",
		full:    true,
	}, {
		// no cmdline
		files: nil,
		err:   "no kernel command line in the gadget",
	}, {
		// not what we are looking for
		files: [][]string{
			{"cmdline.other", `ignored`},
		},
		err: "no kernel command line in the gadget",
	}, {
		files: [][]string{{"cmdline.full", " # error"}},
		full:  true, err: `invalid kernel command line in cmdline\.full: unexpected or invalid use of # in argument "#"`,
	}, {
		files: [][]string{{"cmdline.full", "foo bar baz #error"}},
		full:  true, err: `invalid kernel command line in cmdline\.full: unexpected or invalid use of # in argument "#error"`,
	}, {
		files: [][]string{
			{"cmdline.full", "foo bad =\n"},
		},
		full: true, err: `invalid kernel command line in cmdline\.full: unexpected assignment`,
	}, {
		files: [][]string{
			{"cmdline.extra", "foo bad ="},
		},
		full: false, err: `invalid kernel command line in cmdline\.extra: unexpected assignment`,
	}, {
		files: [][]string{
			{"cmdline.extra", `extra`},
			{"cmdline.full", `full`},
		},
		err: "cannot support both extra and full kernel command lines",
	}} {
		c.Logf("files: %q", tc.files)
		snapPath := snaptest.MakeTestSnapWithFiles(c, string(mockSnapYaml), tc.files)
		cmdline, full, err := gadget.KernelCommandLineFromGadget(snapPath)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
			c.Check(cmdline, Equals, "")
			c.Check(full, Equals, tc.full)
		} else {
			c.Assert(err, IsNil)
			c.Check(cmdline, Equals, tc.cmdline)
			c.Check(full, Equals, tc.full)
		}
	}
}

func (s *gadgetYamlTestSuite) testKernelCommandLineArgs(c *C, whichCmdline string) {
	c.Logf("checking %v", whichCmdline)
	// mock test snap creates a snap directory
	info := snaptest.MockSnapWithFiles(c, string(mockSnapYaml),
		&snap.SideInfo{Revision: snap.R(1234)},
		[][]string{
			{whichCmdline, "## TO BE FILLED BY TEST ##"},
		})

	allowedArgs := []string{
		"debug", "panic", "panic=-1",
		"snapd.debug=1", "snapd.debug",
		"serial=ttyS0,9600n8",
	}

	for _, arg := range allowedArgs {
		c.Logf("trying allowed arg: %q", arg)
		err := ioutil.WriteFile(filepath.Join(info.MountDir(), whichCmdline), []byte(arg), 0644)
		c.Assert(err, IsNil)

		cmdline, _, err := gadget.KernelCommandLineFromGadget(info.MountDir())
		c.Assert(err, IsNil)
		c.Check(cmdline, Equals, arg)
	}

	disallowedArgs := []string{
		"snapd_recovery_mode", "snapd_recovery_mode=recover",
		"snapd_recovery_system", "snapd_recovery_system=", "snapd_recovery_system=1234",
		"root", "root=/foo", "nfsroot=127.0.0.1:/foo",
		"root=123=123",
		"panic root", // chokes on root
		"init", "init=/bin/bash",
	}

	for _, arg := range disallowedArgs {
		c.Logf("trying disallowed arg: %q", arg)
		err := ioutil.WriteFile(filepath.Join(info.MountDir(), whichCmdline), []byte(arg), 0644)
		c.Assert(err, IsNil)

		cmdline, _, err := gadget.KernelCommandLineFromGadget(info.MountDir())
		c.Assert(err, ErrorMatches, fmt.Sprintf(`invalid kernel command line in %v: disallowed kernel argument ".*"`, whichCmdline))
		c.Check(cmdline, Equals, "")
	}
}

func (s *gadgetYamlTestSuite) TestKernelCommandLineArgsExtra(c *C) {
	s.testKernelCommandLineArgs(c, "cmdline.extra")
}

func (s *gadgetYamlTestSuite) TestKernelCommandLineArgsFull(c *C) {
	s.testKernelCommandLineArgs(c, "cmdline.full")
}

var mockDeviceLayout = gadget.OnDiskVolume{
	Structure: []gadget.OnDiskStructure{
		// Note that the first ondisk structure we have is BIOS Boot, even
		// though "in reality" the first ondisk structure is MBR, but the MBR
		// doesn't actually show up in /dev at all, so we don't ever measure it
		// as existing on the disk - the code and test accounts for the MBR
		// structure not being present in the OnDiskVolume
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Name: "BIOS Boot",
					Size: 1 * quantity.SizeMiB,
				},
				StartOffset: 1 * quantity.OffsetMiB,
			},
			Node: "/dev/node1",
		},
	},
	ID:         "anything",
	Device:     "/dev/node",
	Schema:     "gpt",
	Size:       2 * quantity.SizeGiB,
	SectorSize: 512,

	// ( 2 GB / 512 B sector size ) - 33 typical GPT header backup sectors +
	// 1 sector to get the exclusive end
	UsableSectorsEnd: uint64((2*quantity.SizeGiB/512)-33) + 1,
}

const mockSimpleGadgetYaml = `volumes:
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

const mockExtraNonInstallableStructure = `
      - name: foobar
        filesystem-label: the-great-foo
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

func (s *gadgetYamlTestSuite) TestLayoutCompatibilityExtraLaidOutStructureNotOnDisk(c *C) {
	// with an extra non-installable structure in the YAML that is not present
	// on disk, we are not compatible
	gadgetLayout, err := gadgettest.LayoutFromYaml(c.MkDir(), mockSimpleGadgetYaml+mockExtraNonInstallableStructure, nil)
	c.Assert(err, IsNil)
	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &mockDeviceLayout, nil)
	c.Assert(err, ErrorMatches, `cannot find gadget structure #2 \("foobar"\) on disk`)

	// note we don't test adding a non-matching structure, since that is already
	// handled in other tests, if we added a non-matching structure the failure
	// will be handled in the first loop checking that all ondisk structures
	// belong to something in the YAML and that will fail, it will not get to
	// the second loop which is what this test is about.
}

func (s *gadgetYamlTestSuite) TestLayoutCompatibilityMBRStructureAllowedMissingWithStruct(c *C) {
	// we are compatible with the MBR structure in the YAML not present in the
	// ondisk structure

	gadgetLayout, err := gadgettest.LayoutFromYaml(c.MkDir(), mockSimpleGadgetYaml, nil)
	c.Assert(err, IsNil)

	// ensure the first structure is the MBR in the YAML, but the first
	// structure in the device layout is BIOS Boot
	c.Assert(gadgetLayout.LaidOutStructure[0].Role, Equals, "mbr")
	c.Assert(mockDeviceLayout.Structure[0].Name, Equals, "BIOS Boot")

	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &mockDeviceLayout, nil)
	c.Assert(err, IsNil)

	// still okay even with strict options - the absence of the MBR in the
	// ondisk volume is allowed
	opts := &gadget.EnsureLayoutCompatibilityOptions{AssumeCreatablePartitionsCreated: true}
	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &mockDeviceLayout, opts)
	c.Assert(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestLayoutCompatibilityTypeBareStructureAllowedMissingWithStruct(c *C) {
	// we are compatible with the type: bare structure in the YAML not present
	// in the ondisk structure

	const typeBareYAML = `volumes:
  foo:
    bootloader: u-boot
    structure:
      - name: barething
        type: bare
        size: 4096
      - name: some-filesystem
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`

	simpleDeviceLayout := gadget.OnDiskVolume{
		Structure: []gadget.OnDiskStructure{
			// Note that the first ondisk structure we have is not barething,
			// even though "in reality" the first ondisk structure is MBR, but the MBR
			// doesn't actually show up in /dev at all, so we don't ever measure it
			// as existing on the disk - the code and test accounts for the MBR
			// structure not being present in the OnDiskVolume
			{
				LaidOutStructure: gadget.LaidOutStructure{
					VolumeStructure: &gadget.VolumeStructure{
						Name:       "some-filesystem",
						Size:       1 * quantity.SizeGiB,
						Filesystem: "ext4",
					},
					StartOffset: 1*quantity.OffsetMiB + 4096,
				},
				Node: "/dev/node1",
			},
		},
		ID:         "anything",
		Device:     "/dev/node",
		Schema:     "gpt",
		Size:       2 * quantity.SizeGiB,
		SectorSize: 512,

		// ( 2 GB / 512 B sector size ) - 33 typical GPT header backup sectors +
		// 1 sector to get the exclusive end
		UsableSectorsEnd: uint64((2*quantity.SizeGiB/512)-33) + 1,
	}

	gadgetLayout, err := gadgettest.LayoutFromYaml(c.MkDir(), typeBareYAML, nil)
	c.Assert(err, IsNil)

	// ensure the first structure is barething in the YAML, but the first
	// structure in the device layout is some-filesystem
	c.Assert(gadgetLayout.LaidOutStructure[0].Type, Equals, "bare")
	c.Assert(simpleDeviceLayout.Structure[0].Name, Equals, "some-filesystem")

	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &simpleDeviceLayout, nil)
	c.Assert(err, IsNil)

	// still okay even with strict options - the absence of the bare structure
	// in the ondisk volume is allowed
	opts := &gadget.EnsureLayoutCompatibilityOptions{AssumeCreatablePartitionsCreated: true}
	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &simpleDeviceLayout, opts)
	c.Assert(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestLayoutCompatibility(c *C) {
	// same contents (the locally created structure should be ignored)
	gadgetLayout, err := gadgettest.LayoutFromYaml(c.MkDir(), mockSimpleGadgetYaml, nil)
	c.Assert(err, IsNil)
	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &mockDeviceLayout, nil)
	c.Assert(err, IsNil)

	// layout still compatible with a larger disk sector size
	mockDeviceLayout.SectorSize = 4096
	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &mockDeviceLayout, nil)
	c.Assert(err, IsNil)

	// layout not compatible with a sector size that's not a factor of the
	// structure sizes
	mockDeviceLayout.SectorSize = 513
	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &mockDeviceLayout, nil)
	c.Assert(err, ErrorMatches, `gadget volume structure #1 \(\"BIOS Boot\"\) size is not a multiple of disk sector size 513`)

	// reset for the rest of the test
	mockDeviceLayout.SectorSize = 512

	// missing structure (that's ok with default opts)
	gadgetLayoutWithExtras, err := gadgettest.LayoutFromYaml(c.MkDir(), mockSimpleGadgetYaml+mockExtraStructure, nil)
	c.Assert(err, IsNil)
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &mockDeviceLayout, nil)
	c.Assert(err, IsNil)

	// with strict opts, not okay
	opts := &gadget.EnsureLayoutCompatibilityOptions{AssumeCreatablePartitionsCreated: true}
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &mockDeviceLayout, opts)
	c.Assert(err, ErrorMatches, `cannot find gadget structure #2 \("Writable"\) on disk`)

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
			Node: "/dev/node2",
		},
	)
	// extra structure (should fail)
	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &deviceLayoutWithExtras, nil)
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2.* in gadget`)

	// layout is not compatible if the device is too small
	smallDeviceLayout := mockDeviceLayout
	smallDeviceLayout.UsableSectorsEnd = uint64(100 * quantity.SizeMiB / 512)

	// sanity check
	c.Check(gadgetLayoutWithExtras.Size > quantity.Size(smallDeviceLayout.UsableSectorsEnd*uint64(smallDeviceLayout.SectorSize)), Equals, true)
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &smallDeviceLayout, nil)
	c.Assert(err, ErrorMatches, `device /dev/node \(last usable byte at 100 MiB\) is too small to fit the requested layout \(1\.17 GiB\)`)
}

func (s *gadgetYamlTestSuite) TestMBRLayoutCompatibility(c *C) {
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
		ID:               "anything",
		Device:           "/dev/node",
		Schema:           "dos",
		Size:             2 * quantity.SizeGiB,
		UsableSectorsEnd: uint64(2*quantity.SizeGiB/512 - 34 + 1),
		SectorSize:       512,
	}
	gadgetLayout, err := gadgettest.LayoutFromYaml(c.MkDir(), mockMBRGadgetYaml, nil)
	c.Assert(err, IsNil)
	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &mockMBRDeviceLayout, nil)
	c.Assert(err, IsNil)
	// structure is missing from disk
	gadgetLayoutWithExtras, err := gadgettest.LayoutFromYaml(c.MkDir(), mockMBRGadgetYaml+mockExtraStructure, nil)
	c.Assert(err, IsNil)
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &mockMBRDeviceLayout, nil)
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
			Node: "/dev/node2",
		},
	)
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayoutWithExtras, nil)
	c.Assert(err, IsNil)

	// test with a larger sector size that is still an even multiple of the
	// structure sizes in the gadget
	mockMBRDeviceLayout.SectorSize = 4096
	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &mockMBRDeviceLayout, nil)
	c.Assert(err, IsNil)

	// but with a sector size that is not an even multiple of the structure size
	// then we have an error
	mockMBRDeviceLayout.SectorSize = 513
	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &mockMBRDeviceLayout, nil)
	c.Assert(err, ErrorMatches, `gadget volume structure #1 \(\"BIOS Boot\"\) size is not a multiple of disk sector size 513`)

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
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayoutWithExtras, nil)
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node4 \(starting at 1260388352\) in gadget: start offsets do not match \(disk: 1260388352 \(1.17 GiB\) and gadget: 2097152 \(2 MiB\)\)`)
}

func (s *gadgetYamlTestSuite) TestLayoutCompatibilityWithCreatedPartitions(c *C) {
	gadgetLayoutWithExtras, err := gadgettest.LayoutFromYaml(c.MkDir(), mockSimpleGadgetYaml+mockExtraStructure, nil)
	c.Assert(err, IsNil)
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
			Node: "/dev/node2",
		},
	)

	// with no/default opts, then they are compatible
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayout, nil)
	c.Assert(err, IsNil)

	// but strict compatibility check, assuming that the creatable partitions
	// have already been created will fail
	opts := &gadget.EnsureLayoutCompatibilityOptions{AssumeCreatablePartitionsCreated: true}
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayout, opts)
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2 \(starting at 2097152\) in gadget: filesystems do not match \(expected something_else from gadget.yaml, got ext4\)`)

	// we are going to manipulate last structure, which has system-data role
	c.Assert(gadgetLayoutWithExtras.Structure[len(gadgetLayoutWithExtras.Structure)-1].Role, Equals, gadget.SystemData)

	// change the role for the laid out volume to not be a partition role that
	// is created at install time (note that the duplicated seed role here is
	// technically incorrect, you can't have duplicated roles, but this
	// demonstrates that a structure that otherwise fits the bill but isn't a
	// role that is created during install will fail the filesystem match check)
	gadgetLayoutWithExtras.Structure[len(gadgetLayoutWithExtras.Structure)-1].Role = gadget.SystemSeed

	// now we fail to find the /dev/node2 structure from the gadget on disk
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayout, nil)
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2 \(starting at 2097152\) in gadget: filesystems do not match \(expected something_else from gadget.yaml, got ext4\) and the partition is not creatable at install`)

	// undo the role change
	gadgetLayoutWithExtras.Structure[len(gadgetLayoutWithExtras.Structure)-1].Role = gadget.SystemData

	// change the gadget size to be bigger than the on disk size
	gadgetLayoutWithExtras.Structure[len(gadgetLayoutWithExtras.Structure)-1].Size = 10000000 * quantity.SizeMiB

	// now we fail to find the /dev/node2 structure from the gadget on disk because the gadget says it must be bigger
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayout, nil)
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2 \(starting at 2097152\) in gadget: on disk size 1258291200 \(1.17 GiB\) is smaller than gadget size 10485760000000 \(9.54 TiB\)`)

	// change the gadget size to be smaller than the on disk size and the role to be one that is not expanded
	gadgetLayoutWithExtras.Structure[len(gadgetLayoutWithExtras.Structure)-1].Size = 1 * quantity.SizeMiB
	gadgetLayoutWithExtras.Structure[len(gadgetLayoutWithExtras.Structure)-1].Role = gadget.SystemBoot

	// now we fail because the gadget says it should be smaller and it can't be expanded
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayout, nil)
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2 \(starting at 2097152\) in gadget: on disk size 1258291200 \(1.17 GiB\) is larger than gadget size 1048576 \(1 MiB\) \(and the role should not be expanded\)`)

	// but a smaller partition on disk for SystemData role is okay
	gadgetLayoutWithExtras.Structure[len(gadgetLayoutWithExtras.Structure)-1].Role = gadget.SystemData
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithExtras, &deviceLayout, nil)
	c.Assert(err, IsNil)
}

const mockExtraNonInstallableStructureWithoutFilesystem = `
      - name: foobar
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

func (s *gadgetYamlTestSuite) TestLayoutCompatibilityWithUnspecifiedGadgetFilesystemOnDiskHasFilesystem(c *C) {
	gadgetLayoutWithNonInstallableStructureWithoutFs, err := gadgettest.LayoutFromYaml(c.MkDir(), mockSimpleGadgetYaml+mockExtraNonInstallableStructureWithoutFilesystem, nil)
	c.Assert(err, IsNil)
	deviceLayout := mockDeviceLayout

	// device matches, but it has a filesystem
	deviceLayout.Structure = append(deviceLayout.Structure,
		gadget.OnDiskStructure{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Name:       "foobar",
					Size:       1200 * quantity.SizeMiB,
					Label:      "whatever",
					Filesystem: "something",
				},
				StartOffset: 2 * quantity.OffsetMiB,
			},
			Node: "/dev/node2",
		},
	)

	// with no/default opts, then they are compatible
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithNonInstallableStructureWithoutFs, &deviceLayout, nil)
	c.Assert(err, IsNil)

	// still compatible with strict opts
	opts := &gadget.EnsureLayoutCompatibilityOptions{AssumeCreatablePartitionsCreated: true}
	err = gadget.EnsureLayoutCompatibility(gadgetLayoutWithNonInstallableStructureWithoutFs, &deviceLayout, opts)
	c.Assert(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestLayoutCompatibilityWithImplicitSystemData(c *C) {
	gadgetLayout, err := gadgettest.LayoutFromYaml(c.MkDir(), mockUC16YAMLImplicitSystemData, nil)
	c.Assert(err, IsNil)
	deviceLayout := uc16DeviceLayout

	// with no/default opts, then they are not compatible
	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &deviceLayout, nil)
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/sda3 \(starting at 54525952\) in gadget`)

	// compatible with AllowImplicitSystemData however
	opts := &gadget.EnsureLayoutCompatibilityOptions{
		AllowImplicitSystemData: true,
	}
	err = gadget.EnsureLayoutCompatibility(gadgetLayout, &deviceLayout, opts)
	c.Assert(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestSchemaCompatibility(c *C) {
	gadgetLayout, err := gadgettest.LayoutFromYaml(c.MkDir(), mockSimpleGadgetYaml, nil)
	c.Assert(err, IsNil)
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
		err := gadget.EnsureLayoutCompatibility(gadgetLayout, &deviceLayout, nil)
		if tc.e == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.e)
		}
	}
	c.Logf("-----")
}

func (s *gadgetYamlTestSuite) TestIDCompatibility(c *C) {
	gadgetLayout, err := gadgettest.LayoutFromYaml(c.MkDir(), mockSimpleGadgetYaml, nil)
	c.Assert(err, IsNil)
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
		err := gadget.EnsureLayoutCompatibility(gadgetLayout, &deviceLayout, nil)
		if tc.e == "" {
			c.Assert(err, IsNil)
		} else {
			c.Assert(err, ErrorMatches, tc.e)
		}
	}
	c.Logf("-----")
}

func (s *gadgetYamlTestSuite) TestSaveLoadDiskVolumeDeviceTraits(c *C) {
	// example output from a real installed VM
	m := map[string]gadget.DiskVolumeDeviceTraits{
		"foo": {
			OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb",
			OriginalKernelPath: "/dev/vdb",
			DiskID:             "484B4BA1-3EDF-4270-A1A8-378FCBB0E1DE",
			Size:               10 * quantity.SizeGiB,
			SectorSize:         quantity.Size(512),
			Schema:             "gpt",
			Structure: []gadget.DiskStructureDeviceTraits{
				// first structure is a bare structure with no filesystem
				{
					OriginalDevicePath: "/dev/vdb1",
					OriginalKernelPath: "/sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb/vdb1",
					PartitionUUID:      "C06F16ED-A587-4D0E-8EE4-2C3AE8BECE68",
					PartitionLabel:     "barething",
					PartitionType:      "EBBEADAF-22C9-E33B-8F5D-0E81686A68CB",
					Offset:             0x100000,
					Size:               0x1000,
				},
				// this one has a filesystem though
				{
					OriginalDevicePath: "/dev/vdb2",
					OriginalKernelPath: "/sys/devices/pci0000:00/0000:00:04.0/virtio2/block/vdb/vdb2",
					PartitionUUID:      "48ECAEB8-8DD0-41BB-A7A8-5E12FC5985FD",
					PartitionLabel:     "some-filesystem",
					PartitionType:      "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					FilesystemUUID:     "f384e18c-56a3-458a-ac80-4cc29b3d69d9",
					FilesystemLabel:    "some-filesystem",
					FilesystemType:     "ext4",
					Offset:             0x101000,
					Size:               0x40000000,
				},
			},
		},
		"pc": {
			OriginalDevicePath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda",
			OriginalKernelPath: "/dev/vda",
			DiskID:             "46E2573B-7891-4316-B83C-DE0817A7CFB5",
			Schema:             "gpt",
			Structure: []gadget.DiskStructureDeviceTraits{
				{
					OriginalDevicePath: "/dev/vda1",
					OriginalKernelPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda1",
					PartitionUUID:      "21EF798E-4AEE-4941-9AF4-7277437F752F",
					PartitionLabel:     "BIOS\\x20Boot",
					PartitionType:      "21686148-6449-6E6F-744E-656564454649",
					Offset:             0x100000,
					Size:               0x1b8,
				},
				{
					OriginalDevicePath: "/dev/vda2",
					OriginalKernelPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda2",
					PartitionUUID:      "F3C5B560-EF24-48A5-862B-361BCD180464",
					PartitionLabel:     "ubuntu-seed",
					PartitionType:      "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					FilesystemUUID:     "EC87-231A",
					FilesystemLabel:    "ubuntu-seed",
					FilesystemType:     "vfat",
					Offset:             0x200000,
					Size:               0x100000,
				},
				{
					OriginalDevicePath: "/dev/vda3",
					OriginalKernelPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda3",
					PartitionUUID:      "CEDA6CFC-B019-0F4F-9FCE-9A41FF1D444A",
					PartitionLabel:     "ubuntu-boot",
					PartitionType:      "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					FilesystemUUID:     "922f6d2b-520b-4213-8691-81ace98009ff",
					FilesystemLabel:    "ubuntu-boot",
					FilesystemType:     "ext4",
					Offset:             0x4b200000,
					Size:               0x4b000000,
				},
				{
					OriginalDevicePath: "/dev/vda4",
					OriginalKernelPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda4",
					PartitionUUID:      "902A51E4-7B50-EF4C-B3DF-4D2B1E73307B",
					PartitionLabel:     "ubuntu-save",
					PartitionType:      "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					FilesystemUUID:     "7a72b2be-753a-4ce5-ab71-189f4b832ff5",
					FilesystemLabel:    "ubuntu-save",
					FilesystemType:     "ext4",
					Offset:             0x7a000000,
					Size:               0x2ee00000,
				},
				{
					OriginalDevicePath: "/dev/vda5",
					OriginalKernelPath: "/sys/devices/pci0000:00/0000:00:03.0/virtio1/block/vda/vda5",
					PartitionUUID:      "C1C2C91D-9C00-A045-9C8F-A8779BDA5E74",
					PartitionLabel:     "ubuntu-data",
					PartitionType:      "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					FilesystemUUID:     "b0a2e964-7bfc-4fbf-b48a-1fdca17b0539",
					FilesystemLabel:    "ubuntu-data",
					FilesystemType:     "ext4",
					Offset:             0x7b000000,
					Size:               0x1000000,
				},
			},
		},
	}

	// when there is no mapping file, it is not an error, the map returned is
	// just nil/has no items in it
	mAbsent, err := gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDir)
	c.Assert(err, IsNil)
	c.Assert(mAbsent, HasLen, 0)

	// load looks in SnapDeviceDir since it is meant to be used during run mode
	// when /var/lib/snapd/device/disk-mapping.json is the real version from
	// ubuntu-data, but during install mode, we will need to save to the host
	// ubuntu-data which is not located at /run/mnt/data or
	// /var/lib/snapd/device, but rather
	// /run/mnt/ubuntu-data/system-data/var/lib/snapd/device so this takes a
	// directory argument when we save it
	err = gadget.SaveDiskVolumesDeviceTraits(dirs.SnapDeviceDir, m)
	c.Assert(err, IsNil)

	// now that it was saved to dirs.SnapDeviceDir, we can load it correctly
	m2, err := gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDir)
	c.Assert(err, IsNil)

	c.Assert(m, DeepEquals, m2)

	// example output from a Raspi - write the JSON out manually so we can catch
	// regressions between JSON -> go object importing
	const piJson = `
{
  "pi": {
    "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
    "kernel-path": "/dev/mmcblk0",
    "disk-id": "7c301cbd",
    "size": 32010928128,
    "sector-size": 512,
    "schema": "dos",
    "structure": [
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p1",
        "kernel-path": "/dev/mmcblk0p1",
        "partition-uuid": "7c301cbd-01",
        "partition-label": "",
        "partition-type": "0C",
        "filesystem-label": "ubuntu-seed",
        "filesystem-uuid": "0E09-0822",
        "filesystem-type": "vfat",
        "offset": 1048576,
        "size": 1258291200
      },
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p2",
        "kernel-path": "/dev/mmcblk0p2",
        "partition-uuid": "7c301cbd-02",
        "partition-label": "",
        "partition-type": "0C",
        "filesystem-label": "ubuntu-boot",
        "filesystem-uuid": "23F9-881F",
        "filesystem-type": "vfat",
        "offset": 1259339776,
        "size": 786432000
      },
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3",
        "kernel-path": "/dev/mmcblk0p3",
        "partition-uuid": "7c301cbd-03",
        "partition-label": "",
        "partition-type": "83",
        "filesystem-label": "ubuntu-save",
        "filesystem-uuid": "1cdd5826-e9de-4d27-83f7-20249e710590",
        "filesystem-type": "ext4",
        "offset": 2045771776,
        "size": 16777216
      },
      {
        "device-path": "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p4",
        "kernel-path": "/dev/mmcblk0p4",
        "partition-uuid": "7c301cbd-04",
        "partition-label": "",
        "partition-type": "83",
        "filesystem-label": "ubuntu-data",
        "filesystem-uuid": "d7f39661-1da0-48de-8967-ce41343d4345",
        "filesystem-type": "ext4",
        "offset": 2062548992,
        "size": 29948379136
      }
    ]
  }
}
`

	const oneMeg = 1024 * 1024

	expPiMap := map[string]gadget.DiskVolumeDeviceTraits{
		"pi": {
			OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0",
			OriginalKernelPath: "/dev/mmcblk0",
			DiskID:             "7c301cbd",
			Size:               30528 * oneMeg, // ~ 32 GB SD card
			SectorSize:         512,
			Schema:             "dos",
			Structure: []gadget.DiskStructureDeviceTraits{
				{
					OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p1",
					OriginalKernelPath: "/dev/mmcblk0p1",
					PartitionUUID:      "7c301cbd-01",
					PartitionType:      "0C",
					FilesystemUUID:     "0E09-0822",
					FilesystemLabel:    "ubuntu-seed",
					FilesystemType:     "vfat",
					Offset:             oneMeg,
					Size:               (1200) * oneMeg,
				},
				{
					OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p2",
					OriginalKernelPath: "/dev/mmcblk0p2",
					PartitionUUID:      "7c301cbd-02",
					PartitionType:      "0C",
					FilesystemUUID:     "23F9-881F",
					FilesystemLabel:    "ubuntu-boot",
					FilesystemType:     "vfat",
					Offset:             (1 + 1200) * oneMeg,
					Size:               (750) * oneMeg,
				},
				{
					OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p3",
					OriginalKernelPath: "/dev/mmcblk0p3",
					PartitionUUID:      "7c301cbd-03",
					PartitionType:      "83",
					FilesystemUUID:     "1cdd5826-e9de-4d27-83f7-20249e710590",
					FilesystemType:     "ext4",
					FilesystemLabel:    "ubuntu-save",
					Offset:             (1 + 1200 + 750) * oneMeg,
					Size:               16 * oneMeg,
				},
				{
					OriginalDevicePath: "/sys/devices/platform/emmc2bus/fe340000.emmc2/mmc_host/mmc0/mmc0:0001/block/mmcblk0/mmcblk0p4",
					OriginalKernelPath: "/dev/mmcblk0p4",
					PartitionUUID:      "7c301cbd-04",
					PartitionType:      "83",
					FilesystemUUID:     "d7f39661-1da0-48de-8967-ce41343d4345",
					FilesystemLabel:    "ubuntu-data",
					FilesystemType:     "ext4",
					Offset:             (1 + 1200 + 750 + 16) * oneMeg,
					// total size - offset of last structure
					Size: (30528 - (1 + 1200 + 750 + 16)) * oneMeg,
				},
			},
		},
	}

	err = ioutil.WriteFile(filepath.Join(dirs.SnapDeviceDir, "disk-mapping.json"), []byte(piJson), 0644)
	c.Assert(err, IsNil)

	m3, err := gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDir)
	c.Assert(err, IsNil)

	c.Assert(m3, DeepEquals, expPiMap)
}

// adapted from https://github.com/snapcore/pc-amd64-gadget/blob/16/gadget.yaml
// but without the content
const mockUC16YAMLImplicitSystemData = `volumes:
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
      - name: EFI System
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        filesystem: vfat
        filesystem-label: system-boot
        size: 50M
`

// uc16 layout from a VM that has an implicit system-data as the third
// partition
var uc16DeviceLayout = gadget.OnDiskVolume{
	Structure: []gadget.OnDiskStructure{
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Name: "BIOS Boot",
					Type: "21686148-6449-6E6F-744E-656564454649",
					ID:   "b2e891ee-b971-4a2b-b874-694bbf9b821a",
					Size: 1048576,
				},
				StartOffset: 1048576,
			},
			DiskIndex: 1,
			Node:      "/dev/sda1",
			Size:      1048576,
		},
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Name:       "EFI System",
					Type:       "C12A7328-F81F-11D2-BA4B-00A0C93EC93B",
					ID:         "a87e9dcb-b1e1-4eab-89cf-1c2fc057b038",
					Label:      "system-boot",
					Filesystem: "vfat",
					Size:       52428800,
				},
				StartOffset: 2097152,
			},
			DiskIndex: 2,
			Node:      "/dev/sda2",
			Size:      52428800,
		},
		{
			LaidOutStructure: gadget.LaidOutStructure{
				VolumeStructure: &gadget.VolumeStructure{
					Name:       "writable",
					Type:       "0FC63DAF-8483-4772-8E79-3D69D8477DE4",
					ID:         "cba2b8b3-c2e4-4e51-9a57-d35041b7bf9a",
					Label:      "writable",
					Filesystem: "ext4",
					Size:       10682875392,
				},
				StartOffset: 54525952,
			},
			DiskIndex: 3,
			Node:      "/dev/sda3",
			Size:      10682875392,
		},
	},
	ID:               "2a9b0671-4597-433b-b3ad-be99950e8c5e",
	Device:           "/dev/sda",
	Schema:           "gpt",
	Size:             10737418240,
	UsableSectorsEnd: 20971487,
	SectorSize:       512,
}

func (s *gadgetYamlTestSuite) TestOnDiskStructureIsLikelyImplicitSystemDataRoleUC16Implicit(c *C) {
	gadgetLayout, err := gadgettest.LayoutFromYaml(c.MkDir(), mockUC16YAMLImplicitSystemData, nil)
	c.Assert(err, IsNil)
	deviceLayout := uc16DeviceLayout

	// bios boot is not implicit system-data
	matches := gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout, &deviceLayout, deviceLayout.Structure[0])
	c.Assert(matches, Equals, false)

	// EFI system / system-boot is not implicit system-data
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout, &deviceLayout, deviceLayout.Structure[1])
	c.Assert(matches, Equals, false)

	// system-data is though
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, true)

	// the size of the partition does not matter when it comes to being a
	// candidate implicit system-data
	oldSize := deviceLayout.Structure[2].Size
	deviceLayout.Structure[2].Size = 10
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, true)
	deviceLayout.Structure[2].Size = oldSize

	// very large okay too
	deviceLayout.Structure[2].Size = 1000000000000000000
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, true)
	deviceLayout.Structure[2].Size = oldSize

	// if we make system-data not ext4 then it is not
	deviceLayout.Structure[2].Filesystem = "zfs"
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, false)
	deviceLayout.Structure[2].Filesystem = "ext4"

	// if we make the partition type not "Linux filesystem data", then it is not
	deviceLayout.Structure[2].Type = "foo"
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, false)
	deviceLayout.Structure[2].Type = "0FC63DAF-8483-4772-8E79-3D69D8477DE4"

	// if we make the Label not writable, then it is not
	deviceLayout.Structure[2].Label = "foo"
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, false)
	deviceLayout.Structure[2].Label = "writable"

	// if we add another LaidOutStructure Partition to the YAML so that there is
	// not exactly one extra partition on disk compated to the YAML, then it is
	// not
	gadgetLayout.Structure = append(gadgetLayout.Structure, gadget.VolumeStructure{Type: "foo"})
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, false)
	gadgetLayout.Structure = gadgetLayout.Structure[:len(gadgetLayout.Structure)-1]

	// if we make the partition not the last partition, then it is not
	deviceLayout.Structure[2].DiskIndex = 1
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, false)
}

const explicitSystemData = `
      - name: writable
        role: system-data
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        filesystem: ext4
        size: 1G
`

func (s *gadgetYamlTestSuite) TestOnDiskStructureIsLikelyImplicitSystemDataRoleUC16Explicit(c *C) {
	gadgetLayout, err := gadgettest.LayoutFromYaml(c.MkDir(), mockUC16YAMLImplicitSystemData+explicitSystemData, nil)
	c.Assert(err, IsNil)
	deviceLayout := uc16DeviceLayout

	// none of the structures are implicit because we have an explicit
	// system-data role
	for _, volStruct := range deviceLayout.Structure {
		matches := gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout, &deviceLayout, volStruct)
		c.Assert(matches, Equals, false)
	}
}
