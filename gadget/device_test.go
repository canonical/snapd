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
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/quantity"
	"github.com/snapcore/snapd/osutil"
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
	err = os.MkdirAll(filepath.Join(d.dir, "/dev/mapper"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(d.dir, "/dev/fakedevice"), []byte(""), 0644)
	c.Assert(err, IsNil)
}

func (d *deviceSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (d *deviceSuite) setupMockSysfs(c *C) {
	// setup everything for 'writable'
	err := ioutil.WriteFile(filepath.Join(d.dir, "/dev/fakedevice0p1"), []byte(""), 0644)
	c.Assert(err, IsNil)
	err = os.Symlink("../../fakedevice0p1", filepath.Join(d.dir, "/dev/disk/by-label/writable"))
	c.Assert(err, IsNil)
	// make parent device
	err = ioutil.WriteFile(filepath.Join(d.dir, "/dev/fakedevice0"), []byte(""), 0644)
	c.Assert(err, IsNil)
	// and fake /sys/block structure
	err = os.MkdirAll(filepath.Join(d.dir, "/sys/block/fakedevice0/fakedevice0p1"), 0755)
	c.Assert(err, IsNil)
}

func (d *deviceSuite) setupMockSysfsForDevMapper(c *C) {
	// setup a mock /dev/mapper environment (incomplete we have no "happy"
	// test; use a complex setup that mimics LVM in LUKS:
	// /dev/mapper/data_crypt (symlink)
	//   ⤷ /dev/dm-1 (LVM)
	//      ⤷ /dev/dm-0 (LUKS)
	//         ⤷ /dev/fakedevice0 (actual device)
	err := ioutil.WriteFile(filepath.Join(d.dir, "/dev/dm-0"), nil, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(d.dir, "/dev/dm-1"), nil, 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(d.dir, "/dev/fakedevice0"), []byte(""), 0644)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(d.dir, "/dev/fakedevice"), []byte(""), 0644)
	c.Assert(err, IsNil)
	// symlinks added by dm/udev are relative
	err = os.Symlink("../dm-1", filepath.Join(d.dir, "/dev/mapper/data_crypt"))
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(d.dir, "/sys/block/dm-1/slaves/"), 0755)
	c.Assert(err, IsNil)
	// sys symlinks are relative too
	err = os.Symlink("../../dm-0", filepath.Join(d.dir, "/sys/block/dm-1/slaves/dm-0"))
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(d.dir, "/sys/block/dm-0/slaves/"), 0755)
	c.Assert(err, IsNil)
	// real symlink would point to ../../../../<bus, eg. pci>/<addr>/block/fakedevice/fakedevice0
	err = os.Symlink("../../../../fakedevice/fakedevice0", filepath.Join(d.dir, "/sys/block/dm-0/slaves/fakedevice0"))
	c.Assert(err, IsNil)
	err = os.MkdirAll(filepath.Join(d.dir, "/sys/block/fakedevice/fakedevice0"), 0755)
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
		{"foo#bar", "foo#bar"},
		{"Новый_том", "Новый_том"},
		{`pinkié\x20pie`, `pinkié pie`},
	}
	for _, name := range names {
		err := os.Symlink(filepath.Join(d.dir, "/dev/fakedevice"), filepath.Join(d.dir, "/dev/disk/by-partlabel", name.escaped))
		c.Assert(err, IsNil)
	}

	for _, tc := range names {
		c.Logf("trying: %q", tc)
		found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
			VolumeStructure: &gadget.VolumeStructure{Name: tc.structure},
		})
		c.Check(err, IsNil)
		c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice"))
	}
}

func (d *deviceSuite) TestDeviceFindRelativeSymlink(c *C) {
	err := os.Symlink("../../fakedevice", filepath.Join(d.dir, "/dev/disk/by-partlabel/relative"))
	c.Assert(err, IsNil)

	found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{Name: "relative"},
	})
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
		err := os.Symlink(filepath.Join(d.dir, "/dev/fakedevice"), filepath.Join(d.dir, "/dev/disk/by-label", name.escaped))
		c.Assert(err, IsNil)
	}

	for _, tc := range names {
		c.Logf("trying: %q", tc)
		found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
			VolumeStructure: &gadget.VolumeStructure{
				Filesystem: "ext4",
				Label:      tc.structure,
			},
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

	found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:  "bar",
			Label: "foo",
		},
	})
	c.Check(err, IsNil)
	c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice"))
}

