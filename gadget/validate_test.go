// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019-2021 Canonical Ltd
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
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/gadgettest"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/kernel"
)

type validateGadgetTestSuite struct {
	dir string
}

var _ = Suite(&validateGadgetTestSuite{})

func (s *validateGadgetTestSuite) SetUpTest(c *C) {
	s.dir = c.MkDir()
}

func (s *validateGadgetTestSuite) TestRuleValidateStructureReservedLabels(c *C) {
	for _, tc := range []struct {
		role, label, fs, err string
		model                gadget.Model
	}{
		{label: "ubuntu-seed", fs: "vfat", err: `label "ubuntu-seed" is reserved`},
		{label: "UBUNTU-SEED", fs: "vfat", err: `label "UBUNTU-SEED" is reserved`},
		{label: "ubuntu-data", fs: "ext4", err: `label "ubuntu-data" is reserved`},
		// not reserved as it os not vfat and case is enforced
		{label: "UBUNTU-DATA", fs: "ext4"},
		// ok to allow hybrid 20-ready devices
		{label: "ubuntu-boot", fs: "ext4"},
		{label: "ubuntu-save", fs: "ext4"},
		// reserved only if seed present/expected
		{label: "ubuntu-boot", fs: "ext4", err: `label "ubuntu-boot" is reserved`, model: uc20Mod},
		{label: "ubuntu-save", fs: "ext4", err: `label "ubuntu-save" is reserved`, model: uc20Mod},
		// these are ok
		{role: "system-boot", fs: "ext4", label: "ubuntu-boot"},
		{label: "random-ubuntu-label", fs: "ext4"},
	} {
		gi := &gadget.Info{
			Volumes: map[string]*gadget.Volume{
				"vol0": {
					Structure: []gadget.VolumeStructure{{
						Type:       "21686148-6449-6E6F-744E-656564454649",
						Role:       tc.role,
						Filesystem: tc.fs,
						Label:      tc.label,
						Size:       10 * 1024,
					}},
				},
			},
		}
		mylog.Check(gadget.Validate(gi, tc.model, nil))
		if tc.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, ".*: "+tc.err)
		}
	}
}

// rolesYaml produces gadget metadata with volumes with structure withs the given
// role if data, seed or save are != "-", and with their label set to the value
func rolesYaml(c *C, data, seed, save string) *gadget.Info {
	h := `volumes:
  roles:
    schema: gpt
    bootloader: grub
    structure:
`
	if data != "-" {
		h += `
      - name: data
        size: 1G
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-data
`
		if data != "" {
			h += fmt.Sprintf("        filesystem-label: %s\n", data)
		}
	}
	if seed != "-" {
		h += `
      - name: seed
        size: 1G
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        role: system-seed
        filesystem: vfat
`
		if seed != "" {
			h += fmt.Sprintf("        filesystem-label: %s\n", seed)
		}
	}

	if save != "-" {
		h += `
      - name: save
        size: 32M
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        role: system-save
`
		if save != "" {
			h += fmt.Sprintf("        filesystem-label: %s\n", save)
		}
	}

	gi := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(h), nil))

	return gi
}

