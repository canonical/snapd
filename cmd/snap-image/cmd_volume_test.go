// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package main_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"

	. "gopkg.in/check.v1"

	snap_image "github.com/snapcore/snapd/cmd/snap-image"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/testutil"
)

type volumeSuite struct {
	CmdBaseTest

	dir      string
	sfdisk   *testutil.MockCmd
	fakeroot *testutil.MockCmd
	mkfsVfat *testutil.MockCmd
	mcopy    *testutil.MockCmd
}

var _ = Suite(&volumeSuite{})

func (s *volumeSuite) SetUpTest(c *C) {
	s.CmdBaseTest.SetUpTest(c)

	s.dir = c.MkDir()

	s.fakeroot = testutil.MockCommand(c, "fakeroot", "")
	s.AddCleanup(s.fakeroot.Restore)

	// is not really called directly, so make sure it fails if there was a
	// call
	cmdMkfsExt4 := testutil.MockCommand(c, "mkfs.ext4", "echo 'unexpected call'; exit 1")
	s.AddCleanup(cmdMkfsExt4.Restore)

	s.mkfsVfat = testutil.MockCommand(c, "mkfs.vfat", "")
	s.AddCleanup(s.mkfsVfat.Restore)

	s.mcopy = testutil.MockCommand(c, "mcopy", "")
	s.AddCleanup(s.mcopy.Restore)

	s.sfdisk = testutil.MockCommand(c, "sfdisk", fmt.Sprintf("cat > %s/sfdisk.in", s.dir))
	s.AddCleanup(s.sfdisk.Restore)

	os.Setenv("SNAP_DEBUG_IMAGE_NO_CLEANUP", "1")
	s.AddCleanup(func() { os.Unsetenv("SNAP_DEBUG_IMAGE_NO_CLEANUP") })
}

func (s *volumeSuite) TearDownTest(c *C) {
	s.CmdBaseTest.TearDownTest(c)
}

func writeToExistingFile(c *C, where, what string) {
	f, err := os.OpenFile(where, os.O_WRONLY, 0)
	c.Assert(err, IsNil)
	defer f.Close()

	f.WriteString(what)
}

type contentEntry struct {
	offs int64
	what string
}

func verifyFileContent(c *C, where string, content []contentEntry) {
	f, err := os.Open(where)
	c.Assert(err, IsNil)
	defer f.Close()

	for _, expected := range content {
		expectedData := []byte(expected.what)
		buf := make([]byte, len(expectedData))
		n, err := f.ReadAt(buf, expected.offs)
		c.Assert(err, IsNil)
		c.Assert(n, Equals, len(buf))
		c.Check(buf, DeepEquals, expectedData)
	}
}

var gadgetYamlPC = `
volumes:
  pc:
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
        size: 50M
        content:
          - source: grubx64.efi
            target: EFI/boot/grubx64.efi
          - source: shim.efi.signed
            target: EFI/boot/bootx64.efi
          - source: grub.cfg
            target: EFI/ubuntu/grub.cfg
`

