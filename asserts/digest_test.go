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
	"strings"

	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/asserts"
)

type encodeDigestSuite struct{}

var _ = Suite(&encodeDigestSuite{})

func (eds *encodeDigestSuite) TestEncodeDigestOK(c *C) {
	h := crypto.SHA256.New()
	h.Write([]byte("some stuff to hash"))
	digest := h.Sum(nil)
	encoded, err := asserts.EncodeDigest(crypto.SHA256, digest)
	c.Assert(err, IsNil)

	c.Check(strings.HasPrefix(encoded, "sha256 "), Equals, true)
	decoded, err := base64.RawURLEncoding.DecodeString(encoded[len("sha256 "):])
	c.Assert(err, IsNil)
	c.Check(decoded, DeepEquals, digest)
}

func (eds *encodeDigestSuite) TestEncodeDigestErrors(c *C) {
	_, err := asserts.EncodeDigest(crypto.SHA1, nil)
	c.Check(err, ErrorMatches, "unsupported hash")

	_, err = asserts.EncodeDigest(crypto.SHA256, []byte{1, 2})
	c.Check(err, ErrorMatches, "hash digest by sha256 should be 32 bytes")
}