func (s *validateGadgetTestSuite) TestVolumeRulesConsistencyNoModel(c *C) {
	ginfo := func(hasSeed bool, dataLabel string) *gadget.Info {
		seed := "-"
		if hasSeed {
			seed = ""
		}
		return rolesYaml(c, dataLabel, seed, "-")
	}
	ginfoSeed := func(seedLabel string) *gadget.Info {
		return rolesYaml(c, "", seedLabel, "-")
	}

	for i, tc := range []struct {
		gi  *gadget.Info
		err string
	}{
		// we have the system-seed role
		{ginfo(true, ""), ""},
		{ginfo(true, "foobar"), `.* must have an implicit label or "ubuntu-data", not "foobar"`},
		{ginfo(true, "writable"), `.* must have an implicit label or "ubuntu-data", not "writable"`},
		{ginfo(true, "ubuntu-data"), ""},

		// we don't have the system-seed role (old systems)
		{ginfo(false, ""), ""}, // implicit is ok
		{ginfo(false, "foobar"), `.* must have an implicit label or "writable", not "foobar"`},
		{ginfo(false, "writable"), ""},
		{ginfo(false, "ubuntu-data"), `.* must have an implicit label or "writable", not "ubuntu-data"`},
		{ginfo(false, "WRITABLE"), `.* must have an implicit label or "writable", not "WRITABLE"`},
		{ginfoSeed("ubuntu-seed"), ""},
		// It is a vfat partition so this is fine
		{ginfoSeed("UBUNTU-SEED"), ""},
		{ginfoSeed("ubuntu-foo"), `.* must have an implicit label or "ubuntu-seed", not "ubuntu-foo"`},
	} {
		c.Logf("tc: %d %v", i, tc.gi.Volumes["roles"])
		mylog.Check(gadget.Validate(tc.gi, nil, nil))
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}

	// Check system-seed label
	for i, tc := range []struct {
		l   string
		err string
	}{
		{"", ""},
		{"foobar", `system-seed structure must have an implicit label or "ubuntu-seed", not "foobar"`},
		{"ubuntu-seed", ""},
	} {
		c.Logf("tc: %v %v", i, tc.l)
		gi := rolesYaml(c, "", tc.l, "-")
		mylog.Check(gadget.Validate(gi, nil, nil))
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}

	// Check system-seed without system-data
	gi := rolesYaml(c, "-", "-", "-")
	mylog.Check(gadget.Validate(gi, nil, nil))

	gi = rolesYaml(c, "-", "", "-")
	mylog.Check(gadget.Validate(gi, nil, nil))
	c.Assert(err, ErrorMatches, "the system-seed role requires system-data to be defined")

	// Check system-save
	giWithSave := rolesYaml(c, "", "", "")
	mylog.Check(gadget.Validate(giWithSave, nil, nil))

	// use illegal label on system-save
	giWithSave = rolesYaml(c, "", "", "foo")
	mylog.Check(gadget.Validate(giWithSave, nil, nil))
	c.Assert(err, ErrorMatches, `system-save structure must have an implicit label or "ubuntu-save", not "foo"`)
	// complains when save is alone
	giWithSave = rolesYaml(c, "", "-", "")
	mylog.Check(gadget.Validate(giWithSave, nil, nil))
	c.Assert(err, ErrorMatches, "model does not support the system-save role")
	giWithSave = rolesYaml(c, "-", "-", "")
	mylog.Check(gadget.Validate(giWithSave, nil, nil))
	c.Assert(err, ErrorMatches, "model does not support the system-save role")
}

