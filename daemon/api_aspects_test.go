// -*- Mode: Go; indent-tabs-mode: t -*-
/*
 * Copyright (C) 2023 Canonical Ltd
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

	"gopkg.in/check.v1"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
)

type aspectsSuite struct {
	apiBaseSuite

	st *state.State
}

var _ = Suite(&aspectsSuite{})

func (s *aspectsSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectReadAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})

	s.st = state.New(nil)
	o := overlord.MockWithState(s.st)
	s.d = daemon.NewWithOverlord(o)

	s.st.Lock()
	databags := map[string]map[string]aspects.JSONDataBag{
		"system": {"network": aspects.NewJSONDataBag()},
	}
	s.st.Set("aspect-databags", databags)
	s.st.Unlock()
}

func (s *aspectsSuite) setFeatureFlag(c *C) {
	_, confOption := features.AspectsConfiguration.ConfigOption()

	s.st.Lock()
	defer s.st.Unlock()

	tr := config.NewTransaction(s.st)
	err := tr.Set("core", confOption, true)
	c.Assert(err, IsNil)
	tr.Commit()
}

func (s *aspectsSuite) TestGetAspect(c *C) {
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
		restore := daemon.MockAspectstateGet(func(_ *state.State, acc, bundleName, aspect string, fields []string) (interface{}, error) {
			c.Check(acc, Equals, "system", cmt)
			c.Check(bundleName, Equals, "network", cmt)
			c.Check(aspect, Equals, "wifi-setup", cmt)
			c.Check(fields, DeepEquals, []string{"ssid"}, cmt)

			return map[string]interface{}{"ssid": t.value}, nil
		})
		req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup?fields=ssid", nil)
		c.Assert(err, IsNil, cmt)

		rspe := s.syncReq(c, req, nil)
		c.Check(rspe.Status, Equals, 200, cmt)
		c.Check(rspe.Result, DeepEquals, map[string]interface{}{"ssid": t.value}, cmt)

		restore()
	}
}

func (s *aspectsSuite) TestAspectGetMany(c *C) {
	s.setFeatureFlag(c)

	var calls int
	restore := daemon.MockAspectstateGet(func(_ *state.State, _, _, _ string, _ []string) (interface{}, error) {
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

	req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.syncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 200)
	c.Check(rspe.Result, DeepEquals, map[string]interface{}{"ssid": "foo", "password": "bar"})
}

func (s *aspectsSuite) TestAspectGetSomeFieldNotFound(c *C) {
	s.setFeatureFlag(c)

	var calls int
	restore := daemon.MockAspectstateGet(func(_ *state.State, acc, bundle, aspect string, _ []string) (interface{}, error) {
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

	req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.syncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 200)
	c.Check(rspe.Result, DeepEquals, map[string]interface{}{"ssid": "foo"})
}

func (s *aspectsSuite) TestGetAspectNoFieldsFound(c *C) {
	s.setFeatureFlag(c)

	var calls int
	restore := daemon.MockAspectstateGet(func(_ *state.State, _, _, _ string, fields []string) (interface{}, error) {
		calls++
		switch calls {
		case 1:
			return nil, &aspects.NotFoundError{
				Account:    "system",
				BundleName: "network",
				Aspect:     "wifi-setup",
				Operation:  "get",
				Requests:   []string{"ssid", "password"},
				Cause:      "mocked",
			}
		default:
			err := fmt.Errorf("expected 1 call to Get, now on %d", calls)
			c.Error(err)
			return nil, err
		}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 404)
	c.Check(rspe.Error(), Equals, `cannot get "ssid", "password" in aspect system/network/wifi-setup: mocked (api 404)`)
}

func (s *aspectsSuite) TestAspectGetDatabagNotFound(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockAspectstateGet(func(_ *state.State, _, _, _ string, _ []string) (interface{}, error) {
		return nil, &aspects.NotFoundError{Account: "foo", BundleName: "network", Aspect: "wifi-setup", Operation: "get", Requests: []string{"ssid"}, Cause: "mocked"}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/aspects/foo/network/wifi-setup?fields=ssid", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 404)
	c.Check(rspe.Message, Equals, `cannot get "ssid" in aspect foo/network/wifi-setup: mocked`)
}

func (s *aspectsSuite) TestAspectSetManyWithExistingState(c *C) {
	s.st.Lock()

	databag := aspects.NewJSONDataBag()
	err := databag.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	databags := map[string]map[string]aspects.JSONDataBag{
		"system": {"network": databag},
	}
	s.st.Set("aspect-databags", databags)
	s.st.Unlock()

	s.testAspectSetMany(c)
}

func (s *aspectsSuite) TestAspectSetManyWithExistingEmptyState(c *C) {
	s.st.Lock()

	databags := map[string]map[string]aspects.JSONDataBag{
		"system": {"network": aspects.NewJSONDataBag()},
	}
	s.st.Set("aspect-databags", databags)
	s.st.Unlock()

	s.testAspectSetMany(c)
}

func (s *aspectsSuite) TestAspectSetMany(c *C) {
	s.testAspectSetMany(c)
}

func (s *aspectsSuite) testAspectSetMany(c *C) {
	s.setFeatureFlag(c)

	var calls int
	restore := daemon.MockAspectstateSet(func(st *state.State, account, bundle, aspect string, requests map[string]interface{}) error {
		calls++
		switch calls {
		case 1:
			c.Check(requests, DeepEquals, map[string]interface{}{"ssid": "foo", "password": nil})

			bag := aspects.NewJSONDataBag()
			err := bag.Set("wifi.ssid", "foo")
			c.Check(err, IsNil)
			err = bag.Set("wifi.psk", nil)
			c.Check(err, IsNil)

			st.Set("aspect-databags", map[string]map[string]aspects.JSONDataBag{account: {bundle: bag}})
		default:
			err := fmt.Errorf("expected 1 call, now on %d", calls)
			c.Error(err)
			return err
		}

		return nil
	})
	defer restore()

	buf := bytes.NewBufferString(`{"ssid": "foo", "password": null}`)
	req, err := http.NewRequest("PUT", "/v2/aspects/system/network/wifi-setup", buf)
	c.Assert(err, IsNil)

	rspe := s.asyncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 202)

	st := s.d.Overlord().State()
	st.Lock()
	defer st.Unlock()

	chg := st.Change(rspe.Change)
	c.Check(chg.Kind(), check.Equals, "set-aspect")
	c.Check(chg.Summary(), check.Equals, `Set aspect system/network/wifi-setup`)
	c.Check(chg.Status(), Equals, state.DoneStatus)

	var databags map[string]map[string]aspects.JSONDataBag
	err = st.Get("aspect-databags", &databags)
	c.Assert(err, IsNil)

	value, err := databags["system"]["network"].Get("wifi.ssid")
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "foo")

	value, err = databags["system"]["network"].Get("wifi.psk")
	c.Assert(err, FitsTypeOf, aspects.PathError(""))
	c.Assert(value, IsNil)
}

func (s *aspectsSuite) TestGetAspectError(c *C) {
	s.setFeatureFlag(c)

	type test struct {
		name string
		err  error
		code int
	}

	for _, t := range []test{
		{name: "aspect not found", err: &aspects.NotFoundError{}, code: 404},
		{name: "internal", err: errors.New("internal"), code: 500},
	} {
		restore := daemon.MockAspectstateGet(func(_ *state.State, _, _, _ string, _ []string) (interface{}, error) {
			return nil, t.err
		})

		req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup?fields=ssid", nil)
		c.Assert(err, IsNil, Commentf("%s test", t.name))

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, t.code, Commentf("%s test", t.name))
		restore()
	}
}

func (s *aspectsSuite) TestGetAspectMisshapenQuery(c *C) {
	s.setFeatureFlag(c)

	var calls int
	restore := daemon.MockAspectstateGet(func(_ *state.State, _, _, _ string, fields []string) (interface{}, error) {
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

	req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup?fields=,foo.bar,,[1].foo,foo,", nil)
	c.Assert(err, IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{"a": 1})
}

func (s *aspectsSuite) TestSetAspect(c *C) {
	s.setFeatureFlag(c)

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
		restore := daemon.MockAspectstateSet(func(st *state.State, acc, bundleName, aspect string, requests map[string]interface{}) error {
			c.Check(acc, Equals, "system", cmt)
			c.Check(bundleName, Equals, "network", cmt)
			c.Check(aspect, Equals, "wifi-setup", cmt)
			c.Check(requests, DeepEquals, map[string]interface{}{"ssid": t.value}, cmt)

			bag := aspects.NewJSONDataBag()
			err := bag.Set("wifi.ssid", t.value)
			c.Check(err, IsNil)
			st.Set("aspect-databags", map[string]map[string]aspects.JSONDataBag{acc: {bundleName: bag}})

			return nil
		})
		jsonVal, err := json.Marshal(t.value)
		c.Check(err, IsNil, cmt)

		buf := bytes.NewBufferString(fmt.Sprintf(`{"ssid": %s}`, jsonVal))
		req, err := http.NewRequest("PUT", "/v2/aspects/system/network/wifi-setup", buf)
		c.Check(err, IsNil, cmt)
		req.Header.Set("Content-Type", "application/json")

		rspe := s.asyncReq(c, req, nil)
		c.Check(rspe.Status, Equals, 202, cmt)

		st := s.d.Overlord().State()
		st.Lock()
		chg := st.Change(rspe.Change)
		st.Unlock()

		c.Check(chg.Kind(), Equals, "set-aspect", cmt)
		c.Check(chg.Summary(), Equals, `Set aspect system/network/wifi-setup`, cmt)

		st.Lock()
		c.Check(chg.Status(), Equals, state.DoneStatus)

		var databags map[string]map[string]aspects.JSONDataBag
		err = st.Get("aspect-databags", &databags)
		st.Unlock()
		c.Assert(err, IsNil)

		value, err := databags["system"]["network"].Get("wifi.ssid")
		c.Assert(err, IsNil)
		c.Assert(value, DeepEquals, t.value)

		restore()
	}
}

func (s *aspectsSuite) TestUnsetAspect(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockAspectstateSet(func(_ *state.State, acc, bundleName, aspect string, requests map[string]interface{}) error {
		c.Check(acc, Equals, "system")
		c.Check(bundleName, Equals, "network")
		c.Check(aspect, Equals, "wifi-setup")
		c.Check(requests, DeepEquals, map[string]interface{}{"ssid": nil})
		return nil
	})
	defer restore()

	buf := bytes.NewBufferString(`{"ssid": null}`)
	req, err := http.NewRequest("PUT", "/v2/aspects/system/network/wifi-setup", buf)
	c.Assert(err, IsNil)
	req.Header.Set("Content-Type", "application/json")

	rspe := s.asyncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 202)

	st := s.d.Overlord().State()
	st.Lock()
	chg := st.Change(rspe.Change)

	c.Check(chg.Kind(), check.Equals, "set-aspect")
	c.Check(chg.Summary(), check.Equals, `Set aspect system/network/wifi-setup`)
	c.Check(chg.Status(), Equals, state.DoneStatus)
	st.Unlock()
}

func (s *aspectsSuite) TestSetAspectError(c *C) {
	s.setFeatureFlag(c)

	type test struct {
		name string
		err  error
		code int
	}

	for _, t := range []test{
		{name: "not found", err: &aspects.NotFoundError{}, code: 404},
		{name: "internal", err: errors.New("internal"), code: 500},
	} {
		restore := daemon.MockAspectstateSet(func(*state.State, string, string, string, map[string]interface{}) error {
			return t.err
		})
		cmt := Commentf("%s test", t.name)

		buf := bytes.NewBufferString(`{"ssid": null}`)
		req, err := http.NewRequest("PUT", "/v2/aspects/system/network/wifi-setup", buf)
		c.Assert(err, IsNil, cmt)
		req.Header.Set("Content-Type", "application/json")

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, t.code, cmt)
		restore()
	}
}

func (s *aspectsSuite) TestSetAspectEmptyBody(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockAspectstateSet(func(*state.State, string, string, string, map[string]interface{}) error {
		err := errors.New("unexpected call to aspectstate.Set")
		c.Error(err)
		return err
	})
	defer restore()

	req, err := http.NewRequest("PUT", "/v2/aspects/system/network/wifi-setup", &bytes.Buffer{})
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
}

func (s *aspectsSuite) TestSetAspectBadRequest(c *C) {
	s.setFeatureFlag(c)

	buf := bytes.NewBufferString(`{`)
	req, err := http.NewRequest("PUT", "/v2/aspects/system/network/wifi-setup", buf)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, "cannot decode aspect request body: unexpected EOF")
}

func (s *aspectsSuite) TestGetBadRequest(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockAspectstateGet(func(_ *state.State, acc, bundleName, aspect string, fields []string) (interface{}, error) {
		return nil, &aspects.BadRequestError{
			Account:    "acc",
			BundleName: "bundle",
			Aspect:     "foo",
			Operation:  "get",
			Request:    "foo",
			Cause:      "bad request",
		}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/aspects/acc/bundle/foo?fields=foo", &bytes.Buffer{})
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `cannot get "foo" in aspect acc/bundle/foo: bad request`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *aspectsSuite) TestSetBadRequest(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockAspectstateSet(func(*state.State, string, string, string, map[string]interface{}) error {
		return &aspects.BadRequestError{
			Account:    "acc",
			BundleName: "bundle",
			Aspect:     "foo",
			Operation:  "set",
			Request:    "foo",
			Cause:      "bad request",
		}
	})
	defer restore()

	buf := bytes.NewBufferString(`{"a.b.c": "foo"}`)
	req, err := http.NewRequest("PUT", "/v2/aspects/acc/bundle/foo", buf)
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `cannot set "foo" in aspect acc/bundle/foo: bad request`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *aspectsSuite) TestSetFailUnsetFeatureFlag(c *C) {
	restore := daemon.MockAspectstateSet(func(*state.State, string, string, string, map[string]interface{}) error {
		err := fmt.Errorf("unexpected call to aspectstate")
		c.Error(err)
		return err
	})
	defer restore()

	buf := bytes.NewBufferString(`{"a.b.c": "foo"}`)
	req, err := http.NewRequest("PUT", "/v2/aspects/acc/bundle/foo", buf)
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `aspect-based configuration disabled: you must set 'experimental.aspects-configuration' to true`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *aspectsSuite) TestGetFailUnsetFeatureFlag(c *C) {
	restore := daemon.MockAspectstateSet(func(*state.State, string, string, string, map[string]interface{}) error {
		err := fmt.Errorf("unexpected call to aspectstate")
		c.Error(err)
		return err
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/aspects/acc/bundle/foo?fields=my-field", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, `aspect-based configuration disabled: you must set 'experimental.aspects-configuration' to true`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *aspectsSuite) TestGetNoFields(c *C) {
	s.setFeatureFlag(c)

	value := map[string]interface{}{"foo": 1, "bar": "baz", "nested": map[string]interface{}{"a": []interface{}{1, 2}}}
	restore := daemon.MockAspectstateGet(func(_ *state.State, _, _, _ string, fields []string) (interface{}, error) {
		c.Check(fields, IsNil)
		return value, nil
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/aspects/acc/bundle/foo", nil)
	c.Assert(err, IsNil)

	rspe := s.syncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 200)
	c.Check(rspe.Result, DeepEquals, value)
}
