// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2022-2024 Canonical Ltd
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

package registry_test

import (
	"errors"
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/registry"
	"github.com/snapcore/snapd/testutil"
)

type viewSuite struct{}

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&viewSuite{})

func (*viewSuite) TestNewRegistry(c *C) {
	type testcase struct {
		registry map[string]interface{}
		err      string
	}

	tcs := []testcase{
		{
			err: `cannot define registry: no views`,
		},
		{
			registry: map[string]interface{}{"0-a": map[string]interface{}{}},
			err:      `cannot define view "0-a": name must conform to [a-z](?:-?[a-z0-9])*`,
		},
		{
			registry: map[string]interface{}{"bar": "baz"},
			err:      `cannot define view "bar": view must be non-empty map`,
		},
		{
			registry: map[string]interface{}{"bar": map[string]interface{}{}},
			err:      `cannot define view "bar": view must be non-empty map`,
		},
		{
			registry: map[string]interface{}{"bar": map[string]interface{}{"rules": "bar"}},
			err:      `cannot define view "bar": view rules must be non-empty list`,
		},
		{
			registry: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{}}},
			err:      `cannot define view "bar": view rules must be non-empty list`,
		},
		{
			registry: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{"a"}}},
			err:      `cannot define view "bar": each view rule should be a map`,
		},
		{
			registry: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{}}}},
			err:      `cannot define view "bar": view rules must have a "storage" field`,
		},

		{
			registry: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"request": "foo", "storage": 1}}}},
			err:      `cannot define view "bar": "storage" must be a string`,
		},
		{
			registry: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"storage": "foo", "request": 1}}}},
			err:      `cannot define view "bar": "request" must be a string`,
		},
		{
			registry: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"storage": "foo", "request": ""}}}},
			err:      `cannot define view "bar": view rules' "request" field must be non-empty, if it exists`,
		},
		{
			registry: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"request": "foo", "storage": 1}}}},
			err:      `cannot define view "bar": "storage" must be a string`,
		},
		{
			registry: map[string]interface{}{
				"bar": map[string]interface{}{
					"rules": []interface{}{
						map[string]interface{}{"request": "a", "storage": "b"},
						map[string]interface{}{"request": "a", "storage": "c"},
					},
				},
			},
			err: `cannot define view "bar": cannot have several reading rules with the same "request" field`,
		},
		{
			registry: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"request": "foo", "storage": "bar", "access": 1}}}},
			err:      `cannot define view "bar": "access" must be a string`,
		},
		{
			registry: map[string]interface{}{
				"bar": map[string]interface{}{
					"rules": []interface{}{
						map[string]interface{}{"request": "a", "storage": "c", "access": "write"},
						map[string]interface{}{"request": "a", "storage": "b"},
					},
				},
			},
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("test number %d", i+1)
		registry, err := registry.New("acc", "foo", tc.registry, registry.NewJSONSchema())
		if tc.err != "" {
			c.Assert(err, NotNil)
			c.Assert(err.Error(), Equals, tc.err, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Check(registry, Not(IsNil), cmt)
		}
	}
}

func (s *viewSuite) TestMissingRequestDefaultsToStorage(c *C) {
	databag := registry.NewJSONDataBag()
	views := map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"storage": "a.b"},
			},
		},
	}
	reg, err := registry.New("acc", "foo", views, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := reg.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "a.b", "value")
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{
		"a": map[string]interface{}{
			"b": "value",
		},
	})
}

func (s *viewSuite) TestBundleWithSample(c *C) {
	views := map[string]interface{}{
		"wifi-setup": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "ssids", "storage": "wifi.ssids"},
				map[string]interface{}{"access": "read-write", "request": "ssid", "storage": "wifi.ssid"},
				map[string]interface{}{"access": "write", "request": "password", "storage": "wifi.psk"},
				map[string]interface{}{"access": "read", "request": "status", "storage": "wifi.status"},
				map[string]interface{}{"request": "private.{key}", "storage": "wifi.{key}"},
			},
		},
	}
	_, err := registry.New("acc", "foo", views, registry.NewJSONSchema())
	c.Assert(err, IsNil)
}

func (s *viewSuite) TestAccessTypes(c *C) {
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
		registry, err := registry.New("acc", "foo", map[string]interface{}{
			"bar": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "a", "storage": "b", "access": t.access},
				},
			},
		}, registry.NewJSONSchema())

		cmt := Commentf("\"%s access\" sub-test failed", t.access)
		if t.err {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`.*expected 'access' to be either "read-write", "read", "write" or empty but was %q`, t.access), cmt)
			c.Check(registry, IsNil, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Check(registry, Not(IsNil), cmt)
		}
	}
}

