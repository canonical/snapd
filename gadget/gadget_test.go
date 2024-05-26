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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/logger"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/osutil/kcmdline"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/strutil"
	"github.com/snapcore/snapd/testutil"
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
        offset: 24576
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
        offset: 24576
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
        filesystem: vfat-32
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

var gadgetYamlMinSizePC = []byte(`
volumes:
  pc:
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
        min-size: 16M
        size: 32M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`)

var gadgetYamlClassicWithModes = []byte(`
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
          edition: 1
        content:
          - image: pc-core.img
      - name: EFI System partition
        role: system-seed-null
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 99M
        update:
          edition: 1
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        offset: 1202M
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

var gadgetYamlUnorderedParts = []byte(`
volumes:
  myvol:
    schema: gpt
    bootloader: lk
    structure:
      - name: ubuntu-seed
        filesystem: ext4
        size: 500M
        type: 0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-seed
      - name: part3
        offset: 800M
        filesystem: ext4
        size: 500M
        role: system-data
        type: 0FC63DAF-8483-4772-8E79-3D69D8477DE4
      - name: part2
        offset: 501M
        filesystem: ext4
        size: 299M
        type: 0FC63DAF-8483-4772-8E79-3D69D8477DE4
`)

func TestRun(t *testing.T) { TestingT(t) }

func mustParseGadgetSize(c *C, s string) quantity.Size {
	gs := mylog.Check2(quantity.ParseSize(s))

	return gs
}

func mustParseGadgetRelativeOffset(c *C, s string) *gadget.RelativeOffset {
	grs := mylog.Check2(gadget.ParseRelativeOffset(s))

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
	// if model is nil, we allow a missing yaml
	_ := mylog.Check2(gadget.ReadInfo("bogus-path", nil))


	_ = mylog.Check2(gadget.ReadInfo("bogus-path", &gadgettest.ModelCharacteristics{}))
	c.Assert(err, ErrorMatches, ".*meta/gadget.yaml: no such file or directory")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicOptional(c *C) {
	// no meta/gadget.yaml
	gi := mylog.Check2(gadget.ReadInfo(s.dir, &gadgettest.ModelCharacteristics{IsClassic: true}))

	c.Check(gi, NotNil)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicEmptyIsValid(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, nil, 0644))


	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, &gadgettest.ModelCharacteristics{IsClassic: true}))

	c.Assert(ginfo, DeepEquals, &gadget.Info{})
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlOnClassicOnylDefaultsIsValid(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, mockClassicGadgetYaml, 0644))


	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, &gadgettest.ModelCharacteristics{IsClassic: true}))

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
	mylog.Check(os.WriteFile(s.gadgetYamlPath, mockClassicGadgetCoreDefaultsYaml, 0644))


	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, &gadgettest.ModelCharacteristics{IsClassic: true}))

	defaults := gadget.SystemDefaults(ginfo.Defaults)
	c.Check(defaults, DeepEquals, map[string]interface{}{
		"ssh.disable": true,
	})

	yaml := string(mockClassicGadgetCoreDefaultsYaml) + `
  system:
    something: true
`
	mylog.Check(os.WriteFile(s.gadgetYamlPath, []byte(yaml), 0644))

	ginfo = mylog.Check2(gadget.ReadInfo(s.dir, &gadgettest.ModelCharacteristics{IsClassic: true}))


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
	mylog.Check(os.WriteFile(s.gadgetYamlPath, []byte(mockGadgetWithEmptyVolumes), 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, nil))
	c.Assert(err, ErrorMatches, `volume "lun-0" stanza is empty`)
}

func (s *gadgetYamlTestSuite) TestReadGadgetDefaultsMultiline(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, mockClassicGadgetMultilineDefaultsYaml, 0644))


	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, &gadgettest.ModelCharacteristics{IsClassic: true}))

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
	classicMod = &gadgettest.ModelCharacteristics{
		IsClassic: true,
	}
	classicWithModesMod = &gadgettest.ModelCharacteristics{
		IsClassic: true,
		HasModes:  true,
	}
	coreMod = &gadgettest.ModelCharacteristics{
		IsClassic: false,
	}
	uc20Mod = &gadgettest.ModelCharacteristics{
		IsClassic: false,
		HasModes:  true,
	}
)

func checkEnclosingPointsToVolume(c *C, vols map[string]*gadget.Volume) {
	// Make sure we have pointers to the right enclosing volume
	for _, v := range vols {
		for sidx := range v.Structure {
			c.Assert(v.Structure[sidx].EnclosingVolume, Equals, v)
		}
	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlValid(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, mockGadgetYaml, 0644))


	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, coreMod))

	expectedgi := &gadget.Info{
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
		},
	}
	gadget.SetEnclosingVolumeInStructs(expectedgi.Volumes)
	c.Assert(ginfo, DeepEquals, expectedgi)
	checkEnclosingPointsToVolume(c, ginfo.Volumes)
}

func (s *gadgetYamlTestSuite) TestReadMultiVolumeGadgetYamlValid(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, mockMultiVolumeGadgetYaml, 0644))


	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, nil))

	c.Check(ginfo.Volumes, HasLen, 2)
	expectedgi := &gadget.Info{
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
						Offset:     asOffsetPtr(gadget.NonMBRStartOffset),
						Size:       mustParseGadgetSize(c, "128M"),
						MinSize:    mustParseGadgetSize(c, "128M"),
						Filesystem: "vfat",
						Type:       "0C",
						Content: []gadget.VolumeContent{
							{
								UnresolvedSource: "splash.bmp",
								Target:           ".",
							},
						},
						YamlIndex: 0,
					},
					{
						VolumeName: "frobinator-image",
						Role:       "system-data",
						Name:       "writable",
						Label:      "writable",
						Offset:     asOffsetPtr(gadget.NonMBRStartOffset + quantity.Offset(mustParseGadgetSize(c, "128M"))),
						Type:       "83",
						Filesystem: "ext4",
						Size:       mustParseGadgetSize(c, "380M"),
						MinSize:    mustParseGadgetSize(c, "380M"),
						YamlIndex:  1,
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
						MinSize:    623000,
						Offset:     asOffsetPtr(24576),
						Content: []gadget.VolumeContent{
							{
								Image: "u-boot.imz",
							},
						},
					},
				},
			},
		},
	}
	gadget.SetEnclosingVolumeInStructs(expectedgi.Volumes)
	c.Assert(ginfo, DeepEquals, expectedgi)
	checkEnclosingPointsToVolume(c, ginfo.Volumes)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlInvalidBootloader(c *C) {
	mockGadgetYamlBroken := []byte(`
volumes:
 name:
  bootloader: silo
`)
	mylog.Check(os.WriteFile(s.gadgetYamlPath, mockGadgetYamlBroken, 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, nil))
	c.Assert(err, ErrorMatches, "bootloader must be one of grub, u-boot, android-boot, piboot or lk")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlEmptyBootloader(c *C) {
	mockGadgetYamlBroken := []byte(`
volumes:
 name:
  bootloader:
`)
	mylog.Check(os.WriteFile(s.gadgetYamlPath, mockGadgetYamlBroken, 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, &gadgettest.ModelCharacteristics{IsClassic: false}))
	c.Assert(err, ErrorMatches, "bootloader not declared in any volume")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlMissingBootloader(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, nil, 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, &gadgettest.ModelCharacteristics{IsClassic: false}))
	c.Assert(err, ErrorMatches, "bootloader not declared in any volume")
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlInvalidDefaultsKey(c *C) {
	mockGadgetYamlBroken := []byte(`
defaults:
 foo:
  x: 1
`)
	mylog.Check(os.WriteFile(s.gadgetYamlPath, mockGadgetYamlBroken, 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, nil))
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
		mylog.Check(os.WriteFile(s.gadgetYamlPath, []byte(mockGadgetYamlBroken), 0644))


		_ = mylog.Check2(gadget.ReadInfo(s.dir, nil))
		c.Check(err, ErrorMatches, t.expectedErr)
	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlVolumeUpdate(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, mockVolumeUpdateGadgetYaml, 0644))


	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, coreMod))
	c.Check(err, IsNil)
	expectedgi := &gadget.Info{
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
						MinSize:     88888,
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
	}
	gadget.SetEnclosingVolumeInStructs(expectedgi.Volumes)
	c.Assert(ginfo, DeepEquals, expectedgi)
	checkEnclosingPointsToVolume(c, ginfo.Volumes)
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlVolumeUpdateUnhappy(c *C) {
	broken := bytes.Replace(mockVolumeUpdateGadgetYaml, []byte("edition: 5"), []byte("edition: borked"), 1)
	mylog.Check(os.WriteFile(s.gadgetYamlPath, broken, 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, nil))
	c.Check(err, ErrorMatches, `cannot parse gadget metadata: "edition" must be a positive number, not "borked"`)

	broken = bytes.Replace(mockVolumeUpdateGadgetYaml, []byte("edition: 5"), []byte("edition: -5"), 1)
	mylog.Check(os.WriteFile(s.gadgetYamlPath, broken, 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, nil))
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
		mylog.Check(yaml.Unmarshal([]byte(fmt.Sprintf("offset-write: %s", tc.s)), &f))
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
	&gadgettest.ModelCharacteristics{IsClassic: false, HasModes: false},
	&gadgettest.ModelCharacteristics{IsClassic: true, HasModes: false},
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlPCHappy(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, gadgetYamlPC, 0644))


	for _, mod := range classicModelCharacteristics {
		_ = mylog.Check2(gadget.ReadInfo(s.dir, mod))

	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlRPiHappy(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, gadgetYamlRPi, 0644))


	for _, mod := range classicModelCharacteristics {
		_ = mylog.Check2(gadget.ReadInfo(s.dir, mod))

	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlLkHappy(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, gadgetYamlLk, 0644))


	for _, mod := range classicModelCharacteristics {
		_ = mylog.Check2(gadget.ReadInfo(s.dir, mod))

	}
}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlLkUC20Happy(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, gadgetYamlLkUC20, 0644))


	uc20Model := &gadgettest.ModelCharacteristics{
		HasModes:  true,
		IsClassic: false,
	}

	_ = mylog.Check2(gadget.ReadInfo(s.dir, uc20Model))

}

func (s *gadgetYamlTestSuite) TestReadGadgetYamlLkLegacyHappy(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, gadgetYamlLkLegacy, 0644))


	for _, mod := range classicModelCharacteristics {
		_ = mylog.Check2(gadget.ReadInfo(s.dir, mod))

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
		{"aa686148-6449-6e6f-744E-656564454649", "", "gpt"},
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

		vol := &gadget.Volume{Schema: tc.schema}
		mylog.Check(gadget.ValidateVolumeStructure(&gadget.VolumeStructure{Type: tc.s, Size: 123, EnclosingVolume: vol}, vol))
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func mustParseStructureNoImplicit(c *C, s string) *gadget.VolumeStructure {
	var v gadget.VolumeStructure
	mylog.Check(yaml.Unmarshal([]byte(s), &v))

	v.EnclosingVolume = &gadget.Volume{}
	return &v
}

func mustParseStructure(c *C, s string) *gadget.VolumeStructure {
	vs := mustParseStructureNoImplicit(c, s)
	gadget.SetImplicitForVolumeStructure(vs, 0, make(map[string]bool), make(map[string]bool))
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
	vol := &gadget.Volume{Schema: "gpt"}
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
		mylog.Check(gadget.ValidateVolumeStructure(tc.s, tc.v))
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *gadgetYamlTestSuite) TestValidateFilesystem(c *C) {
	vol := &gadget.Volume{Schema: "gpt"}
	for i, tc := range []struct {
		s   string
		err string
	}{
		{"vfat", ""},
		{"vfat-16", ""},
		{"vfat-32", ""},
		{"ext4", ""},
		{"none", ""},
		{"btrfs", `invalid filesystem "btrfs"`},
	} {
		c.Logf("tc: %v %+v", i, tc.s)
		mylog.Check(gadget.ValidateVolumeStructure(&gadget.VolumeStructure{Filesystem: tc.s, Type: "21686148-6449-6E6F-744E-656564454649", Size: 123, EnclosingVolume: vol}, vol))
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
		// invalid
		// A bit redundant it is always set in setImplicitForVolume
		{"", `invalid schema ""`},
		{"some", `invalid schema "some"`},
	} {
		c.Logf("tc: %v %+v", i, tc.s)
		mylog.Check(gadget.ValidateVolume(&gadget.Volume{Name: "name", Schema: tc.s}))
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *gadgetYamlTestSuite) TestValidateVolumePartialSchema(c *C) {
	mylog.Check(gadget.ValidateVolume(&gadget.Volume{Name: "name", Schema: "", Partial: []gadget.PartialProperty{gadget.PartialSchema}}))
	c.Check(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestValidateVolumeSchemaNotOverlapWithGPT(c *C) {
	for i, tc := range []struct {
		s   string
		sz  quantity.Size
		o   quantity.Offset
		err string
	}{
		// in sector 0 only
		{"gpt", 511, 0, ""},
		// might overlap with GPT header, print warning only
		{"gpt", 511, 512, ""},
		{"gpt", 4096, 0, ""},
		// overlap GPT partition table
		{"gpt", 16383, 1024, "invalid structure: GPT header or GPT partition table overlapped with structure \"name\"\n"},
		{"gpt", 2048, 17407, "invalid structure: GPT header or GPT partition table overlapped with structure \"name\"\n"},
		// might overlap with GPT partition table, print warning only
		{"gpt", 2048, 17408, ""},
	} {
		loggerBuf, restore := logger.MockLogger()
		defer restore()

		c.Logf("tc: %v schema: %+v, size: %d, offset: %d", i, tc.s, tc.sz, tc.o)
		mylog.Check(gadget.ValidateVolume(&gadget.Volume{
			Name: "name", Schema: tc.s,
			Structure: []gadget.VolumeStructure{
				{Name: "name", Type: "bare", MinSize: tc.sz, Size: tc.sz, Offset: &tc.o, EnclosingVolume: &gadget.Volume{}},
			},
		}))
		c.Check(err, IsNil)

		start := tc.o
		end := start + quantity.Offset(tc.sz)
		if start < 512*34 && end > 4096 {
			c.Assert(loggerBuf.String(), testutil.Contains,
				fmt.Sprintf("WARNING: invalid structure: GPT header or GPT partition table overlapped with structure \"name\""))
		} else if start < 4096*6 && end > 512 {
			c.Assert(loggerBuf.String(), testutil.Contains,
				fmt.Sprintf("WARNING: GPT header or GPT partition table might be overlapped with structure \"name\""))
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
		mylog.Check(gadget.ValidateVolume(&gadget.Volume{Name: tc.s, Schema: "gpt"}))
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *gadgetYamlTestSuite) TestValidateVolumeDuplicateStructures(c *C) {
	vol := &gadget.Volume{
		Name:   "name",
		Schema: "gpt",
		Structure: []gadget.VolumeStructure{
			{Name: "duplicate", Type: "bare", Size: 1024, Offset: asOffsetPtr(24576)},
			{Name: "duplicate", Type: "21686148-6449-6E6F-744E-656564454649", Size: 2048, Offset: asOffsetPtr(24576)},
		},
	}
	gadget.SetEnclosingVolumeInStructs(map[string]*gadget.Volume{"pc": vol})
	mylog.Check(gadget.ValidateVolume(vol))
	c.Assert(err, ErrorMatches, `structure name "duplicate" is not unique`)
}

func (s *gadgetYamlTestSuite) TestGadgetDuplicateFsLabel(c *C) {
	yamlTemplate := `
volumes:
   minimal:
     bootloader: grub
     structure:
       - name: data1
         filesystem-label: %s
         type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
         size: 1G
       - name: data2
         filesystem-label: %s
         type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
         size: 1G
`

	tests := []struct {
		dupLabel string
		err      string
	}{
		{"foo", `invalid volume "minimal": filesystem label "foo" is not unique`},
		{"writable", `invalid volume "minimal": filesystem label "writable" is not unique`},
		{"ubuntu-data", `invalid volume "minimal": filesystem label "ubuntu-data" is not unique`},
		{"system-boot", `invalid volume "minimal": filesystem label "system-boot" is not unique`},
	}

	for _, t := range tests {
		yaml := fmt.Sprintf(string(yamlTemplate), t.dupLabel, t.dupLabel)
		_ := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(yaml), uc20Mod))
		c.Assert(err, ErrorMatches, t.err)
	}
}

func (s *gadgetYamlTestSuite) TestGadgetDuplicateFsLabelWithCase(c *C) {
	yamlTemplate := `
volumes:
   minimal:
     bootloader: grub
     structure:
       - name: data1
         filesystem-label: %s
         filesystem: %s
         type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
         size: 1G
       - name: data2
         filesystem-label: %s
         filesystem: %s
         type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
         size: 1G
`

	tests := []struct {
		label1, label2   string
		fsType1, fsType2 string
		err              string
	}{
		{"foo", "FOO", "vfat", "vfat", `invalid volume "minimal": filesystem label "FOO" is not unique`},
		{"foo", "FOO", "vfat", "vfat-16", `invalid volume "minimal": filesystem label "FOO" is not unique`},
		{"foo", "FOO", "vfat-16", "vfat-16", `invalid volume "minimal": filesystem label "FOO" is not unique`},
		{"foo", "FOO", "vfat-16", "vfat-32", `invalid volume "minimal": filesystem label "FOO" is not unique`},
		{"foo", "FOO", "ext4", "ext4", ""},
		{"foo", "FOO", "vfat", "ext4", `invalid volume "minimal": filesystem label "FOO" is not unique`},
		{"FOO", "foo", "vfat", "ext4", `invalid volume "minimal": filesystem label "foo" is not unique`},
	}

	for _, t := range tests {
		yaml := fmt.Sprintf(yamlTemplate, t.label1, t.fsType1, t.label2, t.fsType2)
		_ := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(yaml), uc20Mod))
		if t.err == "" {

		} else {
			c.Assert(err, ErrorMatches, t.err)
		}
	}
}

func (s *gadgetYamlTestSuite) TestValidateVolumeErrorsWrapped(c *C) {
	vol := &gadget.Volume{
		Name:   "name",
		Schema: "gpt",
		Structure: []gadget.VolumeStructure{
			{Type: "bare", Size: 1024, Offset: asOffsetPtr(24576)},
			{Type: "bogus", Size: 1024, Offset: asOffsetPtr(24576)},
		},
	}
	gadget.SetEnclosingVolumeInStructs(map[string]*gadget.Volume{"pc": vol})
	mylog.Check(gadget.ValidateVolume(vol))
	c.Assert(err, ErrorMatches, `invalid structure #1: invalid type "bogus": invalid format`)

	vol = &gadget.Volume{
		Name:   "name",
		Schema: "gpt",
		Structure: []gadget.VolumeStructure{
			{Type: "bare", Size: 1024, Offset: asOffsetPtr(24576)},
			{Type: "bogus", Size: 1024, Name: "foo", Offset: asOffsetPtr(24576)},
		},
	}
	gadget.SetEnclosingVolumeInStructs(map[string]*gadget.Volume{"pc": vol})
	mylog.Check(gadget.ValidateVolume(vol))
	c.Assert(err, ErrorMatches, `invalid structure #1 \("foo"\): invalid type "bogus": invalid format`)

	vol = &gadget.Volume{
		Name:   "name",
		Schema: "gpt",
		Structure: []gadget.VolumeStructure{
			{Type: "bare", Name: "foo", Size: 1024, Offset: asOffsetPtr(24576), Content: []gadget.VolumeContent{{UnresolvedSource: "foo"}}},
		},
	}
	gadget.SetEnclosingVolumeInStructs(map[string]*gadget.Volume{"pc": vol})
	mylog.Check(gadget.ValidateVolume(vol))
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
		mylog.Check(gadget.ValidateVolumeStructure(tc.s, &gadget.Volume{Schema: "gpt"}))
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
	mylog.Check(os.WriteFile(s.gadgetYamlPath, []byte(gadgetYamlBadStructureName), 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, nil))
	c.Check(err, ErrorMatches, `invalid volume "pc": structure "other-name" refers to an unexpected structure "bad-name"`)
}

