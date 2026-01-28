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
	c.Assert(confValues, DeepEquals, map[string]any{
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
	c.Assert(confValues, DeepEquals, map[string]any{
		"foo": map[string]any{
			"bar": json.Number("1"),
		},
	})
	c.Assert(keys, DeepEquals, []string{"foo"})

	// stores strings w/o parsing
	opts.String = true
	confValues, keys, err = clientutil.ParseConfigValues([]string{`foo={"bar": 1}`}, opts)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]any{
		"foo": `{"bar": 1}`,
	})
	c.Assert(keys, DeepEquals, []string{"foo"})

	// default is to parse
	confValues, keys, err = clientutil.ParseConfigValues([]string{`foo={"bar": 1}`}, nil)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]any{
		"foo": map[string]any{
			"bar": json.Number("1"),
		},
	})
	c.Assert(keys, DeepEquals, []string{"foo"})

	// unless it's not valid JSON
	confValues, keys, err = clientutil.ParseConfigValues([]string{`foo={"bar": 1`}, nil)
	c.Assert(err, IsNil)
	c.Assert(confValues, DeepEquals, map[string]any{
		"foo": `{"bar": 1`,
	})
	c.Assert(keys, DeepEquals, []string{"foo"})
}

func (s *parseSuite) TestParseConfigValuesEmptyKey(c *C) {
	_, _, err := clientutil.ParseConfigValues([]string{""}, nil)
	c.Assert(err, ErrorMatches, `invalid configuration: "" \(want key=value\)`)

	_, _, err = clientutil.ParseConfigValues([]string{"=value"}, nil)
	c.Assert(err, ErrorMatches, `configuration keys cannot be empty`)

	_, _, err = clientutil.ParseConfigValues([]string{"!"}, nil)
	c.Assert(err, ErrorMatches, `configuration keys cannot be empty \(use key! to unset a key\)`)
}

func (s *parseSuite) TestParseConfdbConstraints(c *C) {
	type testcase struct {
		with []string
		opts clientutil.ConfdbOptions
		res  any
		err  string
	}

	tcs := []testcase{
		{
			res: map[string]any(nil),
		},
		{
			with: []string{"foo"},
			err:  `--with constraints must be in the form <param>=<constraint> but got "foo" instead`,
		},
		{
			with: []string{"foo="},
			err:  `--with constraints must be in the form <param>=<constraint> but got "foo=" instead`,
		},
		{
			with: []string{"="},
			err:  `--with constraints must be in the form <param>=<constraint> but got "=" instead`,
		},
		{
			with: []string{"foo=bar"},
			res: map[string]any{
				"foo": "bar",
			},
		},
		{
			with: []string{"foo=1"},
			res: map[string]any{
				"foo": float64(1),
			},
		},
		{
			with: []string{"foo=true"},
			res: map[string]any{
				"foo": true,
			},
		},
		{
			with: []string{`foo=["a","b"]`},
			res: map[string]any{
				// defaults to treating the array as string
				"foo": `["a","b"]`,
			},
		},

		{
			with: []string{`foo={"a":"b"}`},
			res: map[string]any{
				"foo": `{"a":"b"}`,
			},
		},
		{
			with: []string{`foo=["a","b"]`},
			opts: clientutil.ConfdbOptions{Typed: true},
			err:  `--with constraints cannot take non-scalar JSON constraint: \["a","b"\]`,
		},
		{
			with: []string{`foo={"a":"b"}`},
			opts: clientutil.ConfdbOptions{Typed: true},
			err:  `--with constraints cannot take non-scalar JSON constraint: {"a":"b"}`,
		},
		{
			with: []string{`foo=[asds}`},
			opts: clientutil.ConfdbOptions{Typed: true},
			err:  `cannot unmarshal constraint as JSON as required by -t flag: \[asds}`,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("testcase %d/%d", i+1, len(tcs))
		constraints, err := clientutil.ParseConfdbConstraints(tc.with, tc.opts)

		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Assert(constraints, DeepEquals, tc.res, cmt)
		}
	}
}
