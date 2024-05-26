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

	"github.com/ddkwork/golibrary/mylog"
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
			err:    `cannot define aspect "bar": aspect rules must have a "storage" field`,
		},

		{
			bundle: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"request": "foo", "storage": 1}}}},
			err:    `cannot define aspect "bar": "storage" must be a string`,
		},
		{
			bundle: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"storage": "foo", "request": 1}}}},
			err:    `cannot define aspect "bar": "request" must be a string`,
		},
		{
			bundle: map[string]interface{}{"bar": map[string]interface{}{"rules": []interface{}{map[string]interface{}{"storage": "foo", "request": ""}}}},
			err:    `cannot define aspect "bar": aspect rules' "request" field must be non-empty, if it exists`,
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
		aspectBundle := mylog.Check2(aspects.NewBundle("acc", "foo", tc.bundle, aspects.NewJSONSchema()))
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Check(aspectBundle, Not(IsNil), cmt)
		}
	}
}

func (s *aspectSuite) TestMissingRequestDefaultsToStorage(c *C) {
	databag := aspects.NewJSONDataBag()
	bundle := map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"storage": "a.b"},
			},
		},
	}
	bun := mylog.Check2(aspects.NewBundle("acc", "foo", bundle, aspects.NewJSONSchema()))


	asp := bun.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "a.b", "value"))


	value := mylog.Check2(asp.Get(databag, ""))

	c.Assert(value, DeepEquals, map[string]interface{}{
		"a": map[string]interface{}{
			"b": "value",
		},
	})
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
	_ := mylog.Check2(aspects.NewBundle("acc", "foo", bundle, aspects.NewJSONSchema()))

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
		aspectBundle := mylog.Check2(aspects.NewBundle("acc", "foo", map[string]interface{}{
			"bar": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "a", "storage": "b", "access": t.access},
				},
			},
		}, aspects.NewJSONSchema()))

		cmt := Commentf("\"%s access\" sub-test failed", t.access)
		if t.err {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`.*expected 'access' to be either "read-write", "read", "write" or empty but was %q`, t.access), cmt)
			c.Check(aspectBundle, IsNil, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Check(aspectBundle, Not(IsNil), cmt)
		}
	}
}

func (*aspectSuite) TestGetAndSetAspects(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("system", "network", map[string]interface{}{
		"wifi-setup": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "ssids", "storage": "wifi.ssids"},
				map[string]interface{}{"request": "ssid", "storage": "wifi.ssid"},
				map[string]interface{}{"request": "top-level", "storage": "top-level"},
				map[string]interface{}{"request": "dotted.path", "storage": "dotted"},
			},
		},
	}, aspects.NewJSONSchema()))


	wsAspect := aspectBundle.Aspect("wifi-setup")
	mylog.

		// nested string value
		Check(wsAspect.Set(databag, "ssid", "my-ssid"))


	ssid := mylog.Check2(wsAspect.Get(databag, "ssid"))

	c.Check(ssid, DeepEquals, "my-ssid")
	mylog.

		// nested list value
		Check(wsAspect.Set(databag, "ssids", []string{"one", "two"}))


	ssids := mylog.Check2(wsAspect.Get(databag, "ssids"))

	c.Check(ssids, DeepEquals, []interface{}{"one", "two"})
	mylog.

		// top-level string
		Check(wsAspect.Set(databag, "top-level", "randomValue"))


	topLevel := mylog.Check2(wsAspect.Get(databag, "top-level"))

	c.Check(topLevel, DeepEquals, "randomValue")
	mylog.

		// dotted request paths are permitted
		Check(wsAspect.Set(databag, "dotted.path", 3))


	num := mylog.Check2(wsAspect.Get(databag, "dotted.path"))

	c.Check(num, DeepEquals, float64(3))
}

func (*aspectSuite) TestSetWithNilValueFail(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("system", "test", map[string]interface{}{
		"test": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, aspects.NewJSONSchema()))


	wsAspect := aspectBundle.Aspect("test")
	mylog.Check(wsAspect.Set(databag, "foo", "value"))

	mylog.Check(wsAspect.Set(databag, "foo", nil))
	c.Assert(err, ErrorMatches, `internal error: Set value cannot be nil`)

	ssid := mylog.Check2(wsAspect.Get(databag, "foo"))

	c.Check(ssid, DeepEquals, "value")
}

func (s *aspectSuite) TestAspectNotFound(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "foo", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "top-level", "storage": "top-level"},
				map[string]interface{}{"request": "nested", "storage": "top.nested-one"},
				map[string]interface{}{"request": "other-nested", "storage": "top.nested-two"},
			},
		},
	}, aspects.NewJSONSchema()))


	aspect := aspectBundle.Aspect("bar")

	_ = mylog.Check2(aspect.Get(databag, "missing"))
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "missing" in aspect acc/foo/bar: no matching read rule`)
	mylog.Check(aspect.Set(databag, "missing", "thing"))
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot set "missing" in aspect acc/foo/bar: no matching write rule`)
	mylog.Check(aspect.Unset(databag, "missing"))
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot unset "missing" in aspect acc/foo/bar: no matching write rule`)

	_ = mylog.Check2(aspect.Get(databag, "top-level"))
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "top-level" in aspect acc/foo/bar: matching rules don't map to any values`)
	mylog.Check(aspect.Set(databag, "nested", "thing"))

	mylog.Check(aspect.Unset(databag, "nested"))


	_ = mylog.Check2(aspect.Get(databag, "other-nested"))
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "other-nested" in aspect acc/foo/bar: matching rules don't map to any values`)
}