func (*viewSuite) TestGetAndSetViews(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("system", "network", map[string]interface{}{
		"wifi-setup": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "ssids", "storage": "wifi.ssids"},
				map[string]interface{}{"request": "ssid", "storage": "wifi.ssid"},
				map[string]interface{}{"request": "top-level", "storage": "top-level"},
				map[string]interface{}{"request": "dotted.path", "storage": "dotted"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	wsView := registry.View("wifi-setup")

	// nested string value
	err = wsView.Set(databag, "ssid", "my-ssid")
	c.Assert(err, IsNil)

	ssid, err := wsView.Get(databag, "ssid")
	c.Assert(err, IsNil)
	c.Check(ssid, DeepEquals, "my-ssid")

	// nested list value
	err = wsView.Set(databag, "ssids", []string{"one", "two"})
	c.Assert(err, IsNil)

	ssids, err := wsView.Get(databag, "ssids")
	c.Assert(err, IsNil)
	c.Check(ssids, DeepEquals, []interface{}{"one", "two"})

	// top-level string
	err = wsView.Set(databag, "top-level", "randomValue")
	c.Assert(err, IsNil)

	topLevel, err := wsView.Get(databag, "top-level")
	c.Assert(err, IsNil)
	c.Check(topLevel, DeepEquals, "randomValue")

	// dotted request paths are permitted
	err = wsView.Set(databag, "dotted.path", 3)
	c.Assert(err, IsNil)

	num, err := wsView.Get(databag, "dotted.path")
	c.Assert(err, IsNil)
	c.Check(num, DeepEquals, float64(3))
}

func (*viewSuite) TestSetWithNilValueFail(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("system", "test", map[string]interface{}{
		"test": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	wsView := registry.View("test")

	err = wsView.Set(databag, "foo", "value")
	c.Assert(err, IsNil)

	err = wsView.Set(databag, "foo", nil)
	c.Assert(err, ErrorMatches, `internal error: Set value cannot be nil`)

	ssid, err := wsView.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Check(ssid, DeepEquals, "value")
}

func (s *viewSuite) TestRegistryNotFound(c *C) {
	databag := registry.NewJSONDataBag()
	reg, err := registry.New("acc", "foo", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "top-level", "storage": "top-level"},
				map[string]interface{}{"request": "nested", "storage": "top.nested-one"},
				map[string]interface{}{"request": "other-nested", "storage": "top.nested-two"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := reg.View("bar")

	_, err = view.Get(databag, "missing")
	c.Assert(err, testutil.ErrorIs, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "missing" in registry view acc/foo/bar: no matching read rule`)

	err = view.Set(databag, "missing", "thing")
	c.Assert(err, testutil.ErrorIs, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot set "missing" in registry view acc/foo/bar: no matching write rule`)

	err = view.Unset(databag, "missing")
	c.Assert(err, testutil.ErrorIs, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot unset "missing" in registry view acc/foo/bar: no matching write rule`)

	_, err = view.Get(databag, "top-level")
	c.Assert(err, testutil.ErrorIs, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "top-level" in registry view acc/foo/bar: matching rules don't map to any values`)

	_, err = view.Get(databag, "")
	c.Assert(err, testutil.ErrorIs, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get registry view acc/foo/bar: matching rules don't map to any values`)

	err = view.Set(databag, "nested", "thing")
	c.Assert(err, IsNil)

	err = view.Unset(databag, "nested")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "other-nested")
	c.Assert(err, testutil.ErrorIs, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "other-nested" in registry view acc/foo/bar: matching rules don't map to any values`)
}

func (s *viewSuite) TestViewBadRead(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "foo", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "one", "storage": "one"},
				map[string]interface{}{"request": "onetwo", "storage": "one.two"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("bar")
	err = view.Set(databag, "one", "foo")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "onetwo")
	c.Assert(err, ErrorMatches, `cannot read path prefix "one": prefix maps to string`)
}

func (s *viewSuite) TestViewAccessControl(c *C) {
	for _, t := range []struct {
		access string
		getErr string
		setErr string
	}{
		{
			access: "read-write",
		},
		{
			// defaults to "read-write"
			access: "",
		},
		{
			access: "read",
			// non-access control error, access ok
			getErr: `cannot get "foo" in registry view acc/registry/foo: matching rules don't map to any values`,
			setErr: `cannot set "foo" in registry view acc/registry/foo: no matching write rule`,
		},
		{
			access: "write",
			getErr: `cannot get "foo" in registry view acc/registry/foo: no matching read rule`,
		},
	} {
		cmt := Commentf("sub-test with %q access failed", t.access)
		databag := registry.NewJSONDataBag()
		reg, err := registry.New("acc", "registry", map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "foo", "storage": "foo", "access": t.access},
				},
			},
		}, registry.NewJSONSchema())
		c.Assert(err, IsNil)

		view := reg.View("foo")

		err = view.Set(databag, "foo", "thing")
		if t.setErr != "" {
			c.Assert(err.Error(), Equals, t.setErr, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
		}

		_, err = view.Get(databag, "foo")
		if t.getErr != "" {
			c.Assert(err.Error(), Equals, t.getErr, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
		}
	}
}

type witnessDataBag struct {
	bag              registry.DataBag
	getPath, setPath string
}

func newWitnessDataBag(bag registry.DataBag) *witnessDataBag {
	return &witnessDataBag{bag: bag}
}

func (s *witnessDataBag) Get(path string) (interface{}, error) {
	s.getPath = path
	return s.bag.Get(path)
}

func (s *witnessDataBag) Set(path string, value interface{}) error {
	s.setPath = path
	return s.bag.Set(path, value)
}

func (s *witnessDataBag) Unset(path string) error {
	s.setPath = path
	return s.bag.Unset(path)
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

func (s *viewSuite) TestViewAssertionWithPlaceholder(c *C) {
	for _, t := range []struct {
		rule     map[string]interface{}
		testName string
		request  string
		storage  string
	}{
		{
			testName: "placeholder last to mid",
			rule:     map[string]interface{}{"request": "defaults.{foo}", "storage": "first.{foo}.last"},
			request:  "defaults.abc",
			storage:  "first.abc.last",
		},
		{
			testName: "placeholder first to last",
			rule:     map[string]interface{}{"request": "{bar}.name", "storage": "first.{bar}"},
			request:  "foo.name",
			storage:  "first.foo",
		},
		{
			testName: "placeholder mid to first",
			rule:     map[string]interface{}{"request": "first.{baz}.last", "storage": "{baz}.last"},
			request:  "first.foo.last",
			storage:  "foo.last",
		},
		{
			testName: "two placeholders in order",
			rule:     map[string]interface{}{"request": "first.{foo}.{bar}", "storage": "{foo}.mid.{bar}"},
			request:  "first.one.two",
			storage:  "one.mid.two",
		},
		{
			testName: "two placeholders out of order",
			rule:     map[string]interface{}{"request": "{foo}.mid1.{bar}", "storage": "{bar}.mid2.{foo}"},
			request:  "first2.mid1.two2",
			storage:  "two2.mid2.first2",
		},
		{
			testName: "one placeholder mapping to several",
			rule:     map[string]interface{}{"request": "multi.{foo}", "storage": "{foo}.multi.{foo}"},
			request:  "multi.firstlast",
			storage:  "firstlast.multi.firstlast",
		},
	} {
		cmt := Commentf("sub-test %q failed", t.testName)

		reg, err := registry.New("acc", "reg", map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{t.rule},
			},
		}, registry.NewJSONSchema())
		c.Assert(err, IsNil)
		view := reg.View("foo")

		databag := newWitnessDataBag(registry.NewJSONDataBag())
		err = view.Set(databag, t.request, "expectedValue")
		c.Assert(err, IsNil, cmt)

		value, err := view.Get(databag, t.request)
		c.Assert(err, IsNil, cmt)
		c.Assert(value, DeepEquals, "expectedValue", cmt)

		getPath, setPath := databag.getLastPaths()
		c.Assert(getPath, Equals, t.storage, cmt)
		c.Assert(setPath, Equals, t.storage, cmt)
	}
}

func (s *viewSuite) TestViewRequestAndStorageValidation(c *C) {
	type testcase struct {
		testName string
		request  string
		storage  string
		err      string
	}

	for _, tc := range []testcase{
		{
			testName: "empty subkeys in request",
			request:  "a..b", storage: "a.b", err: `invalid request "a..b": cannot have empty subkeys`,
		},
		{
			testName: "empty subkeys in path",
			request:  "a.b", storage: "c..b", err: `invalid storage "c..b": cannot have empty subkeys`,
		},
		{
			testName: "placeholder mismatch (same number)",
			request:  "bad.{foo}", storage: "bad.{bar}", err: `placeholder "{foo}" from request "bad.{foo}" is absent from storage "bad.{bar}"`,
		},
		{
			testName: "placeholder mismatch (different number)",
			request:  "{foo}", storage: "{foo}.bad.{bar}", err: `request "{foo}" and storage "{foo}.bad.{bar}" have mismatched placeholders`,
		},
		{
			testName: "invalid character in request: $",
			request:  "a.b$", storage: "bad", err: `invalid request "a.b$": invalid subkey "b$"`,
		},
		{
			testName: "invalid character in storage path: é",
			request:  "a.b", storage: "a.é", err: `invalid storage "a.é": invalid subkey "é"`,
		},
		{
			testName: "invalid character in request: _",
			request:  "a.b_c", storage: "a.b-c", err: `invalid request "a.b_c": invalid subkey "b_c"`,
		},
		{
			testName: "invalid leading dash",
			request:  "-a", storage: "a", err: `invalid request "-a": invalid subkey "-a"`,
		},
		{
			testName: "invalid trailing dash",
			request:  "a", storage: "a-", err: `invalid storage "a-": invalid subkey "a-"`,
		},
		{
			testName: "missing closing curly bracket",
			request:  "{a{", storage: "a", err: `invalid request "{a{": invalid subkey "{a{"`,
		},
		{
			testName: "missing opening curly bracket",
			request:  "a", storage: "}a}", err: `invalid storage "}a}": invalid subkey "}a}"`,
		},
		{
			testName: "curly brackets not wrapping subkey",
			request:  "a", storage: "a.b{a}c", err: `invalid storage "a.b{a}c": invalid subkey "b{a}c"`,
		},
		{
			testName: "invalid whitespace character",
			request:  "a. .c", storage: "a.b", err: `invalid request "a. .c": invalid subkey " "`,
		},
	} {
		_, err := registry.New("acc", "foo", map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": tc.request, "storage": tc.storage},
				},
			},
		}, registry.NewJSONSchema())

		cmt := Commentf("sub-test %q failed", tc.testName)
		c.Assert(err, Not(IsNil), cmt)
		c.Assert(err.Error(), Equals, `cannot define view "foo": `+tc.err, cmt)
	}
}