func (s *volumeSuite) TestPrepareHappyPC(c *C) {
	tmpDir := c.MkDir()
	preparedDir := c.MkDir()

	makeDirectoryTree(c, preparedDir, []mockEntry{
		// unpacked gadget contents
		{name: "gadget/meta/gadget.yaml", content: gadgetYamlPC},
		{name: "gadget/pc-boot.img", content: "pc-boot.img"},
		{name: "gadget/pc-core.img", content: "pc-core.img"},
		{name: "gadget/grubx64.efi", content: "grubx64.efi"},
		{name: "gadget/shim.efi.signed", content: "shim-efi.signed"},
		{name: "gadget/grub.cfg", content: "grub.cfg"},
		// grub config prepared by snap prepare-image
		{name: "image/boot/grub/grubenv", content: "grubenv-from-image"},
		{name: "image/boot/grub/grub.cfg", content: "grub.cfg-from-image"},
		// snaps
		{name: "image/var/lib/snapd/seed.yaml", content: "seed.yaml"},
	})

	mockVfat := func(img, label, contentDir string) error {
		c.Check(label, Equals, "system-boot")
		st, err := os.Stat(img)
		c.Assert(err, IsNil)
		// 50MB
		c.Check(st.Size(), Equals, int64(50*1024*1024))

		verifyDirectoryTree(c, contentDir, []mockEntry{
			// from gadget snap
			{name: "EFI/boot/grubx64.efi", content: "grubx64.efi"},
			{name: "EFI/boot/bootx64.efi", content: "shim-efi.signed"},
			// from post stage processing
			{name: "EFI/ubuntu/grubenv", content: "grubenv-from-image"},
			{name: "EFI/ubuntu/grub.cfg", content: "grub.cfg-from-image"},
		})

		writeToExistingFile(c, img, "system-boot")

		return nil
	}
	mockExt4 := func(img, label, contentDir string) error {
		c.Check(label, Equals, "writable")
		st, err := os.Stat(img)
		c.Assert(err, IsNil)
		// writable is at least 8MB
		c.Check(st.Size() > int64(8*1024*1024), Equals, true)

		verifyDirectoryTree(c, contentDir, []mockEntry{
			{name: "system-data/var/lib/snapd/seed.yaml", content: "seed.yaml"},
		})

		writeToExistingFile(c, img, "writable")
		return nil
	}

	restore := gadget.MockMkfsHandlers(map[string]gadget.MkfsFunc{
		"vfat": mockVfat,
		"ext4": mockExt4,
	})
	defer restore()

	rest, err := snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", tmpDir, preparedDir, "pc"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(s.sfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", filepath.Join(tmpDir, "output.img")},
	})

	c.Check(s.stdout.String(), Equals, fmt.Sprintf("image written to: %s\n", filepath.Join(tmpDir, "output.img")))

	verifyFileContent(c, filepath.Join(tmpDir, "output.img"), []contentEntry{
		// mbr
		{offs: 0, what: "pc-boot.img"},
		// TODO: verify offset write

		// BIOS Boot partition
		{offs: 1 * 1024 * 1024, what: "pc-core.img"},
		// EFI system partition
		{offs: 2 * 1024 * 1024, what: "system-boot"},
		// writable partition
		{offs: (2 + 50) * 1024 * 1024, what: "writable"},
	})
}

var gadgetYamlRPi = `
device-tree: bcm2709-rpi-2-b
volumes:
  pi:
    schema: mbr
    bootloader: u-boot
    structure:
      - type: 0C
        filesystem: vfat
        filesystem-label: system-boot
        size: 128M
        content:
          - source: boot-assets/
            target: /
`

