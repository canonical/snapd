// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2019 Canonical Ltd
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

package boot

import (
	"fmt"
	"sync/atomic"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/kernel/fde"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

func NewCoreBootParticipant(s snap.PlaceInfo, t snap.Type, dev snap.Device) *coreBootParticipant {
	bs, err := bootStateFor(t, dev)
	if err != nil {
		panic(err)
	}
	return &coreBootParticipant{s: s, bs: bs}
}

func NewCoreKernel(s snap.PlaceInfo, d snap.Device) *coreKernel {
	return &coreKernel{s, bootloaderOptionsForDeviceKernel(d)}
}

type Trivial = trivial

func (m *Modeenv) WasRead() bool {
	return m.read
}

func (m *Modeenv) DeepEqual(m2 *Modeenv) bool {
	return m.deepEqual(m2)
}

var (
	ModeenvKnownKeys = modeenvKnownKeys

	MarshalModeenvEntryTo        = marshalModeenvEntryTo
	UnmarshalModeenvValueFromCfg = unmarshalModeenvValueFromCfg

	NewTrustedAssetsCache = newTrustedAssetsCache

	ObserveSuccessfulBootWithAssets = observeSuccessfulBootAssets
	SealKeyToModeenv                = sealKeyToModeenvImpl
	ResealKeyToModeenv              = resealKeyToModeenv
	RecoveryBootChainsForSystems    = recoveryBootChainsForSystems
	SealKeyModelParams              = sealKeyModelParams

	BootVarsForTrustedCommandLineFromGadget = bootVarsForTrustedCommandLineFromGadget

	WriteModelToUbuntuBoot = writeModelToUbuntuBoot
)

type BootAssetsMap = bootAssetsMap
type BootCommandLines = bootCommandLines
type TrackedAsset = trackedAsset

func (t *TrackedAsset) Equals(blName, name, hash string) error {
	equal := t.hash == hash &&
		t.name == name &&
		t.blName == blName
	if !equal {
		return fmt.Errorf("not equal to bootloader %q tracked asset %v:%v", t.blName, t.name, t.hash)
	}
	return nil
}

func (o *TrustedAssetsInstallObserver) CurrentTrustedBootAssetsMap() BootAssetsMap {
	return o.currentTrustedBootAssetsMap()
}

func (o *TrustedAssetsInstallObserver) CurrentTrustedRecoveryBootAssetsMap() BootAssetsMap {
	return o.currentTrustedRecoveryBootAssetsMap()
}

func (o *TrustedAssetsInstallObserver) CurrentDataEncryptionKey() keys.EncryptionKey {
	return o.dataEncryptionKey
}

func (o *TrustedAssetsInstallObserver) CurrentSaveEncryptionKey() keys.EncryptionKey {
	return o.saveEncryptionKey
}

func MockSecbootProvisionTPM(f func(mode secboot.TPMProvisionMode, lockoutAuthFile string) error) (restore func()) {
	restore = testutil.Backup(&secbootProvisionTPM)
	secbootProvisionTPM = f
	return restore
}

func MockSecbootSealKeys(f func(keys []secboot.SealKeyRequest, params *secboot.SealKeysParams) error) (restore func()) {
	old := secbootSealKeys
	secbootSealKeys = f
	return func() {
		secbootSealKeys = old
	}
}

func MockSecbootSealKeysWithFDESetupHook(f func(runHook fde.RunSetupHookFunc, keys []secboot.SealKeyRequest, params *secboot.SealKeysWithFDESetupHookParams) error) (restore func()) {
	old := secbootSealKeysWithFDESetupHook
	secbootSealKeysWithFDESetupHook = f
	return func() {
		secbootSealKeysWithFDESetupHook = old
	}
}

func MockSeedReadSystemEssential(f func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error)) (restore func()) {
	old := seedReadSystemEssential
	seedReadSystemEssential = f
	return func() {
		seedReadSystemEssential = old
	}
}

func MockSecbootPCRHandleOfSealedKey(f func(p string) (uint32, error)) (restore func()) {
	restore = testutil.Backup(&secbootPCRHandleOfSealedKey)
	secbootPCRHandleOfSealedKey = f
	return restore
}

func MockSecbootReleasePCRResourceHandles(f func(handles ...uint32) error) (restore func()) {
	restore = testutil.Backup(&secbootReleasePCRResourceHandles)
	secbootReleasePCRResourceHandles = f
	return restore
}

