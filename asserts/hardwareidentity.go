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
	issuerId, err := checkStringMatches(assert.headers, "issuer-id", validAccountID)
	if err != nil {
		return nil, err
	}

	if issuerId != assert.HeaderString("authority-id") {
		return nil, errors.New("issuer id must match authority id")
	}

	manufacturer, err := checkNotEmptyString(assert.headers, "manufacturer")         
	if err != nil {
		return nil, err
	}
	
	hardwareName, err := checkStringMatches(assert.headers, "hardware-name", validModel)         
	if err != nil {
		return nil, err
	}
	
	hardwareId, err := checkNotEmptyString(assert.headers, "hardware-id")           
	if err != nil {
		return nil, err
	}
	
	hardwareIdKey, err := checkNotEmptyString(assert.headers, "hardware-id-key")        
	if err != nil {
		return nil, err
	}
	
	decodedHardwareIdKey, err := DecodePublicKey([]byte(hardwareIdKey))
	if err != nil {
		return nil, err
	}
	
	hardwareIdKeySha3384 := assert.HeaderString("hardware-id-key-sha3-384")

	if hardwareIdKeySha3384 != decodedHardwareIdKey.ID() {
		return nil, fmt.Errorf("hardware id key does not match provided hash")
	}

	if len(assert.body) != 0 {
		return nil, errors.New("body must be empty")
	}

	return &HardwareIdentity{
		assertionBase: assert,
		issuerId: issuerId,
		manufacturer: manufacturer,
		hardwareName: hardwareName,
		hardwareId: hardwareId,
		hardwareIdKey: hardwareIdKey,
		hardwareIdKeySha3384: hardwareIdKeySha3384,
	}, nil
}