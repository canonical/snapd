// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022 Canonical Ltd
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

package aspects_test

import (
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/testutil"
)

type aspectSuite struct{}

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&aspectSuite{})

func (*aspectSuite) TestNewAspectDirectory(c *C) {
	_, err := aspects.NewAspectDirectory("foo", nil, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, ErrorMatches, `cannot create aspects directory: no aspects`)

	_, err = aspects.NewAspectDirectory("foo", map[string]interface{}{
		"bar": "baz",
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, ErrorMatches, `cannot create aspect: access patterns should be a list of maps`)

	_, err = aspects.NewAspectDirectory("foo", map[string]interface{}{
		"bar": []map[string]string{},
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, ErrorMatches, `cannot create aspect without access patterns`)

	_, err = aspects.NewAspectDirectory("foo", map[string]interface{}{
		"bar": []map[string]string{
			{"path": "foo"},
		},
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, ErrorMatches, `cannot create aspect pattern without a "name" field`)

	_, err = aspects.NewAspectDirectory("foo", map[string]interface{}{
		"bar": []map[string]string{
			{"name": "foo"},
		},
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, ErrorMatches, `cannot create aspect pattern without a "path" field`)

	aspectDir, err := aspects.NewAspectDirectory("foo", map[string]interface{}{
		"bar": []map[string]string{
			{"name": "a", "path": "b"},
		},
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, IsNil)
	c.Check(aspectDir, Not(IsNil))
}

func (s *aspectSuite) TestAccessTypes(c *C) {
	type testcase struct {
		access string
		err    bool
	}

	for _, t := range []testcase{
		{
			access: "read",
		},
		{
			access: "write",
		},
		{
			access: "read-write",
		},
		{
			access: "",
		},
		{
			access: "invalid",
			err:    true,
		},
	} {
		aspectDir, err := aspects.NewAspectDirectory("foo", map[string]interface{}{
			"bar": []map[string]string{
				{"name": "a", "path": "b", "access": t.access},
			}}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())

		cmt := Commentf("\"%s access\" sub-test failed", t.access)
		if t.err {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`.*expected 'access' to be one of "read-write", "read", "write" but was %q`, t.access), cmt)
			c.Check(aspectDir, IsNil, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Check(aspectDir, Not(IsNil), cmt)
		}
	}
}

func (*aspectSuite) TestGetAndSetAspects(c *C) {
	aspectDir, err := aspects.NewAspectDirectory("system/network", map[string]interface{}{
		"wifi-setup": []map[string]string{
			{"name": "ssids", "path": "wifi.ssids"},
			{"name": "ssid", "path": "wifi.ssid"},
			{"name": "top-level", "path": "top-level"},
			{"name": "dotted.name", "path": "dotted"},
		},
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	wsAspect := aspectDir.Aspect("wifi-setup")

	// nested string value
	err = wsAspect.Set("ssid", "my-ssid")
	c.Assert(err, IsNil)

	var ssid string
	err = wsAspect.Get("ssid", &ssid)
	c.Assert(err, IsNil)
	c.Check(ssid, Equals, "my-ssid")

	// nested list value
	err = wsAspect.Set("ssids", []string{"one", "two"})
	c.Assert(err, IsNil)

	var ssids []string
	err = wsAspect.Get("ssids", &ssids)
	c.Assert(err, IsNil)
	c.Check(ssids, DeepEquals, []string{"one", "two"})

	// top-level string
	var topLevel string
	err = wsAspect.Set("top-level", "randomValue")
	c.Assert(err, IsNil)

	err = wsAspect.Get("top-level", &topLevel)
	c.Assert(err, IsNil)
	c.Check(topLevel, Equals, "randomValue")

	// dotted names are permitted
	err = wsAspect.Set("dotted.name", 3)
	c.Assert(err, IsNil)

	var num int
	err = wsAspect.Get("dotted.name", &num)
	c.Assert(err, IsNil)
	c.Check(num, Equals, 3)
}

func (s *aspectSuite) TestAspectNotFoundError(c *C) {
	aspectDir, err := aspects.NewAspectDirectory("foo", map[string]interface{}{
		"bar": []map[string]string{
			{"name": "top-level", "path": "top-level"},
			{"name": "nested", "path": "top.nested-one"},
			{"name": "other-nested", "path": "top.nested-two"},
		},
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectDir.Aspect("bar")

	var value string
	err = aspect.Get("missing", &value)
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "missing": name not found`)

	err = aspect.Set("missing", "thing")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot set "missing": name not found`)

	err = aspect.Get("top-level", &value)
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "top-level": value of key path "top-level" not found`)

	err = aspect.Set("nested", "thing")
	c.Assert(err, IsNil)

	err = aspect.Get("other-nested", &value)
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "other-nested": value of key path "top.nested-two" not found`)
}

func (s *aspectSuite) TestAspectBadRead(c *C) {
	aspectDir, err := aspects.NewAspectDirectory("foo", map[string]interface{}{
		"bar": []map[string]string{
			{"name": "one", "path": "one"},
			{"name": "onetwo", "path": "one.two"},
		},
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectDir.Aspect("bar")
	err = aspect.Set("one", "foo")
	c.Assert(err, IsNil)

	var value string
	err = aspect.Get("onetwo", &value)
	c.Assert(err, ErrorMatches, `cannot read path prefix "one": prefix maps to string`)

	var listVal []string
	err = aspect.Get("one", &listVal)
	c.Assert(err, ErrorMatches, `cannot read value of "one" into \*\[\]string: maps to string`)
}

func (s *aspectSuite) TestAspectsAccessControl(c *C) {
	aspectDir, err := aspects.NewAspectDirectory("dir", map[string]interface{}{
		"foo": []map[string]string{
			{"name": "default", "path": "path.default"},
			{"name": "read-write", "path": "path.read-write", "access": "read-write"},
			{"name": "read-only", "path": "path.read-only", "access": "read"},
			{"name": "write-only", "path": "path.write-only", "access": "write"},
		},
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectDir.Aspect("foo")

	for _, t := range []struct {
		name   string
		getErr string
		setErr string
	}{
		{
			name: "read-write",
		},
		{
			// defaults to "read-write"
			name: "default",
		},
		{
			name: "read-only",
			// unrelated error
			getErr: `cannot get "read-only": value of key path "path.read-only" not found`,
			setErr: `cannot set "read-only": path is not writeable`,
		},
		{
			name:   "write-only",
			getErr: `cannot get "write-only": path is not readable`,
		},
	} {
		cmt := Commentf("sub-test %q failed", t.name)
		err := aspect.Set(t.name, "thing")
		if t.setErr != "" {
			c.Assert(err.Error(), Equals, t.setErr, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
		}

		var value string
		err = aspect.Get(t.name, &value)
		if t.getErr != "" {
			c.Assert(err.Error(), Equals, t.getErr, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
		}
	}
}

type witnessDataBag struct {
	bag              aspects.DataBag
	getPath, setPath string
}

func newSpyDataBag(bag aspects.DataBag) *witnessDataBag {
	return &witnessDataBag{bag: bag}
}

func (s *witnessDataBag) Get(path string, value interface{}) error {
	s.getPath = path
	return s.bag.Get(path, value)
}

func (s *witnessDataBag) Set(path string, value interface{}) error {
	s.setPath = path
	return s.bag.Set(path, value)
}

func (s *witnessDataBag) Data() ([]byte, error) {
	return s.bag.Data()
}

// getLastPaths returns the last paths passed into Get and Set and resets them.
func (s *witnessDataBag) getLastPaths() (get, set string) {
	get, set = s.getPath, s.setPath
	s.getPath, s.setPath = "", ""
	return get, set
}

func (s *aspectSuite) TestAspectAssertionWithPlaceholder(c *C) {
	bag := newSpyDataBag(aspects.NewJSONDataBag())

	aspectDir, err := aspects.NewAspectDirectory("dir", map[string]interface{}{
		"foo": []map[string]string{
			{"name": "defaults.{foo}", "path": "first.{foo}.last"},
			{"name": "{bar}.name", "path": "first.{bar}"},
			{"name": "first.{baz}.last", "path": "{baz}.last"},
			{"name": "first.{foo}.{bar}", "path": "{foo}.mid.{bar}"},
			{"name": "{foo}.mid2.{bar}", "path": "{bar}.mid2.{foo}"},
			{"name": "multi.{foo}", "path": "{foo}.multi.{foo}"},
		},
	}, bag, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectDir.Aspect("foo")

	for _, t := range []struct {
		testName string
		name     string
		path     string
	}{
		{
			testName: "placeholder last to mid",
			name:     "defaults.abc",
			path:     "first.abc.last",
		},
		{
			testName: "placeholder first to last",
			name:     "foo.name",
			path:     "first.foo",
		},
		{
			testName: "placeholder mid to first",
			name:     "first.foo.last",
			path:     "foo.last",
		},
		{
			testName: "two placeholders in order",
			name:     "first.one.two",
			path:     "one.mid.two",
		},
		{
			testName: "two placeholders out of order",
			name:     "first2.mid2.two2",
			path:     "two2.mid2.first2",
		},
		{
			testName: "one placeholder mapping to several",
			name:     "multi.firstLast",
			path:     "firstLast.multi.firstLast",
		},
	} {
		cmt := Commentf("sub-test %q failed", t.testName)
		err := aspect.Set(t.name, "expectedValue")
		c.Assert(err, IsNil, cmt)

		var obtainedValue string
		err = aspect.Get(t.name, &obtainedValue)
		c.Assert(err, IsNil, cmt)

		c.Assert(obtainedValue, Equals, "expectedValue", cmt)

		getPath, setPath := bag.getLastPaths()
		c.Assert(getPath, Equals, t.path, cmt)
		c.Assert(setPath, Equals, t.path, cmt)
	}
}

func (s *aspectSuite) TestAspectBadPlaceholderAssertion(c *C) {
	aspectDir, err := aspects.NewAspectDirectory("dir", map[string]interface{}{
		"foo": []map[string]string{
			{"name": "bad.{foo}", "path": "bad.{bar}"},
		},
	}, aspects.NewJSONDataBag(), aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectDir.Aspect("foo")

	err = aspect.Set("bad.foo", "bar")
	c.Assert(err, ErrorMatches, "cannot find path placeholder \"bar\" in the aspect name")
}
