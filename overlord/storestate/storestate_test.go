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
	"io/ioutil"
	"net/url"
	"os"
	"path/filepath"
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

type storeStateSuite struct{}

var _ = Suite(&storeStateSuite{})

func (ss *storeStateSuite) state(c *C, data string) *state.State {
	if data == "" {
		return state.New(nil)
	}

	tmpdir := c.MkDir()
	state_json := filepath.Join(tmpdir, "state.json")
	c.Assert(ioutil.WriteFile(state_json, []byte(data), 0600), IsNil)

	r, err := os.Open(state_json)
	c.Assert(err, IsNil)
	defer r.Close()
	st, err := state.ReadState(nil, r)
	c.Assert(err, IsNil)

	return st
}

func (ss *storeStateSuite) TestDefaultAPI(c *C) {
	st := ss.state(c, "")
	st.Lock()
	api := storestate.API(st)
	st.Unlock()
	c.Check(api, Equals, "")
}

func (ss *storeStateSuite) TestExplicitAPI(c *C) {
	st := ss.state(c, "")
	st.Lock()
	defer st.Unlock()

	storeState := storestate.StoreState{API: "http://example.com/"}
	st.Set("store", &storeState)
	api := storestate.API(st)
	c.Check(api, Equals, storeState.API)
}

func (ss *storeStateSuite) TestSetupStoreCachesState(c *C) {
	st := ss.state(c, "")
	st.Lock()
	defer st.Unlock()

	c.Check(func() { storestate.Store(st) }, PanicMatches,
		"internal error: needing the store before managers have initialized it")
	c.Check(func() { storestate.AuthContextTestsOnly(st) }, PanicMatches,
		"internal error: needing the auth context before managers have initialized it")

	err := storestate.SetupStore(st, &fakeAuthContext{})
	c.Assert(err, IsNil)

	c.Check(storestate.Store(st), NotNil)
	c.Check(storestate.AuthContextTestsOnly(st), NotNil)
}

func (ss *storeStateSuite) TestSetupStoreDefaultAPI(c *C) {
	var config *store.Config
	defer storestate.MockStoreNew(func(c *store.Config, _ auth.AuthContext) *store.Store {
		config = c
		return nil
	})()

	st := ss.state(c, "")
	st.Lock()
	defer st.Unlock()

	err := storestate.SetupStore(st, nil)
	c.Assert(err, IsNil)

	c.Check(config.SearchURI.Host, Equals, "api.snapcraft.io")
}

func (ss *storeStateSuite) TestSetupStoreAPIFromState(c *C) {
	var config *store.Config
	defer storestate.MockStoreNew(func(c *store.Config, _ auth.AuthContext) *store.Store {
		config = c
		return nil
	})()

	st := ss.state(c, `{"data":{"store":{"api": "http://example.com/"}}}`)
	st.Lock()
	defer st.Unlock()

	err := storestate.SetupStore(st, nil)
	c.Assert(err, IsNil)

	c.Check(config.SearchURI.Host, Equals, "example.com")
}

func (ss *storeStateSuite) TestSetupStoreBadEnvironURLOverride(c *C) {
	// We need store api state to trigger this.
	st := ss.state(c, `{"data":{"store":{"api": "http://example.com/"}}}`)
	st.Lock()
	defer st.Unlock()

	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", "://force-api.local/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_API_URL", "")

	err := storestate.SetupStore(st, nil)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_API_URL: parse ://force-api.local/: missing protocol scheme")
}

func (ss *storeStateSuite) TestSetupStoreEmptyAPIFromState(c *C) {
	var config *store.Config
	defer storestate.MockStoreNew(func(c *store.Config, _ auth.AuthContext) *store.Store {
		config = c
		return nil
	})()

	st := ss.state(c, `{"data":{"store":{"api": ""}}}`)
	st.Lock()
	defer st.Unlock()

	err := storestate.SetupStore(st, nil)
	c.Assert(err, IsNil)

	c.Check(config.SearchURI.Host, Equals, "api.snapcraft.io")
}

func (ss *storeStateSuite) TestSetupStoreInvalidAPIFromState(c *C) {
	st := ss.state(c, `{"data":{"store":{"api": "://example.com/"}}}`)
	st.Lock()
	defer st.Unlock()

	err := storestate.SetupStore(st, nil)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "invalid store API URL: parse ://example.com/: missing protocol scheme")
}

func (ss *storeStateSuite) TestStore(c *C) {
	st := ss.state(c, "")
	st.Lock()
	defer st.Unlock()

	sto := &store.Store{}
	storestate.ReplaceStore(st, sto)
	store1 := storestate.Store(st)
	c.Check(store1, Equals, sto)

	// cached
	store2 := storestate.Store(st)
	c.Check(store2, Equals, sto)
}

func (ss *storeStateSuite) TestReplaceStoreAPI(c *C) {
	st := ss.state(c, "")
	st.Lock()
	defer st.Unlock()

	err := storestate.SetupStore(st, &fakeAuthContext{})
	c.Assert(err, IsNil)

	oldStore := storestate.Store(st)
	c.Assert(storestate.API(st), Equals, "")

	api, err := url.Parse("http://example.com/")
	c.Assert(err, IsNil)
	err = storestate.SetupStoreAPI(st, api)
	c.Assert(err, IsNil)

	c.Check(storestate.Store(st), Not(Equals), oldStore)
	c.Check(storestate.API(st), Equals, "http://example.com/")
}

func (ss *storeStateSuite) TestReplaceStoreAPIReset(c *C) {
	st := ss.state(c, "")
	st.Lock()
	defer st.Unlock()

	st.Set("store", map[string]interface{}{
		"api": "http://example.com/",
	})
	c.Assert(storestate.API(st), Not(Equals), "")

	err := storestate.SetupStore(st, &fakeAuthContext{})
	c.Assert(err, IsNil)
	oldStore := storestate.Store(st)

	err = storestate.SetupStoreAPI(st, nil)
	c.Assert(err, IsNil)

	c.Check(storestate.Store(st), Not(Equals), oldStore)
	c.Check(storestate.API(st), Equals, "")
}

func (ss *storeStateSuite) TestReplaceStoreAPIBadEnvironURLOverride(c *C) {
	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", "://force-api.local/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_API_URL", "")

	api, _ := url.Parse("http://example.com/")
	st := ss.state(c, "")
	err := storestate.SetupStoreAPI(st, api)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_API_URL: parse ://force-api.local/: missing protocol scheme")
}
