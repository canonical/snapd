// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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
	"gopkg.in/yaml.v2"

	"github.com/snapcore/snapd/strutil"
)

type orderedMapSuite struct{}

var _ = Suite(&orderedMapSuite{})

func (s *orderedMapSuite) TestBasic(c *C) {
	om := strutil.NewOrderedMap(
		"K1", "bar",
		"K2", "baz",
		"K0", "foo",
	)

	for i := 0; i < 100; i++ {
		c.Assert(om.Keys(), DeepEquals, []string{"K1", "K2", "K0"})
	}

	c.Check(om.Get("K1"), Equals, "bar")
	c.Check(om.Get("K2"), Equals, "baz")
	c.Check(om.Get("K0"), Equals, "foo")

	om.Del("K2")
	c.Assert(om.Keys(), DeepEquals, []string{"K1", "K0"})

	om.Set("K9", "foobar")
	c.Assert(om.Keys(), DeepEquals, []string{"K1", "K0", "K9"})
	c.Check(om.Get("K9"), Equals, "foobar")

	om2 := om.Copy()
	c.Assert(om2.Keys(), DeepEquals, []string{"K1", "K0", "K9"})

	// replaces existing value, inserted at the end
	om.Set("K1", "newbar")
	c.Assert(om.Keys(), DeepEquals, []string{"K0", "K9", "K1"})
	c.Check(om.Get("K1"), Equals, "newbar")
}

func (s *orderedMapSuite) TestYamlTrivial(c *C) {
	yamlStr := []byte(`
k: v
k2: v2
k0: v0
`)
	var om strutil.OrderedMap
	err := yaml.Unmarshal(yamlStr, &om)
	c.Assert(err, IsNil)
	c.Check(om.Keys(), DeepEquals, []string{"k", "k2", "k0"})
}

func (s *orderedMapSuite) TestYamlDupe(c *C) {
	yamlStr := []byte(`
k: v
k0: v0
k: v
`)
	var om strutil.OrderedMap
	err := yaml.Unmarshal(yamlStr, &om)
	c.Assert(err, ErrorMatches, `found duplicate key "k"`)
}

func (s *orderedMapSuite) TestYamlErr(c *C) {
	yamlStr := []byte(`
k:
 nested: var
`)
	var om strutil.OrderedMap
	err := yaml.Unmarshal(yamlStr, &om)
	c.Assert(err, ErrorMatches, "(?m)yaml: unmarshal error.*")
}