func (s *validateGadgetTestSuite) TestValidateConsistencyWithoutModelCharacteristics(c *C) {
	for i, tc := range []struct {
		role  string
		label string
		err   string
	}{
		// when model is nil, the system-seed role and ubuntu-data label on the
		// system-data structure should be consistent
		{"system-seed", "", ""},
		{"system-seed", "writable", `must have an implicit label or "ubuntu-data", not "writable"`},
		{"system-seed", "ubuntu-data", ""},
		{"", "", ""},
		{"", "writable", ""},
		{"", "ubuntu-data", `must have an implicit label or "writable", not "ubuntu-data"`},
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

		makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, b.Bytes())
		ginfo := mylog.Check2(gadget.ReadInfo(s.dir, nil))

		mylog.Check(gadget.Validate(ginfo, nil, nil))
		if tc.err != "" {
			c.Check(err, ErrorMatches, ".* "+tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *validateGadgetTestSuite) TestValidateConsistencyWithModelCharacteristics(c *C) {
	bloader := `
volumes:
  pc:
    bootloader: grub
    schema: mbr
    structure:`

	for i, tc := range []struct {
		addSeed   bool
		addBoot   bool
		isClassic bool
		dataLabel string
		noData    bool
		hasModes  bool
		addSave   bool
		saveLabel string
		err       string
	}{
		{addSeed: true, noData: true, hasModes: true, err: "the system-seed role requires system-data to be defined"},
		{addSeed: true, noData: true, hasModes: false, err: "the system-seed role requires system-data to be defined"},
		{addSeed: true, hasModes: true},
		{addSeed: true, err: `model does not support the system-seed role`},
		{
			addSeed: true, dataLabel: "writable", hasModes: true,
			err: `system-data structure must have an implicit label or "ubuntu-data", not "writable"`,
		},
		{
			addSeed: true, dataLabel: "writable",
			err: `model does not support the system-seed role`,
		},
		{addSeed: true, dataLabel: "ubuntu-data", hasModes: true},
		{
			addSeed: true, dataLabel: "ubuntu-data",
			err: `model does not support the system-seed role`,
		},
		{
			dataLabel: "writable", hasModes: true,
			err: `model requires system-seed structure, but none was found`,
		},
		{dataLabel: "writable"},
		{
			dataLabel: "ubuntu-data", hasModes: true,
			err: `model requires system-seed structure, but none was found`,
		},
		{dataLabel: "ubuntu-data", err: `system-data structure must have an implicit label or "writable", not "ubuntu-data"`},
		{addSave: true, hasModes: true, addSeed: true},
		{addSave: true, err: `model does not support the system-save role`},
		{
			addSeed: true, hasModes: true, addSave: true, saveLabel: "foo",
			err: `system-save structure must have an implicit label or "ubuntu-save", not "foo"`,
		},
		{isClassic: true, hasModes: true, addBoot: true},
		{isClassic: true, hasModes: true, addBoot: true, addSave: true},
		{isClassic: true, hasModes: true, addSeed: true, addBoot: true, addSave: true},
		{isClassic: true, hasModes: true, addBoot: true, addSave: true, saveLabel: "ubuntu-save"},
		{isClassic: true, hasModes: true, err: `system-boot and system-data roles are needed on classic`},
		{isClassic: true, hasModes: true, addBoot: true, addSave: true, saveLabel: "random-label", err: `system-save structure must have an implicit label or "ubuntu-save", not "random-label"`},
	} {
		c.Logf("tc: %v %v %v %v", i, tc.addSeed, tc.dataLabel, tc.hasModes)
		b := &bytes.Buffer{}

		fmt.Fprintf(b, bloader)
		if tc.addSeed {
			fmt.Fprintf(b, `
      - name: Recovery
        size: 10M
        type: 83
        role: system-seed`)
		}
		if tc.addBoot {
			fmt.Fprintf(b, `
      - name: Boot
        size: 10M
        type: 83
        role: system-boot`)
		}

		if !tc.noData {
			fmt.Fprintf(b, `
      - name: Data
        size: 10M
        type: 83
        role: system-data
        filesystem-label: %s`, tc.dataLabel)
		}

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

		makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, b.Bytes())

		mod := &gadgettest.ModelCharacteristics{
			IsClassic: tc.isClassic,
			HasModes:  tc.hasModes,
		}
		ginfo := mylog.Check2(gadget.ReadInfo(s.dir, mod))

		mylog.Check(gadget.Validate(ginfo, mod, nil))
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}

	// test error with no volumes
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(bloader))

	mod := &gadgettest.ModelCharacteristics{
		HasModes: true,
	}

	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, mod))

	mylog.Check(gadget.Validate(ginfo, mod, nil))
	c.Assert(err, ErrorMatches, "model requires system-seed partition, but no system-seed or system-data partition found")
}

func (s *validateGadgetTestSuite) TestValidateSystemRoleSplitAcrossVolumes(c *C) {
	// ATM this is not allowed for UC20
	const gadgetYamlContent = `
volumes:
  pc1:
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
  pc2:
    structure:
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
`

	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))

	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, nil))

	mylog.Check(gadget.Validate(ginfo, nil, nil))
	c.Assert(err, ErrorMatches, `system-boot, system-data, and system-save are expected to share the same volume as system-seed`)
}

func (s *validateGadgetTestSuite) TestValidateRoleDuplicated(c *C) {
	for _, role := range []string{"system-seed", "system-seed-null", "system-data", "system-boot", "system-save"} {
		gadgetYamlContent := fmt.Sprintf(`
volumes:
  pc:
    bootloader: grub
    structure:
      - name: foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        role: %[1]s
      - name: bar
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        role: %[1]s
`, role)
		makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))

		ginfo := mylog.Check2(gadget.ReadInfo(s.dir, nil))

		mylog.Check(gadget.Validate(ginfo, nil, nil))
		c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot have more than one partition with %s role`, role))
	}
}

func (s *validateGadgetTestSuite) TestValidateSystemSeedRoleTwiceAcrossVolumes(c *C) {
	for _, role := range []string{"system-seed", "system-seed-null", "system-data", "system-boot", "system-save"} {
		gadgetYamlContent := fmt.Sprintf(`
volumes:
  pc:
    bootloader: grub
    structure:
      - name: foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        role: %[1]s
  other:
    structure:
      - name: bar
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        role: %[1]s
`, role)
		makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))

		ginfo := mylog.Check2(gadget.ReadInfo(s.dir, nil))

		mylog.Check(gadget.Validate(ginfo, nil, nil))
		c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot have more than one partition with %s role across volumes`, role))
	}
}