func (s *viewSuite) TestViewUnsetTopLevelEntry(c *C) {
	databag := registry.NewJSONDataBag()
	reg, err := registry.New("acc", "foo", map[string]interface{}{
		"my-view": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "bar"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := reg.View("my-view")
	err = view.Set(databag, "foo", "fval")
	c.Assert(err, IsNil)

	err = view.Set(databag, "bar", "bval")
	c.Assert(err, IsNil)

	err = view.Unset(databag, "foo")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "foo")
	c.Assert(err, testutil.ErrorIs, &registry.NotFoundError{})

	value, err := view.Get(databag, "bar")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "bval")
}

func (s *viewSuite) TestViewUnsetLeafWithSiblings(c *C) {
	databag := registry.NewJSONDataBag()
	reg, err := registry.New("acc", "foo", map[string]interface{}{
		"my-view": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "bar", "storage": "foo.bar"},
				map[string]interface{}{"request": "baz", "storage": "foo.baz"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := reg.View("my-view")
	err = view.Set(databag, "bar", "barVal")
	c.Assert(err, IsNil)

	err = view.Set(databag, "baz", "bazVal")
	c.Assert(err, IsNil)

	err = view.Unset(databag, "bar")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "bar")
	c.Assert(err, testutil.ErrorIs, &registry.NotFoundError{})

	// doesn't affect the other leaf entry under "foo"
	value, err := view.Get(databag, "baz")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "bazVal")
}

func (s *viewSuite) TestViewUnsetWithNestedEntry(c *C) {
	databag := registry.NewJSONDataBag()
	reg, err := registry.New("acc", "foo", map[string]interface{}{
		"my-view": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "foo.bar"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := reg.View("my-view")
	err = view.Set(databag, "bar", "barVal")
	c.Assert(err, IsNil)

	err = view.Unset(databag, "foo")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "foo")
	c.Assert(err, testutil.ErrorIs, &registry.NotFoundError{})

	_, err = view.Get(databag, "bar")
	c.Assert(err, testutil.ErrorIs, &registry.NotFoundError{})
}

func (s *viewSuite) TestViewUnsetLeafLeavesEmptyParent(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "foo", map[string]interface{}{
		"my-view": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "foo.bar"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("my-view")
	err = view.Set(databag, "bar", "val")
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, Not(HasLen), 0)

	err = view.Unset(databag, "bar")
	c.Assert(err, IsNil)

	value, err = view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{})
}

func (s *viewSuite) TestViewUnsetAlreadyUnsetEntry(c *C) {
	databag := registry.NewJSONDataBag()
	reg, err := registry.New("acc", "foo", map[string]interface{}{
		"my-view": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "one.bar"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := reg.View("my-view")
	err = view.Unset(databag, "foo")
	c.Assert(err, IsNil)

	err = view.Unset(databag, "bar")
	c.Assert(err, IsNil)
}

func (s *viewSuite) TestJSONDataBagCopy(c *C) {
	bag := registry.NewJSONDataBag()
	err := bag.Set("foo", "bar")
	c.Assert(err, IsNil)

	// precondition check
	data, err := bag.Data()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"foo":"bar"}`)

	bagCopy := bag.Copy()
	data, err = bagCopy.Data()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"foo":"bar"}`)

	// changes in the copied bag don't affect the original
	err = bagCopy.Set("foo", "baz")
	c.Assert(err, IsNil)

	data, err = bag.Data()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"foo":"bar"}`)

	// and vice-versa
	err = bag.Set("foo", "zab")
	c.Assert(err, IsNil)

	data, err = bagCopy.Data()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"foo":"baz"}`)
}

func (s *viewSuite) TestViewGetResultNamespaceMatchesRequest(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "one", "storage": "one"},
				map[string]interface{}{"request": "one.two", "storage": "one.two"},
				map[string]interface{}{"request": "onetwo", "storage": "one.two"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("bar")
	err = databag.Set("one", map[string]interface{}{"two": "value"})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "one.two")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "value")

	value, err = view.Get(databag, "onetwo")
	c.Assert(err, IsNil)
	// the key matches the request, not the storage storage
	c.Assert(value, DeepEquals, "value")

	value, err = view.Get(databag, "one")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"two": "value"})
}

func (s *viewSuite) TestViewGetMatchesOnPrefix(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snapd.status", "storage": "snaps.snapd.status"},
				map[string]interface{}{"request": "snaps", "storage": "snaps"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("statuses")
	err = view.Set(databag, "snaps", map[string]map[string]interface{}{
		"snapd":   {"status": "active", "version": "1.0"},
		"firefox": {"status": "inactive", "version": "9.0"},
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "snapd.status")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "active")

	value, err = view.Get(databag, "snapd")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"status": "active"})
}

func (s *viewSuite) TestViewUnsetValidates(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"test": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, &failingSchema{err: errors.New("boom")})
	c.Assert(err, IsNil)

	view := registry.View("test")
	err = view.Unset(databag, "foo")
	c.Assert(err, ErrorMatches, `cannot unset data: boom`)
}

