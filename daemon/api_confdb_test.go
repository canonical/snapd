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

	st *state.State
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
	databags := map[string]map[string]confdb.JSONDataBag{
		"system": {"network": confdb.NewJSONDataBag()},
	}
	s.st.Set("confdb-databags", databags)
	s.st.Unlock()
}

func (s *confdbSuite) setFeatureFlag(c *C) {
	_, confOption := features.Confdbs.ConfigOption()

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
		restore := daemon.MockConfdbstateGet(func(_ *state.State, acc, confdb, view string, fields []string) (interface{}, error) {
			c.Check(acc, Equals, "system", cmt)
			c.Check(confdb, Equals, "network", cmt)
			c.Check(view, Equals, "wifi-setup", cmt)
			c.Check(fields, DeepEquals, []string{"ssid"}, cmt)

			return map[string]interface{}{"ssid": t.value}, nil
		})
		req, err := http.NewRequest("GET", "/v2/confdbs/system/network/wifi-setup?fields=ssid", nil)
		c.Assert(err, IsNil, cmt)

		rspe := s.syncReq(c, req, nil)
		c.Check(rspe.Status, Equals, 200, cmt)
		c.Check(rspe.Result, DeepEquals, map[string]interface{}{"ssid": t.value}, cmt)

		restore()
	}
}

func (s *confdbSuite) TestViewGetMany(c *C) {
	s.setFeatureFlag(c)

	var calls int
	restore := daemon.MockConfdbstateGet(func(_ *state.State, _, _, _ string, _ []string) (interface{}, error) {
		calls++
		switch calls {
		case 1:
			return map[string]interface{}{"ssid": "foo", "password": "bar"}, nil
		default:
			err := fmt.Errorf("expected 1 call, now on %d", calls)
			c.Error(err)
			return nil, err
		}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/confdbs/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.syncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 200)
	c.Check(rspe.Result, DeepEquals, map[string]interface{}{"ssid": "foo", "password": "bar"})
}

func (s *confdbSuite) TestViewGetSomeFieldNotFound(c *C) {
	s.setFeatureFlag(c)

	var calls int
	restore := daemon.MockConfdbstateGet(func(_ *state.State, acc, confdb, view string, _ []string) (interface{}, error) {
		calls++
		switch calls {
		case 1:
			return map[string]interface{}{"ssid": "foo"}, nil
		default:
			err := fmt.Errorf("expected 1 call, now on %d", calls)
			c.Error(err)
			return nil, err
		}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/confdbs/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.syncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 200)
	c.Check(rspe.Result, DeepEquals, map[string]interface{}{"ssid": "foo"})
}

func (s *confdbSuite) TestGetViewNoFieldsFound(c *C) {
	s.setFeatureFlag(c)

	var calls int
	restore := daemon.MockConfdbstateGet(func(_ *state.State, _, _, _ string, fields []string) (interface{}, error) {
		calls++
		switch calls {
		case 1:
			return nil, confdb.NewNotFoundError("not found")
		default:
			err := fmt.Errorf("expected 1 call to Get, now on %d", calls)
			c.Error(err)
			return nil, err
		}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/confdbs/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 404)
	c.Check(rspe.Error(), Equals, `not found (api 404)`)
}

func (s *confdbSuite) TestViewGetDatabagNotFound(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGet(func(_ *state.State, _, _, _ string, _ []string) (interface{}, error) {
		return nil, confdb.NewNotFoundError("not found")
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/confdbs/foo/network/wifi-setup?fields=ssid", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 404)
	c.Check(rspe.Message, Equals, `not found`)
}

func (s *confdbSuite) TestViewSetMany(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetView(func(st *state.State, account, confdbName, viewName string) (*confdb.View, error) {
		views := map[string]interface{}{
			"wifi-setup": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "ssid", "storage": "wifi.ssid"},
					map[string]interface{}{"request": "password", "storage": "wifi.psk"},
				},
			},
		}

		db, err := confdb.New("system", "network", views, confdb.NewJSONSchema())
		c.Assert(err, IsNil)

		return db.View(viewName), nil
	})
	defer restore()

	s.st.Lock()
	tx, err := confdbstate.NewTransaction(s.st, "system", "network")
	s.st.Unlock()
	c.Assert(err, IsNil)

	var calls int
	restore = daemon.MockConfdbstateGetTransaction(func(ctx *hookstate.Context, st *state.State, view *confdb.View) (*confdbstate.Transaction, confdbstate.CommitTxFunc, error) {
		calls++
		c.Assert(ctx, IsNil)
		c.Assert(view.Name, Equals, "wifi-setup")
		c.Assert(view.Confdb().Account, Equals, "system")
		c.Assert(view.Confdb().Name, Equals, "network")

		c.Assert(err, IsNil)

		return tx, func() (string, <-chan struct{}, error) { return "123", nil, nil }, nil
	})
	defer restore()

	buf := bytes.NewBufferString(`{"ssid": "foo", "password": "bar"}`)
	req, err := http.NewRequest("PUT", "/v2/confdbs/system/network/wifi-setup", buf)
	c.Assert(err, IsNil)

	rspe := s.asyncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 202)

	st := s.d.Overlord().State()
	st.Lock()
	defer st.Unlock()

	c.Assert(rspe.Change, Equals, "123")

	val, err := tx.Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")

	val, err = tx.Get("wifi.psk")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "bar")
}

