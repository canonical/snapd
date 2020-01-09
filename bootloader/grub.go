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

// sanity - grub implements the required interfaces
var (
	_ Bootloader              = (*grub)(nil)
	_ installableBootloader   = (*grub)(nil)
	_ RecoveryAwareBootloader = (*grub)(nil)
)

type grub struct {
	rootdir string

	basedir string
}

// newGrub create a new Grub bootloader object
func newGrub(rootdir string, opts *Options) RecoveryAwareBootloader {
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
		return genericInstallBootConfig(recoveryGrubCfg, systemFile)
	}
	gadgetFile := filepath.Join(gadgetDir, g.Name()+".conf")
	systemFile := filepath.Join(g.rootdir, "/boot/grub/grub.cfg")
	return genericInstallBootConfig(gadgetFile, systemFile)
}

func (g *grub) SetRecoverySystemEnv(recoverySystemDir string, values map[string]string) error {
	if recoverySystemDir == "" {
		return fmt.Errorf("internal error: recoverySystemDir unset")
	}
	recoverySystemGrubEnv := filepath.Join(g.rootdir, recoverySystemDir, "grubenv")
	if err := os.MkdirAll(filepath.Dir(recoverySystemGrubEnv), 0755); err != nil {
		return err
	}
	genv := grubenv.NewEnv(recoverySystemGrubEnv)
	for k, v := range values {
		genv.Set(k, v)
	}
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
