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

package configstate_test

import (
	"github.com/snapcore/snapd/overlord/configstate"

	. "gopkg.in/check.v1"
)

func (s *miscSuite) TestSortPatchKeysEmpty(c *C) {
	patch := map[string]interface{}{}
	keys := configstate.SortPatchKeys(patch)
	c.Assert(keys, IsNil)
}

func (s *miscSuite) TestSortPatchKeys(c *C) {
	patch := map[string]interface{}{
		"a.b.c":         0,
		"a":             0,
		"a.b.c.d":       0,
		"q.w.e.r.t.y.u": 0,
		"f.g":           0,
	}

	keys := configstate.SortPatchKeys(patch)
	c.Assert(keys, DeepEquals, []string{"a", "f.g", "a.b.c", "a.b.c.d", "q.w.e.r.t.y.u"})
}
