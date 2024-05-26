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
	"errors"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
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
	mylog.Check(os.MkdirAll(filepath.Join(d.dir, "/dev/disk/by-label"), 0755))

	mylog.Check(os.MkdirAll(filepath.Join(d.dir, "/dev/disk/by-partlabel"), 0755))

	mylog.Check(os.MkdirAll(filepath.Join(d.dir, "/dev/mapper"), 0755))

	mylog.Check(os.WriteFile(filepath.Join(d.dir, "/dev/fakedevice"), []byte(""), 0644))

}

func (d *deviceSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (d *deviceSuite) TestDeviceFindByStructureName(c *C) {
	names := []struct {
		escaped   string
		structure string
	}{
		{"foo", "foo"},
		{"123", "123"},
		{"foo\\x20bar", "foo bar"},
		{"foo#bar", "foo#bar"},
		{"Новый_том", "Новый_том"},
		{`pinkié\x20pie`, `pinkié pie`},
	}
	for _, name := range names {
		mylog.Check(os.Symlink(filepath.Join(d.dir, "/dev/fakedevice"), filepath.Join(d.dir, "/dev/disk/by-partlabel", name.escaped)))

	}

	for _, tc := range names {
		c.Logf("trying: %q", tc)
		found := mylog.Check2(gadget.FindDeviceForStructure(&gadget.VolumeStructure{Name: tc.structure, EnclosingVolume: &gadget.Volume{}}))
		c.Check(err, IsNil)
		c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice"))
	}
}

func (d *deviceSuite) TestDeviceFindRelativeSymlink(c *C) {
	mylog.Check(os.Symlink("../../fakedevice", filepath.Join(d.dir, "/dev/disk/by-partlabel/relative")))


	found := mylog.Check2(gadget.FindDeviceForStructure(&gadget.VolumeStructure{Name: "relative", EnclosingVolume: &gadget.Volume{}}))
	c.Check(err, IsNil)
	c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice"))
}

func (d *deviceSuite) TestDeviceFindByFilesystemLabel(c *C) {
	names := []struct {
		escaped   string
		structure string
	}{
		{"foo", "foo"},
		{"123", "123"},
		{`foo\x20bar`, "foo bar"},
		{"foo#bar", "foo#bar"},
		{"Новый_том", "Новый_том"},
		{`pinkié\x20pie`, `pinkié pie`},
	}
	for _, name := range names {
		mylog.Check(os.Symlink(filepath.Join(d.dir, "/dev/fakedevice"), filepath.Join(d.dir, "/dev/disk/by-label", name.escaped)))

	}

	for _, tc := range names {
		c.Logf("trying: %q", tc)
		found := mylog.Check2(gadget.FindDeviceForStructure(&gadget.VolumeStructure{
			Filesystem: "ext4",
			Label:      tc.structure,
		}))
		c.Check(err, IsNil)
		c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice"))
	}
}

func (d *deviceSuite) TestDeviceFindChecksPartlabelAndFilesystemLabelHappy(c *C) {
	fakedevice := filepath.Join(d.dir, "/dev/fakedevice")
	mylog.Check(os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/foo")))

	mylog.Check(os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-partlabel/bar")))


	found := mylog.Check2(gadget.FindDeviceForStructure(&gadget.VolumeStructure{
		Name:            "bar",
		Label:           "foo",
		EnclosingVolume: &gadget.Volume{},
	}))
	c.Check(err, IsNil)
	c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice"))
}

func (d *deviceSuite) TestDeviceFindFilesystemLabelToNameFallback(c *C) {
	fakedevice := filepath.Join(d.dir, "/dev/fakedevice")
	mylog.
		// only the by-filesystem-label symlink
		Check(os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/foo")))


	found := mylog.Check2(gadget.FindDeviceForStructure(&gadget.VolumeStructure{
		Name:       "foo",
		Filesystem: "ext4",
	}))
	c.Check(err, IsNil)
	c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice"))
}

func (d *deviceSuite) TestDeviceFindChecksPartlabelAndFilesystemLabelMismatch(c *C) {
	fakedevice := filepath.Join(d.dir, "/dev/fakedevice")
	mylog.Check(os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/foo")))


	// partlabel of the structure points to a different device
	fakedeviceOther := filepath.Join(d.dir, "/dev/fakedevice-other")
	mylog.Check(os.WriteFile(fakedeviceOther, []byte(""), 0644))

	mylog.Check(os.Symlink(fakedeviceOther, filepath.Join(d.dir, "/dev/disk/by-partlabel/bar")))


	found := mylog.Check2(gadget.FindDeviceForStructure(&gadget.VolumeStructure{
		Name:       "bar",
		Label:      "foo",
		Filesystem: "ext4",
	}))
	c.Check(err, ErrorMatches, `conflicting device match, ".*/by-label/foo" points to ".*/fakedevice", previous match ".*/by-partlabel/bar" points to ".*/fakedevice-other"`)
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestDeviceFindNotFound(c *C) {
	found := mylog.Check2(gadget.FindDeviceForStructure(&gadget.VolumeStructure{
		Name:            "bar",
		Label:           "foo",
		EnclosingVolume: &gadget.Volume{},
	}))
	c.Check(err, ErrorMatches, `device not found`)
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestDeviceFindNotFoundEmpty(c *C) {
	// neither name nor filesystem label set
	found := mylog.Check2(gadget.FindDeviceForStructure(&gadget.VolumeStructure{
		Name: "",
		// structure has no filesystem, fs label check is
		// ineffective
		Label:           "",
		EnclosingVolume: &gadget.Volume{},
	}))
	c.Check(err, ErrorMatches, `device not found`)
	c.Check(found, Equals, "")

	// try with proper filesystem now
	found = mylog.Check2(gadget.FindDeviceForStructure(&gadget.VolumeStructure{
		Name:            "",
		Label:           "",
		Filesystem:      "ext4",
		EnclosingVolume: &gadget.Volume{},
	}))
	c.Check(err, ErrorMatches, `device not found`)
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestDeviceFindNotFoundSymlinkPointsNowhere(c *C) {
	fakedevice := filepath.Join(d.dir, "/dev/fakedevice-not-found")
	mylog.Check(os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/foo")))


	found := mylog.Check2(gadget.FindDeviceForStructure(&gadget.VolumeStructure{
		Label: "foo", EnclosingVolume: &gadget.Volume{},
	}))
	c.Check(err, ErrorMatches, `device not found`)
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestDeviceFindNotFoundNotASymlink(c *C) {
	mylog.Check(os.WriteFile(filepath.Join(d.dir, "/dev/disk/by-label/foo"), nil, 0644))


	found := mylog.Check2(gadget.FindDeviceForStructure(&gadget.VolumeStructure{
		Filesystem: "ext4",
		Label:      "foo",
	}))
	c.Check(err, ErrorMatches, `candidate .*/dev/disk/by-label/foo is not a symlink`)
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestDeviceFindBadEvalSymlinks(c *C) {
	fakedevice := filepath.Join(d.dir, "/dev/fakedevice")
	fooSymlink := filepath.Join(d.dir, "/dev/disk/by-label/foo")
	mylog.Check(os.Symlink(fakedevice, fooSymlink))


	restore := gadget.MockEvalSymlinks(func(p string) (string, error) {
		c.Assert(p, Equals, fooSymlink)
		return "", errors.New("failed")
	})
	defer restore()

	found := mylog.Check2(gadget.FindDeviceForStructure(&gadget.VolumeStructure{
		Filesystem: "vfat",
		Label:      "foo",
	}))
	c.Check(err, ErrorMatches, `cannot read device link: failed`)
	c.Check(found, Equals, "")
}