func (s *validateGadgetTestSuite) TestValidateSystemSeedAndSeedNullRolesAcrossVolumes(c *C) {
	gadgetYamlContent := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        role: system-seed
  other:
    structure:
      - name: bar
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        role: system-seed-null
`
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))

	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, nil))

	mylog.Check(gadget.Validate(ginfo, nil, nil))
	c.Assert(err, ErrorMatches, "cannot have more than one partition with system-seed/system-seed-null role across volumes")
}

func (s *validateGadgetTestSuite) TestRuleValidateHybridGadget(c *C) {
	// this is the kind of volumes setup recommended to be
	// prepared for a possible UC18 -> UC20 transition
	hybridyGadgetYaml := []byte(`volumes:
  hybrid:
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
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
          - source: mmx64.efi
            target: EFI/boot/mmx64.efi
          - source: grub.cfg
            target: EFI/ubuntu/grub.cfg
      - name: Ubuntu Boot
        type: 0FC63DAF-8483-4772-8E79-3D69D8477DE4
        filesystem: ext4
        filesystem-label: ubuntu-boot
        size: 750M
`)

	mod := &gadgettest.ModelCharacteristics{
		IsClassic: false,
	}
	giMeta := mylog.Check2(gadget.InfoFromGadgetYaml(hybridyGadgetYaml, mod))

	mylog.Check(gadget.Validate(giMeta, mod, nil))
	c.Check(err, IsNil)
}

func (s *validateGadgetTestSuite) TestRuleValidateHybridGadgetBrokenDupRole(c *C) {
	// this is consistency-wise broken because of the duplicated
	// system-boot role, of which one is implicit
	brokenGadgetYaml := []byte(`volumes:
  hybrid:
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
        size: 1200M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
          - source: mmx64.efi
            target: EFI/boot/mmx64.efi
          - source: grub.cfg
            target: EFI/ubuntu/grub.cfg
      - name: Ubuntu Boot
        type: 0FC63DAF-8483-4772-8E79-3D69D8477DE4
        filesystem: ext4
        filesystem-label: ubuntu-boot
        role: system-boot
        size: 750M
`)

	mod := &gadgettest.ModelCharacteristics{
		IsClassic: false,
	}
	giMeta := mylog.Check2(gadget.InfoFromGadgetYaml(brokenGadgetYaml, mod))

	mylog.Check(gadget.Validate(giMeta, mod, nil))
	c.Check(err, ErrorMatches, `cannot have more than one partition with system-boot role`)
}

func (s *validateGadgetTestSuite) TestValidateContentRawContent(c *C) {
	gadgetYamlContent := `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        content:
          - image: foo.img
            size: 1
`
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))

	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, nil))

	mylog.Check(gadget.ValidateContent(ginfo, s.dir, ""))
	c.Assert(err, ErrorMatches, `structure #0 \("foo"\): content "foo.img": stat .*/foo.img: no such file or directory`)

	// Now create the file with wrong size
	makeSizedFile(c, filepath.Join(s.dir, "foo.img"), 100, nil)
	ginfo = mylog.Check2(gadget.ReadInfo(s.dir, nil))

	mylog.Check(gadget.ValidateContent(ginfo, s.dir, ""))
	c.Assert(err, ErrorMatches, `structure #0 \("foo"\): content "foo.img" size 100 is larger than declared 1`)

	// Now with the right size
	makeSizedFile(c, filepath.Join(s.dir, "foo.img"), 1, nil)
	ginfo = mylog.Check2(gadget.ReadInfo(s.dir, nil))

	mylog.Check(gadget.ValidateContent(ginfo, s.dir, ""))

}

func (s *validateGadgetTestSuite) TestValidateContentMultiVolumeContent(c *C) {
	gadgetYamlContent := `
volumes:
  first:
    bootloader: grub
    structure:
      - name: first-foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        content:
          - image: first.img
  second:
    structure:
      - name: second-foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        content:
          - image: second.img

`
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))
	// only content for the first volume
	makeSizedFile(c, filepath.Join(s.dir, "first.img"), 1, nil)

	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, nil))

	mylog.Check(gadget.ValidateContent(ginfo, s.dir, ""))
	c.Assert(err, ErrorMatches, `structure #0 \("second-foo"\): content "second.img": stat .*/second.img: no such file or directory`)
}