func (s *aspectSuite) TestAspectBadRead(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "foo", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "one", "storage": "one"},
				map[string]interface{}{"request": "onetwo", "storage": "one.two"},
			},
		},
	}, aspects.NewJSONSchema()))


	aspect := aspectBundle.Aspect("bar")
	mylog.Check(aspect.Set(databag, "one", "foo"))


	_ = mylog.Check2(aspect.Get(databag, "onetwo"))
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
		aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "foo", "storage": "foo", "access": t.access},
				},
			},
		}, aspects.NewJSONSchema()))


		aspect := aspectBundle.Aspect("foo")
		mylog.Check(aspect.Set(databag, "foo", "thing"))
		if t.setErr != "" {
			c.Assert(err.Error(), Equals, t.setErr, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
		}

		_ = mylog.Check2(aspect.Get(databag, "foo"))
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

		aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{t.rule},
			},
		}, aspects.NewJSONSchema()))

		aspect := aspectBundle.Aspect("foo")

		databag := newWitnessDataBag(aspects.NewJSONDataBag())
		mylog.Check(aspect.Set(databag, t.request, "expectedValue"))
		c.Assert(err, IsNil, cmt)

		value := mylog.Check2(aspect.Get(databag, t.request))
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
		_ := mylog.Check2(aspects.NewBundle("acc", "foo", map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": tc.request, "storage": tc.storage},
				},
			},
		}, aspects.NewJSONSchema()))

		cmt := Commentf("sub-test %q failed", tc.testName)
		c.Assert(err, Not(IsNil), cmt)
		c.Assert(err.Error(), Equals, `cannot define aspect "foo": `+tc.err, cmt)
	}
}

func (s *aspectSuite) TestAspectUnsetTopLevelEntry(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "foo", map[string]interface{}{
		"my-aspect": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "bar"},
			},
		},
	}, aspects.NewJSONSchema()))


	aspect := aspectBundle.Aspect("my-aspect")
	mylog.Check(aspect.Set(databag, "foo", "fval"))

	mylog.Check(aspect.Set(databag, "bar", "bval"))

	mylog.Check(aspect.Unset(databag, "foo"))


	_ = mylog.Check2(aspect.Get(databag, "foo"))
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})

	value := mylog.Check2(aspect.Get(databag, "bar"))

	c.Assert(value, DeepEquals, "bval")
}

func (s *aspectSuite) TestAspectUnsetLeafWithSiblings(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "foo", map[string]interface{}{
		"my-aspect": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "bar", "storage": "foo.bar"},
				map[string]interface{}{"request": "baz", "storage": "foo.baz"},
			},
		},
	}, aspects.NewJSONSchema()))


	aspect := aspectBundle.Aspect("my-aspect")
	mylog.Check(aspect.Set(databag, "bar", "barVal"))

	mylog.Check(aspect.Set(databag, "baz", "bazVal"))

	mylog.Check(aspect.Unset(databag, "bar"))


	_ = mylog.Check2(aspect.Get(databag, "bar"))
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})

	// doesn't affect the other leaf entry under "foo"
	value := mylog.Check2(aspect.Get(databag, "baz"))

	c.Assert(value, DeepEquals, "bazVal")
}

func (s *aspectSuite) TestAspectUnsetWithNestedEntry(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "foo", map[string]interface{}{
		"my-aspect": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "foo.bar"},
			},
		},
	}, aspects.NewJSONSchema()))


	aspect := aspectBundle.Aspect("my-aspect")
	mylog.Check(aspect.Set(databag, "bar", "barVal"))

	mylog.Check(aspect.Unset(databag, "foo"))


	_ = mylog.Check2(aspect.Get(databag, "foo"))
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})

	_ = mylog.Check2(aspect.Get(databag, "bar"))
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
}

func (s *aspectSuite) TestAspectUnsetLeafLeavesEmptyParent(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "foo", map[string]interface{}{
		"my-aspect": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "foo.bar"},
			},
		},
	}, aspects.NewJSONSchema()))


	aspect := aspectBundle.Aspect("my-aspect")
	mylog.Check(aspect.Set(databag, "bar", "val"))


	value := mylog.Check2(aspect.Get(databag, "foo"))

	c.Assert(value, Not(HasLen), 0)
	mylog.Check(aspect.Unset(databag, "bar"))


	value = mylog.Check2(aspect.Get(databag, "foo"))

	c.Assert(value, DeepEquals, map[string]interface{}{})
}

