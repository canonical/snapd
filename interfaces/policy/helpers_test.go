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

package policy_test

import (
	"fmt"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/policy"
)

type helpersSuite struct{}

var _ = Suite(&helpersSuite{})

type AttributesData struct {
	attrs map[string]interface{}
}

func newAttributesData(input map[string]interface{}) *AttributesData {
	return &AttributesData{
		attrs: input,
	}
}

func (attrs *AttributesData) SetStaticAttr(key string, value interface{}) {
	panic("not implemented")
}

func (attrs *AttributesData) StaticAttr(key string) (interface{}, error) {
	panic("not implemented")
}

func (attrs *AttributesData) StaticAttrs() map[string]interface{} {
	panic("not implemented")
}

func (attrs *AttributesData) Attr(key string) (interface{}, error) {
	if val, ok := attrs.attrs[key]; ok {
		return val, nil
	}
	return nil, fmt.Errorf("attribute %q not found", key)
}

func (attrs *AttributesData) Attrs() (map[string]interface{}, error) {
	panic("not implemented")
}

func (attrs *AttributesData) SetAttr(key string, value interface{}) error {
	panic("not implemented")
}

func (s *helpersSuite) TestNestedGet(c *C) {
	_, err := policy.NestedGet("slot", newAttributesData(nil), "a")
	c.Check(err, ErrorMatches, `slot attribute "a" not found`)

	_, err = policy.NestedGet("plug", newAttributesData(map[string]interface{}{
		"a": "123",
	}), "a.b")
	c.Check(err, ErrorMatches, `plug attribute "a\.b" not found`)

	v, err := policy.NestedGet("slot", newAttributesData(map[string]interface{}{
		"a": "123",
	}), "a")
	c.Check(err, IsNil)
	c.Check(v, Equals, "123")

	v, err = policy.NestedGet("slot", newAttributesData(map[string]interface{}{
		"a": map[string]interface{}{
			"b": []interface{}{"1", "2", "3"},
		},
	}), "a.b")
	c.Check(err, IsNil)
	c.Check(v, DeepEquals, []interface{}{"1", "2", "3"})
}
