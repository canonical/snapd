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

package devicestate_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/overlord/devicestate"
)

type helpersTestSuite struct {
}

var _ = Suite(&helpersTestSuite{})

const gadgetData = `
volumes:
  pc:
    bootloader: grub
    structure:
      - name: mbr
        type: mbr
        size: 440
      - name: SomeStructure
        type: DA,21686148-6449-6E6F-744E-656564454649
        size: 1M
        offset: 1M
        filesystem-label: some-label
`

func (s *helpersTestSuite) TestPartitionFromLabel(c *C) {
	d := c.MkDir()
	err := os.MkdirAll(filepath.Join(d, "meta"), 0755)
	c.Assert(err, IsNil)
	gadgetYaml := filepath.Join(d, "meta", "gadget.yaml")
	err = ioutil.WriteFile(gadgetYaml, []byte(gadgetData), 0644)
	c.Assert(err, IsNil)

	restore := devicestate.MockGadgetFindDeviceForStructure(func(ps *gadget.LaidOutStructure) (string, error) {
		c.Assert(ps.VolumeStructure.Name, Equals, "SomeStructure")
		return "some-node", nil
	})
	defer restore()

	// test existing label
	part, err := devicestate.PartitionFromLabel(d, "some-label")
	c.Assert(err, IsNil)
	c.Assert(part, Equals, "some-node")
}

func (s *helpersTestSuite) TestPartitionFromLabelError(c *C) {
	d := c.MkDir()
	err := os.MkdirAll(filepath.Join(d, "meta"), 0755)
	c.Assert(err, IsNil)
	gadgetYaml := filepath.Join(d, "meta", "gadget.yaml")
	err = ioutil.WriteFile(gadgetYaml, []byte(gadgetData), 0644)
	c.Assert(err, IsNil)

	// test non-existing label
	_, err = devicestate.PartitionFromLabel(d, "doesnt-exist")
	c.Assert(err, ErrorMatches, `cannot find structure with label "doesnt-exist"`)
}

func (s *helpersTestSuite) TestDiskFromPartition(c *C) {
	d := c.MkDir()

	// create sys/class/block/part -> ../../devices/node/node2
	classDir := filepath.Join(d, "sys", "class", "block")
	err := os.MkdirAll(classDir, 0755)
	c.Assert(err, IsNil)
	err = os.Symlink("../../devices/node/node2", filepath.Join(classDir, "part"))
	c.Assert(err, IsNil)

	// create sys/devices/node/dev with maj:min 1:2
	devicesDir := filepath.Join(d, "sys", "devices", "node")
	err = os.MkdirAll(filepath.Join(devicesDir, "node2"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(devicesDir, "dev"), []byte("1:2\n"), 0644)
	c.Assert(err, IsNil)

	// create dev/block/1:2 -> ../disknode
	blockDir := filepath.Join(d, "dev", "block")
	err = os.MkdirAll(blockDir, 0755)
	c.Assert(err, IsNil)
	err = os.Symlink("../disknode", filepath.Join(blockDir, "1:2"))
	c.Assert(err, IsNil)

	// create dev/disknode
	err = ioutil.WriteFile(filepath.Join(d, "dev", "disknode"), []byte{}, 0644)
	c.Assert(err, IsNil)

	restoreSysClassBlock := devicestate.MockSysClassBlock(classDir)
	defer restoreSysClassBlock()

	restoreDevBlock := devicestate.MockDevBlock(blockDir)
	defer restoreDevBlock()

	// test existing device
	device, err := devicestate.DiskFromPartition("/dev/part")
	c.Assert(err, IsNil)
	c.Assert(device, Equals, filepath.Join(d, "dev", "disknode"))

	// test non-existing device
	device, err = devicestate.DiskFromPartition("/dev/doesnt_exist")
	c.Assert(err, ErrorMatches, "cannot resolve symlink.*")
}