func (s *aspectSuite) TestAspectUnsetAlreadyUnsetEntry(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "foo", map[string]interface{}{
		"my-aspect": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "one.bar"},
			},
		},
	}, aspects.NewJSONSchema()))


	aspect := aspectBundle.Aspect("my-aspect")
	mylog.Check(aspect.Unset(databag, "foo"))

	mylog.Check(aspect.Unset(databag, "bar"))

}

func (s *aspectSuite) TestJSONDataBagCopy(c *C) {
	bag := aspects.NewJSONDataBag()
	mylog.Check(bag.Set("foo", "bar"))


	// precondition check
	data := mylog.Check2(bag.Data())

	c.Assert(string(data), Equals, `{"foo":"bar"}`)

	bagCopy := bag.Copy()
	data = mylog.Check2(bagCopy.Data())

	c.Assert(string(data), Equals, `{"foo":"bar"}`)
	mylog.

		// changes in the copied bag don't affect the original
		Check(bagCopy.Set("foo", "baz"))


	data = mylog.Check2(bag.Data())

	c.Assert(string(data), Equals, `{"foo":"bar"}`)
	mylog.

		// and vice-versa
		Check(bag.Set("foo", "zab"))


	data = mylog.Check2(bagCopy.Data())

	c.Assert(string(data), Equals, `{"foo":"baz"}`)
}

func (s *aspectSuite) TestAspectGetResultNamespaceMatchesRequest(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "one", "storage": "one"},
				map[string]interface{}{"request": "one.two", "storage": "one.two"},
				map[string]interface{}{"request": "onetwo", "storage": "one.two"},
			},
		},
	}, aspects.NewJSONSchema()))


	aspect := aspectBundle.Aspect("bar")
	mylog.Check(databag.Set("one", map[string]interface{}{"two": "value"}))


	value := mylog.Check2(aspect.Get(databag, "one.two"))

	c.Assert(value, DeepEquals, "value")

	value = mylog.Check2(aspect.Get(databag, "onetwo"))

	// the key matches the request, not the storage storage
	c.Assert(value, DeepEquals, "value")

	value = mylog.Check2(aspect.Get(databag, "one"))

	c.Assert(value, DeepEquals, map[string]interface{}{"two": "value"})
}

func (s *aspectSuite) TestAspectGetMatchesOnPrefix(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snapd.status", "storage": "snaps.snapd.status"},
				map[string]interface{}{"request": "snaps", "storage": "snaps"},
			},
		},
	}, aspects.NewJSONSchema()))


	aspect := aspectBundle.Aspect("statuses")
	mylog.Check(aspect.Set(databag, "snaps", map[string]map[string]interface{}{
		"snapd":   {"status": "active", "version": "1.0"},
		"firefox": {"status": "inactive", "version": "9.0"},
	}))


	value := mylog.Check2(aspect.Get(databag, "snapd.status"))

	c.Assert(value, DeepEquals, "active")

	value = mylog.Check2(aspect.Get(databag, "snapd"))

	c.Assert(value, DeepEquals, map[string]interface{}{"status": "active"})
}

func (s *aspectSuite) TestAspectUnsetValidates(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"test": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, &failingSchema{err: errors.New("boom")}))


	aspect := aspectBundle.Aspect("test")
	mylog.Check(aspect.Unset(databag, "foo"))
	c.Assert(err, ErrorMatches, `cannot unset data: boom`)
}

func (s *aspectSuite) TestAspectUnsetSkipsReadOnly(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"test": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo", "access": "read"},
			},
		},
	}, aspects.NewJSONSchema()))


	aspect := aspectBundle.Aspect("test")
	mylog.Check(aspect.Unset(databag, "foo"))
	c.Assert(err, ErrorMatches, `cannot unset "foo" in aspect acc/bundle/test: no matching write rule`)
}

func (s *aspectSuite) TestAspectGetNoMatchRequestLongerThanPattern(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snapd", "storage": "snaps.snapd"},
			},
		},
	}, aspects.NewJSONSchema()))


	aspect := aspectBundle.Aspect("statuses")
	mylog.Check(aspect.Set(databag, "snapd", map[string]interface{}{
		"status": "active", "version": "1.0",
	}))


	_ = mylog.Check2(aspect.Get(databag, "snapd.status"))
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
}

func (s *aspectSuite) TestAspectManyPrefixMatches(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "status.firefox", "storage": "snaps.firefox.status"},
				map[string]interface{}{"request": "status.snapd", "storage": "snaps.snapd.status"},
			},
		},
	}, aspects.NewJSONSchema()))


	aspect := aspectBundle.Aspect("statuses")
	mylog.Check(aspect.Set(databag, "status.firefox", "active"))

	mylog.Check(aspect.Set(databag, "status.snapd", "disabled"))


	value := mylog.Check2(aspect.Get(databag, "status"))

	c.Assert(value, DeepEquals,
		map[string]interface{}{
			"snapd":   "disabled",
			"firefox": "active",
		})
}

