// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type ubootTestSuite struct {
	baseBootenvTestSuite
}

var _ = Suite(&ubootTestSuite{})

func (s *ubootTestSuite) TestNewUboot(c *C) {
	// no files means bl is not present, but we can still create the bl object
	u := bootloader.NewUboot(s.rootdir, nil)
	c.Assert(u, NotNil)
	c.Assert(u.Name(), Equals, "uboot")

	present, err := u.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, false)

	// now with files present, the bl is present
	bootloader.MockUbootFiles(c, s.rootdir, nil)
	present, err = u.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, true)
}

func (s *ubootTestSuite) TestUbootGetEnvVar(c *C) {
	bootloader.MockUbootFiles(c, s.rootdir, nil)
	u := bootloader.NewUboot(s.rootdir, nil)
	c.Assert(u, NotNil)
	err := u.SetBootVars(map[string]string{
		"snap_mode": "",
		"snap_core": "4",
	})
	c.Assert(err, IsNil)

	m, err := u.GetBootVars("snap_mode", "snap_core")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"snap_mode": "",
		"snap_core": "4",
	})
}

func (s *ubootTestSuite) TestGetBootloaderWithUboot(c *C) {
	bootloader.MockUbootFiles(c, s.rootdir, nil)

	bootloader, err := bootloader.Find(s.rootdir, nil)
	c.Assert(err, IsNil)
	c.Assert(bootloader.Name(), Equals, "uboot")
}

func (s *ubootTestSuite) TestUbootSetEnvNoUselessWrites(c *C) {
	bootloader.MockUbootFiles(c, s.rootdir, nil)
	u := bootloader.NewUboot(s.rootdir, nil)
	c.Assert(u, NotNil)

	envFile := bootloader.UbootConfigFile(u)
	env, err := ubootenv.Create(envFile, 4096, ubootenv.CreateOptions{HeaderFlagByte: true})
	c.Assert(err, IsNil)
	env.Set("snap_ab", "b")
	env.Set("snap_mode", "")
	err = env.Save()
	c.Assert(err, IsNil)

	st, err := os.Stat(envFile)
	c.Assert(err, IsNil)
	time.Sleep(100 * time.Millisecond)

	// note that we set to the same var as above
	err = u.SetBootVars(map[string]string{"snap_ab": "b"})
	c.Assert(err, IsNil)

	env, err = ubootenv.Open(envFile)
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "snap_ab=b\n")

	st2, err := os.Stat(envFile)
	c.Assert(err, IsNil)
	c.Assert(st.ModTime(), Equals, st2.ModTime())
}

func (s *ubootTestSuite) TestUbootSetBootVarFwEnv(c *C) {
	bootloader.MockUbootFiles(c, s.rootdir, nil)
	u := bootloader.NewUboot(s.rootdir, nil)

	err := u.SetBootVars(map[string]string{"key": "value"})
	c.Assert(err, IsNil)

	content, err := u.GetBootVars("key")
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, map[string]string{"key": "value"})
}

func (s *ubootTestSuite) TestUbootGetBootVarFwEnv(c *C) {
	bootloader.MockUbootFiles(c, s.rootdir, nil)
	u := bootloader.NewUboot(s.rootdir, nil)

	err := u.SetBootVars(map[string]string{"key2": "value2"})
	c.Assert(err, IsNil)

	content, err := u.GetBootVars("key2")
	c.Assert(err, IsNil)
	c.Assert(content, DeepEquals, map[string]string{"key2": "value2"})
}

func (s *ubootTestSuite) TestExtractKernelAssetsAndRemove(c *C) {
	bootloader.MockUbootFiles(c, s.rootdir, nil)
	u := bootloader.NewUboot(s.rootdir, nil)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"dtbs/foo.dtb", "g'day, I'm foo.dtb"},
		{"dtbs/bar.dtb", "hello, I'm bar.dtb"},
		// must be last
		{"meta/kernel.yaml", "version: 4.2"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf, err := snapfile.Open(fn)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, si)
	c.Assert(err, IsNil)

	err = u.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// this is where the kernel/initrd is unpacked
	kernelAssetsDir := filepath.Join(s.rootdir, "boot", "uboot", "ubuntu-kernel_42.snap")

	for _, def := range files {
		if def[0] == "meta/kernel.yaml" {
			break
		}

		fullFn := filepath.Join(kernelAssetsDir, def[0])
		c.Check(fullFn, testutil.FileEquals, def[1])
	}

	// remove
	err = u.RemoveKernelAssets(info)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(kernelAssetsDir), Equals, false)
}

func (s *ubootTestSuite) TestExtractRecoveryKernelAssets(c *C) {
	bootloader.MockUbootFiles(c, s.rootdir, nil)
	u := bootloader.NewUboot(s.rootdir, nil)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"dtbs/foo.dtb", "foo dtb"},
		{"dtbs/bar.dto", "bar dtbo"},
		// must be last
		{"meta/kernel.yaml", "version: 4.2"},
	}
	si := &snap.SideInfo{
		RealName: "ubuntu-kernel",
		Revision: snap.R(42),
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf, err := snapfile.Open(fn)
	c.Assert(err, IsNil)

	info, err := snap.ReadInfoFromSnapFile(snapf, si)
	c.Assert(err, IsNil)

	// try with empty recovery dir first to check the errors
	err = u.ExtractRecoveryKernelAssets("", info, snapf)
	c.Assert(err, ErrorMatches, "internal error: recoverySystemDir unset")

	// now the expected behavior
	err = u.ExtractRecoveryKernelAssets("recovery-dir", info, snapf)
	c.Assert(err, IsNil)

	// this is where the kernel/initrd is unpacked
	kernelAssetsDir := filepath.Join(s.rootdir, "recovery-dir", "kernel")

	for _, def := range files {
		if def[0] == "meta/kernel.yaml" {
			break
		}

		fullFn := filepath.Join(kernelAssetsDir, def[0])
		c.Check(fullFn, testutil.FileEquals, def[1])
	}
}

func (s *ubootTestSuite) TestUbootUC20OptsPlacement(c *C) {
	tt := []struct {
		blOpts  *bootloader.Options
		expEnv  string
		comment string
	}{
		{
			nil,
			"/boot/uboot/uboot.env",
			"traditional uboot.env",
		},
		{
			&bootloader.Options{Role: bootloader.RoleRunMode, NoSlashBoot: true},
			"/uboot/ubuntu/boot.sel",
			"uc20 install mode boot.sel",
		},
		{
			&bootloader.Options{Role: bootloader.RoleRunMode},
			"/boot/uboot/boot.sel",
			"uc20 run mode boot.sel",
		},
		{
			&bootloader.Options{Role: bootloader.RoleRecovery},
			"/uboot/ubuntu/boot.sel",
			"uc20 recovery boot.sel",
		},
	}

	for _, t := range tt {
		dir := c.MkDir()
		bootloader.MockUbootFiles(c, dir, t.blOpts)
		u := bootloader.NewUboot(dir, t.blOpts)
		c.Assert(u, NotNil, Commentf(t.comment))
		c.Assert(bootloader.UbootConfigFile(u), Equals, filepath.Join(dir, t.expEnv), Commentf(t.comment))

		// if we set boot vars on the uboot, we can open the config file and
		// get the same variables
		c.Assert(u.SetBootVars(map[string]string{"hello": "there"}), IsNil)
		env, err := ubootenv.Open(filepath.Join(dir, t.expEnv))
		c.Assert(err, IsNil)
		c.Assert(env.Get("hello"), Equals, "there")
	}
}
