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
			err:    `cannot define aspect "bar": access patterns should be a list of maps`,
		},
		{
			bundle: map[string]interface{}{"bar": []map[string]string{}},
			err:    `cannot define aspect "bar": no access patterns found`,
		},
		{
			bundle: map[string]interface{}{"bar": []map[string]string{{"storage": "foo"}}},
			err:    `cannot define aspect "bar": access patterns must have a "request" field`,
		},
		{
			bundle: map[string]interface{}{"bar": []map[string]string{{"request": "foo"}}},
			err:    `cannot define aspect "bar": access patterns must have a "storage" field`,
		},
		{
			bundle: map[string]interface{}{
				"bar": []map[string]string{
					{"request": "a", "storage": "b"},
					{"request": "a", "storage": "c"},
				},
			},
			err: `cannot define aspect "bar": cannot have several reading rules with the same "request" field`,
		},
		{
			bundle: map[string]interface{}{
				"bar": []map[string]string{
					{"request": "a", "storage": "c", "access": "write"},
					{"request": "a", "storage": "b"},
				},
			},
		},
	}

	for _, tc := range tcs {
		aspectBundle, err := aspects.NewAspectBundle("acc", "foo", tc.bundle, aspects.NewJSONSchema())
		if tc.err != "" {
			c.Assert(err, ErrorMatches, tc.err)
		} else {
			c.Assert(err, IsNil)
			c.Check(aspectBundle, Not(IsNil))
		}
	}
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
		aspectBundle, err := aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
			"bar": []map[string]string{
				{"request": "a", "storage": "b", "access": t.access},
			}}, aspects.NewJSONSchema())

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
	aspectBundle, err := aspects.NewAspectBundle("system", "network", map[string]interface{}{
		"wifi-setup": []map[string]string{
			{"request": "ssids", "storage": "wifi.ssids"},
			{"request": "ssid", "storage": "wifi.ssid"},
			{"request": "top-level", "storage": "top-level"},
			{"request": "dotted.path", "storage": "dotted"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	wsAspect := aspectBundle.Aspect("wifi-setup")

	// nested string value
	err = wsAspect.Set(databag, "ssid", "my-ssid")
	c.Assert(err, IsNil)

	ssid, err := wsAspect.Get(databag, "ssid")
	c.Assert(err, IsNil)
	c.Check(ssid, DeepEquals, map[string]interface{}{"ssid": "my-ssid"})

	// nested list value
	err = wsAspect.Set(databag, "ssids", []string{"one", "two"})
	c.Assert(err, IsNil)

	ssids, err := wsAspect.Get(databag, "ssids")
	c.Assert(err, IsNil)
	c.Check(ssids, DeepEquals, map[string]interface{}{"ssids": []interface{}{"one", "two"}})

	// top-level string
	err = wsAspect.Set(databag, "top-level", "randomValue")
	c.Assert(err, IsNil)

	topLevel, err := wsAspect.Get(databag, "top-level")
	c.Assert(err, IsNil)
	c.Check(topLevel, DeepEquals, map[string]interface{}{"top-level": "randomValue"})

	// dotted request paths are permitted
	err = wsAspect.Set(databag, "dotted.path", 3)
	c.Assert(err, IsNil)

	num, err := wsAspect.Get(databag, "dotted.path")
	c.Assert(err, IsNil)
	c.Check(num, DeepEquals, map[string]interface{}{"dotted.path": float64(3)})
}

func (s *aspectSuite) TestAspectNotFound(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
		"bar": []map[string]string{
			{"request": "top-level", "storage": "top-level"},
			{"request": "nested", "storage": "top.nested-one"},
			{"request": "other-nested", "storage": "top.nested-two"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("bar")

	_, err = aspect.Get(databag, "missing")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find value for "missing" in aspect acc/foo/bar: no matching read rule`)

	err = aspect.Set(databag, "missing", "thing")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find value for "missing" in aspect acc/foo/bar: no matching write rule`)

	_, err = aspect.Get(databag, "top-level")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find value for "top-level" in aspect acc/foo/bar: matching rules don't map to any values`)

	err = aspect.Set(databag, "nested", "thing")
	c.Assert(err, IsNil)

	_, err = aspect.Get(databag, "other-nested")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find value for "other-nested" in aspect acc/foo/bar: matching rules don't map to any values`)
}

func (s *aspectSuite) TestAspectBadRead(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
		"bar": []map[string]string{
			{"request": "one", "storage": "one"},
			{"request": "onetwo", "storage": "one.two"},
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
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"foo": []map[string]string{
			{"request": "default", "storage": "path.default"},
			{"request": "read-write", "storage": "path.read-write", "access": "read-write"},
			{"request": "read-only", "storage": "path.read-only", "access": "read"},
			{"request": "write-only", "storage": "path.write-only", "access": "write"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("foo")

	for _, t := range []struct {
		request string
		getErr  string
		setErr  string
	}{
		{
			request: "read-write",
		},
		{
			// defaults to "read-write"
			request: "default",
		},
		{
			request: "read-only",
			// unrelated error
			getErr: `cannot find value for "read-only" in aspect acc/bundle/foo: matching rules don't map to any values`,
			setErr: `cannot write field "read-only": only supports read access`,
		},
		{
			request: "write-only",
			getErr:  `cannot read field "write-only": only supports write access`,
		},
	} {
		cmt := Commentf("sub-test %q failed", t.request)
		databag := aspects.NewJSONDataBag()

		err := aspect.Set(databag, t.request, "thing")
		if t.setErr != "" {
			c.Assert(err.Error(), Equals, t.setErr, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
		}

		_, err = aspect.Get(databag, t.request)
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
		rule     map[string]string
		testName string
		request  string
		storage  string
	}{
		{
			testName: "placeholder last to mid",
			rule:     map[string]string{"request": "defaults.{foo}", "storage": "first.{foo}.last"},
			request:  "defaults.abc",
			storage:  "first.abc.last",
		},
		{
			testName: "placeholder first to last",
			rule:     map[string]string{"request": "{bar}.name", "storage": "first.{bar}"},
			request:  "foo.name",
			storage:  "first.foo",
		},
		{
			testName: "placeholder mid to first",
			rule:     map[string]string{"request": "first.{baz}.last", "storage": "{baz}.last"},
			request:  "first.foo.last",
			storage:  "foo.last",
		},
		{
			testName: "two placeholders in order",
			rule:     map[string]string{"request": "first.{foo}.{bar}", "storage": "{foo}.mid.{bar}"},
			request:  "first.one.two",
			storage:  "one.mid.two",
		},
		{
			testName: "two placeholders out of order",
			rule:     map[string]string{"request": "{foo}.mid1.{bar}", "storage": "{bar}.mid2.{foo}"},
			request:  "first2.mid1.two2",
			storage:  "two2.mid2.first2",
		},
		{
			testName: "one placeholder mapping to several",
			rule:     map[string]string{"request": "multi.{foo}", "storage": "{foo}.multi.{foo}"},
			request:  "multi.firstlast",
			storage:  "firstlast.multi.firstlast",
		},
	} {
		cmt := Commentf("sub-test %q failed", t.testName)

		aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
			"foo": []map[string]string{t.rule},
		}, aspects.NewJSONSchema())
		c.Assert(err, IsNil)
		aspect := aspectBundle.Aspect("foo")

		databag := newWitnessDataBag(aspects.NewJSONDataBag())
		err = aspect.Set(databag, t.request, "expectedValue")
		c.Assert(err, IsNil, cmt)

		value, err := aspect.Get(databag, t.request)
		c.Assert(err, IsNil, cmt)
		c.Assert(value, DeepEquals, map[string]interface{}{t.request: "expectedValue"}, cmt)

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
		_, err := aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
			"foo": []map[string]string{
				{"request": tc.request, "storage": tc.storage},
			},
		}, aspects.NewJSONSchema())

		cmt := Commentf("sub-test %q failed", tc.testName)
		c.Assert(err, Not(IsNil), cmt)
		c.Assert(err.Error(), Equals, `cannot define aspect "foo": `+tc.err, cmt)
	}
}

func (s *aspectSuite) TestAspectUnsetTopLevelEntry(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
		"my-aspect": []map[string]string{
			{"request": "foo", "storage": "foo"},
			{"request": "bar", "storage": "bar"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("my-aspect")
	err = aspect.Set(databag, "foo", "fval")
	c.Assert(err, IsNil)

	err = aspect.Set(databag, "bar", "bval")
	c.Assert(err, IsNil)

	err = aspect.Set(databag, "foo", nil)
	c.Assert(err, IsNil)

	_, err = aspect.Get(databag, "foo")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})

	value, err := aspect.Get(databag, "bar")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"bar": "bval"})
}

func (s *aspectSuite) TestAspectUnsetLeafWithSiblings(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
		"my-aspect": []map[string]string{
			{"request": "bar", "storage": "foo.bar"},
			{"request": "baz", "storage": "foo.baz"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("my-aspect")
	err = aspect.Set(databag, "bar", "barVal")
	c.Assert(err, IsNil)

	err = aspect.Set(databag, "baz", "bazVal")
	c.Assert(err, IsNil)

	err = aspect.Set(databag, "bar", nil)
	c.Assert(err, IsNil)

	_, err = aspect.Get(databag, "bar")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})

	// doesn't affect the other leaf entry under "foo"
	value, err := aspect.Get(databag, "baz")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"baz": "bazVal"})
}