func (o *TrustedAssetsUpdateObserver) InjectChangedAsset(blName, assetName, hash string, recovery bool) {
	ta := &trackedAsset{
		blName: blName,
		name:   assetName,
		hash:   hash,
	}
	if !recovery {
		o.changedAssets = append(o.changedAssets, ta)
	} else {
		o.seedChangedAssets = append(o.seedChangedAssets, ta)
	}
}

type BootAsset = bootAsset
type BootChain = bootChain
type PredictableBootChains = predictableBootChains

const (
	BootChainEquivalent   = bootChainEquivalent
	BootChainDifferent    = bootChainDifferent
	BootChainUnrevisioned = bootChainUnrevisioned
)

var (
	ToPredictableBootAsset              = toPredictableBootAsset
	ToPredictableBootChain              = toPredictableBootChain
	ToPredictableBootChains             = toPredictableBootChains
	PredictableBootChainsEqualForReseal = predictableBootChainsEqualForReseal
	BootAssetsToLoadChains              = bootAssetsToLoadChains
	BootAssetLess                       = bootAssetLess
	WriteBootChains                     = writeBootChains
	ReadBootChains                      = readBootChains
	IsResealNeeded                      = isResealNeeded

	SetImageBootFlags = setImageBootFlags
	NextBootFlags     = nextBootFlags
	SetNextBootFlags  = setNextBootFlags

	ModelUniqueID = modelUniqueID
)

func SetBootFlagsInBootloader(flags []string, rootDir string) error {
	blVars := make(map[string]string, 1)

	if err := setImageBootFlags(flags, blVars); err != nil {
		return err
	}

	// now find the recovery bootloader in the system dir and set the value on
	// it
	opts := &bootloader.Options{
		Role: bootloader.RoleRecovery,
	}
	bl, err := bootloader.Find(rootDir, opts)
	if err != nil {
		return err
	}

	return bl.SetBootVars(blVars)
}

func (b *bootChain) SecbootModelForSealing() secboot.ModelForSealing {
	return b.modelForSealing()
}

func (b *bootChain) SetKernelBootFile(kbf bootloader.BootFile) {
	b.kernelBootFile = kbf
}

func (b *bootChain) KernelBootFile() bootloader.BootFile {
	return b.kernelBootFile
}

func MockRebootArgsPath(argsPath string) (restore func()) {
	oldRebootArgsPath := rebootArgsPath
	rebootArgsPath = argsPath
	return func() { rebootArgsPath = oldRebootArgsPath }
}

func MockBootloaderFind(f func(rootdir string, opts *bootloader.Options) (bootloader.Bootloader, error)) (restore func()) {
	r := testutil.Backup(&bootloaderFind)
	bootloaderFind = f
	return r
}

func MockHasFDESetupHook(f func(*snap.Info) (bool, error)) (restore func()) {
	oldHasFDESetupHook := HasFDESetupHook
	HasFDESetupHook = f
	return func() {
		HasFDESetupHook = oldHasFDESetupHook
	}
}

func MockRunFDESetupHook(f fde.RunSetupHookFunc) (restore func()) {
	oldRunFDESetupHook := RunFDESetupHook
	RunFDESetupHook = f
	return func() { RunFDESetupHook = oldRunFDESetupHook }
}

func MockResealKeyToModeenvUsingFDESetupHook(f func(string, *Modeenv, bool, bool) error) (restore func()) {
	old := resealKeyToModeenvUsingFDESetupHook
	resealKeyToModeenvUsingFDESetupHook = f
	return func() {
		resealKeyToModeenvUsingFDESetupHook = old
	}
}

func MockModeenvLocked() (restore func()) {
	atomic.AddInt32(&modeenvLocked, 1)
	return func() {
		atomic.AddInt32(&modeenvLocked, -1)
	}
}

func MockAdditionalBootFlags(bootFlags []string) (restore func()) {
	old := understoodBootFlags
	understoodBootFlags = append(understoodBootFlags, bootFlags...)
	return func() {
		understoodBootFlags = old
	}
}

func MockWriteModelToUbuntuBoot(mock func(*asserts.Model) error) (restore func()) {
	old := writeModelToUbuntuBoot
	writeModelToUbuntuBoot = mock
	return func() {
		writeModelToUbuntuBoot = old
	}
}

func EnableTestingRebootFunction() (restore func()) {
	testingRebootItself = true
	return func() { testingRebootItself = false }
}
