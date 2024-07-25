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
	"fmt"
	"net/http"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/asserts/assertstest"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/features"
	"github.com/snapcore/snapd/overlord"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"github.com/snapcore/snapd/registry"
)

type registrySuite struct {
	apiBaseSuite

	st *state.State
}

var _ = Suite(&registrySuite{})

func (s *registrySuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectReadAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})

	s.st = state.New(nil)
	o := overlord.MockWithState(s.st)
	s.d = daemon.NewWithOverlord(o)

	s.st.Lock()
	databags := map[string]map[string]registry.JSONDataBag{
		"system": {"network": registry.NewJSONDataBag()},
	}
	s.st.Set("registry-databags", databags)

	devPrivKey, _ := assertstest.GenerateKey(752)
	signingDB := assertstest.NewSigningDB("acc", devPrivKey)
	c.Assert(signingDB, NotNil)

	headers := map[string]interface{}{
		"authority-id": "system",
		"account-id":   "system",
		"name":         "network",
		"views": map[string]interface{}{
			"setup-wifi": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "ssid", "storage": "wifi.ssid"},
				},
			},
		},
		"timestamp": "2030-11-06T09:16:26Z",
	}

	body := []byte(`{
  "storage": {
    "schema": {
      "wifi": "any"
    }
  }
}`)
	as, err := signingDB.Sign(asserts.RegistryType, headers, body, "")
	c.Assert(err, IsNil)
	reg := as.(*asserts.Registry)

	restore := daemon.MockAssertstateRegistry(func(_ *state.State, account, registryName string) (*asserts.Registry, error) {
		return reg, nil
	})
	s.AddCleanup(restore)
	s.st.Unlock()
}

func (s *registrySuite) setFeatureFlag(c *C) {
	_, confOption := features.Registries.ConfigOption()

	s.st.Lock()
	defer s.st.Unlock()

	tr := config.NewTransaction(s.st)
	err := tr.Set("core", confOption, true)
	c.Assert(err, IsNil)
	tr.Commit()
}

func (s *registrySuite) TestGetView(c *C) {
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
		restore := daemon.MockRegistrystateGetViaView(func(_ *state.State, acc, registry, view string, fields []string) (interface{}, error) {
			c.Check(acc, Equals, "system", cmt)
			c.Check(registry, Equals, "network", cmt)
			c.Check(view, Equals, "wifi-setup", cmt)
			c.Check(fields, DeepEquals, []string{"ssid"}, cmt)

			return map[string]interface{}{"ssid": t.value}, nil
		})
		req, err := http.NewRequest("GET", "/v2/registry/system/network/wifi-setup?fields=ssid", nil)
		c.Assert(err, IsNil, cmt)

		rspe := s.syncReq(c, req, nil)
		c.Check(rspe.Status, Equals, 200, cmt)
		c.Check(rspe.Result, DeepEquals, map[string]interface{}{"ssid": t.value}, cmt)

		restore()
	}
}

