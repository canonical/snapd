// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021-2023 Canonical Ltd
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
	"time"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/boot"
	"github.com/snapcore/snapd/bootloader"
	"github.com/snapcore/snapd/sysconfig"
	"github.com/snapcore/snapd/testutil"
)

var (
	EncryptionAvailabilityCheck             = encryptionAvailabilityCheck
	OrderedCurrentBootImages                = orderedCurrentBootImages
	OrderedCurrentBootImagesHybrid          = orderedCurrentBootImagesHybrid
	CheckFDEFeatures                        = checkFDEFeatures
	PreinstallCheckSupportedWithEnvFallback = preinstallCheckSupportedWithEnvFallback

	UbuntuISOBootMode = ubuntuISOBootMode
	RunBootMode       = runBootMode
	RecoverBootMode   = recoverBootMode
)

type BootMode = bootMode

func MockPreinstallCheckTimeout(tm time.Duration) (restore func()) {
	old := preinstallCheckTimeout
	preinstallCheckTimeout = tm
	return func() {
		preinstallCheckTimeout = old
	}
}

func MockTimeNow(f func() time.Time) (restore func()) {
	old := timeNow
	timeNow = f
	return func() {
		timeNow = old
	}
}

func MockSysconfigConfigureTargetSystem(f func(mod *asserts.Model, opts *sysconfig.Options) error) (restore func()) {
	old := sysconfigConfigureTargetSystem
	sysconfigConfigureTargetSystem = f
	return func() {
		sysconfigConfigureTargetSystem = old
	}
}

func MockBootUseTokens(f func(model *asserts.Model) bool) (restore func()) {
	old := bootUseTokens
	bootUseTokens = f
	return func() {
		bootUseTokens = old
	}
}

func MockSecbootFDEOpteeTAPresent(fn func() bool) (restore func()) {
	restore = testutil.Backup(&secbootFDEOpteeTAPresent)
	secbootFDEOpteeTAPresent = fn
	return restore
}

func MockBootMaybeReadModeenv(f func() (*boot.Modeenv, error)) (restore func()) {
	return testutil.Mock(&bootMaybeReadModeenv, f)
}

func MockFdestateGetRunBootChain(f func() ([]bootloader.BootFile, error)) (restore func()) {
	return testutil.Mock(&fdestateGetRunBootChain, f)
}
