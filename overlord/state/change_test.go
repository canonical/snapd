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

package state_test

import (
	. "gopkg.in/check.v1"

	"github.com/ubuntu-core/snappy/overlord/state"
)

type changeSuite struct{}

var _ = Suite(&changeSuite{})

func (cs *changeSuite) TestNewChange(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "summary...")
	c.Check(chg.Kind(), Equals, "install")
	c.Check(chg.Summary(), Equals, "summary...")
}

func (cs *changeSuite) TestGetSet(c *C) {
	st := state.New(nil)
	st.Lock()
	defer st.Unlock()

	chg := st.NewChange("install", "...")

	chg.Set("a", 1)

	var v int
	err := chg.Get("a", &v)
	c.Assert(err, IsNil)
	c.Check(v, Equals, 1)
}

func (cs *changeSuite) TestGetNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install", "...")
	st.Unlock()

	var v int
	c.Assert(func() { chg.Get("a", &v) }, PanicMatches, "internal error: accessing state without lock")
}

func (cs *changeSuite) TestSetNeedsLock(c *C) {
	st := state.New(nil)
	st.Lock()
	chg := st.NewChange("install", "...")
	st.Unlock()

	c.Assert(func() { chg.Set("a", 1) }, PanicMatches, "internal error: accessing state without lock")
}
