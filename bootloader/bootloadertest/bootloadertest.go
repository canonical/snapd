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
	BootVars         map[string]string
	SetBootVarsCalls int
	SetErr           error
	GetErr           error

	name    string
	bootdir string

	ExtractKernelAssetsCalls []snap.PlaceInfo
	RemoveKernelAssetsCalls  []snap.PlaceInfo

	InstallBootConfigCalled []string
	InstallBootConfigResult bool
	InstallBootConfigErr    error

	RecoverySystemDir      string
	RecoverySystemBootVars map[string]string

	panicMethods map[string]bool

	runKernelImageEnableKernelCalls     []snap.PlaceInfo
	runKernelImageEnableTryKernelCalls  []snap.PlaceInfo
	runKernelImageDisableTryKernelCalls []snap.PlaceInfo
	runKernelImageEnabledKernel         snap.PlaceInfo
	runKernelImageEnabledTryKernel      snap.PlaceInfo

	runKernelImageMockedErrs     map[string]error
	runKernelImageMockedNumCalls map[string]int
}

// ensure MockBootloader implements the Bootloader interface
var _ bootloader.Bootloader = (*MockBootloader)(nil)

func Mock(name, bootdir string) *MockBootloader {
	return &MockBootloader{
		name:    name,
		bootdir: bootdir,

		BootVars: make(map[string]string),

		panicMethods:                 make(map[string]bool),
		runKernelImageMockedErrs:     make(map[string]error),
		runKernelImageMockedNumCalls: make(map[string]int),
	}
}

func (b *MockBootloader) SetBootVars(values map[string]string) error {
	if b.panicMethods["SetBootVars"] {
		panic("mocked reboot panic in SetBootVars")
	}
	b.SetBootVarsCalls++
	for k, v := range values {
		b.BootVars[k] = v
	}
	return b.SetErr
}

