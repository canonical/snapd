// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2020 Canonical Ltd
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

package daemon_test

import (
	"fmt"
	"net/http"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/store"
)

var _ = check.Suite(&findSuite{})

type findSuite struct {
	apiBaseSuite
}

func (s *findSuite) TestFind(c *check.C) {
	s.daemon(c)

	s.suggestedCurrency = "EUR"

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}

	req, err := http.NewRequest("GET", "/v2/find?q=hi", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(snaps[0]["prices"], check.IsNil)
	c.Check(snaps[0]["channels"], check.IsNil)

	c.Check(rsp.SuggestedCurrency, check.Equals, "EUR")

	c.Check(s.storeSearch, check.DeepEquals, store.Search{Query: "hi"})
	c.Check(s.currentSnaps, check.HasLen, 0)
	c.Check(s.actions, check.HasLen, 0)
}

func (s *findSuite) TestFindRefreshes(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}
	s.mockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?select=refresh", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(s.currentSnaps, check.HasLen, 1)
	c.Check(s.actions, check.HasLen, 1)
}

func (s *findSuite) TestFindRefreshSideloaded(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}

	s.mockSnap(c, "name: store\nversion: 1.0")

	var snapst snapstate.SnapState
	st := d.Overlord().State()
	st.Lock()
	err := snapstate.Get(st, "store", &snapst)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Assert(snapst.Sequence, check.HasLen, 1)

	// clear the snapid
	snapst.Sequence[0].SnapID = ""
	st.Lock()
	snapstate.Set(st, "store", &snapst)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/find?select=refresh", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 0)
	c.Check(s.currentSnaps, check.HasLen, 0)
	c.Check(s.actions, check.HasLen, 0)
}

func (s *findSuite) TestFindPrivate(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{}

	req, err := http.NewRequest("GET", "/v2/find?q=foo&select=private", nil)
	c.Assert(err, check.IsNil)

	_ = s.req(c, req, nil).(*daemon.Resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{
		Query:   "foo",
		Private: true,
	})
}

func (s *findSuite) TestFindUserAgentContextCreated(c *check.C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/find", nil)
	c.Assert(err, check.IsNil)
	req.Header.Add("User-Agent", "some-agent/1.0")

	_ = s.req(c, req, nil).(*daemon.Resp)

	c.Check(store.ClientUserAgent(s.ctx), check.Equals, "some-agent/1.0")
}

func (s *findSuite) TestFindOneUserAgentContextCreated(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SnapType: snap.TypeApp,
		Version:  "v2",
		SideInfo: snap.SideInfo{
			RealName: "banana",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}
	req, err := http.NewRequest("GET", "/v2/find?name=foo", nil)
	c.Assert(err, check.IsNil)
	req.Header.Add("User-Agent", "some-agent/1.0")

	_ = s.req(c, req, nil).(*daemon.Resp)

	c.Check(store.ClientUserAgent(s.ctx), check.Equals, "some-agent/1.0")
}

func (s *findSuite) TestFindPrefix(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{}

	req, err := http.NewRequest("GET", "/v2/find?name=foo*", nil)
	c.Assert(err, check.IsNil)

	_ = s.req(c, req, nil).(*daemon.Resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{Query: "foo", Prefix: true})
}

func (s *findSuite) TestFindSection(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{}

	req, err := http.NewRequest("GET", "/v2/find?q=foo&section=bar", nil)
	c.Assert(err, check.IsNil)

	_ = s.req(c, req, nil).(*daemon.Resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{
		Query:    "foo",
		Category: "bar",
	})
}

func (s *findSuite) TestFindScope(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{}

	req, err := http.NewRequest("GET", "/v2/find?q=foo&scope=creep", nil)
	c.Assert(err, check.IsNil)

	_ = s.req(c, req, nil).(*daemon.Resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{
		Query: "foo",
		Scope: "creep",
	})
}

func (s *findSuite) TestFindCommonID(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
		CommonIDs: []string{"org.foo"},
	}}
	s.mockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?name=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["common-ids"], check.DeepEquals, []interface{}{"org.foo"})
}

func (s *findSuite) TestFindByCommonID(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
		CommonIDs: []string{"org.foo"},
	}}
	s.mockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?common-id=org.foo", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(s.storeSearch, check.DeepEquals, store.Search{CommonID: "org.foo"})
}

func (s *findSuite) TestFindOne(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "store",
		},
		Base: "base0",
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "verified",
		},
		Channels: map[string]*snap.ChannelSnapInfo{
			"stable": {
				Revision: snap.R(42),
			},
		},
	}}
	s.mockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?name=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["name"], check.Equals, "store")
	c.Check(snaps[0]["base"], check.Equals, "base0")
	c.Check(snaps[0]["publisher"], check.DeepEquals, map[string]interface{}{
		"id":           "foo-id",
		"username":     "foo",
		"display-name": "Foo",
		"validation":   "verified",
	})
	m := snaps[0]["channels"].(map[string]interface{})["stable"].(map[string]interface{})

	c.Check(m["revision"], check.Equals, "42")
}

func (s *findSuite) TestFindOneNotFound(c *check.C) {
	s.daemon(c)

	s.err = store.ErrSnapNotFound
	s.mockSnap(c, "name: store\nversion: 1.0")

	req, err := http.NewRequest("GET", "/v2/find?name=foo", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{})
	c.Check(rsp.Status, check.Equals, 404)
}

