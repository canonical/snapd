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
	"fmt"
	"os"
	"path/filepath"

	"github.com/snapcore/snapd/bootloader/grubenv"
	"github.com/snapcore/snapd/osutil"
	"github.com/snapcore/snapd/snap"
)

type grub struct {
	rootdir string

	basedir string
}

// newGrub create a new Grub bootloader object
func newGrub(rootdir string, opts *Options) Bootloader {
	g := &grub{rootdir: rootdir}
	if opts != nil && opts.Recovery {
		g.basedir = "/EFI/ubuntu"
	} else {
		g.basedir = "/boot/grub"
	}
	if !osutil.FileExists(g.ConfigFile()) {
		return nil
	}

	return g
}

func (g *grub) Name() string {
	return "grub"
}

func (g *grub) setRootDir(rootdir string) {
	g.rootdir = rootdir
}

func (g *grub) dir() string {
	if g.rootdir == "" {
		panic("internal error: unset rootdir")
	}
	return filepath.Join(g.rootdir, g.basedir)
}

func (g *grub) InstallBootConfig(gadgetDir string, opts *Options) (bool, error) {
	if opts != nil && opts.Recovery {
		recoveryGrubCfg := filepath.Join(gadgetDir, g.Name()+"-recovery.conf")
		systemFile := filepath.Join(g.rootdir, "/EFI/ubuntu/grub.cfg")
		ok, err := genericInstallBootConfig(recoveryGrubCfg, systemFile)
		if !ok {
			return false, nil
		}
		if err != nil {
			return true, err
		}
		return true, g.setRecoveryKernel(opts.RecoverySystem, opts.RecoveryKernel)
	}
	gadgetFile := filepath.Join(gadgetDir, g.Name()+".conf")
	systemFile := filepath.Join(g.rootdir, "/boot/grub/grub.cfg")
	return genericInstallBootConfig(gadgetFile, systemFile)
}

func (g *grub) setRecoveryKernel(recoverySystem, kernel string) error {
	if recoverySystem == "" {
		return fmt.Errorf("internal error: cannot use setRecoveryKernel without a recovery system")
	}
	if kernel == "" {
		return fmt.Errorf("internal error: cannot use setRecoveryKernel without a kernel")
	}
	recoverySystemGrubEnv := filepath.Join(g.rootdir, "systems", recoverySystem, "grubenv")
	if err := os.MkdirAll(filepath.Dir(recoverySystemGrubEnv), 0755); err != nil {
		return err
	}
	genv := grubenv.NewEnv(recoverySystemGrubEnv)
	genv.Set("snapd_recovery_kernel", kernel)
	return genv.Save()
}

func (g *grub) ConfigFile() string {
	return filepath.Join(g.dir(), "grub.cfg")
}

func (g *grub) envFile() string {
	return filepath.Join(g.dir(), "grubenv")
}

func (g *grub) GetBootVars(names ...string) (map[string]string, error) {
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

func (g *grub) SetBootVars(values map[string]string) error {
	env := grubenv.NewEnv(g.envFile())
	if err := env.Load(); err != nil && !os.IsNotExist(err) {
		return err
	}
	for k, v := range values {
		env.Set(k, v)
	}
	return env.Save()
}

func (g *grub) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	// XXX: should we use "kernel.yaml" for this?
	if _, err := snapf.ReadFile("meta/force-kernel-extraction"); err == nil {
		return extractKernelAssetsToBootDir(g.dir(), s, snapf)
	}
	return nil
}

func (g *grub) RemoveKernelAssets(s snap.PlaceInfo) error {
	return removeKernelAssetsFromBootDir(g.dir(), s)
}