func (s *registrySuite) TestViewGetMany(c *C) {
	s.setFeatureFlag(c)

	var calls int
	restore := daemon.MockRegistrystateGetViaView(func(_ *state.State, _, _, _ string, _ []string) (interface{}, error) {
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

	req, err := http.NewRequest("GET", "/v2/registry/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.syncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 200)
	c.Check(rspe.Result, DeepEquals, map[string]interface{}{"ssid": "foo", "password": "bar"})
}

func (s *registrySuite) TestViewGetSomeFieldNotFound(c *C) {
	s.setFeatureFlag(c)

	var calls int
	restore := daemon.MockRegistrystateGetViaView(func(_ *state.State, acc, registry, view string, _ []string) (interface{}, error) {
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

	req, err := http.NewRequest("GET", "/v2/registry/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.syncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 200)
	c.Check(rspe.Result, DeepEquals, map[string]interface{}{"ssid": "foo"})
}

func (s *registrySuite) TestGetViewNoFieldsFound(c *C) {
	s.setFeatureFlag(c)

	var calls int
	restore := daemon.MockRegistrystateGetViaView(func(_ *state.State, _, _, _ string, fields []string) (interface{}, error) {
		calls++
		switch calls {
		case 1:
			return nil, &registry.NotFoundError{
				Account:      "system",
				RegistryName: "network",
				View:         "wifi-setup",
				Operation:    "get",
				Requests:     []string{"ssid", "password"},
				Cause:        "mocked",
			}
		default:
			err := fmt.Errorf("expected 1 call to Get, now on %d", calls)
			c.Error(err)
			return nil, err
		}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/registry/system/network/wifi-setup?fields=ssid,password", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 404)
	c.Check(rspe.Error(), Equals, `cannot get "ssid", "password" in registry view system/network/wifi-setup: mocked (api 404)`)
}

func (s *registrySuite) TestViewGetDatabagNotFound(c *C) {
	s.setFeatureFlag(c)

	restore := daemon.MockRegistrystateGetViaView(func(_ *state.State, _, _, _ string, _ []string) (interface{}, error) {
		return nil, &registry.NotFoundError{Account: "foo", RegistryName: "network", View: "wifi-setup", Operation: "get", Requests: []string{"ssid"}, Cause: "mocked"}
	})
	defer restore()

	req, err := http.NewRequest("GET", "/v2/registry/foo/network/wifi-setup?fields=ssid", nil)
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 404)
	c.Check(rspe.Message, Equals, `cannot get "ssid" in registry view foo/network/wifi-setup: mocked`)
}

// TODO: refactor these tests to match the new tx handling
//func (s *registrySuite) TestViewSetManyWithExistingState(c *C) {
//	s.st.Lock()
//
//	databag := registry.NewJSONDataBag()
//	err := databag.Set("wifi.ssid", "foo")
//	c.Assert(err, IsNil)
//
//	databags := map[string]map[string]registry.JSONDataBag{
//		"system": {"network": databag},
//	}
//	s.st.Set("registry-databags", databags)
//	s.st.Unlock()
//
//	s.testViewSetMany(c)
//}
//
//func (s *registrySuite) TestViewSetManyWithExistingEmptyState(c *C) {
//	s.st.Lock()
//
//	databags := map[string]map[string]registry.JSONDataBag{
//		"system": {"network": registry.NewJSONDataBag()},
//	}
//	s.st.Set("registry-databags", databags)
//	s.st.Unlock()
//
//	s.testViewSetMany(c)
//}
//
//func (s *registrySuite) TestViewSetMany(c *C) {
//	s.testViewSetMany(c)
//}
//
//func (s *registrySuite) testViewSetMany(c *C) {
//	s.setFeatureFlag(c)
//
//	var calls int
//	restore := daemon.MockRegistrystateSetViaView(func(st *state.State, account, registryName, viewName string, requests map[string]interface{}) error {
//		calls++
//		switch calls {
//		case 1:
//			c.Check(requests, DeepEquals, map[string]interface{}{"ssid": "foo", "password": nil})
//
//			bag := registry.NewJSONDataBag()
//			err := bag.Set("wifi.ssid", "foo")
//			c.Check(err, IsNil)
//			err = bag.Unset("wifi.psk")
//			c.Check(err, IsNil)
//
//			st.Set("registry-databags", map[string]map[string]registry.JSONDataBag{account: {registryName: bag}})
//		default:
//			err := fmt.Errorf("expected 1 call, now on %d", calls)
//			c.Error(err)
//			return err
//		}
//
//		return nil
//	})
//	defer restore()
//
//	buf := bytes.NewBufferString(`{"ssid": "foo", "password": null}`)
//	req, err := http.NewRequest("PUT", "/v2/registry/system/network/wifi-setup", buf)
//	c.Assert(err, IsNil)
//
//	rspe := s.asyncReq(c, req, nil)
//	c.Check(rspe.Status, Equals, 202)
//
//	st := s.d.Overlord().State()
//	st.Lock()
//	defer st.Unlock()
//
//	chg := st.Change(rspe.Change)
//	c.Check(chg.Kind(), Equals, "set-registry-view")
//	c.Check(chg.Summary(), Equals, `Set registry view system/network/wifi-setup`)
//	c.Check(chg.Status(), Equals, state.DoneStatus)
//
//	var databags map[string]map[string]registry.JSONDataBag
//	err = st.Get("registry-databags", &databags)
//	c.Assert(err, IsNil)
//
//	value, err := databags["system"]["network"].Get("wifi.ssid")
//	c.Assert(err, IsNil)
//	c.Assert(value, Equals, "foo")
//
//	value, err = databags["system"]["network"].Get("wifi.psk")
//	c.Assert(err, FitsTypeOf, registry.PathError(""))
//	c.Assert(value, IsNil)
//}
//
//func (s *registrySuite) TestGetViewError(c *C) {
//	s.setFeatureFlag(c)
//
//	type test struct {
//		name string
//		err  error
//		code int
//	}
//
//	for _, t := range []test{
//		{name: "registry not found", err: &registry.NotFoundError{}, code: 404},
//		{name: "internal", err: errors.New("internal"), code: 500},
//	} {
//		restore := daemon.MockRegistrystateGetViaView(func(_ *state.State, _, _, _ string, _ []string) (interface{}, error) {
//			return nil, t.err
//		})
//
//		req, err := http.NewRequest("GET", "/v2/registry/system/network/wifi-setup?fields=ssid", nil)
//		c.Assert(err, IsNil, Commentf("%s test", t.name))
//
//		rspe := s.errorReq(c, req, nil)
//		c.Check(rspe.Status, Equals, t.code, Commentf("%s test", t.name))
//		restore()
//	}
//}
//
//func (s *registrySuite) TestGetViewMisshapenQuery(c *C) {
//	s.setFeatureFlag(c)
//
//	var calls int
//	restore := daemon.MockRegistrystateGetViaView(func(_ *state.State, _, _, _ string, fields []string) (interface{}, error) {
//		calls++
//		switch calls {
//		case 1:
//			c.Check(fields, DeepEquals, []string{"foo.bar", "[1].foo", "foo"})
//			return map[string]interface{}{"a": 1}, nil
//		default:
//			err := fmt.Errorf("expected 1 call, now on %d", calls)
//			c.Error(err)
//			return nil, err
//		}
//	})
//	defer restore()
//
//	req, err := http.NewRequest("GET", "/v2/registry/system/network/wifi-setup?fields=,foo.bar,,[1].foo,foo,", nil)
//	c.Assert(err, IsNil)
//
//	rsp := s.syncReq(c, req, nil)
//	c.Check(rsp.Status, Equals, 200)
//	c.Check(rsp.Result, DeepEquals, map[string]interface{}{"a": 1})
//}
//
//func (s *registrySuite) TestSetView(c *C) {
//	s.setFeatureFlag(c)
//
//	type test struct {
//		name  string
//		value interface{}
//	}
//
//	for _, t := range []test{
//		{name: "string", value: "foo"},
//		{name: "integer", value: float64(123)},
//		{name: "list", value: []interface{}{"foo", "bar"}},
//		{name: "map", value: map[string]interface{}{"foo": "bar"}},
//	} {
//		cmt := Commentf("%s test", t.name)
//		restore := daemon.MockRegistrystateSetViaView(func(st *state.State, acc, registryName, view string, requests map[string]interface{}) error {
//			c.Check(acc, Equals, "system", cmt)
//			c.Check(registryName, Equals, "network", cmt)
//			c.Check(view, Equals, "setup-wifi", cmt)
//			c.Check(requests, DeepEquals, map[string]interface{}{"ssid": t.value}, cmt)
//
//			bag := registry.NewJSONDataBag()
//			err := bag.Set("wifi.ssid", t.value)
//			c.Check(err, IsNil)
//			st.Set("registry-databags", map[string]map[string]registry.JSONDataBag{acc: {registryName: bag}})
//
//			return nil
//		})
//		jsonVal, err := json.Marshal(t.value)
//		c.Check(err, IsNil, cmt)
//
//		buf := bytes.NewBufferString(fmt.Sprintf(`{"ssid": %s}`, jsonVal))
//		req, err := http.NewRequest("PUT", "/v2/registry/system/network/setup-wifi", buf)
//		c.Check(err, IsNil, cmt)
//		req.Header.Set("Content-Type", "application/json")
//
//		rspe := s.asyncReq(c, req, nil)
//		c.Check(rspe.Status, Equals, 202, cmt)
//
//		st := s.d.Overlord().State()
//		st.Lock()
//		chg := st.Change(rspe.Change)
//		st.Unlock()
//
//		c.Check(chg.Kind(), Equals, "set-registry-view", cmt)
//		c.Check(chg.Summary(), Equals, `Set registry view system/network/setup-wifi`, cmt)
//
//		st.Lock()
//		c.Check(chg.Status(), Equals, state.DoneStatus)
//
//		var databags map[string]map[string]registry.JSONDataBag
//		err = st.Get("registry-databags", &databags)
//		st.Unlock()
//		c.Assert(err, IsNil)
//
//		value, err := databags["system"]["network"].Get("wifi.ssid")
//		c.Assert(err, IsNil)
//		c.Assert(value, DeepEquals, t.value)
//
//		restore()
//	}
//}
//
//func (s *registrySuite) TestUnsetView(c *C) {
//	s.setFeatureFlag(c)
//
//	restore := daemon.MockRegistrystateSetViaView(func(_ *state.State, acc, registryName, view string, requests map[string]interface{}) error {
//		c.Check(acc, Equals, "system")
//		c.Check(registryName, Equals, "network")
//		c.Check(view, Equals, "wifi-setup")
//		c.Check(requests, DeepEquals, map[string]interface{}{"ssid": nil})
//		return nil
//	})
//	defer restore()
//
//	buf := bytes.NewBufferString(`{"ssid": null}`)
//	req, err := http.NewRequest("PUT", "/v2/registry/system/network/wifi-setup", buf)
//	c.Assert(err, IsNil)
//	req.Header.Set("Content-Type", "application/json")
//
//	rspe := s.asyncReq(c, req, nil)
//	c.Check(rspe.Status, Equals, 202)
//
//	st := s.d.Overlord().State()
//	st.Lock()
//	chg := st.Change(rspe.Change)
//
//	c.Check(chg.Kind(), Equals, "set-registry-view")
//	c.Check(chg.Summary(), Equals, `Set registry view system/network/wifi-setup`)
//	c.Check(chg.Status(), Equals, state.DoneStatus)
//	st.Unlock()
//}
//
//func (s *registrySuite) TestSetViewError(c *C) {
//	s.setFeatureFlag(c)
//
//	type test struct {
//		name string
//		err  error
//		code int
//	}
//
//	for _, t := range []test{
//		{name: "not found", err: &registry.NotFoundError{}, code: 404},
//		{name: "internal", err: errors.New("internal"), code: 500},
//	} {
//		restore := daemon.MockRegistrystateSetViaView(func(*state.State, string, string, string, map[string]interface{}) error {
//			return t.err
//		})
//		cmt := Commentf("%s test", t.name)
//
//		buf := bytes.NewBufferString(`{"ssid": null}`)
//		req, err := http.NewRequest("PUT", "/v2/registry/system/network/wifi-setup", buf)
//		c.Assert(err, IsNil, cmt)
//		req.Header.Set("Content-Type", "application/json")
//
//		rspe := s.errorReq(c, req, nil)
//		c.Check(rspe.Status, Equals, t.code, cmt)
//		restore()
//	}
//}
//
//func (s *registrySuite) TestSetViewEmptyBody(c *C) {
//	s.setFeatureFlag(c)
//
//	restore := daemon.MockRegistrystateSetViaView(func(*state.State, string, string, string, map[string]interface{}) error {
//		err := errors.New("unexpected call to registrystate.Set")
//		c.Error(err)
//		return err
//	})
//	defer restore()
//
//	req, err := http.NewRequest("PUT", "/v2/registry/system/network/wifi-setup", &bytes.Buffer{})
//	req.Header.Set("Content-Type", "application/json")
//	c.Assert(err, IsNil)
//
//	rspe := s.errorReq(c, req, nil)
//	c.Check(rspe.Status, Equals, 400)
//}
//
//func (s *registrySuite) TestSetViewBadRequest(c *C) {
//	s.setFeatureFlag(c)
//
//	buf := bytes.NewBufferString(`{`)
//	req, err := http.NewRequest("PUT", "/v2/registry/system/network/wifi-setup", buf)
//	c.Assert(err, IsNil)
//
//	rspe := s.errorReq(c, req, nil)
//	c.Check(rspe.Status, Equals, 400)
//	c.Check(rspe.Message, Equals, "cannot decode registry request body: unexpected EOF")
//}
//
//func (s *registrySuite) TestGetBadRequest(c *C) {
//	s.setFeatureFlag(c)
//
//	restore := daemon.MockRegistrystateGetViaView(func(_ *state.State, acc, registryName, view string, fields []string) (interface{}, error) {
//		return nil, &registry.BadRequestError{
//			Account:      "acc",
//			RegistryName: "reg",
//			View:         "foo",
//			Operation:    "get",
//			Request:      "foo",
//			Cause:        "bad request",
//		}
//	})
//	defer restore()
//
//	req, err := http.NewRequest("GET", "/v2/registry/acc/reg/foo?fields=foo", &bytes.Buffer{})
//	c.Assert(err, IsNil)
//
//	rspe := s.errorReq(c, req, nil)
//	c.Check(rspe.Status, Equals, 400)
//	c.Check(rspe.Message, Equals, `cannot get "foo" in registry view acc/reg/foo: bad request`)
//	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
//}
//
//func (s *registrySuite) TestSetBadRequest(c *C) {
//	s.setFeatureFlag(c)
//
//	restore := daemon.MockRegistrystateSetViaView(func(*state.State, string, string, string, map[string]interface{}) error {
//		return &registry.BadRequestError{
//			Account:      "acc",
//			RegistryName: "reg",
//			View:         "foo",
//			Operation:    "set",
//			Request:      "foo",
//			Cause:        "bad request",
//		}
//	})
//	defer restore()
//
//	buf := bytes.NewBufferString(`{"a.b.c": "foo"}`)
//	req, err := http.NewRequest("PUT", "/v2/registry/acc/reg/foo", buf)
//	req.Header.Set("Content-Type", "application/json")
//	c.Assert(err, IsNil)
//
//	rspe := s.errorReq(c, req, nil)
//	c.Check(rspe.Status, Equals, 400)
//	c.Check(rspe.Message, Equals, `cannot set "foo" in registry view acc/reg/foo: bad request`)
//	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
//}
//
//func (s *registrySuite) TestSetFailUnsetFeatureFlag(c *C) {
//	restore := daemon.MockRegistrystateSetViaView(func(*state.State, string, string, string, map[string]interface{}) error {
//		err := fmt.Errorf("unexpected call to registrystate")
//		c.Error(err)
//		return err
//	})
//	defer restore()
//
//	buf := bytes.NewBufferString(`{"a.b.c": "foo"}`)
//	req, err := http.NewRequest("PUT", "/v2/registry/acc/reg/foo", buf)
//	req.Header.Set("Content-Type", "application/json")
//	c.Assert(err, IsNil)
//
//	rspe := s.errorReq(c, req, nil)
//	c.Check(rspe.Status, Equals, 400)
//	c.Check(rspe.Message, Equals, `"registries" feature flag is disabled: set 'experimental.registries' to true`)
//	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
//}
//
//func (s *registrySuite) TestGetFailUnsetFeatureFlag(c *C) {
//	restore := daemon.MockRegistrystateSetViaView(func(*state.State, string, string, string, map[string]interface{}) error {
//		err := fmt.Errorf("unexpected call to registrystate")
//		c.Error(err)
//		return err
//	})
//	defer restore()
//
//	req, err := http.NewRequest("GET", "/v2/registry/acc/reg/foo?fields=my-field", nil)
//	c.Assert(err, IsNil)
//
//	rspe := s.errorReq(c, req, nil)
//	c.Check(rspe.Status, Equals, 400)
//	c.Check(rspe.Message, Equals, `"registries" feature flag is disabled: set 'experimental.registries' to true`)
//	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
//}
//
//func (s *registrySuite) TestGetNoFields(c *C) {
//	s.setFeatureFlag(c)
//
//	value := map[string]interface{}{"foo": 1, "bar": "baz", "nested": map[string]interface{}{"a": []interface{}{1, 2}}}
//	restore := daemon.MockRegistrystateGetViaView(func(_ *state.State, _, _, _ string, fields []string) (interface{}, error) {
//		c.Check(fields, IsNil)
//		return value, nil
//	})
//	defer restore()
//
//	req, err := http.NewRequest("GET", "/v2/registry/acc/reg/foo", nil)
//	c.Assert(err, IsNil)
//
//	rspe := s.syncReq(c, req, nil)
//	c.Check(rspe.Status, Equals, 200)
//	c.Check(rspe.Result, DeepEquals, value)
//}
