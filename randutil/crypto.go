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

package randutil

import (
	cryptorand "crypto/rand"
	"encoding/base64"
	"fmt"
)

// CryptoTokenBytes returns a crypthographically random token byte sequence.
func CryptoTokenBytes(nbytes int) ([]byte, error) {
	b := make([]byte, nbytes)
	_, err := cryptorand.Read(b)
	if err != nil {
		return nil, fmt.Errorf("canot obtain %d crypto random bytes: %v", nbytes, err)
	}
	return b, nil
}

// CryptoToken returns a crypthographically random token string.
// The result is URL-safe.
func CryptoToken(nbytes int) (string, error) {
	b, err := CryptoTokenBytes(nbytes)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
