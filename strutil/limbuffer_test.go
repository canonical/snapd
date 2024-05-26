// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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
	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/strutil"
)

type limitedBufferSuite struct{}

var _ = Suite(&limitedBufferSuite{})

func (s *limitedBufferSuite) TestLimitedBuffer(c *C) {
	w := strutil.NewLimitedBuffer(100, 6)

	data := []byte{'a'}
	n := mylog.Check2(w.Write(data))
	
	c.Assert(n, Equals, 1)
	c.Assert(w.Bytes(), DeepEquals, []byte{'a'})

	data = []byte("bcde")
	n = mylog.Check2(w.Write(data))
	
	c.Assert(n, Equals, 4)

	n = mylog.Check2(w.Write([]byte("xyz")))
	
	c.Assert(w.Bytes(), DeepEquals, []byte("cdexyz"))
	c.Assert(n, Equals, 3)

	n = mylog.Check2(w.Write([]byte("12")))
	
	c.Assert(w.Bytes(), DeepEquals, []byte("exyz12"))
	c.Assert(n, Equals, 2)

	// eventually 2 times the size and this triggers the truncate in Write
	n = mylog.Check2(w.Write([]byte("abcd")))
	
	c.Assert(w.Bytes(), DeepEquals, []byte("12abcd"))
	c.Assert(n, Equals, 4)

	// more than maxBytes in a single write
	n = mylog.Check2(w.Write([]byte("abcdefghijklmnopqrstuvwxyz")))
	
	c.Assert(w.Bytes(), DeepEquals, []byte("uvwxyz"))
	c.Assert(n, Equals, 26)
}
