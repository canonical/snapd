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
	_, err := aspects.NewAspectBundle("acc", "foo", nil, aspects.NewJSONSchema())
	c.Assert(err, ErrorMatches, `cannot define aspects bundle: no aspects`)

	_, err = aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
		"bar": "baz",
	}, aspects.NewJSONSchema())
	c.Assert(err, ErrorMatches, `cannot define aspect "bar": access patterns should be a list of maps`)

	_, err = aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
		"bar": []map[string]string{},
	}, aspects.NewJSONSchema())
	c.Assert(err, ErrorMatches, `cannot define aspect "bar": no access patterns found`)

	_, err = aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
		"bar": []map[string]string{
			{"storage": "foo"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, ErrorMatches, `cannot define aspect "bar": access patterns must have a "request" field`)

	_, err = aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
		"bar": []map[string]string{
			{"request": "foo"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, ErrorMatches, `cannot define aspect "bar": access patterns must have a "storage" field`)

	aspectBundle, err := aspects.NewAspectBundle("acc", "foo", map[string]interface{}{
		"bar": []map[string]string{
			{"request": "a", "storage": "b"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)
	c.Check(aspectBundle, Not(IsNil))
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

	var ssid string
	err = wsAspect.Get(databag, "ssid", &ssid)
	c.Assert(err, IsNil)
	c.Check(ssid, Equals, "my-ssid")

	// nested list value
	err = wsAspect.Set(databag, "ssids", []string{"one", "two"})
	c.Assert(err, IsNil)

	var ssids []string
	err = wsAspect.Get(databag, "ssids", &ssids)
	c.Assert(err, IsNil)
	c.Check(ssids, DeepEquals, []string{"one", "two"})

	// top-level string
	var topLevel string
	err = wsAspect.Set(databag, "top-level", "randomValue")
	c.Assert(err, IsNil)

	err = wsAspect.Get(databag, "top-level", &topLevel)
	c.Assert(err, IsNil)
	c.Check(topLevel, Equals, "randomValue")

	// dotted request paths are permitted
	err = wsAspect.Set(databag, "dotted.path", 3)
	c.Assert(err, IsNil)

	var num int
	err = wsAspect.Get(databag, "dotted.path", &num)
	c.Assert(err, IsNil)
	c.Check(num, Equals, 3)
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

	var value string
	err = aspect.Get(databag, "missing", &value)
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find field "missing" of aspect acc/foo/bar: field not found`)

	err = aspect.Set(databag, "missing", "thing")
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find field "missing" of aspect acc/foo/bar: field not found`)

	err = aspect.Get(databag, "top-level", &value)
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find field "top-level" of aspect acc/foo/bar: no value was found under path "top-level"`)

	err = aspect.Set(databag, "nested", "thing")
	c.Assert(err, IsNil)

	err = aspect.Get(databag, "other-nested", &value)
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})
	c.Assert(err, ErrorMatches, `cannot find field "other-nested" of aspect acc/foo/bar: no value was found under path "top.nested-two"`)
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

	var value string
	err = aspect.Get(databag, "onetwo", &value)
	c.Assert(err, ErrorMatches, `cannot read path prefix "one": prefix maps to string`)

	var listVal []string
	err = aspect.Get(databag, "one", &listVal)
	c.Assert(err, ErrorMatches, `cannot read value of "one" into \*\[\]string: maps to string`)
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
			getErr: `cannot find field "read-only" of aspect acc/bundle/foo: no value was found under path "path"`,
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

		var value string
		err = aspect.Get(databag, t.request, &value)
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
	aspectBundle, err := aspects.NewAspectBundle("acc", "bundle", map[string]interface{}{
		"foo": []map[string]string{
			{"request": "defaults.{foo}", "storage": "first.{foo}.last"},
			{"request": "{bar}.name", "storage": "first.{bar}"},
			{"request": "first.{baz}.last", "storage": "{baz}.last"},
			{"request": "first.{foo}.{bar}", "storage": "{foo}.mid.{bar}"},
			{"request": "{foo}.mid2.{bar}", "storage": "{bar}.mid2.{foo}"},
			{"request": "multi.{foo}", "storage": "{foo}.multi.{foo}"},
		},
	}, aspects.NewJSONSchema())
	c.Assert(err, IsNil)

	aspect := aspectBundle.Aspect("foo")

	for _, t := range []struct {
		testName string
		request  string
		storage  string
	}{
		{
			testName: "placeholder last to mid",
			request:  "defaults.abc",
			storage:  "first.abc.last",
		},
		{
			testName: "placeholder first to last",
			request:  "foo.name",
			storage:  "first.foo",
		},
		{
			testName: "placeholder mid to first",
			request:  "first.foo.last",
			storage:  "foo.last",
		},
		{
			testName: "two placeholders in order",
			request:  "first.one.two",
			storage:  "one.mid.two",
		},
		{
			testName: "two placeholders out of order",
			request:  "first2.mid2.two2",
			storage:  "two2.mid2.first2",
		},
		{
			testName: "one placeholder mapping to several",
			request:  "multi.firstLast",
			storage:  "firstLast.multi.firstLast",
		},
	} {
		cmt := Commentf("sub-test %q failed", t.testName)

		databag := newWitnessDataBag(aspects.NewJSONDataBag())
		err := aspect.Set(databag, t.request, "expectedValue")
		c.Assert(err, IsNil, cmt)

		var obtainedValue string
		err = aspect.Get(databag, t.request, &obtainedValue)
		c.Assert(err, IsNil, cmt)

		c.Assert(obtainedValue, Equals, "expectedValue", cmt)

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

	var value string
	err = aspect.Get(databag, "foo", &value)
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})

	err = aspect.Get(databag, "bar", &value)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bval")
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

	var value string
	err = aspect.Get(databag, "bar", &value)
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})

	// doesn't affect the other leaf entry under "foo"
	err = aspect.Get(databag, "baz", &value)
	c.Assert(err, IsNil)
	c.Assert(value, Equals, "bazVal")
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

	var value interface{}
	err = aspect.Get(databag, "foo", &value)
	c.Assert(err, testutil.ErrorIs, &aspects.NotFoundError{})

	err = aspect.Get(databag, "bar", &value)
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

	var value interface{}
	err = aspect.Get(databag, "foo", &value)
	c.Assert(err, IsNil)
	c.Assert(value, Not(HasLen), 0)

	err = aspect.Set(databag, "bar", nil)
	c.Assert(err, IsNil)

	err = aspect.Get(databag, "foo", &value)
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