func (s *aspectSuite) TestAspectCombineNamespacesInPrefixMatches(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "status.foo.bar.firefox", "storage": "snaps.firefox.status"},
				map[string]interface{}{"request": "status.foo.snapd", "storage": "snaps.snapd.status"},
			},
		},
	}, aspects.NewJSONSchema()))

	mylog.Check(databag.Set("snaps", map[string]interface{}{
		"firefox": map[string]interface{}{
			"status": "active",
		},
		"snapd": map[string]interface{}{
			"status": "disabled",
		},
	}))


	aspect := aspectBundle.Aspect("statuses")

	value := mylog.Check2(aspect.Get(databag, "status"))

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
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"motors": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "motors.a.speed", "storage": "new-speed.a"},
				map[string]interface{}{"request": "motors", "storage": "motors"},
			},
		},
	}, aspects.NewJSONSchema()))

	mylog.Check(databag.Set("motors", map[string]interface{}{
		"a": map[string]interface{}{
			"speed": 100,
		},
	}))

	mylog.Check(databag.Set("new-speed", map[string]interface{}{
		"a": 101.5,
	}))


	aspect := aspectBundle.Aspect("motors")

	value := mylog.Check2(aspect.Get(databag, "motors"))

	c.Assert(value, DeepEquals, map[string]interface{}{"a": map[string]interface{}{"speed": 101.5}})
}

func (s *aspectSuite) TestGetSingleScalarOk(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, aspects.NewJSONSchema()))

	mylog.Check(databag.Set("foo", "bar"))


	aspect := aspectBundle.Aspect("foo")

	value := mylog.Check2(aspect.Get(databag, "foo"))

	c.Assert(value, DeepEquals, "bar")
}

func (s *aspectSuite) TestGetMatchScalarAndMapError(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "bar"},
				map[string]interface{}{"request": "foo.baz", "storage": "baz"},
			},
		},
	}, aspects.NewJSONSchema()))

	mylog.Check(databag.Set("bar", 1))

	mylog.Check(databag.Set("baz", 2))


	aspect := aspectBundle.Aspect("foo")

	_ = mylog.Check2(aspect.Get(databag, "foo"))
	c.Assert(err, ErrorMatches, `cannot merge results of different types float64, map\[string\]interface {}`)
}

func (s *aspectSuite) TestGetRulesAreSortedByParentage(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.bar.baz", "storage": "third"},
				map[string]interface{}{"request": "foo", "storage": "first"},
				map[string]interface{}{"request": "foo.bar", "storage": "second"},
			},
		},
	}, aspects.NewJSONSchema()))

	aspect := aspectBundle.Aspect("foo")
	mylog.Check(databag.Set("first", map[string]interface{}{"bar": map[string]interface{}{"baz": "first"}}))


	value := mylog.Check2(aspect.Get(databag, "foo"))

	// returned the value read by entry "foo"
	c.Assert(value, DeepEquals, map[string]interface{}{"bar": map[string]interface{}{"baz": "first"}})
	mylog.Check(databag.Set("second", map[string]interface{}{"baz": "second"}))


	value = mylog.Check2(aspect.Get(databag, "foo"))

	// the leaf is replaced by a value read from a rule that is nested
	c.Assert(value, DeepEquals, map[string]interface{}{"bar": map[string]interface{}{"baz": "second"}})
	mylog.Check(databag.Set("third", "third"))


	value = mylog.Check2(aspect.Get(databag, "foo"))

	// lastly, it reads the value from "foo.bar.baz" the most nested entry
	c.Assert(value, DeepEquals, map[string]interface{}{"bar": map[string]interface{}{"baz": "third"}})
}

func (s *aspectSuite) TestGetUnmatchedPlaceholderReturnsAll(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"snaps": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.{snap}", "storage": "snaps.{snap}"},
			},
		},
	}, aspects.NewJSONSchema()))

	aspect := aspectBundle.Aspect("snaps")
	c.Assert(aspect, NotNil)
	mylog.Check(databag.Set("snaps", map[string]interface{}{
		"snapd": 1,
		"foo": map[string]interface{}{
			"bar": 2,
		},
	}))


	value := mylog.Check2(aspect.Get(databag, "snaps"))

	c.Assert(value, DeepEquals, map[string]interface{}{"snapd": float64(1), "foo": map[string]interface{}{"bar": float64(2)}})
}

func (s *aspectSuite) TestGetUnmatchedPlaceholdersWithNestedValues(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"statuses": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.{snap}.status", "storage": "snaps.{snap}.status"},
			},
		},
	}, aspects.NewJSONSchema()))

	asp := aspectBundle.Aspect("statuses")
	c.Assert(asp, NotNil)
	mylog.Check(databag.Set("snaps", map[string]interface{}{
		"snapd": map[string]interface{}{
			"status": "active",
		},
		"foo": map[string]interface{}{
			"version": 2,
		},
	}))


	value := mylog.Check2(asp.Get(databag, "snaps"))

	c.Assert(value, DeepEquals, map[string]interface{}{"snapd": map[string]interface{}{"status": "active"}})
}