func (s *gadgetYamlTestSuite) TestValidateStructureUpdatePreserveOnlyForFs(c *C) {
	gv := &gadget.Volume{Schema: "gpt"}
	mylog.Check(gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:            "bare",
		Update:          gadget.VolumeUpdate{Preserve: []string{"foo"}},
		Size:            512,
		EnclosingVolume: gv,
	}, gv))
	c.Check(err, ErrorMatches, "preserving files during update is not supported for non-filesystem structures")
	mylog.Check(gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:            "21686148-6449-6E6F-744E-656564454649",
		Update:          gadget.VolumeUpdate{Preserve: []string{"foo"}},
		Size:            512,
		EnclosingVolume: gv,
	}, gv))
	c.Check(err, ErrorMatches, "preserving files during update is not supported for non-filesystem structures")
	mylog.Check(gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:            "21686148-6449-6E6F-744E-656564454649",
		Filesystem:      "vfat",
		Update:          gadget.VolumeUpdate{Preserve: []string{"foo"}},
		Size:            512,
		EnclosingVolume: gv,
	}, gv))
	c.Check(err, IsNil)
}

func (s *gadgetYamlTestSuite) TestValidateStructureUpdatePreserveDuplicates(c *C) {
	gv := &gadget.Volume{Schema: "gpt"}
	mylog.Check(gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:            "21686148-6449-6E6F-744E-656564454649",
		Filesystem:      "vfat",
		Update:          gadget.VolumeUpdate{Edition: 1, Preserve: []string{"foo", "bar"}},
		Size:            512,
		EnclosingVolume: gv,
	}, gv))
	c.Check(err, IsNil)
	mylog.Check(gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:            "21686148-6449-6E6F-744E-656564454649",
		Filesystem:      "vfat",
		Update:          gadget.VolumeUpdate{Edition: 1, Preserve: []string{"foo", "bar", "foo"}},
		Size:            512,
		EnclosingVolume: gv,
	}, gv))
	c.Check(err, ErrorMatches, `duplicate "preserve" entry "foo"`)
}

func (s *gadgetYamlTestSuite) TestValidateStructureSizeRequired(c *C) {
	gv := &gadget.Volume{Schema: "gpt"}
	mylog.Check(gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:            "bare",
		Update:          gadget.VolumeUpdate{Preserve: []string{"foo"}},
		EnclosingVolume: gv,
	}, gv))
	c.Check(err, ErrorMatches, "missing size")
	mylog.Check(gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:            "21686148-6449-6E6F-744E-656564454649",
		Filesystem:      "vfat",
		Update:          gadget.VolumeUpdate{Preserve: []string{"foo"}},
		EnclosingVolume: gv,
	}, gv))
	c.Check(err, ErrorMatches, "missing size")
	mylog.Check(gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:            "21686148-6449-6E6F-744E-656564454649",
		Filesystem:      "vfat",
		Size:            mustParseGadgetSize(c, "123M"),
		Update:          gadget.VolumeUpdate{Preserve: []string{"foo"}},
		EnclosingVolume: gv,
	}, gv))
	c.Check(err, IsNil)
	mylog.Check(gadget.ValidateVolumeStructure(&gadget.VolumeStructure{
		Type:            "21686148-6449-6E6F-744E-656564454649",
		Filesystem:      "vfat",
		MinSize:         mustParseGadgetSize(c, "10M"),
		Size:            mustParseGadgetSize(c, "123M"),
		Update:          gadget.VolumeUpdate{Preserve: []string{"foo"}},
		EnclosingVolume: gv,
	}, gv))
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
        size: 300
        offset: 200
        content:
          - image: pc-core.img
`
	mylog.Check(os.WriteFile(s.gadgetYamlPath, []byte(overlappingGadgetYaml), 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, nil))
	c.Check(err, ErrorMatches, `invalid volume "pc": structure "other-name" overlaps with the preceding structure "mbr"`)
}

func (s *gadgetYamlTestSuite) TestValidateLayoutOverlapOutOfOrder(c *C) {
	outOfOrderGadgetYaml := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: overlaps-with-foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 300
        offset: 200
        content:
          - image: pc-core.img
      - name: foo
        type: DA,21686148-6449-6E6F-744E-656564454648
        size: 200
        offset: 100
        filesystem: vfat
`
	mylog.Check(os.WriteFile(s.gadgetYamlPath, []byte(outOfOrderGadgetYaml), 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, nil))
	c.Check(err, ErrorMatches, `invalid volume "pc": structure "overlaps-with-foo" overlaps with the preceding structure "foo"`)
}

func (s *gadgetYamlTestSuite) TestValidateLayoutOverlapWithMinSize(c *C) {
	overlappingGadgetYaml := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: p1
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 2M
      - name: p2
        type: DA,21686148-6449-6E6F-744E-656564454649
        min-size: 2M
        size: 3M
      - name: p3
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 3M
`
	mylog.Check(os.WriteFile(s.gadgetYamlPath, []byte(overlappingGadgetYaml), 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, nil))
	c.Check(err, ErrorMatches, `invalid volume "pc": structure "p3" overlaps with the preceding structure "p2"`)
}

func (s *gadgetYamlTestSuite) TestValidateCrossStructureMBRFixedOffset(c *C) {
	gadgetYaml := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: other-name
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 10
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
	mylog.Check(os.WriteFile(s.gadgetYamlPath, []byte(gadgetYaml), 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, nil))
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
        size: 10
        offset: 500
        content:
          - image: pc-core.img
      - name: mbr
        type: mbr
        size: 440
        content:
          - image: pc-boot.img
`
	mylog.Check(os.WriteFile(s.gadgetYamlPath, []byte(gadgetYaml), 0644))


	_ = mylog.Check2(gadget.ReadInfo(s.dir, nil))
	c.Check(err, ErrorMatches, `invalid volume "pc": invalid structure #1 \("mbr"\): invalid role "mbr": mbr structure must start at offset 0`)
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
		mylog.Check(os.WriteFile(s.gadgetYamlPath, b.Bytes(), 0644))


		_ = mylog.Check2(gadget.ReadInfoAndValidate(s.dir, nil, nil))
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
	mylog.Check(os.WriteFile(s.gadgetYamlPath, []byte(bloader), 0644))

	mod := &gadgettest.ModelCharacteristics{
		HasModes: true,
	}

	_ = mylog.Check2(gadget.ReadInfoAndValidate(s.dir, mod, nil))
	c.Assert(err, ErrorMatches, "model requires system-seed partition, but no system-seed or system-data partition found")
}

func (s *gadgetYamlTestSuite) TestGadgetReadInfoVsFromMeta(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, gadgetYamlPC, 0644))


	mod := &gadgettest.ModelCharacteristics{
		IsClassic: false,
	}

	giRead := mylog.Check2(gadget.ReadInfo(s.dir, mod))
	c.Check(err, IsNil)

	giMeta := mylog.Check2(gadget.InfoFromGadgetYaml(gadgetYamlPC, mod))
	c.Check(err, IsNil)

	c.Assert(giRead, DeepEquals, giMeta)
}

func (s *gadgetYamlTestSuite) TestReadInfoValidatesEmptySource(c *C) {
	gadgetYamlContent := `
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

	_ := mylog.Check2(gadget.ReadInfo(s.dir, nil))
	c.Assert(err, ErrorMatches, `invalid volume "missing": invalid structure #0 \("missing-content-source"\): invalid content #1: missing source`)
}

func (s *gadgetYamlTestSuite) TestGadgetImplicitSchema(c *C) {
	minimal := []byte(`
volumes:
   minimal:
     bootloader: grub
`)

	tests := map[string][]byte{
		"minimal": minimal,
		"pc":      gadgetYamlPC,
	}

	for volName, yaml := range tests {
		giMeta := mylog.Check2(gadget.InfoFromGadgetYaml(yaml, nil))


		vol := giMeta.Volumes[volName]
		c.Check(vol.Schema, Equals, "gpt")
	}
}

