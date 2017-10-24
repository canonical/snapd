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

	. "gopkg.in/check.v1"
)

func Test(t *testing.T) { TestingT(t) }

type storeStateSuite struct{}

var _ = Suite(&storeStateSuite{})

/*
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

func (ss *storeStateSuite) TestDefaultBaseURL(c *C) {
	st := ss.state(c, "")
	st.Lock()
	baseURL := storestate.BaseURL(st)
	st.Unlock()
	c.Check(baseURL, Equals, "")
}

func (ss *storeStateSuite) TestExplicitBaseURL(c *C) {
	st := ss.state(c, "")
	st.Lock()
	defer st.Unlock()

	storeState := storestate.StoreState{BaseURL: "http://example.com/"}
	st.Set("store", &storeState)
	baseURL := storestate.BaseURL(st)
	c.Check(baseURL, Equals, storeState.BaseURL)
}

func (ss *storeStateSuite) TestSetBaseURL(c *C) {
	st := ss.state(c, "")
	st.Lock()
	defer st.Unlock()

	err := storestate.SetupStore(st, &fakeAuthContext{})
	c.Assert(err, IsNil)

	oldStore := storestate.Store(st)
	c.Assert(storestate.BaseURL(st), Equals, "")

	u, err := url.Parse("http://example.com/")
	c.Assert(err, IsNil)
	err = storestate.SetBaseURL(st, u)
	c.Assert(err, IsNil)

	c.Check(storestate.Store(st), Not(Equals), oldStore)
	c.Check(storestate.BaseURL(st), Equals, "http://example.com/")
}

func (ss *storeStateSuite) TestSetBaseURLReset(c *C) {
	st := ss.state(c, "")
	st.Lock()
	defer st.Unlock()

	st.Set("store", map[string]interface{}{
		"base-url": "http://example.com/",
	})
	c.Assert(storestate.BaseURL(st), Not(Equals), "")

	err := storestate.SetupStore(st, &fakeAuthContext{})
	c.Assert(err, IsNil)
	oldStore := storestate.Store(st)

	err = storestate.SetBaseURL(st, nil)
	c.Assert(err, IsNil)

	c.Check(storestate.Store(st), Not(Equals), oldStore)
	c.Check(storestate.BaseURL(st), Equals, "")
}

func (ss *storeStateSuite) TestSetBaseURLBadEnvironURLOverride(c *C) {
	c.Assert(os.Setenv("SNAPPY_FORCE_API_URL", "://force-api.local/"), IsNil)
	defer os.Setenv("SNAPPY_FORCE_API_URL", "")

	u, _ := url.Parse("http://example.com/")
	st := ss.state(c, "")
	err := storestate.SetBaseURL(st, u)
	c.Assert(err, NotNil)
	c.Check(err, ErrorMatches, "invalid SNAPPY_FORCE_API_URL: parse ://force-api.local/: missing protocol scheme")
}
*/
