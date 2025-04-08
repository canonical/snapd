// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2016 Canonical Ltd
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

package metautil_test

import (
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/metautil"
)

type normalizeTestSuite struct {
}

var _ = Suite(&normalizeTestSuite{})

func TestMain(t *testing.T) { TestingT(t) }

func (s *normalizeTestSuite) TestNormalize(c *C) {
	for _, tc := range []struct {
		v   any
		exp any
		err string
	}{
		{v: "foo", exp: "foo"},
		{v: 1, exp: int64(1)},
		{v: int64(1), exp: int64(1)},
		{v: true, exp: true},
		{v: 0.5, exp: float64(0.5)},
		{v: float32(0.5), exp: float64(0.5)},
		{v: float64(0.5), exp: float64(0.5)},
		{v: []any{1, 0.5, "foo"}, exp: []any{int64(1), float64(0.5), "foo"}},
		{v: map[string]any{"foo": 1}, exp: map[string]any{"foo": int64(1)}},
		{v: map[any]any{"foo": 1}, exp: map[string]any{"foo": int64(1)}},
		{
			v:   map[any]any{"foo": map[any]any{"bar": 0.5}},
			exp: map[string]any{"foo": map[string]any{"bar": float64(0.5)}},
		},
		{v: uint(1), err: "invalid scalar: 1"},
		{v: map[any]any{2: 1}, err: "non-string key: 2"},
		{v: []any{uint(1)}, err: "invalid scalar: 1"},
		{v: map[string]any{"foo": uint(1)}, err: "invalid scalar: 1"},
		{v: map[any]any{"foo": uint(1)}, err: "invalid scalar: 1"},
	} {
		res, err := metautil.NormalizeValue(tc.v)
		if tc.err == "" {
			c.Assert(err, IsNil)
			c.Assert(res, DeepEquals, tc.exp)
		} else {
			c.Assert(err, ErrorMatches, tc.err)
		}
	}
}
