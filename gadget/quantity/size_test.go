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
	"testing"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/gadget/quantity"
)

func TestRun(t *testing.T) { TestingT(t) }

type sizeTestSuite struct{}

var _ = Suite(&sizeTestSuite{})

func (s *sizeTestSuite) TestIECString(c *C) {
	for _, tc := range []struct {
		size quantity.Size
		exp  string
	}{
		{512, "512 B"},
		{1000, "1000 B"},
		{1030, "1.01 KiB"},
		{quantity.SizeKiB + 512, "1.50 KiB"},
		{123 * quantity.SizeKiB, "123 KiB"},
		{512 * quantity.SizeKiB, "512 KiB"},
		{578 * quantity.SizeMiB, "578 MiB"},
		{1*quantity.SizeGiB + 123*quantity.SizeMiB, "1.12 GiB"},
		{1024 * quantity.SizeGiB, "1 TiB"},
		{2 * 1024 * 1024 * 1024 * quantity.SizeGiB, "2048 PiB"},
	} {
		c.Check(tc.size.IECString(), Equals, tc.exp)
	}
}

func (s *sizeTestSuite) TestUnmarshalYAMLSize(c *C) {
	type foo struct {
		Size quantity.Size `yaml:"size"`
	}

	for i, tc := range []struct {
		s   string
		sz  quantity.Size
		err string
	}{
		{"1234", 1234, ""},
		{"1234M", 1234 * quantity.SizeMiB, ""},
		{"1234G", 1234 * quantity.SizeGiB, ""},
		{"0", 0, ""},
		{"a0M", 0, `cannot parse size "a0M": no numerical prefix.*`},
		{"-123", 0, `cannot parse size "-123": size cannot be negative`},
		{"123a", 0, `cannot parse size "123a": invalid suffix "a"`},
	} {
		c.Logf("tc: %v", i)

		var f foo
		mylog.Check(yaml.Unmarshal([]byte(fmt.Sprintf("size: %s", tc.s)), &f))
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err)
		} else {
			c.Check(err, IsNil)
			c.Check(f.Size, Equals, tc.sz)
		}
	}
}

func (s *sizeTestSuite) TestSizeString(c *C) {
	var pSize *quantity.Size
	c.Check(pSize.String(), Equals, "unspecified")

	for _, tc := range []struct {
		size quantity.Size
		exp  string
	}{
		{512, "512"},
		{1030, "1030"},
		{quantity.SizeKiB + 512, "1536"},
		{578 * quantity.SizeMiB, "606076928"},
	} {
		c.Check(tc.size.String(), Equals, tc.exp)
	}
}
