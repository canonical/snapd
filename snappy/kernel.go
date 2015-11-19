package snappy

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

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/ubuntu-core/snappy/partition"
	"github.com/ubuntu-core/snappy/pkg"
	"github.com/ubuntu-core/snappy/pkg/squashfs"
	"github.com/ubuntu-core/snappy/progress"
)

// noramlizeAssetName transforms like "vmlinuz-4.1.0" -> "vmlinuz"
func normalizeKernelInitrdName(name string) string {
	name = filepath.Base(name)
	return strings.SplitN(name, "-", 2)[0]
}

func removeKernelAssets(s *SnapPart, inter interacter) error {
	if s.m.Type != pkg.TypeKernel {
		return nil
	}

	// remove the kernel blob
	blobName := filepath.Base(squashfs.BlobPath(s.basedir))
	dstDir := filepath.Join(partition.BootloaderDir(), blobName)
	if err := os.RemoveAll(dstDir); err != nil {
		return err
	}

	return nil
}

func extractKernelAssets(s *SnapPart, inter progress.Meter, flags InstallFlags) (name string, err error) {
	if s.m.Type != pkg.TypeKernel {
		return "", nil
	}

	// now do the kernel specific bits
	blobName := filepath.Base(squashfs.BlobPath(s.basedir))
	dstDir := filepath.Join(partition.BootloaderDir(), blobName)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return "", err
	}
	dir, err := os.Open(dstDir)
	if err != nil {
		return "", err
	}
	defer dir.Close()

	if s.m.Kernel != "" {
		src := s.m.Kernel
		if err := s.deb.Unpack(src, dstDir); err != nil {
			return name, err
		}
		src = filepath.Join(dstDir, s.m.Kernel)
		dst := filepath.Join(dstDir, normalizeKernelInitrdName(s.m.Kernel))
		if err := os.Rename(src, dst); err != nil {
			return name, err
		}
		if err := dir.Sync(); err != nil {
			return "", err
		}
	}
	if s.m.Initrd != "" {
		src := s.m.Initrd
		if err := s.deb.Unpack(src, dstDir); err != nil {
			return name, err
		}
		src = filepath.Join(dstDir, s.m.Initrd)
		dst := filepath.Join(dstDir, normalizeKernelInitrdName(s.m.Initrd))
		if err := os.Rename(src, dst); err != nil {
			return name, err
		}
		if err := dir.Sync(); err != nil {
			return "", err
		}
	}
	if s.m.Dtbs != "" {
		src := filepath.Join(s.m.Dtbs, "*")
		dst := dstDir
		if err := s.deb.Unpack(src, dst); err != nil {
			return name, err
		}
	}

	return name, dir.Sync()
}

var setBootVar = partition.SetBootVar

func setNextBoot(s *SnapPart) error {
	if s.m.Type != pkg.TypeOS && s.m.Type != pkg.TypeKernel {
		return nil
	}
	var bootvar string
	switch s.m.Type {
	case pkg.TypeOS:
		bootvar = "snappy_os"
	case pkg.TypeKernel:
		bootvar = "snappy_kernel"
	}
	blobName := filepath.Base(squashfs.BlobPath(s.basedir))
	if err := setBootVar(bootvar, blobName); err != nil {
		return err
	}

	if err := setBootVar("snappy_mode", "try"); err != nil {
		return err
	}

	return nil
}
