// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package bootloadertest

import (
	"path/filepath"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/snap"
)

// MockBootloader mocks the bootloader interface and records all
// set/get calls.
type MockBootloader struct {
	BootVars map[string]string
	SetErr   error
	GetErr   error

	name    string
	bootdir string

	ExtractKernelAssetsCalls []snap.PlaceInfo
	RemoveKernelAssetsCalls  []snap.PlaceInfo
}

// ensure MockBootloader implements the Bootloader interface
var _ bootloader.Bootloader = (*MockBootloader)(nil)

func Mock(name, bootdir string) *MockBootloader {
	return &MockBootloader{
		name:    name,
		bootdir: bootdir,

		BootVars: make(map[string]string),
	}
}

func (b *MockBootloader) SetBootVars(values map[string]string) error {
	for k, v := range values {
		b.BootVars[k] = v
	}
	return b.SetErr
}

func (b *MockBootloader) GetBootVars(keys ...string) (map[string]string, error) {
	out := map[string]string{}
	for _, k := range keys {
		out[k] = b.BootVars[k]
	}

	return out, b.GetErr
}

func (b *MockBootloader) Name() string {
	return b.name
}

func (b *MockBootloader) ConfigFile() string {
	return filepath.Join(b.bootdir, "mockboot/mockboot.cfg")
}

func (b *MockBootloader) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	b.ExtractKernelAssetsCalls = append(b.ExtractKernelAssetsCalls, s)
	return nil
}

func (b *MockBootloader) RemoveKernelAssets(s snap.PlaceInfo) error {
	b.RemoveKernelAssetsCalls = append(b.RemoveKernelAssetsCalls, s)
	return nil
}

// SetBootKernel sets the current boot kernel string. Should be
// something like "pc-kernel_1234.snap".
func (b *MockBootloader) SetBootKernel(kernel string) {
	b.SetBootVars(map[string]string{"snap_kernel": kernel})
}

// SetBootBase sets the current boot base string. Should be something
// like "core_1234.snap".
func (b *MockBootloader) SetBootBase(base string) {
	b.SetBootVars(map[string]string{"snap_core": base})
}

func (b *MockBootloader) SetTrying() {
	b.BootVars["snap_mode"] = "trying"
}

func (b *MockBootloader) SetRollbackAccrossReboot() {
	b.BootVars["snap_mode"] = ""
	b.BootVars["snap_try_core"] = ""
	b.BootVars["snap_try_kernel"] = ""
}