func (s *gadgetYamlTestSuite) TestGadgetImplicitRoleMBR(c *C) {
	minimal := []byte(`
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
		giMeta := mylog.Check2(gadget.InfoFromGadgetYaml(yaml, nil))


		vs := giMeta.Volumes[volName].Structure[0]
		c.Check(vs.Role, Equals, "mbr")

		// also layout the volume and check that when laying out the MBR
		// structure it retains the role of MBR, as validated by IsRoleMBR
		vol := giMeta.Volumes[volName]
		ls := mylog.Check2(gadget.LayoutVolumePartially(vol, gadget.OnDiskStructsFromGadget(vol)))

		c.Check(ls.LaidOutStructure[0].VolumeStructure.IsRoleMBR(), Equals, true)
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
		giMeta := mylog.Check2(gadget.InfoFromGadgetYaml(t.yaml, t.model))


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
		giMeta := mylog.Check2(gadget.InfoFromGadgetYaml(t.yaml, coreMod))


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
		{"pc", "ubuntu-seed", gadgetYamlMinSizePC, "ubuntu-seed"},
		{"pc", "ubuntu-boot", gadgetYamlMinSizePC, "ubuntu-boot"},
		{"pc", "ubuntu-data", gadgetYamlMinSizePC, "ubuntu-data"},
		{"pc", "ubuntu-save", gadgetYamlMinSizePC, "ubuntu-save"},
	}

	for _, t := range tests {
		giMeta := mylog.Check2(gadget.InfoFromGadgetYaml(t.yaml, uc20Mod))


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
		_ := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(yaml), t.mod))
		if t.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}

func (s *gadgetYamlTestSuite) TestGadgetFromMetaEmpty(c *C) {
	// this is ok for classic
	giClassic := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(""), classicMod))
	c.Check(err, IsNil)
	c.Assert(giClassic, DeepEquals, &gadget.Info{})

	// but not so much for core
	giCore := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(""), coreMod))
	c.Check(err, ErrorMatches, "bootloader not declared in any volume")
	c.Assert(giCore, IsNil)
}

func (s *gadgetYamlTestSuite) TestLaidOutVolumesFromGadgetMultiVolume(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, mockMultiVolumeUC20GadgetYaml, 0644))

	mylog.Check(os.WriteFile(filepath.Join(s.dir, "u-boot.imz"), nil, 0644))


	all := mylog.Check2(gadgettest.LaidOutVolumesFromGadget(s.dir, "", uc20Mod, secboot.EncryptionTypeNone, nil))


	c.Assert(all, HasLen, 2)
	c.Assert(all["u-boot-frobinator"].LaidOutStructure[0], DeepEquals, gadget.LaidOutStructure{
		OnDiskStructure: gadget.OnDiskStructure{
			Name:        "u-boot",
			Type:        "bare",
			StartOffset: 24576,
			Size:        623000,
		},
		VolumeStructure: &gadget.VolumeStructure{
			VolumeName: "u-boot-frobinator",
			Name:       "u-boot",
			Offset:     asOffsetPtr(24576),
			Size:       quantity.Size(623000),
			MinSize:    quantity.Size(623000),
			Type:       "bare",
			Content: []gadget.VolumeContent{
				{Image: "u-boot.imz"},
			},
			EnclosingVolume: all["u-boot-frobinator"].Volume,
		},
		LaidOutContent: []gadget.LaidOutContent{
			{
				VolumeContent: &gadget.VolumeContent{
					Image: "u-boot.imz",
				},
				StartOffset: 24576,
			},
		},
	})
}

func (s *gadgetYamlTestSuite) TestLaidOutVolumesFromGadgetHappy(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, gadgetYamlPC, 0644))

	for _, fn := range []string{"pc-boot.img", "pc-core.img"} {
		mylog.Check(os.WriteFile(filepath.Join(s.dir, fn), nil, 0644))

	}

	all := mylog.Check2(gadgettest.LaidOutVolumesFromGadget(s.dir, "", coreMod, secboot.EncryptionTypeNone, nil))

	c.Assert(all, HasLen, 1)
	c.Assert(all["pc"].Volume.Bootloader, Equals, "grub")
	// mbr, bios-boot, efi-system
	c.Assert(all["pc"].LaidOutStructure, HasLen, 3)
}

func (s *gadgetYamlTestSuite) TestLaidOutVolumesFromGadgetAndDiskHappy(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, gadgetYamlUC20PC, 0644))

	for _, fn := range []string{"pc-boot.img", "pc-core.img"} {
		mylog.Check(os.WriteFile(filepath.Join(s.dir, fn), nil, 0644))

	}

	gadgetToDiskStruct := map[int]*gadget.OnDiskStructure{
		0: {Name: "mbr"},
		1: {Name: "BIOS Boot"},
		2: {Name: "ubuntu-seed"},
		3: {Name: "ubuntu-boot"},
		4: {Name: "ubuntu-save"},
		5: {Name: "ubuntu-data"},
	}
	all := mylog.Check2(gadgettest.LaidOutVolumesFromGadget(s.dir, "", uc20Mod, secboot.EncryptionTypeNone, nil))

	c.Assert(all, HasLen, 1)
	c.Assert(all["pc"].Volume.Bootloader, Equals, "grub")
	// mbr, bios-boot, seed, boot, save, data
	c.Assert(all["pc"].LaidOutStructure, HasLen, len(gadgetToDiskStruct))
	for i, los := range all["pc"].LaidOutStructure {
		c.Check(los.OnDiskStructure.Name, Equals, gadgetToDiskStruct[i].Name)
		c.Check(los.VolumeStructure.Name, Equals, gadgetToDiskStruct[i].Name)
	}
}

func (s *gadgetYamlTestSuite) TestLaidOutVolumesFromGadgetAndDiskFail(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, gadgetYamlUC20PC, 0644))

	for _, fn := range []string{"pc-boot.img", "pc-core.img"} {
		mylog.Check(os.WriteFile(filepath.Join(s.dir, fn), nil, 0644))

	}

	gadgetToDiskStruct := map[int]*gadget.OnDiskStructure{
		0: {Name: "mbr"},
		1: {Name: "BIOS Boot"},
	}
	volToGadgetToDiskStruct := map[string]map[int]*gadget.OnDiskStructure{
		"pc": gadgetToDiskStruct,
	}
	all := mylog.Check2(gadgettest.LaidOutVolumesFromGadget(s.dir, "", uc20Mod, secboot.EncryptionTypeNone, volToGadgetToDiskStruct))
	c.Assert(err.Error(), Equals, `internal error: partition "ubuntu-seed" not in disk map`)
	c.Assert(all, IsNil)
}

func (s *gadgetYamlTestSuite) testLaidOutVolumesFromGadgetUCHappy(c *C, gadgetYaml []byte) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, gadgetYaml, 0644))

	for _, fn := range []string{"pc-boot.img", "pc-core.img"} {
		mylog.Check(os.WriteFile(filepath.Join(s.dir, fn), nil, 0644))

	}

	all := mylog.Check2(gadgettest.LaidOutVolumesFromGadget(s.dir, "", uc20Mod, secboot.EncryptionTypeNone, nil))

	c.Assert(all, HasLen, 1)
	c.Assert(all["pc"].Volume.Bootloader, Equals, "grub")
	// mbr, bios-boot, ubuntu-seed, ubuntu-save, ubuntu-boot, and ubuntu-data
	c.Assert(all["pc"].LaidOutStructure, HasLen, 6)
}

func (s *gadgetYamlTestSuite) TestLaidOutVolumesFromGadgetUCHappy(c *C) {
	s.testLaidOutVolumesFromGadgetUCHappy(c, gadgetYamlUC20PC)
	s.testLaidOutVolumesFromGadgetUCHappy(c, gadgetYamlMinSizePC)
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
	snapf := mylog.Check2(snapfile.Open(snapPath))


	// if model is nil, we allow a missing gadget.yaml
	_ = mylog.Check2(gadget.ReadInfoFromSnapFile(snapf, nil))


	_ = mylog.Check2(gadget.ReadInfoFromSnapFile(snapf, &gadgettest.ModelCharacteristics{}))
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
	snapf := mylog.Check2(snapfile.Open(snapPath))


	ginfo := mylog.Check2(gadget.ReadInfoFromSnapFile(snapf, nil))

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
	snapf := mylog.Check2(snapfile.Open(snapPath))


	_ = mylog.Check2(gadget.ReadInfoFromSnapFile(snapf, &gadgettest.ModelCharacteristics{HasModes: true}))
	c.Check(err, ErrorMatches, "model requires system-seed partition, but no system-seed or system-data partition found")
}

type gadgetCompatibilityTestSuite struct{}

var _ = Suite(&gadgetCompatibilityTestSuite{})

func (s *gadgetCompatibilityTestSuite) TestGadgetIsCompatibleSelf(c *C) {
	giPC1 := mylog.Check2(gadget.InfoFromGadgetYaml(gadgetYamlPC, coreMod))

	giPC2 := mylog.Check2(gadget.InfoFromGadgetYaml(gadgetYamlPC, coreMod))

	mylog.Check(gadget.IsCompatible(giPC1, giPC2))
	c.Check(err, IsNil)
}

func (s *gadgetCompatibilityTestSuite) TestGadgetIsCompatibleBadVolume(c *C) {
	mockYaml := []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
    id: 0C
`)

	mockOtherYaml := []byte(`
volumes:
  volumename-other:
    schema: mbr
    bootloader: u-boot
    id: 0C
`)
	mockManyYaml := []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
    id: 0C
  volumename-many:
    schema: mbr
    id: 0C
`)
	mockBadIDYaml := []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
    id: 0D
`)
	mockSchemaYaml := []byte(`
volumes:
  volumename:
    schema: gpt
    bootloader: u-boot
    id: 0C
`)
	mockBootloaderYaml := []byte(`
volumes:
  volumename:
    schema: mbr
    bootloader: grub
    id: 0C
`)
	mockNewStructuresYaml := []byte(`
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
		gi := mylog.Check2(gadget.InfoFromGadgetYaml(mockYaml, coreMod))

		giNew := mylog.Check2(gadget.InfoFromGadgetYaml(tc.gadgetYaml, coreMod))

		mylog.Check(gadget.IsCompatible(gi, giNew))
		if tc.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, tc.err)
		}
	}
}

func (s *gadgetCompatibilityTestSuite) TestGadgetIsCompatibleBadStructure(c *C) {
	baseYaml := `
volumes:
  volumename:
    schema: gpt
    bootloader: grub
    id: 0C
    structure:`
	mockYaml := baseYaml + `
      - name: legit
        size: 2M
        type: 00000000-0000-0000-0000-0000deadbeef
        filesystem: ext4
        filesystem-label: fs-legit
`
	mockBadStructureTypeYaml := baseYaml + `
      - name: legit
        size: 2M
        type: 00000000-0000-0000-0000-0000deadcafe
        filesystem: ext4
        filesystem-label: fs-legit
`
	mockBadFsYaml := baseYaml + `
      - name: legit
        size: 2M
        type: 00000000-0000-0000-0000-0000deadbeef
        filesystem: vfat
        filesystem-label: fs-legit
`
	mockBadOffsetYaml := baseYaml + `
      - name: legit
        size: 2M
        type: 00000000-0000-0000-0000-0000deadbeef
        filesystem: ext4
        offset: 2M
        filesystem-label: fs-legit
`
	mockBadLabelYaml := baseYaml + `
      - name: legit
        size: 2M
        type: 00000000-0000-0000-0000-0000deadbeef
        filesystem: ext4
        filesystem-label: fs-non-legit
`
	mockGPTBadNameYaml := baseYaml + `
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
		{mockBadOffsetYaml, `incompatible layout change: incompatible structure #0 \("legit"\) change: new valid structure offset range \[2097152, 2097152\] is not compatible with current \(\[1048576, 1048576\]\)`},
		{mockBadLabelYaml, `incompatible layout change: incompatible structure #0 \("legit"\) change: cannot change filesystem label from "fs-legit" to "fs-non-legit"`},
		{mockGPTBadNameYaml, `incompatible layout change: incompatible structure #0 \("non-legit"\) change: cannot change structure name from "legit" to "non-legit"`},
	} {
		c.Logf("trying: %d %v\n", i, string(tc.gadgetYaml))
		gi := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(mockYaml), coreMod))

		giNew := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(tc.gadgetYaml), coreMod))

		mylog.Check(gadget.IsCompatible(gi, giNew))
		if tc.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, tc.err)
		}

	}
}

func (s *gadgetCompatibilityTestSuite) TestGadgetIsCompatibleStructureNameMBR(c *C) {
	baseYaml := `
volumes:
  volumename:
    schema: mbr
    bootloader: grub
    id: 0C
    structure:`
	mockYaml := baseYaml + `
      - name: legit
        size: 2M
        type: 0A
`
	mockMBRNameOkYaml := baseYaml + `
      - name: non-legit
        size: 2M
        type: 0A
`

	gi := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(mockYaml), coreMod))

	giNew := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(mockMBRNameOkYaml), coreMod))

	mylog.Check(gadget.IsCompatible(gi, giNew))
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
	model := &gadgettest.ModelCharacteristics{}

	mockGadgetYaml := `
volumes:
  volumename:
    schema: gpt
    bootloader: grub
`

	for _, tc := range []struct {
		files [][]string

		cmdline string
		full    bool
		err     string
	}{{
		files: [][]string{
			{"cmdline.extra", "   foo bar baz just-extra\n"},
			{"meta/gadget.yaml", mockGadgetYaml},
		},
		cmdline: "foo bar baz just-extra", full: false,
	}, {
		files: [][]string{
			{"cmdline.full", "    foo bar baz full\n"},
			{"meta/gadget.yaml", mockGadgetYaml},
		},
		cmdline: "foo bar baz full", full: true,
	}, {
		files: [][]string{
			{"cmdline.full", cmdlineMultiLineWithComments},
			{"meta/gadget.yaml", mockGadgetYaml},
		},
		cmdline: "panic=5 reserve=0x300,32 foo=bar baz=baz random=op debug snapd.debug=1 memmap=100M@2G,100M#3G,1G!1024G",
		full:    true,
	}, {
		files: [][]string{
			{"cmdline.full", ""},
			{"meta/gadget.yaml", mockGadgetYaml},
		},
		cmdline: "",
		full:    true,
	}, {
		// no cmdline
		files: [][]string{
			{"meta/gadget.yaml", mockGadgetYaml},
		},
		full:    false,
		cmdline: "",
	}, {
		// not what we are looking for
		files: [][]string{
			{"cmdline.other", `ignored`},
			{"meta/gadget.yaml", mockGadgetYaml},
		},
		full:    false,
		cmdline: "",
	}, {
		files: [][]string{
			{"cmdline.full", " # error"},
			{"meta/gadget.yaml", mockGadgetYaml},
		},
		full: true, err: `invalid kernel command line in cmdline\.full: unexpected or invalid use of # in argument "#"`,
	}, {
		files: [][]string{
			{"cmdline.full", "foo bar baz #error"},
			{"meta/gadget.yaml", mockGadgetYaml},
		},
		full: true, err: `invalid kernel command line in cmdline\.full: unexpected or invalid use of # in argument "#error"`,
	}, {
		files: [][]string{
			{"cmdline.full", "foo bad =\n"},
			{"meta/gadget.yaml", mockGadgetYaml},
		},
		full: true, err: `invalid kernel command line in cmdline\.full: unexpected assignment`,
	}, {
		files: [][]string{
			{"cmdline.extra", "foo bad ="},
			{"meta/gadget.yaml", mockGadgetYaml},
		},
		full: false, err: `invalid kernel command line in cmdline\.extra: unexpected assignment`,
	}, {
		files: [][]string{
			{"cmdline.extra", `extra`},
			{"cmdline.full", `full`},
			{"meta/gadget.yaml", mockGadgetYaml},
		},
		err: "cannot support both extra and full kernel command lines",
	}} {
		c.Logf("files: %q", tc.files)
		snapPath := snaptest.MakeTestSnapWithFiles(c, string(mockSnapYaml), tc.files)
		cmdline, full, _ := mylog.Check4(gadget.KernelCommandLineFromGadget(snapPath, model))
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
			c.Check(cmdline, Equals, "")
			c.Check(full, Equals, tc.full)
		} else {

			c.Check(cmdline, Equals, tc.cmdline)
			c.Check(full, Equals, tc.full)
		}
	}
}

func (s *gadgetYamlTestSuite) testKernelCommandLineArgs(c *C, whichCmdline string) {
	model := &gadgettest.ModelCharacteristics{}

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
		"snapd_system_disk=somedisk",
	}
	mylog.Check(os.WriteFile(filepath.Join(info.MountDir(), "meta", "gadget.yaml"), mockGadgetYaml, 0644))


	for _, arg := range allowedArgs {
		c.Logf("trying allowed arg: %q", arg)
		mylog.Check(os.WriteFile(filepath.Join(info.MountDir(), whichCmdline), []byte(arg), 0644))


		cmdline, _, _ := mylog.Check4(gadget.KernelCommandLineFromGadget(info.MountDir(), model))

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
		mylog.Check(os.WriteFile(filepath.Join(info.MountDir(), whichCmdline), []byte(arg), 0644))


		cmdline, _, _ := mylog.Check4(gadget.KernelCommandLineFromGadget(info.MountDir(), model))
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
			Node:        "/dev/node1",
			Name:        "BIOS Boot",
			Size:        1 * quantity.SizeMiB,
			StartOffset: 1 * quantity.OffsetMiB,
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
	gadgetVolume := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), mockSimpleGadgetYaml+mockExtraNonInstallableStructure, nil))

	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &mockDeviceLayout, nil))
	c.Assert(err, ErrorMatches, `cannot find gadget structure "foobar" on disk`)

	// note we don't test adding a non-matching structure, since that is already
	// handled in other tests, if we added a non-matching structure the failure
	// will be handled in the first loop checking that all ondisk structures
	// belong to something in the YAML and that will fail, it will not get to
	// the second loop which is what this test is about.
}

