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
	}{
		{label: "ubuntu-seed", err: `label "ubuntu-seed" is reserved`},
		// 2020-12-02: disable for customer hotfix
		/*{label: "ubuntu-boot", err: `label "ubuntu-boot" is reserved`},*/
		{label: "ubuntu-data", err: `label "ubuntu-data" is reserved`},
		{label: "ubuntu-save", err: `label "ubuntu-save" is reserved`},
		// these are ok
		{role: "system-boot", label: "ubuntu-boot"},
		{label: "random-ubuntu-label"},
	} {
		err := gadget.RuleValidateVolumeStructure(&gadget.VolumeStructure{
			Type:       "21686148-6449-6E6F-744E-656564454649",
			Role:       tc.role,
			Filesystem: "ext4",
			Label:      tc.label,
			Size:       10 * 1024,
		})
		if tc.err == "" {
			c.Check(err, IsNil)
		} else {
			c.Check(err, ErrorMatches, tc.err)
		}
	}

}

func (s *validateGadgetTestSuite) TestEnsureVolumeRuleConsistency(c *C) {
	state := func(seed bool, label string) *gadget.ValidationState {
		systemDataVolume := &gadget.VolumeStructure{Label: label}
		systemSeedVolume := (*gadget.VolumeStructure)(nil)
		if seed {
			systemSeedVolume = &gadget.VolumeStructure{}
		}
		return &gadget.ValidationState{
			SystemSeed: systemSeedVolume,
			SystemData: systemDataVolume,
		}
	}

	for i, tc := range []struct {
		s   *gadget.ValidationState
		err string
	}{

		// we have the system-seed role
		{state(true, ""), ""},
		{state(true, "foobar"), `.* must have an implicit label or "ubuntu-data", not "foobar"`},
		{state(true, "writable"), `.* must have an implicit label or "ubuntu-data", not "writable"`},
		{state(true, "ubuntu-data"), ""},

		// we don't have the system-seed role (old systems)
		{state(false, ""), ""}, // implicit is ok
		{state(false, "foobar"), `.* must have an implicit label or "writable", not "foobar"`},
		{state(false, "writable"), ""},
		{state(false, "ubuntu-data"), `.* must have an implicit label or "writable", not "ubuntu-data"`},
	} {
		c.Logf("tc: %v %p %v", i, tc.s.SystemSeed, tc.s.SystemData.Label)

		err := gadget.EnsureVolumeRuleConsistency(tc.s, nil)
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
		s := state(true, "")
		s.SystemSeed.Label = tc.l
		err := gadget.EnsureVolumeRuleConsistency(s, nil)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}

	// Check system-seed without system-data
	vs := &gadget.ValidationState{}
	err := gadget.EnsureVolumeRuleConsistency(vs, nil)
	c.Assert(err, IsNil)
	vs.SystemSeed = &gadget.VolumeStructure{}
	err = gadget.EnsureVolumeRuleConsistency(vs, nil)
	c.Assert(err, ErrorMatches, "the system-seed role requires system-data to be defined")

	// Check system-save
	vsWithSave := &gadget.ValidationState{
		SystemData: &gadget.VolumeStructure{},
		SystemSeed: &gadget.VolumeStructure{},
		SystemSave: &gadget.VolumeStructure{},
	}
	err = gadget.EnsureVolumeRuleConsistency(vsWithSave, nil)
	c.Assert(err, IsNil)
	// use illegal label on system-save
	vsWithSave.SystemSave.Label = "foo"
	err = gadget.EnsureVolumeRuleConsistency(vsWithSave, nil)
	c.Assert(err, ErrorMatches, `system-save structure must have an implicit label or "ubuntu-save", not "foo"`)
	// complains when either system-seed or system-data is missing
	vsWithSave.SystemSeed = nil
	err = gadget.EnsureVolumeRuleConsistency(vsWithSave, nil)
	c.Assert(err, ErrorMatches, "system-save requires system-seed and system-data structures")
	vsWithSave.SystemData = nil
	err = gadget.EnsureVolumeRuleConsistency(vsWithSave, nil)
	c.Assert(err, ErrorMatches, "system-save requires system-seed and system-data structures")
}

