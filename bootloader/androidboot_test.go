// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package bootloader_test

import (
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
)

type androidBootTestSuite struct {
	baseBootenvTestSuite
}

var _ = Suite(&androidBootTestSuite{})

func (s *androidBootTestSuite) SetUpTest(c *C) {
	s.baseBootenvTestSuite.SetUpTest(c)

	// the file needs to exist for androidboot object to be created
	bootloader.MockAndroidBootFile(c, s.rootdir, 0644)
}

func (s *androidBootTestSuite) TestNewAndroidboot(c *C) {
	// no files means bl is not present, but we can still create the bl object
	c.Assert(os.RemoveAll(s.rootdir), IsNil)
	a := bootloader.NewAndroidBoot(s.rootdir)
	c.Assert(a, NotNil)
	c.Assert(a.Name(), Equals, "androidboot")

	present := mylog.Check2(a.Present())

	c.Assert(present, Equals, false)

	// now with files present, the bl is present
	bootloader.MockAndroidBootFile(c, s.rootdir, 0644)
	present = mylog.Check2(a.Present())

	c.Assert(present, Equals, true)
}

func (s *androidBootTestSuite) TestSetGetBootVar(c *C) {
	a := bootloader.NewAndroidBoot(s.rootdir)
	bootVars := map[string]string{"snap_mode": boot.TryStatus}
	a.SetBootVars(bootVars)

	v := mylog.Check2(a.GetBootVars("snap_mode"))

	c.Check(v, HasLen, 1)
	c.Check(v["snap_mode"], Equals, boot.TryStatus)
}

func (s *androidBootTestSuite) TestExtractKernelAssetsNoUnpacksKernel(c *C) {
	a := bootloader.NewAndroidBoot(s.rootdir)

	c.Assert(a, NotNil)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"meta/kernel.yaml", "version: 4.2"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf := mylog.Check2(snapfile.Open(fn))


	info := mylog.Check2(snap.ReadInfoFromSnapFile(snapf, si))

	mylog.Check(a.ExtractKernelAssets(info, snapf))


	// kernel is *not* here
	kernimg := filepath.Join(s.rootdir, "boot", "androidboot", "ubuntu-kernel_42.snap", "kernel.img")
	c.Assert(osutil.FileExists(kernimg), Equals, false)
}
