// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2020 Canonical Ltd
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
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/gadget"
)

func NewTrustedAssetsInstallObserver(model *asserts.Model) *TrustedAssetsInstallObserver {
	if model.Grade() == asserts.ModelGradeUnset {
		// no need to observe updates when assets are not managed
		return nil
	}

	return &TrustedAssetsInstallObserver{
		model: model,
	}
}

type TrustedAssetsInstallObserver struct {
	model *asserts.Model
}

// Observe observes the operation related to the content of a given gadget
// structure. In particular, the TrustedAssetsInstallObserver tracks writing of
// managed boot assets, such as the bootloader binary which is measured as part
// of the secure boot.
//
// Implements gadget.ContentObserver.
func (o *TrustedAssetsInstallObserver) Observe(op gadget.ContentOperation, affectedStruct *gadget.LaidOutStructure, root, realSource, relativeTarget string) (bool, error) {
	// TODO:UC20:
	// steps on write action:
	// - copy new asset to assets cache
	// - update modeeenv
	return true, nil
}

// Seal performs the initial sealing of encryption key to the TPM device
// available in the system.
func (o *TrustedAssetsInstallObserver) Seal() error {
	// TODO:UC20: steps:
	// - initial seal
	return nil
}

func NewTrustedAssetsUpdateObserver(model *asserts.Model) *TrustedAssetsUpdateObserver {
	if model.Grade() == asserts.ModelGradeUnset {
		// no need to observe updates when assets are not managed
		return nil
	}

	return &TrustedAssetsUpdateObserver{}
}

// TrustedAssetsUpdateObserver tracks the updates of trusted boot assets and
// attempts to reseal when needed.
type TrustedAssetsUpdateObserver struct{}

// Observe observes the operation related to the update or rollback of the
// content of a given gadget structure. In particular, the
// TrustedAssetsUpdateObserver tracks updates of managed boot assets, such as
// the bootloader binary which is measured as part of the secure boot.
//
// Implements gadget.ContentUpdateObserver.
func (o *TrustedAssetsUpdateObserver) Observe(op gadget.ContentOperation, affectedStruct *gadget.LaidOutStructure, root, realSource, relativeTarget string) (bool, error) {
	// TODO:UC20:
	// steps on write action:
	// - copy new asset to assets cache
	// - update modeeenv
	// steps on rollback action:
	// - drop file from cache if no longer referenced
	// - update modeenv
	return true, nil
}

// BeforeWrite is called when the update process has been staged for execution.
func (o *TrustedAssetsUpdateObserver) BeforeWrite() error {
	// TODO:UC20:
	// - reseal with a given state of modeenv
	return nil
}

// Canceled is called when the update has been canceled, or if changes
// were written and the update has been reverted.
func (o *TrustedAssetsUpdateObserver) Canceled() error {
	// TODO:UC20:
	// - reseal with a given state of modeenv
	return nil
}
