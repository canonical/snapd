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
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/gadget"
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
	err = ioutil.WriteFile(filepath.Join(d.dir, "/dev/fakedevice"), []byte(""), 0644)
	c.Assert(err, IsNil)
}

func (d *deviceSuite) TearDownTest(c *C) {
	dirs.SetRootDir("/")
}

func (d *deviceSuite) setUpWritableFallback(c *C, mountInfo string) {
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

	mockProcSelfFilesystem(c, d.dir, mountInfo)
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

	found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
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

	found, err := gadget.FindDeviceForStructure(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Name:  "bar",
			Label: "foo",
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
			Name:  "",
			Label: "",
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
			Label: "foo",
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
			Label: "foo",
		},
	})
	c.Check(err, ErrorMatches, `cannot read device link: failed`)
	c.Check(found, Equals, "")
}

var writableMountInfo = `26 27 8:3 / /writable rw,relatime shared:7 - ext4 /dev/fakedevice0p1 rw,data=ordered`

func (d *deviceSuite) TestDeviceFindFallbackNotFoundNoWritable(c *C) {
	badMountInfo := `26 27 8:3 / /not-writable rw,relatime shared:7 - ext4 /dev/fakedevice0p1 rw,data=ordered`

	mockProcSelfFilesystem(c, d.dir, badMountInfo)

	found, offs, err := gadget.FindDeviceForStructureWithFallback(&gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Type: "bare",
		},
		StartOffset: 123,
	})
	c.Check(err, ErrorMatches, `device not found`)
	c.Check(found, Equals, "")
	c.Check(offs, Equals, gadget.Size(0))
}

func (d *deviceSuite) TestDeviceFindFallbackBadWritable(c *C) {
	mockProcSelfFilesystem(c, d.dir, writableMountInfo)

	ps := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{
			Type: "bare",
		},
		StartOffset: 123,
	}

	found, offs, err := gadget.FindDeviceForStructureWithFallback(ps)
	c.Check(err, ErrorMatches, `unexpected number of matches \(0\) for /sys/block/\*/fakedevice0p1`)
	c.Check(found, Equals, "")
	c.Check(offs, Equals, gadget.Size(0))

	err = os.MkdirAll(filepath.Join(d.dir, "/sys/block/fakedevice0/fakedevice0p1"), 0755)
	c.Assert(err, IsNil)

	found, offs, err = gadget.FindDeviceForStructureWithFallback(ps)
	c.Check(err, ErrorMatches, `device .*/dev/fakedevice0 does not exist`)
	c.Check(found, Equals, "")
	c.Check(offs, Equals, gadget.Size(0))
}

func (d *deviceSuite) TestDeviceFindFallbackHappyWritable(c *C) {
	d.setUpWritableFallback(c, writableMountInfo)

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
	psNoName := &gadget.LaidOutStructure{
		VolumeStructure: &gadget.VolumeStructure{},
		StartOffset:     123,
	}

	for _, ps := range []*gadget.LaidOutStructure{psJustBare, psBareWithName, psNoName} {
		found, offs, err := gadget.FindDeviceForStructureWithFallback(ps)
		c.Check(err, IsNil)
		c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice0"))
		c.Check(offs, Equals, gadget.Size(123))
	}
}

func (d *deviceSuite) TestDeviceFindFallbackNotForNamedWritable(c *C) {
	d.setUpWritableFallback(c, writableMountInfo)

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
	c.Check(offs, Equals, gadget.Size(0))
}

func (d *deviceSuite) TestDeviceFindFallbackNotForFilesystem(c *C) {
	d.setUpWritableFallback(c, writableMountInfo)

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
	c.Check(offs, Equals, gadget.Size(0))
}

func (d *deviceSuite) TestDeviceFindFallbackBadMountInfo(c *C) {
	d.setUpWritableFallback(c, "garbage")
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
	c.Check(offs, Equals, gadget.Size(0))
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
	c.Check(offs, Equals, gadget.Size(0))

	// create a proper symlink
	err = os.Remove(filepath.Join(d.dir, "/dev/disk/by-partlabel/foo"))
	c.Assert(err, IsNil)
	err = os.Symlink("../../fakedevice", filepath.Join(d.dir, "/dev/disk/by-partlabel/foo"))
	c.Assert(err, IsNil)

	// this should be happy again
	found, offs, err = gadget.FindDeviceForStructureWithFallback(ps)
	c.Assert(err, IsNil)
	c.Check(found, Equals, filepath.Join(d.dir, "/dev/fakedevice"))
	c.Check(offs, Equals, gadget.Size(0))
}

func (d *deviceSuite) TestDeviceEncodeLabel(c *C) {
	// Test output obtained with the following program:
	//
	// #include <string.h>
	// #include <stdio.h>
	// #include <blkid/blkid.h>
	// int main(int argc, char *argv[]) {
	//   char out[2048] = {0};
	//   if (blkid_encode_string(argv[1], out, sizeof(out)) != 0) {
	//     fprintf(stderr, "failed to encode string\n");
	//     return 1;
	//   }
	//   fprintf(stdout, out);
	//   return 0;
	// }
	for i, tc := range []struct {
		what string
		exp  string
	}{
		{"foo", "foo"},
		{"foo bar", `foo\x20bar`},
		{"foo/bar", `foo\x2fbar`},
		{"foo:#.@bar", `foo:#.@bar`},
		{"foo..bar", `foo..bar`},
		{"foo/../bar", `foo\x2f..\x2fbar`},
		{"foo\\bar", `foo\x5cbar`},
		{"Новый_том", "Новый_том"},
		{"befs_test", "befs_test"},
		{"P01_S16A", "P01_S16A"},
		{"pinkié pie", `pinkié\x20pie`},
		{"(EFI Boot)", `\x28EFI\x20Boot\x29`},
		{"[System Boot]", `\x5bSystem\x20Boot\x5d`},
	} {
		c.Logf("tc: %v %q", i, tc)
		res := gadget.EncodeLabel(tc.what)
		c.Check(res, Equals, tc.exp)
	}
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

func mockProcSelfFilesystem(c *C, root, content string) {
	psmi := filepath.Join(root, osutil.ProcSelfMountInfo)
	err := os.MkdirAll(filepath.Dir(psmi), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(psmi, []byte(content), 0644)
	c.Assert(err, IsNil)
}

func (d *deviceSuite) TestDeviceFindMountPointErrorBadMountinfo(c *C) {
	// taken from core18 system

	fakedevice := filepath.Join(d.dir, "/dev/sda2")
	err := ioutil.WriteFile(fakedevice, []byte(""), 0644)
	c.Assert(err, IsNil)
	err = os.Symlink(fakedevice, filepath.Join(d.dir, "/dev/disk/by-label/system-boot"))
	c.Assert(err, IsNil)

	mockProcSelfFilesystem(c, d.dir, "garbage")

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
	mockProcSelfFilesystem(c, d.dir, strings.Replace(mountInfo[1:], "${rootDir}", d.dir, -1))

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

	mockProcSelfFilesystem(c, d.dir, strings.Replace(mountInfoReversed[1:], "${rootDir}", d.dir, -1))

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

	mockProcSelfFilesystem(c, d.dir, strings.Replace(mountInfo[1:], "${rootDir}", d.dir, -1))

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

	mockProcSelfFilesystem(c, d.dir, strings.Replace(mountInfo[1:], "${rootDir}", d.dir, -1))

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

	mockProcSelfFilesystem(c, d.dir, strings.Replace(mountInfo[1:], "${rootDir}", d.dir, -1))

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
