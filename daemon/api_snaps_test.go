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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	//"io/ioutil"
	//"mime/multipart"
	"net/http"
	//"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/check.v1"

	//"github.com/snapcore/snapd/arch"
	//"github.com/snapcore/snapd/asserts"
	//"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	//"github.com/snapcore/snapd/overlord/assertstate/assertstatetest"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/healthstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	//"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snap"
	//"github.com/snapcore/snapd/snap/channel"
	//"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type snapsSuite struct {
	apiBaseSuite
}

var _ = check.Suite(&snapsSuite{})

func (s *snapsSuite) TestSnapsInfoIntegration(c *check.C) {
	s.checkSnapsInfoIntegration(c, false, nil)
}

func (s *snapsSuite) TestSnapsInfoIntegrationSome(c *check.C) {
	s.checkSnapsInfoIntegration(c, false, []string{"foo", "baz"})
}

func (s *snapsSuite) TestSnapsInfoIntegrationAll(c *check.C) {
	s.checkSnapsInfoIntegration(c, true, nil)
}

func (s *snapsSuite) TestSnapsInfoIntegrationAllSome(c *check.C) {
	s.checkSnapsInfoIntegration(c, true, []string{"foo", "baz"})
}

func snapList(rawSnaps interface{}) []map[string]interface{} {
	snaps := make([]map[string]interface{}, len(rawSnaps.([]*json.RawMessage)))
	for i, raw := range rawSnaps.([]*json.RawMessage) {
		err := json.Unmarshal([]byte(*raw), &snaps[i])
		if err != nil {
			panic(err)
		}
	}
	return snaps
}

func (s *snapsSuite) checkSnapsInfoIntegration(c *check.C, all bool, names []string) {
	d := s.daemon(c)

	type tsnap struct {
		name   string
		dev    string
		ver    string
		rev    int
		active bool

		wanted bool
	}

	tsnaps := []tsnap{
		{name: "foo", dev: "bar", ver: "v0.9", rev: 1},
		{name: "foo", dev: "bar", ver: "v1", rev: 5, active: true},
		{name: "bar", dev: "baz", ver: "v2", rev: 10, active: true},
		{name: "baz", dev: "qux", ver: "v3", rev: 15, active: true},
		{name: "qux", dev: "mip", ver: "v4", rev: 20, active: true},
	}
	numExpected := 0

	for _, snp := range tsnaps {
		if all || snp.active {
			if len(names) == 0 {
				numExpected++
				snp.wanted = true
			}
			for _, n := range names {
				if snp.name == n {
					numExpected++
					snp.wanted = true
					break
				}
			}
		}
		s.mkInstalledInState(c, d, snp.name, snp.dev, snp.ver, snap.R(snp.rev), snp.active, "")
	}

	q := url.Values{}
	if all {
		q.Set("select", "all")
	}
	if len(names) > 0 {
		q.Set("snaps", strings.Join(names, ","))
	}
	req, err := http.NewRequest("GET", "/v2/snaps?"+q.Encode(), nil)
	c.Assert(err, check.IsNil)

	rsp, ok := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(ok, check.Equals, true)

	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeSync)
	c.Check(rsp.Status, check.Equals, 200)
	c.Check(rsp.Result, check.NotNil)

	snaps := snapList(rsp.Result)
	c.Check(snaps, check.HasLen, numExpected)

	for _, s := range tsnaps {
		if !((all || s.active) && s.wanted) {
			continue
		}
		var got map[string]interface{}
		for _, got = range snaps {
			if got["name"].(string) == s.name && got["revision"].(string) == snap.R(s.rev).String() {
				break
			}
		}
		c.Check(got["name"], check.Equals, s.name)
		c.Check(got["version"], check.Equals, s.ver)
		c.Check(got["revision"], check.Equals, snap.R(s.rev).String())
		c.Check(got["developer"], check.Equals, s.dev)
		c.Check(got["confinement"], check.Equals, "strict")
	}
}

