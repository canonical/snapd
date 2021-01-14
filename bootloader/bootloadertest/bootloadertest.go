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
	"strings"

	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/snap"
)

// MockBootloader mocks the bootloader interface and records all
// set/get calls.
type MockBootloader struct {
	MockedPresent bool
	PresentErr    error

	BootVars         map[string]string
	SetBootVarsCalls int
	SetErr           error
	GetErr           error

	name    string
	bootdir string

	ExtractKernelAssetsCalls []snap.PlaceInfo
	RemoveKernelAssetsCalls  []snap.PlaceInfo

	InstallBootConfigCalled []string
	InstallBootConfigErr    error

	enabledKernel    snap.PlaceInfo
	enabledTryKernel snap.PlaceInfo

	panicMethods map[string]bool
}

// ensure MockBootloader(s) implement the Bootloader interface
var _ bootloader.Bootloader = (*MockBootloader)(nil)
var _ bootloader.RecoveryAwareBootloader = (*MockRecoveryAwareBootloader)(nil)
var _ bootloader.TrustedAssetsBootloader = (*MockTrustedAssetsBootloader)(nil)
var _ bootloader.ExtractedRunKernelImageBootloader = (*MockExtractedRunKernelImageBootloader)(nil)
var _ bootloader.ExtractedRecoveryKernelImageBootloader = (*MockExtractedRecoveryKernelImageBootloader)(nil)

func Mock(name, bootdir string) *MockBootloader {
	return &MockBootloader{
		name:    name,
		bootdir: bootdir,

		BootVars: make(map[string]string),

		panicMethods: make(map[string]bool),
	}
}

func (b *MockBootloader) maybePanic(which string) {
	if b.panicMethods[which] {
		panic(fmt.Sprintf("mocked reboot panic in %s", which))
	}
}

func (b *MockBootloader) SetBootVars(values map[string]string) error {
	b.maybePanic("SetBootVars")
	b.SetBootVarsCalls++
	for k, v := range values {
		b.BootVars[k] = v
	}
	return b.SetErr
}

func (b *MockBootloader) GetBootVars(keys ...string) (map[string]string, error) {
	b.maybePanic("GetBootVars")

	out := map[string]string{}
	for _, k := range keys {
		out[k] = b.BootVars[k]
	}

	return out, b.GetErr
}

func (b *MockBootloader) Name() string {
	return b.name
}

func (b *MockBootloader) Present() (bool, error) {
	return b.MockedPresent, b.PresentErr
}

func (b *MockBootloader) ExtractKernelAssets(s snap.PlaceInfo, snapf snap.Container) error {
	b.ExtractKernelAssetsCalls = append(b.ExtractKernelAssetsCalls, s)
	return nil
}

func (b *MockBootloader) RemoveKernelAssets(s snap.PlaceInfo) error {
	b.RemoveKernelAssetsCalls = append(b.RemoveKernelAssetsCalls, s)
	return nil
}

func (b *MockBootloader) SetEnabledKernel(s snap.PlaceInfo) (restore func()) {
	oldSn := b.enabledTryKernel
	oldVar := b.BootVars["snap_kernel"]
	b.enabledKernel = s
	b.BootVars["snap_kernel"] = s.Filename()
	return func() {
		b.BootVars["snap_kernel"] = oldVar
		b.enabledKernel = oldSn
	}
}

func (b *MockBootloader) SetEnabledTryKernel(s snap.PlaceInfo) (restore func()) {
	oldSn := b.enabledTryKernel
	oldVar := b.BootVars["snap_try_kernel"]
	b.enabledTryKernel = s
	b.BootVars["snap_try_kernel"] = s.Filename()
	return func() {
		b.BootVars["snap_try_kernel"] = oldVar
		b.enabledTryKernel = oldSn
	}
}

// InstallBootConfig installs the boot config in the gadget directory to the
// mock bootloader's root directory.
func (b *MockBootloader) InstallBootConfig(gadgetDir string, opts *bootloader.Options) error {
	b.InstallBootConfigCalled = append(b.InstallBootConfigCalled, gadgetDir)
	return b.InstallBootConfigErr
}

// MockRecoveryAwareBootloader mocks a bootloader implementing the
// RecoveryAware interface.
type MockRecoveryAwareBootloader struct {
	*MockBootloader

	RecoverySystemDir      string
	RecoverySystemBootVars map[string]string
}

type ExtractedRecoveryKernelCall struct {
	RecoverySystemDir string
	S                 snap.PlaceInfo
}

