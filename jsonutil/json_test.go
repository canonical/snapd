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

package jsonutil_test

import (
	"encoding/json"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/jsonutil"
)

func Test(t *testing.T) { TestingT(t) }

type utilSuite struct{}

var _ = Suite(&utilSuite{})

func (s *utilSuite) TestDecodeError(c *C) {
	input := "{]"
	var output interface{}
	mylog.Check(jsonutil.DecodeWithNumber(strings.NewReader(input), &output))
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `invalid character ']' looking for beginning of object key string`)
}

func (s *utilSuite) TestDecodeErrorOnExcessData(c *C) {
	input := "1000000000[1,2]"
	var output interface{}
	mylog.Check(jsonutil.DecodeWithNumber(strings.NewReader(input), &output))
	c.Assert(err, NotNil)
	c.Assert(err, ErrorMatches, `cannot parse json value`)
}

func (s *utilSuite) TestDecodeSuccess(c *C) {
	input := `{"a":1000000000, "b": 1.2, "c": "foo", "d":null}`
	var output interface{}
	mylog.Check(jsonutil.DecodeWithNumber(strings.NewReader(input), &output))

	c.Assert(output, DeepEquals, map[string]interface{}{
		"a": json.Number("1000000000"),
		"b": json.Number("1.2"),
		"c": "foo",
		"d": nil,
	})
}

func (utilSuite) TestStructFields(c *C) {
	type aStruct struct {
		Foo int `json:"hello"`
		Bar int `json:"potato,stuff"`
	}
	c.Assert(jsonutil.StructFields((*aStruct)(nil)), DeepEquals, []string{"hello", "potato"})
}

func (utilSuite) TestStructFieldsExcept(c *C) {
	type aStruct struct {
		Foo int `json:"hello"`
		Bar int `json:"potato,stuff"`
	}
	c.Assert(jsonutil.StructFields((*aStruct)(nil), "potato"), DeepEquals, []string{"hello"})
	c.Assert(jsonutil.StructFields((*aStruct)(nil), "hello"), DeepEquals, []string{"potato"})
}

func (utilSuite) TestStructFieldsSurvivesNoTag(c *C) {
	type aStruct struct {
		Foo int `json:"hello"`
		Bar int
	}
	c.Assert(jsonutil.StructFields((*aStruct)(nil)), DeepEquals, []string{"hello"})
}