func (s *aspectSuite) TestGetSeveralUnmatchedPlaceholders(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.{b}.c.{d}.e", "storage": "a.{b}.c.{d}.e"},
			},
		},
	}, aspects.NewJSONSchema()))

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(databag.Set("a", map[string]interface{}{
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
	}))


	value := mylog.Check2(asp.Get(databag, "a"))

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
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.{b}.c.{d}.e", "storage": "a.{b}.c.{d}.e"},
				map[string]interface{}{"request": "a.{b}.c.{d}", "storage": "a.{b}.c.{d}"},
				map[string]interface{}{"request": "a.{b}", "storage": "a.{b}"},
				map[string]interface{}{"request": "a", "storage": "a"},
			},
		},
	}, aspects.NewJSONSchema()))

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(databag.Set("a", map[string]interface{}{
		"b": map[string]interface{}{
			"c": map[string]interface{}{
				"d": map[string]interface{}{
					"e": "end",
				},
			},
		},
	}))


	value := mylog.Check2(asp.Get(databag, "a"))

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
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.{b}.c", "storage": "a.{b}.c"},
			},
		},
	}, aspects.NewJSONSchema()))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(databag.Set("a", map[string]interface{}{
		"b": map[string]interface{}{
			"c": "value",
		},
	}))


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
		mylog.Check(asp.Set(databag, tc.request, "value"))
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot set %q in aspect acc/bundle/foo: %s`, tc.request, tc.errMsg), cmt)

		_ = mylog.Check2(asp.Get(databag, tc.request))
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot get %q in aspect acc/bundle/foo: %s`, tc.request, tc.errMsg), cmt)
		mylog.Check(asp.Unset(databag, tc.request))
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot unset %q in aspect acc/bundle/foo: %s`, tc.request, tc.errMsg), cmt)
	}
}

func (s *aspectSuite) TestSetAllowedOnSameRequestButDifferentPaths(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.b.c", "storage": "new", "access": "write"},
				map[string]interface{}{"request": "a.b.c", "storage": "old", "access": "write"},
			},
		},
	}, aspects.NewJSONSchema()))

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "a.b.c", "value"))


	stored := mylog.Check2(databag.Get("old"))

	c.Assert(stored, Equals, "value")

	stored = mylog.Check2(databag.Get("new"))

	c.Assert(stored, Equals, "value")
}

func (s *aspectSuite) TestSetWritesToMoreNestedLast(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		// purposefully unordered to check that Set doesn't depend on well-ordered entries in assertions
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.snapd.name", "storage": "snaps.snapd.name"},
				map[string]interface{}{"request": "snaps.snapd", "storage": "snaps.snapd"},
			},
		},
	}, aspects.NewJSONSchema()))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "snaps.snapd", map[string]interface{}{
		"name": "snapd",
	}))


	val := mylog.Check2(databag.Get("snaps"))


	c.Assert(val, DeepEquals, map[string]interface{}{
		"snapd": map[string]interface{}{
			"name": "snapd",
		},
	})
}

func (s *aspectSuite) TestReadWriteRead(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.b.c", "storage": "a.b.c"},
			},
		},
	}, aspects.NewJSONSchema()))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	initData := map[string]interface{}{
		"b": map[string]interface{}{
			"c": "end",
		},
	}
	mylog.Check(databag.Set("a", initData))


	data := mylog.Check2(asp.Get(databag, "a"))

	c.Assert(data, DeepEquals, initData)
	mylog.Check(asp.Set(databag, "a", data))


	data = mylog.Check2(asp.Get(databag, "a"))

	c.Assert(data, DeepEquals, initData)
}

func (s *aspectSuite) TestReadWriteSameDataAtDifferentLevels(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.b.c", "storage": "a.b.c"},
			},
		},
	}, aspects.NewJSONSchema()))

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	initialData := map[string]interface{}{
		"b": map[string]interface{}{
			"c": "end",
		},
	}
	mylog.Check(databag.Set("a", initialData))


	for _, req := range []string{"a", "a.b", "a.b.c"} {
		val := mylog.Check2(asp.Get(databag, req))

		mylog.Check(asp.Set(databag, req, val))

	}

	data := mylog.Check2(databag.Get("a"))

	c.Assert(data, DeepEquals, initialData)
}

func (s *aspectSuite) TestSetValueMissingNestedLevels(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "a.b", "storage": "a.b"},
			},
		},
	}, aspects.NewJSONSchema()))

	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "a", "foo"))
	c.Assert(err, ErrorMatches, `cannot set "a" in aspect acc/bundle/foo: expected map for unmatched request parts but got string`)
	mylog.Check(asp.Set(databag, "a", map[string]interface{}{"c": "foo"}))
	c.Assert(err, ErrorMatches, `cannot set "a" in aspect acc/bundle/foo: cannot use unmatched part "b" as key in map\[c:foo\]`)
}

func (s *aspectSuite) TestGetReadsStorageLessNestedNamespaceBefore(c *C) {
	// Get reads by order of namespace (not path) nestedness. This test explicitly
	// tests for this and showcases why it matters. In Get we care about building
	// a virtual document from locations in the storage that may evolve over time.
	// In this example, the storage evolve to have version data in a different place
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.snapd", "storage": "snaps.snapd"},
				map[string]interface{}{"request": "snaps.snapd.version", "storage": "anewversion"},
			},
		},
	}, aspects.NewJSONSchema()))

	mylog.Check(databag.Set("snaps", map[string]interface{}{
		"snapd": map[string]interface{}{
			"version": 1,
		},
	}))

	mylog.Check(databag.Set("anewversion", 2))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)

	data := mylog.Check2(asp.Get(databag, "snaps"))

	c.Assert(data, DeepEquals, map[string]interface{}{
		"snapd": map[string]interface{}{
			"version": float64(2),
		},
	})
}

func (s *aspectSuite) TestSetValidateError(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "bar", "storage": "bar"},
			},
		},
	}, &failingSchema{err: errors.New("expected error")}))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "bar", "baz"))
	c.Assert(err, ErrorMatches, "cannot write data: expected error")
}

func (s *aspectSuite) TestSetOverwriteValueWithNewLevel(c *C) {
	databag := aspects.NewJSONDataBag()
	mylog.Check(databag.Set("foo", "bar"))

	mylog.Check(databag.Set("foo.bar", "baz"))


	data := mylog.Check2(databag.Get("foo"))

	c.Assert(data, DeepEquals, map[string]interface{}{"bar": "baz"})
}

func (s *aspectSuite) TestSetValidatesDataWithSchemaPass(c *C) {
	schema := mylog.Check2(aspects.ParseSchema([]byte(`{
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
}`)))


	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "bar", "storage": "bar"},
			},
		},
	}, schema))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "foo", map[string]int{"a": 1, "b": 2}))

	mylog.Check(asp.Set(databag, "bar", []string{"one", "two"}))

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

			schema := mylog.Check2(aspects.ParseSchema([]byte(fmt.Sprintf(`{
	"schema": {
		"foo": %s,
		"bar": %s
	}
}`, one.schemaStr, other.schemaStr))))


			_ = mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
				"foo": map[string]interface{}{
					"rules": []interface{}{
						map[string]interface{}{"request": "foo", "storage": "foo", "access": "write"},
						map[string]interface{}{"request": "foo", "storage": "bar", "access": "write"},
					},
				},
			}, schema))
			c.Assert(err, ErrorMatches, fmt.Sprintf(`.*storage paths "foo" and "bar" for request "foo" require incompatible types: %s != %s`, one.typ, other.typ))
		}
	}
}

func (s *aspectSuite) TestSetPreCheckValueAllowsIntNumberMismatch(c *C) {
	schema := mylog.Check2(aspects.ParseSchema([]byte(`{
	"schema": {
		"foo": "int",
		"bar": "number"
	}
}`)))


	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo", "access": "write"},
				map[string]interface{}{"request": "foo", "storage": "bar", "access": "write"},
			},
		},
	}, schema))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "foo", 1))

	mylog.

		// the schema still checks the data at the end, so setting int schema to a float fails
		Check(asp.Set(databag, "foo", 1.1))
	c.Assert(err, ErrorMatches, `.*cannot accept element in "foo": expected int type but value was number 1.1`)
}

func (*aspectSuite) TestSetPreCheckMultipleAlternativeTypesFail(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["int", "bool"],
		"bar": ["string", {"type": "array", "values": "string"}, {"schema": {"baz":"string"}}]
	}
}`)
	schema := mylog.Check2(aspects.ParseSchema(schemaStr))


	_ = mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo", "access": "write"},
				map[string]interface{}{"request": "foo", "storage": "bar", "access": "write"},
			},
		},
	}, schema))
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
	schema := mylog.Check2(aspects.ParseSchema(schemaStr))


	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo.b.c", "access": "write"},
				map[string]interface{}{"request": "foo", "storage": "bar.b.c", "access": "write"},
			},
		},
	}, schema))
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
	schema := mylog.Check2(aspects.ParseSchema(schemaStr))


	_ = mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "snaps.{snap}", "storage": "snaps.{snap}"},
				map[string]interface{}{"request": "snaps.{snap}.status", "storage": "snaps.{snap}.status"},
			},
		},
	}, schema))

}