func (d *deviceSuite) TestDeviceFindFilesystemLabelToNameFallback(c *C) {
	fakedevice := filepath.Join(d.dir, "/dev/fakedevice")
	// only the by-filesystem-label symlink
	err := os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/foo"))
	c.Assert(err, IsNil)

	found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "foo",
			Filesystem: "ext4",
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

	found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "bar",
			Label:      "foo",
			Filesystem: "ext4",
		},
	})
	c.Check(err, ErrorMatches, `conflicting device match, ".*/by-label/foo" points to ".*/fakedevice", previous match ".*/by-partlabel/bar" points to ".*/fakedevice-other"`)
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestDeviceFindNotFound(c *C) {
	found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
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
	found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name: "",
			// structure has no filesystem, fs label check is
			// ineffective
			Label: "",
		},
	})
	c.Check(err, ErrorMatches, `device not found`)
	c.Check(found, Equals, "")

	// try with proper filesystem now
	found, err = gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "",
			Label:      "",
			Filesystem: "ext4",
		},
	})
	c.Check(err, ErrorMatches, `device not found`)
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestDeviceFindNotFoundSymlinkPointsNowhere(c *C) {
	fakedevice := filepath.Join(d.dir, "/dev/fakedevice-not-found")
	err := os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/foo"))
	c.Assert(err, IsNil)

	found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Label: "foo",
		},
	})
	c.Check(err, ErrorMatches, `device not found`)
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestDeviceFindNotFoundNotASymlink(c *C) {
	err := ioutil.WriteFile(filepath.Join(d.dir, "/dev/disk/by-label/foo"), nil, 0644)
	c.Assert(err, IsNil)

	found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "ext4",
			Label:      "foo",
		},
	})
	c.Check(err, ErrorMatches, `candidate .*/dev/disk/by-label/foo is not a symlink`)
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestDeviceFindBadEvalSymlinks(c *C) {
	fakedevice := filepath.Join(d.dir, "/dev/fakedevice")
	fooSymlink := filepath.Join(d.dir, "/dev/disk/by-label/foo")
	err := os.Symlink(fakedevice, fooSymlink)
	c.Assert(err, IsNil)

	restore := gadget.MockEvalSymlinks(func(p string) (string, error) {
		c.Assert(p, Equals, fooSymlink)
		return "", errors.New("failed")
	})
	defer restore()

	found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Filesystem: "vfat",
			Label:      "foo",
		},
	})
	c.Check(err, ErrorMatches, `cannot read device link: failed`)
	c.Check(found, Equals, "")
}

var writableMountInfoFmt = `26 27 8:3 / /writable rw,relatime shared:7 - ext4 %s/dev/fakedevice0p1 rw,data=ordered`

func (d *deviceSuite) TestDeviceFindFallbackNotFoundNoWritable(c *C) {
	badMountInfoFmt := `26 27 8:3 / /not-writable rw,relatime shared:7 - ext4 %s/dev/fakedevice0p1 rw,data=ordered`
	restore := osutil.MockMountInfo(fmt.Sprintf(badMountInfoFmt, d.dir))
	defer restore()

	found, offs, err := gadget.FindDeviceForStructureWithFallback(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Type: "bare",
		},
		StartOffset: 123,
	})
	c.Check(err, ErrorMatches, `device not found`)
	c.Check(found, Equals, "")
	c.Check(offs, Equals, quantity.Offset(0))
}

