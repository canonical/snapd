// -*- Mode: Go; indent-tabs-mode: t -*-
// Ignore vet for this file to do the unsafe pointer manipulation in the test.
//go:build ignore_vet

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

package main_test

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	main "github.com/snapcore/snapd/cmd/snap-gpio-helper"
	"golang.org/x/sys/unix"
	. "gopkg.in/check.v1"
)

type chipSuite struct{}

var _ = Suite(&chipSuite{})

func (s *chipSuite) TestGpioChardevInfo(c *C) {
	tmpdir := c.MkDir()
	chipPath := filepath.Join(tmpdir, "gpiochip0")
	c.Assert(os.WriteFile(chipPath, nil, 0644), IsNil)

	// This has to match the memory layout of `struct gpiochip_info` found
	// in /include/uapi/linux/gpio.h in the kernel.
	type mockChipInfo struct {
		name, label [32]byte
		lines       uint32
	}

	restore := main.MockUnixSyscall(func(trap, a1, a2, a3 uintptr) (uintptr, uintptr, syscall.Errno) {
		fd, ioctl, ptr := a1, a2, a3
		// Validate syscall
		c.Check(trap, Equals, uintptr(unix.SYS_IOCTL))
		// Validate path for passed fd
		path, err := os.Readlink(fmt.Sprintf("/proc/self/fd/%d", fd))
		c.Assert(err, IsNil)
		c.Check(path, Equals, chipPath)
		// Validate ioctl
		c.Check(ioctl, Equals, uintptr(0x8044b401))
		// Mock returned chip info
		mockInfo := mockChipInfo{lines: 12}
		// Null-terminated name and label
		copy(mockInfo.name[:], "gpiochip0\x00")
		copy(mockInfo.label[:], "label-0\x00")
		*(*mockChipInfo)(unsafe.Pointer(ptr)) = mockInfo
		return 0, 0, 0
	})
	defer restore()

	chip, err := main.GetChipInfo(chipPath)
	c.Assert(err, IsNil)
	c.Check(chip.Path(), Equals, chipPath)
	c.Check(chip.Name(), Equals, "gpiochip0")
	c.Check(chip.Label(), Equals, "label-0")
	c.Check(chip.Label(), Equals, "label-0")
	c.Check(chip.NumLines(), Equals, uint(12))
}
