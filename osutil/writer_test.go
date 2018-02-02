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

package osutil_test

import (
	"bytes"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/osutil"
)

type limitedWriterSuite struct{}

var _ = Suite(&limitedWriterSuite{})

func (s *limitedWriterSuite) TestWriter(c *C) {
	var buffer bytes.Buffer

	w := osutil.NewLimitedWriter(&buffer, 5)

	data := []byte{'a'}
	n, err := w.Write(data)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 1)
	c.Assert(buffer.Bytes(), DeepEquals, []byte{'a'})

	data = []byte("bcde")
	n, err = w.Write(data)
	c.Assert(err, IsNil)
	c.Assert(n, Equals, 4)

	for i := 0; i < 2; i++ {
		n, err = w.Write([]byte{'x'})
		c.Assert(err, NotNil)
		c.Assert(err, ErrorMatches, `buffer capacity exceeded`)
		c.Assert(buffer.Bytes(), DeepEquals, []byte("abcde"))
	}
}
