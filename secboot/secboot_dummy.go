// -*- Mode: Go; indent-tabs-mode: t -*-
// +build nosecboot

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

package secboot

import (
	"fmt"

	"github.com/snapcore/snapd/asserts"
)

func CheckKeySealingSupported() error {
	return fmt.Errorf("build without secboot support")
}

func SealKeys(keys []SealKeyRequest, params *SealKeysParams) error {
	return fmt.Errorf("build without secboot support")
}

func ResealKeys(params *ResealKeysParams) error {
	return fmt.Errorf("build without secboot support")
}

func WriteKeyData(name, path string, encryptedPayload, auxKey, rawhandle []byte, model *asserts.Model) error {
	return fmt.Errorf("build without secboot support")
}

func MarshalKeys(key []byte, auxKey []byte) []byte {
	panic("build without secboot support")
	return nil
}