func (s *snapsSuite) TestSnapsInfoOnlyLocal(c *check.C) {
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
	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(10), true, "")
	st := d.Overlord().State()
	st.Lock()
	st.Set("health", map[string]healthstate.HealthState{
		"local": {Status: healthstate.OkayStatus},
	})
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/snaps?sources=local", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"local"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "local")
	c.Check(snaps[0]["health"], check.DeepEquals, map[string]interface{}{
		"status":    "okay",
		"revision":  "unset",
		"timestamp": "0001-01-01T00:00:00Z",
	})
}

func (s *snapsSuite) TestSnapsInfoAllMixedPublishers(c *check.C) {
	d := s.daemon(c)

	// the first 'local' is from a 'local' snap
	s.mkInstalledInState(c, d, "local", "", "v1", snap.R(-1), false, "")
	s.mkInstalledInState(c, d, "local", "foo", "v2", snap.R(1), false, "")
	s.mkInstalledInState(c, d, "local", "foo", "v3", snap.R(2), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?select=all", nil)
	c.Assert(err, check.IsNil)
	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(rsp.Type, check.Equals, daemon.ResponseTypeSync)

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 3)

	publisher := map[string]interface{}{
		"id":           "foo-id",
		"username":     "foo",
		"display-name": "Foo",
		"validation":   "unproven",
	}

	c.Check(snaps[0]["publisher"], check.IsNil)
	c.Check(snaps[1]["publisher"], check.DeepEquals, publisher)
	c.Check(snaps[2]["publisher"], check.DeepEquals, publisher)
}

func (s *snapsSuite) TestSnapsInfoAll(c *check.C) {
	d := s.daemon(c)

	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(1), false, "")
	s.mkInstalledInState(c, d, "local", "foo", "v2", snap.R(2), false, "")
	s.mkInstalledInState(c, d, "local", "foo", "v3", snap.R(3), true, "")
	s.mkInstalledInState(c, d, "local_foo", "foo", "v4", snap.R(4), true, "")
	brokenInfo := s.mkInstalledInState(c, d, "local_bar", "foo", "v5", snap.R(5), true, "")
	// make sure local_bar is 'broken'
	err := os.Remove(filepath.Join(brokenInfo.MountDir(), "meta", "snap.yaml"))
	c.Assert(err, check.IsNil)

	expectedHappy := map[string]bool{
		"local":     true,
		"local_foo": true,
		"local_bar": true,
	}
	for _, t := range []struct {
		q        string
		numSnaps int
		typ      daemon.ResponseType
	}{
		{"?select=enabled", 3, "sync"},
		{`?select=`, 3, "sync"},
		{"", 3, "sync"},
		{"?select=all", 5, "sync"},
		{"?select=invalid-field", 0, "error"},
	} {
		c.Logf("trying: %v", t)
		req, err := http.NewRequest("GET", fmt.Sprintf("/v2/snaps%s", t.q), nil)
		c.Assert(err, check.IsNil)
		rsp := s.req(c, req, nil).(*daemon.Resp)
		c.Assert(rsp.Type, check.Equals, t.typ)

		if rsp.Type != "error" {
			snaps := snapList(rsp.Result)
			c.Assert(snaps, check.HasLen, t.numSnaps)
			seen := map[string]bool{}
			for _, s := range snaps {
				seen[s["name"].(string)] = true
			}
			c.Assert(seen, check.DeepEquals, expectedHappy)
		}
	}
}

func (s *snapsSuite) TestSnapsInfoOnlyStore(c *check.C) {
	d := s.daemon(c)

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
	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(10), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=store", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"store"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Assert(snaps[0]["name"], check.Equals, "store")
	c.Check(snaps[0]["prices"], check.IsNil)

	c.Check(rsp.SuggestedCurrency, check.Equals, "EUR")
}

