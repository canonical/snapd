// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2018 Canonical Ltd
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

package utils_test

import (
	"encoding/json"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/interfaces/utils"
)

func Test(t *testing.T) {
	TestingT(t)
}

type utilsSuite struct{}

var _ = Suite(&utilsSuite{})

func (s *utilsSuite) TestNormalizeInterfaceAttributes(c *C) {
	normalize := utils.NormalizeInterfaceAttributes
	c.Assert(normalize(false), Equals, false)
	c.Assert(normalize(nil), Equals, nil)
	c.Assert(normalize(42), Equals, int64(42))
	c.Assert(normalize(3.14), Equals, float64(3.14))
	// Funny that, I noticed it only because of missing test coverage.
	c.Assert(normalize(float32(3.14)), Equals, float64(3.140000104904175))
	c.Assert(normalize("banana"), Equals, "banana")
	c.Assert(normalize([]interface{}{42, 3.14, "banana", json.Number("21"), json.Number("0.5")}), DeepEquals,
		[]interface{}{int64(42), float64(3.14), "banana", int64(21), float64(0.5)})
	c.Assert(normalize(map[string]interface{}{"i": 42, "f": 3.14, "s": "banana"}),
		DeepEquals, map[string]interface{}{"i": int64(42), "f": float64(3.14), "s": "banana"})
	c.Assert(normalize(json.Number("1")), Equals, int64(1))
	c.Assert(normalize(json.Number("2.5")), Equals, float64(2.5))

	// Normalize doesn't mutate slices it is given
	sliceIn := []interface{}{42}
	sliceOut := normalize(sliceIn)
	c.Assert(sliceIn, DeepEquals, []interface{}{42})
	c.Assert(sliceOut, DeepEquals, []interface{}{int64(42)})

	// Normalize doesn't mutate maps it is given
	mapIn := map[string]interface{}{"i": 42}
	mapOut := normalize(mapIn)
	c.Assert(mapIn, DeepEquals, map[string]interface{}{"i": 42})
	c.Assert(mapOut, DeepEquals, map[string]interface{}{"i": int64(42)})
}

func (s *utilsSuite) TestCopyAttributes(c *C) {
	cpattr := utils.CopyAttributes

	attrsIn := map[string]interface{}{"i": 42}
	attrsOut := cpattr(attrsIn)
	attrsIn["i"] = "changed"
	c.Assert(attrsIn, DeepEquals, map[string]interface{}{"i": "changed"})
	c.Assert(attrsOut, DeepEquals, map[string]interface{}{"i": 42})

	attrsIn = map[string]interface{}{"ao": []interface{}{1, 2, 3}}
	attrsOut = cpattr(attrsIn)
	attrsIn["ao"].([]interface{})[1] = "changed"
	c.Assert(attrsIn, DeepEquals, map[string]interface{}{"ao": []interface{}{1, "changed", 3}})
	c.Assert(attrsOut, DeepEquals, map[string]interface{}{"ao": []interface{}{1, 2, 3}})
}
