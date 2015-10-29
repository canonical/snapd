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

	"github.com/ubuntu-core/snappy/partition"
	"github.com/ubuntu-core/snappy/pkg/snapfs"
)

func unpackKernel(s *SnapPart) error {
	blobName := filepath.Base(snapfs.BlobPath(s.basedir))
	dstDir := filepath.Join(partition.BootloaderDir(), blobName)
	if err := os.MkdirAll(dstDir, 0755); err != nil {
		return err
	}
	dir, err := os.Open(dstDir)
	if err != nil {
		return err
	}
	defer dir.Close()
	if s.m.Kernel != "" {
		src := s.m.Kernel
		if err := s.deb.Unpack(src, dstDir); err != nil {
			return err
		}
		src = filepath.Join(dstDir, s.m.Kernel)
		dst := filepath.Join(dstDir, partition.NormalizeKernelInitrdName(s.m.Kernel))
		if err := os.Rename(src, dst); err != nil {
			return err
		}
		if err := dir.Sync(); err != nil {
			return err
		}
	}
	if s.m.Initrd != "" {
		src := s.m.Initrd
		if err := s.deb.Unpack(src, dstDir); err != nil {
			return err
		}
		src = filepath.Join(dstDir, s.m.Initrd)
		dst := filepath.Join(dstDir, partition.NormalizeKernelInitrdName(s.m.Initrd))
		if err := os.Rename(src, dst); err != nil {
			return err
		}
		if err := dir.Sync(); err != nil {
			return err
		}
	}
	if s.m.Dtbs != "" {
		src := s.m.Dtbs
		dst := filepath.Join(dstDir, s.m.Dtbs)
		if err := s.deb.Unpack(src, dst); err != nil {
			return err
		}
	}

	return dir.Sync()
}

func removeKernel(s *SnapPart) error {
	bootdir := partition.BootloaderDir()
	if err := os.MkdirAll(filepath.Join(bootdir, s.Version()), 0755); err !=
		nil {
		return err
	}
	blobName := filepath.Base(snapfs.BlobPath(s.basedir))
	dstDir := filepath.Join(bootdir, blobName)
	return os.RemoveAll(dstDir)
}
