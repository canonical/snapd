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
	"errors"
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/aspects"
	"github.com/snapcore/snapd/testutil"
)

type aspectSuite struct{}

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&aspectSuite{})

func (*aspectSuite) TestNewAspectBundle(c *C) {
	type testcase struct {
		bundle map[string]interface{}
		err    string
	}

	tcs := []testcase{
		{
			err: `cannot define aspects bundle: no aspects`,
		},
		{
			bundle: map[string]interface{}{"bar": "baz"},
			err:    `cannot define aspect "bar": aspect must be non-empty map`,
		},
		{
			bundle: map[string]interface{}{"bar": map[string]interface{}{}},
			err:    `cannot define aspect "bar": aspect must be non-empty map`,
		},
		{
			bundle: map[string]interface{}{"bar": map[string]interface{}{"rules": "bar"}},
			err:    `cannot define aspect "bar": aspect rules must be non-empty list`,
		},
		{
			bundle: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{}}},
			err:    `cannot define aspect "bar": aspect rules must be non-empty list`,
		},
		{
			bundle: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{"a"}}},
			err:    `cannot define aspect "bar": each aspect rule should be a map`,
		},
		{
			bundle: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{}}}},
			err:    `cannot define aspect "bar": aspect rules must have a "request" field`,
		},
		{
			bundle: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"request": 1}}}},
			err:    `cannot define aspect "bar": "request" must be a string`,
		},
		{
			bundle: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"request": "foo"}}}},
			err:    `cannot define aspect "bar": aspect rules must have a "storage" field`,
		},
		{
			bundle: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"request": "foo", "storage": 1}}}},
			err:    `cannot define aspect "bar": "storage" must be a string`,
		},
		{
			bundle: map[string]interface{}{
				"bar": map[string]interface{}{
					"rules": []interface{}{
						map[string]interface{}{"request": "a", "storage": "b"},
						map[string]interface{}{"request": "a", "storage": "c"},
					},
				},
			},
			err: `cannot define aspect "bar": cannot have several reading rules with the same "request" field`,
		},
		{
			bundle: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"request": "foo", "storage": "bar", "access": 1}}}},
			err:    `cannot define aspect "bar": "access" must be a string`,
		},
		{
			bundle: map[string]interface{}{
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
		aspectBundle, err := aspects.NewBundle("acc", "foo", tc.bundle, aspects.NewJSONSchema())
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Check(aspectBundle, Not(IsNil), cmt)
		}
	}
}

func (s *aspectSuite) TestBundleWithSample(c *C) {
	bundle := map[string]interface{}{
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
	_, err := aspects.NewBundle("acc", "foo", bundle, aspects.NewJSONSchema())
	c.Assert(err, IsNil)
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
		aspectBundle, err := aspects.NewBundle("acc", "foo", map[string]interface{}{
			"bar": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "a", "storage": "b", "access": t.access},
				},
			},
		}, aspects.NewJSONSchema())

		cmt := Commentf("\"%s access\" sub-test failed", t.access)
		if t.err {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`.*expected 'access' to be one of "read-write", "read", "write" but was %q`, t.access), cmt)
			c.Check(aspectBundle, IsNil, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Check(aspectBundle, Not(IsNil), cmt)
		}
	}
}