func (s *volumeSuite) TestPrepareHappyRPi(c *C) {
	tmpDir := c.MkDir()
	preparedDir := c.MkDir()

	makeDirectoryTree(c, preparedDir, []mockEntry{
		// unpacked gadget contents
		{name: "gadget/meta/gadget.yaml", content: gadgetYamlRPi},
		{name: "gadget/boot-assets/start.elf", content: "start.elf"},
		{name: "gadget/boot-assets/overlays/rpi-display.dtbo", content: "rpi-display.dtbo"},
		{name: "gadget//boot-assets/uboot.env", content: "ubootenv"},
		// uboot env prepared by snap prepare-image
		{name: "image/boot/uboot/uboot.env", content: "ubootenv-from-image"},
		{name: "image/boot/uboot/pi-kernel_123.snap/kernel.img", content: "kernel.img"},
		// snaps
		{name: "image/var/lib/snapd/seed.yaml", content: "seed.yaml"},
	})

	mockVfat := func(img, label, contentDir string) error {
		c.Check(label, Equals, "system-boot")
		st, err := os.Stat(img)
		c.Assert(err, IsNil)
		// 128MB
		c.Check(st.Size(), Equals, int64(128*1024*1024))

		verifyDirectoryTree(c, contentDir, []mockEntry{
			// from gadget snap
			{name: "start.elf", content: "start.elf"},
			{name: "overlays/rpi-display.dtbo", content: "rpi-display.dtbo"},
			// from post stage processing
			{name: "uboot.env", content: "ubootenv-from-image"},
			{name: "pi-kernel_123.snap/kernel.img", content: "kernel.img"},
		})
		writeToExistingFile(c, img, "system-boot")
		return nil
	}
	mockExt4 := func(img, label, contentDir string) error {
		c.Check(label, Equals, "writable")
		st, err := os.Stat(img)
		c.Assert(err, IsNil)
		// writable is at least 8MB
		c.Check(st.Size() > int64(8*1024*1024), Equals, true)

		verifyDirectoryTree(c, contentDir, []mockEntry{
			{name: "system-data/var/lib/snapd/seed.yaml", content: "seed.yaml"},
		})
		writeToExistingFile(c, img, "writable")
		return nil
	}

	restore := gadget.MockMkfsHandlers(map[string]gadget.MkfsFunc{
		"vfat": mockVfat,
		"ext4": mockExt4,
	})
	defer restore()

	rest, err := snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", tmpDir, preparedDir, "pi"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})

	c.Check(s.sfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", filepath.Join(tmpDir, "output.img")},
	})

	c.Check(s.stdout.String(), Equals, fmt.Sprintf("image written to: %s\n", filepath.Join(tmpDir, "output.img")))

	verifyFileContent(c, filepath.Join(tmpDir, "output.img"), []contentEntry{
		// boot partition
		{offs: 1 * 1024 * 1024, what: "system-boot"},
		// writable partition
		{offs: (1 + 128) * 1024 * 1024, what: "writable"},
	})
}

func (s *volumeSuite) TestPrepareMultiVolumePrepareBoth(c *C) {
	var gadgetYamlMultiVolume = `
volumes:
  vol-1:
    schema: mbr
    bootloader: u-boot
    structure:
      - name: uboot
        type: bare
        offset: 1M
        size: 1M
        content:
          - image: foo.img
      - name: writable
        type: 83
        filesystem: ext4
        role: system-data
        size: 10M
  vol-2:
    structure:
      - name: uboot-on-vol2
        type: bare
        offset: 1M
        size: 1M
        content:
          - image: other-foo.img
`

	tmpDirVol1 := c.MkDir()
	tmpDirVol2 := c.MkDir()
	preparedDir := c.MkDir()

	makeDirectoryTree(c, preparedDir, []mockEntry{
		// unpacked gadget contents
		{name: "gadget/meta/gadget.yaml", content: gadgetYamlMultiVolume},
		{name: "gadget/foo.img", content: "foo.img"},
		{name: "gadget/other-foo.img", content: "other-foo.img"},
		// snaps
		{name: "image/var/lib/snapd/seed.yaml", content: "seed.yaml"},
	})

	mockVfat := func(img, label, contentDir string) error {
		return errors.New("unexpected call")
	}
	mockExt4 := func(img, label, contentDir string) error {
		// system-data is defined only for the first image
		c.Assert(strings.HasPrefix(img, tmpDirVol1), Equals, true, Commentf("unexpected image file path: %v", img))
		c.Check(label, Equals, "")
		st, err := os.Stat(img)
		c.Assert(err, IsNil)
		// writable is at least 8MB
		c.Check(st.Size(), Equals, int64(10*1024*1024))

		verifyDirectoryTree(c, contentDir, []mockEntry{
			{name: "system-data/var/lib/snapd/seed.yaml", content: "seed.yaml"},
		})
		writeToExistingFile(c, img, "writable")
		return nil
	}

	restore := gadget.MockMkfsHandlers(map[string]gadget.MkfsFunc{
		"vfat": mockVfat,
		"ext4": mockExt4,
	})
	defer restore()

	rest, err := snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", tmpDirVol1, preparedDir, "vol-1"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.sfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", filepath.Join(tmpDirVol1, "output.img")},
	})
	verifyFileContent(c, filepath.Join(tmpDirVol1, "output.img"), []contentEntry{
		// boot partition
		{offs: 1 * 1024 * 1024, what: "foo.img"},
		// writable partition
		{offs: (1 + 1) * 1024 * 1024, what: "writable"},
	})

	// now for the second volume
	rest, err = snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", tmpDirVol2, preparedDir, "vol-2"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	c.Check(s.sfdisk.Calls(), DeepEquals, [][]string{
		{"sfdisk", filepath.Join(tmpDirVol1, "output.img")},
		{"sfdisk", filepath.Join(tmpDirVol2, "output.img")},
	})
	verifyFileContent(c, filepath.Join(tmpDirVol2, "output.img"), []contentEntry{
		// boot partition
		{offs: 1 * 1024 * 1024, what: "other-foo.img"},
	})
}

