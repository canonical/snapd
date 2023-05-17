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

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/client"
	"github.com/snapcore/snapd/daemon"
	"github.com/snapcore/snapd/overlord/state"
)

type aspectsSuite struct {
	apiBaseSuite
}

var _ = Suite(&aspectsSuite{})

func (s *aspectsSuite) SetUpTest(c *C) {
	s.apiBaseSuite.SetUpTest(c)

	s.expectReadAccess(daemon.OpenAccess{})
	s.expectWriteAccess(daemon.AuthenticatedAccess{Polkit: "io.snapcraft.snapd.manage"})
}

func (s *aspectsSuite) TestGetAspect(c *C) {
	s.daemon(c)

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
		restore := daemon.MockAspectstateGet(func(_ *state.State, acc, bundleName, aspect, field string, value interface{}) error {
			c.Check(acc, Equals, "system", cmt)
			c.Check(bundleName, Equals, "network", cmt)
			c.Check(aspect, Equals, "wifi-setup", cmt)
			c.Check(field, Equals, "ssid", cmt)

			outputValue := reflect.ValueOf(value).Elem()
			outputValue.Set(reflect.ValueOf(t.value))
			return nil
		})

		buf := bytes.NewBufferString(`{"account": "system", "bundle": "network", "aspect": "wifi-setup", "field": "ssid"}`)
		req, err := http.NewRequest("GET", "/v2/aspects", buf)
		req.Header.Set("Content-Type", "application/json")
		c.Assert(err, IsNil, cmt)

		rspe := s.syncReq(c, req, nil)
		c.Check(rspe.Status, Equals, 200, cmt)
		c.Check(rspe.Result, DeepEquals, t.value, cmt)

		restore()
	}
}

func (s *aspectsSuite) TestGetAspectError(c *C) {
	s.daemon(c)

	type test struct {
		name string
		err  error
		code int
	}

	for _, t := range []test{
		{name: "aspect not found", err: &aspects.AspectNotFoundError{}, code: 404},
		{name: "field not found", err: &aspects.FieldNotFoundError{}, code: 404},
		{name: "internal", err: errors.New("internal"), code: 500},
	} {
		restore := daemon.MockAspectstateGet(func(_ *state.State, acc, bundleName, aspect, field string, value interface{}) error {
			return t.err
		})

		buf := bytes.NewBufferString(`{"account": "system", "bundle": "network", "aspect": "wifi-setup", "field": "ssid"}`)
		req, err := http.NewRequest("GET", "/v2/aspects", buf)
		req.Header.Set("Content-Type", "application/json")
		c.Assert(err, IsNil, Commentf("%s test", t.name))

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, t.code, Commentf("%s test", t.name))
		restore()
	}
}

func (s *aspectsSuite) TestGetAspectMissingField(c *C) {
	s.daemon(c)

	type test struct {
		acc    string
		bundle string
		aspect string
		field  string
	}

	for _, t := range []test{
		{bundle: "a", aspect: "b", field: "c"},
		{acc: "a", aspect: "b", field: "c"},
		{acc: "a", bundle: "b", field: "c"},
		{acc: "a", bundle: "b", aspect: "c"},
	} {
		buf := bytes.NewBufferString(fmt.Sprintf(`{"account": %q, "bundle": %q, "aspect": %q, "field": %q}`, t.acc, t.bundle, t.aspect, t.field))
		req, err := http.NewRequest("GET", "/v2/aspects", buf)
		req.Header.Set("Content-Type", "application/json")
		c.Assert(err, IsNil)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, 400)
	}
}

func (s *aspectsSuite) TestGetAspectMalformedRequest(c *C) {
	s.daemon(c)

	buf := bytes.NewBufferString(`{`)
	req, err := http.NewRequest("GET", "/v2/aspects", buf)
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, "cannot decode aspect request body: unexpected EOF")
}

func (s *aspectsSuite) TestSetAspect(c *C) {
	s.daemon(c)

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
		restore := daemon.MockAspectstateSet(func(_ *state.State, acc, bundleName, aspect, field string, value interface{}) error {
			c.Check(acc, Equals, "system", cmt)
			c.Check(bundleName, Equals, "network", cmt)
			c.Check(aspect, Equals, "wifi-setup", cmt)
			c.Check(field, Equals, "ssid", cmt)
			c.Check(value, DeepEquals, t.value, cmt)
			return nil
		})
		jsonVal, err := json.Marshal(t.value)
		c.Check(err, IsNil, cmt)

		buf := bytes.NewBufferString(fmt.Sprintf(`{"account": "system", "bundle": "network", "aspect": "wifi-setup", "field": "ssid", "value": %s}`, jsonVal))
		req, err := http.NewRequest("POST", "/v2/aspects", buf)
		req.Header.Set("Content-Type", "application/json")
		c.Check(err, IsNil, cmt)

		rspe := s.syncReq(c, req, nil)
		c.Check(rspe.Status, Equals, 200, cmt)
		restore()
	}
}

