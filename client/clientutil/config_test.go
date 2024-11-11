// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2024 Canonical Ltd
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

package clientutil_test

import (
	"encoding/json"

	"github.com/snapcore/snapd/client/clientutil"
	. "gopkg.in/check.v1"
)

type parseSuite struct{}

var _ = Suite(&parseSuite{})

func (s *parseSuite) TestParseConfigValues(c *C) {
	// check basic setting and unsetting behaviour
	confValues, keys, err := clientutil.ParseConfigValues([]string{"foo=bar", "baz!"}, nil)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]interface{}{
		"foo": "bar",
		"baz": nil,
	})
	c.Assert(keys, DeepEquals, []string{"foo", "baz"})

	// parses JSON
	opts := &clientutil.ParseConfigOptions{
		Typed: true,
	}
	confValues, keys, err = clientutil.ParseConfigValues([]string{`foo={"bar": 1}`}, opts)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]interface{}{
		"foo": map[string]interface{}{
			"bar": json.Number("1"),
		},
	})
	c.Assert(keys, DeepEquals, []string{"foo"})

	// stores strings w/o parsing
	opts.String = true
	confValues, keys, err = clientutil.ParseConfigValues([]string{`foo={"bar": 1}`}, opts)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]interface{}{
		"foo": `{"bar": 1}`,
	})
	c.Assert(keys, DeepEquals, []string{"foo"})

	// default is to parse
	confValues, keys, err = clientutil.ParseConfigValues([]string{`foo={"bar": 1}`}, nil)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]interface{}{
		"foo": map[string]interface{}{
			"bar": json.Number("1"),
		},
	})
	c.Assert(keys, DeepEquals, []string{"foo"})

	// unless it's not valid JSON
	confValues, keys, err = clientutil.ParseConfigValues([]string{`foo={"bar": 1`}, nil)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]interface{}{
		"foo": `{"bar": 1`,
	})
	c.Assert(keys, DeepEquals, []string{"foo"})
}