func (s *validateGadgetTestSuite) TestValidateConsistencyWithoutConstraints(c *C) {
	for i, tc := range []struct {
		role  string
		label string
		err   string
	}{
		// when constraints are nil, the system-seed role and ubuntu-data label on the
		// system-data structure should be consistent
		{"system-seed", "", ""},
		{"system-seed", "writable", `.* must have an implicit label or "ubuntu-data", not "writable"`},
		{"system-seed", "ubuntu-data", ""},
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

		makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, b.Bytes())
		ginfo, err := gadget.ReadInfo(s.dir, nil)
		c.Assert(err, IsNil)
		err = gadget.Validate(ginfo, nil, nil)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}
}

func (s *validateGadgetTestSuite) TestValidateConsistencyWithConstraints(c *C) {
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
			err: `.* system-data structure must have an implicit label or "ubuntu-data", not "writable"`},
		{addSeed: true, dataLabel: "writable",
			err: `.* model does not support the system-seed role`},
		{addSeed: true, dataLabel: "ubuntu-data", requireSeed: true},
		{addSeed: true, dataLabel: "ubuntu-data",
			err: `.* model does not support the system-seed role`},
		{dataLabel: "writable", requireSeed: true,
			err: `.* model requires system-seed structure, but none was found`},
		{dataLabel: "writable"},
		{dataLabel: "ubuntu-data", requireSeed: true,
			err: `.* model requires system-seed structure, but none was found`},
		{dataLabel: "ubuntu-data", err: `.* system-data structure must have an implicit label or "writable", not "ubuntu-data"`},
		{addSave: true, err: `.* system-save requires system-seed and system-data structures`},
		{addSeed: true, requireSeed: true, addSave: true, saveLabel: "foo",
			err: `.* system-save structure must have an implicit label or "ubuntu-save", not "foo"`},
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

		makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, b.Bytes())

		constraints := &modelConstraints{
			classic:    false,
			systemSeed: tc.requireSeed,
		}

		ginfo, err := gadget.ReadInfo(s.dir, constraints)
		c.Assert(err, IsNil)
		err = gadget.Validate(ginfo, constraints, nil)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
		}
	}

	// test error with no volumes
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(bloader))

	constraints := &modelConstraints{
		systemSeed: true,
	}
	ginfo, err := gadget.ReadInfo(s.dir, constraints)
	c.Assert(err, IsNil)
	err = gadget.Validate(ginfo, constraints, nil)
	c.Assert(err, ErrorMatches, ".*: model requires system-seed partition, but no system-seed or system-data partition found")
}

func (s *validateGadgetTestSuite) TestValidateRoleDuplicated(c *C) {

	for _, role := range []string{"system-seed", "system-data", "system-boot"} {
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
		c.Assert(err, ErrorMatches, fmt.Sprintf(`invalid volume "pc": cannot have more than one partition with %s role`, role))
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

	constraints := &modelConstraints{
		classic: false,
	}
	giMeta, err := gadget.InfoFromGadgetYaml(hybridyGadgetYaml, constraints)
	c.Assert(err, IsNil)

	err = gadget.Validate(giMeta, constraints, nil)
	c.Check(err, IsNil)
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

	mod := &modelConstraints{systemSeed: true}
	ginfo, err := gadget.ReadInfo(s.dir, mod)
	c.Assert(err, IsNil)
	err = gadget.Validate(ginfo, mod, &gadget.ValidationConstraints{
		EncryptedData: true,
	})
	c.Assert(err, ErrorMatches, `gadget does not support encrypted data: volume "vol1" has no structure with system-save role`)
}

func (s *validateGadgetTestSuite) TestValidateEncryptionSupportHappy(c *C) {
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContentWithSave))
	mod := &modelConstraints{systemSeed: true}
	ginfo, err := gadget.ReadInfo(s.dir, mod)
	c.Assert(err, IsNil)
	err = gadget.Validate(ginfo, mod, &gadget.ValidationConstraints{
		EncryptedData: true,
	})
	c.Assert(err, IsNil)
}