func (s *validateGadgetTestSuite) TestValidateContentFilesystemContent(c *C) {
	gadgetYamlContent := `
volumes:
  bad:
    bootloader: grub
    structure:
      - name: bad-struct
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        filesystem: ext4
        content:
          - source: foo/
            target: /

`
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))

	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, nil))

	mylog.Check(gadget.ValidateContent(ginfo, s.dir, ""))
	c.Assert(err, ErrorMatches, `invalid volume "bad": structure #0 \("bad-struct"\), content source:foo/: source path does not exist`)

	// make it a file, which conflicts with foo/ as 'source'
	fooPath := filepath.Join(s.dir, "foo")
	makeSizedFile(c, fooPath, 1, nil)
	mylog.Check(gadget.ValidateContent(ginfo, s.dir, ""))
	c.Assert(err, ErrorMatches, `invalid volume "bad": structure #0 \("bad-struct"\), content source:foo/: cannot specify trailing / for a source which is not a directory`)
	mylog.

		// make it a directory
		Check(os.Remove(fooPath))

	mylog.Check(os.Mkdir(fooPath, 0755))

	mylog.
		// validate should no longer complain
		Check(gadget.ValidateContent(ginfo, s.dir, ""))

}

var gadgetYamlContentNoSave = `
volumes:
  vol1:
    bootloader: grub
    structure:
      - name: ubuntu-seed
        role: system-seed
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        filesystem: ext4
      - name: ubuntu-boot
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        filesystem: ext4
      - name: ubuntu-data
        role: system-data
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        filesystem: ext4
`

var gadgetYamlContentWithSave = gadgetYamlContentNoSave + `
      - name: ubuntu-save
        role: system-save
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        filesystem: ext4
`

func (s *validateGadgetTestSuite) TestValidateEncryptionSupportErr(c *C) {
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContentNoSave))

	mod := &gadgettest.ModelCharacteristics{HasModes: true}
	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, mod))

	mylog.Check(gadget.Validate(ginfo, mod, &gadget.ValidationConstraints{
		EncryptedData: true,
	}))
	c.Assert(err, ErrorMatches, `gadget does not support encrypted data: required partition with system-save role is missing`)
}

func (s *validateGadgetTestSuite) TestValidateEncryptionSupportHappy(c *C) {
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContentWithSave))
	mod := &gadgettest.ModelCharacteristics{HasModes: true}
	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, mod))

	mylog.Check(gadget.Validate(ginfo, mod, &gadget.ValidationConstraints{
		EncryptedData: true,
	}))

}

func (s *validateGadgetTestSuite) TestValidateEncryptionSupportNoUC20(c *C) {
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlPC))

	mod := &gadgettest.ModelCharacteristics{HasModes: false}
	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, mod))

	mylog.Check(gadget.Validate(ginfo, mod, &gadget.ValidationConstraints{
		EncryptedData: true,
	}))
	c.Assert(err, ErrorMatches, `internal error: cannot support encrypted data in a system without modes`)
}

func (s *validateGadgetTestSuite) TestValidateEncryptionSupportMultiVolumeHappy(c *C) {
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(mockMultiVolumeUC20GadgetYaml))
	mod := &gadgettest.ModelCharacteristics{HasModes: true}
	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, mod))

	mylog.Check(gadget.Validate(ginfo, mod, &gadget.ValidationConstraints{
		EncryptedData: true,
	}))

}

var gadgetYamlContentKernelRef = gadgetYamlContentNoSave + `
      - name: other
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 10M
        filesystem: ext4
        content:
          - source: REPLACE_WITH_TC
            target: /
`