func (s *gadgetYamlTestSuite) TestLayoutCompatibilityMBRStructureAllowedMissingWithStruct(c *C) {
	// we are compatible with the MBR structure in the YAML not present in the
	// ondisk structure

	gadgetVolume := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), mockSimpleGadgetYaml, nil))


	// ensure the first structure is the MBR in the YAML, but the first
	// structure in the device layout is BIOS Boot
	c.Assert(gadgetVolume.Structure[0].Role, Equals, "mbr")
	c.Assert(mockDeviceLayout.Structure[0].Name, Equals, "BIOS Boot")

	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &mockDeviceLayout, nil))


	// still okay even with strict options - the absence of the MBR in the
	// ondisk volume is allowed
	opts := &gadget.VolumeCompatibilityOptions{AssumeCreatablePartitionsCreated: true}
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &mockDeviceLayout, opts))

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
				Node:            "/dev/node1",
				Name:            "some-filesystem",
				Size:            1 * quantity.SizeGiB,
				PartitionFSType: "ext4",
				StartOffset:     1*quantity.OffsetMiB + 4096,
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

	gadgetVolume := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), typeBareYAML, nil))


	// ensure the first structure is barething in the YAML, but the first
	// structure in the device layout is some-filesystem
	c.Assert(gadgetVolume.Structure[0].Type, Equals, "bare")
	c.Assert(simpleDeviceLayout.Structure[0].Name, Equals, "some-filesystem")

	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &simpleDeviceLayout, nil))


	// still okay even with strict options - the absence of the bare structure
	// in the ondisk volume is allowed
	opts := &gadget.VolumeCompatibilityOptions{AssumeCreatablePartitionsCreated: true}
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &simpleDeviceLayout, opts))

}

func (s *gadgetYamlTestSuite) TestLayoutCompatibility(c *C) {
	// same contents (the locally created structure should be ignored)
	gadgetVolume := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), mockSimpleGadgetYaml, nil))

	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &mockDeviceLayout, nil))


	// layout still compatible with a larger disk sector size
	mockDeviceLayout.SectorSize = 4096
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &mockDeviceLayout, nil))


	// layout not compatible with a sector size that's not a factor of the
	// structure sizes
	gadgetVolume.Structure[1].Size -= 1
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &mockDeviceLayout, nil))
	c.Assert(err, ErrorMatches, `gadget volume structure "BIOS Boot" size is not a multiple of disk sector size 4096`)
	gadgetVolume.Structure[1].Size += 1

	gadgetVolume.Structure[1].MinSize -= 1
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &mockDeviceLayout, nil))
	c.Assert(err, ErrorMatches, `gadget volume structure "BIOS Boot" size is not a multiple of disk sector size 4096`)
	gadgetVolume.Structure[1].MinSize += 1

	// set t0 512 for the rest of the test
	mockDeviceLayout.SectorSize = 512

	// missing structure (that's ok with default opts)
	gadgetVolumeWithExtras := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), mockSimpleGadgetYaml+mockExtraStructure, nil))

	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &mockDeviceLayout, nil))


	// with strict opts, not okay
	opts := &gadget.VolumeCompatibilityOptions{AssumeCreatablePartitionsCreated: true}
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &mockDeviceLayout, opts))
	c.Assert(err, ErrorMatches, `cannot find gadget structure "Writable" on disk`)

	deviceLayoutWithExtras := mockDeviceLayout
	deviceLayoutWithExtras.Structure = append(deviceLayoutWithExtras.Structure,
		gadget.OnDiskStructure{
			Node:             "/dev/node2",
			Name:             "Extra partition",
			Size:             10 * quantity.SizeMiB,
			PartitionFSLabel: "extra",
			StartOffset:      2 * quantity.OffsetMiB,
		},
	)
	// extra structure (should fail)
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayoutWithExtras, nil))
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2.* in gadget`)

	// layout is not compatible if the device is too small
	smallDeviceLayout := mockDeviceLayout
	smallDeviceLayout.UsableSectorsEnd = uint64(100 * quantity.SizeMiB / 512)

	// validity check
	c.Check(gadgetVolumeWithExtras.MinSize() > quantity.Size(smallDeviceLayout.UsableSectorsEnd*uint64(smallDeviceLayout.SectorSize)), Equals, true)
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &smallDeviceLayout, nil))
	c.Assert(err, ErrorMatches, `device /dev/node \(last usable byte at 100 MiB\) is too small to fit the requested minimal size \(1\.17 GiB\)`)
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
	mockMBRDeviceLayout := gadget.OnDiskVolume{
		Structure: []gadget.OnDiskStructure{
			{
				Node: "/dev/node1",
				// partition names have no
				// meaning in MBR schema
				Name:        "other",
				Size:        440,
				StartOffset: 0,
			},
			{
				Node: "/dev/node2",
				// partition names have no
				// meaning in MBR schema
				Name:        "different BIOS Boot",
				Size:        1 * quantity.SizeMiB,
				StartOffset: 1 * quantity.OffsetMiB,
			},
		},
		ID:               "anything",
		Device:           "/dev/node",
		Schema:           "dos",
		Size:             2 * quantity.SizeGiB,
		UsableSectorsEnd: uint64(2*quantity.SizeGiB/512 - 34 + 1),
		SectorSize:       512,
	}
	gadgetVolume := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), mockMBRGadgetYaml, nil))

	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &mockMBRDeviceLayout, nil))

	// structure is missing from disk
	gadgetVolumeWithExtras := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), mockMBRGadgetYaml+mockExtraStructure, nil))

	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &mockMBRDeviceLayout, nil))

	// add it now
	deviceLayoutWithExtras := mockMBRDeviceLayout
	deviceLayoutWithExtras.Structure = append(deviceLayoutWithExtras.Structure,
		gadget.OnDiskStructure{
			Node: "/dev/node2",
			// name is ignored with MBR schema
			Name:             "Extra partition",
			Size:             1200 * quantity.SizeMiB,
			PartitionFSLabel: "extra",
			PartitionFSType:  "ext4",
			Type:             "83",
			StartOffset:      2 * quantity.OffsetMiB,
		},
	)
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &deviceLayoutWithExtras, nil))


	// test with a larger sector size that is still an even multiple of the
	// structure sizes in the gadget
	mockMBRDeviceLayout.SectorSize = 4096
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &mockMBRDeviceLayout, nil))


	// but with a sector size that is not an even multiple of the structure size
	// then we have an error
	mockMBRDeviceLayout.SectorSize = 513
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &mockMBRDeviceLayout, nil))
	c.Assert(err, ErrorMatches, `gadget volume structure "BIOS Boot" size is not a multiple of disk sector size 513`)

	// add another structure that's not part of the gadget
	deviceLayoutWithExtras.Structure = append(deviceLayoutWithExtras.Structure,
		gadget.OnDiskStructure{
			Node: "/dev/node4",
			// name is ignored with MBR schema
			Name:        "Extra extra partition",
			Size:        1 * quantity.SizeMiB,
			StartOffset: 1202 * quantity.OffsetMiB,
		},
	)
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &deviceLayoutWithExtras, nil))
	c.Assert(err.Error(), Equals, `cannot find disk partition /dev/node4 (starting at 1260388352) in gadget: disk partition "Extra extra partition" offset 1260388352 (1.17 GiB) is not in the valid gadget interval (min: 2097152 (2 MiB): max: 2097152 (2 MiB))`)
}

func (s *gadgetYamlTestSuite) TestLayoutCompatibilityWithCreatedPartitions(c *C) {
	gadgetVolumeWithExtras := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), mockSimpleGadgetYaml+mockExtraStructure, nil))

	deviceLayout := mockDeviceLayout

	// device matches gadget except for the filesystem type
	deviceLayout.Structure = append(deviceLayout.Structure,
		gadget.OnDiskStructure{
			Node:             "/dev/node2",
			Name:             "Writable",
			Size:             1200 * quantity.SizeMiB,
			PartitionFSLabel: "writable",
			PartitionFSType:  "something_else",
			StartOffset:      2 * quantity.OffsetMiB,
		},
	)

	// with no/default opts, then they are compatible
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &deviceLayout, nil))


	// but strict compatibility check, assuming that the creatable partitions
	// have already been created will fail
	opts := &gadget.VolumeCompatibilityOptions{AssumeCreatablePartitionsCreated: true}
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &deviceLayout, opts))
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2 \(starting at 2097152\) in gadget: filesystems do not match: declared as ext4, got something_else`)

	// we are going to manipulate last structure, which has system-data role
	c.Assert(gadgetVolumeWithExtras.Structure[len(gadgetVolumeWithExtras.Structure)-1].Role, Equals, gadget.SystemData)

	// change the role for the laid out volume to not be a partition role that
	// is created at install time (note that the duplicated seed role here is
	// technically incorrect, you can't have duplicated roles, but this
	// demonstrates that a structure that otherwise fits the bill but isn't a
	// role that is created during install will fail the filesystem match check)
	gadgetVolumeWithExtras.Structure[len(gadgetVolumeWithExtras.Structure)-1].Role = gadget.SystemSeed

	// now we fail to find the /dev/node2 structure from the gadget on disk
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &deviceLayout, nil))
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2 \(starting at 2097152\) in gadget: filesystems do not match \(and the partition is not creatable at install\): declared as ext4, got something_else`)

	// note that we don't get the bit about "and the partition is not creatable at install"
	// if we set the strict option, which is not set at install
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &deviceLayout, opts))
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2 \(starting at 2097152\) in gadget: filesystems do not match: declared as ext4, got something_else`)

	// undo the role change
	gadgetVolumeWithExtras.Structure[len(gadgetVolumeWithExtras.Structure)-1].Role = gadget.SystemData

	// change the gadget size to be bigger than the on disk size
	gadgetVolumeWithExtras.Structure[len(gadgetVolumeWithExtras.Structure)-1].Size = 10000000 * quantity.SizeMiB
	gadgetVolumeWithExtras.Structure[len(gadgetVolumeWithExtras.Structure)-1].MinSize = 10000000 * quantity.SizeMiB

	// now we fail to find the /dev/node2 structure from the gadget on disk because the gadget says it must be bigger
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &deviceLayout, nil))
	c.Assert(err.Error(), Equals, `device /dev/node (last usable byte at 2.00 GiB) is too small to fit the requested minimal size (9.54 TiB)`)

	// change the gadget size to be smaller than the on disk size and the role to be one that is not expanded
	gadgetVolumeWithExtras.Structure[len(gadgetVolumeWithExtras.Structure)-1].Size = 1 * quantity.SizeMiB
	gadgetVolumeWithExtras.Structure[len(gadgetVolumeWithExtras.Structure)-1].MinSize = 1 * quantity.SizeMiB
	gadgetVolumeWithExtras.Structure[len(gadgetVolumeWithExtras.Structure)-1].Role = gadget.SystemBoot

	// now we fail because the gadget says it should be smaller and it can't be expanded
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &deviceLayout, nil))
	c.Assert(err.Error(), Equals, `cannot find disk partition /dev/node2 (starting at 2097152) in gadget: on disk size 1258291200 (1.17 GiB) is larger than gadget size 1048576 (1 MiB) (and the role should not be expanded)`)

	// but a smaller partition on disk for SystemData role is okay
	gadgetVolumeWithExtras.Structure[len(gadgetVolumeWithExtras.Structure)-1].Role = gadget.SystemData
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &deviceLayout, nil))

}

const mockExtraNonInstallableStructureWithoutFilesystem = `
      - name: foobar
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1200M
`

func (s *gadgetYamlTestSuite) TestLayoutCompatibilityWithUnspecifiedGadgetFilesystemOnDiskHasFilesystem(c *C) {
	gadgetVolumeWithNonInstallableStructureWithoutFs := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), mockSimpleGadgetYaml+mockExtraNonInstallableStructureWithoutFilesystem, nil))

	deviceLayout := mockDeviceLayout

	// device matches, but it has a filesystem
	deviceLayout.Structure = append(deviceLayout.Structure,
		gadget.OnDiskStructure{
			Node:             "/dev/node2",
			Name:             "foobar",
			Size:             1200 * quantity.SizeMiB,
			PartitionFSLabel: "whatever",
			PartitionFSType:  "something",
			StartOffset:      2 * quantity.OffsetMiB,
		},
	)

	// with no/default opts, then they are compatible
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithNonInstallableStructureWithoutFs, &deviceLayout, nil))


	// still compatible with strict opts
	opts := &gadget.VolumeCompatibilityOptions{AssumeCreatablePartitionsCreated: true}
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithNonInstallableStructureWithoutFs, &deviceLayout, opts))

}

