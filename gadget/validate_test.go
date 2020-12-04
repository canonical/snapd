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
		{state(true, "foobar"), "system-data structure must not have a label"},
		{state(true, "writable"), "system-data structure must not have a label"},
		{state(true, "ubuntu-data"), "system-data structure must not have a label"},

		// we don't have the system-seed role (old systems)
		{state(false, ""), ""}, // implicit is ok
		{state(false, "foobar"), `.* must have an implicit label or "writable", not "foobar"`},
		{state(false, "writable"), ""},
		{state(false, "ubuntu-data"), `.* must have an implicit label or "writable", not "ubuntu-data"`},
	} {
		c.Logf("tc: %v %p %v", i, tc.s.SystemSeed, tc.s.SystemData.Label)

		err := gadget.EnsureVolumeRuleConsistency(tc.s, nil)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
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
		{"foobar", "system-seed structure must not have a label"},
		{"ubuntu-seed", "system-seed structure must not have a label"},
	} {
		c.Logf("tc: %v %v", i, tc.l)
		s := state(true, "")
		s.SystemSeed.Label = tc.l
		err := gadget.EnsureVolumeRuleConsistency(s, nil)
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
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
	c.Assert(err, ErrorMatches, "system-save structure must not have a label")
	// complains when either system-seed or system-data is missing
	vsWithSave.SystemSeed = nil
	err = gadget.EnsureVolumeRuleConsistency(vsWithSave, nil)
	c.Assert(err, ErrorMatches, "system-save requires system-seed and system-data structures")
	vsWithSave.SystemData = nil
	err = gadget.EnsureVolumeRuleConsistency(vsWithSave, nil)
	c.Assert(err, ErrorMatches, "system-save requires system-seed and system-data structures")
}

func (s *validateGadgetTestSuite) TestValidateMissingRawContent(c *C) {
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

	err := gadget.Validate(s.dir, nil, nil)
	c.Assert(err, ErrorMatches, `invalid layout of volume "pc": cannot lay out structure #0 \("foo"\): content "foo.img": stat .*/foo.img: no such file or directory`)
}

func (s *validateGadgetTestSuite) TestValidateMultiVolumeContent(c *C) {
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

	err := gadget.Validate(s.dir, nil, nil)
	c.Assert(err, ErrorMatches, `invalid layout of volume "second": cannot lay out structure #0 \("second-foo"\): content "second.img": stat .*/second.img: no such file or directory`)
}

func (s *validateGadgetTestSuite) TestValidateBorkedMeta(c *C) {
	var gadgetYamlContent = `
volumes:
  borked:
    bootloader: bleh
    structure:
      - name: first-foo
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M

`
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))

	err := gadget.Validate(s.dir, nil, nil)
	c.Assert(err, ErrorMatches, `invalid gadget metadata: bootloader must be one of .*`)
}

func (s *validateGadgetTestSuite) TestValidateFilesystemContent(c *C) {
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

	err := gadget.Validate(s.dir, nil, nil)
	c.Assert(err, ErrorMatches, `invalid volume "bad": structure #0 \("bad-struct"\), content source:foo/: source path does not exist`)

	// make it a file, which conflicts with foo/ as 'source'
	fooPath := filepath.Join(s.dir, "foo")
	makeSizedFile(c, fooPath, 1, nil)
	err = gadget.Validate(s.dir, nil, nil)
	c.Assert(err, ErrorMatches, `invalid volume "bad": structure #0 \("bad-struct"\), content source:foo/: cannot specify trailing / for a source which is not a directory`)

	// make it a directory
	err = os.Remove(fooPath)
	c.Assert(err, IsNil)
	err = os.Mkdir(fooPath, 0755)
	c.Assert(err, IsNil)
	// validate should no longer complain
	err = gadget.Validate(s.dir, nil, nil)
	c.Assert(err, IsNil)
}

func (s *validateGadgetTestSuite) TestValidateClassic(c *C) {
	var gadgetYamlContent = `
# on classic this can be empty
`
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContent))

	err := gadget.Validate(s.dir, nil, nil)
	c.Assert(err, IsNil)

	err = gadget.Validate(s.dir, &modelConstraints{classic: true}, nil)
	c.Assert(err, IsNil)

	err = gadget.Validate(s.dir, &modelConstraints{classic: false}, nil)
	c.Assert(err, ErrorMatches, "invalid gadget metadata: bootloader not declared in any volume")
}

func (s *validateGadgetTestSuite) TestValidateSystemSeedRoleTwice(c *C) {

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
		err := gadget.Validate(s.dir, nil, nil)
		c.Assert(err, ErrorMatches, fmt.Sprintf(`invalid gadget metadata: invalid volume "pc": cannot have more than one partition with %s role`, role))
	}
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
	err := gadget.Validate(s.dir, &modelConstraints{systemSeed: true}, &gadget.ValidationConstraints{
		EncryptedData: true,
	})
	c.Assert(err, ErrorMatches, `gadget does not support encrypted data: volume "vol1" has no structure with system-save role`)
}

func (s *validateGadgetTestSuite) TestValidateEncryptionSupportHappy(c *C) {
	makeSizedFile(c, filepath.Join(s.dir, "meta/gadget.yaml"), 0, []byte(gadgetYamlContentWithSave))
	err := gadget.Validate(s.dir, &modelConstraints{systemSeed: true}, &gadget.ValidationConstraints{
		EncryptedData: true,
	})
	c.Assert(err, IsNil)
}
