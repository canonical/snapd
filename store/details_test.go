// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2018 Canonical Ltd
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

package store_test

import (
	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/store"
)

type detailsSuite struct{}

var _ = check.Suite(detailsSuite{})

func (detailsSuite) TestStructFields(c *check.C) {
	type aStruct struct {
		Foo int `json:"hello"`
		Bar int `json:"potato,stuff"`
	}
	c.Assert(store.GetStructFields((*aStruct)(nil)), check.DeepEquals, []string{"hello", "potato"})
}

func (detailsSuite) TestStructFieldsExcept(c *check.C) {
	type aStruct struct {
		Foo int `json:"hello"`
		Bar int `json:"potato,stuff"`
	}
	c.Assert(store.GetStructFields((*aStruct)(nil), "potato"), check.DeepEquals, []string{"hello"})
	c.Assert(store.GetStructFields((*aStruct)(nil), "hello"), check.DeepEquals, []string{"potato"})
}

func (detailsSuite) TestStructFieldsSurvivesNoTag(c *check.C) {
	type aStruct struct {
		Foo int `json:"hello"`
		Bar int
	}
	c.Assert(store.GetStructFields((*aStruct)(nil)), check.DeepEquals, []string{"hello"})
}
