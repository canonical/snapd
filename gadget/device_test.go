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
	// "fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
)

type deviceSuite struct {
	dir string
}

var _ = Suite(&deviceSuite{})

func (d *deviceSuite) SetUpTest(c *C) {
	d.dir = c.MkDir()
	dirs.SetRootDir(d.dir)

	err := os.MkdirAll(filepath.Join(d.dir, "/dev/disk/by-label"), 0755)
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(d.dir, "/dev/disk/by-partlabel"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(d.dir, "/dev/fakedevice"), []byte(""), 0644)
	c.Assert(err, IsNil)
}

func (d *deviceSuite) TestDeviceFindByStructureName(c *C) {
	names := []struct {
		escaped   string
		structure string
	}{
		{"foo", "foo"},
		{"123", "123"},
		{"foo\\x20bar", "foo bar"},
		{"foo\\x23bar", "foo#bar"},
	}
	for _, name := range names {
		err := os.Symlink(filepath.Join(d.dir, "/dev/fakedevice"), filepath.Join(d.dir, "/dev/disk/by-partlabel", name.escaped))
		c.Assert(err, IsNil)
	}

	for _, tc := range names {
		c.Logf("trying: %q", tc)
		found, err := gadget.FindDeviceForStructure(&gadget.PositionedStructure{
			VolumeStructure: &gadget.VolumeStructure{Name: tc.structure},
		})
		c.Check(err, IsNil)
		c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice"))
	}
}

func (d *deviceSuite) TestDeviceFindByFilesystemLabel(c *C) {
	names := []struct {
		escaped   string
		structure string
	}{
		{"foo", "foo"},
		{"123", "123"},
		{"foo\\x20bar", "foo bar"},
		{"foo\\x23bar", "foo#bar"},
	}
	for _, name := range names {
		err := os.Symlink(filepath.Join(d.dir, "/dev/fakedevice"), filepath.Join(d.dir, "/dev/disk/by-label", name.escaped))
		c.Assert(err, IsNil)
	}

	for _, tc := range names {
		c.Logf("trying: %q", tc)
		found, err := gadget.FindDeviceForStructure(&gadget.PositionedStructure{
			VolumeStructure: &gadget.VolumeStructure{Label: tc.structure},
		})
		c.Check(err, IsNil)
		c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice"))
	}
}

func (d *deviceSuite) TestDeviceFindChecksPartlabelAndFilesystemLabelHappy(c *C) {
	fakedevice := filepath.Join(d.dir, "/dev/fakedevice")
	err := os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/foo"))
	c.Assert(err, IsNil)

	err = os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-partlabel/bar"))
	c.Assert(err, IsNil)

	found, err := gadget.FindDeviceForStructure(&gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:  "bar",
			Label: "foo",
		},
	})
	c.Check(err, IsNil)
	c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice"))
}

func (d *deviceSuite) TestDeviceFindChecksPartlabelAndFilesystemLabelMismatch(c *C) {
	fakedevice := filepath.Join(d.dir, "/dev/fakedevice")
	err := os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/foo"))
	c.Assert(err, IsNil)

	// partlabel of the structure points to a different device
	fakedeviceOther := filepath.Join(d.dir, "/dev/fakedevice-other")
	err = ioutil.WriteFile(fakedeviceOther, []byte(""), 0644)
	c.Assert(err, IsNil)
	err = os.Symlink(fakedeviceOther, filepath.Join(d.dir, "/dev/disk/by-partlabel/bar"))
	c.Assert(err, IsNil)

	found, err := gadget.FindDeviceForStructure(&gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:  "bar",
			Label: "foo",
		},
	})
	c.Check(err, ErrorMatches, `conflicting device match, ".*/by-label/foo" points to ".*/fakedevice", previous match ".*/by-partlabel/bar" points to ".*/fakedevice-other"`)
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestDeviceFindNotFound(c *C) {
	found, err := gadget.FindDeviceForStructure(&gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:  "bar",
			Label: "foo",
		},
	})
	c.Check(err, ErrorMatches, `device not found`)
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestDeviceFindNotFoundEmpty(c *C) {
	// neither name nor filesystem label set
	found, err := gadget.FindDeviceForStructure(&gadget.PositionedStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:  "",
			Label: "",
		},
	})
	c.Check(err, ErrorMatches, `device not found`)
	c.Check(found, Equals, "")
}