// MockExtractedRecoveryKernelImageBootloader mocks a bootloader implementing
// the ExtractedRecoveryKernelImage interface.
type MockExtractedRecoveryKernelImageBootloader struct {
	*MockBootloader

	ExtractRecoveryKernelAssetsCalls []ExtractedRecoveryKernelCall
}

// ExtractedRecoveryKernelImage derives a MockRecoveryAwareBootloader from a base
// MockBootloader.
func (b *MockBootloader) ExtractedRecoveryKernelImage() *MockExtractedRecoveryKernelImageBootloader {
	return &MockExtractedRecoveryKernelImageBootloader{MockBootloader: b}
}

// ExtractRecoveryKernelAssets extracts the kernel assets for the provided
// kernel snap into the specified recovery system dir; part of
// RecoveryAwareBootloader.
func (b *MockExtractedRecoveryKernelImageBootloader) ExtractRecoveryKernelAssets(recoverySystemDir string, s snap.PlaceInfo, snapf snap.Container) error {
	if recoverySystemDir == "" {
		panic("MockBootloader.ExtractRecoveryKernelAssets called without recoverySystemDir")
	}

	b.ExtractRecoveryKernelAssetsCalls = append(
		b.ExtractRecoveryKernelAssetsCalls,
		ExtractedRecoveryKernelCall{
			S:                 s,
			RecoverySystemDir: recoverySystemDir},
	)
	return nil
}

// RecoveryAware derives a MockRecoveryAwareBootloader from a base
// MockBootloader.
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

// GetRecoverySystemEnv gets the recovery system environment bootloader
// variables; part of RecoveryAwareBootloader.
func (b *MockRecoveryAwareBootloader) GetRecoverySystemEnv(recoverySystemDir, key string) (string, error) {
	if recoverySystemDir == "" {
		panic("MockBootloader.GetRecoverySystemEnv called without recoverySystemDir")
	}
	b.RecoverySystemDir = recoverySystemDir
	return b.RecoverySystemBootVars[key], nil
}

// MockExtractedRunKernelImageBootloader mocks a bootloader
// implementing the ExtractedRunKernelImageBootloader interface.
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

// WithExtractedRunKernelImage derives a MockExtractedRunKernelImageBootloader
// from a base MockBootloader.
func (b *MockBootloader) WithExtractedRunKernelImage() *MockExtractedRunKernelImageBootloader {
	return &MockExtractedRunKernelImageBootloader{
		MockBootloader: b,

		runKernelImageMockedErrs:     make(map[string]error),
		runKernelImageMockedNumCalls: make(map[string]int),
	}
}

// SetEnabledKernel sets the current kernel "symlink" as returned
// by Kernel(); returns' a restore function to set it back to what it was
// before.
func (b *MockExtractedRunKernelImageBootloader) SetEnabledKernel(kernel snap.PlaceInfo) (restore func()) {
	old := b.runKernelImageEnabledKernel
	b.runKernelImageEnabledKernel = kernel
	return func() {
		b.runKernelImageEnabledKernel = old
	}
}

