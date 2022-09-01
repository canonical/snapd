// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2014-2021 Canonical Ltd
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
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/check.v1"

	"github.com/snapcore/snapd/arch"
	"github.com/snapcore/snapd/asserts/snapasserts"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/dirs"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/healthstate"
	"github.com/snapcore/snapd/overlord/snapstate"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/sandbox"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/snap/channel"
	"github.com/snapcore/snapd/snap/snaptest"
	"github.com/snapcore/snapd/store"
	"github.com/snapcore/snapd/testutil"
)

type snapsSuite struct {
	apiBaseSuite
}

var _ = check.Suite(&snapsSuite{})

func (s *snapsSuite) SetUpTest(c *check.C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})
}

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

	rsp := s.syncReq(c, req, nil)
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

	rsp := s.syncReq(c, req, nil)

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
	rsp := s.syncReq(c, req, nil)

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
		rsp := s.jsonReq(c, req, nil)
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

	rsp := s.syncReq(c, req, nil)

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
	user, err := auth.NewUser(state, auth.NewUserData{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	state.Unlock()
	c.Check(err, check.IsNil)

	req, err := http.NewRequest("GET", "/v2/snaps?sources=store", nil)
	c.Assert(err, check.IsNil)

	c.Assert(s.user, check.IsNil)

	_ = s.syncReq(c, req, user)

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

	rsp := s.syncReq(c, req, nil)

	// presence of 'store' in sources bounces request over to /find
	c.Assert(rsp.Sources, check.DeepEquals, []string{"store"})

	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["version"], check.Equals, "v42")

	// as does a 'q'
	req, err = http.NewRequest("GET", "/v2/snaps?q=what", nil)
	c.Assert(err, check.IsNil)
	rsp = s.syncReq(c, req, nil)
	snaps = snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
	c.Check(snaps[0]["version"], check.Equals, "v42")

	// otherwise, local only
	req, err = http.NewRequest("GET", "/v2/snaps", nil)
	c.Assert(err, check.IsNil)
	rsp = s.syncReq(c, req, nil)
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

	rsp := s.syncReq(c, req, nil)

	c.Assert(rsp.Sources, check.DeepEquals, []string{"local"})
	snaps := snapList(rsp.Result)
	c.Assert(snaps, check.HasLen, 1)
}

func (s *snapsSuite) TestSnapsInfoFilterRemote(c *check.C) {
	s.daemon(c)

	s.rsnaps = nil

	req, err := http.NewRequest("GET", "/v2/snaps?q=foo&sources=store", nil)
	c.Assert(err, check.IsNil)

	rsp := s.syncReq(c, req, nil)

	c.Check(s.storeSearch, check.DeepEquals, store.Search{Query: "foo"})

	c.Assert(rsp.Result, check.NotNil)
}

func (s *snapsSuite) TestPostSnapsVerifyMultiSnapInstruction(c *check.C) {
	s.daemonWithOverlordMockAndStore()

	buf := strings.NewReader(`{"action": "install","snaps":["ubuntu-core"]}`)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/json")

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, testutil.Contains, `cannot install "ubuntu-core", please use "core" instead`)
}

func (s *snapsSuite) TestPostSnapsUnsupportedMultiOp(c *check.C) {
	s.daemonWithOverlordMockAndStore()

	buf := strings.NewReader(`{"action": "switch","snaps":["foo"]}`)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/json")

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, testutil.Contains, `unsupported multi-snap operation "switch"`)
}

func (s *snapsSuite) TestPostSnapsNoWeirdses(c *check.C) {
	s.daemonWithOverlordMockAndStore()

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

			rspe := s.errorReq(c, req, nil)
			c.Check(rspe.Status, check.Equals, 400)
			c.Check(rspe.Message, testutil.Contains, `unsupported option provided for multi-snap operation`)
		}
	}
}

func (s *snapsSuite) TestPostSnapsOp(c *check.C) {
	systemRestartImmediate := s.testPostSnapsOp(c, "", "application/json")
	c.Check(systemRestartImmediate, check.Equals, false)
}

func (s *snapsSuite) TestPostSnapsOpMoreComplexContentType(c *check.C) {
	systemRestartImmediate := s.testPostSnapsOp(c, "", "application/json; charset=utf-8")
	c.Check(systemRestartImmediate, check.Equals, false)
}

func (s *snapsSuite) TestPostSnapsOpSystemRestartImmediate(c *check.C) {
	systemRestartImmediate := s.testPostSnapsOp(c, `"system-restart-immediate": true`, "application/json")
	c.Check(systemRestartImmediate, check.Equals, true)
}

func (s *snapsSuite) testPostSnapsOp(c *check.C, extraJSON, contentType string) (systemRestartImmediate bool) {
	defer daemon.MockAssertstateRefreshSnapAssertions(func(*state.State, int, *assertstate.RefreshAssertionsOptions) error { return nil })()
	defer daemon.MockSnapstateUpdateMany(func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		c.Check(names, check.HasLen, 0)
		t := s.NewTask("fake-refresh-all", "Refreshing everything")
		return []string{"fake1", "fake2"}, []*state.TaskSet{state.NewTaskSet(t)}, nil
	})()

	d := s.daemonWithOverlordMockAndStore()

	if extraJSON != "" {
		extraJSON = "," + extraJSON
	}
	buf := bytes.NewBufferString(fmt.Sprintf(`{"action": "refresh"%s}`, extraJSON))
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", contentType)

	rsp := s.asyncReq(c, req, nil)

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Check(chg.Summary(), check.Equals, `Refresh snaps "fake1", "fake2"`)
	var apiData map[string]interface{}
	c.Check(chg.Get("api-data", &apiData), check.IsNil)
	c.Check(apiData["snap-names"], check.DeepEquals, []interface{}{"fake1", "fake2"})
	err = chg.Get("system-restart-immediate", &systemRestartImmediate)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		c.Error(err)
	}
	return systemRestartImmediate
}

func (s *snapsSuite) TestPostSnapsOpInvalidCharset(c *check.C) {
	s.daemon(c)

	buf := bytes.NewBufferString(`{"action": "refresh"}`)
	req, err := http.NewRequest("POST", "/v2/snaps", buf)
	c.Assert(err, check.IsNil)
	req.Header.Set("Content-Type", "application/json; charset=iso-8859-1")

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, testutil.Contains, "unknown charset in content type")
}

