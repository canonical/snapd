// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2023 Canonical Ltd
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

package install

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	"github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/secboot/keys"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

// // var (
// // 	MaybeApplyPreseededData = maybeApplyPreseededData
// // )

// func MockMaybeApplyPreseededData(f func(st *state.State, model *asserts.Model, ubuntuSeedDir, sysLabel, writableDir string) (bool, error)) (restore func()) {
// 	r := testutil.Backup(&maybeApplyPreseededData)
// 	maybeApplyPreseededData = f
// 	return r
// }

func MockSeedOpen(f func(seedDir, label string) (seed.Seed, error)) (restore func()) {
	r := testutil.Backup(&seedOpen)
	seedOpen = f
	return r
}

func MockBootMakeSystemRunnable(f func(model *asserts.Model, bootWith *boot.BootableSet, seal *boot.TrustedAssetsInstallObserver) error) (restore func()) {
	restore = testutil.Backup(&bootMakeRunnable)
	bootMakeRunnable = f
	return restore
}

func MockBootMakeSystemRunnableAfterDataReset(f func(model *asserts.Model, bootWith *boot.BootableSet, seal *boot.TrustedAssetsInstallObserver) error) (restore func()) {
	restore = testutil.Backup(&bootMakeRunnableAfterDataReset)
	bootMakeRunnableAfterDataReset = f
	return restore
}

func MockBootEnsureNextBootToRunMode(f func(systemLabel string) error) (restore func()) {
	old := bootEnsureNextBootToRunMode
	bootEnsureNextBootToRunMode = f
	return func() {
		bootEnsureNextBootToRunMode = old
	}
}

func MockSecbootCheckTPMKeySealingSupported(f func() error) (restore func()) {
	old := secbootCheckTPMKeySealingSupported
	secbootCheckTPMKeySealingSupported = f
	return func() {
		secbootCheckTPMKeySealingSupported = old
	}
}

func MockSysconfigConfigureTargetSystem(f func(mod *asserts.Model, opts *sysconfig.Options) error) (restore func()) {
	old := sysconfigConfigureTargetSystem
	sysconfigConfigureTargetSystem = f
	return func() {
		sysconfigConfigureTargetSystem = old
	}
}

func MockInstallRun(f func(model gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*install.InstalledSystemSideData, error)) (restore func()) {
	old := installRun
	installRun = f
	return func() {
		installRun = old
	}
}

func MockInstallFactoryReset(f func(model gadget.Model, gadgetRoot, kernelRoot, device string, options install.Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*install.InstalledSystemSideData, error)) (restore func()) {
	restore = testutil.Backup(&installFactoryReset)
	installFactoryReset = f
	return restore
}

func MockInstallWriteContent(f func(onVolumes map[string]*gadget.Volume, allLaidOutVols map[string]*gadget.LaidOutVolume, encSetupData *install.EncryptionSetupData, observer gadget.ContentObserver, perfTimings timings.Measurer) ([]*gadget.OnDiskVolume, error)) (restore func()) {
	old := installWriteContent
	installWriteContent = f
	return func() {
		installWriteContent = old
	}
}

func MockInstallMountVolumes(f func(onVolumes map[string]*gadget.Volume, encSetupData *install.EncryptionSetupData) (espMntDir string, unmount func() error, err error)) (restore func()) {
	old := installMountVolumes
	installMountVolumes = f
	return func() {
		installMountVolumes = old
	}
}

func MockInstallEncryptPartitions(f func(onVolumes map[string]*gadget.Volume, encryptionType secboot.EncryptionType, model *asserts.Model, gadgetRoot, kernelRoot string, perfTimings timings.Measurer) (*install.EncryptionSetupData, error)) (restore func()) {
	old := installEncryptPartitions
	installEncryptPartitions = f
	return func() {
		installEncryptPartitions = old
	}
}

func MockInstallSaveStorageTraits(f func(model gadget.Model, allLaidOutVols map[string]*gadget.LaidOutVolume, encryptSetupData *install.EncryptionSetupData) error) (restore func()) {
	old := installSaveStorageTraits
	installSaveStorageTraits = f
	return func() {
		installSaveStorageTraits = old
	}
}

func MockSecbootStageEncryptionKeyChange(f func(node string, key keys.EncryptionKey) error) (restore func()) {
	restore = testutil.Backup(&secbootStageEncryptionKeyChange)
	secbootStageEncryptionKeyChange = f
	return restore
}

func MockEncryptionSetupDataInCache(st *state.State, label string) (restore func()) {
	st.Lock()
	defer st.Unlock()
	var esd *install.EncryptionSetupData
	labelToEncData := map[string]*install.MockEncryptedDeviceAndRole{
		"ubuntu-save": {
			Role:            "system-save",
			EncryptedDevice: "/dev/mapper/ubuntu-save",
		},
		"ubuntu-data": {
			Role:            "system-data",
			EncryptedDevice: "/dev/mapper/ubuntu-data",
		},
	}
	esd = install.MockEncryptionSetupData(labelToEncData)
	st.Cache(encryptionSetupDataKey{label}, esd)
	return func() { CleanUpEncryptionSetupDataInCache(st, label) }
}

func CheckEncryptionSetupDataFromCache(st *state.State, label string) error {
	cached := st.Cached(encryptionSetupDataKey{label})
	if cached == nil {
		return fmt.Errorf("no EncryptionSetupData found in cache")
	}
	if _, ok := cached.(*install.EncryptionSetupData); !ok {
		return fmt.Errorf("wrong data type under encryptionSetupDataKey")
	}
	return nil
}

func CleanUpEncryptionSetupDataInCache(st *state.State, label string) {
	st.Lock()
	defer st.Unlock()
	key := encryptionSetupDataKey{label}
	st.Cache(key, nil)
}