// SetEnabledTryKernel sets the current try-kernel "symlink" as
// returned by TryKernel(). If set to nil, TryKernel()'s second return value
// will be false; returns' a restore function to set it back to what it was
// before.
func (b *MockExtractedRunKernelImageBootloader) SetEnabledTryKernel(kernel snap.PlaceInfo) (restore func()) {
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

// SetRunKernelImagePanic allows setting any method in the
// ExtractedRunKernelImageBootloader interface on
// MockExtractedRunKernelImageBootloader to panic instead of
// returning. This allows one to test what would happen if the system
// was rebooted during execution of a particular
// function. Specifically, the panic will be done immediately entering
// the function so setting SetBootVars to panic will emulate a reboot
// before any boot vars are set persistently
func (b *MockExtractedRunKernelImageBootloader) SetRunKernelImagePanic(f string) (restore func()) {
	switch f {
	case "EnableKernel", "EnableTryKernel", "Kernel", "TryKernel", "DisableTryKernel", "SetBootVars", "GetBootVars":
		old := b.panicMethods[f]
		b.panicMethods[f] = true
		return func() {
			b.panicMethods[f] = old
		}
	default:
		panic(fmt.Sprintf("unknown ExtractedRunKernelImageBootloader method %q to mock reboot via panic for", f))
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
	b.maybePanic("EnableKernel")
	b.runKernelImageEnableKernelCalls = append(b.runKernelImageEnableKernelCalls, s)
	b.runKernelImageEnabledKernel = s
	return b.runKernelImageMockedErrs["EnableKernel"]
}

// EnableTryKernel enables a try-kernel; part of
// ExtractedRunKernelImageBootloader.
func (b *MockExtractedRunKernelImageBootloader) EnableTryKernel(s snap.PlaceInfo) error {
	b.maybePanic("EnableTryKernel")
	b.runKernelImageEnableTryKernelCalls = append(b.runKernelImageEnableTryKernelCalls, s)
	b.runKernelImageEnabledTryKernel = s
	return b.runKernelImageMockedErrs["EnableTryKernel"]
}

// Kernel returns the current kernel set in the bootloader; part of
// ExtractedRunKernelImageBootloader.
func (b *MockExtractedRunKernelImageBootloader) Kernel() (snap.PlaceInfo, error) {
	b.maybePanic("Kernel")
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
	b.maybePanic("TryKernel")
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
	b.maybePanic("DisableTryKernel")
	b.runKernelImageMockedNumCalls["DisableTryKernel"]++
	b.runKernelImageEnabledTryKernel = nil
	return b.runKernelImageMockedErrs["DisableTryKernel"]
}

// MockTrustedAssetsBootloader mocks a bootloader implementing the
// bootloader.TrustedAssetsBootloader interface.
type MockTrustedAssetsBootloader struct {
	*MockBootloader

	TrustedAssetsList  []string
	TrustedAssetsErr   error
	TrustedAssetsCalls int

	RecoveryBootChainList []bootloader.BootFile
	RecoveryBootChainErr  error
	BootChainList         []bootloader.BootFile
	BootChainErr          error

	RecoveryBootChainCalls []string
	BootChainRunBl         []bootloader.Bootloader
	BootChainKernelPath    []string

	UpdateErr                  error
	UpdateCalls                int
	Updated                    bool
	ManagedAssetsList          []string
	StaticCommandLine          string
	CandidateStaticCommandLine string
	CommandLineErr             error
}

func (b *MockBootloader) WithTrustedAssets() *MockTrustedAssetsBootloader {
	return &MockTrustedAssetsBootloader{
		MockBootloader: b,
	}
}

func (b *MockTrustedAssetsBootloader) ManagedAssets() []string {
	return b.ManagedAssetsList
}

func (b *MockTrustedAssetsBootloader) UpdateBootConfig() (bool, error) {
	b.UpdateCalls++
	return b.Updated, b.UpdateErr
}

func glueCommandLine(modeArg, systemArg, staticArgs, extraArgs string) string {
	args := []string(nil)
	for _, argSet := range []string{modeArg, systemArg, staticArgs, extraArgs} {
		if argSet != "" {
			args = append(args, argSet)
		}
	}
	line := strings.Join(args, " ")
	return strings.TrimSpace(line)
}

func (b *MockTrustedAssetsBootloader) CommandLine(modeArg, systemArg, extraArgs string) (string, error) {
	if b.CommandLineErr != nil {
		return "", b.CommandLineErr
	}
	return glueCommandLine(modeArg, systemArg, b.StaticCommandLine, extraArgs), nil
}

func (b *MockTrustedAssetsBootloader) CandidateCommandLine(modeArg, systemArg, extraArgs string) (string, error) {
	if b.CommandLineErr != nil {
		return "", b.CommandLineErr
	}
	return glueCommandLine(modeArg, systemArg, b.CandidateStaticCommandLine, extraArgs), nil
}

func (b *MockTrustedAssetsBootloader) TrustedAssets() ([]string, error) {
	b.TrustedAssetsCalls++
	return b.TrustedAssetsList, b.TrustedAssetsErr
}

func (b *MockTrustedAssetsBootloader) RecoveryBootChain(kernelPath string) ([]bootloader.BootFile, error) {
	b.RecoveryBootChainCalls = append(b.RecoveryBootChainCalls, kernelPath)
	return b.RecoveryBootChainList, b.RecoveryBootChainErr
}

func (b *MockTrustedAssetsBootloader) BootChain(runBl bootloader.Bootloader, kernelPath string) ([]bootloader.BootFile, error) {
	b.BootChainRunBl = append(b.BootChainRunBl, runBl)
	b.BootChainKernelPath = append(b.BootChainKernelPath, kernelPath)
	return b.BootChainList, b.BootChainErr
}