func (s *gadgetYamlTestSuite) TestLayoutCompatibilityWithImplicitSystemData(c *C) {
	gadgetVolume := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), gadgettest.UC16YAMLImplicitSystemData, nil))

	deviceLayout := gadgettest.UC16DeviceLayout

	// with no/default opts, then they are not compatible
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayout, nil))
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/sda3 \(starting at 54525952\) in gadget`)

	// compatible with AllowImplicitSystemData however
	opts := &gadget.VolumeCompatibilityOptions{
		AllowImplicitSystemData: true,
	}
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayout, opts))

}

var mockEncDeviceLayout = gadget.OnDiskVolume{
	Structure: []gadget.OnDiskStructure{
		// Note that the first ondisk structure we have is BIOS Boot, even
		// though in reality the first ondisk structure is MBR, but the MBR
		// doesn't actually show up in /dev at all, so we don't ever measure it
		// as existing on the disk - the code and test accounts for the MBR
		// structure not being present in the OnDiskVolume
		{
			Node:        "/dev/node1",
			Name:        "BIOS Boot",
			Size:        1 * quantity.SizeMiB,
			StartOffset: 1 * quantity.OffsetMiB,
		},
		{
			Node:             "/dev/node2",
			Name:             "Writable",
			Size:             1200 * quantity.SizeMiB,
			PartitionFSType:  "crypto_LUKS",
			PartitionFSLabel: "Writable-enc",
			StartOffset:      2 * quantity.OffsetMiB,
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

func (s *gadgetYamlTestSuite) TestLayoutCompatibilityWithLUKSEncryptedPartitions(c *C) {
	gadgetVolume := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), mockSimpleGadgetYaml+mockExtraStructure, nil))

	deviceLayout := mockEncDeviceLayout

	mockLog, r := logger.MockLogger()
	defer r()

	// if we set the EncryptedPartitions and assume partitions are already
	// created then they match

	encParams := gadget.StructureEncryptionParameters{Method: gadget.EncryptionLUKS}
	encParams.SetUnknownKeys(map[string]string{"foo": "secret-foo"})

	encOpts := &gadget.VolumeCompatibilityOptions{
		AssumeCreatablePartitionsCreated: true,
		ExpectedStructureEncryption: map[string]gadget.StructureEncryptionParameters{
			"Writable": encParams,
		},
	}

	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayout, encOpts))


	// we had a log message about the unknown/unsupported parameter
	c.Assert(mockLog.String(), testutil.Contains, "ignoring unknown expected encryption structure parameter \"foo\"")
	// but we didn't log anything about the value in case that is secret for
	// whatever reason
	c.Assert(mockLog.String(), Not(testutil.Contains), "secret-foo")

	// but if the name of the partition does not match "-enc" then it is not
	// valid
	deviceLayout.Structure[1].PartitionFSLabel = "Writable"
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayout, encOpts))
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2 \(starting at 2097152\) in gadget: partition Writable is expected to be encrypted but is not named Writable-enc`)

	// the filesystem must also be reported as crypto_LUKS
	deviceLayout.Structure[1].PartitionFSLabel = "Writable-enc"
	deviceLayout.Structure[1].PartitionFSType = "ext4"
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayout, encOpts))
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2 \(starting at 2097152\) in gadget: partition Writable is expected to be encrypted but does not have an encrypted filesystem`)

	deviceLayout.Structure[1].PartitionFSType = "crypto_LUKS"

	// but without encrypted partition information and strict assumptions, they
	// do not match due to differing filesystems
	opts := &gadget.VolumeCompatibilityOptions{AssumeCreatablePartitionsCreated: true}
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayout, opts))
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2 \(starting at 2097152\) in gadget: filesystems do not match: declared as ext4, got crypto_LUKS`)

	// with less strict options however they match since this role is creatable
	// at install
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayout, nil))


	// unsupported encryption types
	invalidEncOptions := &gadget.VolumeCompatibilityOptions{
		AssumeCreatablePartitionsCreated: true,
		ExpectedStructureEncryption: map[string]gadget.StructureEncryptionParameters{
			"Writable": {Method: "other"},
		},
	}
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayout, invalidEncOptions))
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2 \(starting at 2097152\) in gadget: unsupported encrypted partition type "other"`)

	// missing an encrypted partition from the gadget.yaml
	missingEncStructureOptions := &gadget.VolumeCompatibilityOptions{
		AssumeCreatablePartitionsCreated: true,
		ExpectedStructureEncryption: map[string]gadget.StructureEncryptionParameters{
			"Writable": {Method: gadget.EncryptionLUKS},
			"missing":  {Method: gadget.EncryptionLUKS},
		},
	}
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayout, missingEncStructureOptions))
	c.Assert(err, ErrorMatches, `expected encrypted structure missing not present in gadget`)

	// missing required method
	invalidEncStructureOptions := &gadget.VolumeCompatibilityOptions{
		AssumeCreatablePartitionsCreated: true,
		ExpectedStructureEncryption: map[string]gadget.StructureEncryptionParameters{
			"Writable": {},
		},
	}
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayout, invalidEncStructureOptions))
	c.Assert(err, ErrorMatches, `cannot find disk partition /dev/node2 \(starting at 2097152\) in gadget: encrypted structure parameter missing required parameter "method"`)
}

func (s *gadgetYamlTestSuite) TestSchemaCompatibility(c *C) {
	gadgetVolume := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), mockSimpleGadgetYaml, nil))

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
		gadgetVolume.Schema = tc.gs
		deviceLayout.Schema = tc.ds
		_ := mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayout, nil))
		if tc.e == "" {

		} else {
			c.Assert(err, ErrorMatches, tc.e)
		}
	}
	c.Logf("-----")
}

func (s *gadgetYamlTestSuite) TestIDCompatibility(c *C) {
	gadgetVolume := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), mockSimpleGadgetYaml, nil))

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
		gadgetVolume.ID = tc.gid
		deviceLayout.ID = tc.did
		_ := mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &deviceLayout, nil))
		if tc.e == "" {

		} else {
			c.Assert(err, ErrorMatches, tc.e)
		}
	}
	c.Logf("-----")
}

const mockMinSizeGadgetYaml = `volumes:
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
      - name: ubuntu-save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        min-size: 8M
        size: 98M
      # Offset could be between 10M and 100M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 100M
`

func (s *gadgetYamlTestSuite) TestCompatibilityWithMinSizePartitions(c *C) {
	mockDiskVolDataAndSaveParts := gadget.OnDiskVolume{
		Structure: []gadget.OnDiskStructure{
			// Note that the first ondisk structure we have is BIOS
			// Boot, even though in reality the first ondisk
			// structure is MBR, but the MBR doesn't actually show
			// up in /dev at all, so we don't ever measure it as
			// existing on the disk - the code and test accounts for
			// the MBR structure not being present in the
			// OnDiskVolume
			{
				Node:        "/dev/node1",
				Name:        "BIOS Boot",
				Size:        1 * quantity.SizeMiB,
				StartOffset: 1 * quantity.OffsetMiB,
			},
			{
				Node:             "/dev/node2",
				Name:             "ubuntu-save",
				Size:             8 * quantity.SizeMiB,
				PartitionFSType:  "ext4",
				PartitionFSLabel: "ubuntu-save",
				StartOffset:      2 * quantity.OffsetMiB,
			},
			{
				Node:             "/dev/node3",
				Name:             "ubuntu-data",
				Size:             100 * quantity.SizeMiB,
				PartitionFSType:  "ext4",
				PartitionFSLabel: "ubuntu-data",
				StartOffset:      10 * quantity.OffsetMiB,
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

	gadgetVolume := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(),
		mockMinSizeGadgetYaml, nil))

	diskVol := mockDiskVolDataAndSaveParts

	match := mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &diskVol, nil))

	c.Assert(match, DeepEquals, map[int]*gadget.OnDiskStructure{
		0: {
			Name:        "mbr",
			Type:        "mbr",
			StartOffset: 0,
			Size:        440,
		},
		1: &mockDiskVolDataAndSaveParts.Structure[0],
		2: &mockDiskVolDataAndSaveParts.Structure[1],
		3: &mockDiskVolDataAndSaveParts.Structure[2],
	})

	diskVol.Structure[1].Size = 98 * quantity.SizeMiB
	diskVol.Structure[2].StartOffset = 100 * quantity.OffsetMiB
	match = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &diskVol, nil))

	c.Assert(match, DeepEquals, map[int]*gadget.OnDiskStructure{
		0: {
			Name:        "mbr",
			Type:        "mbr",
			StartOffset: 0,
			Size:        440,
		},
		1: &mockDiskVolDataAndSaveParts.Structure[0],
		2: &mockDiskVolDataAndSaveParts.Structure[1],
		3: &mockDiskVolDataAndSaveParts.Structure[2],
	})

	diskVol.Structure[1].Size = 98 * quantity.SizeMiB
	diskVol.Structure[2].StartOffset = 101 * quantity.OffsetMiB
	match = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &diskVol, nil))
	c.Assert(err.Error(), Equals, `cannot find disk partition /dev/node3 (starting at 105906176) in gadget: disk partition "ubuntu-data" offset 105906176 (101 MiB) is not in the valid gadget interval (min: 10485760 (10 MiB): max: 104857600 (100 MiB))`)
	c.Assert(match, IsNil)

	diskVol.Structure[1].Size = 6 * quantity.SizeMiB
	diskVol.Structure[2].StartOffset = 8 * quantity.OffsetMiB
	match = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &diskVol, nil))
	c.Assert(err.Error(), Equals, `cannot find disk partition /dev/node2 (starting at 2097152) in gadget: on disk size 6291456 (6 MiB) is smaller than gadget min size 8388608 (8 MiB)`)
	c.Assert(match, IsNil)

	diskVol.Structure[1].Size = 100 * quantity.SizeMiB
	diskVol.Structure[2].StartOffset = 102 * quantity.OffsetMiB
	match = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &diskVol, nil))
	c.Assert(err.Error(), Equals, `cannot find disk partition /dev/node2 (starting at 2097152) in gadget: on disk size 104857600 (100 MiB) is larger than gadget size 102760448 (98 MiB) (and the role should not be expanded)`)
	c.Assert(match, IsNil)
}

const mockMinSizeGadgetYamlWithBare = `volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: ubuntu-save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        min-size: 8M
        size: 98M
      - name: empty-slice
        type: bare
        size: 1M
      # Offset could be between 10M and 100M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 100M
