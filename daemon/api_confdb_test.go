// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023-2024 Canonical Ltd
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
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/hookstate"
	"github.com/snapcore/snapd/overlord/state"
)

type confdbSuite struct {
	apiBaseSuite

	st     *state.State
	schema *confdb.Schema
}

var _ = Suite(&confdbSuite{})

func (s *confdbSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectReadAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})

	s.st = state.New(nil)
	o := overlord.MockWithState(s.st)
	s.d = daemon.NewWithOverlord(o)

	s.st.Lock()
	databags := map[string]map[string]confdb.JSONDatabag{
		"system": {"network": confdb.NewJSONDatabag()},
	}
	s.st.Set("confdb-databags", databags)
	s.st.Unlock()

	views := map[string]interface{}{
		"wifi-setup": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "ssid", "storage": "wifi.ssid"},
			},
		},
	}

	schema, err := confdb.NewSchema("system", "network", views, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	s.schema = schema
}

func (s *confdbSuite) setFeatureFlag(c *C) {
	_, confOption := features.Confdb.ConfigOption()

	s.st.Lock()
	defer s.st.Unlock()

	tr := config.NewTransaction(s.st)
	err := tr.Set("core", confOption, true)
	c.Assert(err, IsNil)
	tr.Commit()
}

func (s *confdbSuite) TestGetView(c *C) {
	s.setFeatureFlag(c)

	type test struct {
		name  string
		value interface{}
	}

	for _, t := range []test{
		{name: "string", value: "foo"},
		{name: "integer", value: 123},
		{name: "list", value: []string{"foo", "bar"}},
		{name: "map", value: map[string]int{"foo": 123}},
	} {
		cmt := Commentf("%s test", t.name)

		restoreGet := daemon.MockConfdbstateGetView(func(_ *state.State, acc, confdbSchema, viewName string) (*confdb.View, error) {
			c.Check(acc, Equals, "system", cmt)
			c.Check(confdbSchema, Equals, "network", cmt)
			c.Check(viewName, Equals, "wifi-setup", cmt)

			return s.schema.View(viewName), nil
		})

		restoreLoad := daemon.MockConfdbstateLoadConfdbAsync(func(_ *state.State, view *confdb.View, requests []string) (string, error) {
			c.Assert(view.Name, Equals, "wifi-setup")
			c.Assert(requests, DeepEquals, []string{"ssid"})
			return "123", nil
		})

		req, err := http.NewRequest("GET", "/v2/confdb/system/network/wifi-setup?fields=ssid", nil)
		c.Assert(err, IsNil, cmt)

		rspe := s.asyncReq(c, req, nil)
		c.Check(rspe.Status, Equals, 202, cmt)
		c.Check(rspe.Change, Equals, "123", cmt)

		restoreGet()
		restoreLoad()
	}
}

func (s *confdbSuite) TestViewGetMany(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetView(func(_ *state.State, acc, confdbSchema, viewName string) (*confdb.View, error) {
		c.Assert(acc, Equals, "system")
		c.Assert(confdbSchema, Equals, "network")
		c.Assert(viewName, Equals, "wifi-setup")

		return s.schema.View(viewName), nil
	})
	defer restore()

	restore = daemon.MockConfdbstateLoadConfdbAsync(func(_ *state.State, view *confdb.View, requests []string) (string, error) {
		c.Assert(requests, DeepEquals, []string{"ssid", "password"})
		c.Assert(view.Name, Equals, "wifi-setup")
		return "123", nil
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/confdb/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.asyncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 202)
	c.Check(rspe.Change, Equals, "123")
}

func (s *confdbSuite) TestViewSetMany(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetView(func(st *state.State, account, confdbName, viewName string) (*confdb.View, error) {
		return s.schema.View(viewName), nil
	})
	defer restore()

	var calls int
	restore = daemon.MockConfdbstateGetTransaction(func(ctx *hookstate.Context, st *state.State, view *confdb.View) (*confdbstate.Transaction, confdbstate.CommitTxFunc, error) {
		c.Assert(ctx, IsNil)
		c.Assert(view.Name, Equals, "wifi-setup")
		c.Assert(view.Schema().Account, Equals, "system")
		c.Assert(view.Schema().Name, Equals, "network")

		return nil, func() (string, <-chan struct{}, error) { calls++; return "123", nil, nil }, nil
	})
	defer restore()

	restore = daemon.MockConfdbstateSetViaView(func(_ confdb.Databag, view *confdb.View, values map[string]interface{}) error {
		c.Assert(view.Name, Equals, "wifi-setup")
		c.Assert(values, DeepEquals, map[string]interface{}{"ssid": "foo", "password": "bar"})
		return nil
	})
	defer restore()

	buf := bytes.NewBufferString(`{"ssid": "foo", "password": "bar"}`)
	req, err := http.NewRequest("PUT", "/v2/confdb/system/network/wifi-setup", buf)
	c.Assert(err, IsNil)

	rspe := s.asyncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 202)
	c.Check(rspe.Change, Equals, "123")
}