func (*aspectSuite) TestSchemaMismatchCheckMultipleAlternativeTypesHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["int", "bool"],
		"bar": ["string", "bool"]
	}
}`)
	schema := mylog.Check2(aspects.ParseSchema(schemaStr))


	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo", "access": "write"},
				map[string]interface{}{"request": "foo", "storage": "bar", "access": "write"},
			},
		},
	}, schema))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "foo", true))

}

func (s *aspectSuite) TestSetUnmatchedPlaceholderLeaf(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}", "storage": "foo.{bar}"},
			},
		},
	}, aspects.NewJSONSchema()))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"bar": "value",
		"baz": "other",
	}))


	data := mylog.Check2(asp.Get(databag, "foo"))

	c.Assert(data, DeepEquals, map[string]interface{}{
		"bar": "value",
		"baz": "other",
	})
}

func (s *aspectSuite) TestSetUnmatchedPlaceholderMidPath(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}.nested", "storage": "foo.{bar}.nested"},
			},
		},
	}, aspects.NewJSONSchema()))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"bar": map[string]interface{}{"nested": "value"},
		"baz": map[string]interface{}{"nested": "other"},
	}))


	data := mylog.Check2(asp.Get(databag, "foo"))

	c.Assert(data, DeepEquals, map[string]interface{}{
		"bar": map[string]interface{}{"nested": "value"},
		"baz": map[string]interface{}{"nested": "other"},
	})
}

func (s *aspectSuite) TestSetManyUnmatchedPlaceholders(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}.a.{baz}", "storage": "foo.{bar}.{baz}"},
			},
		},
	}, aspects.NewJSONSchema()))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"a": map[string]interface{}{"a": map[string]interface{}{
			"c": "value",
			"d": "other",
		}},
		"b": map[string]interface{}{"a": map[string]interface{}{
			"e": "value",
			"f": "other",
		}},
	}))


	data := mylog.Check2(asp.Get(databag, "foo"))

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

func (s *aspectSuite) TestUnsetUnmatchedPlaceholderLast(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}", "storage": "foo.{bar}"},
			},
		},
	}, aspects.NewJSONSchema()))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"bar": "value",
		"baz": "other",
	}))

	mylog.Check(asp.Unset(databag, "foo"))


	_ = mylog.Check2(asp.Get(databag, "foo"))
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot get "foo" in aspect acc/bundle/foo: matching rules don't map to any values`)
}