`

func (s *gadgetYamlTestSuite) TestCompatibilityWithMinSizePartitionsWithBare(c *C) {
	mockDiskVolDataAndSaveParts := gadget.OnDiskVolume{
		Structure: []gadget.OnDiskStructure{
			// Note that the first ondisk structure we have is BIOS
			// Boot, even though in reality the first ondisk
			// structure is MBR, but the MBR doesn't actually show
			// up in /dev at all, so we don't ever measure it as
			// existing on the disk - the code and test accounts for
			// the MBR structure not being present in the
			// OnDiskVolume
			{
				Node:             "/dev/node2",
				Name:             "ubuntu-save",
				Size:             8 * quantity.SizeMiB,
				PartitionFSType:  "ext4",
				PartitionFSLabel: "ubuntu-save",
				StartOffset:      quantity.OffsetMiB,
			},
			{
				Node:             "/dev/node3",
				Name:             "ubuntu-data",
				Size:             100 * quantity.SizeMiB,
				PartitionFSType:  "ext4",
				PartitionFSLabel: "ubuntu-data",
				StartOffset:      10 * quantity.OffsetMiB,
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

	gadgetVolume := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(),
		mockMinSizeGadgetYamlWithBare, nil))

	diskVol := mockDiskVolDataAndSaveParts

	match := mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &diskVol, nil))

	expected := map[int]*gadget.OnDiskStructure{
		0: {
			Name:        "mbr",
			Type:        "mbr",
			StartOffset: 0,
			Size:        440,
		},
		1: &mockDiskVolDataAndSaveParts.Structure[0],
		2: {
			Name:        "empty-slice",
			Type:        "bare",
			StartOffset: 9 * quantity.OffsetMiB,
			Size:        quantity.SizeMiB,
		},
		3: &mockDiskVolDataAndSaveParts.Structure[1],
	}
	c.Assert(match, DeepEquals, expected)

	diskVol.Structure[0].Size = 98 * quantity.SizeMiB
	diskVol.Structure[1].StartOffset = 100 * quantity.OffsetMiB
	expected[2].StartOffset = 99 * quantity.OffsetMiB
	match = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &diskVol, nil))

	c.Assert(match, DeepEquals, expected)
}

const mockPartialGadgetYaml = `volumes:
  pc:
    partial: [schema, structure, filesystem, size]
    bootloader: grub
    structure:
      - name: ubuntu-save
        role: system-save
        offset: 2M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
      - name: ubuntu-data
        role: system-data
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 100M
`

func (s *gadgetYamlTestSuite) TestDiskCompatibilityWithPartialGadget(c *C) {
	mockDiskVolDataAndSaveParts := gadget.OnDiskVolume{
		Structure: []gadget.OnDiskStructure{
			{
				Node:        "/dev/node1",
				Name:        "BIOS Boot",
				Size:        1 * quantity.SizeMiB,
				StartOffset: 1 * quantity.OffsetMiB,
			},
			{
				Node:             "/dev/node2",
				Name:             "ubuntu-save",
				Size:             8 * quantity.SizeMiB,
				PartitionFSType:  "ext4",
				PartitionFSLabel: "ubuntu-save",
				StartOffset:      2 * quantity.OffsetMiB,
			},
			{
				Node:             "/dev/node3",
				Name:             "ubuntu-data",
				Size:             200 * quantity.SizeMiB,
				PartitionFSType:  "ext4",
				PartitionFSLabel: "ubuntu-data",
				StartOffset:      10 * quantity.OffsetMiB,
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

	gadgetVolume := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(),
		mockPartialGadgetYaml, nil))

	diskVol := mockDiskVolDataAndSaveParts

	// Compatible as we have defined partial
	match := mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &diskVol, nil))

	c.Assert(match, DeepEquals, map[int]*gadget.OnDiskStructure{
		0: &mockDiskVolDataAndSaveParts.Structure[1],
		1: &mockDiskVolDataAndSaveParts.Structure[2],
	})

	// Not compatible if no partial structure
	gadgetVolume.Partial = []gadget.PartialProperty{gadget.PartialSchema, gadget.PartialSize, gadget.PartialFilesystem}
	match = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolume, &diskVol, nil))
	c.Assert(match, IsNil)
	c.Assert(err.Error(), Equals, "cannot find disk partition /dev/node1 (starting at 1048576) in gadget")
}

var multipleUC20DisksDeviceTraitsMap = map[string]gadget.DiskVolumeDeviceTraits{
	"foo": gadgettest.VMExtraVolumeDeviceTraits,
	"pc":  gadgettest.VMSystemVolumeDeviceTraits,
}

func (s *gadgetYamlTestSuite) TestSaveLoadDiskVolumeDeviceTraits(c *C) {
	// when there is no mapping file, it is not an error, the map returned is
	// just nil/has no items in it
	mAbsent := mylog.Check2(gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDir))

	c.Assert(mAbsent, HasLen, 0)
	mylog.

		// load looks in SnapDeviceDir since it is meant to be used during run mode
		// when /var/lib/snapd/device/disk-mapping.json is the real version from
		// ubuntu-data, but during install mode, we will need to save to the host
		// ubuntu-data which is not located at /run/mnt/data or
		// /var/lib/snapd/device, but rather
		// /run/mnt/ubuntu-data/system-data/var/lib/snapd/device so this takes a
		// directory argument when we save it
		Check(gadget.SaveDiskVolumesDeviceTraits(dirs.SnapDeviceDir, multipleUC20DisksDeviceTraitsMap))


	// now that it was saved to dirs.SnapDeviceDir, we can load it correctly
	m2 := mylog.Check2(gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDir))


	c.Assert(multipleUC20DisksDeviceTraitsMap, DeepEquals, m2)

	// write out example output from a Raspi so we can catch
	// regressions between JSON -> go object importing

	expPiMap := map[string]gadget.DiskVolumeDeviceTraits{
		"pi": gadgettest.ExpectedRaspiDiskVolumeDeviceTraits,
	}
	mylog.Check(os.WriteFile(
		filepath.Join(dirs.SnapDeviceDir, "disk-mapping.json"),
		[]byte(gadgettest.ExpectedRaspiDiskVolumeDeviceTraitsJSON),
		0644,
	))


	m3 := mylog.Check2(gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDir))


	c.Assert(m3, DeepEquals, expPiMap)

	// do the same for a mock LUKS encrypted raspi
	expPiLUKSMap := map[string]gadget.DiskVolumeDeviceTraits{
		"pi": gadgettest.ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraits,
	}
	mylog.Check(os.WriteFile(
		filepath.Join(dirs.SnapDeviceDir, "disk-mapping.json"),
		[]byte(gadgettest.ExpectedLUKSEncryptedRaspiDiskVolumeDeviceTraitsJSON),
		0644,
	))


	m4 := mylog.Check2(gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDir))


	c.Assert(m4, DeepEquals, expPiLUKSMap)

	// if disk-mapping.jso is empty file (zero size), we should handle this as device traits are not
	// available
	f := mylog.Check2(os.Create(filepath.Join(dirs.SnapDeviceDir, "disk-mapping.json")))

	f.Close()
	mAbsent = mylog.Check2(gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDir))

	c.Assert(mAbsent, HasLen, 0)
}

func (s *gadgetYamlTestSuite) TestOnDiskStructureIsLikelyImplicitSystemDataRoleUC16Implicit(c *C) {
	gadgetLayout := mylog.Check2(gadgettest.LayoutFromYaml(c.MkDir(), gadgettest.UC16YAMLImplicitSystemData, nil))

	deviceLayout := gadgettest.UC16DeviceLayout

	// bios boot is not implicit system-data
	matches := gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout.Volume, &deviceLayout, deviceLayout.Structure[0])
	c.Assert(matches, Equals, false)

	// EFI system / system-boot is not implicit system-data
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout.Volume, &deviceLayout, deviceLayout.Structure[1])
	c.Assert(matches, Equals, false)

	// system-data is though
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout.Volume, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, true)

	// the size of the partition does not matter when it comes to being a
	// candidate implicit system-data
	oldSize := deviceLayout.Structure[2].Size
	deviceLayout.Structure[2].Size = 10
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout.Volume, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, true)
	deviceLayout.Structure[2].Size = oldSize

	// very large okay too
	deviceLayout.Structure[2].Size = 1000000000000000000
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout.Volume, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, true)
	deviceLayout.Structure[2].Size = oldSize

	// if we make system-data not ext4 then it is not
	deviceLayout.Structure[2].PartitionFSType = "zfs"
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout.Volume, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, false)
	deviceLayout.Structure[2].PartitionFSType = "ext4"

	// if we make the partition type not "Linux filesystem data", then it is not
	deviceLayout.Structure[2].Type = "foo"
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout.Volume, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, false)
	deviceLayout.Structure[2].Type = "0FC63DAF-8483-4772-8E79-3D69D8477DE4"

	// if we make the Label not writable, then it is not
	deviceLayout.Structure[2].PartitionFSLabel = "foo"
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout.Volume, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, false)
	deviceLayout.Structure[2].PartitionFSLabel = "writable"

	// if we add another LaidOutStructure Partition to the YAML so that there is
	// not exactly one extra partition on disk compated to the YAML, then it is
	// not
	gadgetLayout.Volume.Structure = append(gadgetLayout.Volume.Structure, gadget.VolumeStructure{Type: "foo"})
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout.Volume, &deviceLayout, deviceLayout.Structure[2])
	c.Assert(matches, Equals, false)
	gadgetLayout.Volume.Structure = gadgetLayout.Volume.Structure[:len(gadgetLayout.Volume.Structure)-1]

	// if we make the partition not the last partition, then it is not
	deviceLayout.Structure[2].DiskIndex = 1
	matches = gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout.Volume, &deviceLayout, deviceLayout.Structure[2])
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
	gadgetLayout := mylog.Check2(gadgettest.LayoutFromYaml(c.MkDir(), gadgettest.UC16YAMLImplicitSystemData+explicitSystemData, nil))

	deviceLayout := gadgettest.UC16DeviceLayout

	// none of the structures are implicit because we have an explicit
	// system-data role
	for _, volStruct := range deviceLayout.Structure {
		matches := gadget.OnDiskStructureIsLikelyImplicitSystemDataRole(gadgetLayout.Volume, &deviceLayout, volStruct)
		c.Assert(matches, Equals, false)
	}
}

func (s *gadgetYamlTestSuite) TestAllDiskVolumeDeviceTraitsUnhappy(c *C) {
	vol := mylog.Check2(gadgettest.LayoutFromYaml(c.MkDir(), gadgettest.MockExtraVolumeYAML, nil))


	// don't setup the expected/needed symlinks in /dev
	m := map[string]*gadget.Volume{
		"foo": vol.Volume,
	}
	_ = mylog.Check2(gadget.AllDiskVolumeDeviceTraits(m, nil))
	c.Assert(err, ErrorMatches, `cannot find disk for volume foo from gadget`)
}

func (s *gadgetYamlTestSuite) TestAllDiskVolumeDeviceTraitsHappy(c *C) {
	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev"), 0755))

	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755))

	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/foo1")
	mylog.Check(os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/nofspart")))

	mylog.Check(os.WriteFile(fakedevicepart, nil, 0644))


	// mock the device name
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": gadgettest.MockExtraVolumeDiskMapping,
	})
	defer restore()

	// mock the partition device node going to a particular disk
	restore = disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		fakedevicepart: gadgettest.MockExtraVolumeDiskMapping,
	})
	defer restore()

	vol := mylog.Check2(gadgettest.LayoutFromYaml(c.MkDir(), gadgettest.MockExtraVolumeYAML, nil))


	m := map[string]*gadget.Volume{
		"foo": vol.Volume,
	}
	traitsMap := mylog.Check2(gadget.AllDiskVolumeDeviceTraits(m, nil))


	c.Assert(traitsMap, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"foo": gadgettest.MockExtraVolumeDeviceTraits,
	})
}

func (s *gadgetYamlTestSuite) TestAllDiskVolumeDeviceTraitsTriesAllStructures(c *C) {
	mylog.
		// make a symlink from the filesystem label to /dev/foo2 - note that in
		// reality we would have a symlink for /dev/foo1, since that partition
		// exists, but here we pretend that we for whatever reason don't find
		// /dev/foo1 but we keep going and check /dev/foo2 and at that point
		// everything matches up
		Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev"), 0755))

	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label"), 0755))

	fakedevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/foo2")
	mylog.Check(os.Symlink(fakedevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-label/some-filesystem")))

	mylog.Check(os.WriteFile(fakedevicepart, nil, 0644))


	// mock the device name
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/foo": gadgettest.MockExtraVolumeDiskMapping,
	})
	defer restore()

	// mock the partition device node going to a particular disk
	restore = disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		fakedevicepart: gadgettest.MockExtraVolumeDiskMapping,
	})
	defer restore()

	vol := mylog.Check2(gadgettest.LayoutFromYaml(c.MkDir(), gadgettest.MockExtraVolumeYAML, nil))


	m := map[string]*gadget.Volume{
		"foo": vol.Volume,
	}
	traitsMap := mylog.Check2(gadget.AllDiskVolumeDeviceTraits(m, nil))


	c.Assert(traitsMap, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"foo": gadgettest.MockExtraVolumeDeviceTraits,
	})
}

func (s *gadgetYamlTestSuite) TestAllDiskVolumeDeviceTraitsMultipleGPTVolumes(c *C) {
	mylog.
		// make a symlink for the partition label for nofspart to /dev/vdb1
		Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev"), 0755))

	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755))

	fooVolDevicePart := filepath.Join(dirs.GlobalRootDir, "/dev/vdb1")
	mylog.Check(os.Symlink(fooVolDevicePart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/nofspart")))

	mylog.Check(os.WriteFile(fooVolDevicePart, nil, 0644))


	// make a symlink for the partition label for "BIOS Boot" to /dev/vda1
	fakepcdevicepart := filepath.Join(dirs.GlobalRootDir, "/dev/vda1")
	mylog.Check(os.Symlink(fakepcdevicepart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/BIOS\\x20Boot")))

	mylog.Check(os.WriteFile(fakepcdevicepart, nil, 0644))


	// mock the device name
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/vda": gadgettest.VMSystemVolumeDiskMapping,
		"/dev/vdb": gadgettest.VMExtraVolumeDiskMapping,
	})
	defer restore()

	// mock the partition device nodes going to a particular disks
	restore = disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		fakepcdevicepart: gadgettest.VMSystemVolumeDiskMapping,
		fooVolDevicePart: gadgettest.VMExtraVolumeDiskMapping,
	})
	defer restore()

	mod := &gadgettest.ModelCharacteristics{
		HasModes: true,
	}

	laidOutVols := mylog.Check2(gadgettest.LayoutMultiVolumeFromYaml(
		c.MkDir(),
		"",
		gadgettest.MultiVolumeUC20GadgetYaml,
		mod,
	))


	vols := map[string]*gadget.Volume{}
	for name, lov := range laidOutVols {
		vols[name] = lov.Volume
	}
	traitsMap := mylog.Check2(gadget.AllDiskVolumeDeviceTraits(vols, nil))


	c.Assert(traitsMap, DeepEquals, multipleUC20DisksDeviceTraitsMap)
	mylog.

		// check that an expected json serialization still equals the map we
		// constructed
		Check(os.MkdirAll(dirs.SnapDeviceDir, 0755))

	mylog.Check(os.WriteFile(
		filepath.Join(dirs.SnapDeviceDir, "disk-mapping.json"),
		[]byte(gadgettest.VMMultiVolumeUC20DiskTraitsJSON),
		0644,
	))


	traitsDeviceMap2 := mylog.Check2(gadget.LoadDiskVolumesDeviceTraits(dirs.SnapDeviceDir))


	c.Assert(traitsDeviceMap2, DeepEquals, traitsMap)
}

func (s *gadgetYamlTestSuite) TestAllDiskVolumeDeviceTraitsImplicitSystemDataHappy(c *C) {
	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev"), 0755))

	mylog.Check(os.MkdirAll(filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel"), 0755))

	biosBootPart := filepath.Join(dirs.GlobalRootDir, "/dev/sda1")
	mylog.Check(os.Symlink(biosBootPart, filepath.Join(dirs.GlobalRootDir, "/dev/disk/by-partlabel/BIOS\\x20Boot")))

	mylog.Check(os.WriteFile(biosBootPart, nil, 0644))


	// mock the device name
	restore := disks.MockDeviceNameToDiskMapping(map[string]*disks.MockDiskMapping{
		"/dev/sda": gadgettest.UC16ImplicitSystemDataMockDiskMapping,
	})
	defer restore()

	// mock the partition device node going to a particular disk
	restore = disks.MockPartitionDeviceNodeToDiskMapping(map[string]*disks.MockDiskMapping{
		biosBootPart: gadgettest.UC16ImplicitSystemDataMockDiskMapping,
	})
	defer restore()

	vol := mylog.Check2(gadgettest.LayoutFromYaml(c.MkDir(), gadgettest.UC16YAMLImplicitSystemData, nil))


	m := map[string]*gadget.Volume{
		"pc": vol.Volume,
	}

	// the volume cannot be found with no opts set
	_ = mylog.Check2(gadget.AllDiskVolumeDeviceTraits(m, nil))
	c.Assert(err, ErrorMatches, `cannot gather disk traits for device /dev/sda to use with volume pc: volume pc is not compatible with disk /dev/sda: cannot find disk partition /dev/sda3 \(starting at 54525952\) in gadget`)

	// with opts for pc then it can be found
	optsMap := map[string]*gadget.DiskVolumeValidationOptions{
		"pc": {
			AllowImplicitSystemData: true,
		},
	}
	traitsMap := mylog.Check2(gadget.AllDiskVolumeDeviceTraits(m, optsMap))


	c.Assert(traitsMap, DeepEquals, map[string]gadget.DiskVolumeDeviceTraits{
		"pc": gadgettest.UC16ImplicitSystemDataDeviceTraits,
	})
}

func (s *gadgetYamlTestSuite) TestGadgetInfoHasSameYamlAndJsonTags(c *C) {
	// TODO: once we move to go 1.17 just use
	//       reflect.StructField.IsExported() directly
	isExported := func(s reflect.StructField) bool {
		// see https://pkg.go.dev/reflect#StructField
		return s.PkgPath == ""
	}

	tagsValid := func(c *C, i interface{}, noYaml []string) {
		st := reflect.TypeOf(i).Elem()
		num := st.NumField()
		for i := 0; i < num; i++ {
			// ensure yaml/json is consistent
			tagYaml := st.Field(i).Tag.Get("yaml")
			tagJSON := st.Field(i).Tag.Get("json")
			if tagJSON == "-" {
				continue
			}
			if strutil.ListContains(noYaml, st.Field(i).Name) {
				c.Check(tagYaml, Equals, "-")
				c.Check(tagJSON, Not(Equals), "")
				c.Check(tagJSON, Not(Equals), "-")
			} else {
				c.Check(tagYaml, Equals, tagJSON)
			}

			// ensure we don't accidentally export fields
			// without tags
			if tagJSON == "" && isExported(st.Field(i)) {
				c.Errorf("field %q exported but has no json tag", st.Field(i).Name)
			}
		}
	}

	tagsValid(c, &gadget.Volume{}, nil)
	// gadget.VolumeStructure.Device is never part of
	// Yaml so the test checks that the yaml tag is "-"
	noYaml := []string{"Device"}
	tagsValid(c, &gadget.VolumeStructure{}, noYaml)
	tagsValid(c, &gadget.VolumeContent{}, nil)
	tagsValid(c, &gadget.RelativeOffset{}, nil)
	tagsValid(c, &gadget.VolumeUpdate{}, nil)
}

func (s *gadgetYamlTestSuite) TestGadgetInfoVolumeInternalFieldsNoJSON(c *C) {
	// check
	enc := mylog.Check2(json.Marshal(&gadget.Volume{
		// not json exported
		Name: "should-be-ignored-by-json",
		// exported
		Partial:    []gadget.PartialProperty{gadget.PartialSize},
		Schema:     "mbr",
		Bootloader: "grub",
		ID:         "0c",
		Structure:  []gadget.VolumeStructure{},
	}))

	c.Check(string(enc), Equals, `{"partial":["size"],"schema":"mbr","bootloader":"grub","id":"0c","structure":[]}`)
}

func (s *gadgetYamlTestSuite) TestGadgetInfoVolumeStructureInternalFieldsNoJSON(c *C) {
	volS := &gadget.VolumeStructure{
		// not json exported
		VolumeName: "should-be-ignored-by-json",
		// exported
		Name:        "pc",
		Label:       "ubuntu-seed",
		Role:        "system-seed",
		Offset:      asOffsetPtr(123),
		OffsetWrite: mustParseGadgetRelativeOffset(c, "mbr+92"),
		Size:        888,
		Type:        "0C",
		ID:          "gpt-id",
		Filesystem:  "vfat",
		Content: []gadget.VolumeContent{
			{
				UnresolvedSource: "source",
				Target:           "some-target",
				Image:            "image",
				Offset:           asOffsetPtr(12),
				Size:             321,
				Unpack:           true,
			},
		},
		Update: gadget.VolumeUpdate{
			Edition:  2,
			Preserve: []string{"foo"},
		},
	}
	b := mylog.Check2(json.Marshal(volS))

	// ensure the json looks json-ish
	c.Check(string(b), Equals, `{"name":"pc","filesystem-label":"ubuntu-seed","offset":123,"offset-write":{"relative-to":"mbr","offset":92},"min-size":0,"size":888,"type":"0C","role":"system-seed","id":"gpt-id","filesystem":"vfat","content":[{"source":"source","target":"some-target","image":"image","offset":12,"size":321,"unpack":true}],"update":{"edition":2,"preserve":["foo"]}}`)

	// check that the new structure has no volumeName
	var newVolS *gadget.VolumeStructure
	mylog.Check(json.Unmarshal(b, &newVolS))

	c.Check(newVolS.VolumeName, Equals, "")
	// but otherwise they are identical
	newVolS.VolumeName = volS.VolumeName
	c.Check(volS, DeepEquals, newVolS)
}

func (s *gadgetYamlTestSuite) TestLaidOutVolumesFromClassicWithModesGadgetHappy(c *C) {
	mylog.Check(os.WriteFile(s.gadgetYamlPath, gadgetYamlClassicWithModes, 0644))

	for _, fn := range []string{"pc-boot.img", "pc-core.img"} {
		mylog.Check(os.WriteFile(filepath.Join(s.dir, fn), nil, 0644))

	}

	all := mylog.Check2(gadgettest.LaidOutVolumesFromGadget(s.dir, "", classicWithModesMod, secboot.EncryptionTypeNone, nil))

	c.Assert(all, HasLen, 1)
	c.Assert(all["pc"].Volume.Bootloader, Equals, "grub")
	c.Assert(all["pc"].LaidOutStructure, HasLen, 6)
	c.Assert(all["pc"].Structure[2].Role, Equals, gadget.SystemSeedNull)
	c.Assert(all["pc"].Structure[2].Label, Equals, "ubuntu-seed")
}

func (s *gadgetYamlTestSuite) TestHasRole(c *C) {
	tests := []struct {
		yaml  []byte
		roles []string
		found string
	}{
		{yaml: gadgetYamlUC20PC, roles: []string{gadget.SystemData}, found: gadget.SystemData},
		{yaml: gadgetYamlUC20PC, roles: []string{"system-other"}, found: ""},
		{yaml: gadgetYamlUC20PC, roles: []string{gadget.SystemSeed, gadget.SystemSeedNull}, found: gadget.SystemSeed},
		{yaml: gadgetYamlClassicWithModes, roles: []string{gadget.SystemSeed, gadget.SystemSeedNull}, found: gadget.SystemSeedNull},
	}

	for _, t := range tests {
		mylog.Check(os.WriteFile(s.gadgetYamlPath, t.yaml, 0644))


		found := mylog.Check2(gadget.HasRole(s.dir, t.roles))

		c.Check(found, Equals, t.found)
	}
}

func (s *gadgetYamlTestSuite) TestHasRoleUnhappy(c *C) {
	_ := mylog.Check2(gadget.HasRole("bogus-path", []string{gadget.SystemData}))
	c.Check(err, ErrorMatches, `.*meta/gadget.yaml: no such file or directory`)
	mylog.Check(os.WriteFile(s.gadgetYamlPath, []byte(`{`), 0644))

	_ = mylog.Check2(gadget.HasRole(s.dir, []string{gadget.SystemData}))
	c.Check(err, ErrorMatches, `cannot minimally parse gadget metadata: yaml:.*`)
}

func appendAllowListToYaml(allow []string, templ string) string {
	for _, arg := range allow {
		templ += fmt.Sprintf("    - %s\n", arg)
	}
	return templ
}

func (s *gadgetYamlTestSuite) TestKernelCmdlineAllow(c *C) {
	yamlTemplate := `
volumes:
  pc:
    bootloader: grub
kernel-cmdline:
  allow:
`

	tests := []struct {
		allowList []string
		err       string
	}{
		{[]string{"foo=bar", "my-param.state=blah"}, ""},
		{[]string{"foo="}, ""},
		{[]string{"foo", "bar", `my-param.state="blah"`}, ""},
		{[]string{"foo bar"}, `cannot parse gadget metadata: "foo bar" is not a unique kernel argument`},
	}

	for _, t := range tests {
		c.Logf("allowList %v", t.allowList)
		yaml := appendAllowListToYaml(t.allowList, yamlTemplate)
		gi := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(yaml), uc20Mod))

	}
}

func (s *gadgetYamlTestSuite) testVolumeMinSize(c *C, gadgetYaml []byte, volSizes map[string]quantity.Size) {
	ginfo := mylog.Check2(gadget.InfoFromGadgetYaml(gadgetYaml, nil))


	c.Assert(len(ginfo.Volumes), Equals, len(volSizes))
	for k, v := range ginfo.Volumes {
		c.Logf("checking size of volume %s", k)
		c.Check(v.MinSize(), Equals, quantity.Size(volSizes[k]))
	}
}

func (s *gadgetYamlTestSuite) TestVolumeMinSize(c *C) {
	for _, tc := range []struct {
		gadgetYaml []byte
		volsSizes  map[string]quantity.Size
	}{
		{
			gadgetYaml: gadgetYamlUnorderedParts,
			volsSizes: map[string]quantity.Size{
				"myvol": 1300 * quantity.SizeMiB,
			},
		},
		{
			gadgetYaml: mockMultiVolumeUC20GadgetYaml,
			volsSizes: map[string]quantity.Size{
				"frobinator-image":  (1 + 500 + 10 + 500 + 1024) * quantity.SizeMiB,
				"u-boot-frobinator": 24576 + 623000,
			},
		},
		{
			gadgetYaml: mockMultiVolumeGadgetYaml,
			volsSizes: map[string]quantity.Size{
				"frobinator-image":  (1 + 128 + 380) * quantity.SizeMiB,
				"u-boot-frobinator": 24576 + 623000,
			},
		},
		{
			gadgetYaml: mockVolumeUpdateGadgetYaml,
			volsSizes: map[string]quantity.Size{
				"bootloader": 12345 + 88888,
			},
		},
		{
			gadgetYaml: gadgetYamlPC,
			volsSizes: map[string]quantity.Size{
				"pc": (1 + 1 + 50) * quantity.SizeMiB,
			},
		},
		{
			gadgetYaml: gadgetYamlUC20PC,
			volsSizes: map[string]quantity.Size{
				"pc": (1 + 1 + 1200 + 750 + 16 + 1024) * quantity.SizeMiB,
			},
		},
		{
			gadgetYaml: gadgetYamlMinSizePC,
			volsSizes: map[string]quantity.Size{
				"pc": (1 + 1 + 1200 + 750 + 16 + 1024) * quantity.SizeMiB,
			},
		},
	} {
		c.Logf("test min size for %s", tc.gadgetYaml)
		s.testVolumeMinSize(c, tc.gadgetYaml, tc.volsSizes)
	}
}

func (s *gadgetYamlTestSuite) TestOrderStructuresByOffset(c *C) {
	for _, tc := range []struct {
		unordered   []gadget.VolumeStructure
		ordered     []gadget.VolumeStructure
		description string
	}{
		{
			unordered: []gadget.VolumeStructure{
				{Offset: asOffsetPtr(100)},
				{Offset: asOffsetPtr(0)},
				{Offset: nil},
				{Offset: asOffsetPtr(50)},
			},
			ordered: []gadget.VolumeStructure{
				{Offset: asOffsetPtr(0)},
				{Offset: nil},
				{Offset: asOffsetPtr(50)},
				{Offset: asOffsetPtr(100)},
			},
			description: "test one",
		},
		{
			unordered:   []gadget.VolumeStructure{},
			ordered:     []gadget.VolumeStructure{},
			description: "test two",
		},
		{
			unordered: []gadget.VolumeStructure{
				{Offset: asOffsetPtr(300)},
				{Offset: nil, Name: "nil1"},
				{Offset: asOffsetPtr(1)},
				{Offset: asOffsetPtr(100)},
				{Offset: nil, Name: "nil2"},
				{Offset: nil, Name: "nil3"},
			},
			ordered: []gadget.VolumeStructure{
				{Offset: asOffsetPtr(1)},
				{Offset: asOffsetPtr(100)},
				{Offset: nil, Name: "nil2"},
				{Offset: nil, Name: "nil3"},
				{Offset: asOffsetPtr(300)},
				{Offset: nil, Name: "nil1"},
			},
			description: "test three",
		},
	} {
		c.Logf("testing order structures: %s", tc.description)
		ordered := gadget.OrderStructuresByOffset(tc.unordered)
		c.Check(ordered, DeepEquals, tc.ordered)
	}
}

func (s *gadgetYamlTestSuite) TestGadgetUnorderedStructures(c *C) {
	unorderedYaml := []byte(`
volumes:
  unordered:
    bootloader: u-boot
    schema: gpt
    structure:
      - name: ubuntu-seed
        filesystem: ext4
        size: 499M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-seed
      - name: ubuntu-save
        size: 100M
        offset: 700M
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-save
      - name: ubuntu-boot
        filesystem: ext4
        size: 100M
        offset: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-boot
      - name: other1
        size: 100M
        type: bare
      - name: ubuntu-data
        filesystem: ext4
        offset: 800M
        size: 1G
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-data
`)

	// TODO add more tests when min-size is introduced
	tests := []struct {
		yaml         []byte
		orderedNames []string
		info         string
	}{
		{
			yaml: unorderedYaml,
			orderedNames: []string{
				"ubuntu-seed", "ubuntu-boot",
				"other1", "ubuntu-save", "ubuntu-data",
			},
			info: "test one",
		},
	}
	for _, tc := range tests {
		c.Logf("tc: %s", tc.info)
		giMeta := mylog.Check2(gadget.InfoFromGadgetYaml(tc.yaml, nil))

		c.Assert(len(giMeta.Volumes), Equals, 1)

		var vol *gadget.Volume
		for vn := range giMeta.Volumes {
			vol = giMeta.Volumes[vn]
		}
		names := []string{}
		for _, s := range vol.Structure {
			names = append(names, s.Name)
		}
		c.Check(names, DeepEquals, tc.orderedNames)
	}
}

func (s *gadgetYamlTestSuite) TestValidStartOffset(c *C) {
	type validOffsetTc struct {
		structIdx int
		offset    quantity.Offset
		err       *gadget.InvalidOffsetError
	}
	for _, tc := range []struct {
		vs          gadget.Volume
		votcs       []validOffsetTc
		description string
	}{
		{
			vs: gadget.Volume{
				Structure: []gadget.VolumeStructure{
					{Offset: asOffsetPtr(0), MinSize: 10, Size: 20},
					{Offset: nil, MinSize: 10, Size: 20},
					{Offset: nil, MinSize: 10, Size: 20},
					{Offset: asOffsetPtr(50), MinSize: 100, Size: 100},
				},
			},
			votcs: []validOffsetTc{
				{structIdx: 0, offset: 0, err: nil},
				{structIdx: 0, offset: 5, err: gadget.NewInvalidOffsetError(5, 0, 0)},
				{structIdx: 1, offset: 0, err: gadget.NewInvalidOffsetError(0, 10, 20)},
				{structIdx: 1, offset: 10, err: nil},
				{structIdx: 1, offset: 15, err: nil},
				{structIdx: 1, offset: 20, err: nil},
				{structIdx: 1, offset: 21, err: gadget.NewInvalidOffsetError(21, 10, 20)},
				{structIdx: 1, offset: gadget.UnboundedStructureOffset, err: gadget.NewInvalidOffsetError(gadget.UnboundedStructureOffset, 10, 20)},
				{structIdx: 2, offset: 10, err: gadget.NewInvalidOffsetError(10, 20, 40)},
				{structIdx: 2, offset: 20, err: nil},
				{structIdx: 2, offset: 30, err: nil},
				{structIdx: 2, offset: 40, err: nil},
				{structIdx: 2, offset: 41, err: gadget.NewInvalidOffsetError(41, 20, 40)},
				{structIdx: 2, offset: gadget.UnboundedStructureOffset, err: gadget.NewInvalidOffsetError(gadget.UnboundedStructureOffset, 20, 40)},
				{structIdx: 3, offset: 49, err: gadget.NewInvalidOffsetError(49, 50, 50)},
				{structIdx: 3, offset: 50, err: nil},
				{structIdx: 3, offset: 51, err: gadget.NewInvalidOffsetError(51, 50, 50)},
			},
			description: "test one",
		},
		{
			vs: gadget.Volume{
				Structure: []gadget.VolumeStructure{
					{Offset: asOffsetPtr(0), MinSize: 10, Size: 100},
					{Offset: nil, MinSize: 10, Size: 10},
					{Offset: asOffsetPtr(80), MinSize: 100, Size: 100},
				},
			},
			votcs: []validOffsetTc{
				{structIdx: 0, offset: 0, err: nil},
				{structIdx: 0, offset: 1, err: gadget.NewInvalidOffsetError(1, 0, 0)},
				{structIdx: 1, offset: 9, err: gadget.NewInvalidOffsetError(9, 10, 70)},
				{structIdx: 1, offset: 10, err: nil},
				{structIdx: 1, offset: 70, err: nil},
				{structIdx: 1, offset: 71, err: gadget.NewInvalidOffsetError(71, 10, 70)},
				{structIdx: 2, offset: 79, err: gadget.NewInvalidOffsetError(79, 80, 80)},
				{structIdx: 2, offset: 80, err: nil},
				{structIdx: 2, offset: 81, err: gadget.NewInvalidOffsetError(81, 80, 80)},
			},
			description: "test two",
		},
		{
			// This tests restriction 2 in maxStructureOffset (see
			// comments in function).
			vs: gadget.Volume{
				Structure: []gadget.VolumeStructure{
					{Offset: asOffsetPtr(0), MinSize: 20, Size: 40},
					{Offset: nil, MinSize: 20, Size: 40},
					{Offset: nil, MinSize: 20, Size: 20},
					{Offset: nil, MinSize: 20, Size: 20},
					{Offset: asOffsetPtr(100), MinSize: 100, Size: 100},
				},
			},
			votcs: []validOffsetTc{
				{structIdx: 2, offset: 39, err: gadget.NewInvalidOffsetError(39, 40, 60)},
				{structIdx: 2, offset: 40, err: nil},
				{structIdx: 2, offset: 60, err: nil},
				{structIdx: 2, offset: 61, err: gadget.NewInvalidOffsetError(61, 40, 60)},
			},
			description: "test three",
		},
		{
			vs: gadget.Volume{
				Partial: []gadget.PartialProperty{gadget.PartialSize},
				Structure: []gadget.VolumeStructure{
					{Offset: asOffsetPtr(0), MinSize: 20, Size: 40},
					{Offset: nil},
					{Offset: nil, MinSize: 20, Size: 20},
				},
			},
			votcs: []validOffsetTc{
				{structIdx: 2, offset: 19, err: gadget.NewInvalidOffsetError(19, 20, gadget.UnboundedStructureOffset)},
				{structIdx: 2, offset: 1000, err: nil},
			},
			description: "test four",
		},
	} {
		for sidx := range tc.vs.Structure {
			tc.vs.Structure[sidx].EnclosingVolume = &tc.vs
		}

		for _, votc := range tc.votcs {
			c.Logf("testing valid offset: %s (%+v)", tc.description, votc)
			if votc.err == nil {
				c.Check(gadget.CheckValidStartOffset(votc.offset,
					tc.vs.Structure, votc.structIdx), IsNil)
			} else {
				c.Check(gadget.CheckValidStartOffset(votc.offset,
					tc.vs.Structure, votc.structIdx),
					DeepEquals, votc.err)
			}
		}
	}
}

func (p *layoutTestSuite) TestOffsetWriteOutOfVolumeFails(c *C) {
	gadgetYamlStructure := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        # 1GB
        offset-write: mbr+2097152
`
	gi := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(gadgetYamlStructure), nil))
	c.Check(gi, IsNil)
	c.Assert(err.Error(), Equals, `invalid volume "pc": structure "foo" wants to write offset of 4 bytes to 2097152, outside of referred structure "mbr"`)
}