func (s *confdbSuite) TestGetViewError(c *C) {
	s.setFeatureFlag(c)

	type test struct {
		name string
		err  error
		code int
	}

	for _, t := range []test{
		{name: "confdb not found", err: confdb.NewNotFoundError("boom"), code: 404},
		{name: "internal", err: errors.New("internal"), code: 500},
	} {
		restore := daemon.MockConfdbstateGetView(func(_ *state.State, _, _, _ string) (*confdb.View, error) {
			return nil, t.err
		})

		req, err := http.NewRequest("GET", "/v2/confdb/system/network/wifi-setup?fields=ssid", nil)
		c.Assert(err, IsNil, Commentf("%s test", t.name))

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, t.code, Commentf("%s test", t.name))
		restore()
	}
}

func (s *confdbSuite) TestGetViewMisshapenQuery(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetView(func(_ *state.State, acc, confdbSchema, viewName string) (*confdb.View, error) {
		c.Assert(acc, Equals, "system")
		c.Assert(confdbSchema, Equals, "network")
		c.Assert(viewName, Equals, "wifi-setup")

		return s.schema.View(viewName), nil
	})
	defer restore()

	restore = daemon.MockConfdbstateLoadConfdbAsync(func(_ *state.State, _ *confdb.View, requests []string) (string, error) {
		c.Check(requests, DeepEquals, []string{"foo.bar", "[1].foo", "foo"})
		return "123", nil
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/confdb/system/network/wifi-setup?fields=,foo.bar,,[1].foo,foo,", nil)
	c.Assert(err, IsNil)

	rspe := s.asyncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 202)
	c.Check(rspe.Change, Equals, "123")
}

func (s *confdbSuite) TestSetView(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetView(func(st *state.State, account, confdbName, viewName string) (*confdb.View, error) {

		return s.schema.View(viewName), nil
	})
	defer restore()

	type test struct {
		name  string
		value interface{}
	}

	for _, t := range []test{
		{name: "string", value: "foo"},
		{name: "integer", value: float64(123)},
		{name: "list", value: []interface{}{"foo", "bar"}},
		{name: "map", value: map[string]interface{}{"foo": "bar"}},
	} {
		cmt := Commentf("%s test", t.name)
		s.st.Lock()
		tx, err := confdbstate.NewTransaction(s.st, "system", "network")
		s.st.Unlock()
		c.Assert(err, IsNil, cmt)

		var calls int
		restoreGetTx := daemon.MockConfdbstateGetTransaction(func(ctx *hookstate.Context, st *state.State, view *confdb.View) (*confdbstate.Transaction, confdbstate.CommitTxFunc, error) {
			calls++
			c.Assert(ctx, IsNil, cmt)
			c.Assert(view.Name, Equals, "wifi-setup", cmt)
			c.Assert(view.Schema().Account, Equals, "system", cmt)
			c.Assert(view.Schema().Name, Equals, "network", cmt)

			return tx, func() (string, <-chan struct{}, error) { return "123", nil, nil }, nil
		})

		var called bool
		restoreSet := daemon.MockConfdbstateSetViaView(func(bag confdb.Databag, view *confdb.View, values map[string]interface{}) error {
			called = true
			c.Assert(view.Name, Equals, "wifi-setup")
			c.Assert(values, DeepEquals, map[string]interface{}{"ssid": t.value})
			return nil
		})

		jsonVal, err := json.Marshal(t.value)
		c.Check(err, IsNil, cmt)

		buf := bytes.NewBufferString(fmt.Sprintf(`{"ssid": %s}`, jsonVal))
		req, err := http.NewRequest("PUT", "/v2/confdb/system/network/wifi-setup", buf)
		c.Check(err, IsNil, cmt)
		req.Header.Set("Content-Type", "application/json")

		rspe := s.asyncReq(c, req, nil)
		c.Assert(rspe.Status, Equals, 202, cmt)
		c.Assert(rspe.Change, Equals, "123")
		c.Assert(called, Equals, true)

		restoreGetTx()
		restoreSet()
	}
}

func (s *confdbSuite) TestUnsetView(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetView(func(st *state.State, account, confdbName, viewName string) (*confdb.View, error) {
		return s.schema.View(viewName), nil
	})
	defer restore()

	s.st.Lock()
	tx, err := confdbstate.NewTransaction(s.st, "system", "network")
	s.st.Unlock()
	c.Assert(err, IsNil)

	err = tx.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	var calls int
	restore = daemon.MockConfdbstateGetTransaction(func(ctx *hookstate.Context, st *state.State, view *confdb.View) (*confdbstate.Transaction, confdbstate.CommitTxFunc, error) {
		calls++
		c.Assert(ctx, IsNil)
		c.Assert(view.Name, Equals, "wifi-setup")
		c.Assert(view.Schema().Account, Equals, "system")
		c.Assert(view.Schema().Name, Equals, "network")

		return tx, func() (string, <-chan struct{}, error) { return "123", nil, nil }, nil
	})
	defer restore()

	var called bool
	restore = daemon.MockConfdbstateSetViaView(func(bag confdb.Databag, view *confdb.View, values map[string]interface{}) error {
		called = true
		c.Assert(view.Name, Equals, "wifi-setup")
		c.Assert(values, DeepEquals, map[string]interface{}{"ssid": nil})
		return nil
	})
	defer restore()

	buf := bytes.NewBufferString(`{"ssid": null}`)
	req, err := http.NewRequest("PUT", "/v2/confdb/system/network/wifi-setup", buf)
	c.Check(err, IsNil)
	req.Header.Set("Content-Type", "application/json")

	rspe := s.asyncReq(c, req, nil)
	c.Assert(rspe.Status, Equals, 202)
	c.Assert(rspe.Change, Equals, "123")
	c.Assert(called, Equals, true)
}

func (s *confdbSuite) TestSetViewError(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetView(func(st *state.State, account, confdbName, viewName string) (*confdb.View, error) {
		return s.schema.View(viewName), nil
	})
	defer restore()

	type test struct {
		name string
		err  error
		code int
	}

	for _, t := range []test{
		{name: "not found", err: &confdb.NotFoundError{}, code: 404},
		{name: "internal", err: errors.New("internal"), code: 500},
	} {
		restore := daemon.MockConfdbstateGetTransaction(func(ctx *hookstate.Context, st *state.State, view *confdb.View) (*confdbstate.Transaction, confdbstate.CommitTxFunc, error) {
			return nil, nil, t.err
		})
		cmt := Commentf("%s test", t.name)

		buf := bytes.NewBufferString(`{"ssid": "foo"}`)
		req, err := http.NewRequest("PUT", "/v2/confdb/system/network/wifi-setup", buf)
		c.Assert(err, IsNil, cmt)
		req.Header.Set("Content-Type", "application/json")

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, t.code, cmt)
		restore()
	}
}