func (s *snapsSuite) TestRefreshAll(c *check.C) {
	refreshSnapAssertions := false
	var refreshAssertionsOpts *assertstate.RefreshAssertionsOptions
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		refreshSnapAssertions = true
		refreshAssertionsOpts = opts
		return assertstate.RefreshSnapAssertions(s, userID, opts)
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
		refreshSnapAssertions = false
		refreshAssertionsOpts = nil

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
		c.Check(refreshSnapAssertions, check.Equals, true)
		c.Assert(refreshAssertionsOpts, check.NotNil)
		c.Check(refreshAssertionsOpts.IsRefreshOfAllSnaps, check.Equals, true)
	}
}

func (s *snapsSuite) TestRefreshAllNoChanges(c *check.C) {
	refreshSnapAssertions := false
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		refreshSnapAssertions = true
		return assertstate.RefreshSnapAssertions(s, userID, opts)
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
	c.Check(refreshSnapAssertions, check.Equals, true)
}

func (s *snapsSuite) TestRefreshAllRestoresValidationSets(c *check.C) {
	refreshSnapAssertions := false
	var refreshAssertionsOpts *assertstate.RefreshAssertionsOptions
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		refreshSnapAssertions = true
		refreshAssertionsOpts = opts
		return nil
	})()

	defer daemon.MockAssertstateRestoreValidationSetsTracking(func(s *state.State) error {
		return nil
	})()

	defer daemon.MockSnapstateUpdateMany(func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		return nil, nil, fmt.Errorf("boom")
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{Action: "refresh"}
	st := d.Overlord().State()
	st.Lock()
	_, err := inst.DispatchForMany()(inst, st)
	st.Unlock()
	c.Assert(err, check.ErrorMatches, "boom")
	c.Check(refreshSnapAssertions, check.Equals, true)
	c.Assert(refreshAssertionsOpts, check.NotNil)
	c.Check(refreshAssertionsOpts.IsRefreshOfAllSnaps, check.Equals, true)
}

func (s *snapsSuite) TestRefreshManyTransactionally(c *check.C) {
	var calledFlags *snapstate.Flags

	refreshSnapAssertions := false
	var refreshAssertionsOpts *assertstate.RefreshAssertionsOptions
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		refreshSnapAssertions = true
		refreshAssertionsOpts = opts
		return nil
	})()

	defer daemon.MockSnapstateUpdateMany(func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		calledFlags = flags

		c.Check(names, check.HasLen, 2)
		t := s.NewTask("fake-refresh-2", "Refreshing two")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action:      "refresh",
		Transaction: client.TransactionAllSnaps,
		Snaps:       []string{"foo", "bar"},
	}
	st := d.Overlord().State()
	st.Lock()
	res, err := inst.DispatchForMany()(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Refresh snaps "foo", "bar"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
	c.Check(refreshSnapAssertions, check.Equals, true)
	c.Assert(refreshAssertionsOpts, check.NotNil)
	c.Check(refreshAssertionsOpts.IsRefreshOfAllSnaps, check.Equals, false)

	c.Check(calledFlags.Transaction, check.Equals, client.TransactionAllSnaps)
}

func (s *snapsSuite) TestRefreshMany(c *check.C) {
	refreshSnapAssertions := false
	var refreshAssertionsOpts *assertstate.RefreshAssertionsOptions
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		refreshSnapAssertions = true
		refreshAssertionsOpts = opts
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
	c.Check(refreshSnapAssertions, check.Equals, true)
	c.Assert(refreshAssertionsOpts, check.NotNil)
	c.Check(refreshAssertionsOpts.IsRefreshOfAllSnaps, check.Equals, false)
}

func (s *snapsSuite) TestRefreshManyIgnoreRunning(c *check.C) {
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		return nil
	})()

	var calledFlags *snapstate.Flags
	defer daemon.MockSnapstateUpdateMany(func(_ context.Context, s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		calledFlags = flags

		c.Check(names, check.HasLen, 2)
		t := s.NewTask("fake-refresh-2", "Refreshing two")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action:        "refresh",
		Snaps:         []string{"foo", "bar"},
		IgnoreRunning: true,
	}
	st := d.Overlord().State()
	st.Lock()
	res, err := inst.DispatchForMany()(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Refresh snaps "foo", "bar"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)
	c.Check(calledFlags.IgnoreRunning, check.Equals, true)
}

func (s *snapsSuite) TestRefreshMany1(c *check.C) {
	refreshSnapAssertions := false
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		refreshSnapAssertions = true
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
	c.Check(refreshSnapAssertions, check.Equals, true)
}

