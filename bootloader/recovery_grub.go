// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

type recoveryGrub struct {
	rootdir string
}

// newRecoveryGrub create a new recovery Grub bootloader object
func newRecoveryGrub(rootdir string) Bootloader {
	g := &recoveryGrub{rootdir: rootdir}
	if !osutil.FileExists(g.ConfigFile()) {
		return nil
	}

	return g
}

func (g *recoveryGrub) Name() string {
	return "grub-recovery"
}

func (g *recoveryGrub) setRootDir(rootdir string) {
	g.rootdir = rootdir
}

func (g *recoveryGrub) dir() string {
	if g.rootdir == "" {
		panic("internal error: unset rootdir")
	}
	return filepath.Join(g.rootdir, "EFI/ubuntu")
}

func (g *recoveryGrub) ConfigFile() string {
	return filepath.Join(g.dir(), "grub.cfg")
}

func (g *recoveryGrub) envFile() string {
	return filepath.Join(g.dir(), "grubenv")
}

func (g *recoveryGrub) GetBootVars(names ...string) (map[string]string, error) {
	out := make(map[string]string)

	env := grubenv.NewEnv(g.envFile())
	if err := env.Load(); err != nil {
		return nil, err
	}

	for _, name := range names {
		out[name] = env.Get(name)
	}

	return out, nil
}

func (g *recoveryGrub) SetBootVars(values map[string]string) error {
	env := grubenv.NewEnv(g.envFile())
	if err := env.Load(); err != nil && !os.IsNotExist(err) {
		return err
	}
	for k, v := range values {
		env.Set(k, v)
	}
	return env.Save()
}

func (g *recoveryGrub) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	return nil
}

func (g *recoveryGrub) RemoveKernelAssets(s snap.PlaceInfo) error {
	return nil
}