func (s *aspectSuite) TestAspectUnsetWithNestedEntry(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
		"my-aspect": []map[string]string{
			{"request": "foo", "storage": "foo"},
			{"request": "bar", "storage": "foo.bar"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("my-aspect")
	err = aspect.Set(databag, "bar", "barVal")
	c.Assert(err, IsNil)

	err = aspect.Set(databag, "foo", nil)
	c.Assert(err, IsNil)

	_, err = aspect.Get(databag, "foo")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})

	_, err = aspect.Get(databag, "bar")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
}

func (s *aspectSuite) TestAspectUnsetLeafUnsetsParent(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
		"my-aspect": []map[string]string{
			{"request": "foo", "storage": "foo"},
			{"request": "bar", "storage": "foo.bar"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("my-aspect")
	err = aspect.Set(databag, "bar", "val")
	c.Assert(err, IsNil)

	value, err := aspect.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, Not(HasLen), 0)

	err = aspect.Set(databag, "bar", nil)
	c.Assert(err, IsNil)

	_, err = aspect.Get(databag, "foo")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
}

func (s *aspectSuite) TestAspectUnsetAlreadyUnsetEntry(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
		"my-aspect": []map[string]string{
			{"request": "foo", "storage": "foo"},
			{"request": "bar", "storage": "one.bar"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("my-aspect")
	err = aspect.Set(databag, "foo", nil)
	c.Assert(err, IsNil)

	err = aspect.Set(databag, "bar", nil)
	c.Assert(err, IsNil)
}

func (s *aspectSuite) TestInvalidAccessError(c *C) {
	err := &aspects.InvalidAccessError{RequestedAccess: 1, FieldAccess: 2, Field: "foo"}
	c.Assert(err, testutil.ErrorIs, &aspects.InvalidAccessError{})
	c.Assert(err, ErrorMatches, `cannot read field "foo": only supports write access`)

	err = &aspects.InvalidAccessError{RequestedAccess: 2, FieldAccess: 1, Field: "foo"}
	c.Assert(err, testutil.ErrorIs, &aspects.InvalidAccessError{})
	c.Assert(err, ErrorMatches, `cannot write field "foo": only supports read access`)
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
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"bar": []map[string]string{
			{"request": "one", "storage": "one"},
			{"request": "one.two", "storage": "one.two"},
			{"request": "onetwo", "storage": "one.two"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("bar")
	err = aspect.Set(databag, "one", map[string]interface{}{"two": "value"})
	c.Assert(err, IsNil)

	value, err := aspect.Get(databag, "one.two")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"one.two": "value"})

	value, err = aspect.Get(databag, "onetwo")
	c.Assert(err, IsNil)
	// the key matches the request, not the storage storage
	c.Assert(value, DeepEquals, map[string]interface{}{"onetwo": "value"})

	value, err = aspect.Get(databag, "one")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"one": map[string]interface{}{"two": "value"}})
}

func (s *aspectSuite) TestAspectGetMatchesOnPrefix(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"statuses": []map[string]string{
			{"request": "snapd.status", "storage": "snaps.snapd.status"},
			{"request": "snaps", "storage": "snaps"},
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
	c.Assert(value, DeepEquals, map[string]interface{}{"snapd.status": "active"})

	value, err = aspect.Get(databag, "snapd")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"snapd": map[string]interface{}{"status": "active"}})
}

