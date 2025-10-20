// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package osutil

import (
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

func MemfdSecret() (fd int, err error) {
	return unix.MemfdSecret(0)
}

type MemfdSecretBuffer struct {
	fd   int
	data []byte
}

// NewMemfdSecretBuffer initializes a MemfdSecretBuffer.
func NewMemfdSecretBuffer(fd int) (mem *MemfdSecretBuffer, err error) {
	if fd == -1 {
		fd, err = unix.MemfdSecret(0)
		if err != nil {
			return nil, err
		}
	}

	finfo, err := os.Stat(fmt.Sprintf("/proc/self/fd/%d", fd))
	if err != nil {
		return nil, err
	}

	if finfo.Size() == 0 {
		return &MemfdSecretBuffer{fd: fd, data: nil}, nil
	}

	data, err := unix.Mmap(fd, 0, int(finfo.Size()), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return nil, err
	}

	return &MemfdSecretBuffer{fd: fd, data: data}, nil
}

func (mem *MemfdSecretBuffer) Bytes() []byte {
	return mem.data
}

func (mem *MemfdSecretBuffer) Truncate(n int) error {
	// delete the old mapping
	if err := unix.Munmap(mem.data); err != nil {
		return err
	}

	if err := unix.Ftruncate(mem.fd, int64(n)); err != nil {
		return err
	}

	data, err := unix.Mmap(mem.fd, 0, n, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return err
	}

	mem.data = data
	return nil
}

func (mem *MemfdSecretBuffer) Len() int {
	return len(mem.data)
}

func (mem *MemfdSecretBuffer) Fd() int {
	return mem.fd
}