func (s *aspectSuite) TestUnsetUnmatchedPlaceholderMid(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "all.{bar}", "storage": "foo.{bar}"},
				map[string]interface{}{"request": "one.{bar}", "storage": "foo.{bar}.one"},
			},
		},
	}, aspects.NewJSONSchema()))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "all", map[string]interface{}{
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
	}))

	mylog.Check(asp.Unset(databag, "one"))


	val := mylog.Check2(asp.Get(databag, "all"))

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
		pathsToValues := mylog.Check2(aspects.GetValuesThroughPaths(tc.path, tc.suffix, tc.value))

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
		aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
			"foo": map[string]interface{}{
				"rules": []interface{}{
					map[string]interface{}{"request": "a.{x}.d", "storage": "a.{x}"},
					map[string]interface{}{"request": "c.d.e.f", "storage": "d"},
					map[string]interface{}{"request": "b.f", "storage": "b.f"},
				},
			},
		}, aspects.NewJSONSchema()))


		asp := aspectBundle.Aspect("foo")
		c.Assert(asp, NotNil)
		mylog.Check(asp.Set(databag, tc.request, tc.value))
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
		bundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
			"foo": map[string]interface{}{
				"summary": val,
				"rules": []interface{}{
					map[string]interface{}{"request": "foo", "storage": "foo"},
				},
			},
		}, nil))
		c.Check(err.Error(), Equals, fmt.Sprintf(`cannot define aspect "foo": aspect summary must be a string but got %T`, val))
		c.Check(bundle, IsNil)
	}
}

func (*aspectSuite) TestAspectSummary(c *C) {
	bundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"summary": "some summary of the aspect",
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, aspects.NewJSONSchema()))

	c.Assert(bundle, NotNil)
}

func (s *aspectSuite) TestGetEntireAspect(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.{bar}", "storage": "foo-path.{bar}"},
				map[string]interface{}{"request": "abc", "storage": "abc-path"},
				map[string]interface{}{"request": "write-only", "storage": "write-only", "access": "write"},
			},
		},
	}, aspects.NewJSONSchema()))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"bar": "value",
		"baz": "other",
	}))

	mylog.Check(asp.Set(databag, "abc", "cba"))

	mylog.Check(asp.Set(databag, "write-only", "value"))


	result := mylog.Check2(asp.Get(databag, ""))


	c.Assert(result, DeepEquals, map[string]interface{}{
		"foo": map[string]interface{}{
			"bar": "value",
			"baz": "other",
		},
		"abc": "cba",
	})
}