func (s *validateGadgetTestSuite) TestValidateContentKernelAssetsRef(c *C) {
	for _, tc := range []struct {
		source, asset, content string
		good                   bool
	}{
		{"$kernel:a/b", "a", "b", true},
		{"$kernel:A/b", "A", "b", true},
		{"$kernel:a-a/bb", "a-a", "bb", true},
		{"$kernel:a-a/b-b", "a-a", "b-b", true},
		{"$kernel:aB-0/cD-3", "aB-0", "cD-3", true},
		{"$kernel:aB-0/foo-21B.dtb", "aB-0", "foo-21B.dtb", true},
		{"$kernel:aB-0/nested/bar-77A.raw", "aB-0", "nested/bar-77A.raw", true},
		{"$kernel:a/a/", "a", "a/", true},
		// no starting with "-"
		{source: "$kernel:-/-"},
		// assets and content need to be there
		{source: "$kernel:ab"},
		{source: "$kernel:/"},
		{source: "$kernel:a/"},
		{source: "$kernel:/a"},
		// invalid asset name
		{source: "$kernel:#garbage/a"},
		// invalid content part
		{source: "$kernel:a//"},
		{source: "$kernel:a///"},
		{source: "$kernel:a////"},
		{source: "$kernel:a/a/../"},
	} {
		gadgetYaml := strings.Replace(gadgetYamlContentKernelRef, "REPLACE_WITH_TC", tc.source, -1)
		makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYaml))
		ginfo := mylog.Check2(gadget.ReadInfoAndValidate(s.dir, nil, nil))

		mylog.Check(gadget.ValidateContent(ginfo, s.dir, ""))
		if tc.good {
			c.Check(err, IsNil, Commentf(tc.source))
			// asset validates correctly, so let's make sure that
			// individual pieces are correct too
			assetName, content := mylog.Check3(gadget.SplitKernelRef(tc.source))

			c.Check(assetName, Equals, tc.asset)
			c.Check(content, Equals, tc.content)
		} else {
			errStr := fmt.Sprintf(`invalid volume "vol1": cannot use kernel reference "%s": .*`, regexp.QuoteMeta(tc.source))
			c.Check(err, ErrorMatches, errStr, Commentf(tc.source))
		}
	}
}

func (s *validateGadgetTestSuite) TestSplitKernelRefErrors(c *C) {
	for _, tc := range []struct {
		kernelRef string
		errStr    string
	}{
		{"no-kernel-ref", `internal error: splitKernelRef called for non kernel ref "no-kernel-ref"`},
		{"$kernel:a", `invalid asset and content in kernel ref "\$kernel:a"`},
		{"$kernel:a/", `missing asset name or content in kernel ref "\$kernel:a/"`},
		{"$kernel:/b", `missing asset name or content in kernel ref "\$kernel:/b"`},
		{"$kernel:a!invalid/b", `invalid asset name in kernel ref "\$kernel:a!invalid/b"`},
		{"$kernel:a/b/..", `invalid content in kernel ref "\$kernel:a/b/.."`},
		{"$kernel:a/b//", `invalid content in kernel ref "\$kernel:a/b//"`},
		{"$kernel:a/b/./", `invalid content in kernel ref "\$kernel:a/b/./"`},
	} {
		_, _ := mylog.Check3(gadget.SplitKernelRef(tc.kernelRef))
		c.Check(err, ErrorMatches, tc.errStr, Commentf("kernelRef: %s", tc.kernelRef))
	}
}

