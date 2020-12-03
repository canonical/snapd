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

package bootloader

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/bootloader/lkenv"
	"github.com/snapcore/snapd/bootloader/ubootenv"
)

// creates a new Androidboot bootloader object
func NewAndroidBoot(rootdir string) Bootloader {
	return newAndroidBoot(rootdir, nil)
}

func MockAndroidBootFile(c *C, rootdir string, mode os.FileMode) {
	f := &androidboot{rootdir: rootdir}
	err := os.MkdirAll(f.dir(), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(f.configFile(), nil, mode)
	c.Assert(err, IsNil)
}

func NewUboot(rootdir string, blOpts *Options) ExtractedRecoveryKernelImageBootloader {
	return newUboot(rootdir, blOpts).(ExtractedRecoveryKernelImageBootloader)
}

func MockUbootFiles(c *C, rootdir string, blOpts *Options) {
	u := &uboot{rootdir: rootdir}
	u.setDefaults()
	u.processBlOpts(blOpts)
	err := os.MkdirAll(u.dir(), 0755)
	c.Assert(err, IsNil)

	// ensure that we have a valid uboot.env too
	env, err := ubootenv.Create(u.envFile(), 4096)
	c.Assert(err, IsNil)
	err = env.Save()
	c.Assert(err, IsNil)
}

func NewGrub(rootdir string, opts *Options) RecoveryAwareBootloader {
	return newGrub(rootdir, opts).(RecoveryAwareBootloader)
}

func MockGrubFiles(c *C, rootdir string) {
	err := os.MkdirAll(filepath.Join(rootdir, "/boot/grub"), 0755)
	c.Assert(err, IsNil)
	err = ioutil.WriteFile(filepath.Join(rootdir, "/boot/grub/grub.cfg"), nil, 0644)
	c.Assert(err, IsNil)
}

func NewLk(rootdir string, opts *Options) Bootloader {
	if opts == nil {
		opts = &Options{}
	}
	return newLk(rootdir, opts)
}

func LkConfigFile(b Bootloader) string {
	lk := b.(*lk)
	return lk.envFile()
}

func UbootConfigFile(b Bootloader) string {
	u := b.(*uboot)
	return u.envFile()
}

func MockLkFiles(c *C, rootdir string, opts *Options) {
	if opts == nil {
		opts = &Options{}
	}
	l := &lk{rootdir: rootdir, inRuntimeMode: !opts.PrepareImageTime}
	err := os.MkdirAll(l.dir(), 0755)
	c.Assert(err, IsNil)

	// first create empty env file
	buf := make([]byte, 4096)
	err = ioutil.WriteFile(l.envFile(), buf, 0660)
	c.Assert(err, IsNil)
	// now write env in it with correct crc
	env := lkenv.NewEnv(l.envFile(), "", lkenv.V1)
	env.InitializeBootPartitions("boot_a", "boot_b")
	err = env.Save()
	c.Assert(err, IsNil)
}

func LkRuntimeMode(b Bootloader) bool {
	lk := b.(*lk)
	return lk.inRuntimeMode
}

func MockAddBootloaderToFind(blConstructor func(string, *Options) Bootloader) (restore func()) {
	oldLen := len(bootloaders)
	bootloaders = append(bootloaders, blConstructor)
	return func() {
		bootloaders = bootloaders[:oldLen]
	}
}

var (
	EditionFromDiskConfigAsset           = editionFromDiskConfigAsset
	EditionFromConfigAsset               = editionFromConfigAsset
	ConfigAssetFrom                      = configAssetFrom
	StaticCommandLineForGrubAssetEdition = staticCommandLineForGrubAssetEdition
)