func (*aspectSuite) TestGetAndSetAspects(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("system", "network", map[string]interface{}{
		"wifi-setup": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "ssids", "storage": "wifi.ssids"},
				map[string]interface{}{"request": "ssid", "storage": "wifi.ssid"},
				map[string]interface{}{"request": "top-level", "storage": "top-level"},
				map[string]interface{}{"request": "dotted.path", "storage": "dotted"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	wsAspect := aspectBundle.Aspect("wifi-setup")

	// nested string value
	err = wsAspect.Set(databag, "ssid", "my-ssid")
	c.Assert(err, IsNil)

	ssid, err := wsAspect.Get(databag, "ssid")
	c.Assert(err, IsNil)
	c.Check(ssid, DeepEquals, "my-ssid")

	// nested list value
	err = wsAspect.Set(databag, "ssids", []string{"one", "two"})
	c.Assert(err, IsNil)

	ssids, err := wsAspect.Get(databag, "ssids")
	c.Assert(err, IsNil)
	c.Check(ssids, DeepEquals, []interface{}{"one", "two"})

	// top-level string
	err = wsAspect.Set(databag, "top-level", "randomValue")
	c.Assert(err, IsNil)

	topLevel, err := wsAspect.Get(databag, "top-level")
	c.Assert(err, IsNil)
	c.Check(topLevel, DeepEquals, "randomValue")

	// dotted request paths are permitted
	err = wsAspect.Set(databag, "dotted.path", 3)
	c.Assert(err, IsNil)

	num, err := wsAspect.Get(databag, "dotted.path")
	c.Assert(err, IsNil)
	c.Check(num, DeepEquals, float64(3))
}

func (*aspectSuite) TestSetWithNilValueFail(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("system", "test", map[string]interface{}{
		"test": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	wsAspect := aspectBundle.Aspect("test")

	err = wsAspect.Set(databag, "foo", "value")
	c.Assert(err, IsNil)

	err = wsAspect.Set(databag, "foo", nil)
	c.Assert(err, ErrorMatches, `Set value cannot be nil`)

	ssid, err := wsAspect.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Check(ssid, DeepEquals, "value")
}

func (s *aspectSuite) TestAspectNotFound(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "foo", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "top-level", "storage": "top-level"},
				map[string]interface{}{"request": "nested", "storage": "top.nested-one"},
				map[string]interface{}{"request": "other-nested", "storage": "top.nested-two"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("bar")

	_, err = aspect.Get(databag, "missing")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "missing" in aspect acc/foo/bar: no matching read rule`)

	err = aspect.Set(databag, "missing", "thing")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot set "missing" in aspect acc/foo/bar: no matching write rule`)

	err = aspect.Unset(databag, "missing")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot unset "missing" in aspect acc/foo/bar: no matching write rule`)

	_, err = aspect.Get(databag, "top-level")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "top-level" in aspect acc/foo/bar: matching rules don't map to any values`)

	err = aspect.Set(databag, "nested", "thing")
	c.Assert(err, IsNil)

	err = aspect.Unset(databag, "nested")
	c.Assert(err, IsNil)

	_, err = aspect.Get(databag, "other-nested")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "other-nested" in aspect acc/foo/bar: matching rules don't map to any values`)
}

func (s *aspectSuite) TestAspectBadRead(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "foo", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "one", "storage": "one"},
				map[string]interface{}{"request": "onetwo", "storage": "one.two"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("bar")
	err = aspect.Set(databag, "one", "foo")
	c.Assert(err, IsNil)

	_, err = aspect.Get(databag, "onetwo")
	c.Assert(err, ErrorMatches, `cannot read path prefix "one": prefix maps to string`)
}

func (s *aspectSuite) TestAspectsAccessControl(c *C) {
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
			getErr: `cannot get "foo" in aspect acc/bundle/foo: matching rules don't map to any values`,
			setErr: `cannot set "foo" in aspect acc/bundle/foo: no matching write rule`,
		},
		{
			access: "write",
			getErr: `cannot get "foo" in aspect acc/bundle/foo: no matching read rule`,
		},
	} {
		cmt := Commentf("sub-test with %q access failed", t.access)
		databag := aspects.NewJSONDataBag()
		aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "foo", "storage": "foo", "access": t.access},
				},
			},
		}, aspects.NewJSONSchema())
		c.Assert(err, IsNil)

		aspect := aspectBundle.Aspect("foo")

		err = aspect.Set(databag, "foo", "thing")
		if t.setErr != "" {
			c.Assert(err.Error(), Equals, t.setErr, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
		}

		_, err = aspect.Get(databag, "foo")
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

func newWitnessDataBag(bag aspects.DataBag) *witnessDataBag {
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

func (s *aspectSuite) TestAspectAssertionWithPlaceholder(c *C) {
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

		aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{t.rule},
			},
		}, aspects.NewJSONSchema())
		c.Assert(err, IsNil)
		aspect := aspectBundle.Aspect("foo")

		databag := newWitnessDataBag(aspects.NewJSONDataBag())
		err = aspect.Set(databag, t.request, "expectedValue")
		c.Assert(err, IsNil, cmt)

		value, err := aspect.Get(databag, t.request)
		c.Assert(err, IsNil, cmt)
		c.Assert(value, DeepEquals, "expectedValue", cmt)

		getPath, setPath := databag.getLastPaths()
		c.Assert(getPath, Equals, t.storage, cmt)
		c.Assert(setPath, Equals, t.storage, cmt)
	}
}