func (s *snapsSuite) TestSnapsInfoStoreWithAuth(c *check.C) {
	d := s.daemon(c)

	state := d.Overlord().State()
	state.Lock()
	user, err := auth.NewUser(state, "username", "email@test.com", "macaroon", []string{"discharge"})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/snaps?sources=store", nil)
	c.Assert(err, check.IsNil)

	c.Assert(s.user, check.IsNil)

	_ = s.req(c, req, user).(*daemon.Resp)

	// ensure user was set
	c.Assert(s.user, check.DeepEquals, user)
}

func (s *snapsSuite) TestSnapsInfoLocalAndStore(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = []*snap.Info{{
		Version: "v42",
		SideInfo: snap.SideInfo{
			RealName: "remote",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(10), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps?sources=local,store", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	// presence of 'store' in sources bounces request over to /find
	c.Assert(rsp.Sources, check.DeepEquals, []string{"store"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["version"], check.Equals, "v42")

	// as does a 'q'
	req, err = http.NewRequest("GET", "/v2/snaps?q=what", nil)
	c.Assert(err, check.IsNil)
	rsp = s.req(c, req, nil).(*daemon.Resp)
	snaps = snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["version"], check.Equals, "v42")

	// otherwise, local only
	req, err = http.NewRequest("GET", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)
	rsp = s.req(c, req, nil).(*daemon.Resp)
	snaps = snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["version"], check.Equals, "v1")
}

func (s *snapsSuite) TestSnapsInfoDefaultSources(c *check.C) {
	d := s.daemon(c)

	s.rsnaps = []*snap.Info{{
		SideInfo: snap.SideInfo{
			RealName: "remote",
		},
		Publisher: snap.StoreAccount{
			ID:          "foo-id",
			Username:    "foo",
			DisplayName: "Foo",
			Validation:  "unproven",
		},
	}}
	s.mkInstalledInState(c, d, "local", "foo", "v1", snap.R(10), true, "")

	req, err := http.NewRequest("GET", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"local"})
	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
}

func (s *snapsSuite) TestSnapsInfoFilterRemote(c *check.C) {
	s.daemon(c)

	s.rsnaps = nil

	req, err := http.NewRequest("GET", "/v2/snaps?q=foo&sources=store", nil)
	c.Assert(err, check.IsNil)

	rsp := s.req(c, req, nil).(*daemon.Resp)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{Query: "foo"})

	c.Assert(rsp.Result, check.NotNil)
}

func (s *snapsSuite) TestPostSnapsVerifyMultiSnapInstruction(c *check.C) {
	s.daemonWithOverlordMockAndStore(c)

	buf := strings.NewReader(`{"action": "install","snaps":["ubuntu-core"]}`)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/json")

	rsp := s.req(c, req, nil).(*daemon.Resp)

	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, testutil.Contains, `cannot install "ubuntu-core", please use "core" instead`)
}

func (s *snapsSuite) TestPostSnapsUnsupportedMultiOp(c *check.C) {
	s.daemonWithOverlordMockAndStore(c)

	buf := strings.NewReader(`{"action": "switch","snaps":["foo"]}`)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/json")

	rsp := s.req(c, req, nil).(*daemon.Resp)

	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, testutil.Contains, `unsupported multi-snap operation "switch"`)
}

func (s *snapsSuite) TestPostSnapsNoWeirdses(c *check.C) {
	s.daemonWithOverlordMockAndStore(c)

	// one could add more actions here ... 🤷
	for _, action := range []string{"install", "refresh", "remove"} {
		for weird, v := range map[string]string{
			"channel":      `"beta"`,
			"revision":     `"1"`,
			"devmode":      "true",
			"jailmode":     "true",
			"cohort-key":   `"what"`,
			"leave-cohort": "true",
			"purge":        "true",
		} {
			buf := strings.NewReader(fmt.Sprintf(`{"action": "%s","snaps":["foo","bar"], "%s": %s}`, action, weird, v))
			req, err := http.NewRequest("POST", "/v2/snaps", buf)
			c.Assert(err, check.IsNil)
			req.Header.Set("Content-Type", "application/json")

			rsp := s.req(c, req, nil).(*daemon.Resp)

			c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError)
			c.Check(rsp.Status, check.Equals, 400)
			c.Check(rsp.Result.(*daemon.ErrorResult).Message, testutil.Contains, `unsupported option provided for multi-snap operation`)
		}
	}
}

