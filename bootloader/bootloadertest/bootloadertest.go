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
	"fmt"
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

	InstallBootConfigCalled []string
	InstallBootConfigResult bool
	InstallBootConfigErr    error

	RecoverySystemDir      string
	RecoverySystemBootVars map[string]string
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

func (b *MockBootloader) SetTryingDuringReboot() error {
	if b.BootVars["snap_mode"] != "try" {
		return fmt.Errorf("bootloader must be in 'try' mode")
	}
	b.BootVars["snap_mode"] = "trying"
	return nil
}

// SetRollbackAcrossReboot will simulate a rollback across reboots. This
// means that the bootloader had "snap_try_{core,kernel}" set but this
// boot failed. In this case the bootloader will clear
// "snap_try_{core,kernel}" and "snap_mode" which means the "old" kernel,core
// in "snap_{core,kernel}" will be used.
func (b *MockBootloader) SetRollbackAcrossReboot() error {
	if b.BootVars["snap_mode"] != "try" {
		return fmt.Errorf("rollback can only be simulated in 'try' mode")
	}
	if b.BootVars["snap_core"] == "" && b.BootVars["snap_kernel"] == "" {
		return fmt.Errorf("rollback can only be simulated if either snap_core or snap_kernel is set")
	}
	// clean try bootvars and snap_mode
	b.BootVars["snap_mode"] = ""
	b.BootVars["snap_try_core"] = ""
	b.BootVars["snap_try_kernel"] = ""
	return nil
}

func (b *MockBootloader) InstallBootConfig(gadgetDir string, opts *bootloader.Options) (bool, error) {
	b.InstallBootConfigCalled = append(b.InstallBootConfigCalled, gadgetDir)
	return b.InstallBootConfigResult, b.InstallBootConfigErr
}

func (b *MockBootloader) SetRecoverySystemEnv(recoverySystemDir string, blVars map[string]string) error {
	if recoverySystemDir == "" {
		panic("MockBootloader.SetRecoverySystemEnv called without recoverySystemDir")
	}
	b.RecoverySystemDir = recoverySystemDir
	b.RecoverySystemBootVars = blVars
	return nil
}
