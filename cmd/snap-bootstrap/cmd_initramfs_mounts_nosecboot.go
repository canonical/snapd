// -*- Mode: Go; indent-tabs-mode: t -*-
//go:build nosecboot

/*
 * Copyright (C) 2019-2020 Canonical Ltd
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
	"errors"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/osutil/disks"
	"github.com/snapcore/snapd/secboot"
)

var (
	errNotImplemented = errors.New("not implemented")
)

func init() {
	secbootProvisionForCVM = func(_ string) error {
		return errNotImplemented
	}
	secbootMeasureSnapSystemEpochWhenPossible = func() error {
		return errNotImplemented
	}
	secbootMeasureSnapModelWhenPossible = func(_ func() (*asserts.Model, error)) error {
		return errNotImplemented
	}
	secbootUnlockVolumeUsingSealedKeyIfEncrypted = func(disk disks.Disk, name string, sealedEncryptionKeyFile string, opts *secboot.UnlockVolumeUsingSealedKeyOptions) (secboot.UnlockResult, error) {
		return secboot.UnlockResult{}, errNotImplemented
	}
	secbootUnlockEncryptedVolumeUsingKey = func(disk disks.Disk, name string, key []byte) (secboot.UnlockResult, error) {
		return secboot.UnlockResult{}, errNotImplemented
	}

	secbootLockSealedKeys = func() error {
		return errNotImplemented
	}
}
