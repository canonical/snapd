// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package boot_test

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/partition"
	"github.com/snapcore/snapd/progress"
	"github.com/snapcore/snapd/release"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snaptest"
)

func TestKernelOS(t *testing.T) { TestingT(t) }

// XXX: share this
// mockBootloader mocks the bootloader interface and records all
// set/get calls
type mockBootloader struct {
	bootvars map[string]string
	bootdir  string
	name     string
}

func newMockBootloader(bootdir string) *mockBootloader {
	return &mockBootloader{
		bootvars: make(map[string]string),
		bootdir:  bootdir,
		name:     "mocky",
	}
}

func (b *mockBootloader) SetBootVar(key, value string) error {
	b.bootvars[key] = value
	return nil
}

func (b *mockBootloader) GetBootVar(key string) (string, error) {
	return b.bootvars[key], nil
}

func (b *mockBootloader) Dir() string {
	return b.bootdir
}

func (b *mockBootloader) Name() string {
	return b.name
}

type kernelOSSuite struct {
	bootloader *mockBootloader
}

var _ = Suite(&kernelOSSuite{})

func (s *kernelOSSuite) SetUpTest(c *C) {
	dirs.SetRootDir(c.MkDir())
	s.bootloader = newMockBootloader(c.MkDir())
	partition.ForceBootloader(s.bootloader)
}

func (s *kernelOSSuite) TearDownTest(c *C) {
	dirs.SetRootDir("")
	partition.ForceBootloader(nil)
}

func populate(c *C, dir string, files [][]string) {
	for _, def := range files {
		filename := def[0]
		content := def[1]
		basedir := filepath.Dir(filepath.Join(dir, filename))
		err := os.MkdirAll(basedir, 0755)
		c.Assert(err, IsNil)
		err = ioutil.WriteFile(filepath.Join(dir, filename), []byte(content), 0644)
		c.Assert(err, IsNil)
	}
}

const packageKernel = `
name: ubuntu-kernel
version: 4.0-1
type: kernel
vendor: Someone
`

func (s *kernelOSSuite) TestExtractKernelAssetsAndRemove(c *C) {
	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"dtbs/foo.dtb", "g'day, I'm foo.dtb"},
		{"dtbs/bar.dtb", "hello, I'm bar.dtb"},
		// must be last
		{"meta/kernel.yaml", "version: 4.2"},
	}

	si := &snap.SideInfo{
		OfficialName: "ubuntu-kernel",
		Revision:     snap.R(42),
	}
	snap := snaptest.MockSnap(c, packageKernel, si)
	populate(c, snap.MountDir(), files)

	err := boot.ExtractKernelAssets(snap, &progress.NullProgress{})
	c.Assert(err, IsNil)

	// this is where the kernel/initrd is unpacked
	bootdir := s.bootloader.Dir()

	kernelAssetsDir := filepath.Join(bootdir, "ubuntu-kernel_42.snap")

	for _, def := range files {
		if def[0] == "meta/kernel.yaml" {
			break
		}

		fullFn := filepath.Join(kernelAssetsDir, def[0])
		content, err := ioutil.ReadFile(fullFn)
		c.Assert(err, IsNil)
		c.Assert(string(content), Equals, def[1])
	}

	// remove
	err = boot.RemoveKernelAssets(snap, &progress.NullProgress{})
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(kernelAssetsDir), Equals, false)
}

func (s *kernelOSSuite) TestExtractKernelAssetsNoUnpacksKernelForGrub(c *C) {
	// pretend to be a grub system
	s.bootloader.name = "grub"

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	si := &snap.SideInfo{
		OfficialName: "ubuntu-kernel",
		Revision:     snap.R(42),
	}
	snap := snaptest.MockSnap(c, packageKernel, si)
	populate(c, snap.MountDir(), files)

	err := boot.ExtractKernelAssets(snap, &progress.NullProgress{})
	c.Assert(err, IsNil)

	// kernel is *not* here
	kernimg := filepath.Join(s.bootloader.Dir(), "ubuntu-kernel_42.snap", "kernel.img")
	c.Assert(osutil.FileExists(kernimg), Equals, false)
}

func (s *kernelOSSuite) TestExtractKernelAssetsError(c *C) {
	info := &snap.Info{}
	info.Type = snap.TypeApp

	err := boot.ExtractKernelAssets(info, nil)
	c.Assert(err, ErrorMatches, `cannot extract kernel assets from snap type "app"`)
}

// SetNextBoot should do nothing on classic LP: #1580403
func (s *kernelOSSuite) TestSetNextBootOnClassic(c *C) {
	restore := release.MockOnClassic(true)
	defer restore()

	// Create a fake OS snap that we try to update
	snapInfo := snaptest.MockSnap(c, "type: os", &snap.SideInfo{Revision: snap.R(42)})
	err := boot.SetNextBoot(snapInfo)
	c.Assert(err, IsNil)

	c.Assert(s.bootloader.bootvars, HasLen, 0)
}

func (s *kernelOSSuite) TestSetNextBootForCore(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	info := &snap.Info{}
	info.Type = snap.TypeOS
	info.OfficialName = "core"
	info.Revision = snap.R(100)

	err := boot.SetNextBoot(info)
	c.Assert(err, IsNil)

	c.Assert(s.bootloader.bootvars, DeepEquals, map[string]string{
		"snappy_os":   "core_100.snap",
		"snappy_mode": "try",
	})

	c.Check(boot.KernelOrOsRebootRequired(info), Equals, true)
}

func (s *kernelOSSuite) TestSetNextBootForKernel(c *C) {
	restore := release.MockOnClassic(false)
	defer restore()

	info := &snap.Info{}
	info.Type = snap.TypeKernel
	info.OfficialName = "krnl"
	info.Revision = snap.R(42)

	err := boot.SetNextBoot(info)
	c.Assert(err, IsNil)

	c.Assert(s.bootloader.bootvars, DeepEquals, map[string]string{
		"snappy_kernel": "krnl_42.snap",
		"snappy_mode":   "try",
	})

	s.bootloader.bootvars["snappy_good_kernel"] = "krnl_40.snap"
	s.bootloader.bootvars["snappy_kernel"] = "krnl_42.snap"
	c.Check(boot.KernelOrOsRebootRequired(info), Equals, true)

	// simulate good boot
	s.bootloader.bootvars["snappy_good_kernel"] = "krnl_42.snap"
	c.Check(boot.KernelOrOsRebootRequired(info), Equals, false)
}
