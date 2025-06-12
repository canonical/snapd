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
	"time"

	"gopkg.in/check.v1"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/assertstate"
	"github.com/snapcore/snapd/overlord/auth"
	"github.com/snapcore/snapd/overlord/confdbstate"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/devicestate"
	"github.com/snapcore/snapd/overlord/devicestate/devicestatetest"
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

	views := map[string]any{
		"wifi-setup": map[string]any{
			"rules": []any{
				map[string]any{"request": "ssid", "storage": "wifi.ssid"},
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
		value any
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

		req, err := http.NewRequest("GET", "/v2/confdb/system/network/wifi-setup?keys=ssid", nil)
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

	req, err := http.NewRequest("GET", "/v2/confdb/system/network/wifi-setup?keys=ssid,password", nil)
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

	restore = daemon.MockConfdbstateSetViaView(func(_ confdb.Databag, view *confdb.View, values map[string]any) error {
		c.Assert(view.Name, Equals, "wifi-setup")
		c.Assert(values, DeepEquals, map[string]any{"ssid": "foo", "password": "bar"})
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
		name   string
		err    error
		status int
		kind   client.ErrorKind
	}

	notFoundErr := &asserts.NotFoundError{
		Type:    asserts.ConfdbSchemaType,
		Headers: map[string]string{"name": "network", "account-id": "system"},
	}

	for _, t := range []test{
		{name: "no assertion", err: notFoundErr, status: 400, kind: client.ErrorKindAssertionNotFound},
		{name: "no view", err: &confdbstate.NoViewError{}, status: 400, kind: client.ErrorKindConfdbViewNotFound},
		{name: "internal", err: errors.New("internal"), status: 500},
	} {
		restore := daemon.MockConfdbstateGetView(func(_ *state.State, _, _, _ string) (*confdb.View, error) {
			return nil, t.err
		})

		cmt := Commentf("%s test", t.name)
		req, err := http.NewRequest("GET", "/v2/confdb/system/network/wifi-setup?keys=ssid", nil)
		c.Assert(err, IsNil, cmt)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, t.status, cmt)
		c.Check(rspe.Kind, Equals, t.kind, cmt)

		buf := bytes.NewBufferString(`{"ssid": "foo", "password": "bar"}`)
		req, err = http.NewRequest("PUT", "/v2/confdb/system/network/wifi-setup", buf)
		c.Assert(err, IsNil, cmt)

		rspe = s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, t.status, cmt)
		c.Check(rspe.Kind, Equals, t.kind, cmt)
		restore()
	}
}

func (s *confdbSuite) TestGetTxError(c *C) {
	s.setFeatureFlag(c)

	type test struct {
		name   string
		err    error
		status int
		kind   client.ErrorKind
	}

	view := s.schema.View("wifi-setup")
	restore := daemon.MockConfdbstateGetView(func(_ *state.State, acc, confdbSchema, viewName string) (*confdb.View, error) {
		return view, nil
	})
	defer restore()

	for _, t := range []test{
		{name: "no data", err: confdb.NewNoDataError(view, nil), status: 400, kind: client.ErrorKindConfigNoSuchOption},
		{name: "no match", err: confdb.NewNoMatchError(view, "", nil), status: 400, kind: client.ErrorKindConfdbNoMatchingRule},
		{name: "internal", err: errors.New("internal"), status: 500},
	} {
		restore := daemon.MockConfdbstateLoadConfdbAsync(func(*state.State, *confdb.View, []string) (string, error) {
			return "", t.err
		})

		cmt := Commentf("%s test", t.name)
		req, err := http.NewRequest("GET", "/v2/confdb/system/network/wifi-setup?fields=ssid", nil)
		c.Assert(err, IsNil, cmt)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, t.status, cmt)
		c.Check(rspe.Kind, Equals, t.kind, cmt)
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

	req, err := http.NewRequest("GET", "/v2/confdb/system/network/wifi-setup?keys=,foo.bar,,[1].foo,foo,", nil)
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
		value any
	}

	for _, t := range []test{
		{name: "string", value: "foo"},
		{name: "integer", value: float64(123)},
		{name: "list", value: []any{"foo", "bar"}},
		{name: "map", value: map[string]any{"foo": "bar"}},
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
		restoreSet := daemon.MockConfdbstateSetViaView(func(bag confdb.Databag, view *confdb.View, values map[string]any) error {
			called = true
			c.Assert(view.Name, Equals, "wifi-setup")
			c.Assert(values, DeepEquals, map[string]any{"ssid": t.value})
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
	restore = daemon.MockConfdbstateSetViaView(func(bag confdb.Databag, view *confdb.View, values map[string]any) error {
		called = true
		c.Assert(view.Name, Equals, "wifi-setup")
		c.Assert(values, DeepEquals, map[string]any{"ssid": nil})
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
		name   string
		err    error
		status int
		kind   client.ErrorKind
	}

	view := s.schema.View("wifi-setup")
	for _, t := range []test{
		{name: "no data", err: confdb.NewNoDataError(view, nil), status: 400, kind: client.ErrorKindConfigNoSuchOption},
		{name: "no match", err: confdb.NewNoMatchError(view, "", nil), status: 400, kind: client.ErrorKindConfdbNoMatchingRule},
		{name: "internal", err: errors.New("internal"), status: 500},
		{name: "bad query", err: &confdb.BadRequestError{}, status: 400},
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
		c.Check(rspe.Status, Equals, t.status, cmt)
		c.Check(rspe.Kind, Equals, t.kind, cmt)
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
	c.Check(rspe.Message, Equals, `feature flag "confdb" is disabled: set 'experimental.confdb' to true`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *confdbSuite) TestGetFailUnsetFeatureFlag(c *C) {
	req, err := http.NewRequest("GET", "/v2/confdb/system/network/wifi-setup?keys=my-key", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `feature flag "confdb" is disabled: set 'experimental.confdb' to true`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *confdbSuite) TestGetNoKeys(c *C) {
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

type confdbControlSuite struct {
	apiBaseSuite

	st     *state.State
	brands *assertstest.SigningAccounts
	serial *asserts.Serial
}

var _ = Suite(&confdbControlSuite{})

var (
	deviceKey, _ = assertstest.GenerateKey(752)
)

func (s *confdbControlSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})

	s.st = state.New(nil)
	o := overlord.MockWithState(s.st)
	s.d = daemon.NewWithOverlord(o)

	hookMgr, err := hookstate.Manager(s.st, o.TaskRunner())
	c.Assert(err, check.IsNil)

	deviceMgr, err := devicestate.Manager(s.st, hookMgr, o.TaskRunner(), nil)
	c.Assert(err, check.IsNil)
	o.AddManager(deviceMgr)

	assertMgr, err := assertstate.Manager(s.st, o.TaskRunner())
	c.Assert(err, check.IsNil)
	o.AddManager(assertMgr)

	storeStack := assertstest.NewStoreStack("can0nical", nil)
	s.brands = assertstest.NewSigningAccounts(storeStack)
}

func (s *confdbControlSuite) setFeatureFlag(c *C, confName string) {
	s.st.Lock()
	tr := config.NewTransaction(s.st)
	err := tr.Set("core", confName, true)
	c.Assert(err, IsNil)
	tr.Commit()
	s.st.Unlock()
}

func (s *confdbControlSuite) prereqs(c *C) {
	s.setFeatureFlag(c, "experimental.confdb")
	s.setFeatureFlag(c, "experimental.confdb-control")

	s.st.Lock()
	encDevKey, _ := asserts.EncodePublicKey(deviceKey.PublicKey())
	a, err := s.brands.Signing("can0nical").Sign(asserts.SerialType, map[string]any{
		"brand-id":            "can0nical",
		"model":               "generic-classic",
		"serial":              "serial-serial",
		"device-key":          string(encDevKey),
		"device-key-sha3-384": deviceKey.PublicKey().ID(),
		"timestamp":           time.Now().Format(time.RFC3339),
	}, nil, "")
	c.Assert(err, IsNil)

	err = assertstate.Add(s.st, a)
	c.Assert(err, IsNil)
	s.serial = a.(*asserts.Serial)

	devicestatetest.SetDevice(s.st, &auth.DeviceState{
		Brand:  "can0nical",
		Model:  "generic-classic",
		Serial: "serial-serial",
	})
	s.st.Unlock()
}

func (s *confdbControlSuite) TestConfdbFlagNotEnabled(c *C) {
	req, err := http.NewRequest("POST", "/v2/confdb", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `feature flag "confdb" is disabled: set 'experimental.confdb' to true`)
}

func (s *confdbControlSuite) TestConfdbControlFlagNotEnabled(c *C) {
	s.setFeatureFlag(c, "experimental.confdb")

	req, err := http.NewRequest("POST", "/v2/confdb", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `feature flag "confdb-control" is disabled: set 'experimental.confdb-control' to true`)
}

func (s *confdbControlSuite) TestConfdbControlActionNoSerial(c *C) {
	s.setFeatureFlag(c, "experimental.confdb")
	s.setFeatureFlag(c, "experimental.confdb-control")

	req, err := http.NewRequest("POST", "/v2/confdb", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 500)
	c.Check(rspe.Message, Equals, "device has no identity yet")
}

func (s *confdbControlSuite) TestConfdbControlActionOK(c *C) {
	s.prereqs(c)
	jane := map[string]any{
		"operators":       []any{"jane"},
		"authentications": []any{"store"},
		"views":           []any{"account/confdb/view"},
	}
	restore := daemon.MockDeviceStateSignConfdbControl(func(m *devicestate.DeviceManager, groups []any, revision int) (*asserts.ConfdbControl, error) {
		a, err := asserts.SignWithoutAuthority(asserts.ConfdbControlType, map[string]any{
			"brand-id": "can0nical",
			"model":    "generic-classic",
			"serial":   "serial-serial",
			"groups":   []any{jane},
		}, nil, deviceKey)
		c.Assert(err, IsNil)
		return a.(*asserts.ConfdbControl), nil
	})
	defer restore()

	body := `{"action": "delegate", "operator-id": "jane", "authentications": ["store"], "views": ["account/confdb/view"]}`
	req, err := http.NewRequest("POST", "/v2/confdb", bytes.NewBufferString(body))
	c.Assert(err, IsNil)
	s.asUserAuth(c, req)

	rsp := s.syncReq(c, req, nil)
	c.Assert(rsp.Status, Equals, 200)
	c.Check(rsp.Result, DeepEquals, nil)

	s.st.Lock()
	a, err := assertstate.DB(s.st).Find(
		asserts.ConfdbControlType,
		map[string]string{"brand-id": "can0nical", "model": "generic-classic", "serial": "serial-serial"},
	)
	c.Assert(err, IsNil)
	cc := a.(*asserts.ConfdbControl)
	ctrl := cc.Control()
	c.Check(ctrl.Groups(), DeepEquals, []any{jane})
	s.st.Unlock()
}

func (s *confdbControlSuite) TestConfdbControlActionSigningErr(c *C) {
	s.prereqs(c)

	body := `{"action": "delegate", "operator-id": "jane", "authentications": ["store"], "views": ["account/confdb/view"]}`
	req, err := http.NewRequest("POST", "/v2/confdb", bytes.NewBufferString(body))
	c.Assert(err, IsNil)
	s.asUserAuth(c, req)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 500)
	c.Check(rspe.Message, Equals, "cannot sign confdb-control without device key")
}

func (s *confdbControlSuite) TestConfdbControlActionAckErr(c *C) {
	s.prereqs(c)
	restore := daemon.MockDeviceStateSignConfdbControl(func(m *devicestate.DeviceManager, groups []any, revision int) (*asserts.ConfdbControl, error) {
		a := assertstest.FakeAssertion(
			map[string]any{
				"type":     "confdb-control",
				"brand-id": "can0nical",
				"model":    "generic-classic",
				"serial":   "serial-serial",
				"groups":   []any{},
			},
		)
		return a.(*asserts.ConfdbControl), nil
	})
	defer restore()

	s.st.Lock()
	jane := map[string]any{
		"operators":       []any{"jane"},
		"authentications": []any{"store"},
		"views":           []any{"account/confdb/view"},
	}
	a, err := asserts.SignWithoutAuthority(asserts.ConfdbControlType, map[string]any{
		"brand-id": "can0nical",
		"model":    "generic-classic",
		"serial":   "serial-serial",
		"groups":   []any{jane},
	}, nil, deviceKey)
	c.Assert(err, IsNil)
	assertstate.Add(s.st, a)
	s.st.Unlock()

	body := `{"action": "undelegate", "operator-id": "jane", "authentications": ["store"]}`
	req, err := http.NewRequest("POST", "/v2/confdb", bytes.NewBufferString(body))
	c.Assert(err, IsNil)
	s.asUserAuth(c, req)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 500)
	c.Check(
		rspe.Message,
		Equals,
		`cannot check no-authority assertion type "confdb-control": confdb-control's signing key doesn't match the device key`,
	)
}

func (s *confdbControlSuite) TestConfdbControlActionInvalidRequest(c *C) {
	s.prereqs(c)

	type testcase struct {
		body   string
		errMsg string
	}
	tcs := []testcase{
		{
			body:   "}",
			errMsg: "cannot decode request body: invalid character '}' looking for beginning of value",
		},
		{
			body:   `{"action": "unknown", "operator-id": "jane"}`,
			errMsg: `unknown action "unknown"`,
		},
		{
			body:   `{"action": "delegate", "operator-id": "jane", "authentications": ["unknown"]}`,
			errMsg: "cannot delegate: invalid authentication method: unknown",
		},
	}

	for _, tc := range tcs {
		req, err := http.NewRequest("POST", "/v2/confdb", bytes.NewBufferString(tc.body))
		c.Assert(err, IsNil)
		s.asUserAuth(c, req)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, 400)
		c.Check(rspe.Message, Equals, tc.errMsg)
	}
}
