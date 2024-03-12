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
	"strings"
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
	r := bootloader.MockPibootFiles(c, s.rootdir, nil)
	defer r()
	present, err = p.Present()
	c.Assert(err, IsNil)
	c.Assert(present, Equals, true)
}

func (s *pibootTestSuite) TestPibootGetEnvVar(c *C) {
	// We need PrepareImageTime due to fixed reference to /run/mnt otherwise
	opts := bootloader.Options{PrepareImageTime: true,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	r := bootloader.MockPibootFiles(c, s.rootdir, &opts)
	defer r()
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
	r := bootloader.MockPibootFiles(c, s.rootdir, nil)
	defer r()

	bootloader, err := bootloader.Find(s.rootdir, nil)
	c.Assert(err, IsNil)
	c.Assert(bootloader.Name(), Equals, "piboot")
}

func (s *pibootTestSuite) testPibootSetEnvWriteOnlyIfChanged(c *C, fromInitramfs bool) {
	opts := bootloader.Options{PrepareImageTime: true,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	r := bootloader.MockPibootFiles(c, s.rootdir, &opts)
	defer r()
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
	if fromInitramfs {
		nsbl, ok := p.(bootloader.NotScriptableBootloader)
		c.Assert(ok, Equals, true)
		err = nsbl.SetBootVarsFromInitramfs(map[string]string{"snap_ab": "b"})
	} else {
		err = p.SetBootVars(map[string]string{"snap_ab": "b"})
	}
	c.Assert(err, IsNil)

	st2, err := os.Stat(envFile)
	c.Assert(err, IsNil)
	c.Assert(st.ModTime(), Equals, st2.ModTime())
}

func (s *pibootTestSuite) TestPibootSetEnvWriteOnlyIfChanged(c *C) {
	// Run test from rootfs and from initramfs
	fromInitramfs := false
	s.testPibootSetEnvWriteOnlyIfChanged(c, fromInitramfs)
	fromInitramfs = true
	s.testPibootSetEnvWriteOnlyIfChanged(c, fromInitramfs)
}

func (s *pibootTestSuite) testExtractKernelAssets(c *C, dtbDir string) {
	opts := bootloader.Options{PrepareImageTime: true,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	r := bootloader.MockPibootFiles(c, s.rootdir, &opts)
	defer r()
	p := bootloader.NewPiboot(s.rootdir, &opts)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{filepath.Join(dtbDir, "foo.dtb"), "g'day, I'm foo.dtb"},
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

func (s *pibootTestSuite) TestExtractKernelAssets(c *C) {
	// armhf and arm64 kernel snaps store dtbs in different places
	s.testExtractKernelAssets(c, "dtbs")
	s.testExtractKernelAssets(c, "dtbs/broadcom")
}

func (s *pibootTestSuite) testExtractRecoveryKernelAssets(c *C, dtbDir string) {
	opts := bootloader.Options{PrepareImageTime: true,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	r := bootloader.MockPibootFiles(c, s.rootdir, &opts)
	defer r()
	p := bootloader.NewPiboot(s.rootdir, &opts)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{filepath.Join(dtbDir, "foo.dtb"), "g'day, I'm foo.dtb"},
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

func (s *pibootTestSuite) TestExtractRecoveryKernelAssets(c *C) {
	// armhf and arm64 kernel snaps store dtbs in different places
	s.testExtractRecoveryKernelAssets(c, "dtbs")
	s.testExtractRecoveryKernelAssets(c, "dtbs/broadcom")
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
		restore := bootloader.MockPibootFiles(c, dir, t.blOpts)
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
		restore()
	}
}

func (s *pibootTestSuite) TestCreateConfig(c *C) {
	opts := bootloader.Options{PrepareImageTime: false,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	r := bootloader.MockPibootFiles(c, s.rootdir, &opts)
	defer r()
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
			data: " snapd_recovery_mode=run\n",
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
	r := bootloader.MockPibootFiles(c, s.rootdir, &opts)
	defer r()
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
			data: " snapd_recovery_mode=run kernel_status=trying\n",
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
			data: " snapd_recovery_mode=run\n",
		},
	}
	for _, fInfo := range files {
		readData, err := ioutil.ReadFile(fInfo.path)
		c.Assert(err, IsNil)
		c.Assert(string(readData), Equals, fInfo.data)
	}
}

func (s *pibootTestSuite) TestCreateConfigCurrentNotEmpty(c *C) {
	opts := bootloader.Options{PrepareImageTime: false,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	r := bootloader.MockPibootFiles(c, s.rootdir, &opts)
	defer r()

	// Get some extra kernel command line parameters
	err := os.WriteFile(filepath.Join(s.rootdir, "cmdline.txt"),
		[]byte("opt1=foo bar\n"), 0644)
	c.Assert(err, IsNil)
	// Add some options to already existing config.txt
	err = os.WriteFile(filepath.Join(s.rootdir, "config.txt"),
		[]byte("rpi.option1=val\nos_prefix=1\nrpi.option2=val\n"), 0644)
	c.Assert(err, IsNil)
	p := bootloader.NewPiboot(s.rootdir, &opts)

	err = p.SetBootVars(map[string]string{
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
			data: "rpi.option1=val\nos_prefix=/piboot/ubuntu/pi-kernel_1/\nrpi.option2=val\n",
		},
		{
			path: filepath.Join(s.rootdir, "piboot/ubuntu/pi-kernel_1/cmdline.txt"),
			data: "opt1=foo bar snapd_recovery_mode=run\n",
		},
	}
	for _, fInfo := range files {
		readData, err := ioutil.ReadFile(fInfo.path)
		c.Assert(err, IsNil)
		c.Assert(string(readData), Equals, fInfo.data)
	}

	// Now set variables like in an update
	err = p.SetBootVars(map[string]string{
		"snap_kernel":         "pi-kernel_1",
		"snap_try_kernel":     "pi-kernel_2",
		"snapd_recovery_mode": "run",
		"kernel_status":       boot.TryStatus})
	c.Assert(err, IsNil)

	files = []struct {
		path string
		data string
	}{
		{
			path: filepath.Join(s.rootdir, "tryboot.txt"),
			data: "rpi.option1=val\nos_prefix=/piboot/ubuntu/pi-kernel_2/\nrpi.option2=val\n",
		},
		{
			path: filepath.Join(s.rootdir, "config.txt"),
			data: "rpi.option1=val\nos_prefix=/piboot/ubuntu/pi-kernel_1/\nrpi.option2=val\n",
		},
		{
			path: filepath.Join(s.rootdir, "piboot/ubuntu/pi-kernel_2/cmdline.txt"),
			data: "opt1=foo bar snapd_recovery_mode=run kernel_status=trying\n",
		},
	}
	for _, fInfo := range files {
		readData, err := ioutil.ReadFile(fInfo.path)
		c.Assert(err, IsNil)
		c.Assert(string(readData), Equals, fInfo.data)
	}
}

func (s *pibootTestSuite) TestOnlyOneOsPrefix(c *C) {
	opts := bootloader.Options{PrepareImageTime: false,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	r := bootloader.MockPibootFiles(c, s.rootdir, &opts)
	defer r()

	// Introuce two os_prefix lines
	err := os.WriteFile(filepath.Join(s.rootdir, "config.txt"),
		[]byte("os_prefix=1\nos_prefix=2\n"), 0644)
	c.Assert(err, IsNil)
	p := bootloader.NewPiboot(s.rootdir, &opts)

	err = p.SetBootVars(map[string]string{
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
			data: "os_prefix=/piboot/ubuntu/pi-kernel_1/\n# os_prefix=2\n",
		},
		{
			path: filepath.Join(s.rootdir, "piboot/ubuntu/pi-kernel_1/cmdline.txt"),
			data: " snapd_recovery_mode=run\n",
		},
	}
	for _, fInfo := range files {
		readData, err := ioutil.ReadFile(fInfo.path)
		c.Assert(err, IsNil)
		c.Assert(string(readData), Equals, fInfo.data)
	}
}

func (s *pibootTestSuite) TestGetRebootArguments(c *C) {
	opts := bootloader.Options{PrepareImageTime: false,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	r := bootloader.MockPibootFiles(c, s.rootdir, &opts)
	defer r()
	p := bootloader.NewPiboot(s.rootdir, &opts)
	c.Assert(p, NotNil)
	rbl, ok := p.(bootloader.RebootBootloader)
	c.Assert(ok, Equals, true)

	args, err := rbl.GetRebootArguments()
	c.Assert(err, IsNil)
	c.Assert(args, Equals, "")

	err = p.SetBootVars(map[string]string{"kernel_status": "try"})
	c.Assert(err, IsNil)

	args, err = rbl.GetRebootArguments()
	c.Assert(err, IsNil)
	c.Assert(args, Equals, "0 tryboot")
	err = p.SetBootVars(map[string]string{"kernel_status": ""})
	c.Assert(err, IsNil)
}

func (s *pibootTestSuite) TestGetRebootArgumentsNoEnv(c *C) {
	opts := bootloader.Options{PrepareImageTime: false,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	p := bootloader.NewPiboot(s.rootdir, &opts)
	c.Assert(p, NotNil)
	rbl, ok := p.(bootloader.RebootBootloader)
	c.Assert(ok, Equals, true)

	args, err := rbl.GetRebootArguments()
	c.Assert(err, ErrorMatches, "open .*/piboot.conf: no such file or directory")
	c.Assert(args, Equals, "")
}

func (s *pibootTestSuite) TestSetBootVarsFromInitramfs(c *C) {
	opts := bootloader.Options{PrepareImageTime: false,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	r := bootloader.MockPibootFiles(c, s.rootdir, &opts)
	defer r()
	p := bootloader.NewPiboot(s.rootdir, &opts)
	c.Assert(p, NotNil)
	nsbl, ok := p.(bootloader.NotScriptableBootloader)
	c.Assert(ok, Equals, true)

	err := nsbl.SetBootVarsFromInitramfs(map[string]string{"kernel_status": "trying"})
	c.Assert(err, IsNil)

	m, err := p.GetBootVars("kernel_status")
	c.Assert(err, IsNil)
	c.Assert(m, DeepEquals, map[string]string{
		"kernel_status": "trying",
	})
}

func (s *pibootTestSuite) testExtractKernelAssetsAndRemove(c *C, dtbDir string) {
	opts := bootloader.Options{PrepareImageTime: false,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	r := bootloader.MockPibootFiles(c, s.rootdir, &opts)
	defer r()
	p := bootloader.NewPiboot(s.rootdir, &opts)
	c.Assert(p, NotNil)

	files := [][]string{
		{"kernel.img", "I'm a kernel"},
		{"initrd.img", "...and I'm an initrd"},
		{filepath.Join(dtbDir, "foo.dtb"), "g'day, I'm foo.dtb"},
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

	err = p.ExtractKernelAssets(info, snapf)
	c.Assert(err, IsNil)

	// this is where the kernel/initrd is unpacked
	kernelAssetsDir := filepath.Join(s.rootdir, "piboot", "ubuntu", "ubuntu-kernel_42.snap")

	for _, def := range files {
		if def[0] == "meta/kernel.yaml" {
			break
		}

		destPath := def[0]
		if strings.HasPrefix(destPath, "dtbs/broadcom/") {
			destPath = strings.TrimPrefix(destPath, "dtbs/broadcom/")
		} else if strings.HasPrefix(destPath, "dtbs/") {
			destPath = strings.TrimPrefix(destPath, "dtbs/")
		}
		fullFn := filepath.Join(kernelAssetsDir, destPath)
		c.Check(fullFn, testutil.FileEquals, def[1])
	}

	// remove
	err = p.RemoveKernelAssets(info)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(kernelAssetsDir), Equals, false)
}

func (s *pibootTestSuite) TestExtractKernelAssetsAndRemove(c *C) {
	// armhf and arm64 kernel snaps store dtbs in different places
	s.testExtractKernelAssetsAndRemove(c, "dtbs")
	s.testExtractKernelAssetsAndRemove(c, "dtbs/broadcom")
}

func (s *pibootTestSuite) testExtractKernelAssetsOnRPi4CheckEeprom(c *C, rpiRevisionCode, eepromTimeStamp []byte, errExpected bool) {
	opts := bootloader.Options{PrepareImageTime: false,
		Role: bootloader.RoleRunMode, NoSlashBoot: true}
	r := bootloader.MockPibootFiles(c, s.rootdir, &opts)
	defer r()
	r = bootloader.MockRPi4Files(c, s.rootdir, rpiRevisionCode, eepromTimeStamp)
	defer r()
	p := bootloader.NewPiboot(s.rootdir, &opts)
	c.Assert(p, NotNil)

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

	err = p.ExtractKernelAssets(info, snapf)
	if errExpected {
		c.Check(err.Error(), Equals,
			"your EEPROM does not support tryboot, please upgrade to a newer one before installing Ubuntu Core - see http://forum.snapcraft.io/t/29455 for more details")
		return
	}

	c.Assert(err, IsNil)

	// this is where the kernel/initrd is unpacked
	kernelAssetsDir := filepath.Join(s.rootdir, "piboot", "ubuntu", "ubuntu-kernel_42.snap")

	for _, def := range files {
		if def[0] == "meta/kernel.yaml" {
			break
		}

		destPath := def[0]
		if strings.HasPrefix(destPath, "dtbs/broadcom/") {
			destPath = strings.TrimPrefix(destPath, "dtbs/broadcom/")
		} else if strings.HasPrefix(destPath, "dtbs/") {
			destPath = strings.TrimPrefix(destPath, "dtbs/")
		}
		fullFn := filepath.Join(kernelAssetsDir, destPath)
		c.Check(fullFn, testutil.FileEquals, def[1])
	}

	// remove
	err = p.RemoveKernelAssets(info)
	c.Assert(err, IsNil)

	c.Check(osutil.FileExists(kernelAssetsDir), Equals, false)
}

func (s *pibootTestSuite) TestExtractKernelAssetsOnRPi4CheckEeprom(c *C) {
	// Rev code is RPi4, eeprom supports tryboot
	expectFailure := false
	s.testExtractKernelAssetsOnRPi4CheckEeprom(c,
		[]byte{0x00, 0xc0, 0x31, 0x11},
		[]byte{0x61, 0xf0, 0x09, 0x91},
		expectFailure)
	// Rev code is RPi4, eeprom does not support tryboot
	expectFailure = true
	s.testExtractKernelAssetsOnRPi4CheckEeprom(c,
		[]byte{0x00, 0xc0, 0x31, 0x11},
		[]byte{0x60, 0x53, 0x15, 0x32},
		expectFailure)
	// Rev code is RPi3
	expectFailure = false
	s.testExtractKernelAssetsOnRPi4CheckEeprom(c,
		[]byte{0x00, 0xa0, 0x20, 0x82},
		[]byte{},
		expectFailure)
}