func (s *volumeSuite) TestPrepareMultiVolumePrepareNoAutoSystemData(c *C) {
	var gadgetYamlMultiVolume = `
volumes:
  vol-1:
    schema: mbr
    bootloader: u-boot
    structure:
      - name: uboot
        type: bare
        offset: 1M
        size: 1M
        content:
          - image: foo.img
  vol-2:
    structure:
      - name: uboot-on-vol2
        type: bare
        offset: 1M
        size: 1M
        content:
          - image: bar.img
`

	tmpDirVol1 := c.MkDir()
	tmpDirVol2 := c.MkDir()
	preparedDir := c.MkDir()

	makeDirectoryTree(c, preparedDir, []mockEntry{
		// unpacked gadget contents
		{name: "gadget/meta/gadget.yaml", content: gadgetYamlMultiVolume},
		{name: "gadget/foo.img", content: "foo.img"},
		{name: "gadget/bar.img", content: "bar.img"},
		// not shipped in the image
		{name: "image/var/lib/snapd/seed.yaml", content: "seed.yaml"},
	})

	mockVfat := func(img, label, contentDir string) error {
		return errors.New("unexpected call")
	}
	mockExt4 := func(img, label, contentDir string) error {
		return errors.New("unexpected call")
	}
	restore := gadget.MockMkfsHandlers(map[string]gadget.MkfsFunc{
		"vfat": mockVfat,
		"ext4": mockExt4,
	})
	defer restore()

	rest, err := snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", tmpDirVol1, preparedDir, "vol-1"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	verifyFileContent(c, filepath.Join(tmpDirVol1, "output.img"), []contentEntry{
		// boot partition
		{offs: 1 * 1024 * 1024, what: "foo.img"},
	})

	// now for the second volume
	rest, err = snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", tmpDirVol2, preparedDir, "vol-2"})
	c.Assert(err, IsNil)
	c.Assert(rest, DeepEquals, []string{})
	verifyFileContent(c, filepath.Join(tmpDirVol2, "output.img"), []contentEntry{
		// boot partition
		{offs: 1 * 1024 * 1024, what: "bar.img"},
	})
}