func (d *deviceSuite) TestDeviceFindFallbackBadWritable(c *C) {
	restore := osutil.MockMountInfo(fmt.Sprintf(writableMountInfoFmt, d.dir))
	defer restore()

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Type: "bare",
		},
		StartOffset: 123,
	}

	found, offs, err := gadget.FindDeviceForStructureWithFallback(ps)
	c.Check(err, ErrorMatches, `lstat .*/dev/fakedevice0p1: no such file or directory`)
	c.Check(found, Equals, "")
	c.Check(offs, Equals, quantity.Offset(0))

	c.Assert(ioutil.WriteFile(filepath.Join(d.dir, "dev/fakedevice0p1"), nil, 064), IsNil)

	found, offs, err = gadget.FindDeviceForStructureWithFallback(ps)
	c.Check(err, ErrorMatches, `unexpected number of matches \(0\) for /sys/block/\*/fakedevice0p1`)
	c.Check(found, Equals, "")
	c.Check(offs, Equals, quantity.Offset(0))

	err = os.MkdirAll(filepath.Join(d.dir, "/sys/block/fakedevice0/fakedevice0p1"), 0755)
	c.Assert(err, IsNil)

	found, offs, err = gadget.FindDeviceForStructureWithFallback(ps)
	c.Check(err, ErrorMatches, `device .*/dev/fakedevice0 does not exist`)
	c.Check(found, Equals, "")
	c.Check(offs, Equals, quantity.Offset(0))
}

func (d *deviceSuite) TestDeviceFindFallbackHappyWritable(c *C) {
	d.setupMockSysfs(c)
	restore := osutil.MockMountInfo(fmt.Sprintf(writableMountInfoFmt, d.dir))
	defer restore()

	psJustBare := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Type: "bare",
		},
		StartOffset: 123,
	}
	psBareWithName := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Type: "bare",
			Name: "foo",
		},
		StartOffset: 123,
	}
	psMBR := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Type: "mbr",
			Role: "mbr",
			Name: "mbr",
		},
		StartOffset: 0,
	}
	psNoName := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{},
		StartOffset:     123,
	}

	for _, ps := range []*gadget.LaidOutStructure{psJustBare, psBareWithName, psNoName, psMBR} {
		found, offs, err := gadget.FindDeviceForStructureWithFallback(ps)
		c.Check(err, IsNil)
		c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice0"))
		if ps.Type != "mbr" {
			c.Check(offs, Equals, quantity.Offset(123))
		} else {
			c.Check(offs, Equals, quantity.Offset(0))
		}
	}
}

func (d *deviceSuite) TestDeviceFindFallbackNotForNamedWritable(c *C) {
	d.setupMockSysfs(c)
	restore := osutil.MockMountInfo(fmt.Sprintf(writableMountInfoFmt, d.dir))
	defer restore()

	// should not hit the fallback path
	psNamed := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name: "foo",
		},
		StartOffset: 123,
	}
	found, offs, err := gadget.FindDeviceForStructureWithFallback(psNamed)
	c.Check(err, Equals, gadget.ErrDeviceNotFound)
	c.Check(found, Equals, "")
	c.Check(offs, Equals, quantity.Offset(0))
}

func (d *deviceSuite) TestDeviceFindFallbackNotForFilesystem(c *C) {
	d.setupMockSysfs(c)
	restore := osutil.MockMountInfo(writableMountInfoFmt)
	defer restore()

	psFs := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Label:      "foo",
			Filesystem: "ext4",
		},
		StartOffset: 123,
	}
	found, offs, err := gadget.FindDeviceForStructureWithFallback(psFs)
	c.Check(err, ErrorMatches, "internal error: cannot use with filesystem structures")
	c.Check(found, Equals, "")
	c.Check(offs, Equals, quantity.Offset(0))
}

func (d *deviceSuite) TestDeviceFindFallbackBadMountInfo(c *C) {
	d.setupMockSysfs(c)
	restore := osutil.MockMountInfo("garbage")
	defer restore()
	psFs := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name: "foo",
			Type: "bare",
		},
		StartOffset: 123,
	}
	found, offs, err := gadget.FindDeviceForStructureWithFallback(psFs)
	c.Check(err, ErrorMatches, "cannot read mount info: .*")
	c.Check(found, Equals, "")
	c.Check(offs, Equals, quantity.Offset(0))
}

