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
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
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
	issuerId, err := checkNotEmptyString(assert.headers, "issuer-id")         
	if err != nil {
		return nil, err
	}
	if !validAccountID.MatchString(issuerId) {
		return nil, fmt.Errorf("invalid issuer id: %s", issuerId)
	}


	manufacturer, err := checkNotEmptyString(assert.headers, "manufacturer")         
	if err != nil {
		return nil, err
	}
	
	hardwareName, err := checkNotEmptyString(assert.headers, "hardware-name")         
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
	
	if !IsParsablePemPublicKeyBody(hardwareIdKey) {
		return nil, errors.New(`"hardware-id-key" header should be the body of a PEM`)
	}

	// don't check if non-empty as check was done for primary key
	hardwareIdKeySha3384, _ := checkNotEmptyString(assert.headers, "hardware-id-key-sha3-384") 

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


// IsParsablePemPublicKeyBody checks if s is a valid PEM body for RFC 7468 "PUBLIC KEY"
func IsParsablePemPublicKeyBody(s string) bool {
    cleaned := strings.ReplaceAll(s, "\n", "")
    cleaned = strings.ReplaceAll(cleaned, "\r", "")
    cleaned = strings.TrimSpace(cleaned)

    // Must not contain encapsulation boundaries
    if strings.Contains(cleaned, "-----BEGIN") || strings.Contains(cleaned, "-----END") {
        return false
    }

    // Base64 decode: PEM may have whitespace, so we ignore it above
    _, err := base64.StdEncoding.DecodeString(cleaned)

    return err == nil
}