func (b *MockBootloader) GetBootVars(keys ...string) (map[string]string, error) {
	if b.panicMethods["GetBootVars"] {
		panic("mocked reboot panic in GetBootVars")
	}

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

// InstallBootConfig installs the boot config in the gadget directory to the
// mock bootloader's root directory.
func (b *MockBootloader) InstallBootConfig(gadgetDir string, opts *bootloader.Options) (bool, error) {
	b.InstallBootConfigCalled = append(b.InstallBootConfigCalled, gadgetDir)
	return b.InstallBootConfigResult, b.InstallBootConfigErr
}

// SetRecoverySystemEnv sets the recovery system environment bootloader
// variables; part of RecoveryAwareBootloader.
func (b *MockBootloader) SetRecoverySystemEnv(recoverySystemDir string, blVars map[string]string) error {
	if recoverySystemDir == "" {
		panic("MockBootloader.SetRecoverySystemEnv called without recoverySystemDir")
	}
	b.RecoverySystemDir = recoverySystemDir
	b.RecoverySystemBootVars = blVars
	return nil
}

// SetRunKernelImageEnabledKernel sets the current kernel "symlink" as returned
// by Kernel(); returns' a restore function to set it back to what it was
// before.
func (b *MockBootloader) SetRunKernelImageEnabledKernel(kernel snap.PlaceInfo) (restore func()) {
	old := b.runKernelImageEnabledKernel
	b.runKernelImageEnabledKernel = kernel
	return func() {
		b.runKernelImageEnabledKernel = old
	}
}

// SetRunKernelImageEnabledTryKernel sets the current try-kernel "symlink" as
// returned by TryKernel(). If set to nil, TryKernel()'s second return value
// will be false; returns' a restore function to set it back to what it was
// before.
func (b *MockBootloader) SetRunKernelImageEnabledTryKernel(kernel snap.PlaceInfo) (restore func()) {
	old := b.runKernelImageEnabledTryKernel
	b.runKernelImageEnabledTryKernel = kernel
	return func() {
		b.runKernelImageEnabledTryKernel = old
	}
}

// SetRunKernelImageFunctionError allows setting an error to be returned for the
// specified function; it returns a restore function to set it back to what it
// was before.
func (b *MockBootloader) SetRunKernelImageFunctionError(f string, err error) (restore func()) {
	// check the function
	switch f {
	case "EnableKernel", "EnableTryKernel", "Kernel", "TryKernel", "DisableTryKernel":
		old := b.runKernelImageMockedErrs[f]
		b.runKernelImageMockedErrs[f] = err
		return func() {
			b.runKernelImageMockedErrs[f] = old
		}
	default:
		panic(fmt.Sprintf("unknown ExtractedRunKernelImageBootloader method %q to mock error for", f))
	}
}

// SetRunKernelImagePanic allows setting any method in the Bootloader interface
// on MockBootloader to panic instead of returning. This allows one to test what
// would happen if the system was rebooted during execution of a particular
// function. Specifically, the panic will be done as late as possible in the
// method so setting SetBootVars to panic will panic _after_ setting the boot
// vars persistently.
func (b *MockBootloader) SetRunKernelImagePanic(f string) (restore func()) {
	switch f {
	case "EnableKernel", "EnableTryKernel", "Kernel", "TryKernel", "DisableTryKernel", "SetBootVars", "GetBootVars":
		old := b.panicMethods[f]
		b.panicMethods[f] = true
		return func() {
			b.panicMethods[f] = old
		}
	default:
		panic(fmt.Sprintf("unknown ExtractedRunKernelImageBootloader method %q to mock error for", f))
	}
}

// GetRunKernelImageFunctionSnapCalls returns which snaps were specified during
// execution, in order of calls, as well as the number of calls for methods that
// don't take a snap to set.
func (b *MockBootloader) GetRunKernelImageFunctionSnapCalls(f string) ([]snap.PlaceInfo, int) {
	switch f {
	case "EnableKernel":
		l := b.runKernelImageEnableKernelCalls
		return l, len(l)
	case "EnableTryKernel":
		l := b.runKernelImageEnableTryKernelCalls
		return l, len(l)
	case "Kernel", "TryKernel", "DisableTryKernel":
		return nil, b.runKernelImageMockedNumCalls[f]
	default:
		panic(fmt.Sprintf("unknown ExtractedRunKernelImageBootloader method %q to return snap args for", f))
	}
}

// EnableKernel enables the kernel; part of ExtractedRunKernelImageBootloader.
func (b *MockBootloader) EnableKernel(s snap.PlaceInfo) error {
	if b.panicMethods["EnableKernel"] {
		panic("mocked reboot panic in EnableKernel")
	}
	b.runKernelImageEnableKernelCalls = append(b.runKernelImageEnableKernelCalls, s)

	b.runKernelImageEnabledKernel = s
	return b.runKernelImageMockedErrs["EnableKernel"]
}

// EnableTryKernel enables a try-kernel; part of
// ExtractedRunKernelImageBootloader.
func (b *MockBootloader) EnableTryKernel(s snap.PlaceInfo) error {
	if b.panicMethods["EnableTryKernel"] {
		panic("mocked reboot panic in EnableTryKernel")
	}
	b.runKernelImageEnableTryKernelCalls = append(b.runKernelImageEnableTryKernelCalls, s)
	b.runKernelImageEnabledTryKernel = s
	return b.runKernelImageMockedErrs["EnableTryKernel"]
}

// Kernel returns the current kernel set in the bootloader; part of
// ExtractedRunKernelImageBootloader.
func (b *MockBootloader) Kernel() (snap.PlaceInfo, error) {
	if b.panicMethods["Kernel"] {
		panic("mocked reboot panic in Kernel")
	}
	b.runKernelImageMockedNumCalls["Kernel"]++
	err := b.runKernelImageMockedErrs["Kernel"]
	if err != nil {
		return nil, err
	}
	return b.runKernelImageEnabledKernel, nil
}

// TryKernel returns the current kernel set in the bootloader; part of
// ExtractedRunKernelImageBootloader.
func (b *MockBootloader) TryKernel() (snap.PlaceInfo, bool, error) {
	if b.panicMethods["TryKernel"] {
		panic("mocked reboot panic in TryKernel")
	}
	b.runKernelImageMockedNumCalls["TryKernel"]++
	err := b.runKernelImageMockedErrs["TryKernel"]
	if err != nil {
		return nil, false, err
	}
	if b.runKernelImageEnabledTryKernel == nil {
		return nil, false, nil
	}
	return b.runKernelImageEnabledTryKernel, true, nil
}

// DisableTryKernel removes the current try-kernel "symlink" set in the
// bootloader; part of ExtractedRunKernelImageBootloader.
func (b *MockBootloader) DisableTryKernel() error {
	if b.panicMethods["DisableTryKernel"] {
		panic("mocked reboot panic in DisableTryKernel")
	}
	b.runKernelImageMockedNumCalls["DisableTryKernel"]++
	return b.runKernelImageMockedErrs["DisableTryKernel"]
}
