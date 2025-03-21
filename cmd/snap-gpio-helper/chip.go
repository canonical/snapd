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

package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/snapcore/snapd/dirs"
	"golang.org/x/sys/unix"
)

type GPIOChardev interface {
	Name() string
	Path() string
	Label() string
	NumLines() uint
}

// This has to match the memory layout of `struct gpiochip_info` found
// in /include/uapi/linux/gpio.h in the kernel.
type kernelChipInfo struct {
	name, label [32]byte
	lines       uint32
}

type chipInfo struct {
	kernelChipInfo

	path string
}

func (c *chipInfo) Name() string {
	// remove terminating null character
	return string(bytes.TrimRight(c.name[:], "\x00"))
}

func (c *chipInfo) Label() string {
	// remove terminating null character
	return string(bytes.TrimRight(c.label[:], "\x00"))
}

func (c *chipInfo) NumLines() uint {
	return uint(c.lines)
}

func (c *chipInfo) Path() string {
	return c.path
}

func (c *chipInfo) String() string {
	return fmt.Sprintf("(name: %s, label: %s, lines: %d)", c.Name(), c.Label(), c.lines)
}

const _GPIO_GET_CHIPINFO_IOCTL uintptr = 0x8044b401

var unixSyscall = unix.Syscall

var getChipInfo = func(path string) (GPIOChardev, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var kci kernelChipInfo
	_, _, errno := unixSyscall(unix.SYS_IOCTL, f.Fd(), _GPIO_GET_CHIPINFO_IOCTL, uintptr(unsafe.Pointer(&kci)))
	if errno != 0 {
		return nil, errno
	}

	chip := &chipInfo{
		path:           path,
		kernelChipInfo: kci,
	}
	return chip, nil
}

func findChips(filter func(chip GPIOChardev) bool) ([]GPIOChardev, error) {
	allPaths, err := filepath.Glob(filepath.Join(dirs.DevDir, "/gpiochip*"))
	if err != nil {
		return nil, err
	}

	var matched []GPIOChardev
	for _, path := range allPaths {
		chip, err := getChipInfo(path)
		if err != nil {
			return nil, err
		}
		if filter(chip) {
			matched = append(matched, chip)
		}
	}

	return matched, nil
}