func (s *validateGadgetTestSuite) TestCanResolveOneVolumeKernelRef(c *C) {
	lv := &gadget.LaidOutVolume{
		Volume: &gadget.Volume{
			Bootloader: "grub",
			Schema:     "gpt",
			Structure: []gadget.VolumeStructure{
				{
					Name:       "foo",
					Size:       5 * quantity.SizeMiB,
					Filesystem: "ext4",
				},
			},
		},
	}

	contentNoKernelRef := []gadget.VolumeContent{
		{UnresolvedSource: "/content", Target: "/"},
	}
	contentOneKernelRef := []gadget.VolumeContent{
		{UnresolvedSource: "/content", Target: "/"},
		{UnresolvedSource: "$kernel:ref/foo", Target: "/"},
	}
	contentTwoKernelRefs := []gadget.VolumeContent{
		{UnresolvedSource: "/content", Target: "/"},
		{UnresolvedSource: "$kernel:ref/foo", Target: "/"},
		{UnresolvedSource: "$kernel:ref2/bar", Target: "/"},
	}

	kInfoNoRefs := &kernel.Info{}
	kInfoOneRefButUpdateFlagFalse := &kernel.Info{
		Assets: map[string]*kernel.Asset{
			"ref": {
				// note that update is false here
				Update:  false,
				Content: []string{"some-file"},
			},
		},
	}
	kInfoOneRef := &kernel.Info{
		Assets: map[string]*kernel.Asset{
			"ref": {
				Update:  true,
				Content: []string{"some-file"},
			},
		},
	}
	kInfoOneRefDifferentName := &kernel.Info{
		Assets: map[string]*kernel.Asset{
			"ref-other": {
				Update:  true,
				Content: []string{"some-file"},
			},
		},
	}
	kInfoTwoRefs := &kernel.Info{
		Assets: map[string]*kernel.Asset{
			"ref": {
				Update:  true,
				Content: []string{"some-file"},
			},
			"ref2": {
				Update:  true,
				Content: []string{"other-file"},
			},
		},
	}

	for _, tc := range []struct {
		volumeContent  []gadget.VolumeContent
		kinfo          *kernel.Info
		consumed       bool
		consumedErr    string
		consumesOneErr string
	}{
		// happy case: trivial
		{contentNoKernelRef, kInfoNoRefs, false, "", ""},

		// happy case: if kernel asset has "Update: false"
		{contentNoKernelRef, kInfoOneRefButUpdateFlagFalse, false, "", ""},

		// unhappy case: kernel has one or more unresolved references in gadget
		{contentNoKernelRef, kInfoOneRef, false, "", "gadget does not consume any of the kernel assets needing synced update \"ref\""},
		{contentNoKernelRef, kInfoTwoRefs, false, "", "gadget does not consume any of the kernel assets needing synced update \"ref\", \"ref2\""},

		// unhappy case: gadget needs different asset than kernel provides
		{contentOneKernelRef, kInfoOneRefDifferentName, false, "", "gadget does not consume any of the kernel assets needing synced update \"ref-other\""},

		// happy case: exactly one matching kernel ref
		{contentOneKernelRef, kInfoOneRef, true, "", ""},
		// happy case: one matching, one missing kernel ref, still considered fine
		{contentTwoKernelRefs, kInfoTwoRefs, true, "", ""},
	} {
		lv.Structure[0].Content = tc.volumeContent
		consumed := mylog.Check2(gadget.GadgetVolumeKernelUpdateAssetsConsumed(lv.Volume, tc.kinfo))
		if tc.consumedErr == "" {
			c.Check(err, IsNil, Commentf("should not fail %v", tc.volumeContent))
			c.Check(consumed, Equals, tc.consumed)
		} else {
			c.Check(err, ErrorMatches, tc.consumedErr, Commentf("should fail %v", tc.volumeContent))
		}
		mylog.Check(gadget.GadgetVolumeConsumesOneKernelUpdateAsset(lv.Volume, tc.kinfo))
		if tc.consumesOneErr == "" {
			c.Check(err, IsNil, Commentf("should not fail %v", tc.volumeContent))
		} else {
			c.Check(err, ErrorMatches, tc.consumesOneErr, Commentf("should fail %v", tc.volumeContent))
		}
	}
}

func (s *validateGadgetTestSuite) TestValidateContentKernelRefMissing(c *C) {
	gadgetYamlContent := `
volumes:
  first:
    bootloader: grub
    structure:
      - name: first-foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        content:
          - image: first.img
  second:
    structure:
      - name: second-foo
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 128M
        content:
          - source: $kernel:ref/foo
            target: /
`
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))
	makeSizedFile(c, filepath.Join(s.dir, "first.img"), 1, nil)

	// note that there is no kernel.yaml
	kernelUnpackDir := c.MkDir()

	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, nil))

	mylog.Check(gadget.ValidateContent(ginfo, s.dir, kernelUnpackDir))
	c.Assert(err, ErrorMatches, `.*cannot find "ref" in kernel info.*`)
}

func (s *validateGadgetTestSuite) TestValidateContentKernelRefNotInGadget(c *C) {
	gadgetYamlContent := `
volumes:
  first:
    bootloader: grub
    structure:
      - name: first-foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        content:
          - image: first.img
  second:
    structure:
      - name: second-foo
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 128M
        content:
          - source: foo
            target: /
`
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))
	makeSizedFile(c, filepath.Join(s.dir, "first.img"), 1, nil)
	makeSizedFile(c, filepath.Join(s.dir, "foo"), 1, nil)

	kernelUnpackDir := c.MkDir()
	kernelYamlContent := `
assets:
 ref:
  update: true
  content:
   - dtbs/`
	makeSizedFile(c, filepath.Join(kernelUnpackDir, "meta/kernel.yaml"), 0, []byte(kernelYamlContent))
	makeSizedFile(c, filepath.Join(kernelUnpackDir, "dtbs/foo.dtb"), 0, []byte("foo.dtb content"))

	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, nil))

	mylog.Check(gadget.ValidateContent(ginfo, s.dir, kernelUnpackDir))
	c.Assert(err, ErrorMatches, `no asset from the kernel.yaml needing synced update is consumed by the gadget at "/.*"`)
}

