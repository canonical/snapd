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

package bootloader

import (
	"path/filepath"

	"github.com/snapcore/snapd/bootloader/ubootenv"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

type uboot struct {
	rootdir string
}

// newUboot create a new Uboot bootloader object
func newUboot(rootdir string) Bootloader {
	u := &uboot{rootdir: rootdir}
	if !osutil.FileExists(u.envFile()) {
		return nil
	}

	return u
}

func (u *uboot) Name() string {
	return "uboot"
}

func (u *uboot) setRootDir(rootdir string) {
	u.rootdir = rootdir
}

func (u *uboot) dir() string {
	if u.rootdir == "" {
		panic("internal error: unset rootdir")
	}
	return filepath.Join(u.rootdir, "/boot/uboot")
}

func (u *uboot) InstallBootConfig(gadgetDir string, opts *Options) (bool, error) {
	gadgetFile := filepath.Join(gadgetDir, u.Name()+".conf")
	systemFile := u.ConfigFile()
	return genericInstallBootConfig(gadgetFile, systemFile)
}

func (u *uboot) ConfigFile() string {
	return u.envFile()
}

func (u *uboot) envFile() string {
	return filepath.Join(u.dir(), "uboot.env")
}

func (u *uboot) SetBootVars(values map[string]string) error {
	env, err := ubootenv.OpenWithFlags(u.envFile(), ubootenv.OpenBestEffort)
	if err != nil {
		return err
	}

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

	env, err := ubootenv.OpenWithFlags(u.envFile(), ubootenv.OpenBestEffort)
	if err != nil {
		return nil, err
	}

	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (u *uboot) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	dstDir := filepath.Join(u.dir(), s.Filename())
	assets := []string{"kernel.img", "initrd.img", "dtbs/*"}
	return extractKernelAssetsToBootDir(dstDir, s, snapf, assets)
}

func (u *uboot) RemoveKernelAssets(s snap.PlaceInfo) error {
	return removeKernelAssetsFromBootDir(u.dir(), s)
}