func (s *aspectSuite) TestAspectRequestAndStorageValidation(c *C) {
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
		_, err := aspects.NewBundle("acc", "foo", map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": tc.request, "storage": tc.storage},
				},
			},
		}, aspects.NewJSONSchema())

		cmt := Commentf("sub-test %q failed", tc.testName)
		c.Assert(err, Not(IsNil), cmt)
		c.Assert(err.Error(), Equals, `cannot define aspect "foo": `+tc.err, cmt)
	}
}

func (s *aspectSuite) TestAspectUnsetTopLevelEntry(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "foo", map[string]interface{}{
		"my-aspect": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "bar"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("my-aspect")
	err = aspect.Set(databag, "foo", "fval")
	c.Assert(err, IsNil)

	err = aspect.Set(databag, "bar", "bval")
	c.Assert(err, IsNil)

	err = aspect.Unset(databag, "foo")
	c.Assert(err, IsNil)

	_, err = aspect.Get(databag, "foo")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})

	value, err := aspect.Get(databag, "bar")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "bval")
}

func (s *aspectSuite) TestAspectUnsetLeafWithSiblings(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "foo", map[string]interface{}{
		"my-aspect": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "bar", "storage": "foo.bar"},
				map[string]interface{}{"request": "baz", "storage": "foo.baz"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("my-aspect")
	err = aspect.Set(databag, "bar", "barVal")
	c.Assert(err, IsNil)

	err = aspect.Set(databag, "baz", "bazVal")
	c.Assert(err, IsNil)

	err = aspect.Unset(databag, "bar")
	c.Assert(err, IsNil)

	_, err = aspect.Get(databag, "bar")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})

	// doesn't affect the other leaf entry under "foo"
	value, err := aspect.Get(databag, "baz")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "bazVal")
}

func (s *aspectSuite) TestAspectUnsetWithNestedEntry(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "foo", map[string]interface{}{
		"my-aspect": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "foo.bar"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("my-aspect")
	err = aspect.Set(databag, "bar", "barVal")
	c.Assert(err, IsNil)

	err = aspect.Unset(databag, "foo")
	c.Assert(err, IsNil)

	_, err = aspect.Get(databag, "foo")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})

	_, err = aspect.Get(databag, "bar")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
}

func (s *aspectSuite) TestAspectUnsetLeafUnsetsParent(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "foo", map[string]interface{}{
		"my-aspect": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "foo.bar"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("my-aspect")
	err = aspect.Set(databag, "bar", "val")
	c.Assert(err, IsNil)

	value, err := aspect.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, Not(HasLen), 0)

	err = aspect.Unset(databag, "bar")
	c.Assert(err, IsNil)

	_, err = aspect.Get(databag, "foo")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
}

func (s *aspectSuite) TestAspectUnsetAlreadyUnsetEntry(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "foo", map[string]interface{}{
		"my-aspect": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "one.bar"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("my-aspect")
	err = aspect.Unset(databag, "foo")
	c.Assert(err, IsNil)

	err = aspect.Unset(databag, "bar")
	c.Assert(err, IsNil)
}

func (s *aspectSuite) TestJSONDataBagCopy(c *C) {
	bag := aspects.NewJSONDataBag()
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

func (s *aspectSuite) TestAspectGetResultNamespaceMatchesRequest(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "one", "storage": "one"},
				map[string]interface{}{"request": "one.two", "storage": "one.two"},
				map[string]interface{}{"request": "onetwo", "storage": "one.two"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("bar")
	err = databag.Set("one", map[string]interface{}{"two": "value"})
	c.Assert(err, IsNil)

	value, err := aspect.Get(databag, "one.two")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "value")

	value, err = aspect.Get(databag, "onetwo")
	c.Assert(err, IsNil)
	// the key matches the request, not the storage storage
	c.Assert(value, DeepEquals, "value")

	value, err = aspect.Get(databag, "one")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"two": "value"})
}

