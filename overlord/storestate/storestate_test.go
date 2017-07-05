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
	"net/url"
	"os"
	"testing"

	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/overlord/storestate"
	"github.com/snapcore/snapd/store"

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type fakeAuthContext struct{}

func (*fakeAuthContext) Device() (*auth.DeviceState, error) {
	panic("fakeAuthContext Device is not implemented")
}

func (*fakeAuthContext) UpdateDeviceAuth(*auth.DeviceState, string) (*auth.DeviceState, error) {
	panic("fakeAuthContext UpdateDeviceAuth is not implemented")
}

func (*fakeAuthContext) UpdateUserAuth(*auth.UserState, []string) (*auth.UserState, error) {
	panic("fakeAuthContext UpdateUserAuth is not implemented")
}

func (*fakeAuthContext) StoreID(string) (string, error) {
	panic("fakeAuthContext StoreID is not implemented")
}

func (*fakeAuthContext) DeviceSessionRequestParams(nonce string) (*auth.DeviceSessionRequestParams, error) {
	panic("fakeAuthContext DeviceSessionRequestParams is not implemented")
}

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

func (ss *storeStateSuite) TestReplaceStoreAPI(c *C) {
	ss.state.Lock()
	defer ss.state.Unlock()

	oldStore := &store.Store{}
	storestate.ReplaceStore(ss.state, oldStore)
	c.Assert(storestate.API(ss.state), Equals, "")

	storestate.ReplaceAuthContext(ss.state, &fakeAuthContext{})

	api, err := url.Parse("http://example.com/")
	c.Assert(err, IsNil)
	err = storestate.ReplaceStoreAPI(ss.state, api)
	c.Assert(err, IsNil)

	c.Check(storestate.Store(ss.state), Not(Equals), oldStore)
	c.Check(storestate.API(ss.state), Equals, "http://example.com/")
}

func (ss *storeStateSuite) TestReplaceStoreAPIReset(c *C) {
	ss.state.Lock()
	defer ss.state.Unlock()

	oldStore := &store.Store{}
	storestate.ReplaceStore(ss.state, oldStore)
	ss.state.Set("store", map[string]interface{}{
		"api": "http://example.com/",
	})
	c.Assert(storestate.API(ss.state), Not(Equals), "")

	storestate.ReplaceAuthContext(ss.state, &fakeAuthContext{})

	err := storestate.ReplaceStoreAPI(ss.state, nil)
	c.Assert(err, IsNil)

	c.Check(storestate.Store(ss.state), Not(Equals), oldStore)
	c.Check(storestate.API(ss.state), Equals, "")
}

func (ss *storeStateSuite) TestReplaceStoreAPIBadEnvironURLOverride(c *C) {
	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", "://force-api.local/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_API_URL", "")

	api, _ := url.Parse("http://example.com/")
	err := storestate.ReplaceStoreAPI(ss.state, api)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_API_URL: parse ://force-api.local/: missing protocol scheme")
}

func (ss *storeStateSuite) TestAuthContext(c *C) {
	ss.state.Lock()
	defer ss.state.Unlock()

	authContext := &fakeAuthContext{}
	storestate.ReplaceAuthContext(ss.state, authContext)
	c.Check(storestate.AuthContext(ss.state), Equals, authContext)
}
