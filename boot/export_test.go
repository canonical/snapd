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

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/timings"
)

func NewCoreBootParticipant(s snap.PlaceInfo, t snap.Type, dev Device) *coreBootParticipant {
	bs, err := bootStateFor(t, dev)
	if err != nil {
		panic(err)
	}
	return &coreBootParticipant{s: s, bs: bs}
}

func NewCoreKernel(s snap.PlaceInfo, d Device) *coreKernel {
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
	SealKeyToModeenv                = sealKeyToModeenv
	ResealKeyToModeenv              = resealKeyToModeenv
	RecoveryBootChainsForSystems    = recoveryBootChainsForSystems
	SealKeyModelParams              = sealKeyModelParams
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

func (o *TrustedAssetsInstallObserver) CurrentDataEncryptionKey() secboot.EncryptionKey {
	return o.dataEncryptionKey
}

func (o *TrustedAssetsInstallObserver) CurrentSaveEncryptionKey() secboot.EncryptionKey {
	return o.saveEncryptionKey
}

func MockSecbootSealKeys(f func(keys []secboot.SealKeyRequest, params *secboot.SealKeysParams) error) (restore func()) {
	old := secbootSealKeys
	secbootSealKeys = f
	return func() {
		secbootSealKeys = old
	}
}

func MockSecbootResealKeys(f func(params *secboot.ResealKeysParams) error) (restore func()) {
	old := secbootResealKeys
	secbootResealKeys = f
	return func() {
		secbootResealKeys = old
	}
}

func MockSeedReadSystemEssential(f func(seedDir, label string, essentialTypes []snap.Type, tm timings.Measurer) (*asserts.Model, []*seed.Snap, error)) (restore func()) {
	old := seedReadSystemEssential
	seedReadSystemEssential = f
	return func() {
		seedReadSystemEssential = old
	}
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
)

func (b *bootChain) SetModelAssertion(model *asserts.Model) {
	b.model = model
}

func (b *bootChain) SetKernelBootFile(kbf bootloader.BootFile) {
	b.kernelBootFile = kbf
}

func (b *bootChain) KernelBootFile() bootloader.BootFile {
	return b.kernelBootFile
}

func MockHasFDESetupHook(f func() (bool, error)) (restore func()) {
	oldHasFDESetupHook := HasFDESetupHook
	HasFDESetupHook = f
	return func() {
		HasFDESetupHook = oldHasFDESetupHook
	}
}

func MockRunFDESetupHook(f func(string, *FDESetupHookParams) ([]byte, error)) (restore func()) {
	oldRunFDESetupHook := RunFDESetupHook
	RunFDESetupHook = f
	return func() { RunFDESetupHook = oldRunFDESetupHook }
}
