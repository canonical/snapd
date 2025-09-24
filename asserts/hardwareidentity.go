// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2025 Canonical Ltd
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

package asserts

import (
	"errors"
	"fmt"
)

type HardwareIdentity struct {
	assertionBase

	issuerId              string
	manufacturer          string
	hardwareName          string
	hardwareId            string
	hardwareIdKey         string
	hardwareIdKeySha3384  string
}

func (h *HardwareIdentity) IssuerId() string {
	return h.issuerId
}

func (h *HardwareIdentity) Manufacturer() string {
	return h.manufacturer
}

func (h *HardwareIdentity) HardwareName() string {
	return h.hardwareName
}

func (h *HardwareIdentity) HardwareId() string {
	return h.hardwareId
}

func (h *HardwareIdentity) HardwareIdKey() string {
	return h.hardwareIdKey
}

func (h *HardwareIdentity) HardwareIdKeySha3384() string {
	return h.hardwareIdKeySha3384
}

func assembleHardwareIdentity(assert assertionBase) (Assertion, error) {
	issuerId := assert.HeaderString("issuer-id")
	if !validAccountID.MatchString(issuerId) {
		return nil, fmt.Errorf("invalid account id: %s", issuerId)
	}

	if len(assert.body) != 0 {
		return nil, errors.New("body must be empty")
	}

	return &HardwareIdentity{
		issuerId: issuerId,
	}, nil
}