func (s *aspectSuite) TestAspectGetMatchesOnPrefix(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snapd.status", "storage": "snaps.snapd.status"},
				map[string]interface{}{"request": "snaps", "storage": "snaps"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("statuses")
	err = aspect.Set(databag, "snaps", map[string]map[string]interface{}{
		"snapd":   {"status": "active", "version": "1.0"},
		"firefox": {"status": "inactive", "version": "9.0"},
	})
	c.Assert(err, IsNil)

	value, err := aspect.Get(databag, "snapd.status")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "active")

	value, err = aspect.Get(databag, "snapd")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"status": "active"})
}

func (s *aspectSuite) TestAspectUnsetValidates(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"test": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, &failingSchema{err: errors.New("boom")})
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("test")
	err = aspect.Unset(databag, "foo")
	c.Assert(err, ErrorMatches, `cannot unset data: boom`)
}

func (s *aspectSuite) TestAspectUnsetSkipsReadOnly(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"test": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo", "access": "read"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("test")
	err = aspect.Unset(databag, "foo")
	c.Assert(err, ErrorMatches, `cannot unset "foo" in aspect acc/bundle/test: no matching write rule`)
}

func (s *aspectSuite) TestAspectGetNoMatchRequestLongerThanPattern(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snapd", "storage": "snaps.snapd"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("statuses")
	err = aspect.Set(databag, "snapd", map[string]interface{}{
		"status": "active", "version": "1.0",
	})
	c.Assert(err, IsNil)

	_, err = aspect.Get(databag, "snapd.status")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
}

func (s *aspectSuite) TestAspectManyPrefixMatches(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "status.firefox", "storage": "snaps.firefox.status"},
				map[string]interface{}{"request": "status.snapd", "storage": "snaps.snapd.status"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("statuses")
	err = aspect.Set(databag, "status.firefox", "active")
	c.Assert(err, IsNil)

	err = aspect.Set(databag, "status.snapd", "disabled")
	c.Assert(err, IsNil)

	value, err := aspect.Get(databag, "status")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals,
		map[string]interface{}{
			"snapd":   "disabled",
			"firefox": "active",
		})
}

func (s *aspectSuite) TestAspectCombineNamespacesInPrefixMatches(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "status.foo.bar.firefox", "storage": "snaps.firefox.status"},
				map[string]interface{}{"request": "status.foo.snapd", "storage": "snaps.snapd.status"},
			},
		},
	}, aspects.NewJSONSchema())
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

	aspect := aspectBundle.Aspect("statuses")

	value, err := aspect.Get(databag, "status")
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

func (s *aspectSuite) TestGetScalarOverwritesLeafOfMapValue(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"motors": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "motors.a.speed", "storage": "new-speed.a"},
				map[string]interface{}{"request": "motors", "storage": "motors"},
			},
		},
	}, aspects.NewJSONSchema())
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

	aspect := aspectBundle.Aspect("motors")

	value, err := aspect.Get(databag, "motors")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"a": map[string]interface{}{"speed": 101.5}})
}

func (s *aspectSuite) TestGetSingleScalarOk(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set("foo", "bar")
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("foo")

	value, err := aspect.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "bar")
}

func (s *aspectSuite) TestGetMatchScalarAndMapError(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "bar"},
				map[string]interface{}{"request": "foo.baz", "storage": "baz"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set("bar", 1)
	c.Assert(err, IsNil)
	err = databag.Set("baz", 2)
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("foo")

	_, err = aspect.Get(databag, "foo")
	c.Assert(err, ErrorMatches, `cannot merge results of different types float64, map\[string\]interface {}`)
}