func (s *aspectsSuite) TestUnsetAspect(c *C) {
	s.daemon(c)

	restore := daemon.MockAspectstateSet(func(_ *state.State, acc, bundleName, aspect, field string, value interface{}) error {
		c.Check(acc, Equals, "system")
		c.Check(bundleName, Equals, "network")
		c.Check(aspect, Equals, "wifi-setup")
		c.Check(field, Equals, "ssid")
		c.Check(value, IsNil)
		return nil
	})
	defer restore()

	buf := bytes.NewBufferString(`{"account": "system", "bundle": "network", "aspect": "wifi-setup", "field": "ssid"}`)
	req, err := http.NewRequest("POST", "/v2/aspects", buf)
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)

	rspe := s.syncReq(c, req, nil)
	c.Check(rspe.Status, Equals, 200)
}

func (s *aspectsSuite) TestSetAspectError(c *C) {
	s.daemon(c)

	type test struct {
		name string
		err  error
		code int
	}

	for _, t := range []test{
		{name: "aspect not found", err: &aspects.AspectNotFoundError{}, code: 404},
		{name: "field not found", err: &aspects.FieldNotFoundError{}, code: 404},
		{name: "internal", err: errors.New("internal"), code: 500},
	} {
		restore := daemon.MockAspectstateSet(func(_ *state.State, acc, bundleName, aspect, field string, val interface{}) error {
			return t.err
		})

		buf := bytes.NewBufferString(`{"account": "system", "bundle": "network", "aspect": "wifi-setup", "field": "ssid", "value": "foo"}`)
		req, err := http.NewRequest("POST", "/v2/aspects", buf)
		req.Header.Set("Content-Type", "application/json")
		c.Assert(err, IsNil, Commentf("%s test", t.name))

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, t.code, Commentf("%s test", t.name))
		restore()
	}
}

func (s *aspectsSuite) TestSetAspectMissingRequest(c *C) {
	s.daemon(c)

	type test struct {
		acc    string
		bundle string
		aspect string
		field  string
	}

	for _, t := range []test{
		{bundle: "a", aspect: "b", field: "c"},
		{acc: "a", aspect: "b", field: "c"},
		{acc: "a", bundle: "b", field: "c"},
		{acc: "a", bundle: "b", aspect: "c"},
	} {
		buf := bytes.NewBufferString(fmt.Sprintf(`{"account": %q, "bundle": %q, "aspect": %q, "field": %q, "value": "foo"}`, t.acc, t.bundle, t.aspect, t.field))
		req, err := http.NewRequest("POST", "/v2/aspects", buf)
		req.Header.Set("Content-Type", "application/json")
		c.Assert(err, IsNil)

		rspe := s.errorReq(c, req, nil)
		c.Check(rspe.Status, Equals, 400)
	}
}

func (s *aspectsSuite) TestSetAspectBadRequest(c *C) {
	s.daemon(c)

	buf := bytes.NewBufferString(`{`)
	req, err := http.NewRequest("POST", "/v2/aspects", buf)
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 400)
	c.Check(rspe.Message, Equals, "cannot decode aspect request body: unexpected EOF")
}

func (s *aspectsSuite) TestSetAspectNotAllowed(c *C) {
	s.daemon(c)

	restore := daemon.MockAspectstateSet(func(_ *state.State, acc, bundleName, aspect, field string, val interface{}) error {
		return &aspects.InvalidAccessError{RequestedAccess: 2, FieldAccess: 1, Field: "foo"}
	})
	defer restore()

	buf := bytes.NewBufferString(`{"account": "system", "bundle": "network", "aspect": "wifi-setup", "field": "foo", "value": "bar"}`)
	req, err := http.NewRequest("POST", "/v2/aspects", buf)
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 403)
	c.Check(rspe.Message, Equals, `cannot write field "foo": only supports read access`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}

func (s *aspectsSuite) TestGetAspectNotAllowed(c *C) {
	s.daemon(c)

	restore := daemon.MockAspectstateGet(func(_ *state.State, acc, bundleName, aspect, field string, val interface{}) error {
		return &aspects.InvalidAccessError{RequestedAccess: 1, FieldAccess: 2, Field: "foo"}
	})
	defer restore()

	buf := bytes.NewBufferString(`{"account": "system", "bundle": "network", "aspect": "wifi-setup", "field": "foo"}`)
	req, err := http.NewRequest("GET", "/v2/aspects", buf)
	req.Header.Set("Content-Type", "application/json")
	c.Assert(err, IsNil)

	rspe := s.errorReq(c, req, nil)
	c.Check(rspe.Status, Equals, 403)
	c.Check(rspe.Message, Equals, `cannot read field "foo": only supports write access`)
	c.Check(rspe.Kind, Equals, client.ErrorKind(""))
}
