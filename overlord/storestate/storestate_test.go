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

package storestate_test

import (
	"testing"

	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storestate"
	"github.com/snapcore/snapd/store"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type storeStateSuite struct {
	state *state.State
}

var _ = Suite(&storeStateSuite{})

func (ss *storeStateSuite) SetUpTest(c *C) {
	ss.state = state.New(nil)
}

func (ss *storeStateSuite) TestDefaultAPI(c *C) {
	ss.state.Lock()
	api := storestate.API(ss.state)
	ss.state.Unlock()
	c.Check(api, Equals, "")
}

func (ss *storeStateSuite) TestExplicitAPI(c *C) {
	ss.state.Lock()
	defer ss.state.Unlock()

	storeState := storestate.StoreState{API: "http://example.com/"}
	ss.state.Set("store", &storeState)
	api := storestate.API(ss.state)
	c.Check(api, Equals, storeState.API)
}

func (ss *storeStateSuite) TestStore(c *C) {
	ss.state.Lock()
	defer ss.state.Unlock()

	sto := &store.Store{}
	storestate.ReplaceStore(ss.state, sto)
	store1 := storestate.Store(ss.state)
	c.Check(store1, Equals, sto)

	// cached
	store2 := storestate.Store(ss.state)
	c.Check(store2, Equals, sto)
}
