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

package partition

import (
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mvo5/uboot-go/uenv"
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/helpers"
)

// TODO move to uboot specific test suite.

const fakeUbootEnvData = `
# This is a snappy variables and boot logic file and is entirely generated and
# managed by Snappy. Modifications may break boot
######
# functions to load kernel, initrd and fdt from various env values
loadfiles=run loadkernel; run loadinitrd; run loadfdt
loadkernel=load mmc ${mmcdev}:${mmcpart} ${loadaddr} ${snappy_ab}/${kernel_file}
loadinitrd=load mmc ${mmcdev}:${mmcpart} ${initrd_addr} ${snappy_ab}/${initrd_file}; setenv initrd_size ${filesize}
loadfdt=load mmc ${mmcdev}:${mmcpart} ${fdtaddr} ${snappy_ab}/dtbs/${fdtfile}

# standard kernel and initrd file names; NB: fdtfile is set early from bootcmd
kernel_file=vmlinuz
initrd_file=initrd.img
fdtfile=am335x-boneblack.dtb

# extra kernel cmdline args, set via mmcroot
snappy_cmdline=init=/lib/systemd/systemd ro panic=-1 fixrtc

# boot logic
# either "a" or "b"; target partition we want to boot
snappy_ab=a
# stamp file indicating a new version is being tried; removed by s-i after boot
snappy_stamp=snappy-stamp.txt
# either "regular" (normal boot) or "try" when trying a new version
snappy_mode=regular
# compat
snappy_trial_boot=0
# if we are trying a new version, check if stamp file is already there to revert
# to other version
snappy_boot=if test "${snappy_mode}" = "try"; then if test -e mmc ${bootpart} ${snappy_stamp}; then if test "${snappy_ab}" = "a"; then setenv snappy_ab "b"; else setenv snappy_ab "a"; fi; else fatwrite mmc ${mmcdev}:${mmcpart} 0x0 ${snappy_stamp} 0; fi; fi; run loadfiles; setenv mmcroot /dev/disk/by-label/system-${snappy_ab} ${snappy_cmdline}; run mmcargs; bootz ${loadaddr} ${initrd_addr}:${initrd_size} ${fdtaddr}
`

func (s *PartitionTestSuite) makeFakeUbootEnv(c *C) {
	err := os.MkdirAll(bootloaderUbootDir(), 0755)
	c.Assert(err, IsNil)

	// this file just needs to exist
	err = ioutil.WriteFile(bootloaderUbootConfigFile(), []byte(""), 0644)
	c.Assert(err, IsNil)

	// this file needs specific data
	err = ioutil.WriteFile(bootloaderUbootEnvFile(), []byte(fakeUbootEnvData), 0644)
	c.Assert(err, IsNil)
}

func (s *PartitionTestSuite) TestNewUbootNoUbootReturnsNil(c *C) {
	partition := New()
	u := newUboot(partition)
	c.Assert(u, IsNil)
}

func (s *PartitionTestSuite) TestNewUboot(c *C) {
	s.makeFakeUbootEnv(c)

	partition := New()
	u := newUboot(partition)
	c.Assert(u, NotNil)
	c.Assert(u.Name(), Equals, bootloaderNameUboot)
}

func (s *PartitionTestSuite) TestUbootGetBootVar(c *C) {
	s.makeFakeUbootEnv(c)

	partition := New()
	u := newUboot(partition)

	nextBoot, err := u.GetBootVar(bootloaderRootfsVar)
	c.Assert(err, IsNil)
	// the https://developer.ubuntu.com/en/snappy/porting guide says
	// we always use the short names
	c.Assert(nextBoot, Equals, "a")

	// ensure that nextBootIsOther works too
	c.Assert(partition.IsNextBootOther(), Equals, false)
}

