// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/snapfile"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/testutil"
)

type pibootTestSuite struct {
	baseBootenvTestSuite
}

var _ = Suite(&pibootTestSuite{})

func (s *pibootTestSuite) TestNewPiboot(c *C) {
	// no files means bl is not present, but we can still create the bl object
	p := bootloader.NewPiboot(s.rootdir, nil)
	c.Assert(p, NotNil)
	c.Assert(p.Name(), Equals, "piboot")

	present, err := p.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, false)

	// now with files present, the bl is present
	bootloader.MockPibootFiles(c, s.rootdir, nil)
	present, err = p.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, true)
}

func (s *pibootTestSuite) TestPibootGetEnvVar(c *C) {
	// We need PrepareImageTime due to fixed reference to /run/mnt otherwise
	opts := bootloader.Options{PrepareImageTime: true,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	bootloader.MockPibootFiles(c, s.rootdir, &opts)
	p := bootloader.NewPiboot(s.rootdir, &opts)
	c.Assert(p, NotNil)
	err := p.SetBootVars(map[string]string{
		"snap_mode": "",
		"snap_core": "4",
	})
	c.Assert(err, IsNil)

	m, err := p.GetBootVars("snap_mode", "snap_core")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"snap_mode": "",
		"snap_core": "4",
	})
}

func (s *pibootTestSuite) TestGetBootloaderWithPiboot(c *C) {
	bootloader.MockPibootFiles(c, s.rootdir, nil)

	bootloader, err := bootloader.Find(s.rootdir, nil)
	c.Assert(err, IsNil)
	c.Assert(bootloader.Name(), Equals, "piboot")
}

func (s *pibootTestSuite) TestPibootSetEnvWriteOnlyIfChanged(c *C) {
	opts := bootloader.Options{PrepareImageTime: true,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	bootloader.MockPibootFiles(c, s.rootdir, &opts)
	p := bootloader.NewPiboot(s.rootdir, &opts)
	c.Assert(p, NotNil)

	envFile := bootloader.PibootConfigFile(p)
	env, err := ubootenv.OpenWithFlags(envFile, ubootenv.OpenBestEffort)
	c.Assert(err, IsNil)
	env.Set("snap_ab", "b")
	env.Set("snap_mode", "")
	err = env.Save()
	c.Assert(err, IsNil)

	st, err := os.Stat(envFile)
	c.Assert(err, IsNil)
	time.Sleep(100 * time.Millisecond)

	// note that we set to the same var to the same value as above
	err = p.SetBootVars(map[string]string{"snap_ab": "b"})
	c.Assert(err, IsNil)

	st2, err := os.Stat(envFile)
	c.Assert(err, IsNil)
	c.Assert(st.ModTime(), Equals, st2.ModTime())
}

func (s *pibootTestSuite) TestExtractKernelAssets(c *C) {
	opts := bootloader.Options{PrepareImageTime: true,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	bootloader.MockPibootFiles(c, s.rootdir, &opts)
	p := bootloader.NewPiboot(s.rootdir, &opts)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"dtbs/broadcom/foo.dtb", "g'day, I'm foo.dtb"},
		{"dtbs/overlays/bar.dtbo", "hello, I'm bar.dtbo"},
		// must be last
		{"meta/kernel.yaml", "version: 4.2"},
	}
	fn := snaptest.MakeTestSnapWithFiles(c, packageKernel, files)
	snapf, err := snapfile.Open(fn)
	c.Assert(err, IsNil)

	assetsDir, err := ioutil.TempDir("", "kernel-assets")
	c.Assert(err, IsNil)
	defer os.RemoveAll(assetsDir)

	err = bootloader.LayoutKernelAssetsToDir(p, snapf, assetsDir)
	c.Assert(err, IsNil)
	// Do again, as extracting might be called again for an
	// already extracted kernel.
	err = bootloader.LayoutKernelAssetsToDir(p, snapf, assetsDir)
	c.Assert(err, IsNil)

	// Extraction folders for files slice
	destDirs := []string{
		assetsDir, assetsDir, assetsDir, filepath.Join(assetsDir, "overlays"),
	}
	for i, dir := range destDirs {
		fullFn := filepath.Join(dir, filepath.Base(files[i][0]))
		c.Check(fullFn, testutil.FileEquals, files[i][1])
	}

	// Check that file required by piboot is created
	readmeFn := filepath.Join(assetsDir, "overlays", "README")
	c.Check(readmeFn, testutil.FilePresent)
}