func (s *confdbSuite) TestSetViewBadRequests(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetTransaction(func(ctx *hookstate.Context, st *state.State, view *confdb.View) (*confdbstate.Transaction, confdbstate.CommitTxFunc, error) {
		err := errors.New("unexpected call to confdbstate.Set")
		c.Error(err)
		return nil, nil, err
	})
	defer restore()

	type testcase struct {
		body   *bytes.Buffer
		errMsg string
	}
	tcs := []testcase{
		{
			body:   &bytes.Buffer{},
			errMsg: "cannot decode confdb request body: EOF",
		},
		{
			body:   bytes.NewBufferString("{"),
			errMsg: "cannot decode confdb request body: unexpected EOF",
		},
	}

	for _, tc := range tcs {
		req, err := http.NewRequest("PUT", "/v2/confdb/system/network/wifi-setup", tc.body)
		req.Header.Set("Content-Type", "application/json")
		c.Assert(err, IsNil)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, 400)
		c.Check(rspe.Message, Equals, tc.errMsg)
	}
}

func (s *confdbSuite) TestSetFailUnsetFeatureFlag(c *C) {
	restore := daemon.MockConfdbstateGetView(func(st *state.State, account, confdbName, viewName string) (*confdb.View, error) {
		err := fmt.Errorf("unexpected call to confdbstate")
		c.Error(err)
		return nil, err
	})
	defer restore()

	buf := bytes.NewBufferString(`{"a.b.c": "foo"}`)
	req, err := http.NewRequest("PUT", "/v2/confdb/system/network/wifi-setup", buf)
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `"confdb" feature flag is disabled: set 'experimental.confdb' to true`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *confdbSuite) TestGetFailUnsetFeatureFlag(c *C) {
	req, err := http.NewRequest("GET", "/v2/confdb/system/network/wifi-setup?fields=my-field", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `"confdb" feature flag is disabled: set 'experimental.confdb' to true`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *confdbSuite) TestGetNoFields(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetView(func(_ *state.State, acc, confdbSchema, viewName string) (*confdb.View, error) {
		c.Assert(acc, Equals, "system")
		c.Assert(confdbSchema, Equals, "network")
		c.Assert(viewName, Equals, "wifi-setup")

		return s.schema.View(viewName), nil
	})
	defer restore()

	restore = daemon.MockConfdbstateLoadConfdbAsync(func(_ *state.State, _ *confdb.View, requests []string) (string, error) {
		c.Assert(requests, IsNil)
		return "123", nil
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/confdb/system/network/wifi-setup", nil)
	c.Assert(err, IsNil)

	rspe := s.asyncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 202)
	c.Check(rspe.Change, Equals, "123")
}