func (s *PartitionTestSuite) TestUbootToggleRootFS(c *C) {
	s.makeFakeUbootEnv(c)

	partition := New()
	u := newUboot(partition)
	c.Assert(u, NotNil)

	err := u.ToggleRootFS("b")
	c.Assert(err, IsNil)

	nextBoot, err := u.GetBootVar(bootloaderRootfsVar)
	c.Assert(err, IsNil)
	c.Assert(nextBoot, Equals, "b")

	// ensure that nextBootIsOther works too
	c.Assert(partition.IsNextBootOther(), Equals, true)
}

func (s *PartitionTestSuite) TestUbootGetEnvVar(c *C) {
	s.makeFakeUbootEnv(c)

	partition := New()
	u := newUboot(partition)
	c.Assert(u, NotNil)

	v, err := u.GetBootVar(bootloaderBootmodeVar)
	c.Assert(err, IsNil)
	c.Assert(v, Equals, "regular")

	v, err = u.GetBootVar(bootloaderRootfsVar)
	c.Assert(err, IsNil)
	c.Assert(v, Equals, "a")
}

func (s *PartitionTestSuite) TestGetBootloaderWithUboot(c *C) {
	s.makeFakeUbootEnv(c)
	p := New()
	bootloader, err := bootloader(p)
	c.Assert(err, IsNil)
	c.Assert(bootloader.Name(), Equals, bootloaderNameUboot)
}

func makeMockAssetsDir(c *C) {
	for _, f := range []string{"assets/vmlinuz", "assets/initrd.img", "assets/dtbs/foo.dtb", "assets/dtbs/bar.dtb"} {
		p := filepath.Join(cacheDir, f)
		os.MkdirAll(filepath.Dir(p), 0755)
		err := ioutil.WriteFile(p, []byte(f), 0644)
		c.Assert(err, IsNil)
	}
}

func (s *PartitionTestSuite) TestHandleAssets(c *C) {
	s.makeFakeUbootEnv(c)
	p := New()
	bootloader, err := bootloader(p)
	c.Assert(err, IsNil)

	// mock the hardwareYaml and the cacheDir
	hardwareSpecFile = makeHardwareYaml(c, "")
	cacheDir = c.MkDir()

	// create mock assets/
	makeMockAssetsDir(c)

	// run the handle assets code
	err = bootloader.HandleAssets()
	c.Assert(err, IsNil)

	// ensure the files are where we expect them
	otherBootPath := bootloader.(*uboot).otherBootPath
	for _, f := range []string{"vmlinuz", "initrd.img", "dtbs/foo.dtb", "dtbs/bar.dtb"} {
		content, err := ioutil.ReadFile(filepath.Join(otherBootPath, f))
		c.Assert(err, IsNil)
		// match content
		c.Assert(strings.HasSuffix(string(content), f), Equals, true)
	}

	// ensure nothing left behind
	c.Assert(helpers.FileExists(filepath.Join(cacheDir, "assets")), Equals, false)
	c.Assert(helpers.FileExists(hardwareSpecFile), Equals, false)
}

func (s *PartitionTestSuite) TestHandleAssetsVerifyBootloader(c *C) {
	s.makeFakeUbootEnv(c)
	p := New()
	bootloader, err := bootloader(p)
	c.Assert(err, IsNil)

	// mock the hardwareYaml and the cacheDir
	hardwareSpecFile = makeHardwareYaml(c, "bootloader: grub")
	cacheDir = c.MkDir()

	err = bootloader.HandleAssets()
	c.Assert(err, NotNil)
}

func (s *PartitionTestSuite) TestHandleAssetsFailVerifyPartitionLayout(c *C) {
	s.makeFakeUbootEnv(c)
	p := New()
	bootloader, err := bootloader(p)
	c.Assert(err, IsNil)

	// mock the hardwareYaml and the cacheDir
	hardwareSpecFile = makeHardwareYaml(c, `
bootloader: u-boot
partition-layout: inplace
`)
	err = bootloader.HandleAssets()
	c.Assert(err, NotNil)
}

func (s *PartitionTestSuite) TestHandleAssetsNoHardwareYaml(c *C) {
	s.makeFakeUbootEnv(c)
	hardwareSpecFile = filepath.Join(c.MkDir(), "non-existent.yaml")

	p := New()
	bootloader, err := bootloader(p)
	c.Assert(err, IsNil)

	c.Assert(bootloader.HandleAssets(), IsNil)
}