func (s *aspectSuite) TestGetRulesAreSortedByParentage(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.bar.baz", "storage": "third"},
				map[string]interface{}{"request": "foo", "storage": "first"},
				map[string]interface{}{"request": "foo.bar", "storage": "second"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)
	aspect := aspectBundle.Aspect("foo")

	err = databag.Set("first", map[string]interface{}{"bar": map[string]interface{}{"baz": "first"}})
	c.Assert(err, IsNil)

	value, err := aspect.Get(databag, "foo")
	c.Assert(err, IsNil)
	// returned the value read by entry "foo"
	c.Assert(value, DeepEquals, map[string]interface{}{"bar": map[string]interface{}{"baz": "first"}})

	err = databag.Set("second", map[string]interface{}{"baz": "second"})
	c.Assert(err, IsNil)

	value, err = aspect.Get(databag, "foo")
	c.Assert(err, IsNil)
	// the leaf is replaced by a value read from a rule that is nested
	c.Assert(value, DeepEquals, map[string]interface{}{"bar": map[string]interface{}{"baz": "second"}})

	err = databag.Set("third", "third")
	c.Assert(err, IsNil)

	value, err = aspect.Get(databag, "foo")
	c.Assert(err, IsNil)
	// lastly, it reads the value from "foo.bar.baz" the most nested entry
	c.Assert(value, DeepEquals, map[string]interface{}{"bar": map[string]interface{}{"baz": "third"}})
}

func (s *aspectSuite) TestGetUnmatchedPlaceholderReturnsAll(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"snaps": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.{snap}", "storage": "snaps.{snap}"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)
	aspect := aspectBundle.Aspect("snaps")
	c.Assert(aspect, NotNil)

	err = databag.Set("snaps", map[string]interface{}{
		"snapd": 1,
		"foo": map[string]interface{}{
			"bar": 2,
		},
	})
	c.Assert(err, IsNil)

	value, err := aspect.Get(databag, "snaps")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"snapd": float64(1), "foo": map[string]interface{}{"bar": float64(2)}})
}

func (s *aspectSuite) TestGetUnmatchedPlaceholdersWithNestedValues(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.{snap}.status", "storage": "snaps.{snap}.status"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)
	asp := aspectBundle.Aspect("statuses")
	c.Assert(asp, NotNil)

	err = databag.Set("snaps", map[string]interface{}{
		"snapd": map[string]interface{}{
			"status": "active",
		},
		"foo": map[string]interface{}{
			"version": 2,
		},
	})
	c.Assert(err, IsNil)

	value, err := asp.Get(databag, "snaps")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"snapd": map[string]interface{}{"status": "active"}})
}

