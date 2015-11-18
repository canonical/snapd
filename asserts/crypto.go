// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2015 Canonical Ltd
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
	"crypto/rand"
	"crypto/rsa"
	"time"

	"golang.org/x/crypto/openpgp/packet"
)

// TODO: eventually this should be the only non-test file using/importing directly from golang.org/x/crypto

func generatePrivateKey() (*packet.PrivateKey, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, err
	}
	return packet.NewRSAPrivateKey(time.Now(), priv), nil
}

type signaturePrim interface{}

// xxx does this belongs here or just a subset
type publicKey interface {
	// IsKeyValidAt returns whether the public key is valid at 'when' time
	IsKeyValidAt(when time.Time) bool
	// KeyFingerprint returns the key fingerprint.
	KeyFingerprint() string
	// Verify verifies the signature of content using the key.
	Verify(content []byte, sig signaturePrim) error
}

func parseSignature(signature []byte) (keyID string, sig signaturePrim, err error) {
	// xxx implement me
	return "", nil, nil
}