func (s *pibootTestSuite) TestExtractRecoveryKernelAssets(c *C) {
	opts := bootloader.Options{PrepareImageTime: true,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	bootloader.MockPibootFiles(c, s.rootdir, &opts)
	p := bootloader.NewPiboot(s.rootdir, &opts)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{"dtbs/broadcom/foo.dtb", "g'day, I'm foo.dtb"},
		{"dtbs/overlays/bar.dtbo", "hello, I'm bar.dtbo"},
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
	err = p.ExtractRecoveryKernelAssets("", info, snapf)
	c.Assert(err, ErrorMatches, "internal error: recoverySystemDir unset")

	// now the expected behavior
	err = p.ExtractRecoveryKernelAssets("recovery-dir", info, snapf)
	c.Assert(err, IsNil)

	// Extraction folders for files slice
	assetsDir := filepath.Join(s.rootdir, "recovery-dir", "kernel")
	destDirs := []string{
		assetsDir, assetsDir, assetsDir, filepath.Join(assetsDir, "overlays"),
	}
	for i, dir := range destDirs {
		fullFn := filepath.Join(dir, filepath.Base(files[i][0]))
		c.Check(fullFn, testutil.FileEquals, files[i][1])
	}

	// Check that file required by piboot is created
	readmeFn := filepath.Join(assetsDir, "overlays", "README")
	c.Check(readmeFn, testutil.FilePresent)
}

func (s *pibootTestSuite) TestPibootUC20OptsPlacement(c *C) {
	tt := []struct {
		blOpts  *bootloader.Options
		expEnv  string
		comment string
	}{
		{
			&bootloader.Options{PrepareImageTime: true,
				Role: bootloader.RoleRunMode, NoSlashBoot: true},
			"/piboot/ubuntu/piboot.conf",
			"uc20 install mode piboot.conf",
		},
		{
			&bootloader.Options{PrepareImageTime: true,
				Role: bootloader.RoleRunMode},
			"/boot/piboot/piboot.conf",
			"uc20 run mode piboot.conf",
		},
		{
			&bootloader.Options{PrepareImageTime: true,
				Role: bootloader.RoleRecovery},
			"/piboot/ubuntu/piboot.conf",
			"uc20 recovery piboot.conf",
		},
	}

	for _, t := range tt {
		dir := c.MkDir()
		bootloader.MockPibootFiles(c, dir, t.blOpts)
		p := bootloader.NewPiboot(dir, t.blOpts)
		c.Assert(p, NotNil, Commentf(t.comment))
		c.Assert(bootloader.PibootConfigFile(p), Equals,
			filepath.Join(dir, t.expEnv), Commentf(t.comment))

		// if we set boot vars on the piboot, we can open the config file and
		// get the same variables
		c.Assert(p.SetBootVars(map[string]string{"hello": "there"}), IsNil)
		env, err := ubootenv.OpenWithFlags(filepath.Join(dir, t.expEnv),
			ubootenv.OpenBestEffort)
		c.Assert(err, IsNil)
		c.Assert(env.Get("hello"), Equals, "there")
	}
}

func (s *pibootTestSuite) TestCreateConfig(c *C) {
	opts := bootloader.Options{PrepareImageTime: false,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	bootloader.MockPibootFiles(c, s.rootdir, &opts)
	p := bootloader.NewPiboot(s.rootdir, &opts)

	err := p.SetBootVars(map[string]string{
		"snap_kernel":         "pi-kernel_1",
		"snapd_recovery_mode": "run",
		"kernel_status":       boot.DefaultStatus})
	c.Assert(err, IsNil)

	files := []struct {
		path string
		data string
	}{
		{
			path: filepath.Join(s.rootdir, "config.txt"),
			data: "\nos_prefix=/piboot/ubuntu/pi-kernel_1/\n",
		},
		{
			path: filepath.Join(s.rootdir, "piboot/ubuntu/pi-kernel_1/cmdline.txt"),
			data: "  snapd_recovery_mode=run\n",
		},
	}
	for _, fInfo := range files {
		readData, err := ioutil.ReadFile(fInfo.path)
		c.Assert(err, IsNil)
		c.Assert(string(readData), Equals, fInfo.data)
	}
}

func (s *pibootTestSuite) TestCreateTrybootCfg(c *C) {
	opts := bootloader.Options{PrepareImageTime: false,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	bootloader.MockPibootFiles(c, s.rootdir, &opts)
	p := bootloader.NewPiboot(s.rootdir, &opts)

	err := p.SetBootVars(map[string]string{
		"snap_kernel":         "pi-kernel_1",
		"snap_try_kernel":     "pi-kernel_2",
		"snapd_recovery_mode": "run",
		"kernel_status":       boot.TryStatus})
	c.Assert(err, IsNil)

	files := []struct {
		path string
		data string
	}{
		{
			path: filepath.Join(s.rootdir, "tryboot.txt"),
			data: "\nos_prefix=/piboot/ubuntu/pi-kernel_2/\n",
		},
		{
			path: filepath.Join(s.rootdir, "piboot/ubuntu/pi-kernel_2/cmdline.txt"),
			data: "  snapd_recovery_mode=run kernel_status=trying\n",
		},
		{
			path: filepath.Join(s.rootdir, "reboot-param"),
			data: "0 tryboot\n",
		},
	}
	for _, fInfo := range files {
		readData, err := ioutil.ReadFile(fInfo.path)
		c.Assert(err, IsNil)
		c.Assert(string(readData), Equals, fInfo.data)
	}

	// Now set variables like in an after update reboot
	err = p.SetBootVars(map[string]string{
		"snap_kernel":         "pi-kernel_2",
		"snap_try_kernel":     "",
		"snapd_recovery_mode": "run",
		"kernel_status":       boot.DefaultStatus})
	c.Assert(err, IsNil)

	c.Assert(osutil.FileExists(filepath.Join(s.rootdir, "tryboot.txt")), Equals, false)

	files = []struct {
		path string
		data string
	}{
		{
			path: filepath.Join(s.rootdir, "config.txt"),
			data: "\nos_prefix=/piboot/ubuntu/pi-kernel_2/\n",
		},
		{
			path: filepath.Join(s.rootdir, "piboot/ubuntu/pi-kernel_2/cmdline.txt"),
			data: "  snapd_recovery_mode=run\n",
		},
	}
	for _, fInfo := range files {
		readData, err := ioutil.ReadFile(fInfo.path)
		c.Assert(err, IsNil)
		c.Assert(string(readData), Equals, fInfo.data)
	}
}