func (s *viewSuite) TestViewUnsetSkipsReadOnly(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"test": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo", "access": "read"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("test")
	err = view.Unset(databag, "foo")
	c.Assert(err, ErrorMatches, `cannot unset "foo" in registry view acc/registry/test: no matching write rule`)
}

func (s *viewSuite) TestViewGetNoMatchRequestLongerThanPattern(c *C) {
	databag := registry.NewJSONDataBag()
	reg, err := registry.New("acc", "registry", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snapd", "storage": "snaps.snapd"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := reg.View("statuses")
	err = view.Set(databag, "snapd", map[string]interface{}{
		"status": "active", "version": "1.0",
	})
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "snapd.status")
	c.Assert(err, testutil.ErrorIs, &registry.NotFoundError{})
}

func (s *viewSuite) TestViewManyPrefixMatches(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "status.firefox", "storage": "snaps.firefox.status"},
				map[string]interface{}{"request": "status.snapd", "storage": "snaps.snapd.status"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("statuses")
	err = view.Set(databag, "status.firefox", "active")
	c.Assert(err, IsNil)

	err = view.Set(databag, "status.snapd", "disabled")
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "status")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals,
		map[string]interface{}{
			"snapd":   "disabled",
			"firefox": "active",
		})
}

func (s *viewSuite) TestViewCombineNamespacesInPrefixMatches(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "status.foo.bar.firefox", "storage": "snaps.firefox.status"},
				map[string]interface{}{"request": "status.foo.snapd", "storage": "snaps.snapd.status"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set("snaps", map[string]interface{}{
		"firefox": map[string]interface{}{
			"status": "active",
		},
		"snapd": map[string]interface{}{
			"status": "disabled",
		},
	})
	c.Assert(err, IsNil)

	view := registry.View("statuses")

	value, err := view.Get(databag, "status")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals,
		map[string]interface{}{
			"foo": map[string]interface{}{
				"bar": map[string]interface{}{
					"firefox": "active",
				},
				"snapd": "disabled",
			},
		})
}

func (s *viewSuite) TestGetScalarOverwritesLeafOfMapValue(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"motors": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "motors.a.speed", "storage": "new-speed.a"},
				map[string]interface{}{"request": "motors", "storage": "motors"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set("motors", map[string]interface{}{
		"a": map[string]interface{}{
			"speed": 100,
		},
	})
	c.Assert(err, IsNil)

	err = databag.Set("new-speed", map[string]interface{}{
		"a": 101.5,
	})
	c.Assert(err, IsNil)

	view := registry.View("motors")

	value, err := view.Get(databag, "motors")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"a": map[string]interface{}{"speed": 101.5}})
}

func (s *viewSuite) TestGetSingleScalarOk(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set("foo", "bar")
	c.Assert(err, IsNil)

	view := registry.View("foo")

	value, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "bar")
}

func (s *viewSuite) TestGetMatchScalarAndMapError(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "bar"},
				map[string]interface{}{"request": "foo.baz", "storage": "baz"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set("bar", 1)
	c.Assert(err, IsNil)
	err = databag.Set("baz", 2)
	c.Assert(err, IsNil)

	view := registry.View("foo")

	_, err = view.Get(databag, "foo")
	c.Assert(err, ErrorMatches, `cannot merge results of different types float64, map\[string\]interface {}`)
}

func (s *viewSuite) TestGetRulesAreSortedByParentage(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.bar.baz", "storage": "third"},
				map[string]interface{}{"request": "foo", "storage": "first"},
				map[string]interface{}{"request": "foo.bar", "storage": "second"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)
	view := registry.View("foo")

	err = databag.Set("first", map[string]interface{}{"bar": map[string]interface{}{"baz": "first"}})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	// returned the value read by entry "foo"
	c.Assert(value, DeepEquals, map[string]interface{}{"bar": map[string]interface{}{"baz": "first"}})

	err = databag.Set("second", map[string]interface{}{"baz": "second"})
	c.Assert(err, IsNil)

	value, err = view.Get(databag, "foo")
	c.Assert(err, IsNil)
	// the leaf is replaced by a value read from a rule that is nested
	c.Assert(value, DeepEquals, map[string]interface{}{"bar": map[string]interface{}{"baz": "second"}})

	err = databag.Set("third", "third")
	c.Assert(err, IsNil)

	value, err = view.Get(databag, "foo")
	c.Assert(err, IsNil)
	// lastly, it reads the value from "foo.bar.baz" the most nested entry
	c.Assert(value, DeepEquals, map[string]interface{}{"bar": map[string]interface{}{"baz": "third"}})
}

func (s *viewSuite) TestGetUnmatchedPlaceholderReturnsAll(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"snaps": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.{snap}", "storage": "snaps.{snap}"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)
	view := registry.View("snaps")
	c.Assert(view, NotNil)

	err = databag.Set("snaps", map[string]interface{}{
		"snapd": 1,
		"foo": map[string]interface{}{
			"bar": 2,
		},
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "snaps")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"snapd": float64(1), "foo": map[string]interface{}{"bar": float64(2)}})
}

func (s *viewSuite) TestGetUnmatchedPlaceholdersWithNestedValues(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.{snap}.status", "storage": "snaps.{snap}.status"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)
	view := registry.View("statuses")
	c.Assert(view, NotNil)

	err = databag.Set("snaps", map[string]interface{}{
		"snapd": map[string]interface{}{
			"status": "active",
		},
		"foo": map[string]interface{}{
			"version": 2,
		},
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "snaps")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"snapd": map[string]interface{}{"status": "active"}})
}

