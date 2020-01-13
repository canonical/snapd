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
	_ Bootloader                        = (*grub)(nil)
	_ installableBootloader             = (*grub)(nil)
	_ RecoveryAwareBootloader           = (*grub)(nil)
	_ ExtractedRunKernelImageBootloader = (*grub)(nil)
)

type grub struct {
	rootdir string

	basedir string

	uefiRunKernelExtraction bool
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
	if opts != nil {
		g.uefiRunKernelExtraction = opts.ExtractedRunKernelImage
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

func (g *grub) extractedKernelDir(prefix string, s snap.PlaceInfo) string {
	return filepath.Join(
		prefix,
		filepath.Base(s.MountFile()),
	)
}

func (g *grub) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	// default kernel assets are:
	// - kernel.img
	// - initrd.img
	// - dtbs/*
	var assets []string
	if g.uefiRunKernelExtraction {
		assets = []string{"kernel.efi"}
	} else {
		assets = []string{"kernel.img", "initrd.img", "dtbs/*"}
	}

	// extraction can be forced through either a special file in the kernel snap
	// or through an option in the bootloader
	_, err := snapf.ReadFile("meta/force-kernel-extraction")
	if g.uefiRunKernelExtraction || err == nil {
		return extractKernelAssetsToBootDir(
			g.extractedKernelDir(g.dir(), s),
			s,
			snapf,
			assets,
		)
	}
	return nil
}

func (g *grub) RemoveKernelAssets(s snap.PlaceInfo) error {
	return removeKernelAssetsFromBootDir(g.dir(), s)
}

// ExtractedRunKernelImageBootloader helper methods

func (g *grub) makeKernelEfiSymlink(s snap.PlaceInfo, name string) error {
	return os.Symlink(
		// don't use g.dir() because we don't want the rootdir in the symlink
		// target, we want EFI/ubuntu always, even if grub is really working
		// from /run/mnt/ubuntu-boot/EFI/ubuntu, etc.
		filepath.Join(g.extractedKernelDir(g.basedir, s), "kernel.efi"),
		filepath.Join(g.rootdir, name),
	)
}

func (g *grub) unlinkKernelEfiSymlink(name string) error {
	symlink := filepath.Join(g.rootdir, name)
	if osutil.FileExists(symlink) {
		return osutil.UnlinkMany(g.rootdir, []string{name})
	}

	// return more helpful error if the symlink doesn't exist
	return fmt.Errorf("cannot disable kernel, symlink %s missing", symlink)
}

func (g *grub) readKernelSymlink(name string) (snap.PlaceInfo, error) {
	// read the symlink from <grub-root-dir>/<name> to
	// <grub-root-dir>/EFI/ubuntu/<snap-name>.snap/<name> and parse the
	// directory (which is supposed to be the name of the snap)
	targetKernelEfi, err := os.Readlink(filepath.Join(g.rootdir, "kernel.efi"))
	if err != nil {
		return nil, fmt.Errorf("couldn't read kernel.efi symlink: %v", err)
	}
	kernelSnapFileName := filepath.Base(filepath.Dir(targetKernelEfi))
	sn, err := snap.ParsePlaceInfoFromSnapFileName(kernelSnapFileName)
	if err != nil {
		return nil, fmt.Errorf(
			"bad kernel snap file path at %q, unable to parse into snap file name: %v",
			kernelSnapFileName,
			err,
		)
	}
	return sn, nil
}

// actual ExtractedRunKernelImageBootloader methods

func (g *grub) EnableKernel(s snap.PlaceInfo) error {
	// add symlink from ubuntuBootPartition/kernel.efi to
	// <ubuntu-boot>/EFI/ubuntu/<snap-name>.snap/kernel.efi
	// so that we are consistent between uc16/uc18 and uc20 with where we
	// extract kernels
	return g.makeKernelEfiSymlink(s, "kernel.efi")
}

func (g *grub) EnableTryKernel(s snap.PlaceInfo) error {
	// add symlink from ubuntuBootPartition/kernel.efi to
	// <ubuntu-boot>/EFI/ubuntu/<snap-name>.snap/kernel.efi
	// so that we are consistent between uc16/uc18 and uc20 with where we
	// extract kernels
	return g.makeKernelEfiSymlink(s, "try-kernel.efi")
}

func (g *grub) DisableTryKernel() error {
	return g.unlinkKernelEfiSymlink("try-kernel.efi")
}

func (g *grub) Kernel() (snap.PlaceInfo, error) {
	return g.readKernelSymlink("kernel.efi")
}

func (g *grub) TryKernel() (snap.PlaceInfo, bool, error) {
	// try to read the symlink from ubuntuBootPartition/try-kernel.efi
	if osutil.FileExists(filepath.Join(g.rootdir, "try-kernel.efi")) {
		p, err := g.readKernelSymlink("try-kernel.efi")
		// if we failed to read the symlink, then the try kernel isn't usable,
		// so return err because the symlink is there
		if err != nil {
			return nil, false, err
		}
		return p, true, nil
	}
	return nil, false, nil
}