func (d *deviceSuite) TestDeviceFindFallbackPassThrough(c *C) {
	err := ioutil.WriteFile(filepath.Join(d.dir, "/dev/disk/by-partlabel/foo"), nil, 0644)
	c.Assert(err, IsNil)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name: "foo",
		},
	}
	found, offs, err := gadget.FindDeviceForStructureWithFallback(ps)
	c.Check(err, ErrorMatches, `candidate .*/dev/disk/by-partlabel/foo is not a symlink`)
	c.Check(found, Equals, "")
	c.Check(offs, Equals, quantity.Offset(0))

	// create a proper symlink
	err = os.Remove(filepath.Join(d.dir, "/dev/disk/by-partlabel/foo"))
	c.Assert(err, IsNil)
	err = os.Symlink("../../fakedevice", filepath.Join(d.dir, "/dev/disk/by-partlabel/foo"))
	c.Assert(err, IsNil)

	// this should be happy again
	found, offs, err = gadget.FindDeviceForStructureWithFallback(ps)
	c.Assert(err, IsNil)
	c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice"))
	c.Check(offs, Equals, quantity.Offset(0))
}

func (d *deviceSuite) TestDeviceFindMountPointErrorsWithBare(c *C) {
	p, err := gadget.FindMountPointForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			// no filesystem
			Filesystem: "",
		},
	})
	c.Assert(err, ErrorMatches, "no filesystem defined")
	c.Check(p, Equals, "")

	p, err = gadget.FindMountPointForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			// also counts as bare structure
			Filesystem: "none",
		},
	})
	c.Assert(err, ErrorMatches, "no filesystem defined")
	c.Check(p, Equals, "")
}

func (d *deviceSuite) TestDeviceFindMountPointErrorsFromDevice(c *C) {
	p, err := gadget.FindMountPointForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Label:      "bar",
			Filesystem: "ext4",
		},
	})
	c.Assert(err, ErrorMatches, "device not found")
	c.Check(p, Equals, "")

	p, err = gadget.FindMountPointForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "bar",
			Filesystem: "ext4",
		},
	})
	c.Assert(err, ErrorMatches, "device not found")
	c.Check(p, Equals, "")
}

func (d *deviceSuite) TestDeviceFindMountPointErrorBadMountinfo(c *C) {
	// taken from core18 system

	fakedevice := filepath.Join(d.dir, "/dev/sda2")
	err := ioutil.WriteFile(fakedevice, []byte(""), 0644)
	c.Assert(err, IsNil)
	err = os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/system-boot"))
	c.Assert(err, IsNil)
	restore := osutil.MockMountInfo("garbage")
	defer restore()

	found, err := gadget.FindMountPointForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "EFI System",
			Label:      "system-boot",
			Filesystem: "vfat",
		},
	})
	c.Check(err, ErrorMatches, "cannot read mount info: .*")
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestDeviceFindMountPointByLabeHappySimple(c *C) {
	// taken from core18 system

	fakedevice := filepath.Join(d.dir, "/dev/sda2")
	err := ioutil.WriteFile(fakedevice, []byte(""), 0644)
	c.Assert(err, IsNil)
	err = os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/system-boot"))
	c.Assert(err, IsNil)
	err = os.Symlink(fakedevice, filepath.Join(d.dir, `/dev/disk/by-partlabel/EFI\x20System`))
	c.Assert(err, IsNil)

	mountInfo := `
170 27 8:2 / /boot/efi rw,relatime shared:58 - vfat ${rootDir}/dev/sda2 rw,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro
172 27 8:2 /EFI/ubuntu /boot/grub rw,relatime shared:58 - vfat ${rootDir}/dev/sda2 rw,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro
`
	restore := osutil.MockMountInfo(strings.Replace(mountInfo[1:], "${rootDir}", d.dir, -1))
	defer restore()

	found, err := gadget.FindMountPointForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "EFI System",
			Label:      "system-boot",
			Filesystem: "vfat",
		},
	})
	c.Check(err, IsNil)
	c.Check(found, Equals, "/boot/efi")
}

