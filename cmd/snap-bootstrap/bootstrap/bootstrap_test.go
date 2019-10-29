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
package bootstrap_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/cmd/snap-bootstrap/bootstrap"
	"github.com/snapcore/snapd/gadget"
)

func TestBootstrap(t *testing.T) { TestingT(t) }

type bootstrapSuite struct{}

var _ = Suite(&bootstrapSuite{})

// XXX: write a very high level integration like test here that
// mocks the world (sfdisk,lsblk,mkfs,...)? probably silly as
// each part inside bootstrap is tested and we have a spread test

func (s *bootstrapSuite) TestBootstrapRunError(c *C) {
	err := bootstrap.Run("", "", nil)
	c.Assert(err, ErrorMatches, "cannot use empty gadget root directory")

	err = bootstrap.Run("some-dir", "", nil)
	c.Assert(err, ErrorMatches, "cannot use empty device node")
}

const mockGadgetYaml = `volumes:
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

func (s *bootstrapSuite) TestLayoutCompatibility(c *C) {
	// same contents
	layout1 := layoutFromYaml(c, mockGadgetYaml, "pc")
	layout2 := layoutFromYaml(c, mockGadgetYaml, "pc")
	err := bootstrap.EnsureLayoutCompatibility(layout1, layout2)
	c.Assert(err, IsNil)

	// missing structure (that's ok)
	layout1 = layoutFromYaml(c, mockGadgetYaml+mockExtraStructure, "pc")
	err = bootstrap.EnsureLayoutCompatibility(layout1, layout2)
	c.Assert(err, IsNil)

	// extra structure (should fail)
	err = bootstrap.EnsureLayoutCompatibility(layout2, layout1)
	c.Assert(err, ErrorMatches, `cannot find disk partition "writable".* in gadget`)
}

func layoutFromYaml(c *C, gadgetYaml, volume string) *gadget.LaidOutVolume {
	gadgetRoot := filepath.Join(c.MkDir(), "gadget")
	err := os.MkdirAll(filepath.Join(gadgetRoot, "meta"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(gadgetRoot, "meta", "gadget.yaml"), []byte(gadgetYaml), 0644)
	c.Assert(err, IsNil)
	pv, err := gadget.PositionedVolumeFromGadget(gadgetRoot)
	c.Assert(err, IsNil)
	return pv
}