func (s *viewSuite) TestGetSeveralUnmatchedPlaceholders(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.{b}.c.{d}.e", "storage": "a.{b}.c.{d}.e"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)
	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = databag.Set("a", map[string]interface{}{
		"b1": map[string]interface{}{
			"c": map[string]interface{}{
				// the request can be fulfilled here
				"d1": map[string]interface{}{
					"e": "end",
					"f": "not-included",
				},
				"d2": "f",
			},
			"x": 1,
		},
		"b2": map[string]interface{}{
			"c": map[string]interface{}{
				// but not here
				"d1": "e",
				"d2": "f",
			},
			"x": 1,
		},
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "a")
	c.Assert(err, IsNil)
	expected := map[string]interface{}{
		"b1": map[string]interface{}{
			"c": map[string]interface{}{
				"d1": map[string]interface{}{
					"e": "end",
				},
			},
		},
	}
	c.Assert(value, DeepEquals, expected)
}

func (s *viewSuite) TestGetMergeAtDifferentLevels(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.{b}.c.{d}.e", "storage": "a.{b}.c.{d}.e"},
				map[string]interface{}{"request": "a.{b}.c.{d}", "storage": "a.{b}.c.{d}"},
				map[string]interface{}{"request": "a.{b}", "storage": "a.{b}"},
				map[string]interface{}{"request": "a", "storage": "a"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)
	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = databag.Set("a", map[string]interface{}{
		"b": map[string]interface{}{
			"c": map[string]interface{}{
				"d": map[string]interface{}{
					"e": "end",
				},
			},
		},
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "a")
	c.Assert(err, IsNil)
	expected := map[string]interface{}{
		"b": map[string]interface{}{
			"c": map[string]interface{}{
				"d": map[string]interface{}{
					"e": "end",
				},
			},
		},
	}
	c.Assert(value, DeepEquals, expected)
}

func (s *viewSuite) TestBadRequestPaths(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.{b}.c", "storage": "a.{b}.c"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = databag.Set("a", map[string]interface{}{
		"b": map[string]interface{}{
			"c": "value",
		},
	})
	c.Assert(err, IsNil)

	type testcase struct {
		request string
		errMsg  string
	}

	tcs := []testcase{
		{
			request: "a.",
			errMsg:  "cannot have empty subkeys",
		},
		{
			request: "a.b.",
			errMsg:  "cannot have empty subkeys",
		},
		{
			request: ".a",
			errMsg:  "cannot have empty subkeys",
		},
		{
			request: ".",
			errMsg:  "cannot have empty subkeys",
		},
		{
			request: "a..b",
			errMsg:  "cannot have empty subkeys",
		},
		{
			request: "a.{b}",
			errMsg:  `invalid subkey "{b}"`,
		},
		{
			request: "a.-b",
			errMsg:  `invalid subkey "-b"`,
		},
		{
			request: "a.b-",
			errMsg:  `invalid subkey "b-"`,
		},
		{
			request: "0-b",
			errMsg:  `invalid subkey "0-b"`,
		},
	}

	for _, tc := range tcs {
		cmt := Commentf("test %q failed", tc.request)
		err = view.Set(databag, tc.request, "value")
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot set %q in registry view acc/registry/foo: %s`, tc.request, tc.errMsg), cmt)

		_, err = view.Get(databag, tc.request)
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot get %q in registry view acc/registry/foo: %s`, tc.request, tc.errMsg), cmt)

		err = view.Unset(databag, tc.request)
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot unset %q in registry view acc/registry/foo: %s`, tc.request, tc.errMsg), cmt)
	}
}

func (s *viewSuite) TestSetAllowedOnSameRequestButDifferentPaths(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.b.c", "storage": "new", "access": "write"},
				map[string]interface{}{"request": "a.b.c", "storage": "old", "access": "write"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)
	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "a.b.c", "value")
	c.Assert(err, IsNil)

	stored, err := databag.Get("old")
	c.Assert(err, IsNil)
	c.Assert(stored, Equals, "value")

	stored, err = databag.Get("new")
	c.Assert(err, IsNil)
	c.Assert(stored, Equals, "value")
}

func (s *viewSuite) TestSetWritesToMoreNestedLast(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		// purposefully unordered to check that Set doesn't depend on well-ordered entries in assertions
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.snapd.name", "storage": "snaps.snapd.name"},
				map[string]interface{}{"request": "snaps.snapd", "storage": "snaps.snapd"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "snaps.snapd", map[string]interface{}{
		"name": "snapd",
	})
	c.Assert(err, IsNil)

	val, err := databag.Get("snaps")
	c.Assert(err, IsNil)

	c.Assert(val, DeepEquals, map[string]interface{}{
		"snapd": map[string]interface{}{
			"name": "snapd",
		},
	})
}

func (s *viewSuite) TestReadWriteRead(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.b.c", "storage": "a.b.c"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	initData := map[string]interface{}{
		"b": map[string]interface{}{
			"c": "end",
		},
	}
	err = databag.Set("a", initData)
	c.Assert(err, IsNil)

	data, err := view.Get(databag, "a")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, initData)

	err = view.Set(databag, "a", data)
	c.Assert(err, IsNil)

	data, err = view.Get(databag, "a")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, initData)
}

func (s *viewSuite) TestReadWriteSameDataAtDifferentLevels(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.b.c", "storage": "a.b.c"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)
	view := registry.View("foo")
	c.Assert(view, NotNil)

	initialData := map[string]interface{}{
		"b": map[string]interface{}{
			"c": "end",
		},
	}
	err = databag.Set("a", initialData)
	c.Assert(err, IsNil)

	for _, req := range []string{"a", "a.b", "a.b.c"} {
		val, err := view.Get(databag, req)
		c.Assert(err, IsNil)

		err = view.Set(databag, req, val)
		c.Assert(err, IsNil)
	}

	data, err := databag.Get("a")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, initialData)
}

func (s *viewSuite) TestSetValueMissingNestedLevels(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.b", "storage": "a.b"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)
	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "a", "foo")
	c.Assert(err, ErrorMatches, `cannot set "a" in registry view acc/registry/foo: expected map for unmatched request parts but got string`)

	err = view.Set(databag, "a", map[string]interface{}{"c": "foo"})
	c.Assert(err, ErrorMatches, `cannot set "a" in registry view acc/registry/foo: cannot use unmatched part "b" as key in map\[c:foo\]`)
}

func (s *viewSuite) TestGetReadsStorageLessNestedNamespaceBefore(c *C) {
	// Get reads by order of namespace (not path) nestedness. This test explicitly
	// tests for this and showcases why it matters. In Get we care about building
	// a virtual document from locations in the storage that may evolve over time.
	// In this example, the storage evolve to have version data in a different place
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.snapd", "storage": "snaps.snapd"},
				map[string]interface{}{"request": "snaps.snapd.version", "storage": "anewversion"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set("snaps", map[string]interface{}{
		"snapd": map[string]interface{}{
			"version": 1,
		},
	})
	c.Assert(err, IsNil)

	err = databag.Set("anewversion", 2)
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	data, err := view.Get(databag, "snaps")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]interface{}{
		"snapd": map[string]interface{}{
			"version": float64(2),
		},
	})
}

func (s *viewSuite) TestSetValidateError(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "bar", "storage": "bar"},
			},
		},
	}, &failingSchema{err: errors.New("expected error")})
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "bar", "baz")
	c.Assert(err, ErrorMatches, "cannot write data: expected error")
}

func (s *viewSuite) TestSetOverwriteValueWithNewLevel(c *C) {
	databag := registry.NewJSONDataBag()
	err := databag.Set("foo", "bar")
	c.Assert(err, IsNil)

	err = databag.Set("foo.bar", "baz")
	c.Assert(err, IsNil)

	data, err := databag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]interface{}{"bar": "baz"})
}

func (s *viewSuite) TestSetValidatesDataWithSchemaPass(c *C) {
	schema, err := registry.ParseSchema([]byte(`{
	"aliases": {
		"int-map": {
			"type": "map",
			"values": {
				"type": "int",
				"min": 0
			}
		},
		"str-array": {
			"type": "array",
			"values": {
				"type": "string"
			}
		}
	},
	"schema": {
		"foo": "$int-map",
		"bar": "$str-array"
	}
}`))
	c.Assert(err, IsNil)

	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "bar"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]int{"a": 1, "b": 2})
	c.Assert(err, IsNil)

	err = view.Set(databag, "bar", []string{"one", "two"})
	c.Assert(err, IsNil)
}

func (s *viewSuite) TestSetPreCheckValueFailsIncompatibleTypes(c *C) {
	type schemaType struct {
		schemaStr string
		typ       string
		value     interface{}
	}

	types := []schemaType{
		{
			schemaStr: `"int"`,
			typ:       "int",
			value:     int(0),
		},
		{
			schemaStr: `"number"`,
			typ:       "number",
			value:     float64(0),
		},
		{
			schemaStr: `"string"`,
			typ:       "string",
			value:     "foo",
		},
		{
			schemaStr: `"bool"`,
			typ:       "bool",
			value:     true,
		},
		{
			schemaStr: `{"type": "array", "values": "any"}`,
			typ:       "array",
			value:     []string{"foo"},
		},
		{
			schemaStr: `{"type": "map", "values": "any"}`,
			typ:       "map",
			value:     map[string]string{"foo": "bar"},
		},
	}

	for _, one := range types {
		for _, other := range types {
			if one.typ == other.typ || (one.typ == "int" && other.typ == "number") ||
				(one.typ == "number" && other.typ == "int") {
				continue
			}

			schema, err := registry.ParseSchema([]byte(fmt.Sprintf(`{
	"schema": {
		"foo": %s,
		"bar": %s
	}
}`, one.schemaStr, other.schemaStr)))
			c.Assert(err, IsNil)

			_, err = registry.New("acc", "registry", map[string]interface{}{
				"foo": map[string]interface{}{
					"rules": []interface{}{
						map[string]interface{}{"request": "foo", "storage": "foo", "access": "write"},
						map[string]interface{}{"request": "foo", "storage": "bar", "access": "write"},
					},
				},
			}, schema)
			c.Assert(err, ErrorMatches, fmt.Sprintf(`.*storage paths "foo" and "bar" for request "foo" require incompatible types: %s != %s`, one.typ, other.typ))
		}
	}
}

func (s *viewSuite) TestSetPreCheckValueAllowsIntNumberMismatch(c *C) {
	schema, err := registry.ParseSchema([]byte(`{
	"schema": {
		"foo": "int",
		"bar": "number"
	}
}`))
	c.Assert(err, IsNil)

	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo", "access": "write"},
				map[string]interface{}{"request": "foo", "storage": "bar", "access": "write"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", 1)
	c.Assert(err, IsNil)

	// the schema still checks the data at the end, so setting int schema to a float fails
	err = view.Set(databag, "foo", 1.1)
	c.Assert(err, ErrorMatches, `.*cannot accept element in "foo": expected int type but value was number 1.1`)
}

func (*viewSuite) TestSetPreCheckMultipleAlternativeTypesFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["int", "bool"],
		"bar": ["string", {"type": "array", "values": "string"}, {"schema": {"baz":"string"}}]
	}
}`)
	schema, err := registry.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	_, err = registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo", "access": "write"},
				map[string]interface{}{"request": "foo", "storage": "bar", "access": "write"},
			},
		},
	}, schema)
	c.Assert(err, ErrorMatches, `.*storage paths "foo" and "bar" for request "foo" require incompatible types: \[int, bool\] != \[string, array, map\]`)
}

