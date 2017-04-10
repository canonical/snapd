// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2016-2017 Canonical Ltd
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

package devicestate

/*
#cgo pkg-config: openssl
#include "rsa_generate_key.h"
*/
import "C"

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"unsafe"
)

func generateRSAKey(keyLength uint64) (*rsa.PrivateKey, error) {
	var privateKey C.SnapdRSAKeyGenerationBuffer

	switch C.snapd_rsa_generate_key(C.uint64_t(keyLength), &privateKey) {
	case C.SNAPD_RSA_KEY_GENERATION_SEED_FAILURE:
		return nil, errors.New("cannot generate RSA key: RNG not seeded")
	case C.SNAPD_RSA_KEY_GENERATION_ALLOCATION_FAILURE:
		return nil, errors.New("cannot generate RSA key: could not allocate memory")
	case C.SNAPD_RSA_KEY_GENERATION_KEY_GENERATION_FAILURE:
		return nil, errors.New("cannot generate RSA key")
	case C.SNAPD_RSA_KEY_GENERATION_MARSHAL_FAILURE:
		return nil, errors.New("cannot generate RSA key: could not marshal key")
	case C.SNAPD_RSA_KEY_GENERATION_SUCCESS:
		break
	}

	defer C.free(unsafe.Pointer(privateKey.memory))
	blk, _ := pem.Decode(C.GoBytes(unsafe.Pointer(privateKey.memory), C.int(privateKey.size)))
	if blk == nil {
		return nil, errors.New("cannot decode PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}

	err = key.Validate()
	if err != nil {
		return nil, err
	}

	return key, err
}
