// -*- Mode: Go; indent-tabs-mode: t -*-
// +build nosecboot

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
)

var (
	errNotImplemented = errors.New("not implemented")
)

func init() {
	secbootMeasureSnapSystemEpochWhenPossible = func() error {
		return errNotImplemented
	}
	secbootMeasureSnapModelWhenPossible = func(_ func() (*asserts.Model, error)) error {
		return errNotImplemented
	}
	secbootUnlockVolumeIfEncrypted = func(disk disks.Disk, name string, encryptionKeyDir string, lockKeysOnFinish bool) (string, bool, error) {
		return "", false, errNotImplemented
	}
}
