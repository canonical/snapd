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
	"reflect"

	"gopkg.in/check.v1"
	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/testutil"
)

type aspectsSuite struct {
	apiBaseSuite
}

var _ = Suite(&aspectsSuite{})

func (s *aspectsSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectReadAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})

	st := state.New(nil)
	o := overlord.MockWithState(st)
	s.d = daemon.NewWithOverlord(o)

	st.Lock()
	databags := map[string]map[string]aspects.JSONDataBag{
		"system": {"network": aspects.NewJSONDataBag()},
	}
	st.Set("aspect-databags", databags)
	st.Unlock()

}

func (s *aspectsSuite) TestGetAspect(c *C) {
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
		restore := daemon.MockAspectstateGet(func(_ aspects.DataBag, acc, bundleName, aspect, field string, value interface{}) error {
			c.Check(acc, Equals, "system", cmt)
			c.Check(bundleName, Equals, "network", cmt)
			c.Check(aspect, Equals, "wifi-setup", cmt)
			c.Check(field, Equals, "ssid", cmt)

			outputValue := reflect.ValueOf(value).Elem()
			outputValue.Set(reflect.ValueOf(t.value))
			return nil
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
	var calls int
	restore := daemon.MockAspectstateGet(func(_ aspects.DataBag, _, _, _, _ string, value interface{}) error {
		calls++
		switch calls {
		case 1:
			*value.(*interface{}) = "foo"
		case 2:
			*value.(*interface{}) = "bar"
		default:
			err := fmt.Errorf("expected 2 calls, now on %d", calls)
			c.Error(err)
			return err
		}

		return nil
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.syncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 200)
	c.Check(rspe.Result, DeepEquals, map[string]interface{}{"ssid": "foo", "password": "bar"})
}

func (s *aspectsSuite) TestAspectGetSomeFieldNotFound(c *C) {
	var calls int
	restore := daemon.MockAspectstateGet(func(_ aspects.DataBag, acc, bundle, aspect, _ string, value interface{}) error {
		calls++
		switch calls {
		case 1:
			*value.(*interface{}) = "foo"
		case 2:
			return &aspects.NotFoundError{}
		default:
			err := fmt.Errorf("expected 2 calls, now on %d", calls)
			c.Error(err)
			return err
		}

		return nil
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.syncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 200)
	c.Check(rspe.Result, DeepEquals, map[string]interface{}{"ssid": "foo"})
}

func (s *aspectsSuite) TestGetAspectNoFieldsFound(c *C) {
	var calls int
	restore := daemon.MockAspectstateGet(func(_ aspects.DataBag, _, _, _, _ string, _ interface{}) error {
		calls++
		err := &aspects.NotFoundError{
			Account:    "foo",
			BundleName: "network",
			Aspect:     "wifi-setup",
			Cause:      "mocked",
		}

		switch calls {
		case 1:
			err.Field = "ssid"
		case 2:
			err.Field = "password"
		default:
			err := fmt.Errorf("expected 2 calls to Get, now on %d", calls)
			c.Error(err)
			return err
		}

		return err
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 404)
	c.Check(rspe.Error(), Equals, `cannot get fields "ssid", "password" of aspect system/network/wifi-setup (api 404)`)
}

func (s *aspectsSuite) TestAspectGetDatabagNotFound(c *C) {
	restore := daemon.MockAspectstateGet(func(_ aspects.DataBag, _, _, _, _ string, _ interface{}) error {
		return &aspects.NotFoundError{Account: "foo", BundleName: "network", Aspect: "wifi-setup", Field: "ssid", Cause: "mocked"}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/aspects/foo/network/wifi-setup?fields=ssid", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 404)
	c.Check(rspe.Message, Equals, `cannot find field "ssid" of aspect foo/network/wifi-setup: mocked`)
}

func (s *aspectsSuite) TestAspectSetManyWithExistingState(c *C) {
	st := s.d.Overlord().State()
	st.Lock()

	databag := aspects.NewJSONDataBag()
	err := databag.Set("wifi.ssid", "foo")
	c.Assert(err, IsNil)

	databags := map[string]map[string]aspects.JSONDataBag{
		"system": {"network": databag},
	}
	st.Set("aspect-databags", databags)
	st.Unlock()

	s.testAspectSetMany(c)
}

func (s *aspectsSuite) TestAspectSetManyWithExistingEmptyState(c *C) {
	st := s.d.Overlord().State()
	st.Lock()

	databags := map[string]map[string]aspects.JSONDataBag{
		"system": {"network": aspects.NewJSONDataBag()},
	}
	st.Set("aspect-databags", databags)
	st.Unlock()

	s.testAspectSetMany(c)
}

func (s *aspectsSuite) TestAspectSetMany(c *C) {
	s.testAspectSetMany(c)
}

func (s *aspectsSuite) testAspectSetMany(c *C) {
	var calls int
	restore := daemon.MockAspectstateSet(func(bag aspects.DataBag, _, _, _, field string, value interface{}) error {
		calls++
		switch calls {
		case 1, 2:
			if field == "ssid" {
				c.Assert(value, Equals, "foo")
				return bag.Set("wifi.ssid", value)
			} else if field == "password" {
				c.Assert(value, IsNil)
				return bag.Set("wifi.psk", nil)
			} else {
				c.Errorf("expected field to be \"ssid\" or \"password\" but got %q", field)
			}

		default:
			err := fmt.Errorf("expected 2 calls, now on %d", calls)
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

	var databags map[string]map[string]aspects.JSONDataBag
	err = st.Get("aspect-databags", &databags)
	c.Assert(err, IsNil)

	var value interface{}
	err = databags["system"]["network"].Get("wifi.ssid", &value)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "foo")

	err = databags["system"]["network"].Get("wifi.psk", &value)
	c.Assert(err, FitsTypeOf, aspects.PathNotFoundError(""))
}

func (s *aspectsSuite) TestGetAspectError(c *C) {
	type test struct {
		name string
		err  error
		code int
	}

	for _, t := range []test{
		{name: "aspect not found", err: &aspects.NotFoundError{}, code: 404},
		{name: "internal", err: errors.New("internal"), code: 500},
		{name: "invalid access", err: &aspects.InvalidAccessError{RequestedAccess: 1, FieldAccess: 2, Field: "foo"}, code: 403},
	} {
		restore := daemon.MockAspectstateGet(func(_ aspects.DataBag, _, _, _, _ string, _ interface{}) error {
			return t.err
		})

		req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup?fields=ssid", nil)
		c.Assert(err, IsNil, Commentf("%s test", t.name))

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, t.code, Commentf("%s test", t.name))
		restore()
	}
}

func (s *aspectsSuite) TestGetAspectMissingField(c *C) {
	req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Error(), Equals, "missing aspect fields (api)")
}

func (s *aspectsSuite) TestGetAspectMisshapenQuery(c *C) {
	var calls int
	restore := daemon.MockAspectstateGet(func(_ aspects.DataBag, _, _, _, field string, value interface{}) error {
		calls++
		switch calls {
		case 1:
			c.Check(field, Equals, "foo.bar")
		case 2:
			c.Check(field, Equals, "[1].foo")
		case 3:
			c.Check(field, Equals, "foo")
		default:
			c.Errorf("only expected 3 requests, now on %d", calls)
		}

		*value.(*interface{}) = calls
		return nil
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup?fields=,foo.bar,,[1].foo,foo,", nil)
	c.Assert(err, IsNil)

	rsp := s.syncReq(c, req, nil)
	c.Check(rsp.Status, Equals, 200)
	c.Check(rsp.Result, DeepEquals, map[string]interface{}{"foo.bar": 1, "[1].foo": 2, "foo": 3})
}

func (s *aspectsSuite) TestSetAspect(c *C) {
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
		restore := daemon.MockAspectstateSet(func(bag aspects.DataBag, acc, bundleName, aspect, field string, value interface{}) error {
			c.Check(acc, Equals, "system", cmt)
			c.Check(bundleName, Equals, "network", cmt)
			c.Check(aspect, Equals, "wifi-setup", cmt)
			c.Check(field, Equals, "ssid", cmt)
			c.Check(value, DeepEquals, t.value, cmt)
			c.Assert(bag, NotNil)
			return bag.Set("wifi.ssid", value)
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
		var databags map[string]map[string]aspects.JSONDataBag
		err = st.Get("aspect-databags", &databags)
		st.Unlock()
		c.Assert(err, IsNil)

		var value interface{}
		err = databags["system"]["network"].Get("wifi.ssid", &value)
		c.Assert(err, IsNil)
		c.Assert(value, DeepEquals, t.value)

		restore()
	}
}

func (s *aspectsSuite) TestUnsetAspect(c *C) {
	restore := daemon.MockAspectstateSet(func(_ aspects.DataBag, acc, bundleName, aspect, field string, value interface{}) error {
		c.Check(acc, Equals, "system")
		c.Check(bundleName, Equals, "network")
		c.Check(aspect, Equals, "wifi-setup")
		c.Check(field, Equals, "ssid")
		c.Check(value, testutil.IsInterfaceNil)
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
	st.Unlock()

	c.Check(chg.Kind(), check.Equals, "set-aspect")
	c.Check(chg.Summary(), check.Equals, `Set aspect system/network/wifi-setup`)
}

func (s *aspectsSuite) TestSetAspectError(c *C) {
	type test struct {
		name string
		err  error
		code int
	}

	for _, t := range []test{
		{name: "not found", err: &aspects.NotFoundError{}, code: 404},
		{name: "internal", err: errors.New("internal"), code: 500},
		{name: "invalid access", err: &aspects.InvalidAccessError{}, code: 403},
	} {
		restore := daemon.MockAspectstateSet(func(aspects.DataBag, string, string, string, string, interface{}) error {
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
	restore := daemon.MockAspectstateSet(func(aspects.DataBag, string, string, string, string, interface{}) error {
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
	buf := bytes.NewBufferString(`{`)
	req, err := http.NewRequest("PUT", "/v2/aspects/system/network/wifi-setup", buf)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, "cannot decode aspect request body: unexpected EOF")
}

func (s *aspectsSuite) TestSetAspectNotAllowed(c *C) {
	restore := daemon.MockAspectstateSet(func(_ aspects.DataBag, acc, bundleName, aspect, field string, val interface{}) error {
		return &aspects.InvalidAccessError{RequestedAccess: 2, FieldAccess: 1, Field: "foo"}
	})
	defer restore()

	buf := bytes.NewBufferString(`{"foo": "bar"}`)
	req, err := http.NewRequest("PUT", "/v2/aspects/system/network/wifi-setup", buf)
	c.Assert(err, IsNil)
	req.Header.Set("Content-Type", "application/json")

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 403)
	c.Check(rspe.Message, Equals, `cannot write field "foo": only supports read access`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *aspectsSuite) TestGetAspectNotAllowed(c *C) {
	restore := daemon.MockAspectstateGet(func(_ aspects.DataBag, acc, bundleName, aspect, field string, val interface{}) error {
		return &aspects.InvalidAccessError{RequestedAccess: 1, FieldAccess: 2, Field: "foo"}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/aspects/system/network/wifi-setup?fields=foo", &bytes.Buffer{})
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 403)
	c.Check(rspe.Message, Equals, `cannot read field "foo": only supports write access`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}