func (s *gadgetYamlTestSuite) TestValidateOffsetWrite(c *C) {
	for i, tc := range []struct {
		offWrite        *gadget.RelativeOffset
		firstStrName    string
		firstStrOffset  *quantity.Offset
		firstStrMinSize quantity.Size
		volSize         quantity.Size
		err             string
	}{
		{
			offWrite: nil,
			err:      "",
		},
		{
			offWrite:        &gadget.RelativeOffset{"foo", 0},
			firstStrName:    "mbr",
			firstStrOffset:  asOffsetPtr(0),
			firstStrMinSize: 440,
			volSize:         512,
			err:             `structure "test" refers to an unexpected structure "foo"`,
		},
		{
			offWrite:        &gadget.RelativeOffset{"mbr", 437},
			firstStrName:    "mbr",
			firstStrOffset:  asOffsetPtr(0),
			firstStrMinSize: 440,
			volSize:         512,
			err:             `structure "test" wants to write offset of 4 bytes to 437, outside of referred structure "mbr"`,
		},
		{
			offWrite:        &gadget.RelativeOffset{"mbr", 436},
			firstStrName:    "mbr",
			firstStrOffset:  asOffsetPtr(0),
			firstStrMinSize: 440,
			volSize:         512,
			err:             "",
		},
		{
			offWrite:        &gadget.RelativeOffset{"", 1024},
			firstStrName:    "mbr",
			firstStrOffset:  asOffsetPtr(0),
			firstStrMinSize: 440,
			volSize:         512,
			err:             `structure "test" wants to write offset of 4 bytes to 1024, outside of volume of min size 512`,
		},
		{
			offWrite:        &gadget.RelativeOffset{"", 100},
			firstStrName:    "mbr",
			firstStrOffset:  asOffsetPtr(0),
			firstStrMinSize: 440,
			volSize:         512,
			err:             "",
		},
	} {
		c.Logf("tc: %v", i)
		mylog.Check(gadget.ValidateOffsetWrite(
			&gadget.VolumeStructure{Name: "test", OffsetWrite: tc.offWrite},
			&gadget.VolumeStructure{Name: tc.firstStrName, Offset: tc.firstStrOffset, MinSize: tc.firstStrMinSize}, tc.volSize))
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *gadgetYamlTestSuite) TestGadgetValidPartials(c *C) {
	yamlTemplate := `
volumes:
  frobinator-image:
    partial: [%s]
    bootloader: grub
`

	tests := []struct {
		partial string
		err     string
	}{
		{"", ""},
		{"size", ""},
		{"filesystem", ""},
		{"schema", ""},
		{"structure", ""},
		{"bootloader", `"bootloader" is not a valid partial value`},
		{"size,structure,schema", ""},
		{"size,structure,schema,bootloader", `"bootloader" is not a valid partial value`},
		{"size,structure,size", `partial value "size" is repeated`},
	}

	for _, tc := range tests {
		c.Logf("testing partial %q", tc.partial)
		yaml := fmt.Sprintf(yamlTemplate, tc.partial)
		_ := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(yaml), nil))
		if tc.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err.Error(), Equals, tc.err)
		}
	}
}

