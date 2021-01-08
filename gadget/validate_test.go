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
	"os"
	"path/filepath"
	"regexp"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
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
		role, label, err string
		model            gadget.Model
	}{
		{label: "ubuntu-seed", err: `label "ubuntu-seed" is reserved`},
		{label: "ubuntu-data", err: `label "ubuntu-data" is reserved`},
		// ok to allow hybrid 20-ready devices
		{label: "ubuntu-boot"},
		{label: "ubuntu-save"},
		// reserved only if seed present/expected
		{label: "ubuntu-boot", err: `label "ubuntu-boot" is reserved`, model: uc20Mod},
		{label: "ubuntu-save", err: `label "ubuntu-save" is reserved`, model: uc20Mod},
		// these are ok
		{role: "system-boot", label: "ubuntu-boot"},
		{label: "random-ubuntu-label"},
	} {
		gi := &gadget.Info{
			Volumes: map[string]*gadget.Volume{
				"vol0": {
					Structure: []gadget.VolumeStructure{{
						Type:       "21686148-6449-6E6F-744E-656564454649",
						Role:       tc.role,
						Filesystem: "ext4",
						Label:      tc.label,
						Size:       10 * 1024,
					}},
				},
			},
		}
		err := gadget.Validate(gi, tc.model, nil)
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

	gi, err := gadget.InfoFromGadgetYaml([]byte(h), nil)
	c.Assert(err, IsNil)
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
	} {
		c.Logf("tc: %d %v", i, tc.gi.Volumes["roles"])

		err := gadget.Validate(tc.gi, nil, nil)
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
		err := gadget.Validate(gi, nil, nil)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}

	// Check system-seed without system-data
	gi := rolesYaml(c, "-", "-", "-")
	err := gadget.Validate(gi, nil, nil)
	c.Assert(err, IsNil)
	gi = rolesYaml(c, "-", "", "-")
	err = gadget.Validate(gi, nil, nil)
	c.Assert(err, ErrorMatches, "the system-seed role requires system-data to be defined")

	// Check system-save
	giWithSave := rolesYaml(c, "", "", "")
	err = gadget.Validate(giWithSave, nil, nil)
	c.Assert(err, IsNil)
	// use illegal label on system-save
	giWithSave = rolesYaml(c, "", "", "foo")
	err = gadget.Validate(giWithSave, nil, nil)
	c.Assert(err, ErrorMatches, `system-save structure must have an implicit label or "ubuntu-save", not "foo"`)
	// complains when save is alone
	giWithSave = rolesYaml(c, "", "-", "")
	err = gadget.Validate(giWithSave, nil, nil)
	c.Assert(err, ErrorMatches, "model does not support the system-save role")
	giWithSave = rolesYaml(c, "-", "-", "")
	err = gadget.Validate(giWithSave, nil, nil)
	c.Assert(err, ErrorMatches, "model does not support the system-save role")
}