func (s *snapsSuite) TestInstallMany(c *check.C) {
	defer daemon.MockSnapstateInstallMany(func(s *state.State, names []string, userID int, _ *snapstate.Flags) ([]string, []*state.TaskSet, error) {
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

func (s *snapsSuite) TestInstallManyTransactionally(c *check.C) {
	var calledFlags *snapstate.Flags

	defer daemon.MockSnapstateInstallMany(func(s *state.State, names []string, userID int, flags *snapstate.Flags) ([]string, []*state.TaskSet, error) {
		calledFlags = flags

		c.Check(names, check.HasLen, 2)
		t := s.NewTask("fake-install-2", "Install two")
		return names, []*state.TaskSet{state.NewTaskSet(t)}, nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action:      "install",
		Transaction: client.TransactionAllSnaps,
		Snaps:       []string{"foo", "bar"},
	}

	st := d.Overlord().State()
	st.Lock()
	res, err := inst.DispatchForMany()(inst, st)
	st.Unlock()
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Install snaps "foo", "bar"`)
	c.Check(res.Affected, check.DeepEquals, inst.Snaps)

	c.Check(calledFlags.Transaction, check.Equals, client.TransactionAllSnaps)
}

func (s *snapsSuite) TestInstallManyEmptyName(c *check.C) {
	defer daemon.MockSnapstateInstallMany(func(_ *state.State, _ []string, _ int, _ *snapstate.Flags) ([]string, []*state.TaskSet, error) {
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
Names=snap.foo.svc1.service
ActiveState=fumbling
UnitFileState=enabled
NeedDaemonReload=no
`),
		[]byte(`Type=forking
Id=snap.foo.svc2.service
Names=snap.foo.svc2.service
ActiveState=active
UnitFileState=disabled
NeedDaemonReload=no
`),
		[]byte(`Type=oneshot
Id=snap.foo.svc3.service
Names=snap.foo.svc3.service
ActiveState=reloading
UnitFileState=static
NeedDaemonReload=no
`),
		[]byte(`Type=notify
Id=snap.foo.svc4.service
Names=snap.foo.svc4.service
ActiveState=inactive
UnitFileState=potatoes
NeedDaemonReload=no
`),
		[]byte(`Type=simple
Id=snap.foo.svc5.service
Names=snap.foo.svc5.service
ActiveState=inactive
UnitFileState=static
NeedDaemonReload=no
`),
		[]byte(`Id=snap.foo.svc5.timer
Names=snap.foo.svc5.timer
ActiveState=active
UnitFileState=enabled
`),
		[]byte(`Type=simple
Id=snap.foo.svc6.service
Names=snap.foo.svc6.service
ActiveState=inactive
UnitFileState=static
NeedDaemonReload=no
`),
		[]byte(`Id=snap.foo.svc6.sock.socket
Names=snap.foo.svc6.sock.socket
ActiveState=active
UnitFileState=enabled
`),
		[]byte(`Type=simple
Id=snap.foo.svc7.service
Names=snap.foo.svc7.service
ActiveState=inactive
UnitFileState=static
NeedDaemonReload=no
`),
		[]byte(`Id=snap.foo.svc7.other-sock.socket
Names=snap.foo.svc7.other-sock.socket
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
	rsp := s.syncReq(c, req, nil)

	c.Assert(rsp.Result, check.FitsTypeOf, &client.Snap{})
	m := rsp.Result.(*client.Snap)

	// installed-size depends on vagaries of the filesystem, just check type
	c.Check(m.InstalledSize, check.FitsTypeOf, int64(0))
	m.InstalledSize = 0
	// ditto install-date
	c.Check(m.InstallDate, check.FitsTypeOf, time.Time{})
	m.InstallDate = time.Time{}

	expected := &daemon.RespJSON{
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
					Daemon:      "simple",
					DaemonScope: snap.SystemDaemon,
					Enabled:     true,
					Active:      false,
				}, {
					Snap: "foo", Name: "svc2",
					Daemon:      "forking",
					DaemonScope: snap.SystemDaemon,
					Enabled:     false,
					Active:      true,
				}, {
					Snap: "foo", Name: "svc3",
					Daemon:      "oneshot",
					DaemonScope: snap.SystemDaemon,
					Enabled:     true,
					Active:      true,
				}, {
					Snap: "foo", Name: "svc4",
					Daemon:      "notify",
					DaemonScope: snap.SystemDaemon,
					Enabled:     false,
					Active:      false,
				}, {
					Snap: "foo", Name: "svc5",
					Daemon:      "simple",
					DaemonScope: snap.SystemDaemon,
					Enabled:     true,
					Active:      false,
					Activators: []client.AppActivator{
						{Name: "svc5", Type: "timer", Active: true, Enabled: true},
					},
				}, {
					Snap: "foo", Name: "svc6",
					Daemon:      "simple",
					DaemonScope: snap.SystemDaemon,
					Enabled:     true,
					Active:      false,
					Activators: []client.AppActivator{
						{Name: "sock", Type: "socket", Active: true, Enabled: true},
					},
				}, {
					Snap: "foo", Name: "svc7",
					Daemon:      "simple",
					DaemonScope: snap.SystemDaemon,
					Enabled:     true,
					Active:      false,
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
	}

	c.Check(rsp.Result, check.DeepEquals, expected.Result)
}

func (s *snapsSuite) TestSnapInfoNotFound(c *check.C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	c.Check(s.errorReq(c, req, nil).Status, check.Equals, 404)
}

func (s *snapsSuite) TestSnapInfoNoneFound(c *check.C) {
	s.daemon(c)

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	c.Check(s.errorReq(c, req, nil).Status, check.Equals, 404)
}

func (s *snapsSuite) TestSnapInfoIgnoresRemoteErrors(c *check.C) {
	s.daemon(c)
	s.err = errors.New("weird")

	req, err := http.NewRequest("GET", "/v2/snaps/gfoo", nil)
	c.Assert(err, check.IsNil)
	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 404)
	c.Check(rspe.Message, check.Not(check.Equals), "")
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
			EditedContact:     "mailto:alice@example.com",
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
		Contact:          "mailto:alice@example.com",
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

func (s *snapsSuite) TestPostSnapBadRequest(c *check.C) {
	s.daemon(c)

	buf := bytes.NewBufferString(`hello`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Not(check.Equals), "")
}

func (s *snapsSuite) TestPostSnapBadAction(c *check.C) {
	s.daemon(c)

	buf := bytes.NewBufferString(`{"action": "potato"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Not(check.Equals), "")
}

func (s *snapsSuite) TestPostSnapBadChannel(c *check.C) {
	s.daemon(c)

	buf := bytes.NewBufferString(`{"channel": "1/2/3/4"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, check.Not(check.Equals), "")
}

func (s *snapsSuite) TestPostSnap(c *check.C) {
	checkOpts := func(opts *snapstate.RevisionOptions) {
		// no channel in -> no channel out
		c.Check(opts.Channel, check.Equals, "")
	}
	summary, systemRestartImmediate := s.testPostSnap(c, "", checkOpts)
	c.Check(summary, check.Equals, `Install "foo" snap`)
	c.Check(systemRestartImmediate, check.Equals, false)
}

func (s *snapsSuite) TestPostSnapWithChannel(c *check.C) {
	checkOpts := func(opts *snapstate.RevisionOptions) {
		// channel in -> channel out
		c.Check(opts.Channel, check.Equals, "xyzzy")
	}
	summary, systemRestartImmediate := s.testPostSnap(c, `"channel": "xyzzy"`, checkOpts)
	c.Check(summary, check.Equals, `Install "foo" snap from "xyzzy" channel`)
	c.Check(systemRestartImmediate, check.Equals, false)
}

func (s *snapsSuite) TestPostSnapSystemRestartImmediate(c *check.C) {
	_, systemRestartImmediate := s.testPostSnap(c, `"system-restart-immediate": true`, nil)
	c.Check(systemRestartImmediate, check.Equals, true)
}

func (s *snapsSuite) testPostSnap(c *check.C, extraJSON string, checkOpts func(opts *snapstate.RevisionOptions)) (summary string, systemRestartImmediate bool) {
	d := s.daemonWithOverlordMock()

	soon := 0
	var origEnsureStateSoon func(*state.State)
	origEnsureStateSoon, restore := daemon.MockEnsureStateSoon(func(st *state.State) {
		soon++
		origEnsureStateSoon(st)
	})
	defer restore()

	checked := false
	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		if checkOpts != nil {
			checkOpts(opts)
		}
		checked = true
		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	var buf *bytes.Buffer
	if extraJSON != "" {
		extraJSON = "," + extraJSON
	}
	buf = bytes.NewBufferString(fmt.Sprintf(`{"action": "install"%s}`, extraJSON))
	req, err := http.NewRequest("POST", "/v2/snaps/foo", buf)
	c.Assert(err, check.IsNil)

	rsp := s.asyncReq(c, req, nil)

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	var names []string
	err = chg.Get("snap-names", &names)
	c.Assert(err, check.IsNil)
	c.Check(names, check.DeepEquals, []string{"foo"})

	c.Check(checked, check.Equals, true)
	c.Check(soon, check.Equals, 1)
	c.Check(chg.Tasks()[0].Summary(), check.Equals, "Doing a fake install")

	summary = chg.Summary()
	err = chg.Get("system-restart-immediate", &systemRestartImmediate)
	if err != nil && !errors.Is(err, state.ErrNoState) {
		c.Error(err)
	}
	return summary, systemRestartImmediate
}

func (s *snapsSuite) TestPostSnapVerifySnapInstruction(c *check.C) {
	s.daemonWithOverlordMock()

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/ubuntu-core", buf)
	c.Assert(err, check.IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, check.Equals, 400)
	c.Check(rspe.Message, testutil.Contains, `cannot install "ubuntu-core", please use "core" instead`)
}

func (s *snapsSuite) TestPostSnapCohortUnsupportedAction(c *check.C) {
	s.daemonWithOverlordMock()
	const expectedErr = "cohort-key can only be specified for install, refresh, or switch"

	for _, action := range []string{"remove", "revert", "enable", "disable", "xyzzy"} {
		buf := strings.NewReader(fmt.Sprintf(`{"action": "%s", "cohort-key": "32"}`, action))
		req, err := http.NewRequest("POST", "/v2/snaps/some-snap", buf)
		c.Assert(err, check.IsNil)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, check.Equals, 400, check.Commentf("%q", action))
		c.Check(rspe.Message, check.Equals, expectedErr, check.Commentf("%q", action))
	}
}

func (s *snapsSuite) TestPostSnapQuotaGroupWrongAction(c *check.C) {
	s.daemonWithOverlordMock()
	const expectedErr = "quota-group can only be specified for install"

	for _, action := range []string{"remove", "refresh", "revert", "enable", "disable", "xyzzy"} {
		buf := strings.NewReader(fmt.Sprintf(`{"action": "%s", "quota-group": "foo"}`, action))
		req, err := http.NewRequest("POST", "/v2/snaps/some-snap", buf)
		c.Assert(err, check.IsNil)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, check.Equals, 400, check.Commentf("%q", action))
		c.Check(rspe.Message, check.Equals, expectedErr, check.Commentf("%q", action))
	}
}

func (s *snapsSuite) TestPostSnapLeaveCohortUnsupportedAction(c *check.C) {
	s.daemonWithOverlordMock()
	const expectedErr = "leave-cohort can only be specified for refresh or switch"

	for _, action := range []string{"install", "remove", "revert", "enable", "disable", "xyzzy"} {
		buf := strings.NewReader(fmt.Sprintf(`{"action": "%s", "leave-cohort": true}`, action))
		req, err := http.NewRequest("POST", "/v2/snaps/some-snap", buf)
		c.Assert(err, check.IsNil)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, check.Equals, 400, check.Commentf("%q", action))
		c.Check(rspe.Message, check.Equals, expectedErr, check.Commentf("%q", action))
	}
}

func (s *snapsSuite) TestPostSnapCohortIncompat(c *check.C) {
	s.daemonWithOverlordMock()
	type T struct {
		opts   string
		errmsg string
	}

	for i, t := range []T{
		// TODO: more?
		{`"cohort-key": "what", "revision": "42"`, `cannot specify both cohort-key and revision`},
		{`"cohort-key": "what", "leave-cohort": true`, `cannot specify both cohort-key and leave-cohort`},
	} {
		buf := strings.NewReader(fmt.Sprintf(`{"action": "refresh", %s}`, t.opts))
		req, err := http.NewRequest("POST", "/v2/snaps/some-snap", buf)
		c.Assert(err, check.IsNil, check.Commentf("%d (%s)", i, t.opts))

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, check.Equals, 400, check.Commentf("%d (%s)", i, t.opts))
		c.Check(rspe.Message, check.Equals, t.errmsg, check.Commentf("%d (%s)", i, t.opts))
	}
}

func (s *snapsSuite) TestPostSnapSetsUser(c *check.C) {
	d := s.daemon(c)

	_, restore := daemon.MockEnsureStateSoon(func(st *state.State) {})
	defer restore()

	checked := false
	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		c.Check(userID, check.Equals, 1)
		checked = true
		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	state := d.Overlord().State()
	state.Lock()
	user, err := auth.NewUser(state, auth.NewUserData{
		Username:   "username",
		Email:      "email@test.com",
		Macaroon:   "macaroon",
		Discharges: []string{"discharge"},
	})
	state.Unlock()
	c.Check(err, check.IsNil)

	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := s.asyncReq(c, req, user)

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)
	c.Check(checked, check.Equals, true)
}

func (s *snapsSuite) TestPostSnapEnableDisableSwitchRevision(c *check.C) {
	s.daemon(c)

	for _, action := range []string{"enable", "disable", "switch"} {
		buf := bytes.NewBufferString(`{"action": "` + action + `", "revision": "42"}`)
		req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
		c.Assert(err, check.IsNil)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, check.Equals, 400)
		c.Check(rspe.Message, testutil.Contains, "takes no revision")
	}
}

func (s *snapsSuite) TestInstall(c *check.C) {
	var calledName string

	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledName = name

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action: "install",
		// Install the snap in developer mode
		DevMode: true,
		Snaps:   []string{"fake"},
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)
	c.Check(calledName, check.Equals, "fake")
}

func (s *snapsSuite) TestInstallWithQuotaGroup(c *check.C) {
	var calledFlags snapstate.Flags

	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action:         "install",
		Snaps:          []string{"fake"},
		QuotaGroupName: "test-group",
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)
	c.Check(calledFlags.QuotaGroupName, check.Equals, "test-group")
}

func (s *snapsSuite) TestInstallDevMode(c *check.C) {
	var calledFlags snapstate.Flags

	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action: "install",
		// Install the snap in developer mode
		DevMode: true,
		Snaps:   []string{"fake"},
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags.DevMode, check.Equals, true)
}

func (s *snapsSuite) TestInstallJailMode(c *check.C) {
	var calledFlags snapstate.Flags

	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action:   "install",
		JailMode: true,
		Snaps:    []string{"fake"},
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags.JailMode, check.Equals, true)
}

func (s *snapsSuite) TestInstallJailModeDevModeOS(c *check.C) {
	restore := sandbox.MockForceDevMode(true)
	defer restore()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action:   "install",
		JailMode: true,
		Snaps:    []string{"foo"},
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.ErrorMatches, "this system cannot honour the jailmode flag")
}

func (s *snapsSuite) TestInstallJailModeDevMode(c *check.C) {
	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action:   "install",
		DevMode:  true,
		JailMode: true,
		Snaps:    []string{"foo"},
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.ErrorMatches, "cannot use devmode and jailmode flags together")
}

func (s *snapsSuite) TestInstallCohort(c *check.C) {
	var calledName string
	var calledCohort string

	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledName = name
		calledCohort = opts.CohortKey

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action: "install",
		Snaps:  []string{"fake"},
	}
	inst.CohortKey = "To the legion of the lost ones, to the cohort of the damned."

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	msg, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)
	c.Check(calledName, check.Equals, "fake")
	c.Check(calledCohort, check.Equals, "To the legion of the lost ones, to the cohort of the damned.")
	c.Check(msg, check.Equals, `Install "fake" snap from "…e damned." cohort`)
}

func (s *snapsSuite) TestInstallIgnoreValidation(c *check.C) {
	var calledFlags snapstate.Flags
	installQueue := []string{}

	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		installQueue = append(installQueue, name)
		calledFlags = flags
		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		return nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action:           "install",
		IgnoreValidation: true,
		Snaps:            []string{"some-snap"},
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	flags := snapstate.Flags{}
	flags.IgnoreValidation = true

	c.Check(calledFlags, check.DeepEquals, flags)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Install "some-snap" snap`)
}

func (s *snapsSuite) TestInstallEmptyName(c *check.C) {
	defer daemon.MockSnapstateInstall(func(ctx context.Context, _ *state.State, _ string, _ *snapstate.RevisionOptions, _ int, _ snapstate.Flags) (*state.TaskSet, error) {
		return nil, errors.New("should not be called")
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action: "install",
		Snaps:  []string{""},
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.ErrorMatches, "cannot install snap with empty name")
}

func (s *snapsSuite) TestInstallOnNonDevModeDistro(c *check.C) {
	s.testInstall(c, false, snapstate.Flags{}, snap.R(0))
}
func (s *snapsSuite) TestInstallOnDevModeDistro(c *check.C) {
	s.testInstall(c, true, snapstate.Flags{}, snap.R(0))
}
func (s *snapsSuite) TestInstallRevision(c *check.C) {
	s.testInstall(c, false, snapstate.Flags{}, snap.R(42))
}

func (s *snapsSuite) testInstall(c *check.C, forcedDevmode bool, flags snapstate.Flags, revision snap.Revision) {
	calledFlags := snapstate.Flags{}
	installQueue := []string{}
	restore := sandbox.MockForceDevMode(forcedDevmode)
	defer restore()

	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		installQueue = append(installQueue, name)
		c.Check(revision, check.Equals, opts.Revision)

		t := s.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	d := s.daemonWithFakeSnapManager(c)

	var buf bytes.Buffer
	if revision.Unset() {
		buf.WriteString(`{"action": "install"}`)
	} else {
		fmt.Fprintf(&buf, `{"action": "install", "revision": %s}`, revision.String())
	}
	req, err := http.NewRequest("POST", "/v2/snaps/some-snap", &buf)
	c.Assert(err, check.IsNil)

	rsp := s.asyncReq(c, req, nil)

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Check(chg.Tasks(), check.HasLen, 1)

	st.Unlock()
	s.waitTrivialChange(c, chg)
	st.Lock()

	c.Check(chg.Status(), check.Equals, state.DoneStatus)
	c.Check(calledFlags, check.Equals, flags)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(chg.Kind(), check.Equals, "install-snap")
	c.Check(chg.Summary(), check.Equals, `Install "some-snap" snap`)
}

func (s *snapsSuite) TestInstallUserAgentContextCreated(c *check.C) {
	defer daemon.MockSnapstateInstall(func(ctx context.Context, st *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		s.ctx = ctx
		t := st.NewTask("fake-install-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()

	s.daemonWithFakeSnapManager(c)

	var buf bytes.Buffer
	buf.WriteString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/some-snap", &buf)
	s.asRootAuth(req)
	c.Assert(err, check.IsNil)
	req.Header.Add("User-Agent", "some-agent/1.0")

	rec := httptest.NewRecorder()
	s.serveHTTP(c, rec, req)
	c.Assert(rec.Code, check.Equals, 202)
	c.Check(store.ClientUserAgent(s.ctx), check.Equals, "some-agent/1.0")
}

func (s *snapsSuite) TestInstallFails(c *check.C) {
	defer daemon.MockSnapstateInstall(func(ctx context.Context, s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		t := s.NewTask("fake-install-snap-error", "Install task")
		return state.NewTaskSet(t), nil
	})()

	d := s.daemonWithFakeSnapManager(c)
	buf := bytes.NewBufferString(`{"action": "install"}`)
	req, err := http.NewRequest("POST", "/v2/snaps/hello-world", buf)
	c.Assert(err, check.IsNil)

	rsp := s.asyncReq(c, req, nil)

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	chg := st.Change(rsp.Change)
	c.Assert(chg, check.NotNil)

	c.Check(chg.Tasks(), check.HasLen, 1)

	st.Unlock()
	s.waitTrivialChange(c, chg)
	st.Lock()

	c.Check(chg.Err(), check.ErrorMatches, `(?sm).*Install task \(fake-install-snap-error errored\)`)
}

func (s *snapsSuite) TestRefresh(c *check.C) {
	var calledFlags snapstate.Flags
	calledUserID := 0
	installQueue := []string{}
	assertstateCalledUserID := 0

	defer daemon.MockSnapstateUpdate(func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		calledUserID = userID
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		assertstateCalledUserID = userID
		return nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action: "refresh",
		Snaps:  []string{"some-snap"},
	}
	inst.SetUserID(17)

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(assertstateCalledUserID, check.Equals, 17)
	c.Check(calledFlags, check.DeepEquals, snapstate.Flags{})
	c.Check(calledUserID, check.Equals, 17)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *snapsSuite) TestRefreshDevMode(c *check.C) {
	var calledFlags snapstate.Flags
	calledUserID := 0
	installQueue := []string{}

	defer daemon.MockSnapstateUpdate(func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		calledUserID = userID
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		return nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action:  "refresh",
		DevMode: true,
		Snaps:   []string{"some-snap"},
	}
	inst.SetUserID(17)

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	flags := snapstate.Flags{}
	flags.DevMode = true
	c.Check(calledFlags, check.DeepEquals, flags)
	c.Check(calledUserID, check.Equals, 17)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *snapsSuite) TestRefreshClassic(c *check.C) {
	var calledFlags snapstate.Flags

	defer daemon.MockSnapstateUpdate(func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		return nil, nil
	})()
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		return nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action:  "refresh",
		Classic: true,
		Snaps:   []string{"some-snap"},
	}
	inst.SetUserID(17)

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	_, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(calledFlags, check.DeepEquals, snapstate.Flags{Classic: true})
}

func (s *snapsSuite) TestRefreshIgnoreValidation(c *check.C) {
	var calledFlags snapstate.Flags
	calledUserID := 0
	installQueue := []string{}

	defer daemon.MockSnapstateUpdate(func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		calledUserID = userID
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		return nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action:           "refresh",
		IgnoreValidation: true,
		Snaps:            []string{"some-snap"},
	}
	inst.SetUserID(17)

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	flags := snapstate.Flags{}
	flags.IgnoreValidation = true

	c.Check(calledFlags, check.DeepEquals, flags)
	c.Check(calledUserID, check.Equals, 17)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *snapsSuite) TestRefreshIgnoreRunning(c *check.C) {
	var calledFlags snapstate.Flags
	installQueue := []string{}

	defer daemon.MockSnapstateUpdate(func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		calledFlags = flags
		installQueue = append(installQueue, name)

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		return nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action:        "refresh",
		IgnoreRunning: true,
		Snaps:         []string{"some-snap"},
	}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	flags := snapstate.Flags{}
	flags.IgnoreRunning = true

	c.Check(calledFlags, check.DeepEquals, flags)
	c.Check(err, check.IsNil)
	c.Check(installQueue, check.DeepEquals, []string{"some-snap"})
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *snapsSuite) TestRefreshCohort(c *check.C) {
	cohort := ""

	defer daemon.MockSnapstateUpdate(func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		cohort = opts.CohortKey

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		return nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action: "refresh",
		Snaps:  []string{"some-snap"},
	}
	inst.CohortKey = "xyzzy"

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(cohort, check.Equals, "xyzzy")
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *snapsSuite) TestRefreshLeaveCohort(c *check.C) {
	var leave *bool

	defer daemon.MockSnapstateUpdate(func(s *state.State, name string, opts *snapstate.RevisionOptions, userID int, flags snapstate.Flags) (*state.TaskSet, error) {
		leave = &opts.LeaveCohort

		t := s.NewTask("fake-refresh-snap", "Doing a fake install")
		return state.NewTaskSet(t), nil
	})()
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		return nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{
		Action: "refresh",
		Snaps:  []string{"some-snap"},
	}
	inst.LeaveCohort = true

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)

	c.Check(*leave, check.Equals, true)
	c.Check(summary, check.Equals, `Refresh "some-snap" snap`)
}

func (s *snapsSuite) TestSwitchInstruction(c *check.C) {
	var cohort, channel string
	var leave *bool

	defer daemon.MockSnapstateSwitch(func(s *state.State, name string, opts *snapstate.RevisionOptions) (*state.TaskSet, error) {
		cohort = opts.CohortKey
		leave = &opts.LeaveCohort
		channel = opts.Channel

		t := s.NewTask("fake-switch", "Doing a fake switch")
		return state.NewTaskSet(t), nil
	})()

	d := s.daemon(c)
	st := d.Overlord().State()

	type T struct {
		channel string
		cohort  string
		leave   bool
		summary string
	}
	table := []T{
		{"", "some-cohort", false, `Switch "some-snap" snap to cohort "…me-cohort"`},
		{"some-channel", "", false, `Switch "some-snap" snap to channel "some-channel"`},
		{"some-channel", "some-cohort", false, `Switch "some-snap" snap to channel "some-channel" and cohort "…me-cohort"`},
		{"", "", true, `Switch "some-snap" snap away from cohort`},
		{"some-channel", "", true, `Switch "some-snap" snap to channel "some-channel" and away from cohort`},
	}

	for _, t := range table {
		cohort, channel = "", ""
		leave = nil
		inst := &daemon.SnapInstruction{
			Action: "switch",
			Snaps:  []string{"some-snap"},
		}
		inst.CohortKey = t.cohort
		inst.LeaveCohort = t.leave
		inst.Channel = t.channel

		st.Lock()
		summary, _, err := inst.Dispatch()(inst, st)
		st.Unlock()
		c.Check(err, check.IsNil)

		c.Check(cohort, check.Equals, t.cohort)
		c.Check(channel, check.Equals, t.channel)
		c.Check(summary, check.Equals, t.summary)
		c.Check(*leave, check.Equals, t.leave)
	}
}

func (s *snapsSuite) testRevertSnap(inst *daemon.SnapInstruction, c *check.C) {
	queue := []string{}

	instFlags, err := inst.ModeFlags()
	c.Assert(err, check.IsNil)

	defer daemon.MockSnapstateRevert(func(s *state.State, name string, flags snapstate.Flags, fromChange string) (*state.TaskSet, error) {
		c.Check(flags, check.Equals, instFlags)
		queue = append(queue, name)
		return nil, nil
	})()
	defer daemon.MockSnapstateRevertToRevision(func(s *state.State, name string, rev snap.Revision, flags snapstate.Flags, fromChange string) (*state.TaskSet, error) {
		c.Check(flags, check.Equals, instFlags)
		queue = append(queue, fmt.Sprintf("%s (%s)", name, rev))
		return nil, nil
	})()

	d := s.daemon(c)
	inst.Action = "revert"
	inst.Snaps = []string{"some-snap"}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()
	summary, _, err := inst.Dispatch()(inst, st)
	c.Check(err, check.IsNil)
	if inst.Revision.Unset() {
		c.Check(queue, check.DeepEquals, []string{inst.Snaps[0]})
	} else {
		c.Check(queue, check.DeepEquals, []string{fmt.Sprintf("%s (%s)", inst.Snaps[0], inst.Revision)})
	}
	c.Check(summary, check.Equals, `Revert "some-snap" snap`)
}

func (s *snapsSuite) TestRevertSnap(c *check.C) {
	s.testRevertSnap(&daemon.SnapInstruction{}, c)
}

func (s *snapsSuite) TestRevertSnapDevMode(c *check.C) {
	s.testRevertSnap(&daemon.SnapInstruction{DevMode: true}, c)
}

func (s *snapsSuite) TestRevertSnapJailMode(c *check.C) {
	s.testRevertSnap(&daemon.SnapInstruction{JailMode: true}, c)
}

func (s *snapsSuite) TestRevertSnapClassic(c *check.C) {
	s.testRevertSnap(&daemon.SnapInstruction{Classic: true}, c)
}

func (s *snapsSuite) TestRevertSnapToRevision(c *check.C) {
	inst := &daemon.SnapInstruction{}
	inst.Revision = snap.R(1)
	s.testRevertSnap(inst, c)
}

func (s *snapsSuite) TestRevertSnapToRevisionDevMode(c *check.C) {
	inst := &daemon.SnapInstruction{}
	inst.Revision = snap.R(1)
	inst.DevMode = true
	s.testRevertSnap(inst, c)
}

func (s *snapsSuite) TestRevertSnapToRevisionJailMode(c *check.C) {
	inst := &daemon.SnapInstruction{}
	inst.Revision = snap.R(1)
	inst.JailMode = true
	s.testRevertSnap(inst, c)
}

func (s *snapsSuite) TestRevertSnapToRevisionClassic(c *check.C) {
	inst := &daemon.SnapInstruction{}
	inst.Revision = snap.R(1)
	inst.Classic = true
	s.testRevertSnap(inst, c)
}

func (s *snapsSuite) TestErrToResponseNoSnapsDoesNotPanic(c *check.C) {
	si := &daemon.SnapInstruction{Action: "frobble"}
	errors := []error{
		store.ErrSnapNotFound,
		&store.RevisionNotAvailableError{},
		store.ErrNoUpdateAvailable,
		store.ErrLocalSnap,
		&snap.AlreadyInstalledError{Snap: "foo"},
		&snap.NotInstalledError{Snap: "foo"},
		&snapstate.SnapNeedsDevModeError{Snap: "foo"},
		&snapstate.SnapNeedsClassicError{Snap: "foo"},
		&snapstate.SnapNeedsClassicSystemError{Snap: "foo"},
		fakeNetError{message: "other"},
		fakeNetError{message: "timeout", timeout: true},
		fakeNetError{message: "temp", temporary: true},
		errors.New("some other error"),
	}

	for _, err := range errors {
		rspe := si.ErrToResponse(err)
		com := check.Commentf("%v", err)
		c.Assert(rspe, check.NotNil, com)
		status := rspe.Status
		c.Check(status/100 == 4 || status/100 == 5, check.Equals, true, com)
	}
}

func (s *snapsSuite) TestErrToResponseForRevisionNotAvailable(c *check.C) {
	si := &daemon.SnapInstruction{Action: "frobble", Snaps: []string{"foo"}}

	thisArch := arch.DpkgArchitecture()

	err := &store.RevisionNotAvailableError{
		Action:  "install",
		Channel: "stable",
		Releases: []channel.Channel{
			snaptest.MustParseChannel("beta", thisArch),
		},
	}
	rspe := si.ErrToResponse(err)
	c.Check(rspe, check.DeepEquals, &daemon.APIError{
		Status:  404,
		Message: "no snap revision on specified channel",
		Kind:    client.ErrorKindSnapChannelNotAvailable,
		Value: map[string]interface{}{
			"snap-name":    "foo",
			"action":       "install",
			"channel":      "stable",
			"architecture": thisArch,
			"releases": []map[string]interface{}{
				{"architecture": thisArch, "channel": "beta"},
			},
		},
	})

	err = &store.RevisionNotAvailableError{
		Action:  "install",
		Channel: "stable",
		Releases: []channel.Channel{
			snaptest.MustParseChannel("beta", "other-arch"),
		},
	}
	rspe = si.ErrToResponse(err)
	c.Check(rspe, check.DeepEquals, &daemon.APIError{
		Status:  404,
		Message: "no snap revision on specified architecture",
		Kind:    client.ErrorKindSnapArchitectureNotAvailable,
		Value: map[string]interface{}{
			"snap-name":    "foo",
			"action":       "install",
			"channel":      "stable",
			"architecture": thisArch,
			"releases": []map[string]interface{}{
				{"architecture": "other-arch", "channel": "beta"},
			},
		},
	})

	err = &store.RevisionNotAvailableError{}
	rspe = si.ErrToResponse(err)
	c.Check(rspe, check.DeepEquals, &daemon.APIError{
		Status:  404,
		Message: "no snap revision available as specified",
		Kind:    client.ErrorKindSnapRevisionNotAvailable,
		Value:   "foo",
	})
}

func (s *snapsSuite) TestErrToResponseForChangeConflict(c *check.C) {
	si := &daemon.SnapInstruction{Action: "frobble", Snaps: []string{"foo"}}

	err := &snapstate.ChangeConflictError{Snap: "foo", ChangeKind: "install"}
	rspe := si.ErrToResponse(err)
	c.Check(rspe, check.DeepEquals, &daemon.APIError{
		Status:  409,
		Message: `snap "foo" has "install" change in progress`,
		Kind:    client.ErrorKindSnapChangeConflict,
		Value: map[string]interface{}{
			"snap-name":   "foo",
			"change-kind": "install",
		},
	})

	// only snap
	err = &snapstate.ChangeConflictError{Snap: "foo"}
	rspe = si.ErrToResponse(err)
	c.Check(rspe, check.DeepEquals, &daemon.APIError{
		Status:  409,
		Message: `snap "foo" has changes in progress`,
		Kind:    client.ErrorKindSnapChangeConflict,
		Value: map[string]interface{}{
			"snap-name": "foo",
		},
	})

	// only kind
	err = &snapstate.ChangeConflictError{Message: "specific error msg", ChangeKind: "some-global-op"}
	rspe = si.ErrToResponse(err)
	c.Check(rspe, check.DeepEquals, &daemon.APIError{
		Status:  409,
		Message: "specific error msg",
		Kind:    client.ErrorKindSnapChangeConflict,
		Value: map[string]interface{}{
			"change-kind": "some-global-op",
		},
	})
}

func (s *snapsSuite) TestPostSnapInvalidTransaction(c *check.C) {
	s.daemonWithOverlordMock()

	for _, action := range []string{"remove", "revert", "enable", "disable", "xyzzy"} {
		expectedErr := fmt.Sprintf(`transaction type is unsupported for "%s" actions`, action)
		buf := strings.NewReader(fmt.Sprintf(`{"action": "%s", "transaction": "per-snap"}`, action))
		req, err := http.NewRequest("POST", "/v2/snaps/some-snap", buf)
		c.Assert(err, check.IsNil)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, check.Equals, 400, check.Commentf("%q", action))
		c.Check(rspe.Message, check.Equals, expectedErr, check.Commentf("%q", action))
	}
}

func (s *snapsSuite) TestPostSnapWrongTransaction(c *check.C) {
	s.daemonWithOverlordMock()
	const expectedErr = "invalid value for transaction type: xyz"

	for _, action := range []string{"install", "refresh"} {
		buf := strings.NewReader(fmt.Sprintf(`{"action": "%s", "transaction": "xyz"}`, action))
		req, err := http.NewRequest("POST", "/v2/snaps/some-snap", buf)
		c.Assert(err, check.IsNil)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, check.Equals, 400, check.Commentf("%q", action))
		c.Check(rspe.Message, check.Equals, expectedErr, check.Commentf("%q", action))
	}
}

func (s *snapsSuite) TestRefreshEnforce(c *check.C) {
	var refreshSnapAssertions bool

	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		refreshSnapAssertions = true
		c.Check(opts, check.IsNil)
		return nil
	})()

	var tryEnforceValidationSets bool
	defer daemon.MockAssertstateTryEnforceValidationSets(func(st *state.State, validationSets []string, userID int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) error {
		tryEnforceValidationSets = true
		return nil
	})()

	defer daemon.MockSnapstateEnforceSnaps(func(ctx context.Context, st *state.State, validationSets []string, validErr *snapasserts.ValidationSetsValidationError, userID int) ([]*state.TaskSet, []string, error) {
		c.Check(validationSets, check.DeepEquals, []string{"foo/bar=2", "foo/baz"})
		t := st.NewTask("fake-enforce-snaps", "...")
		return []*state.TaskSet{state.NewTaskSet(t)}, []string{"some-snap", "other-snap"}, nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{Action: "refresh", ValidationSets: []string{"foo/bar=2", "foo/baz"}}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()

	res, err := inst.DispatchForMany()(inst, st)
	c.Assert(err, check.IsNil)
	c.Check(res.Summary, check.Equals, `Enforced validation sets: "foo/bar=2", "foo/baz"`)
	c.Check(res.Affected, check.DeepEquals, []string{"some-snap", "other-snap"})
	c.Check(refreshSnapAssertions, check.Equals, true)
	c.Check(tryEnforceValidationSets, check.Equals, true)
}

func (s *snapsSuite) TestRefreshEnforceTryEnforceValidationSetsError(c *check.C) {
	var refreshSnapAssertions int
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		refreshSnapAssertions++
		c.Check(opts, check.IsNil)
		return nil
	})()

	tryEnforceErr := fmt.Errorf("boom")
	defer daemon.MockAssertstateTryEnforceValidationSets(func(st *state.State, validationSets []string, userID int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) error {
		return tryEnforceErr
	})()

	var snapstateEnforceSnaps int
	defer daemon.MockSnapstateEnforceSnaps(func(ctx context.Context, st *state.State, validationSets []string, validErr *snapasserts.ValidationSetsValidationError, userID int) ([]*state.TaskSet, []string, error) {
		snapstateEnforceSnaps++
		c.Check(validErr, check.NotNil)
		return nil, nil, nil
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{Action: "refresh", ValidationSets: []string{"foo/baz"}}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()

	_, err := inst.DispatchForMany()(inst, st)
	c.Assert(err, check.ErrorMatches, `boom`)
	c.Check(refreshSnapAssertions, check.Equals, 1)
	c.Check(snapstateEnforceSnaps, check.Equals, 0)

	// ValidationSetsValidationError is expected and fine
	tryEnforceErr = &snapasserts.ValidationSetsValidationError{}

	_, err = inst.DispatchForMany()(inst, st)
	c.Assert(err, check.IsNil)
	c.Check(refreshSnapAssertions, check.Equals, 2)
	c.Check(snapstateEnforceSnaps, check.Equals, 1)
}

func (s *snapsSuite) TestRefreshEnforceWithSnapsIsAnError(c *check.C) {
	var refreshSnapAssertions bool
	defer daemon.MockAssertstateRefreshSnapAssertions(func(s *state.State, userID int, opts *assertstate.RefreshAssertionsOptions) error {
		refreshSnapAssertions = true
		c.Check(opts, check.IsNil)
		return fmt.Errorf("unexptected")
	})()

	var tryEnforceValidationSets bool
	defer daemon.MockAssertstateTryEnforceValidationSets(func(st *state.State, validationSets []string, userID int, snaps []*snapasserts.InstalledSnap, ignoreValidation map[string]bool) error {
		tryEnforceValidationSets = true
		return fmt.Errorf("unexpected")
	})()

	var snapstateEnforceSnaps bool
	defer daemon.MockSnapstateEnforceSnaps(func(ctx context.Context, st *state.State, validationSets []string, validErr *snapasserts.ValidationSetsValidationError, userID int) ([]*state.TaskSet, []string, error) {
		snapstateEnforceSnaps = true
		return nil, nil, fmt.Errorf("unexpected")
	})()

	d := s.daemon(c)
	inst := &daemon.SnapInstruction{Action: "refresh", Snaps: []string{"some-snap"}, ValidationSets: []string{"foo/baz"}}

	st := d.Overlord().State()
	st.Lock()
	defer st.Unlock()

	_, err := inst.DispatchForMany()(inst, st)
	c.Assert(err, check.ErrorMatches, `snap names cannot be specified with validation sets to enforce`)
	c.Check(refreshSnapAssertions, check.Equals, false)
	c.Check(tryEnforceValidationSets, check.Equals, false)
	c.Check(snapstateEnforceSnaps, check.Equals, false)
}
