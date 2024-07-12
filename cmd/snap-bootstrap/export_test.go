// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2024 Canonical Ltd
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
	"fmt"
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/gadget"
	gadgetInstall "github.com/snapcore/snapd/gadget/install"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
	"github.com/snapcore/snapd/seed"
	"github.com/snapcore/snapd/testutil"
	"github.com/snapcore/snapd/timings"
)

var (
	Parser = parser

	DoSystemdMount = doSystemdMountImpl

	MountNonDataPartitionMatchingKernelDisk = mountNonDataPartitionMatchingKernelDisk

	GetNonUEFISystemDisk = getNonUEFISystemDisk
)

type SystemdMountOptions = systemdMountOptions

type RecoverDegradedState = recoverDegradedState

type PartitionState = partitionState

func (r *RecoverDegradedState) Degraded(isEncrypted bool) bool {
	m := recoverModeStateMachine{
		isEncryptedDev: isEncrypted,
		degradedState:  r,
	}
	return m.degraded()
}

func MockPollWaitForLabel(newPollDur time.Duration) (restore func()) {
	restore = testutil.Backup(&pollWaitForLabel)
	pollWaitForLabel = newPollDur
	return restore
}

func MockPollWaitForLabelIters(newNumIters int) (restore func()) {
	restore = testutil.Backup(&pollWaitForLabelIters)
	pollWaitForLabelIters = newNumIters
	return restore
}

func MockTimeNow(f func() time.Time) (restore func()) {
	old := timeNow
	timeNow = f
	return func() {
		timeNow = old
	}
}

func MockOsutilSetTime(f func(t time.Time) error) (restore func()) {
	old := osutilSetTime
	osutilSetTime = f
	return func() {
		osutilSetTime = old
	}
}

func MockOsutilIsMounted(f func(string) (bool, error)) (restore func()) {
	old := osutilIsMounted
	osutilIsMounted = f
	return func() {
		osutilIsMounted = old
	}
}

func MockSystemdMount(f func(_, _ string, opts *SystemdMountOptions) error) (restore func()) {
	old := doSystemdMount
	doSystemdMount = f
	return func() {
		doSystemdMount = old
	}
}

func MockTriggerwatchWait(f func(_ time.Duration, _ time.Duration) error) (restore func()) {
	oldTriggerwatchWait := triggerwatchWait
	triggerwatchWait = f
	return func() {
		triggerwatchWait = oldTriggerwatchWait
	}
}

var DefaultTimeout = defaultTimeout
var DefaultDeviceTimeout = defaultDeviceTimeout

func MockDefaultMarkerFile(p string) (restore func()) {
	old := defaultMarkerFile
	defaultMarkerFile = p
	return func() {
		defaultMarkerFile = old
	}
}

func MockSecbootUnlockVolumeUsingSealedKeyIfEncrypted(f func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error)) (restore func()) {
	old := secbootUnlockVolumeUsingSealedKeyIfEncrypted
	secbootUnlockVolumeUsingSealedKeyIfEncrypted = f
	return func() {
		secbootUnlockVolumeUsingSealedKeyIfEncrypted = old
	}
}

func MockSecbootUnlockEncryptedVolumeUsingPlatformKey(f func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error)) (restore func()) {
	old := secbootUnlockEncryptedVolumeUsingPlatformKey
	secbootUnlockEncryptedVolumeUsingPlatformKey = f
	return func() {
		secbootUnlockEncryptedVolumeUsingPlatformKey = old
	}
}

func MockSecbootProvisionForCVM(f func(_ string) error) (restore func()) {
	old := secbootProvisionForCVM
	secbootProvisionForCVM = f
	return func() {
		secbootProvisionForCVM = old
	}
}

func MockSecbootMeasureSnapSystemEpochWhenPossible(f func() error) (restore func()) {
	old := secbootMeasureSnapSystemEpochWhenPossible
	secbootMeasureSnapSystemEpochWhenPossible = f
	return func() {
		secbootMeasureSnapSystemEpochWhenPossible = old
	}
}

func MockSecbootMeasureSnapModelWhenPossible(f func(findModel func() (*asserts.Model, error)) error) (restore func()) {
	old := secbootMeasureSnapModelWhenPossible
	secbootMeasureSnapModelWhenPossible = f
	return func() {
		secbootMeasureSnapModelWhenPossible = old
	}
}

func MockSecbootLockSealedKeys(f func() error) (restore func()) {
	old := secbootLockSealedKeys
	secbootLockSealedKeys = f
	return func() {
		secbootLockSealedKeys = old
	}
}

func MockPartitionUUIDForBootedKernelDisk(uuid string) (restore func()) {
	old := bootFindPartitionUUIDForBootedKernelDisk
	bootFindPartitionUUIDForBootedKernelDisk = func() (string, error) {
		if uuid == "" {
			// mock error
			return "", fmt.Errorf("mocked error")
		}
		return uuid, nil
	}

	return func() {
		bootFindPartitionUUIDForBootedKernelDisk = old
	}
}

func MockTryRecoverySystemHealthCheck(mock func(gadget.Model) error) (restore func()) {
	old := tryRecoverySystemHealthCheck
	tryRecoverySystemHealthCheck = mock
	return func() {
		tryRecoverySystemHealthCheck = old
	}
}

func MockWaitFile(f func(string, time.Duration, int) error) (restore func()) {
	old := waitFile
	waitFile = f
	return func() {
		waitFile = old
	}
}

var WaitFile = waitFile

func MockGadgetInstallRun(f func(model gadget.Model, gadgetRoot string, kernelSnapInfo *gadgetInstall.KernelSnapInfo, bootDevice string, options gadgetInstall.Options, observer gadget.ContentObserver, perfTimings timings.Measurer) (*gadgetInstall.InstalledSystemSideData, error)) (restore func()) {
	old := gadgetInstallRun
	gadgetInstallRun = f
	return func() {
		gadgetInstallRun = old
	}
}

func MockMakeRunnableStandaloneSystem(f func(model *asserts.Model, bootWith *boot.BootableSet, obs boot.TrustedAssetsInstallObserver) error) (restore func()) {
	old := bootMakeRunnableStandaloneSystem
	bootMakeRunnableStandaloneSystem = f
	return func() {
		bootMakeRunnableStandaloneSystem = old
	}
}

func MockApplyPreseededData(f func(preseedSeed seed.PreseedCapable, writableDir string) error) (restore func()) {
	old := installApplyPreseededData
	installApplyPreseededData = f
	return func() {
		installApplyPreseededData = old
	}
}

func MockEnsureNextBootToRunMode(f func(systemLabel string) error) (restore func()) {
	old := bootEnsureNextBootToRunMode
	bootEnsureNextBootToRunMode = f
	return func() {
		bootEnsureNextBootToRunMode = old
	}
}

func MockBuildInstallObserver(f func(model *asserts.Model, gadgetDir string, useEncryption bool) (observer gadget.ContentObserver, trustedObserver boot.TrustedAssetsInstallObserver, err error)) (restore func()) {
	old := installBuildInstallObserver
	installBuildInstallObserver = f
	return func() {
		installBuildInstallObserver = old
	}
}