func (*viewSuite) TestAssertionRuleSchemaMismatch(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": "int",
		"bar": {
			"schema": {
				"b": "string"
			}
		}
	}
}`)
	schema, err := registry.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo.b.c", "access": "write"},
				map[string]interface{}{"request": "foo", "storage": "bar.b.c", "access": "write"},
			},
		},
	}, schema)
	c.Assert(err, ErrorMatches, `.*storage path "foo.b.c" for request "foo" is invalid after "foo": cannot follow path beyond "int" type`)
	c.Assert(registry, IsNil)
}

func (*viewSuite) TestSchemaMismatchCheckDifferentLevelPaths(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"snaps": {
			"values": {
				"schema": {
					"status": {
						"type": "string"
					}
				}
			}
		}
	}
}`)
	schema, err := registry.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	_, err = registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.{snap}", "storage": "snaps.{snap}"},
				map[string]interface{}{"request": "snaps.{snap}.status", "storage": "snaps.{snap}.status"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)
}

func (*viewSuite) TestSchemaMismatchCheckMultipleAlternativeTypesHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["int", "bool"],
		"bar": ["string", "bool"]
	}
}`)
	schema, err := registry.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo", "access": "write"},
				map[string]interface{}{"request": "foo", "storage": "bar", "access": "write"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", true)
	c.Assert(err, IsNil)
}

func (s *viewSuite) TestSetUnmatchedPlaceholderLeaf(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}", "storage": "foo.{bar}"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]interface{}{
		"bar": "value",
		"baz": "other",
	})
	c.Assert(err, IsNil)

	data, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]interface{}{
		"bar": "value",
		"baz": "other",
	})
}

func (s *viewSuite) TestSetUnmatchedPlaceholderMidPath(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}.nested", "storage": "foo.{bar}.nested"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]interface{}{
		"bar": map[string]interface{}{"nested": "value"},
		"baz": map[string]interface{}{"nested": "other"},
	})
	c.Assert(err, IsNil)

	data, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]interface{}{
		"bar": map[string]interface{}{"nested": "value"},
		"baz": map[string]interface{}{"nested": "other"},
	})
}

func (s *viewSuite) TestSetManyUnmatchedPlaceholders(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}.a.{baz}", "storage": "foo.{bar}.{baz}"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]interface{}{
		"a": map[string]interface{}{"a": map[string]interface{}{
			"c": "value",
			"d": "other",
		}},
		"b": map[string]interface{}{"a": map[string]interface{}{
			"e": "value",
			"f": "other",
		}},
	})
	c.Assert(err, IsNil)

	data, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]interface{}{
		"a": map[string]interface{}{"a": map[string]interface{}{
			"c": "value",
			"d": "other",
		}},
		"b": map[string]interface{}{"a": map[string]interface{}{
			"e": "value",
			"f": "other",
		}},
	})
}

func (s *viewSuite) TestUnsetUnmatchedPlaceholderLast(c *C) {
	databag := registry.NewJSONDataBag()
	reg, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}", "storage": "foo.{bar}"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := reg.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]interface{}{
		"bar": "value",
		"baz": "other",
	})
	c.Assert(err, IsNil)

	err = view.Unset(databag, "foo")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "foo")
	c.Assert(err, testutil.ErrorIs, &registry.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "foo" in registry view acc/registry/foo: matching rules don't map to any values`)
}

