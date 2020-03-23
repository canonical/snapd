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

package randutil_test

import (
	"encoding/base64"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/randutil"
)

type cryptoRandutilSuite struct{}

var _ = Suite(&cryptoRandutilSuite{})

func (s *cryptoRandutilSuite) TestCryptoTokenBytes(c *C) {
	x, err := randutil.CryptoTokenBytes(5)
	c.Assert(err, IsNil)
	c.Check(x, HasLen, 5)
}

func (s *cryptoRandutilSuite) TestCryptoToken(c *C) {
	x, err := randutil.CryptoToken(5)
	c.Assert(err, IsNil)

	b, err := base64.RawURLEncoding.DecodeString(x)
	c.Assert(err, IsNil)
	c.Check(b, HasLen, 5)
}
