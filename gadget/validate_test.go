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