func (s *viewSuite) TestUnsetUnmatchedPlaceholderMid(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "all.{bar}", "storage": "foo.{bar}"},
				map[string]interface{}{"request": "one.{bar}", "storage": "foo.{bar}.one"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "all", map[string]interface{}{
		// should remove only the "one" path
		"a": map[string]interface{}{
			"one": "value",
			"two": "other",
		},
		// the nested value should be removed, leaving an empty map
		"b": map[string]interface{}{
			"one": "value",
		},
		// should be untouched (no "one" path)
		"c": map[string]interface{}{
			"two": "value",
		},
	})
	c.Assert(err, IsNil)

	err = view.Unset(databag, "one")
	c.Assert(err, IsNil)

	val, err := view.Get(databag, "all")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, map[string]interface{}{
		"a": map[string]interface{}{
			"two": "other",
		},
		"b": map[string]interface{}{},
		"c": map[string]interface{}{
			"two": "value",
		},
	})
}

func (s *viewSuite) TestGetValuesThroughPaths(c *C) {
	type testcase struct {
		path     string
		suffix   []string
		value    interface{}
		expected map[string]interface{}
		err      string
	}

	tcs := []testcase{
		{
			path:     "foo.bar",
			suffix:   nil,
			value:    "value",
			expected: map[string]interface{}{"foo.bar": "value"},
		},
		{
			path:     "foo.{bar}",
			suffix:   []string{"{bar}"},
			value:    map[string]interface{}{"a": "value", "b": "other"},
			expected: map[string]interface{}{"foo.a": "value", "foo.b": "other"},
		},
		{
			path:   "foo.{bar}.baz",
			suffix: []string{"{bar}", "baz"},
			value: map[string]interface{}{
				"a": map[string]interface{}{"baz": "value"},
				"b": map[string]interface{}{"baz": "other"},
			},
			expected: map[string]interface{}{"foo.a.baz": "value", "foo.b.baz": "other"},
		},
		{
			path:   "foo.{bar}.{baz}.last",
			suffix: []string{"{bar}", "{baz}"},
			value: map[string]interface{}{
				"a": map[string]interface{}{"b": "value"},
				"c": map[string]interface{}{"d": "other"},
			},
			expected: map[string]interface{}{"foo.a.b.last": "value", "foo.c.d.last": "other"},
		},

		{
			path:   "foo.{bar}",
			suffix: []string{"{bar}", "baz"},
			value: map[string]interface{}{
				"a": map[string]interface{}{"baz": "value", "ignore": 1},
				"b": map[string]interface{}{"baz": "other", "ignore": 1},
			},
			expected: map[string]interface{}{"foo.a": "value", "foo.b": "other"},
		},
		{
			path:   "foo.{bar}",
			suffix: []string{"{bar}"},
			value:  "a",
			err:    "expected map for unmatched request parts but got string",
		},
		{
			path:   "foo.{bar}",
			suffix: []string{"{bar}", "baz"},
			value: map[string]interface{}{
				"a": map[string]interface{}{"notbaz": 1},
				"b": map[string]interface{}{"notbaz": 1},
			},
			err: `cannot use unmatched part "baz" as key in map\[notbaz:1\]`,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("failed test number %d", i+1)
		pathsToValues, err := registry.GetValuesThroughPaths(tc.path, tc.suffix, tc.value)

		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err, cmt)
			c.Check(pathsToValues, IsNil, cmt)
		} else {
			c.Check(err, IsNil, cmt)
			c.Check(pathsToValues, DeepEquals, tc.expected, cmt)
		}
	}
}

func (s *viewSuite) TestViewSetErrorIfValueContainsUnusedParts(c *C) {
	type testcase struct {
		request string
		value   interface{}
		err     string
	}

	tcs := []testcase{
		{
			request: "a",
			value: map[string]interface{}{
				"b": map[string]interface{}{"d": "value", "u": 1},
			},
			err: `cannot set "a" in registry view acc/registry/foo: value contains unused data under "b.u"`,
		},
		{
			request: "a",
			value: map[string]interface{}{
				"b": map[string]interface{}{"d": "value", "u": 1},
				"c": map[string]interface{}{"d": "value"},
			},
			err: `cannot set "a" in registry view acc/registry/foo: value contains unused data under "b.u"`,
		},
		{
			request: "b",
			value: map[string]interface{}{
				"e": []interface{}{"a"},
				"f": 1,
			},
			err: `cannot set "b" in registry view acc/registry/foo: value contains unused data under "e"`,
		},
		{
			request: "c",
			value: map[string]interface{}{
				"d": map[string]interface{}{
					"e": map[string]interface{}{
						"f": "value",
					},
					"f": 1,
				},
			},
			err: `cannot set "c" in registry view acc/registry/foo: value contains unused data under "d.f"`,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("failed test number %d", i+1)
		databag := registry.NewJSONDataBag()
		registry, err := registry.New("acc", "registry", map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "a.{x}.d", "storage": "a.{x}"},
					map[string]interface{}{"request": "c.d.e.f", "storage": "d"},
					map[string]interface{}{"request": "b.f", "storage": "b.f"},
				},
			},
		}, registry.NewJSONSchema())
		c.Assert(err, IsNil)

		view := registry.View("foo")
		c.Assert(view, NotNil)

		err = view.Set(databag, tc.request, tc.value)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err, cmt)
		} else {
			c.Check(err, IsNil, cmt)
		}
	}
}

func (*viewSuite) TestViewSummaryWrongType(c *C) {
	for _, val := range []interface{}{
		1,
		true,
		[]interface{}{"foo"},
		map[string]interface{}{"foo": "bar"},
	} {
		registry, err := registry.New("acc", "registry", map[string]interface{}{
			"foo": map[string]interface{}{
				"summary": val,
				"rules": []interface{}{
					map[string]interface{}{"request": "foo", "storage": "foo"},
				},
			},
		}, nil)
		c.Check(err.Error(), Equals, fmt.Sprintf(`cannot define view "foo": view summary must be a string but got %T`, val))
		c.Check(registry, IsNil)
	}
}

func (*viewSuite) TestViewSummary(c *C) {
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"summary": "some summary of the view",
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)
	c.Assert(registry, NotNil)
}

func (s *viewSuite) TestGetEntireView(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}", "storage": "foo-path.{bar}"},
				map[string]interface{}{"request": "abc", "storage": "abc-path"},
				map[string]interface{}{"request": "write-only", "storage": "write-only", "access": "write"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]interface{}{
		"bar": "value",
		"baz": "other",
	})
	c.Assert(err, IsNil)

	err = view.Set(databag, "abc", "cba")
	c.Assert(err, IsNil)

	err = view.Set(databag, "write-only", "value")
	c.Assert(err, IsNil)

	result, err := view.Get(databag, "")
	c.Assert(err, IsNil)

	c.Assert(result, DeepEquals, map[string]interface{}{
		"foo": map[string]interface{}{
			"bar": "value",
			"baz": "other",
		},
		"abc": "cba",
	})
}