func (s *PartitionTestSuite) TestHandleAssetsBadHardwareYaml(c *C) {
	s.makeFakeUbootEnv(c)
	p := New()
	bootloader, err := bootloader(p)
	c.Assert(err, IsNil)

	hardwareSpecFile = makeHardwareYaml(c, `
bootloader u-boot
`)

	c.Assert(bootloader.HandleAssets(), NotNil)
}

func (s *PartitionTestSuite) TestUbootMarkCurrentBootSuccessful(c *C) {
	s.makeFakeUbootEnv(c)

	// To simulate what uboot does for a "try" mode boot, create a
	// stamp file. uboot will expect this file to be removed by
	// "snappy booted" if the system boots successfully. If this
	// file exists when uboot starts, it will know that the previous
	// boot failed, and will therefore toggle to the other rootfs.
	err := ioutil.WriteFile(bootloaderUbootStampFile(), []byte(""), 0640)
	c.Assert(err, IsNil)
	c.Assert(helpers.FileExists(bootloaderUbootStampFile()), Equals, true)

	partition := New()
	u := newUboot(partition)
	c.Assert(u, NotNil)

	// enter "try" mode so that we check to ensure that snappy
	// correctly modifies the snappy_mode variable from "try" to
	// "regular" to denote a good boot.
	err = u.ToggleRootFS("b")
	c.Assert(err, IsNil)

	c.Assert(helpers.FileExists(bootloaderUbootEnvFile()), Equals, true)
	bytes, err := ioutil.ReadFile(bootloaderUbootEnvFile())
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(bytes), "snappy_mode=try"), Equals, true)
	c.Assert(strings.Contains(string(bytes), "snappy_mode=regular"), Equals, false)
	c.Assert(strings.Contains(string(bytes), "snappy_ab=b"), Equals, true)

	err = u.MarkCurrentBootSuccessful("b")
	c.Assert(err, IsNil)

	c.Assert(helpers.FileExists(bootloaderUbootStampFile()), Equals, false)
	c.Assert(helpers.FileExists(bootloaderUbootEnvFile()), Equals, true)

	bytes, err = ioutil.ReadFile(bootloaderUbootEnvFile())
	c.Assert(err, IsNil)
	c.Assert(strings.Contains(string(bytes), "snappy_mode=try"), Equals, false)
	c.Assert(strings.Contains(string(bytes), "snappy_mode=regular"), Equals, true)
	c.Assert(strings.Contains(string(bytes), "snappy_ab=b"), Equals, true)
}

func (s *PartitionTestSuite) TestNoWriteNotNeeded(c *C) {
	s.makeFakeUbootEnv(c)

	atomiCall := false
	atomicWriteFile = func(a string, b []byte, c os.FileMode, f helpers.AtomicWriteFlags) error {
		atomiCall = true
		return helpers.AtomicWriteFile(a, b, c, f)
	}

	partition := New()
	u := newUboot(partition)
	c.Assert(u, NotNil)

	c.Check(u.MarkCurrentBootSuccessful("a"), IsNil)
	c.Assert(atomiCall, Equals, false)
}

func (s *PartitionTestSuite) TestWriteDueToMissingValues(c *C) {
	s.makeFakeUbootEnv(c)

	// this file needs specific data
	c.Assert(ioutil.WriteFile(bootloaderUbootEnvFile(), []byte(""), 0644), IsNil)

	atomiCall := false
	atomicWriteFile = func(a string, b []byte, c os.FileMode, f helpers.AtomicWriteFlags) error {
		atomiCall = true
		return helpers.AtomicWriteFile(a, b, c, f)
	}

	partition := New()
	u := newUboot(partition)
	c.Assert(u, NotNil)

	c.Check(u.MarkCurrentBootSuccessful("a"), IsNil)
	c.Assert(atomiCall, Equals, true)

	bytes, err := ioutil.ReadFile(bootloaderUbootEnvFile())
	c.Assert(err, IsNil)
	c.Check(strings.Contains(string(bytes), "snappy_mode=try"), Equals, false)
	c.Check(strings.Contains(string(bytes), "snappy_mode=regular"), Equals, true)
	c.Check(strings.Contains(string(bytes), "snappy_ab=a"), Equals, true)
}

