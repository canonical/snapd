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
	"crypto"
	_ "crypto/sha256"
	"encoding/base64"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
)

type encodeDigestSuite struct{}

var _ = Suite(&encodeDigestSuite{})

func (eds *encodeDigestSuite) TestEncodeDigestOK(c *C) {
	h := crypto.SHA512.New()
	h.Write([]byte("some stuff to hash"))
	digest := h.Sum(nil)
	encoded := mylog.Check2(asserts.EncodeDigest(crypto.SHA512, digest))


	decoded := mylog.Check2(base64.RawURLEncoding.DecodeString(encoded))

	c.Check(decoded, DeepEquals, digest)

	// sha3-384
	b := mylog.Check2(base64.RawURLEncoding.DecodeString(blobSHA3_384))

	encoded = mylog.Check2(asserts.EncodeDigest(crypto.SHA3_384, b))

	c.Check(encoded, Equals, blobSHA3_384)
}

func (eds *encodeDigestSuite) TestEncodeDigestErrors(c *C) {
	_ := mylog.Check2(asserts.EncodeDigest(crypto.SHA1, nil))
	c.Check(err, ErrorMatches, "unsupported hash")

	_ = mylog.Check2(asserts.EncodeDigest(crypto.SHA512, []byte{1, 2}))
	c.Check(err, ErrorMatches, "hash digest by sha512 should be 64 bytes")

	_ = mylog.Check2(asserts.EncodeDigest(crypto.SHA3_384, []byte{1, 2}))
	c.Check(err, ErrorMatches, "hash digest by sha3-384 should be 48 bytes")
}