func (d *deviceSuite) TestDeviceFindMountPointByLabeHappyReversed(c *C) {
	// taken from core18 system

	fakedevice := filepath.Join(d.dir, "/dev/sda2")
	err := ioutil.WriteFile(fakedevice, []byte(""), 0644)
	c.Assert(err, IsNil)
	// single property match
	err = os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/system-boot"))
	c.Assert(err, IsNil)

	// reverse the order of lines
	mountInfoReversed := `
172 27 8:2 /EFI/ubuntu /boot/grub rw,relatime shared:58 - vfat ${rootDir}/dev/sda2 rw,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro
170 27 8:2 / /boot/efi rw,relatime shared:58 - vfat ${rootDir}/dev/sda2 rw,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro
`

	restore := osutil.MockMountInfo(strings.Replace(mountInfoReversed[1:], "${rootDir}", d.dir, -1))
	defer restore()

	found, err := gadget.FindMountPointForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "EFI System",
			Label:      "system-boot",
			Filesystem: "vfat",
		},
	})
	c.Check(err, IsNil)
	c.Check(found, Equals, "/boot/efi")
}

func (d *deviceSuite) TestDeviceFindMountPointPicksFirstMatch(c *C) {
	// taken from core18 system

	fakedevice := filepath.Join(d.dir, "/dev/sda2")
	err := ioutil.WriteFile(fakedevice, []byte(""), 0644)
	c.Assert(err, IsNil)
	// single property match
	err = os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/system-boot"))
	c.Assert(err, IsNil)

	mountInfo := `
852 134 8:2 / /mnt/foo rw,relatime shared:58 - vfat ${rootDir}/dev/sda2 rw,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro
172 27 8:2 /EFI/ubuntu /boot/grub rw,relatime shared:58 - vfat ${rootDir}/dev/sda2 rw,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro
170 27 8:2 / /boot/efi rw,relatime shared:58 - vfat ${rootDir}/dev/sda2 rw,fmask=0022,dmask=0022,codepage=437,iocharset=iso8859-1,shortname=mixed,errors=remount-ro
`

	restore := osutil.MockMountInfo(strings.Replace(mountInfo[1:], "${rootDir}", d.dir, -1))
	defer restore()

	found, err := gadget.FindMountPointForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "EFI System",
			Label:      "system-boot",
			Filesystem: "vfat",
		},
	})
	c.Check(err, IsNil)
	c.Check(found, Equals, "/mnt/foo")
}

func (d *deviceSuite) TestDeviceFindMountPointByPartlabel(c *C) {
	fakedevice := filepath.Join(d.dir, "/dev/fakedevice")
	err := ioutil.WriteFile(fakedevice, []byte(""), 0644)
	c.Assert(err, IsNil)
	err = os.Symlink(fakedevice, filepath.Join(d.dir, `/dev/disk/by-partlabel/pinkié\x20pie`))
	c.Assert(err, IsNil)

	mountInfo := `
170 27 8:2 / /mount-point rw,relatime shared:58 - ext4 ${rootDir}/dev/fakedevice rw
`

	restore := osutil.MockMountInfo(strings.Replace(mountInfo[1:], "${rootDir}", d.dir, -1))
	defer restore()

	found, err := gadget.FindMountPointForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:       "pinkié pie",
			Filesystem: "ext4",
		},
	})
	c.Check(err, IsNil)
	c.Check(found, Equals, "/mount-point")
}

func (d *deviceSuite) TestDeviceFindMountPointChecksFilesystem(c *C) {
	fakedevice := filepath.Join(d.dir, "/dev/fakedevice")
	err := ioutil.WriteFile(fakedevice, []byte(""), 0644)
	c.Assert(err, IsNil)
	err = os.Symlink(fakedevice, filepath.Join(d.dir, `/dev/disk/by-partlabel/label`))
	c.Assert(err, IsNil)

	mountInfo := `
170 27 8:2 / /mount-point rw,relatime shared:58 - vfat ${rootDir}/dev/fakedevice rw
`

	restore := osutil.MockMountInfo(strings.Replace(mountInfo[1:], "${rootDir}", d.dir, -1))
	defer restore()

	found, err := gadget.FindMountPointForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name: "label",
			// different fs than mount entry
			Filesystem: "ext4",
		},
	})
	c.Check(err, ErrorMatches, "mount point not found")
	c.Check(found, Equals, "")
}

func (d *deviceSuite) TestParentDiskFromMountSource(c *C) {
	d.setupMockSysfs(c)

	disk, err := gadget.ParentDiskFromMountSource(filepath.Join(dirs.GlobalRootDir, "/dev/fakedevice0p1"))
	c.Assert(err, IsNil)
	c.Check(disk, Matches, ".*/dev/fakedevice0")
}

