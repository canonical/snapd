// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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
}

// ensure MockBootloader(s) implement the Bootloader interfaceces
var _ bootloader.Bootloader = (*MockBootloader)(nil)
var _ bootloader.ExtractedRunKernelImageBootloader = (*MockExtractedRunKernelImageBootloader)(nil)
var _ bootloader.RecoveryAwareBootloader = (*MockRecoveryAwareBootloader)(nil)

func Mock(name, bootdir string) *MockBootloader {
	return &MockBootloader{
		name:    name,
		bootdir: bootdir,

		BootVars: make(map[string]string),
	}
}

func (b *MockBootloader) SetBootVars(values map[string]string) error {
	b.SetBootVarsCalls++
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

// InstallBootConfig installs the boot config in the gadget directory to the
// mock bootloader's root directory.
func (b *MockBootloader) InstallBootConfig(gadgetDir string, opts *bootloader.Options) (bool, error) {
	b.InstallBootConfigCalled = append(b.InstallBootConfigCalled, gadgetDir)
	return b.InstallBootConfigResult, b.InstallBootConfigErr
}

// XXX ...
type MockRecoveryAwareBootloader struct {
	*MockBootloader

	RecoverySystemDir      string
	RecoverySystemBootVars map[string]string
}

// XXX ...
func (b *MockBootloader) RecoveryAware() *MockRecoveryAwareBootloader {
	return &MockRecoveryAwareBootloader{MockBootloader: b}
}

// SetRecoverySystemEnv sets the recovery system environment bootloader
// variables; part of RecoveryAwareBootloader.
func (b *MockRecoveryAwareBootloader) SetRecoverySystemEnv(recoverySystemDir string, blVars map[string]string) error {
	if recoverySystemDir == "" {
		panic("MockBootloader.SetRecoverySystemEnv called without recoverySystemDir")
	}
	b.RecoverySystemDir = recoverySystemDir
	b.RecoverySystemBootVars = blVars
	return nil
}

// XXX ...
type MockExtractedRunKernelImageBootloader struct {
	*MockBootloader

	runKernelImageEnableKernelCalls     []snap.PlaceInfo
	runKernelImageEnableTryKernelCalls  []snap.PlaceInfo
	runKernelImageDisableTryKernelCalls []snap.PlaceInfo
	runKernelImageEnabledKernel         snap.PlaceInfo
	runKernelImageEnabledTryKernel      snap.PlaceInfo

	runKernelImageMockedErrs     map[string]error
	runKernelImageMockedNumCalls map[string]int
}

// XXX ...
func (b *MockBootloader) WithExtractedRunKernelImage() *MockExtractedRunKernelImageBootloader {
	return &MockExtractedRunKernelImageBootloader{
		MockBootloader: b,

		runKernelImageMockedErrs:     make(map[string]error),
		runKernelImageMockedNumCalls: make(map[string]int),
	}
}

// SetRunKernelImageEnabledKernel sets the current kernel "symlink" as returned
// by Kernel(); returns' a restore function to set it back to what it was
// before.
func (b *MockExtractedRunKernelImageBootloader) SetRunKernelImageEnabledKernel(kernel snap.PlaceInfo) (restore func()) {
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
func (b *MockExtractedRunKernelImageBootloader) SetRunKernelImageEnabledTryKernel(kernel snap.PlaceInfo) (restore func()) {
	old := b.runKernelImageEnabledTryKernel
	b.runKernelImageEnabledTryKernel = kernel
	return func() {
		b.runKernelImageEnabledTryKernel = old
	}
}

// SetRunKernelImageFunctionError allows setting an error to be returned for the
// specified function; it returns a restore function to set it back to what it
// was before.
func (b *MockExtractedRunKernelImageBootloader) SetRunKernelImageFunctionError(f string, err error) (restore func()) {
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

// GetRunKernelImageFunctionSnapCalls returns which snaps were specified during
// execution, in order of calls, as well as the number of calls for methods that
// don't take a snap to set.
func (b *MockExtractedRunKernelImageBootloader) GetRunKernelImageFunctionSnapCalls(f string) ([]snap.PlaceInfo, int) {
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
func (b *MockExtractedRunKernelImageBootloader) EnableKernel(s snap.PlaceInfo) error {
	b.runKernelImageEnableKernelCalls = append(b.runKernelImageEnableKernelCalls, s)
	b.runKernelImageEnabledKernel = s
	return b.runKernelImageMockedErrs["EnableKernel"]
}

// EnableTryKernel enables a try-kernel; part of
// ExtractedRunKernelImageBootloader.
func (b *MockExtractedRunKernelImageBootloader) EnableTryKernel(s snap.PlaceInfo) error {
	b.runKernelImageEnableTryKernelCalls = append(b.runKernelImageEnableTryKernelCalls, s)
	b.runKernelImageEnabledTryKernel = s
	return b.runKernelImageMockedErrs["EnableTryKernel"]
}

// Kernel returns the current kernel set in the bootloader; part of
// ExtractedRunKernelImageBootloader.
func (b *MockExtractedRunKernelImageBootloader) Kernel() (snap.PlaceInfo, error) {
	b.runKernelImageMockedNumCalls["Kernel"]++
	err := b.runKernelImageMockedErrs["Kernel"]
	if err != nil {
		return nil, err
	}
	return b.runKernelImageEnabledKernel, nil
}

// TryKernel returns the current kernel set in the bootloader; part of
// ExtractedRunKernelImageBootloader.
func (b *MockExtractedRunKernelImageBootloader) TryKernel() (snap.PlaceInfo, error) {
	b.runKernelImageMockedNumCalls["TryKernel"]++
	err := b.runKernelImageMockedErrs["TryKernel"]
	if err != nil {
		return nil, err
	}
	if b.runKernelImageEnabledTryKernel == nil {
		return nil, bootloader.ErrNoTryKernelRef
	}
	return b.runKernelImageEnabledTryKernel, nil
}

// DisableTryKernel removes the current try-kernel "symlink" set in the
// bootloader; part of ExtractedRunKernelImageBootloader.
func (b *MockExtractedRunKernelImageBootloader) DisableTryKernel() error {
	b.runKernelImageMockedNumCalls["DisableTryKernel"]++
	return b.runKernelImageMockedErrs["DisableTryKernel"]
}
