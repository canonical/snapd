// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015 Canonical Ltd
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

package asserts_test

import (
	"fmt"

	"golang.org/x/crypto/openpgp/packet"

	"github.com/ubuntu-core/snappy/asserts"
)

// private keys to use in tests
var (
	testPrivKey0 = genTestPrivKey()
	testPrivKey1 = genTestPrivKey()
	testPrivKey2 = genTestPrivKey()
)

func genTestPrivKey() *packet.PrivateKey {
	privKey, err := asserts.GeneratePrivateKeyInTest()
	if err != nil {
		panic(fmt.Errorf("failed to create priv key for tests: %v", err))
	}
	return privKey
}