func (s *gadgetYamlTestSuite) TestGadgetPartialSize(c *C) {
	yaml := []byte(`
volumes:
  frobinator-image:
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

	// Not defining size in a structure is fine
	_ := mylog.Check2(gadget.InfoFromGadgetYaml(yaml, nil))


	// but if defined, things are still checked
	yaml = append(yaml, []byte(`
      - name: ubuntu-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-data
        size: 1M
        min-size: 2M
`)...)
	_ = mylog.Check2(gadget.InfoFromGadgetYaml(yaml, nil))
	c.Assert(err.Error(), Equals, `invalid volume "frobinator-image": invalid structure #4 ("ubuntu-data"): min-size (2097152) is bigger than size (1048576)`)
}

func (s *gadgetYamlTestSuite) TestGadgetPartialFilesystem(c *C) {
	yaml := []byte(`
volumes:
  frobinator-image:
    partial: [filesystem]
    bootloader: grub
    schema: gpt
    structure:
      - name: mbr
        type: mbr
        size: 440
        update:
          edition: 1
        content:
          - image: mbr.img
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
        content:
          - source: splash.bmp
            target: .
      - name: ubuntu-data
        size: 1000M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-data
`)

	// Not defining filesystem in a structure is fine
	_ := mylog.Check2(gadget.InfoFromGadgetYaml(yaml, nil))


	// checks for bare still happen
	yaml = append(yaml, []byte(`
      - name: boot-fw
        type: bare
        size: 1M
        content:
          - source: splash.bmp
            target: .
`)...)
	_ = mylog.Check2(gadget.InfoFromGadgetYaml(yaml, nil))
	c.Assert(err.Error(), Equals, `invalid volume "frobinator-image": invalid structure #5 ("boot-fw"): invalid content #0: cannot use non-image content for bare file system`)
}

func (s *gadgetYamlTestSuite) TestGadgetPartialSchema(c *C) {
	yaml := []byte(`
volumes:
  frobinator-image:
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

	// Not defining schema is fine
	_ := mylog.Check2(gadget.InfoFromGadgetYaml(yaml, nil))


	// but fails if type does not contain both mbr type and gpt guid
	yamlNoGPTGuid := append(yaml, []byte(`
      - name: data
        type: 83
        size: 1M
`)...)
	_ = mylog.Check2(gadget.InfoFromGadgetYaml(yamlNoGPTGuid, nil))
	c.Assert(err.Error(), Equals, `invalid volume "frobinator-image": invalid structure #4 ("data"): invalid type "83": both MBR type and GUID structure type needs to be defined on partial schemas`)
	yamlNoMBRType := append(yaml, []byte(`
      - name: data
        type: 0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1M
`)...)
	_ = mylog.Check2(gadget.InfoFromGadgetYaml(yamlNoMBRType, nil))
	c.Assert(err.Error(), Equals, `invalid volume "frobinator-image": invalid structure #4 ("data"): invalid type "0FC63DAF-8483-4772-8E79-3D69D8477DE4": both MBR type and GUID structure type needs to be defined on partial schemas`)
}

func (s *gadgetYamlTestSuite) TestGadgetPartialSchemaButStillSet(c *C) {
	yaml := []byte(`
volumes:
  frobinator-image:
    partial: [schema]
    schema: gpt
    bootloader: u-boot
`)

	// Not defining schema is fine
	_ := mylog.Check2(gadget.InfoFromGadgetYaml(yaml, nil))
	c.Assert(err.Error(), Equals,
		`invalid volume "frobinator-image": partial schema is set but schema is still specified as "gpt"`)
}

func (s *gadgetYamlTestSuite) TestGadgetPartialStructure(c *C) {
	yaml := []byte(`
volumes:
  frobinator-image:
    partial: [structure]
    bootloader: u-boot
    schema: gpt
    structure:
      - name: ubuntu-seed
        filesystem: ext4
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-seed
      # Space for some unknown structure in the middle is left around
      - name: ubuntu-boot
        filesystem: ext4
        offset: 1000M
        size: 500M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-boot
`)

	// This test does not do a lot as this code does not check gaps
	// between structures, but is left as a safeguard.
	_ := mylog.Check2(gadget.InfoFromGadgetYaml(yaml, nil))

}

func newPartialGadgetYaml(c *C) *gadget.Info {
	gi := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(mockPartialGadgetYaml), coreMod))

	return gi
}

func (s *gadgetCompatibilityTestSuite) TestPartialGadgetIsCompatible(c *C) {
	gi1 := newPartialGadgetYaml(c)
	gi2 := newPartialGadgetYaml(c)
	mylog.

		// self-compatible
		Check(gadget.IsCompatible(gi1, gi2))
	c.Check(err, IsNil)

	// from partial schema to defined is ok
	gi1 = newPartialGadgetYaml(c)
	gi2 = newPartialGadgetYaml(c)
	gi2.Volumes["pc"].Partial = []gadget.PartialProperty{gadget.PartialStructure, gadget.PartialFilesystem, gadget.PartialSize}
	gi2.Volumes["pc"].Schema = "gpt"
	mylog.Check(gadget.IsCompatible(gi1, gi2))
	c.Check(err, IsNil)

	// from defined to partial schema is not
	gi1 = newPartialGadgetYaml(c)
	gi1.Volumes["pc"].Partial = []gadget.PartialProperty{gadget.PartialStructure, gadget.PartialFilesystem, gadget.PartialSize}
	gi1.Volumes["pc"].Schema = "gpt"
	gi2 = newPartialGadgetYaml(c)
	mylog.Check(gadget.IsCompatible(gi1, gi2))
	c.Check(err.Error(), Equals, "incompatible layout change: new schema is partial, while old was not")

	// set filesystems in new
	gi1 = newPartialGadgetYaml(c)
	gi2 = newPartialGadgetYaml(c)
	gi2.Volumes["pc"].Partial = []gadget.PartialProperty{gadget.PartialStructure, gadget.PartialSchema, gadget.PartialSize}
	for istr := range gi2.Volumes["pc"].Structure {
		gi2.Volumes["pc"].Structure[istr].Filesystem = "ext4"
	}
	mylog.Check(gadget.IsCompatible(gi1, gi2))
	c.Check(err, IsNil)

	// set missing sizes in new
	gi1 = newPartialGadgetYaml(c)
	gi2 = newPartialGadgetYaml(c)
	gi2.Volumes["pc"].Partial = []gadget.PartialProperty{gadget.PartialStructure, gadget.PartialFilesystem, gadget.PartialSchema}
	gi2.Volumes["pc"].Structure[0].Size = quantity.SizeMiB
	mylog.Check(gadget.IsCompatible(gi1, gi2))
	c.Check(err, IsNil)
}

func (s *gadgetCompatibilityTestSuite) TestStructFromYamlIndex(c *C) {
	gi := mylog.Check2(gadget.InfoFromGadgetYaml(gadgetYamlUC20PC, nil))


	vol := gi.Volumes["pc"]
	for _, st := range vol.Structure {
		stFromIdx := vol.StructFromYamlIndex(st.YamlIndex)
		c.Check(stFromIdx, DeepEquals, &st)
	}

	// Error cases
	c.Check(vol.StructFromYamlIndex(100), IsNil)
	idx := mylog.Check2(vol.YamlIdxToStructureIdx(100))
	c.Check(idx, Equals, -1)
	c.Check(err.Error(), Equals, "structure with yaml index 100 not found")
}

func (s *gadgetYamlTestSuite) TestFindBootVolumeHappy(c *C) {
	gi := mylog.Check2(gadget.InfoFromGadgetYaml(mockMultiVolumeGadgetYaml, nil))


	bootVol := mylog.Check2(gadget.FindBootVolume(gi.Volumes))

	c.Assert(bootVol, DeepEquals, gi.Volumes["frobinator-image"])
}

func (s *gadgetYamlTestSuite) TestFindBootVolumeFail(c *C) {
	gi := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(`volumes:
  volumename:
    schema: mbr
    bootloader: u-boot
    id:     0C
    structure:
      - filesystem-label: system-data
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
`), nil))


	bootVol := mylog.Check2(gadget.FindBootVolume(gi.Volumes))
	c.Assert(err.Error(), Equals, "no volume has system-boot role")
	c.Assert(bootVol, IsNil)
}

var yamlContentWithOffset = []byte(`
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
            offset: 1M
`)

func (s *gadgetYamlTestSuite) TestVolumeCopy(c *C) {
	for _, yaml := range [][]byte{
		mockMultiVolumeUC20GadgetYaml,
		[]byte(mockPartialGadgetYaml),
		gadgetYamlUC20PC,
		yamlContentWithOffset,
	} {

		gi := mylog.Check2(gadget.InfoFromGadgetYaml(yaml, nil))


		for _, v := range gi.Volumes {
			newV := v.Copy()
			c.Assert(newV, DeepEquals, v)
		}
	}
}

func (s *gadgetYamlTestSuite) TestLayoutCompatibilityVfatPartitions(c *C) {
	const mockFat16Structure = `
      - name: Writable
        role: system-data
        filesystem-label: writable
        filesystem: vfat-16
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 64M
`

	gadgetVolumeWithExtras := mylog.Check2(gadgettest.VolumeFromYaml(c.MkDir(), mockSimpleGadgetYaml+mockFat16Structure, nil))

	deviceLayout := mockDeviceLayout

	// device matches gadget except for the filesystem type
	deviceLayout.Structure = append(deviceLayout.Structure,
		gadget.OnDiskStructure{
			Node:             "/dev/node2",
			Name:             "Writable",
			Size:             64 * quantity.SizeMiB,
			PartitionFSLabel: "writable",
			PartitionFSType:  "vfat",
			StartOffset:      2 * quantity.OffsetMiB,
		},
	)

	// strict compatibility check, assuming that the creatable partitions
	// have already been created will fail
	opts := &gadget.VolumeCompatibilityOptions{AssumeCreatablePartitionsCreated: true}
	_ = mylog.Check2(gadget.EnsureVolumeCompatibility(gadgetVolumeWithExtras, &deviceLayout, opts))

}

func (s *gadgetYamlTestSuite) TestGadgetToLinuxFilesystem(c *C) {
	for i, tc := range []struct {
		vs    *gadget.VolumeStructure
		linFs string
	}{
		{&gadget.VolumeStructure{Filesystem: "ext4"}, "ext4"},
		{&gadget.VolumeStructure{Filesystem: "vfat"}, "vfat"},
		{&gadget.VolumeStructure{Filesystem: "vfat-16"}, "vfat"},
		{&gadget.VolumeStructure{Filesystem: "vfat-32"}, "vfat"},
	} {
		c.Logf("case %d: %s", i, tc.linFs)
		c.Check(tc.vs.LinuxFilesystem(), Equals, tc.linFs)
	}
}

func (s *gadgetYamlTestSuite) TestGadgetInfoHasRole(c *C) {
	info := gadget.Info{
		Volumes: map[string]*gadget.Volume{
			"name": {
				Structure: []gadget.VolumeStructure{
					{Role: gadget.SystemSeed},
				},
			},
			"other-name": {
				Structure: []gadget.VolumeStructure{
					{Role: gadget.SystemBoot},
				},
			},
		},
	}

	c.Check(info.HasRole(gadget.SystemSeed), Equals, true)
	c.Check(info.HasRole(gadget.SystemBoot), Equals, true)
	c.Check(info.HasRole(gadget.SystemSeedNull), Equals, false)
}
