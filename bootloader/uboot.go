// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2022 Canonical Ltd
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
	"fmt"
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

// uboot implements the required interfaces
var (
	_ Bootloader                             = (*uboot)(nil)
	_ ExtractedRecoveryKernelImageBootloader = (*uboot)(nil)
)

type uboot struct {
	rootdir string
	basedir string

	ubootEnvFileName string
}

func (u *uboot) setDefaults() {
	u.basedir = "/boot/uboot/"
	u.ubootEnvFileName = "uboot.env"
}

func (u *uboot) processBlOpts(blOpts *Options) {
	if blOpts != nil {
		switch {
		case blOpts.Role == RoleRecovery || blOpts.NoSlashBoot:
			// RoleRecovery or NoSlashBoot imply we use
			// the "boot.sel" simple text format file in
			// /uboot/ubuntu as it exists on the partition
			// directly
			u.basedir = "/uboot/ubuntu/"
			fallthrough
		case blOpts.Role == RoleRunMode:
			// if RoleRunMode (and no NoSlashBoot), we
			// expect to find /boot/uboot/boot.sel
			u.ubootEnvFileName = "boot.sel"
		}
	}
}

// newUboot create a new Uboot bootloader object
func newUboot(rootdir string, blOpts *Options) Bootloader {
	u := &uboot{
		rootdir: rootdir,
	}
	u.setDefaults()
	u.processBlOpts(blOpts)

	return u
}

func (u *uboot) Name() string {
	return "uboot"
}

func (u *uboot) dir() string {
	if u.rootdir == "" {
		panic("internal error: unset rootdir")
	}
	return filepath.Join(u.rootdir, u.basedir)
}

func (u *uboot) useHeaderFlagByte(gadgetDir string) bool {
	// if there is a "pattern" boot.sel in the gadget snap, we follow its
	// lead. If opening it as a uboot env fails in any way we just go with
	// the default.
	gadgetEnv := mylog.Check2(ubootenv.OpenWithFlags(filepath.Join(gadgetDir, u.ubootEnvFileName), ubootenv.OpenBestEffort))
	if err == nil {
		return gadgetEnv.HeaderFlagByte()
	}

	// Otherwise we use the (historical) default and assume uboot is built with
	// SYS_REDUNDAND_ENVIRONMENT=y
	return true
}

func (u *uboot) InstallBootConfig(gadgetDir string, blOpts *Options) error {
	gadgetFile := filepath.Join(gadgetDir, u.Name()+".conf")
	// if the gadget file is empty, then we don't install anything
	// this is because there are some gadgets, namely the 20 pi gadget right
	// now, that don't use a uboot.env to boot and instead use a boot.scr, and
	// installing a uboot.env file of any form in the root directory will break
	// the boot.scr, so for these setups we just don't install anything
	// TODO:UC20: how can we do this better? maybe parse the file to get the
	//            actual format?
	st := mylog.Check2(os.Stat(gadgetFile))

	if st.Size() == 0 {
		// we have an empty uboot.conf, and hence a uboot bootloader in the
		// gadget, but nothing to copy in this case and instead just install our
		// own boot.sel file
		u.processBlOpts(blOpts)
		mylog.Check(os.MkdirAll(filepath.Dir(u.envFile()), 0755))

		// TODO:UC20: what's a reasonable size for this file?
		env := mylog.Check2(ubootenv.Create(u.envFile(), 4096, ubootenv.CreateOptions{HeaderFlagByte: u.useHeaderFlagByte(gadgetDir)}))
		mylog.Check(env.Save())

		return nil
	}

	// InstallBootConfig gets called on a uboot that does not come from newUboot
	// so we need to apply the defaults here
	u.setDefaults()

	if blOpts != nil && blOpts.Role == RoleRecovery {
		// not supported yet, this is traditional uboot.env from gadget
		// TODO:UC20: support this use-case
		return fmt.Errorf("non-empty uboot.env not supported on UC20+ yet")
	}

	systemFile := u.envFile()
	return genericInstallBootConfig(gadgetFile, systemFile)
}

func (u *uboot) Present() (bool, error) {
	return osutil.FileExists(u.envFile()), nil
}

func (u *uboot) envFile() string {
	return filepath.Join(u.dir(), u.ubootEnvFileName)
}

func (u *uboot) SetBootVars(values map[string]string) error {
	env := mylog.Check2(ubootenv.OpenWithFlags(u.envFile(), ubootenv.OpenBestEffort))

	dirty := false
	for k, v := range values {
		// already set to the right value, nothing to do
		if env.Get(k) == v {
			continue
		}
		env.Set(k, v)
		dirty = true
	}

	if dirty {
		return env.Save()
	}

	return nil
}

func (u *uboot) GetBootVars(names ...string) (map[string]string, error) {
	out := map[string]string{}

	env := mylog.Check2(ubootenv.OpenWithFlags(u.envFile(), ubootenv.OpenBestEffort))

	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (u *uboot) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	dstDir := filepath.Join(u.dir(), s.Filename())
	assets := []string{"kernel.img", "initrd.img", "dtbs/*"}
	return extractKernelAssetsToBootDir(dstDir, snapf, assets)
}

func (u *uboot) ExtractRecoveryKernelAssets(recoverySystemDir string, s snap.PlaceInfo, snapf snap.Container) error {
	if recoverySystemDir == "" {
		return fmt.Errorf("internal error: recoverySystemDir unset")
	}

	recoverySystemUbootKernelAssetsDir := filepath.Join(u.rootdir, recoverySystemDir, "kernel")
	assets := []string{"kernel.img", "initrd.img", "dtbs/*"}
	return extractKernelAssetsToBootDir(recoverySystemUbootKernelAssetsDir, snapf, assets)
}

func (u *uboot) RemoveKernelAssets(s snap.PlaceInfo) error {
	return removeKernelAssetsFromBootDir(u.dir(), s)
}