func (*viewSuite) TestViewContentRule(c *C) {
	views := map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{
					"request": "a",
					"storage": "c",
					"content": []interface{}{
						map[string]interface{}{
							"request": "b",
							"storage": "d",
						},
					},
				},
			},
		},
	}

	reg, err := registry.New("acc", "foo", views, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	databag := registry.NewJSONDataBag()
	err = databag.Set("c.d", "value")
	c.Assert(err, IsNil)

	view := reg.View("bar")
	val, err := view.Get(databag, "a.b")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "value")

	err = view.Set(databag, "a.b", "other")
	c.Assert(err, IsNil)

	val, err = view.Get(databag, "a.b")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "other")
}

func (*viewSuite) TestViewWriteContentRuleNestedInRead(c *C) {
	views := map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{
					"request": "a",
					"storage": "c",
					"access":  "read",
					"content": []interface{}{
						map[string]interface{}{
							"request": "b",
							"storage": "d",
							"access":  "write",
						},
					},
				},
			},
		},
	}

	reg, err := registry.New("acc", "foo", views, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	databag := registry.NewJSONDataBag()
	view := reg.View("bar")
	err = view.Set(databag, "a.b", "value")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "a.b")
	c.Assert(err, ErrorMatches, `.*: no matching read rule`)

	val, err := view.Get(databag, "a")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, map[string]interface{}{"d": "value"})
}

func (*viewSuite) TestViewInvalidContentRules(c *C) {
	type testcase struct {
		content interface{}
		err     string
	}

	tcs := []testcase{
		{
			content: []interface{}{},
			err:     `.*"content" must be a non-empty list`,
		},
		{
			content: map[string]interface{}{},
			err:     `.*"content" must be a non-empty list`,
		},
		{
			content: []interface{}{map[string]interface{}{"request": "a"}},
			err:     `.*view rules must have a "storage" field`,
		},
	}

	for _, tc := range tcs {
		rules := map[string]interface{}{
			"bar": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{
						"request": "a",
						"storage": "c",
						"content": tc.content,
					},
				},
			},
		}

		_, err := registry.New("acc", "foo", rules, registry.NewJSONSchema())
		c.Assert(err, ErrorMatches, tc.err)
	}
}

func (*viewSuite) TestViewSeveralNestedContentRules(c *C) {
	rules := map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{
					"request": "a",
					"storage": "a",
					"content": []interface{}{
						map[string]interface{}{
							"request": "b.c",
							"storage": "b.c",
							"content": []interface{}{
								map[string]interface{}{
									"request": "d",
									"storage": "d",
								},
							},
						},
					},
				},
			},
		},
	}

	reg, err := registry.New("acc", "foo", rules, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	databag := registry.NewJSONDataBag()
	view := reg.View("bar")
	err = view.Set(databag, "a.b.c.d", "value")
	c.Assert(err, IsNil)

	val, err := view.Get(databag, "a.b.c.d")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "value")
}

func (*viewSuite) TestViewInvalidMapKeys(c *C) {
	reg, err := registry.New("acc", "foo", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{
					"request": "foo",
					"storage": "foo",
				},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	databag := registry.NewJSONDataBag()
	view := reg.View("bar")

	type testcase struct {
		value      interface{}
		invalidKey string
	}

	tcs := []testcase{
		{
			value:      map[string]interface{}{"-foo": 2},
			invalidKey: "-foo",
		},
		{
			value:      map[string]interface{}{"foo--bar": 2},
			invalidKey: "foo--bar",
		},
		{
			value:      map[string]interface{}{"foo-": 2},
			invalidKey: "foo-",
		},
		{
			value:      map[string]interface{}{"foo": map[string]interface{}{"-bar": 2}},
			invalidKey: "-bar",
		},
		{
			value:      map[string]interface{}{"foo": map[string]interface{}{"bar": map[string]interface{}{"baz-": 2}}},
			invalidKey: "baz-",
		},
		{
			value:      []interface{}{map[string]interface{}{"foo": 2}, map[string]interface{}{"bar-": 2}},
			invalidKey: "bar-",
		},
		{
			value:      []interface{}{nil, map[string]interface{}{"bar-": 2}},
			invalidKey: "bar-",
		},
		{
			value:      map[string]interface{}{"foo": nil, "bar": map[string]interface{}{"-baz": 2}},
			invalidKey: "-baz",
		},
	}

	for _, tc := range tcs {
		cmt := Commentf("expected invalid key err for value: %v", tc.value)
		err = view.Set(databag, "foo", tc.value)
		c.Assert(err, ErrorMatches, fmt.Sprintf("cannot set \"foo\" in registry view acc/foo/bar: key %q doesn't conform to required format: .*", tc.invalidKey), cmt)
	}
}

func (s *viewSuite) TestSetUsingMapWithNilValuesAtLeaves(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "foo.a", "storage": "foo.a"},
				map[string]interface{}{"request": "foo.b", "storage": "foo.b"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]interface{}{
		"a": "value",
		"b": "other",
	})
	c.Assert(err, IsNil)

	err = view.Set(databag, "foo", map[string]interface{}{
		"a": nil,
		"b": nil,
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{})
}

func (s *viewSuite) TestSetWithMultiplePathsNestedAtLeaves(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "registry", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.a", "storage": "foo.a"},
				map[string]interface{}{"request": "foo.b", "storage": "foo.b"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]interface{}{
		"a": map[string]interface{}{
			"c": "value",
			"d": "other",
		},
		"b": "other",
	})
	c.Assert(err, IsNil)

	err = view.Set(databag, "foo", map[string]interface{}{
		"a": map[string]interface{}{
			"d": nil,
		},
		"b": nil,
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{
		// consistent with the previous configuration mechanism
		"a": map[string]interface{}{},
	})
}

func (s *viewSuite) TestSetWithNilAndNonNilLeaves(c *C) {
	databag := registry.NewJSONDataBag()
	registry, err := registry.New("acc", "reg", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	view := registry.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]interface{}{
		"a": "value",
		"b": "other",
	})
	c.Assert(err, IsNil)

	err = view.Set(databag, "foo", map[string]interface{}{
		"a": nil,
		"c": "value",
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	// nil values aren't stored but non-nil values are
	c.Assert(value, DeepEquals, map[string]interface{}{
		"c": "value",
	})
}

func (*viewSuite) TestSetEnforcesNestednessLimit(c *C) {
	restore := registry.MockMaxValueDepth(2)
	defer restore()

	reg, err := registry.New("acc", "foo", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{
					"request": "foo",
					"storage": "foo",
				},
			},
		},
	}, registry.NewJSONSchema())
	c.Assert(err, IsNil)

	databag := registry.NewJSONDataBag()
	view := reg.View("bar")

	err = view.Set(databag, "foo", map[string]interface{}{
		"bar": "baz",
	})
	c.Assert(err, IsNil)

	err = view.Set(databag, "foo", map[string]interface{}{
		"bar": map[string]interface{}{
			"baz": "value",
		},
	})
	c.Assert(err, ErrorMatches, `cannot set "foo" in registry view acc/foo/bar: value cannot have more than 2 nested levels`)
}