func (s *confdbSuite) TestGetViewError(c *C) {
	s.setFeatureFlag(c)

	type test struct {
		name string
		err  error
		code int
	}

	for _, t := range []test{
		{name: "confdb not found", err: &confdb.NotFoundError{}, code: 404},
		{name: "internal", err: errors.New("internal"), code: 500},
	} {
		restore := daemon.MockConfdbstateGet(func(_ *state.State, _, _, _ string, _ []string) (interface{}, error) {
			return nil, t.err
		})

		req, err := http.NewRequest("GET", "/v2/confdbs/system/network/wifi-setup?fields=ssid", nil)
		c.Assert(err, IsNil, Commentf("%s test", t.name))

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, t.code, Commentf("%s test", t.name))
		restore()
	}
}

func (s *confdbSuite) TestGetViewMisshapenQuery(c *C) {
	s.setFeatureFlag(c)

	var calls int
	restore := daemon.MockConfdbstateGet(func(_ *state.State, _, _, _ string, fields []string) (interface{}, error) {
		calls++
		switch calls {
		case 1:
			c.Check(fields, DeepEquals, []string{"foo.bar", "[1].foo", "foo"})
			return map[string]interface{}{"a": 1}, nil
		default:
			err := fmt.Errorf("expected 1 call, now on %d", calls)
			c.Error(err)
			return nil, err
		}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/confdbs/system/network/wifi-setup?fields=,foo.bar,,[1].foo,foo,", nil)
	c.Assert(err, IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{"a": 1})
}

func (s *confdbSuite) TestSetView(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetView(func(st *state.State, account, confdbName, viewName string) (*confdb.View, error) {
		views := map[string]interface{}{
			"wifi-setup": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "ssid", "storage": "wifi.ssid"},
				},
			},
		}

		db, err := confdb.New("system", "network", views, confdb.NewJSONSchema())
		c.Assert(err, IsNil)

		return db.View(viewName), nil
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
		restore := daemon.MockConfdbstateGetTransaction(func(ctx *hookstate.Context, st *state.State, view *confdb.View) (*confdbstate.Transaction, confdbstate.CommitTxFunc, error) {
			calls++
			c.Assert(ctx, IsNil, cmt)
			c.Assert(view.Name, Equals, "wifi-setup", cmt)
			c.Assert(view.Confdb().Account, Equals, "system", cmt)
			c.Assert(view.Confdb().Name, Equals, "network", cmt)

			return tx, func() (string, <-chan struct{}, error) { return "123", nil, nil }, nil
		})

		jsonVal, err := json.Marshal(t.value)
		c.Check(err, IsNil, cmt)

		buf := bytes.NewBufferString(fmt.Sprintf(`{"ssid": %s}`, jsonVal))
		req, err := http.NewRequest("PUT", "/v2/confdbs/system/network/wifi-setup", buf)
		c.Check(err, IsNil, cmt)
		req.Header.Set("Content-Type", "application/json")

		rspe := s.asyncReq(c, req, nil)
		c.Check(rspe.Status, Equals, 202, cmt)

		c.Assert(rspe.Change, Equals, "123")
		val, err := tx.Get("wifi.ssid")
		c.Assert(err, IsNil, cmt)
		c.Assert(val, DeepEquals, t.value, cmt)

		restore()
	}
}