func (s *aspectSuite) TestGetSeveralUnmatchedPlaceholders(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.{b}.c.{d}.e", "storage": "a.{b}.c.{d}.e"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)
	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

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

	value, err := asp.Get(databag, "a")
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

func (s *aspectSuite) TestGetMergeAtDifferentLevels(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.{b}.c.{d}.e", "storage": "a.{b}.c.{d}.e"},
				map[string]interface{}{"request": "a.{b}.c.{d}", "storage": "a.{b}.c.{d}"},
				map[string]interface{}{"request": "a.{b}", "storage": "a.{b}"},
				map[string]interface{}{"request": "a", "storage": "a"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)
	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

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

	value, err := asp.Get(databag, "a")
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

func (s *aspectSuite) TestBadRequestPaths(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.{b}.c", "storage": "a.{b}.c"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

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
	}

	for _, tc := range tcs {
		cmt := Commentf("test %q failed", tc.request)
		err = asp.Set(databag, tc.request, "value")
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot set %q in aspect acc/bundle/foo: %s`, tc.request, tc.errMsg), cmt)

		_, err = asp.Get(databag, tc.request)
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot get %q in aspect acc/bundle/foo: %s`, tc.request, tc.errMsg), cmt)

		err = asp.Unset(databag, tc.request)
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot unset %q in aspect acc/bundle/foo: %s`, tc.request, tc.errMsg), cmt)
	}
}

func (s *aspectSuite) TestSetAllowedOnSameRequestButDifferentPaths(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.b.c", "storage": "new", "access": "write"},
				map[string]interface{}{"request": "a.b.c", "storage": "old", "access": "write"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)
	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	err = asp.Set(databag, "a.b.c", "value")
	c.Assert(err, IsNil)

	stored, err := databag.Get("old")
	c.Assert(err, IsNil)
	c.Assert(stored, Equals, "value")

	stored, err = databag.Get("new")
	c.Assert(err, IsNil)
	c.Assert(stored, Equals, "value")
}

func (s *aspectSuite) TestSetWritesToMoreNestedLast(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		// purposefully unordered to check that Set doesn't depend on well-ordered entries in assertions
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.snapd.name", "storage": "snaps.snapd.name"},
				map[string]interface{}{"request": "snaps.snapd", "storage": "snaps.snapd"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	err = asp.Set(databag, "snaps.snapd", map[string]interface{}{
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

func (s *aspectSuite) TestReadWriteRead(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.b.c", "storage": "a.b.c"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	initData := map[string]interface{}{
		"b": map[string]interface{}{
			"c": "end",
		},
	}
	err = databag.Set("a", initData)
	c.Assert(err, IsNil)

	data, err := asp.Get(databag, "a")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, initData)

	err = asp.Set(databag, "a", data)
	c.Assert(err, IsNil)

	data, err = asp.Get(databag, "a")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, initData)
}

func (s *aspectSuite) TestReadWriteSameDataAtDifferentLevels(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.b.c", "storage": "a.b.c"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)
	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	initialData := map[string]interface{}{
		"b": map[string]interface{}{
			"c": "end",
		},
	}
	err = databag.Set("a", initialData)
	c.Assert(err, IsNil)

	for _, req := range []string{"a", "a.b", "a.b.c"} {
		val, err := asp.Get(databag, req)
		c.Assert(err, IsNil)

		err = asp.Set(databag, req, val)
		c.Assert(err, IsNil)
	}

	data, err := databag.Get("a")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, initialData)
}

func (s *aspectSuite) TestSetValueMissingNestedLevels(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.b", "storage": "a.b"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)
	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	err = asp.Set(databag, "a", "foo")
	c.Assert(err, ErrorMatches, `cannot set "a" in aspect acc/bundle/foo: expected map for unmatched request parts but got string`)

	err = asp.Set(databag, "a", map[string]interface{}{"c": "foo"})
	c.Assert(err, ErrorMatches, `cannot set "a" in aspect acc/bundle/foo: cannot use unmatched part "b" as key in map\[c:foo\]`)
}

func (s *aspectSuite) TestGetReadsStorageLessNestedNamespaceBefore(c *C) {
	// Get reads by order of namespace (not path) nestedness. This test explicitly
	// tests for this and showcases why it matters. In Get we care about building
	// a virtual document from locations in the storage that may evolve over time.
	// In this example, the storage evolve to have version data in a different place
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.snapd", "storage": "snaps.snapd"},
				map[string]interface{}{"request": "snaps.snapd.version", "storage": "anewversion"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set("snaps", map[string]interface{}{
		"snapd": map[string]interface{}{
			"version": 1,
		},
	})
	c.Assert(err, IsNil)

	err = databag.Set("anewversion", 2)
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	data, err := asp.Get(databag, "snaps")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]interface{}{
		"snapd": map[string]interface{}{
			"version": float64(2),
		},
	})
}

func (s *aspectSuite) TestSetValidateError(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "bar", "storage": "bar"},
			},
		},
	}, &failingSchema{err: errors.New("expected error")})
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	err = asp.Set(databag, "bar", "baz")
	c.Assert(err, ErrorMatches, "cannot write data: expected error")
}

func (s *aspectSuite) TestSetOverwriteValueWithNewLevel(c *C) {
	databag := aspects.NewJSONDataBag()
	err := databag.Set("foo", "bar")
	c.Assert(err, IsNil)

	err = databag.Set("foo.bar", "baz")
	c.Assert(err, IsNil)

	data, err := databag.Get("foo")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]interface{}{"bar": "baz"})
}

func (s *aspectSuite) TestSetValidatesDataWithSchemaPass(c *C) {
	schema, err := aspects.ParseSchema([]byte(`{
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

	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "bar"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	err = asp.Set(databag, "foo", map[string]int{"a": 1, "b": 2})
	c.Assert(err, IsNil)

	err = asp.Set(databag, "bar", []string{"one", "two"})
	c.Assert(err, IsNil)
}

func (s *aspectSuite) TestSetPreCheckValueFailsIncompatibleTypes(c *C) {
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

			schema, err := aspects.ParseSchema([]byte(fmt.Sprintf(`{
	"schema": {
		"foo": %s,
		"bar": %s
	}
}`, one.schemaStr, other.schemaStr)))
			c.Assert(err, IsNil)

			_, err = aspects.NewBundle("acc", "bundle", map[string]interface{}{
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

func (s *aspectSuite) TestSetPreCheckValueAllowsIntNumberMismatch(c *C) {
	schema, err := aspects.ParseSchema([]byte(`{
	"schema": {
		"foo": "int",
		"bar": "number"
	}
}`))
	c.Assert(err, IsNil)

	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo", "access": "write"},
				map[string]interface{}{"request": "foo", "storage": "bar", "access": "write"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	err = asp.Set(databag, "foo", 1)
	c.Assert(err, IsNil)

	// the schema still checks the data at the end, so setting int schema to a float fails
	err = asp.Set(databag, "foo", 1.1)
	c.Assert(err, ErrorMatches, `.*cannot accept element in "foo": expected int type but value was number 1.1`)
}

func (*aspectSuite) TestSetPreCheckMultipleAlternativeTypesFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["int", "bool"],
		"bar": ["string", {"type": "array", "values": "string"}, {"schema": {"baz":"string"}}]
	}
}`)
	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	_, err = aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo", "access": "write"},
				map[string]interface{}{"request": "foo", "storage": "bar", "access": "write"},
			},
		},
	}, schema)
	c.Assert(err, ErrorMatches, `.*storage paths "foo" and "bar" for request "foo" require incompatible types: \[int, bool\] != \[string, array, map\]`)
}

func (*aspectSuite) TestAssertionRuleSchemaMismatch(c *C) {
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
	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo.b.c", "access": "write"},
				map[string]interface{}{"request": "foo", "storage": "bar.b.c", "access": "write"},
			},
		},
	}, schema)
	c.Assert(err, ErrorMatches, `.*storage path "foo.b.c" for request "foo" is invalid after "foo": cannot follow path beyond "int" type`)
	c.Assert(aspectBundle, IsNil)
}

func (*aspectSuite) TestSchemaMismatchCheckDifferentLevelPaths(c *C) {
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
	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	_, err = aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.{snap}", "storage": "snaps.{snap}"},
				map[string]interface{}{"request": "snaps.{snap}.status", "storage": "snaps.{snap}.status"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)
}

func (*aspectSuite) TestSchemaMismatchCheckMultipleAlternativeTypesHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["int", "bool"],
		"bar": ["string", "bool"]
	}
}`)
	schema, err := aspects.ParseSchema(schemaStr)
	c.Assert(err, IsNil)

	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo", "access": "write"},
				map[string]interface{}{"request": "foo", "storage": "bar", "access": "write"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	err = asp.Set(databag, "foo", true)
	c.Assert(err, IsNil)
}

func (s *aspectSuite) TestSetUnmatchedPlaceholderLeaf(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}", "storage": "foo.{bar}"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	err = asp.Set(databag, "foo", map[string]interface{}{
		"bar": "value",
		"baz": "other",
	})
	c.Assert(err, IsNil)

	data, err := asp.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]interface{}{
		"bar": "value",
		"baz": "other",
	})
}

