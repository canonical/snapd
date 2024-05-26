// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2019 Canonical Ltd
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

package quantity_test

import (
	"fmt"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/gadget/quantity"
)

type offsetTestSuite struct{}

var _ = Suite(&offsetTestSuite{})

func (s *offsetTestSuite) TestIECString(c *C) {
	for _, tc := range []struct {
		offset quantity.Offset
		exp    string
	}{
		{512, "512 B"},
		{1000, "1000 B"},
		{1030, "1.01 KiB"},
		{quantity.OffsetKiB + 512, "1.50 KiB"},
		{123 * quantity.OffsetKiB, "123 KiB"},
		{512 * quantity.OffsetKiB, "512 KiB"},
		{578 * quantity.OffsetMiB, "578 MiB"},
		{1024*quantity.OffsetMiB + 123*quantity.OffsetMiB, "1.12 GiB"},
		{1024 * 1024 * quantity.OffsetMiB, "1 TiB"},
		{2 * 1024 * 1024 * 1024 * 1024 * quantity.OffsetMiB, "2048 PiB"},
	} {
		c.Check(tc.offset.IECString(), Equals, tc.exp)
	}
}

func (s *offsetTestSuite) TestUnmarshalYAMLSize(c *C) {
	type foo struct {
		Offset quantity.Offset `yaml:"offset"`
	}

	for i, tc := range []struct {
		s   string
		sz  quantity.Offset
		err string
	}{
		{"1234", 1234, ""},
		{"1234M", 1234 * quantity.OffsetMiB, ""},
		{"1234G", 1234 * 1024 * quantity.OffsetMiB, ""},
		{"0", 0, ""},
		{"a0M", 0, `cannot parse offset "a0M": no numerical prefix.*`},
		{"-123", 0, `cannot parse offset "-123": offset cannot be negative`},
		{"123a", 0, `cannot parse offset "123a": invalid suffix "a"`},
	} {
		c.Logf("tc: %v", i)

		var f foo
		mylog.Check(yaml.Unmarshal([]byte(fmt.Sprintf("offset: %s", tc.s)), &f))
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
			c.Check(f.Offset, Equals, tc.sz)
		}
	}
}

func (s *offsetTestSuite) TestOffsetString(c *C) {
	var pOffset *quantity.Offset
	c.Check(pOffset.String(), Equals, "unspecified")

	for _, tc := range []struct {
		offset quantity.Offset
		exp    string
	}{
		{512, "512"},
		{1030, "1030"},
		{quantity.OffsetKiB + 512, "1536"},
		{578 * quantity.OffsetMiB, "606076928"},
	} {
		c.Check(tc.offset.String(), Equals, tc.exp)
	}
}