func (s *volumeSuite) TestPrepareVolumeErrors(c *C) {
	var gadgetYaml = `
volumes:
  vol-1:
    schema: mbr
    bootloader: u-boot
    structure:
      - name: uboot
        type: bare
        offset: 1M
        size: 1M
        content:
          - image: foo.img
      - filesystem: ext4
        size: 1M
        type: 83
        role: system-data
`

	tmpDir := c.MkDir()
	preparedDir := c.MkDir()

	makeDirectoryTree(c, preparedDir, []mockEntry{
		// unpacked gadget contents
		{name: "gadget/meta/gadget.yaml", content: gadgetYaml},
	})

	mockVfat := func(img, label, contentDir string) error {
		return errors.New("unexpected call")
	}
	mockExt4 := func(img, label, contentDir string) error {
		return errors.New("controlled fail")
	}
	restore := gadget.MockMkfsHandlers(map[string]gadget.MkfsFunc{
		"vfat": mockVfat,
		"ext4": mockExt4,
	})
	defer restore()

	_, err := snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", "bogus-work-dir", preparedDir, "vol-1"})
	c.Assert(err, ErrorMatches, `work directory "bogus-work-dir" does not exist`)

	_, err = snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", tmpDir, "bogus-prepared-dir", "vol-1"})
	c.Assert(err, ErrorMatches, `open bogus-prepared-dir/gadget/meta/gadget.yaml: no such file or directory`)

	_, err = snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", tmpDir, preparedDir, "bad-volume"})
	c.Assert(err, ErrorMatches, `volume "bad-volume" not defined`)

	// <prepared-dir>/image is missing
	_, err = snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", tmpDir, preparedDir, "vol-1"})
	// note bogus-dir is quoted with simple ' or fancy quotes depending on locale
	c.Assert(err, ErrorMatches, "cannot calculate the size of root filesystem: running du failed: .*/image.: No such file or directory")

	// larger than declared system-data size
	makeDirectoryTree(c, preparedDir, []mockEntry{
		// unpacked gadget contents
		{name: "image/large-file", content: strings.Repeat("0", 2*1024*1024)},
		{name: "image/small-file", content: strings.Repeat("0", 100*1024)},
	})

	_, err = snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", tmpDir, preparedDir, "vol-1"})
	c.Assert(err, ErrorMatches, "rootfs size [0-9]+ is larger than declared system-data size 1048576")

	// remaining file should fit
	err = os.Remove(filepath.Join(preparedDir, "image/large-file"))
	c.Assert(err, IsNil)

	// still missing declared foo.img for uboot structure
	_, err = snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", tmpDir, preparedDir, "vol-1"})
	c.Assert(err, ErrorMatches, `cannot lay out volume "vol-1": cannot lay out structure #0 \("uboot"\): content "foo.img": .* no such file or directory`)

	makeDirectoryTree(c, preparedDir, []mockEntry{
		// unpacked gadget contents
		{name: "gadget/foo.img", content: "foo.img"},
	})

	_, err = snap_image.Parser().ParseArgs([]string{"prepare-volume", "--work-dir", tmpDir, preparedDir, "vol-1"})
	c.Assert(err, ErrorMatches, `cannot write structure #1 \("writable"\): cannot create filesystem image: cannot create "ext4" filesystem: controlled fail`)
}

func (s *volumeSuite) TestMakeSizedFile(c *C) {
	p := filepath.Join(s.dir, "foo")
	f, err := snap_image.MakeSizedFile(p, 1024)
	c.Assert(err, IsNil)
	c.Assert(f, NotNil)
	defer f.Close()
	c.Assert(f.Name(), Equals, p)
	// in case f corresponds to open, but already removed file
	c.Assert(osutil.FileExists(p), Equals, true)

	st, err := f.Stat()
	c.Assert(err, IsNil)
	c.Assert(st.Size(), Equals, int64(1024))
}

type mockEntry struct {
	name    string
	content string
}

func makeDirectoryTree(c *C, rootDir string, entries []mockEntry) {
	for _, e := range entries {
		p := filepath.Join(rootDir, e.name)
		if strings.HasSuffix(e.name, "/") {
			c.Assert(e.content, HasLen, 0, Commentf("content data not allowed for directory entry %q", e.name))
			err := os.MkdirAll(p, 0755)
			c.Assert(err, IsNil)
		} else {
			err := os.MkdirAll(filepath.Dir(p), 0755)
			c.Assert(err, IsNil)
			err = ioutil.WriteFile(p, []byte(e.content), 0644)
			c.Assert(err, IsNil)
		}
	}
}