func (s *snapsSuite) TestPostSnapsOp(c *check.C) {
	s.testPostSnapsOp(c, "application/json")
}

func (s *snapsSuite) TestPostSnapsOpMoreComplexContentType(c *check.C) {
	s.testPostSnapsOp(c, "application/json; charset=utf-8")
}

func (s *snapsSuite) testPostSnapsOp(c *check.C, contentType string) {
	defer daemon.MockAssertstateRefreshSnapDeclarations(func(*state.State, int) error { return nil })()
	defer daemon.MockSnapstateUpdateMany(func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 0)
		t := s.NewTask("fake-refresh-all", "Refreshing everything")
		return []string{"fake1", "fake2"}, []*state.TaskSet{state.NewTaskSet(t)}, nil
	})()

	d := s.daemonWithOverlordMockAndStore(c)

	buf := bytes.NewBufferString(`{"action": "refresh"}`)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", contentType)

	rsp, ok := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(ok, check.Equals, true)
	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeAsync)

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Check(chg.Summary(), check.Equals, `Refresh snaps "fake1", "fake2"`)
	var apiData map[string]interface{}
	c.Check(chg.Get("api-data", &apiData), check.IsNil)
	c.Check(apiData["snap-names"], check.DeepEquals, []interface{}{"fake1", "fake2"})
}

func (s *snapsSuite) TestPostSnapsOpInvalidCharset(c *check.C) {
	s.daemon(c)

	buf := bytes.NewBufferString(`{"action": "refresh"}`)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/json; charset=iso-8859-1")

	rsp := s.req(c, req, nil).(*daemon.Resp)
	c.Check(rsp.Status, check.Equals, 400)
	c.Check(rsp.Result.(*daemon.ErrorResult).Message, testutil.Contains, "unknown charset in content type")
}

func (s *snapsSuite) TestRefreshAll(c *check.C) {
	refreshSnapDecls := false
	defer daemon.MockAssertstateRefreshSnapDeclarations(func(s *state.State, userID int) error {
		refreshSnapDecls = true
		return assertstate.RefreshSnapDeclarations(s, userID)
	})()

	d := s.daemon(c)

	for _, tst := range []struct {
		snaps []string
		msg   string
	}{
		{nil, "Refresh all snaps: no updates"},
		{[]string{"fake"}, `Refresh snap "fake"`},
		{[]string{"fake1", "fake2"}, `Refresh snaps "fake1", "fake2"`},
	} {
		refreshSnapDecls = false

		defer daemon.MockSnapstateUpdateMany(func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
			c.Check(names, check.HasLen, 0)
			t := s.NewTask("fake-refresh-all", "Refreshing everything")
			return tst.snaps, []*state.TaskSet{state.NewTaskSet(t)}, nil
		})()

		inst := &daemon.SnapInstruction{Action: "refresh"}
		st := d.Overlord().State()
		st.Lock()
		res, err := inst.DispatchForMany()(inst, st)
		st.Unlock()
		c.Assert(err, check.IsNil)
		c.Check(res.Summary, check.Equals, tst.msg)
		c.Check(refreshSnapDecls, check.Equals, true)
	}
}