func (d *deviceSuite) TestParentDiskFromMountSourceBadSymlinkErr(c *C) {
	d.setupMockSysfs(c)

	err := os.Symlink("../bad-target", filepath.Join(d.dir, "/dev/mapper/bad-target-symlink"))
	c.Assert(err, IsNil)

	_, err = gadget.ParentDiskFromMountSource(filepath.Join(dirs.GlobalRootDir, "/dev/mapper/bad-target-symlink"))
	c.Assert(err, ErrorMatches, `cannot resolve mount source symlink .*/dev/mapper/bad-target-symlink: lstat .*/dev/bad-target: no such file or directory`)
}

func (d *deviceSuite) TestParentDiskFromMountSourceDeviceMapperHappy(c *C) {
	d.setupMockSysfsForDevMapper(c)

	disk, err := gadget.ParentDiskFromMountSource(filepath.Join(dirs.GlobalRootDir, "/dev/mapper/data_crypt"))

	c.Assert(err, IsNil)
	c.Check(disk, Matches, ".*/dev/fakedevice")
}

func (d *deviceSuite) TestParentDiskFromMountSourceDeviceMapperErrGlob(c *C) {
	d.setupMockSysfsForDevMapper(c)

	// break the intermediate slaves directory
	c.Assert(os.RemoveAll(filepath.Join(d.dir, "/sys/block/dm-0/slaves/fakedevice0")), IsNil)

	_, err := gadget.ParentDiskFromMountSource(filepath.Join(dirs.GlobalRootDir, "/dev/mapper/data_crypt"))
	c.Assert(err, ErrorMatches, "cannot resolve device mapper device dm-1: unexpected number of dm device dm-0 slaves: 0")

	c.Assert(os.Chmod(filepath.Join(d.dir, "/sys/block/dm-0"), 0000), IsNil)
	defer os.Chmod(filepath.Join(d.dir, "/sys/block/dm-0"), 0755)

	_, err = gadget.ParentDiskFromMountSource(filepath.Join(dirs.GlobalRootDir, "/dev/mapper/data_crypt"))
	c.Assert(err, ErrorMatches, "cannot resolve device mapper device dm-1: unexpected number of dm device dm-0 slaves: 0")
}

func (d *deviceSuite) TestParentDiskFromMountSourceDeviceMapperErrTargetDevice(c *C) {
	d.setupMockSysfsForDevMapper(c)

	c.Assert(os.RemoveAll(filepath.Join(d.dir, "/sys/block/fakedevice")), IsNil)

	_, err := gadget.ParentDiskFromMountSource(filepath.Join(dirs.GlobalRootDir, "/dev/mapper/data_crypt"))
	c.Assert(err, ErrorMatches, `unexpected number of matches \(0\) for /sys/block/\*/fakedevice0`)
}

func (d *deviceSuite) TestParentDiskFromMountSourceDeviceMapperLevels(c *C) {
	err := os.Symlink("../dm-6", filepath.Join(d.dir, "/dev/mapper/data_crypt"))
	c.Assert(err, IsNil)
	for i := 6; i > 0; i-- {
		err := ioutil.WriteFile(filepath.Join(d.dir, fmt.Sprintf("/dev/dm-%v", i)), nil, 0644)
		c.Assert(err, IsNil)
		err = os.MkdirAll(filepath.Join(d.dir, fmt.Sprintf("/sys/block/dm-%v/slaves/", i)), 0755)
		c.Assert(err, IsNil)
		// sys symlinks are relative too
		err = os.Symlink(fmt.Sprintf("../../dm-%v", i-1), filepath.Join(d.dir, fmt.Sprintf("/sys/block/dm-%v/slaves/dm-%v", i, i-1)))
		c.Assert(err, IsNil)
	}

	_, err = gadget.ParentDiskFromMountSource(filepath.Join(dirs.GlobalRootDir, "/dev/mapper/data_crypt"))
	c.Assert(err, ErrorMatches, `cannot resolve device mapper device dm-6: too many levels`)
}