func verifyDirectoryTree(c *C, rootDir string, entries []mockEntry) {
	for _, e := range entries {
		p := filepath.Join(rootDir, e.name)
		if strings.HasSuffix(e.name, "/") {
			c.Check(osutil.IsDirectory(p), Equals, true, Commentf("expected %q to be a directory", e.name))
		} else {
			c.Check(p, testutil.FileEquals, e.content)
		}
	}
}

func (s *volumeSuite) TestCopyTree(c *C) {
	src := c.MkDir()
	dst := c.MkDir()

	tree := []mockEntry{
		{name: "foo", content: "foo"},
		{name: "bar/inside-bar", content: "bar"},
		{name: "bar/baz/inside-baz", content: "baz"},
		{name: "not-foo/not-bar", content: "not"},
		{name: "empty-dir/"},
	}
	makeDirectoryTree(c, src, tree)

	err := snap_image.CopyTree(src, dst)
	c.Assert(err, IsNil)

	c.Assert(osutil.IsDirectory(filepath.Join(dst, "empty-dir")), Equals, true)
	c.Assert(filepath.Join(dst, "foo"), testutil.FileEquals, "foo")
	c.Assert(filepath.Join(dst, "bar/inside-bar"), testutil.FileEquals, "bar")
	c.Assert(filepath.Join(dst, "bar/baz/inside-baz"), testutil.FileEquals, "baz")
	c.Assert(filepath.Join(dst, "not-foo/not-bar"), testutil.FileEquals, "not")
}

func (s *volumeSuite) TestMeasureDirSizeReal(c *C) {
	dir := c.MkDir()

	tree := []mockEntry{
		{name: "foo", content: strings.Repeat("1", 1024)},
		{name: "bar/foo", content: strings.Repeat("1", 2048)},
	}
	makeDirectoryTree(c, dir, tree)

	sz, err := snap_image.MeasureRootfs(dir)
	c.Assert(err, IsNil)
	// because du rounds up the size to disk/fs specific blocks, we can't
	// just take a known value
	c.Assert(sz > 3072, Equals, true, Commentf("unexpected size: %v", sz))

	sz, err = snap_image.MeasureRootfs("bogus-dir")
	// note bogus-dir is quoted with simple ' or fancy quotes depending on locale
	c.Assert(err, ErrorMatches, `running du failed: du: cannot access .bogus-dir.: .*`)
	c.Assert(sz, Equals, int64(0))
}

func (s *volumeSuite) TestMeasureDirSizeMocked(c *C) {
	cmdDuOk := testutil.MockCommand(c, "du", "echo '1234      .'")
	defer cmdDuOk.Restore()

	dir := c.MkDir()

	sz, err := snap_image.MeasureRootfs(dir)
	c.Assert(err, IsNil)
	c.Assert(sz, Equals, int64(1234))
	c.Assert(cmdDuOk.Calls(), DeepEquals, [][]string{
		{"du", "-s", "-B1", dir},
	})

	cmdDuBadFormat := testutil.MockCommand(c, "du", "echo 'this is some bad format'")
	defer cmdDuBadFormat.Restore()

	sz, err = snap_image.MeasureRootfs(dir)
	c.Assert(err, ErrorMatches, `unexpected output: "this is some bad format\\n"`)
	c.Assert(sz, Equals, int64(0))

	cmdDuBadErr := testutil.MockCommand(c, "du", "echo 'fail'; exit 1")
	defer cmdDuBadErr.Restore()

	sz, err = snap_image.MeasureRootfs(dir)
	c.Assert(err, ErrorMatches, `running du failed: fail`)
	c.Assert(sz, Equals, int64(0))
}

func (s *volumeSuite) TestAlignSize(c *C) {
	c.Check(snap_image.AlignSize(1024, 4096), Equals, int64(4096))
	c.Check(snap_image.AlignSize(4095, 4096), Equals, int64(4096))
	c.Check(snap_image.AlignSize(4096, 512), Equals, int64(4096))
	c.Check(snap_image.AlignSize(4095, 512), Equals, int64(4096))
	c.Check(snap_image.AlignSize(4097, 512), Equals, int64(4608))

}