func (s *validateGadgetTestSuite) TestValidateConsistencyWithoutModelCharateristics(c *C) {
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
		ginfo, err := gadget.ReadInfo(s.dir, nil)
		c.Assert(err, IsNil)
		err = gadget.Validate(ginfo, nil, nil)
		if tc.err != "" {
			c.Check(err, ErrorMatches, ".* "+tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *validateGadgetTestSuite) TestValidateConsistencyWithModelCharateristics(c *C) {
	bloader := `
volumes:
  pc:
    bootloader: grub
    schema: mbr
    structure:`

	for i, tc := range []struct {
		addSeed     bool
		dataLabel   string
		noData      bool
		requireSeed bool
		addSave     bool
		saveLabel   string
		err         string
	}{
		{addSeed: true, noData: true, requireSeed: true, err: "the system-seed role requires system-data to be defined"},
		{addSeed: true, noData: true, requireSeed: false, err: "the system-seed role requires system-data to be defined"},
		{addSeed: true, requireSeed: true},
		{addSeed: true, err: `model does not support the system-seed role`},
		{addSeed: true, dataLabel: "writable", requireSeed: true,
			err: `system-data structure must have an implicit label or "ubuntu-data", not "writable"`},
		{addSeed: true, dataLabel: "writable",
			err: `model does not support the system-seed role`},
		{addSeed: true, dataLabel: "ubuntu-data", requireSeed: true},
		{addSeed: true, dataLabel: "ubuntu-data",
			err: `model does not support the system-seed role`},
		{dataLabel: "writable", requireSeed: true,
			err: `model requires system-seed structure, but none was found`},
		{dataLabel: "writable"},
		{dataLabel: "ubuntu-data", requireSeed: true,
			err: `model requires system-seed structure, but none was found`},
		{dataLabel: "ubuntu-data", err: `system-data structure must have an implicit label or "writable", not "ubuntu-data"`},
		{addSave: true, requireSeed: true, addSeed: true},
		{addSave: true, err: `model does not support the system-save role`},
		{addSeed: true, requireSeed: true, addSave: true, saveLabel: "foo",
			err: `system-save structure must have an implicit label or "ubuntu-save", not "foo"`},
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

		mod := &modelCharateristics{
			classic:    false,
			systemSeed: tc.requireSeed,
		}
		ginfo, err := gadget.ReadInfo(s.dir, mod)
		c.Assert(err, IsNil)
		err = gadget.Validate(ginfo, mod, nil)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}

	// test error with no volumes
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(bloader))

	mod := &modelCharateristics{
		systemSeed: true,
	}

	ginfo, err := gadget.ReadInfo(s.dir, mod)
	c.Assert(err, IsNil)
	err = gadget.Validate(ginfo, mod, nil)
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

	ginfo, err := gadget.ReadInfo(s.dir, nil)
	c.Assert(err, IsNil)
	err = gadget.Validate(ginfo, nil, nil)
	c.Assert(err, ErrorMatches, `system-boot, system-data, and system-save are expected to share the same volume as system-seed`)
}

func (s *validateGadgetTestSuite) TestValidateRoleDuplicated(c *C) {

	for _, role := range []string{"system-seed", "system-data", "system-boot", "system-save"} {
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

		ginfo, err := gadget.ReadInfo(s.dir, nil)
		c.Assert(err, IsNil)
		err = gadget.Validate(ginfo, nil, nil)
		c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot have more than one partition with %s role`, role))
	}
}

func (s *validateGadgetTestSuite) TestValidateSystemSeedRoleTwiceAcrossVolumes(c *C) {

	for _, role := range []string{"system-seed", "system-data", "system-boot", "system-save"} {
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

		ginfo, err := gadget.ReadInfo(s.dir, nil)
		c.Assert(err, IsNil)
		err = gadget.Validate(ginfo, nil, nil)
		c.Assert(err, ErrorMatches, fmt.Sprintf(`cannot have more than one partition with %s role across volumes`, role))
	}
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

	mod := &modelCharateristics{
		classic: false,
	}
	giMeta, err := gadget.InfoFromGadgetYaml(hybridyGadgetYaml, mod)
	c.Assert(err, IsNil)

	err = gadget.Validate(giMeta, mod, nil)
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

	mod := &modelCharateristics{
		classic: false,
	}
	giMeta, err := gadget.InfoFromGadgetYaml(brokenGadgetYaml, mod)
	c.Assert(err, IsNil)

	err = gadget.Validate(giMeta, mod, nil)
	c.Check(err, ErrorMatches, `cannot have more than one partition with system-boot role`)
}

func (s *validateGadgetTestSuite) TestValidateContentMissingRawContent(c *C) {
	var gadgetYamlContent = `
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

`
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))

	ginfo, err := gadget.ReadInfo(s.dir, nil)
	c.Assert(err, IsNil)
	err = gadget.ValidateContent(ginfo, s.dir)
	c.Assert(err, ErrorMatches, `invalid layout of volume "pc": cannot lay out structure #0 \("foo"\): content "foo.img": stat .*/foo.img: no such file or directory`)
}

func (s *validateGadgetTestSuite) TestValidateContentMultiVolumeContent(c *C) {
	var gadgetYamlContent = `
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

	ginfo, err := gadget.ReadInfo(s.dir, nil)
	c.Assert(err, IsNil)
	err = gadget.ValidateContent(ginfo, s.dir)
	c.Assert(err, ErrorMatches, `invalid layout of volume "second": cannot lay out structure #0 \("second-foo"\): content "second.img": stat .*/second.img: no such file or directory`)
}

func (s *validateGadgetTestSuite) TestValidateContentFilesystemContent(c *C) {
	var gadgetYamlContent = `
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

	ginfo, err := gadget.ReadInfo(s.dir, nil)
	c.Assert(err, IsNil)
	err = gadget.ValidateContent(ginfo, s.dir)
	c.Assert(err, ErrorMatches, `invalid volume "bad": structure #0 \("bad-struct"\), content source:foo/: source path does not exist`)

	// make it a file, which conflicts with foo/ as 'source'
	fooPath := filepath.Join(s.dir, "foo")
	makeSizedFile(c, fooPath, 1, nil)
	err = gadget.ValidateContent(ginfo, s.dir)
	c.Assert(err, ErrorMatches, `invalid volume "bad": structure #0 \("bad-struct"\), content source:foo/: cannot specify trailing / for a source which is not a directory`)

	// make it a directory
	err = os.Remove(fooPath)
	c.Assert(err, IsNil)
	err = os.Mkdir(fooPath, 0755)
	c.Assert(err, IsNil)
	// validate should no longer complain
	err = gadget.ValidateContent(ginfo, s.dir)
	c.Assert(err, IsNil)
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

	mod := &modelCharateristics{systemSeed: true}
	ginfo, err := gadget.ReadInfo(s.dir, mod)
	c.Assert(err, IsNil)
	err = gadget.Validate(ginfo, mod, &gadget.ValidationConstraints{
		EncryptedData: true,
	})
	c.Assert(err, ErrorMatches, `gadget does not support encrypted data: volume "vol1" has no structure with system-save role`)
}

func (s *validateGadgetTestSuite) TestValidateEncryptionSupportHappy(c *C) {
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContentWithSave))
	mod := &modelCharateristics{systemSeed: true}
	ginfo, err := gadget.ReadInfo(s.dir, mod)
	c.Assert(err, IsNil)
	err = gadget.Validate(ginfo, mod, &gadget.ValidationConstraints{
		EncryptedData: true,
	})
	c.Assert(err, IsNil)
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
		ginfo, err := gadget.ReadInfoAndValidate(s.dir, nil, nil)
		c.Assert(err, IsNil)
		err = gadget.ValidateContent(ginfo, s.dir)
		if tc.good {
			c.Check(err, IsNil, Commentf(tc.source))
			// asset validates correctly, so let's make sure that
			// individual pieces are correct too
			assetName, content, err := gadget.SplitKernelRef(tc.source)
			c.Assert(err, IsNil)
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
		_, _, err := gadget.SplitKernelRef(tc.kernelRef)
		c.Check(err, ErrorMatches, tc.errStr, Commentf("kernelRef: %s", tc.kernelRef))
	}
}
