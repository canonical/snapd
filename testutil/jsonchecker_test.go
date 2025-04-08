// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2021 Canonical Ltd
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

package testutil_test

import (
	"gopkg.in/check.v1"

	. "github.com/snapcore/snapd/testutil"
)

type jsonCheckerSuite struct{}

var _ = check.Suite(&jsonCheckerSuite{})

func (*jsonCheckerSuite) TestFilePresent(c *check.C) {
	testInfo(c, JsonEquals, "JsonEqual", []string{"obtained", "expected"})

	type subRef struct {
		Foo    string    `json:"foo"`
		Baz    []float32 `json:"baz"`
		hidden bool
	}
	type ref struct {
		Number int      `json:"number"`
		String string   `json:"string,omitempty"`
		List   []string `json:"list"`
		Map    *subRef  `json:"map,omitempty"`
		hidden bool
	}

	testCheck(c, JsonEquals, true, "",
		map[string]any{
			"number": 42,
			"string": "foo",
			"list":   []string{"123", "456"},
			"map": map[string]any{
				"foo": "bar",
				"baz": []any{0.2, 0.3},
			},
		}, ref{
			Number: 42,
			String: "foo",
			List:   []string{"123", "456"},
			Map: &subRef{
				Foo: "bar",
				Baz: []float32{0.2, 0.3},
				// this is transparent to the checker
				hidden: true,
			},
			// this is transparent to the checker
			hidden: true,
		})
	testCheck(c, JsonEquals, true, "",
		map[string]any{
			"number": 42,
			"string": "foo",
			"list":   []any(nil),
		}, ref{
			Number: 42,
			String: "foo",
		})
	testCheck(c, JsonEquals, true, "",
		map[string]any{
			"number": 42,
			"list":   []any(nil),
		}, ref{
			Number: 42,
		})
	testCheck(c, JsonEquals, false, "", 12, "12")
	testCheck(c, JsonEquals, false, "Difference:\n...     [0]: 12 != 24\n", []int{12}, []int{24})
	testCheck(c, JsonEquals, false, "Difference:\n...     [0]: string != float64\n", []string{"abc"}, []int{24})
}
