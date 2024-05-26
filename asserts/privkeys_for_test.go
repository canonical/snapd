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
	"encoding/base64"

	"golang.org/x/crypto/openpgp/packet"
	"golang.org/x/crypto/sha3"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
)

// private keys to use in tests
var (
	// use a shorter key length here for test keys because otherwise
	// they take too long to generate;
	// the ones that care use pregenerated keys of the right length
	// or use GenerateKey directly
	testPrivKey0, _               = assertstest.GenerateKey(752)
	testPrivKey1, testPrivKey1RSA = assertstest.GenerateKey(752)
	testPrivKey2, _               = assertstest.GenerateKey(752)

	testPrivKey1SHA3_384 string
)

func init() {
	pkt := packet.NewRSAPrivateKey(asserts.V1FixedTimestamp, testPrivKey1RSA)
	h := sha3.New384()
	h.Write([]byte{0x1})
	mylog.Check(pkt.PublicKey.Serialize(h))

	testPrivKey1SHA3_384 = base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