func (s *aspectSuite) TestAspectGetNoMatchRequestLongerThanPattern(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"statuses": []map[string]string{
			{"request": "snapd", "storage": "snaps.snapd"},
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
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"statuses": []map[string]string{
			{"request": "status.firefox", "storage": "snaps.firefox.status"},
			{"request": "status.snapd", "storage": "snaps.snapd.status"},
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
	c.Assert(value, DeepEquals, map[string]interface{}{
		"status": map[string]interface{}{
			"snapd":   "disabled",
			"firefox": "active",
		},
	})
}

func (s *aspectSuite) TestAspectCombineNamespacesInPrefixMatches(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"statuses": []map[string]string{
			{"request": "status.foo.bar.firefox", "storage": "snaps.firefox.status"},
			{"request": "status.foo.snapd", "storage": "snaps.snapd.status"},
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
	c.Assert(value, DeepEquals, map[string]interface{}{
		"status": map[string]interface{}{
			"foo": map[string]interface{}{
				"bar": map[string]interface{}{
					"firefox": "active",
				},
				"snapd": "disabled",
			},
		},
	})
}

func (s *aspectSuite) TestGetScalarOverwritesLeafOfMapValue(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"motors": []map[string]string{
			{"request": "motors.a.speed", "storage": "new-speed.a"},
			{"request": "motors", "storage": "motors"},
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
	c.Assert(value, DeepEquals, map[string]interface{}{"motors": map[string]interface{}{"a": map[string]interface{}{"speed": 101.5}}})
}

func (s *aspectSuite) TestGetSingleScalarOk(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"foo": []map[string]string{
			{"request": "foo", "storage": "foo"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set("foo", "bar")
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("foo")

	value, err := aspect.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]interface{}{"foo": "bar"})
}

func (s *aspectSuite) TestGetMatchScalarAndMapError(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"foo": []map[string]string{
			{"request": "foo", "storage": "bar"},
			{"request": "foo.baz", "storage": "baz"},
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
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"foo": []map[string]string{
			{"request": "foo.bar.baz", "storage": "third"},
			{"request": "foo", "storage": "first"},
			{"request": "foo.bar", "storage": "second"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)
	aspect := aspectBundle.Aspect("foo")

	err = databag.Set("first", map[string]interface{}{"bar": map[string]interface{}{"baz": "first"}})
	c.Assert(err, IsNil)

	value, err := aspect.Get(databag, "foo")
	c.Assert(err, IsNil)
	// returned the value read by entry "foo"
	c.Assert(value, DeepEquals, map[string]interface{}{"foo": map[string]interface{}{"bar": map[string]interface{}{"baz": "first"}}})

	err = databag.Set("second", map[string]interface{}{"baz": "second"})
	c.Assert(err, IsNil)

	value, err = aspect.Get(databag, "foo")
	c.Assert(err, IsNil)
	// the leaf is replaced by a value read from a rule that is nested
	c.Assert(value, DeepEquals, map[string]interface{}{"foo": map[string]interface{}{"bar": map[string]interface{}{"baz": "second"}}})

	err = databag.Set("third", "third")
	c.Assert(err, IsNil)

	value, err = aspect.Get(databag, "foo")
	c.Assert(err, IsNil)
	// lastly, it reads the value from "foo.bar.baz" the most nested entry
	c.Assert(value, DeepEquals, map[string]interface{}{"foo": map[string]interface{}{"bar": map[string]interface{}{"baz": "third"}}})
}

func (s *aspectSuite) TestGetUnmatchedPlaceholderReturnsAll(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"snaps": []map[string]string{
			{"request": "snaps.{snap}", "storage": "snaps.{snap}"},
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
	c.Assert(value, DeepEquals, map[string]interface{}{"snaps": map[string]interface{}{"snapd": float64(1), "foo": map[string]interface{}{"bar": float64(2)}}})
}

func (s *aspectSuite) TestGetUnmatchedPlaceholdersWithNestedValues(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"statuses": []map[string]string{
			{"request": "snaps.{snap}.status", "storage": "snaps.{snap}.status"},
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
	c.Assert(value, DeepEquals, map[string]interface{}{"snaps": map[string]interface{}{"snapd": map[string]interface{}{"status": "active"}}})
}

func (s *aspectSuite) TestGetSeveralUnmatchedPlaceholders(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"foo": []map[string]string{
			{"request": "a.{b}.c.{d}.e", "storage": "a.{b}.c.{d}.e"},
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
		"a": map[string]interface{}{
			"b1": map[string]interface{}{
				"c": map[string]interface{}{
					"d1": map[string]interface{}{
						"e": "end",
					},
				},
			},
		},
	}
	c.Assert(value, DeepEquals, expected)
}

func (s *aspectSuite) TestGetMergeAtDifferentLevels(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"foo": []map[string]string{
			{"request": "a.{b}.c.{d}.e", "storage": "a.{b}.c.{d}.e"},
			{"request": "a.{b}.c.{d}", "storage": "a.{b}.c.{d}"},
			{"request": "a.{b}", "storage": "a.{b}"},
			{"request": "a", "storage": "a"},
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
		"a": map[string]interface{}{
			"b": map[string]interface{}{
				"c": map[string]interface{}{
					"d": map[string]interface{}{
						"e": "end",
					},
				},
			},
		},
	}
	c.Assert(value, DeepEquals, expected)
}

func (s *aspectSuite) TestBadRequestPaths(c *C) {
	databag := aspects.NewJSONDataBag()
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"foo": []map[string]string{
			{"request": "a.{b}.c", "storage": "a.{b}.c"},
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
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot parse Set request: %s`, tc.errMsg), cmt)

		_, err = asp.Get(databag, tc.request)
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot parse Get request: %s`, tc.errMsg), cmt)
	}
}
