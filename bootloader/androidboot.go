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
	"os"
	"path/filepath"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/bootloader/androidbootenv"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

type androidboot struct {
	rootdir string
}

// newAndroidboot creates a new Androidboot bootloader object
func newAndroidBoot(rootdir string, _ *Options) Bootloader {
	a := &androidboot{rootdir: rootdir}
	return a
}

func (a *androidboot) Name() string {
	return "androidboot"
}

func (a *androidboot) dir() string {
	if a.rootdir == "" {
		panic("internal error: unset rootdir")
	}
	return filepath.Join(a.rootdir, "/boot/androidboot")
}

func (a *androidboot) InstallBootConfig(gadgetDir string, opts *Options) error {
	gadgetFile := filepath.Join(gadgetDir, a.Name()+".conf")
	systemFile := a.configFile()
	return genericInstallBootConfig(gadgetFile, systemFile)
}

func (a *androidboot) Present() (bool, error) {
	return osutil.FileExists(a.configFile()), nil
}

func (a *androidboot) configFile() string {
	return filepath.Join(a.dir(), "androidboot.env")
}

func (a *androidboot) GetBootVars(names ...string) (map[string]string, error) {
	env := androidbootenv.NewEnv(a.configFile())
	mylog.Check(env.Load())

	out := make(map[string]string, len(names))
	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (a *androidboot) SetBootVars(values map[string]string) error {
	env := androidbootenv.NewEnv(a.configFile())
	if mylog.Check(env.Load()); err != nil && !os.IsNotExist(err) {
		return err
	}
	for k, v := range values {
		env.Set(k, v)
	}
	return env.Save()
}

func (a *androidboot) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	return nil
}

func (a *androidboot) RemoveKernelAssets(s snap.PlaceInfo) error {
	return nil
}
