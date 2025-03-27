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

package device

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"github.com/snapcore/snapd/dirs"
	"golang.org/x/sys/unix"
)

func SnapGpioChardevPath(instanceName, plugOrSlot string) string {
	return filepath.Join(dirs.SnapGpioChardevDir, instanceName, plugOrSlot)
}

type GPIOChardev struct {
	Path     string
	Name     string
	Label    string
	NumLines uint
}

func (c *GPIOChardev) String() string {
	return fmt.Sprintf("(name: %s, label: %s, lines: %d)", c.Name, c.Label, c.NumLines)
}

// This has to match the memory layout of `struct gpiochip_info` found
// in /include/uapi/linux/gpio.h in the kernel.
type kernelChipInfo struct {
	name, label [32]byte
	lines       uint32
}

const _GPIO_GET_CHIPINFO_IOCTL uintptr = 0x8044b401

var unixSyscall = unix.Syscall

var ioctlGetChipInfo = func(path string) (*kernelChipInfo, error) {
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

	return &kci, nil
}

func GetGpioChardevChipInfo(path string) (*GPIOChardev, error) {
	kci, err := ioctlGetChipInfo(path)
	if err != nil {
		return nil, err
	}

	chip := &GPIOChardev{
		Path:     path,
		Name:     string(bytes.TrimRight(kci.name[:], "\x00")),
		Label:    string(bytes.TrimRight(kci.label[:], "\x00")),
		NumLines: uint(kci.lines),
	}
	return chip, nil
}
