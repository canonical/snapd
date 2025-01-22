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

package strutil_test

import (
	"math"

	"github.com/snapcore/snapd/strutil"
	. "gopkg.in/check.v1"
)

type entropySuite struct{}

var _ = Suite(&entropySuite{})

func (s *entropySuite) TestEntropy(c *C) {
	for _, tc := range []struct {
		s               string
		expectedEntropy float64
	}{
		{"aaaaaaaaaaaaaaaa", math.Log2(26) * 2},       // 26 lowercase pool, passphrase translates to aa after prunning
		{"aaaaaaBBBaaaa111", math.Log2(26+26+10) * 8}, // 26+26+10 (upper,lower,digits), passphrase translates to aaBBaa11 after prunning
		{"لينكس", math.Log2(5) * 5},                   // 5 non-ASCII character adding 1 to the base each
	} {
		c.Assert(strutil.Entropy(tc.s), Equals, tc.expectedEntropy)
	}
}