func (s *snapsSuite) TestRefreshAllNoChanges(c *check.C) {
	refreshSnapDecls := false
	defer daemon.MockAssertstateRefreshSnapDeclarations(func(s *state.State, userID int) error {
		refreshSnapDecls = true
		return assertstate.RefreshSnapDeclarations(s, userID)
	})()

	defer daemon.MockSnapstateUpdateMany(func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 0)
		return nil, nil, nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{Action: "refresh"}
	st := d.Overlord().State()
	st.Lock()
	res, err := inst.DispatchForMany()(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Refresh all snaps: no updates`)
	c.Check(refreshSnapDecls, check.Equals, true)
}

func (s *snapsSuite) TestRefreshMany(c *check.C) {
	refreshSnapDecls := false
	defer daemon.MockAssertstateRefreshSnapDeclarations(func(s *state.State, userID int) error {
		refreshSnapDecls = true
		return nil
	})()

	defer daemon.MockSnapstateUpdateMany(func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 2)
		t := s.NewTask("fake-refresh-2", "Refreshing two")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{Action: "refresh", Snaps: []string{"foo", "bar"}}
	st := d.Overlord().State()
	st.Lock()
	res, err := inst.DispatchForMany()(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Refresh snaps "foo", "bar"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
	c.Check(refreshSnapDecls, check.Equals, true)
}

func (s *snapsSuite) TestRefreshMany1(c *check.C) {
	refreshSnapDecls := false
	defer daemon.MockAssertstateRefreshSnapDeclarations(func(s *state.State, userID int) error {
		refreshSnapDecls = true
		return nil
	})()

	defer daemon.MockSnapstateUpdateMany(func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 1)
		t := s.NewTask("fake-refresh-1", "Refreshing one")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{Action: "refresh", Snaps: []string{"foo"}}
	st := d.Overlord().State()
	st.Lock()
	res, err := inst.DispatchForMany()(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Refresh snap "foo"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
	c.Check(refreshSnapDecls, check.Equals, true)
}

func (s *snapsSuite) TestInstallMany(c *check.C) {
	defer daemon.MockSnapstateInstallMany(func(s *state.State, names []string, userID int) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 2)
		t := s.NewTask("fake-install-2", "Install two")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{Action: "install", Snaps: []string{"foo", "bar"}}
	st := d.Overlord().State()
	st.Lock()
	res, err := inst.DispatchForMany()(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Install snaps "foo", "bar"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
}

func (s *snapsSuite) TestInstallManyEmptyName(c *check.C) {
	defer daemon.MockSnapstateInstallMany(func(_ *state.State, _ []string, _ int) ([]string, []*state.TaskSet, error) {
		return nil, nil, errors.New("should not be called")
	})()
	d := s.daemon(c)
	inst := &daemon.SnapInstruction{Action: "install", Snaps: []string{"", "bar"}}
	st := d.Overlord().State()
	st.Lock()
	res, err := inst.DispatchForMany()(inst, st)
	st.Unlock()
	c.Assert(res, check.IsNil)
	c.Assert(err, check.ErrorMatches, "cannot install snap with empty name")
}

func (s *snapsSuite) TestRemoveMany(c *check.C) {
	defer daemon.MockSnapstateRemoveMany(func(s *state.State, names []string) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 2)
		t := s.NewTask("fake-remove-2", "Remove two")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{Action: "remove", Snaps: []string{"foo", "bar"}}
	st := d.Overlord().State()
	st.Lock()
	res, err := inst.DispatchForMany()(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Remove snaps "foo", "bar"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
}

func (s *snapsSuite) TestSnapInfoOneIntegration(c *check.C) {
	d := s.daemon(c)

	// we have v0 [r5] installed
	s.mkInstalledInState(c, d, "foo", "bar", "v0", snap.R(5), false, "")
	// and v1 [r10] is current
	s.mkInstalledInState(c, d, "foo", "bar", "v1", snap.R(10), true, `title: title
description: description
summary: summary
license: GPL-3.0
base: base18
apps:
  cmd:
    command: some.cmd
  cmd2:
    command: other.cmd
  cmd3:
    command: other.cmd
    common-id: org.foo.cmd
  svc1:
    command: somed1
    daemon: simple
  svc2:
    command: somed2
    daemon: forking
  svc3:
    command: somed3
    daemon: oneshot
  svc4:
    command: somed4
    daemon: notify
  svc5:
    command: some5
    timer: mon1,12:15
    daemon: simple
  svc6:
    command: some6
    daemon: simple
    sockets:
       sock:
         listen-stream: $SNAP_COMMON/run.sock
  svc7:
    command: some7
    daemon: simple
    sockets:
       other-sock:
         listen-stream: $SNAP_COMMON/other-run.sock
`)
	df := s.mkInstalledDesktopFile(c, "foo_cmd.desktop", "[Desktop]\nExec=foo.cmd %U")
	s.SysctlBufs = [][]byte{
		[]byte(`Type=simple
Id=snap.foo.svc1.service
ActiveState=fumbling
UnitFileState=enabled
`),
		[]byte(`Type=forking
Id=snap.foo.svc2.service
ActiveState=active
UnitFileState=disabled
`),
		[]byte(`Type=oneshot
Id=snap.foo.svc3.service
ActiveState=reloading
UnitFileState=static
`),
		[]byte(`Type=notify
Id=snap.foo.svc4.service
ActiveState=inactive
UnitFileState=potatoes
`),
		[]byte(`Type=simple
Id=snap.foo.svc5.service
ActiveState=inactive
UnitFileState=static
`),
		[]byte(`Id=snap.foo.svc5.timer
ActiveState=active
UnitFileState=enabled
`),
		[]byte(`Type=simple
Id=snap.foo.svc6.service
ActiveState=inactive
UnitFileState=static
`),
		[]byte(`Id=snap.foo.svc6.sock.socket
ActiveState=active
UnitFileState=enabled
`),
		[]byte(`Type=simple
Id=snap.foo.svc7.service
ActiveState=inactive
UnitFileState=static
`),
		[]byte(`Id=snap.foo.svc7.other-sock.socket
ActiveState=inactive
UnitFileState=enabled
`),
	}

	var snapst snapstate.SnapState
	st := d.Overlord().State()
	st.Lock()
	st.Set("health", map[string]healthstate.HealthState{
		"foo": {Status: healthstate.OkayStatus},
	})
	err := snapstate.Get(st, "foo", &snapst)
	st.Unlock()
	c.Assert(err, check.IsNil)

	// modify state
	snapst.TrackingChannel = "beta"
	snapst.IgnoreValidation = true
	snapst.CohortKey = "some-long-cohort-key"
	st.Lock()
	snapstate.Set(st, "foo", &snapst)
	st.Unlock()

	req, err := http.NewRequest("GET", "/v2/snaps/foo", nil)
	c.Assert(err, check.IsNil)
	rsp, ok := s.req(c, req, nil).(*daemon.Resp)
	c.Assert(ok, check.Equals, true)

	c.Assert(rsp, check.NotNil)
	c.Assert(rsp.Result, check.FitsTypeOf, &client.Snap{})
	m := rsp.Result.(*client.Snap)

	// installed-size depends on vagaries of the filesystem, just check type
	c.Check(m.InstalledSize, check.FitsTypeOf, int64(0))
	m.InstalledSize = 0
	// ditto install-date
	c.Check(m.InstallDate, check.FitsTypeOf, time.Time{})
	m.InstallDate = time.Time{}

	meta := &daemon.Meta{}
	expected := &daemon.Resp{
		Type:   daemon.ResponseTypeSync,
		Status: 200,
		Result: &client.Snap{
			ID:               "foo-id",
			Name:             "foo",
			Revision:         snap.R(10),
			Version:          "v1",
			Channel:          "stable",
			TrackingChannel:  "beta",
			IgnoreValidation: true,
			Title:            "title",
			Summary:          "summary",
			Description:      "description",
			Developer:        "bar",
			Publisher: &snap.StoreAccount{
				ID:          "bar-id",
				Username:    "bar",
				DisplayName: "Bar",
				Validation:  "unproven",
			},
			Status:      "active",
			Health:      &client.SnapHealth{Status: "okay"},
			Icon:        "/v2/icons/foo/icon",
			Type:        string(snap.TypeApp),
			Base:        "base18",
			Private:     false,
			DevMode:     false,
			JailMode:    false,
			Confinement: string(snap.StrictConfinement),
			TryMode:     false,
			MountedFrom: filepath.Join(dirs.SnapBlobDir, "foo_10.snap"),
			Apps: []client.AppInfo{
				{
					Snap: "foo", Name: "cmd",
					DesktopFile: df,
				}, {
					// no desktop file
					Snap: "foo", Name: "cmd2",
				}, {
					// has AppStream ID
					Snap: "foo", Name: "cmd3",
					CommonID: "org.foo.cmd",
				}, {
					// services
					Snap: "foo", Name: "svc1",
					Daemon:  "simple",
					Enabled: true,
					Active:  false,
				}, {
					Snap: "foo", Name: "svc2",
					Daemon:  "forking",
					Enabled: false,
					Active:  true,
				}, {
					Snap: "foo", Name: "svc3",
					Daemon:  "oneshot",
					Enabled: true,
					Active:  true,
				}, {
					Snap: "foo", Name: "svc4",
					Daemon:  "notify",
					Enabled: false,
					Active:  false,
				}, {
					Snap: "foo", Name: "svc5",
					Daemon:  "simple",
					Enabled: true,
					Active:  false,
					Activators: []client.AppActivator{
						{Name: "svc5", Type: "timer", Active: true, Enabled: true},
					},
				}, {
					Snap: "foo", Name: "svc6",
					Daemon:  "simple",
					Enabled: true,
					Active:  false,
					Activators: []client.AppActivator{
						{Name: "sock", Type: "socket", Active: true, Enabled: true},
					},
				}, {
					Snap: "foo", Name: "svc7",
					Daemon:  "simple",
					Enabled: true,
					Active:  false,
					Activators: []client.AppActivator{
						{Name: "other-sock", Type: "socket", Active: false, Enabled: true},
					},
				},
			},
			Broken:    "",
			Contact:   "",
			License:   "GPL-3.0",
			CommonIDs: []string{"org.foo.cmd"},
			CohortKey: "some-long-cohort-key",
		},
		Meta: meta,
	}

	c.Check(rsp.Result, check.DeepEquals, expected.Result)
}

func (s *snapsSuite) TestSnapInfoNotFound(c *check.C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	c.Check(s.req(c, req, nil).(*daemon.Resp).Status, check.Equals, 404)
}

func (s *snapsSuite) TestSnapInfoNoneFound(c *check.C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	c.Check(s.req(c, req, nil).(*daemon.Resp).Status, check.Equals, 404)
}

func (s *snapsSuite) TestSnapInfoIgnoresRemoteErrors(c *check.C) {
	s.daemon(c)
	s.err = errors.New("weird")

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	rsp := s.req(c, req, nil).(*daemon.Resp)

	c.Check(rsp.Type, check.Equals, daemon.ResponseTypeError)
	c.Check(rsp.Status, check.Equals, 404)
	c.Check(rsp.Result, check.NotNil)
}

func (s *snapsSuite) TestMapLocalFields(c *check.C) {
	media := snap.MediaInfos{
		{
			Type: "screenshot",
			URL:  "https://example.com/shot1.svg",
		}, {
			Type: "icon",
			URL:  "https://example.com/icon.png",
		}, {
			Type: "screenshot",
			URL:  "https://example.com/shot2.svg",
		},
	}

	publisher := snap.StoreAccount{
		ID:          "some-dev-id",
		Username:    "some-dev",
		DisplayName: "Some Developer",
		Validation:  "poor",
	}
	info := &snap.Info{
		SideInfo: snap.SideInfo{
			SnapID:            "some-snap-id",
			RealName:          "some-snap",
			EditedTitle:       "A Title",
			EditedSummary:     "a summary",
			EditedDescription: "the\nlong\ndescription",
			Channel:           "bleeding/edge",
			Contact:           "alice@example.com",
			Revision:          snap.R(7),
			Private:           true,
		},
		InstanceKey: "instance",
		SnapType:    "app",
		Base:        "the-base",
		Version:     "v1.0",
		License:     "MIT",
		Broken:      "very",
		Confinement: "very strict",
		CommonIDs:   []string{"foo", "bar"},
		Media:       media,
		DownloadInfo: snap.DownloadInfo{
			Size:     42,
			Sha3_384: "some-sum",
		},
		Publisher: publisher,
	}

	// make InstallDate work
	c.Assert(os.MkdirAll(info.MountDir(), 0755), check.IsNil)
	c.Assert(os.Symlink("7", filepath.Join(info.MountDir(), "..", "current")), check.IsNil)

	info.Apps = map[string]*snap.AppInfo{
		"foo": {Snap: info, Name: "foo", Command: "foo"},
		"bar": {Snap: info, Name: "bar", Command: "bar"},
	}
	about := daemon.MakeAboutSnap(info, &snapstate.SnapState{
		Active:          true,
		TrackingChannel: "flaky/beta",
		Current:         snap.R(7),
		Flags: snapstate.Flags{
			IgnoreValidation: true,
			DevMode:          true,
			JailMode:         true,
		},
	},
	)

	expected := &client.Snap{
		ID:               "some-snap-id",
		Name:             "some-snap_instance",
		Summary:          "a summary",
		Description:      "the\nlong\ndescription",
		Developer:        "some-dev",
		Publisher:        &publisher,
		Icon:             "https://example.com/icon.png",
		Type:             "app",
		Base:             "the-base",
		Version:          "v1.0",
		Revision:         snap.R(7),
		Channel:          "bleeding/edge",
		TrackingChannel:  "flaky/beta",
		InstallDate:      info.InstallDate(),
		InstalledSize:    42,
		Status:           "active",
		Confinement:      "very strict",
		IgnoreValidation: true,
		DevMode:          true,
		JailMode:         true,
		Private:          true,
		Broken:           "very",
		Contact:          "alice@example.com",
		Title:            "A Title",
		License:          "MIT",
		CommonIDs:        []string{"foo", "bar"},
		MountedFrom:      filepath.Join(dirs.SnapBlobDir, "some-snap_instance_7.snap"),
		Media:            media,
		Apps: []client.AppInfo{
			{Snap: "some-snap_instance", Name: "bar"},
			{Snap: "some-snap_instance", Name: "foo"},
		},
	}
	c.Check(daemon.MapLocal(about, nil), check.DeepEquals, expected)
}

func (s *snapsSuite) TestMapLocalOfTryResolvesSymlink(c *check.C) {
	c.Assert(os.MkdirAll(dirs.SnapBlobDir, 0755), check.IsNil)

	info := snap.Info{SideInfo: snap.SideInfo{RealName: "hello", Revision: snap.R(1)}}
	snapst := snapstate.SnapState{}
	mountFile := info.MountFile()
	about := daemon.MakeAboutSnap(&info, &snapst)

	// if not a 'try', then MountedFrom is just MountFile()
	c.Check(daemon.MapLocal(about, nil).MountedFrom, check.Equals, mountFile)

	// if it's a try, then MountedFrom resolves the symlink
	// (note it doesn't matter, here, whether the target of the link exists)
	snapst.TryMode = true
	c.Assert(os.Symlink("/xyzzy", mountFile), check.IsNil)
	c.Check(daemon.MapLocal(about, nil).MountedFrom, check.Equals, "/xyzzy")

	// if the readlink fails, it's unset
	c.Assert(os.Remove(mountFile), check.IsNil)
	c.Check(daemon.MapLocal(about, nil).MountedFrom, check.Equals, "")
}