func (s *aspectSuite) TestSetUnmatchedPlaceholderMidPath(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}.nested", "storage": "foo.{bar}.nested"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	err = asp.Set(databag, "foo", map[string]interface{}{
		"bar": map[string]interface{}{"nested": "value"},
		"baz": map[string]interface{}{"nested": "other"},
	})
	c.Assert(err, IsNil)

	data, err := asp.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]interface{}{
		"bar": map[string]interface{}{"nested": "value"},
		"baz": map[string]interface{}{"nested": "other"},
	})
}

func (s *aspectSuite) TestSetManyUnmatchedPlaceholders(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}.a.{baz}", "storage": "foo.{bar}.{baz}"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	err = asp.Set(databag, "foo", map[string]interface{}{
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

	data, err := asp.Get(databag, "foo")
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

func (s *aspectSuite) TestUnsetUnmatchedPlaceholder(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}", "storage": "foo.{bar}"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	err = asp.Set(databag, "foo", map[string]interface{}{
		"bar": "value",
		"baz": "other",
	})
	c.Assert(err, IsNil)

	err = asp.Unset(databag, "foo")
	c.Assert(err, ErrorMatches, `cannot unset "foo" in aspect acc/bundle/foo: cannot unset with unmatched placeholders`)
}

func (s *aspectSuite) TestGetValuesThroughPaths(c *C) {
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
		pathsToValues, err := aspects.GetValuesThroughPaths(tc.path, tc.suffix, tc.value)

		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err, cmt)
			c.Check(pathsToValues, IsNil, cmt)
		} else {
			c.Check(err, IsNil, cmt)
			c.Check(pathsToValues, DeepEquals, tc.expected, cmt)
		}
	}
}