func (s *findSuite) TestFindOneWithAuth(c *check.C) {
	d := s.daemon(c)

	state := d.Overlord().State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/find?q=name:gfoo", nil)
	c.Assert(err, check.IsNil)

	c.Assert(s.user, check.IsNil)

	_, ok := s.req(c, req, user).(*daemon.Resp)
	c.Assert(ok, check.Equals, true)
	// ensure user was set
	c.Assert(s.user, check.DeepEquals, user)
}

func (s *findSuite) TestFindRefreshNotOther(c *check.C) {
	s.daemon(c)

	for _, other := range []string{"name", "q", "common-id"} {
		req, err := http.NewRequest("GET", "/v2/find?select=refresh&"+other+"=foo*", nil)
		c.Assert(err, check.IsNil)

		rsp := s.req(c, req, nil).(*daemon.Resp)
		c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError)
		c.Check(rsp.Status, check.Equals, 400)
		c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Equals, "cannot use '"+other+"' with 'select=refresh'")
	}
}

func (s *findSuite) TestFindNotTogether(c *check.C) {
	s.daemon(c)

	queries := map[string]string{"q": "foo", "name": "foo*", "common-id": "foo"}
	for ki, vi := range queries {
		for kj, vj := range queries {
			if ki == kj {
				continue
			}

			req, err := http.NewRequest("GET", fmt.Sprintf("/v2/find?%s=%s&%s=%s", ki, vi, kj, vj), nil)
			c.Assert(err, check.IsNil)

			rsp := s.req(c, req, nil).(*daemon.Resp)
			c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError)
			c.Check(rsp.Status, check.Equals, 400)
			exp1 := "cannot use '" + ki + "' and '" + kj + "' together"
			exp2 := "cannot use '" + kj + "' and '" + ki + "' together"
			c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Matches, exp1+"|"+exp2)
		}
	}
}

func (s *findSuite) TestFindBadQueryReturnsCorrectErrorKind(c *check.C) {
	s.daemon(c)

	s.err = store.ErrBadQuery
	req, err := http.NewRequest("GET", "/v2/find?q=return-bad-query-please", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, check.Matches, "bad query")
	c.Check(rsp.Result.(*daemon.ErrorResult).Kind, check.Equals, client.ErrorKindBadQuery)
}

func (s *findSuite) TestFindPriced(c *check.C) {
	s.daemon(c)

	s.suggestedCurrency = "GBP"

	s.rsnaps = []*snap.Info{{
		SnapType: snap.TypeApp,
		Version:  "v2",
		Prices: map[string]float64{
			"GBP": 1.23,
			"EUR": 2.34,
		},
		MustBuy: true,
		SideInfo: snap.SideInfo{
			RealName: "banana",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}

	req, err := http.NewRequest("GET", "/v2/find?q=banana&channel=stable", nil)
	c.Assert(err, check.IsNil)
	rsp, ok := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(ok, check.Equals, true)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)

	snap := snaps[0]
	c.Check(snap["name"], check.Equals, "banana")
	c.Check(snap["prices"], check.DeepEquals, map[string]interface{}{
		"EUR": 2.34,
		"GBP": 1.23,
	})
	c.Check(snap["status"], check.Equals, "priced")

	c.Check(rsp.SuggestedCurrency, check.Equals, "GBP")
}

func (s *findSuite) TestFindScreenshotted(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SnapType: snap.TypeApp,
		Version:  "v2",
		Media: []snap.MediaInfo{
			{
				Type:   "screenshot",
				URL:    "http://example.com/screenshot.png",
				Width:  800,
				Height: 1280,
			},
			{
				Type: "screenshot",
				URL:  "http://example.com/screenshot2.png",
			},
		},
		MustBuy: true,
		SideInfo: snap.SideInfo{
			RealName: "test-screenshot",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}

	req, err := http.NewRequest("GET", "/v2/find?q=test-screenshot", nil)
	c.Assert(err, check.IsNil)
	rsp, ok := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(ok, check.Equals, true)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)

	c.Check(snaps[0]["name"], check.Equals, "test-screenshot")
	c.Check(snaps[0]["media"], check.DeepEquals, []interface{}{
		map[string]interface{}{
			"type":   "screenshot",
			"url":    "http://example.com/screenshot.png",
			"width":  float64(800),
			"height": float64(1280),
		},
		map[string]interface{}{
			"type": "screenshot",
			"url":  "http://example.com/screenshot2.png",
		},
	})
}

func (s *findSuite) TestSnapsStoreConfinement(c *check.C) {
	s.daemon(c)

	s.rsnaps = []*snap.Info{
		{
			// no explicit confinement in this one
			SideInfo: snap.SideInfo{
				RealName: "foo",
			},
		},
		{
			Confinement: snap.StrictConfinement,
			SideInfo: snap.SideInfo{
				RealName: "bar",
			},
		},
		{
			Confinement: snap.DevModeConfinement,
			SideInfo: snap.SideInfo{
				RealName: "baz",
			},
		},
	}

	req, err := http.NewRequest("GET", "/v2/find", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 3)

	for i, ss := range [][2]string{
		{"foo", string(snap.StrictConfinement)},
		{"bar", string(snap.StrictConfinement)},
		{"baz", string(snap.DevModeConfinement)},
	} {
		name, mode := ss[0], ss[1]
		c.Check(snaps[i]["name"], check.Equals, name, check.Commentf(name))
		c.Check(snaps[i]["confinement"], check.Equals, mode, check.Commentf(name))
	}
}
