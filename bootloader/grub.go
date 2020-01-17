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
	if opts != nil && (opts.Recovery || opts.NoSlashBoot) {
		g.basedir = "EFI/ubuntu"
	} else {
		g.basedir = "boot/grub"
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
	// use a relative symlink destination so that it resolves properly, if grub
	// is located at /run/mnt/ubuntu-boot or /boot/grub, etc.
	extractedKernel := filepath.Join(
		filepath.Base(s.MountFile()),
		"kernel.efi",
	)

	// check that the kernel snap has been extracted already so we don't
	// inadvertently create a dangling symlink
	// expand the relative symlink from g.dir()
	if !osutil.FileExists(filepath.Join(g.dir(), extractedKernel)) {
		return fmt.Errorf(
			"cannot enable %s at %s: %v",
			name,
			extractedKernel,
			os.ErrNotExist,
		)
	}

	return os.Symlink(
		extractedKernel,
		filepath.Join(g.dir(), name),
	)
}

// unlinkKernelEfiSymlink will remove the specified symlink if it exists. Note
// that if the symlink is "dangling", it will still remove the symlink without
// returning an error. This is useful for example to disable a try-kernel that
// was incorrectly created.
func (g *grub) unlinkKernelEfiSymlink(name string) error {
	symlink := filepath.Join(g.dir(), name)
	// check that the symlink exists
	if _, err := os.Lstat(symlink); err == nil {
		// symlink exists, remove it unconditionally
		return os.Remove(symlink)
	}
	return nil
}

func (g *grub) readKernelSymlink(name string) (snap.PlaceInfo, error) {
	// read the symlink from <grub-dir>/<name> to
	// <grub-dir>/<snap-file-name>/<name> and parse the
	// directory (which is supposed to be the name of the snap) into the snap
	link := filepath.Join(g.dir(), name)

	// check that the symlink is not dangling before continuing
	if !osutil.FileExists(link) {
		return nil, fmt.Errorf("cannot read dangling symlink %s", name)
	}

	targetKernelEfi, err := os.Readlink(link)
	if err != nil {
		return nil, fmt.Errorf("cannot read %s symlink: %v", link, err)
	}

	kernelSnapFileName := filepath.Base(filepath.Dir(targetKernelEfi))
	sn, err := snap.ParsePlaceInfoFromSnapFileName(kernelSnapFileName)
	if err != nil {
		return nil, fmt.Errorf(
			"cannot parse kernel snap file name from symlink target %q: %v",
			kernelSnapFileName,
			err,
		)
	}
	return sn, nil
}

// actual ExtractedRunKernelImageBootloader methods

// EnableKernel will install a kernel.efi symlink in the bootloader partition,
// pointing to the referenced kernel snap. EnableKernel() will fail if the
// referenced kernel snap does not exist.
func (g *grub) EnableKernel(s snap.PlaceInfo) error {
	// add symlink from ubuntuBootPartition/kernel.efi to
	// <ubuntu-boot>/EFI/ubuntu/<snap-name>.snap/kernel.efi
	// so that we are consistent between uc16/uc18 and uc20 with where we
	// extract kernels
	return g.makeKernelEfiSymlink(s, "kernel.efi")
}

// EnableTryKernel will install a try-kernel.efi symlink in the bootloader
// partition, pointing towards the referenced kernel snap. EnableTryKernel()
// will fail if the referenced kernel snap does not exist.
func (g *grub) EnableTryKernel(s snap.PlaceInfo) error {
	// add symlink from ubuntuBootPartition/kernel.efi to
	// <ubuntu-boot>/EFI/ubuntu/<snap-name>.snap/kernel.efi
	// so that we are consistent between uc16/uc18 and uc20 with where we
	// extract kernels
	return g.makeKernelEfiSymlink(s, "try-kernel.efi")
}

// DisableTryKernel will remove the try-kernel.efi symlink if it exists. Note
// that when performing an update, you should probably first use EnableKernel(),
// then DisableTryKernel() for maximum safety.
func (g *grub) DisableTryKernel() error {
	return g.unlinkKernelEfiSymlink("try-kernel.efi")
}

// Kernel will return the kernel snap currently installed in the bootloader
// partition, pointed to by the kernel.efi symlink.
func (g *grub) Kernel() (snap.PlaceInfo, error) {
	return g.readKernelSymlink("kernel.efi")
}

// TryKernel will return the kernel snap currently being tried if it exists and
// false if there is not currently a try-kernel.efi symlink. Note if the symlink
// exists but does not point to an existing file an error will be returned.
func (g *grub) TryKernel() (snap.PlaceInfo, bool, error) {
	// check that the _symlink_ exists, not that it points to something real
	// we check for whether it is a dangling symlink inside readKernelSymlink,
	// which returns an error when the symlink is dangling
	_, err := os.Lstat(filepath.Join(g.dir(), "try-kernel.efi"))
	if err == nil {
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
