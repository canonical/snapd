// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016-2020 Canonical Ltd
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
	"github.com/snapcore/snapd/osutil/disks"
)

var (
	Parser = parser

	DoSystemdMount = doSystemdMountImpl
)

type SystemdMountOptions = systemdMountOptions

func MockTimeNow(f func() time.Time) (restore func()) {
	old := timeNow
	timeNow = f
	return func() {
		timeNow = old
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

func MockTriggerwatchWait(f func(_ time.Duration) error) (restore func()) {
	oldTriggerwatchWait := triggerwatchWait
	triggerwatchWait = f
	return func() {
		triggerwatchWait = oldTriggerwatchWait
	}
}

var DefaultTimeout = defaultTimeout

func MockDefaultMarkerFile(p string) (restore func()) {
	old := defaultMarkerFile
	defaultMarkerFile = p
	return func() {
		defaultMarkerFile = old
	}
}

func MockSecbootUnlockVolumeIfEncrypted(f func(disk disks.Disk, name string, encryptionKeyDir string, lockKeysOnFinish bool) (string, bool, error)) (restore func()) {
	old := secbootUnlockVolumeIfEncrypted
	secbootUnlockVolumeIfEncrypted = f
	return func() {
		secbootUnlockVolumeIfEncrypted = old
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