func (s *aspectSuite) TestAspectSetErrorIfValueContainsUnusedParts(c *C) {
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
			err: `cannot set "a" in aspect acc/bundle/foo: value contains unused data under "b.u"`,
		},
		{
			request: "a",
			value: map[string]interface{}{
				"b": map[string]interface{}{"d": "value", "u": 1},
				"c": map[string]interface{}{"d": "value"},
			},
			err: `cannot set "a" in aspect acc/bundle/foo: value contains unused data under "b.u"`,
		},
		{
			request: "b",
			value: map[string]interface{}{
				"e": []interface{}{"a"},
				"f": 1,
			},
			err: `cannot set "b" in aspect acc/bundle/foo: value contains unused data under "e"`,
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
			err: `cannot set "c" in aspect acc/bundle/foo: value contains unused data under "d.f"`,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("failed test number %d", i+1)
		databag := aspects.NewJSONDataBag()
		aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "a.{x}.d", "storage": "a.{x}"},
					map[string]interface{}{"request": "c.d.e.f", "storage": "d"},
					map[string]interface{}{"request": "b.f", "storage": "b.f"},
				},
			},
		}, aspects.NewJSONSchema())
		c.Assert(err, IsNil)

		asp := aspectBundle.Aspect("foo")
		c.Assert(asp, NotNil)

		err = asp.Set(databag, tc.request, tc.value)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err, cmt)
		} else {
			c.Check(err, IsNil, cmt)
		}
	}
}

func (*aspectSuite) TestAspectSummaryWrongType(c *C) {
	for _, val := range []interface{}{
		1,
		true,
		[]interface{}{"foo"},
		map[string]interface{}{"foo": "bar"},
	} {
		bundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
			"foo": map[string]interface{}{
				"summary": val,
				"rules": []interface{}{
					map[string]interface{}{"request": "foo", "storage": "foo"},
				},
			},
		}, nil)
		c.Check(err.Error(), Equals, fmt.Sprintf(`cannot define aspect "foo": aspect summary must be a string but got %T`, val))
		c.Check(bundle, IsNil)
	}
}

func (*aspectSuite) TestAspectSummary(c *C) {
	bundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"summary": "some summary of the aspect",
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)
	c.Assert(bundle, NotNil)
}

func (s *aspectSuite) TestGetEntireAspect(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}", "storage": "foo-path.{bar}"},
				map[string]interface{}{"request": "abc", "storage": "abc-path"},
				map[string]interface{}{"request": "write-only", "storage": "write-only", "access": "write"},
			},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	err = asp.Set(databag, "foo", map[string]interface{}{
		"bar": "value",
		"baz": "other",
	})
	c.Assert(err, IsNil)

	err = asp.Set(databag, "abc", "cba")
	c.Assert(err, IsNil)

	err = asp.Set(databag, "write-only", "value")
	c.Assert(err, IsNil)

	result, err := asp.Get(databag, "")
	c.Assert(err, IsNil)

	c.Assert(result, DeepEquals, map[string]interface{}{
		"foo": map[string]interface{}{
			"bar": "value",
			"baz": "other",
		},
		"abc": "cba",
	})
}