func (s *PartitionTestSuite) TestUbootMarkCurrentBootSuccessfulFwEnv(c *C) {
	s.makeFakeUbootEnv(c)

	env, err := uenv.Create(bootloaderUbootFwEnvFile(), 4096)
	c.Assert(err, IsNil)
	env.Set("snappy_ab", "b")
	env.Set("snappy_mode", "try")
	env.Set("snappy_trial_boot", "1")
	err = env.Save()
	c.Assert(err, IsNil)

	partition := New()
	u := newUboot(partition)
	c.Assert(u, NotNil)

	err = u.MarkCurrentBootSuccessful("b")
	c.Assert(err, IsNil)

	env, err = uenv.Open(bootloaderUbootFwEnvFile())
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "snappy_ab=b\nsnappy_mode=regular\nsnappy_trial_boot=0\n")
}

func (s *PartitionTestSuite) TestUbootSetEnvNoUselessWrites(c *C) {
	s.makeFakeUbootEnv(c)

	env, err := uenv.Create(bootloaderUbootFwEnvFile(), 4096)
	c.Assert(err, IsNil)
	env.Set("snappy_ab", "b")
	env.Set("snappy_mode", "regular")
	err = env.Save()
	c.Assert(err, IsNil)

	st, err := os.Stat(bootloaderUbootFwEnvFile())
	c.Assert(err, IsNil)
	time.Sleep(100 * time.Millisecond)

	partition := New()
	u := newUboot(partition)
	c.Assert(u, NotNil)

	// note that we set to the same var as above
	err = setBootVarFwEnv(bootloaderRootfsVar, "b")
	c.Assert(err, IsNil)

	env, err = uenv.Open(bootloaderUbootFwEnvFile())
	c.Assert(err, IsNil)
	c.Assert(env.String(), Equals, "snappy_ab=b\nsnappy_mode=regular\n")

	st2, err := os.Stat(bootloaderUbootFwEnvFile())
	c.Assert(err, IsNil)
	c.Assert(st.ModTime(), Equals, st2.ModTime())
}

func (s *PartitionTestSuite) TestUbootSetBootVarLegacy(c *C) {
	s.makeFakeUbootEnv(c)

	partition := New()
	u := newUboot(partition)
	c.Assert(u, NotNil)

	content, err := getBootVarLegacy(bootloaderRootfsVar)
	c.Assert(content, Equals, "a")

	err = setBootVarLegacy(bootloaderRootfsVar, "b")
	c.Assert(err, IsNil)

	content, err = getBootVarLegacy(bootloaderRootfsVar)
	c.Assert(content, Equals, "b")
}

func (s *PartitionTestSuite) TestUbootSetBootVarFwEnv(c *C) {
	s.makeFakeUbootEnv(c)
	env, err := uenv.Create(bootloaderUbootFwEnvFile(), 4096)
	c.Assert(err, IsNil)
	err = env.Save()
	c.Assert(err, IsNil)

	err = setBootVarFwEnv("key", "value")
	c.Assert(err, IsNil)

	partition := New()
	u := newUboot(partition)
	content, err := u.GetBootVar("key")
	c.Assert(err, IsNil)
	c.Assert(content, Equals, "value")
}

func (s *PartitionTestSuite) TestUbootGetBootVarFwEnv(c *C) {
	s.makeFakeUbootEnv(c)
	env, err := uenv.Create(bootloaderUbootFwEnvFile(), 4096)
	c.Assert(err, IsNil)
	env.Set("key2", "value2")
	err = env.Save()
	c.Assert(err, IsNil)

	partition := New()
	u := newUboot(partition)
	content, err := u.GetBootVar("key2")
	c.Assert(err, IsNil)
	c.Assert(content, Equals, "value2")
}