func (s *confdbSuite) TestUnsetView(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetView(func(st *state.State, account, confdbName, viewName string) (*confdb.View, error) {
		views := map[string]interface{}{
			"wifi-setup": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "ssid", "storage": "wifi.ssid"},
				},
			},
		}

		db, err := confdb.New("system", "network", views, confdb.NewJSONSchema())
		c.Assert(err, IsNil)

		return db.View(viewName), nil
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
		c.Assert(view.Confdb().Account, Equals, "system")
		c.Assert(view.Confdb().Name, Equals, "network")

		return tx, func() (string, <-chan struct{}, error) { return "123", nil, nil }, nil
	})
	defer restore()

	buf := bytes.NewBufferString(`{"ssid": null}`)
	req, err := http.NewRequest("PUT", "/v2/confdbs/system/network/wifi-setup", buf)
	c.Check(err, IsNil)
	req.Header.Set("Content-Type", "application/json")

	rspe := s.asyncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 202)

	c.Assert(rspe.Change, Equals, "123")
	val, err := tx.Get("wifi.ssid")
	c.Assert(err, FitsTypeOf, confdb.PathError(""))
	c.Assert(val, IsNil)
}

func (s *confdbSuite) TestSetViewError(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetView(func(st *state.State, account, confdbName, viewName string) (*confdb.View, error) {
		views := map[string]interface{}{
			"wifi-setup": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "ssid", "storage": "wifi.ssid"},
				},
			},
		}

		db, err := confdb.New("system", "network", views, confdb.NewJSONSchema())
		c.Assert(err, IsNil)

		return db.View(viewName), nil
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
		req, err := http.NewRequest("PUT", "/v2/confdbs/system/network/wifi-setup", buf)
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
		req, err := http.NewRequest("PUT", "/v2/confdbs/system/network/wifi-setup", tc.body)
		req.Header.Set("Content-Type", "application/json")
		c.Assert(err, IsNil)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, 400)
		c.Check(rspe.Message, Equals, tc.errMsg)
	}
}

func (s *confdbSuite) TestGetBadRequest(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGet(func(_ *state.State, acc, confdbName, view string, fields []string) (interface{}, error) {
		return nil, &confdb.BadRequestError{
			Account:    "acc",
			ConfdbName: "db",
			View:       "foo",
			Operation:  "get",
			Request:    "foo",
			Cause:      "bad request",
		}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/confdbs/acc/db/foo?fields=foo", &bytes.Buffer{})
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `cannot get "foo" in confdb view acc/db/foo: bad request`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *confdbSuite) TestSetBadRequest(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockConfdbstateGetView(func(st *state.State, account, confdbName, viewName string) (*confdb.View, error) {
		// this could be returned when setting the databag, not getting the view
		// but the error handling is the same so this shortens the test
		return nil, &confdb.BadRequestError{
			Account:    "acc",
			ConfdbName: "db",
			View:       "foo",
			Operation:  "set",
			Request:    "foo",
			Cause:      "bad request",
		}
	})
	defer restore()

	buf := bytes.NewBufferString(`{"a.b.c": "foo"}`)
	req, err := http.NewRequest("PUT", "/v2/confdbs/acc/db/foo", buf)
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `cannot set "foo" in confdb view acc/db/foo: bad request`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *confdbSuite) TestSetFailUnsetFeatureFlag(c *C) {

	restore := daemon.MockConfdbstateGetView(func(st *state.State, account, confdbName, viewName string) (*confdb.View, error) {
		err := fmt.Errorf("unexpected call to confdbstate")
		c.Error(err)
		return nil, err
	})
	defer restore()

	buf := bytes.NewBufferString(`{"a.b.c": "foo"}`)
	req, err := http.NewRequest("PUT", "/v2/confdbs/acc/db/foo", buf)
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `"confdbs" feature flag is disabled: set 'experimental.confdbs' to true`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *confdbSuite) TestGetFailUnsetFeatureFlag(c *C) {
	restore := daemon.MockConfdbstateGet(func(*state.State, string, string, string, []string) (interface{}, error) {
		err := fmt.Errorf("unexpected call to confdbstate")
		c.Error(err)
		return nil, err
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/confdbs/acc/db/foo?fields=my-field", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `"confdbs" feature flag is disabled: set 'experimental.confdbs' to true`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *confdbSuite) TestGetNoFields(c *C) {
	s.setFeatureFlag(c)

	value := map[string]interface{}{"foo": 1, "bar": "baz", "nested": map[string]interface{}{"a": []interface{}{1, 2}}}
	restore := daemon.MockConfdbstateGet(func(_ *state.State, _, _, _ string, fields []string) (interface{}, error) {
		c.Check(fields, IsNil)
		return value, nil
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/confdbs/acc/db/foo", nil)
	c.Assert(err, IsNil)

	rspe := s.syncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 200)
	c.Check(rspe.Result, DeepEquals, value)
}