func (*aspectSuite) TestAspectContentRule(c *C) {
	rules := map[string]interface{}{
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

	bundle := mylog.Check2(aspects.NewBundle("acc", "foo", rules, aspects.NewJSONSchema()))


	databag := aspects.NewJSONDataBag()
	mylog.Check(databag.Set("c.d", "value"))


	asp := bundle.Aspect("bar")
	val := mylog.Check2(asp.Get(databag, "a.b"))

	c.Assert(val, Equals, "value")
	mylog.Check(asp.Set(databag, "a.b", "other"))


	val = mylog.Check2(asp.Get(databag, "a.b"))

	c.Assert(val, Equals, "other")
}

func (*aspectSuite) TestAspectWriteContentRuleNestedInRead(c *C) {
	rules := map[string]interface{}{
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

	bundle := mylog.Check2(aspects.NewBundle("acc", "foo", rules, aspects.NewJSONSchema()))


	databag := aspects.NewJSONDataBag()
	asp := bundle.Aspect("bar")
	mylog.Check(asp.Set(databag, "a.b", "value"))


	_ = mylog.Check2(asp.Get(databag, "a.b"))
	c.Assert(err, ErrorMatches, `.*: no matching read rule`)

	val := mylog.Check2(asp.Get(databag, "a"))

	c.Assert(val, DeepEquals, map[string]interface{}{"d": "value"})
}

func (*aspectSuite) TestAspectInvalidContentRules(c *C) {
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
			err:     `.*aspect rules must have a "storage" field`,
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

		_ := mylog.Check2(aspects.NewBundle("acc", "foo", rules, aspects.NewJSONSchema()))
		c.Assert(err, ErrorMatches, tc.err)
	}
}

func (*aspectSuite) TestAspectSeveralNestedContentRules(c *C) {
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

	bundle := mylog.Check2(aspects.NewBundle("acc", "foo", rules, aspects.NewJSONSchema()))


	databag := aspects.NewJSONDataBag()
	asp := bundle.Aspect("bar")
	mylog.Check(asp.Set(databag, "a.b.c.d", "value"))


	val := mylog.Check2(asp.Get(databag, "a.b.c.d"))

	c.Assert(val, Equals, "value")
}

func (*aspectSuite) TestAspectInvalidMapKeys(c *C) {
	bundle := mylog.Check2(aspects.NewBundle("acc", "foo", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{
					"request": "foo",
					"storage": "foo",
				},
			},
		},
	}, aspects.NewJSONSchema()))


	databag := aspects.NewJSONDataBag()
	asp := bundle.Aspect("bar")

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
		mylog.Check(asp.Set(databag, "foo", tc.value))
		c.Assert(err, ErrorMatches, fmt.Sprintf("cannot set \"foo\" in aspect acc/foo/bar: key %q doesn't conform to required format: .*", tc.invalidKey), cmt)
	}
}

func (s *aspectSuite) TestSetUsingMapWithNilValuesAtLeaves(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
				map[string]interface{}{"request": "foo.a", "storage": "foo.a"},
				map[string]interface{}{"request": "foo.b", "storage": "foo.b"},
			},
		},
	}, aspects.NewJSONSchema()))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"a": "value",
		"b": "other",
	}))

	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"a": nil,
		"b": nil,
	}))


	value := mylog.Check2(asp.Get(databag, "foo"))

	c.Assert(value, DeepEquals, map[string]interface{}{})
}

func (s *aspectSuite) TestSetWithMultiplePathsNestedAtLeaves(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo.a", "storage": "foo.a"},
				map[string]interface{}{"request": "foo.b", "storage": "foo.b"},
			},
		},
	}, aspects.NewJSONSchema()))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"a": map[string]interface{}{
			"c": "value",
			"d": "other",
		},
		"b": "other",
	}))

	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"a": map[string]interface{}{
			"d": nil,
		},
		"b": nil,
	}))


	value := mylog.Check2(asp.Get(databag, "foo"))

	c.Assert(value, DeepEquals, map[string]interface{}{
		// consistent with the previous configuration mechanism
		"a": map[string]interface{}{},
	})
}

func (s *aspectSuite) TestSetWithNilAndNonNilLeaves(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle := mylog.Check2(aspects.NewBundle("acc", "bundle", map[string]interface{}{
		"foo": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{"request": "foo", "storage": "foo"},
			},
		},
	}, aspects.NewJSONSchema()))


	asp := aspectBundle.Aspect("foo")
	c.Assert(asp, NotNil)
	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"a": "value",
		"b": "other",
	}))

	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"a": nil,
		"c": "value",
	}))


	value := mylog.Check2(asp.Get(databag, "foo"))

	// nil values aren't stored but non-nil values are
	c.Assert(value, DeepEquals, map[string]interface{}{
		"c": "value",
	})
}

func (*aspectSuite) TestSetEnforcesNestednessLimit(c *C) {
	restore := aspects.MockMaxValueDepth(2)
	defer restore()

	bundle := mylog.Check2(aspects.NewBundle("acc", "foo", map[string]interface{}{
		"bar": map[string]interface{}{
			"rules": []interface{}{
				map[string]interface{}{
					"request": "foo",
					"storage": "foo",
				},
			},
		},
	}, aspects.NewJSONSchema()))


	databag := aspects.NewJSONDataBag()
	asp := bundle.Aspect("bar")
	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"bar": "baz",
	}))

	mylog.Check(asp.Set(databag, "foo", map[string]interface{}{
		"bar": map[string]interface{}{
			"baz": "value",
		},
	}))
	c.Assert(err, ErrorMatches, `cannot set "foo" in aspect acc/foo/bar: value cannot have more than 2 nested levels`)
}
