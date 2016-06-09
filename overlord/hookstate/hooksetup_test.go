// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package hookstate

import (
	"encoding/json"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/snap"
)

func TestHooks(t *testing.T) { TestingT(t) }

type hooksSuite struct{}

var _ = Suite(&hooksSuite{})

func (s *hooksSuite) TestJsonMarshalHookSetup(c *C) {
	hookSetup := newHookSetup("snap-name", snap.R(1), "hook-name")
	out, err := json.Marshal(hookSetup)
	c.Assert(err, IsNil)
	c.Check(string(out), Equals, "{\"snap\":\"snap-name\",\"revision\":\"1\",\"hook\":\"hook-name\"}")
}

func (s *hooksSuite) TestJsonUnmarshalHookSetup(c *C) {
	out, err := json.Marshal(newHookSetup("snap-name", snap.R(1), "hook-name"))
	c.Assert(err, IsNil)

	var setup hookSetup
	err = json.Unmarshal(out, &setup)
	c.Assert(err, IsNil)
	c.Check(setup.Snap, Equals, "snap-name")
	c.Check(setup.Revision, Equals, snap.R(1))
	c.Check(setup.Hook, Equals, "hook-name")
}