func (s *validateGadgetTestSuite) TestValidateClassicWithModesGadget(c *C) {
	gadgetYaml := `volumes:
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
      - name: EFI System partition
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 99M
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        offset: 1202M
        size: 750M
      - name: ubuntu-save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 16M
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 4312776192
`

	mod := &gadgettest.ModelCharacteristics{
		IsClassic: true,
		HasModes:  true,
	}
	giMeta := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(gadgetYaml), mod))

	mylog.Check(gadget.Validate(giMeta, mod, nil))
	c.Check(err, IsNil)
}

func (s *validateGadgetTestSuite) TestValidateSystemRoleSplitAcrossVolumesClassicOk(c *C) {
	// This is allowed for classic with modes
	const gadgetYaml = `
volumes:
  pc1:
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
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        # whats the appropriate size?
        size: 750M
      - name: ubuntu-save
        role: system-save
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 16M
  pc2:
    structure:
      - name: ubuntu-data
        role: system-data
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        size: 1G
`

	mod := &gadgettest.ModelCharacteristics{
		IsClassic: true,
		HasModes:  true,
	}
	giMeta := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(gadgetYaml), mod))

	mylog.Check(gadget.Validate(giMeta, mod, nil))
	c.Check(err, IsNil)
}

func (s *validateGadgetTestSuite) TestValidateSystemRoleSplitAcrossVolumesClassicFail(c *C) {
	// This is not allowed for classic with modes
	const gadgetYaml = `
volumes:
  pc1:
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
      - name: ubuntu-seed
        role: system-seed
        filesystem: vfat
        # UEFI will boot the ESP partition by default first
        type: EF,C12A7328-F81F-11D2-BA4B-00A0C93EC93B
        size: 1200M
      - name: ubuntu-boot
        role: system-boot
        filesystem: ext4
        type: 83,0FC63DAF-8483-4772-8E79-3D69D8477DE4
        # whats the appropriate size?
        size: 750M
  pc2:
    structure:
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
`

	mod := &gadgettest.ModelCharacteristics{
		IsClassic: true,
		HasModes:  true,
	}
	giMeta := mylog.Check2(gadget.InfoFromGadgetYaml([]byte(gadgetYaml), mod))

	mylog.Check(gadget.Validate(giMeta, mod, nil))
	c.Check(err, ErrorMatches, `system-boot and system-save are expected to share the same volume`)
}

func (s *validateGadgetTestSuite) TestValidateClassicWithModesNoEncryptHappy(c *C) {
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgettest.SingleVolumeClassicwithModesNoEncryptGadgetYaml))
	mod := &gadgettest.ModelCharacteristics{HasModes: true, IsClassic: true}
	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, mod))

	mylog.Check(gadget.Validate(ginfo, mod, &gadget.ValidationConstraints{
		EncryptedData: true,
	}))
	c.Assert(err, ErrorMatches, `gadget does not support encrypted data: required partition with system-save role is missing`)
	mylog.

		// Now validate without model
		Check(gadget.Validate(ginfo, nil, &gadget.ValidationConstraints{
			EncryptedData: true,
		}))
	c.Assert(err, ErrorMatches, `gadget does not support encrypted data: required partition with system-save role is missing`)
	mylog.

		// Should be fine if no encryption
		Check(gadget.Validate(ginfo, mod, &gadget.ValidationConstraints{}))

	mylog.

		// Now validate without model
		Check(gadget.Validate(ginfo, nil, &gadget.ValidationConstraints{}))

}

func (s *validateGadgetTestSuite) TestValidateClassicWithModesEncryptHappy(c *C) {
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgettest.SingleVolumeClassicwithModesEncryptGadgetYaml))
	mod := &gadgettest.ModelCharacteristics{HasModes: true, IsClassic: true}
	ginfo := mylog.Check2(gadget.ReadInfo(s.dir, mod))

	mylog.Check(gadget.Validate(ginfo, mod, &gadget.ValidationConstraints{
		EncryptedData: true,
	}))

	mylog.

		// Now validate without model
		Check(gadget.Validate(ginfo, nil, &gadget.ValidationConstraints{
			EncryptedData: true,
		}))

	mylog.

		// Should be fine if no encryption
		Check(gadget.Validate(ginfo, mod, &gadget.ValidationConstraints{}))

	mylog.

		// Now validate without model
		Check(gadget.Validate(ginfo, nil, &gadget.ValidationConstraints{}))

}
