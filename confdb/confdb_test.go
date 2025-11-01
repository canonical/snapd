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

package confdb_test

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/confdb"
	"github.com/snapcore/snapd/testutil"
)

type viewSuite struct{}

func Test(t *testing.T) { TestingT(t) }

var _ = Suite(&viewSuite{})

type failingSchema struct {
	err error
}

func (f *failingSchema) Validate([]byte) error { return f.err }
func (f *failingSchema) SchemaAt(path []confdb.Accessor) ([]confdb.DatabagSchema, error) {
	return []confdb.DatabagSchema{f}, nil
}
func (f *failingSchema) Type() confdb.SchemaType { return confdb.Any }
func (f *failingSchema) Ephemeral() bool         { return false }
func (f *failingSchema) NestedEphemeral() bool   { return false }

func parsePath(c *C, path string) []confdb.Accessor {
	accs, err := confdb.ParsePathIntoAccessors(path, confdb.ParseOptions{})
	c.Assert(err, IsNil)
	return accs
}

func (*viewSuite) TestNewConfdb(c *C) {
	type testcase struct {
		confdb map[string]any
		err    string
	}

	tcs := []testcase{
		{
			err: `cannot define confdb schema: no views`,
		},
		{
			confdb: map[string]any{"0-a": map[string]any{}},
			err:    `cannot define view "0-a": name must conform to [a-z](?:-?[a-z0-9])*`,
		},
		{
			confdb: map[string]any{"bar": "baz"},
			err:    `cannot define view "bar": view must be non-empty map`,
		},
		{
			confdb: map[string]any{"bar": map[string]any{}},
			err:    `cannot define view "bar": view must be non-empty map`,
		},
		{
			confdb: map[string]any{"bar": map[string]any{"rules": "bar"}},
			err:    `cannot define view "bar": view rules must be non-empty list`,
		},
		{
			confdb: map[string]any{"bar": map[string]any{"rules": []any{}}},
			err:    `cannot define view "bar": view rules must be non-empty list`,
		},
		{
			confdb: map[string]any{"bar": map[string]any{"rules": []any{"a"}}},
			err:    `cannot define view "bar": each view rule should be a map`,
		},
		{
			confdb: map[string]any{"bar": map[string]any{"rules": []any{map[string]any{}}}},
			err:    `cannot define view "bar": view rules must have a "storage" field`,
		},

		{
			confdb: map[string]any{"bar": map[string]any{"rules": []any{map[string]any{"request": "foo", "storage": 1}}}},
			err:    `cannot define view "bar": "storage" must be a string`,
		},
		{
			confdb: map[string]any{"bar": map[string]any{"rules": []any{map[string]any{"storage": "foo", "request": 1}}}},
			err:    `cannot define view "bar": "request" must be a string`,
		},
		{
			confdb: map[string]any{"bar": map[string]any{"rules": []any{map[string]any{"storage": "foo", "request": ""}}}},
			err:    `cannot define view "bar": view rules' "request" field must be non-empty, if it exists`,
		},
		{
			confdb: map[string]any{"bar": map[string]any{"rules": []any{map[string]any{"request": "foo", "storage": 1}}}},
			err:    `cannot define view "bar": "storage" must be a string`,
		},
		{
			confdb: map[string]any{
				"bar": map[string]any{
					"rules": []any{
						map[string]any{"request": "a", "storage": "b"},
						map[string]any{"request": "a", "storage": "c"},
					},
				},
			},
			err: `cannot define view "bar": cannot have several reading rules with the same "request" field`,
		},
		{
			confdb: map[string]any{"bar": map[string]any{"rules": []any{map[string]any{"request": "foo", "storage": "bar", "access": 1}}}},
			err:    `cannot define view "bar": "access" must be a string`,
		},
		{
			confdb: map[string]any{
				"bar": map[string]any{
					"rules": []any{
						map[string]any{"request": "a", "storage": "c", "access": "write"},
						map[string]any{"request": "a", "storage": "b"},
					},
				},
			},
		},
		{
			confdb: map[string]any{
				"bar": map[string]any{
					"rules": []any{
						map[string]any{"request": "a.{bar}[{bar}]", "storage": "foo[{bar}].{bar}"},
					},
				},
			},
			err: `cannot define view "bar": cannot use same name "bar" for key and index placeholder: a.{bar}[{bar}]`,
		},
		{
			confdb: map[string]any{
				"bar": map[string]any{
					"rules": []any{
						map[string]any{"request": "{bar}.a.{bar}", "storage": "foo.{bar}"},
					},
				},
			},
			err: `cannot define view "bar": request cannot have more than one placeholder with the same name "bar": {bar}.a.{bar}`,
		},
		{
			confdb: map[string]any{
				"bar": map[string]any{
					"rules": []any{
						map[string]any{"request": "a.{bar}", "storage": "foo.{bar}.{bar}"},
					},
				},
			},
		},
		{
			confdb: map[string]any{
				"bar": map[string]any{
					"rules": []any{
						map[string]any{"request": "a.b", "storage": "[1].b"},
					},
				},
			},
			err: `cannot define view "bar": invalid storage "[1].b": cannot have empty subkeys`,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("test number %d", i+1)
		confdb, err := confdb.NewSchema("acc", "foo", tc.confdb, confdb.NewJSONSchema())
		if tc.err != "" {
			c.Assert(err, NotNil, cmt)
			c.Assert(err.Error(), Equals, tc.err, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Check(confdb, NotNil, cmt)
		}
	}
}

func (s *viewSuite) TestMissingRequestDefaultsToStorage(c *C) {
	databag := confdb.NewJSONDatabag()
	views := map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"storage": "a.b"},
			},
		},
	}
	db, err := confdb.NewSchema("acc", "foo", views, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := db.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "a.b", "value")
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]any{
		"a": map[string]any{
			"b": "value",
		},
	})
}

func (s *viewSuite) TestConfdbSchemaWithSample(c *C) {
	views := map[string]any{
		"wifi-setup": map[string]any{
			"rules": []any{
				map[string]any{"request": "ssids", "storage": "wifi.ssids"},
				map[string]any{"access": "read-write", "request": "ssid", "storage": "wifi.ssid"},
				map[string]any{"access": "write", "request": "password", "storage": "wifi.psk"},
				map[string]any{"access": "read", "request": "status", "storage": "wifi.status"},
				map[string]any{"request": "private.{key}", "storage": "wifi.{key}"},
			},
		},
	}
	schema, err := confdb.NewSchema("acc", "foo", views, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("other")
	c.Assert(view, IsNil)
	view = schema.View("wifi-setup")
	c.Assert(view, NotNil)
	c.Assert(view.Schema(), Equals, schema)
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
		schema, err := confdb.NewSchema("acc", "foo", map[string]any{
			"bar": map[string]any{
				"rules": []any{
					map[string]any{"request": "a", "storage": "b", "access": t.access},
				},
			},
		}, confdb.NewJSONSchema())

		cmt := Commentf("\"%s access\" sub-test failed", t.access)
		if t.err {
			c.Assert(err, ErrorMatches, fmt.Sprintf(`.*expected 'access' to be either "read-write", "read", "write" or empty but was %q`, t.access), cmt)
			c.Check(schema, IsNil, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
			c.Check(schema, NotNil, cmt)
		}
	}
}

func (*viewSuite) TestGetAndSetViews(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("system", "network", map[string]any{
		"wifi-setup": map[string]any{
			"rules": []any{
				map[string]any{"request": "ssids", "storage": "wifi.ssids"},
				map[string]any{"request": "ssid", "storage": "wifi.ssid"},
				map[string]any{"request": "top-level", "storage": "top-level"},
				map[string]any{"request": "dotted.path", "storage": "dotted"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	wsView := schema.View("wifi-setup")

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
	c.Check(ssids, DeepEquals, []any{"one", "two"})

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
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("system", "test", map[string]any{
		"test": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	wsView := schema.View("test")

	err = wsView.Set(databag, "foo", "value")
	c.Assert(err, IsNil)

	err = wsView.Set(databag, "foo", nil)
	c.Assert(err, ErrorMatches, `internal error: Set value cannot be nil`)

	ssid, err := wsView.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Check(ssid, DeepEquals, "value")
}

func (s *viewSuite) TestConfdbNotFoundErrors(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "foo", map[string]any{
		"bar": map[string]any{
			"rules": []any{
				map[string]any{"request": "top-level", "storage": "top-level"},
				map[string]any{"request": "nested", "storage": "top.nested-one"},
				map[string]any{"request": "other-nested", "storage": "top.nested-two"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("bar")

	_, err = view.Get(databag, "missing")
	c.Assert(err, testutil.ErrorIs, &confdb.NoMatchError{})
	c.Assert(err, ErrorMatches, `cannot get "missing" through acc/foo/bar: no matching rule`)

	err = view.Set(databag, "missing", "thing")
	c.Assert(err, testutil.ErrorIs, &confdb.NoMatchError{})
	c.Assert(err, ErrorMatches, `cannot set "missing" through acc/foo/bar: no matching rule`)

	err = view.Unset(databag, "missing")
	c.Assert(err, testutil.ErrorIs, &confdb.NoMatchError{})
	c.Assert(err, ErrorMatches, `cannot unset "missing" through acc/foo/bar: no matching rule`)

	_, err = view.Get(databag, "top-level")
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})
	c.Assert(err, ErrorMatches, `cannot get "top-level" through acc/foo/bar: no data`)

	_, err = view.Get(databag, "")
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})
	c.Assert(err, ErrorMatches, `cannot get acc/foo/bar: no data`)

	err = view.Set(databag, "nested", "thing")
	c.Assert(err, IsNil)

	err = view.Unset(databag, "nested")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "other-nested")
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})
	c.Assert(err, ErrorMatches, `cannot get "other-nested" through acc/foo/bar: no data`)
}

func (s *viewSuite) TestConfdbNoMatchAllSubkeyTypes(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "foo", map[string]any{
		"bar": map[string]any{
			"rules": []any{
				map[string]any{"request": "a.{b}[{m}][{n}]", "storage": "a.{b}[{m}][{n}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("bar")

	// check each sub-key in the rule path rejects an unmatchable request
	for _, request := range []string{"b", "a[1]", "a.b.c", "a.b[1].d"} {
		_, err = view.Get(databag, request)
		c.Assert(err, testutil.ErrorIs, &confdb.NoMatchError{})
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot get %q through acc/foo/bar: no matching rule`, request))
	}

	// but they accept the right request
	_, err = view.Get(databag, "a.b[1][0]")
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})
}

func (s *viewSuite) TestViewBadRead(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "foo", map[string]any{
		"bar": map[string]any{
			"rules": []any{
				map[string]any{"request": "one", "storage": "one"},
				map[string]any{"request": "onetwo", "storage": "one.two"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("bar")
	err = view.Set(databag, "one", "foo")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "onetwo")
	c.Assert(err, ErrorMatches, `cannot decode databag at path "one": expected container type but got string`)
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
			getErr: `cannot get "foo" through acc/confdb/foo: no data`,
			setErr: `cannot set "foo" through acc/confdb/foo: no matching rule`,
		},
		{
			access: "write",
			getErr: `cannot get "foo" through acc/confdb/foo: no matching rule`,
		},
	} {
		cmt := Commentf("sub-test with %q access failed", t.access)
		databag := confdb.NewJSONDatabag()
		schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
			"foo": map[string]any{
				"rules": []any{
					map[string]any{"request": "foo", "storage": "foo", "access": t.access},
				},
			},
		}, confdb.NewJSONSchema())
		c.Assert(err, IsNil)

		view := schema.View("foo")

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

type witnessDatabag struct {
	bag              confdb.Databag
	getPath, setPath string
}

func newWitnessDatabag(bag confdb.Databag) *witnessDatabag {
	return &witnessDatabag{bag: bag}
}

func (s *witnessDatabag) Get(path []confdb.Accessor) (any, error) {
	s.getPath = confdb.JoinAccessors(path)
	return s.bag.Get(path)
}

func (s *witnessDatabag) Set(path []confdb.Accessor, value any) error {
	s.setPath = confdb.JoinAccessors(path)
	return s.bag.Set(path, value)
}

func (s *witnessDatabag) Unset(path []confdb.Accessor) error {
	s.setPath = confdb.JoinAccessors(path)
	return s.bag.Unset(path)
}

func (s *witnessDatabag) Data() ([]byte, error) {
	return s.bag.Data()
}

// getLastPaths returns the last paths passed into Get and Set and resets them.
func (s *witnessDatabag) getLastPaths() (get, set string) {
	get, set = s.getPath, s.setPath
	s.getPath, s.setPath = "", ""
	return get, set
}

func (s *viewSuite) TestViewAssertionWithPlaceholder(c *C) {
	for _, t := range []struct {
		rule     map[string]any
		testName string
		request  string
		storage  string
	}{
		{
			testName: "placeholder last to mid",
			rule:     map[string]any{"request": "defaults.{foo}", "storage": "first.{foo}.last"},
			request:  "defaults.abc",
			storage:  "first.abc.last",
		},
		{
			testName: "placeholder first to last",
			rule:     map[string]any{"request": "{bar}.name", "storage": "first.{bar}"},
			request:  "foo.name",
			storage:  "first.foo",
		},
		{
			testName: "placeholder mid to first",
			rule:     map[string]any{"request": "first.{baz}.last", "storage": "{baz}.last"},
			request:  "first.foo.last",
			storage:  "foo.last",
		},
		{
			testName: "two placeholders in order",
			rule:     map[string]any{"request": "first.{foo}.{bar}", "storage": "{foo}.mid.{bar}"},
			request:  "first.one.two",
			storage:  "one.mid.two",
		},
		{
			testName: "two placeholders out of order",
			rule:     map[string]any{"request": "{foo}.mid1.{bar}", "storage": "{bar}.mid2.{foo}"},
			request:  "first2.mid1.two2",
			storage:  "two2.mid2.first2",
		},
		{
			testName: "one placeholder mapping to several",
			rule:     map[string]any{"request": "multi.{foo}", "storage": "{foo}.multi.{foo}"},
			request:  "multi.firstlast",
			storage:  "firstlast.multi.firstlast",
		},
	} {
		cmt := Commentf("sub-test %q failed", t.testName)

		schema, err := confdb.NewSchema("acc", "db", map[string]any{
			"foo": map[string]any{
				"rules": []any{t.rule},
			},
		}, confdb.NewJSONSchema())
		c.Assert(err, IsNil)
		view := schema.View("foo")

		databag := newWitnessDatabag(confdb.NewJSONDatabag())
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
			request:  "bad.{foo}", storage: "bad.{bar}", err: `placeholder "foo" from request "bad.{foo}" is absent from storage "bad.{bar}"`,
		},
		{
			testName: "index placeholder mismatch",
			request:  "bad[{foo}]", storage: "bad[{bar}]", err: `placeholder "foo" from request "bad[{foo}]" is absent from storage "bad[{bar}]"`,
		},
		{
			testName: "index placeholder mismatch despite key placeholder with same name",
			request:  "bad[{foo}]", storage: "bad.{foo}", err: `request "bad[{foo}]" and storage "bad.{foo}" have mismatched placeholders`,
		},
		{
			testName: "repeated placeholder in request",
			request:  "{bar}.a.{bar}",
			storage:  "foo.{bar}",
			err:      `request cannot have more than one placeholder with the same name "bar": {bar}.a.{bar}`,
		},
		{
			testName: "repeated placeholder but in field and index",
			request:  "{bar}[{bar}]",
			storage:  "{bar}[{bar}]",
			err:      `cannot use same name "bar" for key and index placeholder: {bar}[{bar}]`,
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
		_, err := confdb.NewSchema("acc", "foo", map[string]any{
			"foo": map[string]any{
				"rules": []any{
					map[string]any{"request": tc.request, "storage": tc.storage},
				},
			},
		}, confdb.NewJSONSchema())

		cmt := Commentf("sub-test %q failed", tc.testName)
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, `cannot define view "foo": `+tc.err, cmt)
	}
}

func (s *viewSuite) TestViewUnsetTopLevelEntry(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "foo", map[string]any{
		"my-view": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
				map[string]any{"request": "bar", "storage": "bar"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("my-view")
	err = view.Set(databag, "foo", "fval")
	c.Assert(err, IsNil)

	err = view.Set(databag, "bar", "bval")
	c.Assert(err, IsNil)

	err = view.Unset(databag, "foo")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "foo")
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})

	value, err := view.Get(databag, "bar")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "bval")
}

func (s *viewSuite) TestViewUnsetLeafWithSiblings(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "foo", map[string]any{
		"my-view": map[string]any{
			"rules": []any{
				map[string]any{"request": "bar", "storage": "foo.bar"},
				map[string]any{"request": "baz", "storage": "foo.baz"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("my-view")
	err = view.Set(databag, "bar", "barVal")
	c.Assert(err, IsNil)

	err = view.Set(databag, "baz", "bazVal")
	c.Assert(err, IsNil)

	err = view.Unset(databag, "bar")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "bar")
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})

	// doesn't affect the other leaf entry under "foo"
	value, err := view.Get(databag, "baz")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "bazVal")
}

func (s *viewSuite) TestViewUnsetWithNestedEntry(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "foo", map[string]any{
		"my-view": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
				map[string]any{"request": "bar", "storage": "foo.bar"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("my-view")
	err = view.Set(databag, "bar", "barVal")
	c.Assert(err, IsNil)

	err = view.Unset(databag, "foo")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "foo")
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})

	_, err = view.Get(databag, "bar")
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})
}

func (s *viewSuite) TestViewUnsetLeafLeavesEmptyParent(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "foo", map[string]any{
		"my-view": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
				map[string]any{"request": "foo.bar", "storage": "foo.bar"},
				map[string]any{"request": "a[{n}]", "storage": "a[{n}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	view := schema.View("my-view")

	// check we leave an empty map
	err = view.Set(databag, "foo.bar", "val")
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, Not(HasLen), 0)

	err = view.Unset(databag, "foo.bar")
	c.Assert(err, IsNil)

	value, err = view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]any{})

	// check we leave an empty list
	err = view.Set(databag, "a", []any{[]any{"foo"}})
	c.Assert(err, IsNil)

	value, err = view.Get(databag, "a")
	c.Assert(err, IsNil)
	c.Assert(value, Not(HasLen), 0)

	err = view.Unset(databag, "a[0]")
	c.Assert(err, IsNil)

	val, err := view.Get(databag, "a")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{})
}

func (s *viewSuite) TestViewUnsetAlreadyUnsetEntry(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "foo", map[string]any{
		"my-view": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
				map[string]any{"request": "bar", "storage": "one.bar"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("my-view")
	err = view.Unset(databag, "foo")
	c.Assert(err, IsNil)

	err = view.Unset(databag, "bar")
	c.Assert(err, IsNil)
}

func (s *viewSuite) TestJSONDatabagCopy(c *C) {
	bag := confdb.NewJSONDatabag()
	path := parsePath(c, "foo")
	err := bag.Set(path, "bar")
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
	err = bagCopy.Set(path, "baz")
	c.Assert(err, IsNil)

	data, err = bag.Data()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"foo":"bar"}`)

	// and vice-versa
	err = bag.Set(path, "zab")
	c.Assert(err, IsNil)

	data, err = bagCopy.Data()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"foo":"baz"}`)
}

func (s *viewSuite) TestJSONDataOverwrite(c *C) {
	bag := confdb.NewJSONDatabag()
	err := bag.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)

	// precondition check
	data, err := bag.Data()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"foo":"bar"}`)

	err = bag.Overwrite([]byte(`{"bar":"foo"}`))
	c.Assert(err, IsNil)

	val, err := bag.Get(parsePath(c, "bar"))
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "foo")

	data, err = bag.Data()
	c.Assert(err, IsNil)
	c.Assert(string(data), Equals, `{"bar":"foo"}`)
}

func (s *viewSuite) TestViewGetResultNamespaceMatchesRequest(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"bar": map[string]any{
			"rules": []any{
				map[string]any{"request": "one", "storage": "one"},
				map[string]any{"request": "one.two", "storage": "one.two"},
				map[string]any{"request": "onetwo", "storage": "one.two"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("bar")
	err = databag.Set(parsePath(c, "one"), map[string]any{"two": "value"})
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
	c.Assert(value, DeepEquals, map[string]any{"two": "value"})
}

func (s *viewSuite) TestViewGetMatchesOnPrefix(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"statuses": map[string]any{
			"rules": []any{
				map[string]any{"request": "snapd.status", "storage": "snaps.snapd.status"},
				map[string]any{"request": "snaps", "storage": "snaps"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("statuses")
	err = view.Set(databag, "snaps", map[string]map[string]any{
		"snapd":   {"status": "active", "version": "1.0"},
		"firefox": {"status": "inactive", "version": "9.0"},
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "snapd.status")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "active")

	value, err = view.Get(databag, "snapd")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]any{"status": "active"})
}

func (s *viewSuite) TestViewUnsetValidates(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"test": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
			},
		},
	}, &failingSchema{err: errors.New("boom")})
	c.Assert(err, IsNil)

	view := schema.View("test")
	err = view.Unset(databag, "foo")
	c.Assert(err, ErrorMatches, `cannot unset data: boom`)
}

func (s *viewSuite) TestViewUnsetSkipsReadOnly(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"test": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo", "access": "read"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("test")
	err = view.Unset(databag, "foo")
	c.Assert(err, ErrorMatches, `cannot unset "foo" through acc/confdb/test: no matching rule`)
}

func (s *viewSuite) TestViewGetNoMatchRequestLongerThanPattern(c *C) {
	databag := confdb.NewJSONDatabag()
	db, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"statuses": map[string]any{
			"rules": []any{
				map[string]any{"request": "snapd", "storage": "snaps.snapd"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := db.View("statuses")
	err = view.Set(databag, "snapd", map[string]any{
		"status": "active", "version": "1.0",
	})
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "snapd.status")
	c.Assert(err, testutil.ErrorIs, &confdb.NoMatchError{})
}

func (s *viewSuite) TestViewManyPrefixMatches(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"statuses": map[string]any{
			"rules": []any{
				map[string]any{"request": "status.firefox", "storage": "snaps.firefox.status"},
				map[string]any{"request": "status.snapd", "storage": "snaps.snapd.status"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("statuses")
	err = view.Set(databag, "status.firefox", "active")
	c.Assert(err, IsNil)

	err = view.Set(databag, "status.snapd", "disabled")
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "status")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals,
		map[string]any{
			"snapd":   "disabled",
			"firefox": "active",
		})
}

func (s *viewSuite) TestViewCombineNamespacesInPrefixMatches(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"statuses": map[string]any{
			"rules": []any{
				map[string]any{"request": "status.foo.bar.firefox", "storage": "snaps.firefox.status"},
				map[string]any{"request": "status.foo.snapd", "storage": "snaps.snapd.status"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set(parsePath(c, "snaps"), map[string]any{
		"firefox": map[string]any{
			"status": "active",
		},
		"snapd": map[string]any{
			"status": "disabled",
		},
	})
	c.Assert(err, IsNil)

	view := schema.View("statuses")

	value, err := view.Get(databag, "status")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals,
		map[string]any{
			"foo": map[string]any{
				"bar": map[string]any{
					"firefox": "active",
				},
				"snapd": "disabled",
			},
		})
}

func (s *viewSuite) TestGetScalarOverwritesLeafOfMapValue(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"motors": map[string]any{
			"rules": []any{
				map[string]any{"request": "motors.a.speed", "storage": "new-speed.a"},
				map[string]any{"request": "motors", "storage": "motors"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set(parsePath(c, "motors"), map[string]any{
		"a": map[string]any{
			"speed": 100,
		},
	})
	c.Assert(err, IsNil)

	err = databag.Set(parsePath(c, "new-speed"), map[string]any{
		"a": 101.5,
	})
	c.Assert(err, IsNil)

	view := schema.View("motors")

	value, err := view.Get(databag, "motors")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]any{"a": map[string]any{"speed": 101.5}})
}

func (s *viewSuite) TestGetSingleScalarOk(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set(parsePath(c, "foo"), "bar")
	c.Assert(err, IsNil)

	view := schema.View("foo")

	value, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, "bar")
}

func (s *viewSuite) TestGetMatchScalarAndMapError(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "bar"},
				map[string]any{"request": "foo.baz", "storage": "baz"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set(parsePath(c, "bar"), 1)
	c.Assert(err, IsNil)

	err = databag.Set(parsePath(c, "baz"), 2)
	c.Assert(err, IsNil)

	view := schema.View("foo")

	_, err = view.Get(databag, "foo")
	c.Assert(err, ErrorMatches, `cannot merge results of different types float64, map\[string\]interface {}`)
}

func (s *viewSuite) TestGetRulesAreSortedByParentage(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo.bar.baz", "storage": "third"},
				map[string]any{"request": "foo", "storage": "first"},
				map[string]any{"request": "foo.bar", "storage": "second"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	view := schema.View("foo")

	err = databag.Set(parsePath(c, "first"), map[string]any{"bar": map[string]any{"baz": "first"}})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	// returned the value read by entry "foo"
	c.Assert(value, DeepEquals, map[string]any{"bar": map[string]any{"baz": "first"}})

	err = databag.Set(parsePath(c, "second"), map[string]any{"baz": "second"})
	c.Assert(err, IsNil)

	value, err = view.Get(databag, "foo")
	c.Assert(err, IsNil)
	// the leaf is replaced by a value read from a rule that is nested
	c.Assert(value, DeepEquals, map[string]any{"bar": map[string]any{"baz": "second"}})

	err = databag.Set(parsePath(c, "third"), "third")
	c.Assert(err, IsNil)

	value, err = view.Get(databag, "foo")
	c.Assert(err, IsNil)
	// lastly, it reads the value from "foo.bar.baz" the most nested entry
	c.Assert(value, DeepEquals, map[string]any{"bar": map[string]any{"baz": "third"}})
}

func (s *viewSuite) TestGetUnmatchedPlaceholderReturnsAll(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"snaps": map[string]any{
			"rules": []any{
				map[string]any{"request": "snaps.{snap}", "storage": "snaps.{snap}"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	view := schema.View("snaps")
	c.Assert(view, NotNil)

	err = databag.Set(parsePath(c, "snaps"), map[string]any{
		"snapd": 1,
		"foo": map[string]any{
			"bar": 2,
		},
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "snaps")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]any{"snapd": float64(1), "foo": map[string]any{"bar": float64(2)}})
}

func (s *viewSuite) TestGetUnmatchedPlaceholdersWithNestedValues(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"statuses": map[string]any{
			"rules": []any{
				map[string]any{"request": "snaps.{snap}.status", "storage": "snaps.{snap}.status"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	view := schema.View("statuses")
	c.Assert(view, NotNil)

	err = databag.Set(parsePath(c, "snaps"), map[string]any{
		"snapd": map[string]any{
			"status": "active",
		},
		"foo": map[string]any{
			"version": 2,
		},
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "snaps")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]any{"snapd": map[string]any{"status": "active"}})
}

func (s *viewSuite) TestGetSeveralUnmatchedPlaceholders(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a.{b}.c.{d}.e", "storage": "a.{b}.c.{d}.e"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = databag.Set(parsePath(c, "a"), map[string]any{
		"b1": map[string]any{
			"c": map[string]any{
				// the request can be fulfilled here
				"d1": map[string]any{
					"e": "end",
					"f": "not-included",
				},
				"d2": "f",
			},
			"x": 1,
		},
		"b2": map[string]any{
			"c": map[string]any{
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
	expected := map[string]any{
		"b1": map[string]any{
			"c": map[string]any{
				"d1": map[string]any{
					"e": "end",
				},
			},
		},
	}
	c.Assert(value, DeepEquals, expected)
}

func (s *viewSuite) TestGetMergeAtDifferentLevels(c *C) {
	databag := confdb.NewJSONDatabag()
	confdb, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a.{b}.c.{d}.e", "storage": "a.{b}.c.{d}.e"},
				map[string]any{"request": "a.{b}.c.{d}", "storage": "a.{b}.c.{d}"},
				map[string]any{"request": "a.{b}", "storage": "a.{b}"},
				map[string]any{"request": "a", "storage": "a"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	view := confdb.View("foo")
	c.Assert(view, NotNil)

	err = databag.Set(parsePath(c, "a"), map[string]any{
		"b": map[string]any{
			"c": map[string]any{
				"d": map[string]any{
					"e": "end",
				},
			},
		},
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "a")
	c.Assert(err, IsNil)
	expected := map[string]any{
		"b": map[string]any{
			"c": map[string]any{
				"d": map[string]any{
					"e": "end",
				},
			},
		},
	}
	c.Assert(value, DeepEquals, expected)
}

func (s *viewSuite) TestBadRequestPaths(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a.{b}.c", "storage": "a.{b}.c"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = databag.Set(parsePath(c, "a"), map[string]any{
		"b": map[string]any{
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
			request: "a[",
			errMsg:  "invalid subkey \"[\"",
		},
		{
			request: "a[b",
			errMsg:  "invalid subkey \"[b\"",
		},
		{
			request: "a[b.c",
			errMsg:  "invalid subkey \"[b\"",
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
			errMsg:  `invalid subkey "{b}": path only supports literal keys and indexes`,
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
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot set %q through confdb view acc/confdb/foo: %s`, tc.request, tc.errMsg), cmt)

		_, err = view.Get(databag, tc.request)
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot get %q through confdb view acc/confdb/foo: %s`, tc.request, tc.errMsg), cmt)

		err = view.Unset(databag, tc.request)
		c.Assert(err, NotNil, cmt)
		c.Assert(err.Error(), Equals, fmt.Sprintf(`cannot unset %q through confdb view acc/confdb/foo: %s`, tc.request, tc.errMsg), cmt)
	}

	cmt := Commentf("last test case failed")
	err = view.Set(databag, "", "value")
	c.Assert(err, NotNil, cmt)
	c.Assert(err.Error(), Equals, `cannot set empty path through confdb view acc/confdb/foo`, cmt)
	c.Assert(err, testutil.ErrorIs, &confdb.BadRequestError{}, cmt)
}

func (s *viewSuite) TestSetAllowedOnSameRequestButDifferentPaths(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a.b.c", "storage": "new", "access": "write"},
				map[string]any{"request": "a.b.c", "storage": "old", "access": "write"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "a.b.c", "value")
	c.Assert(err, IsNil)

	stored, err := databag.Get(parsePath(c, "old"))
	c.Assert(err, IsNil)
	c.Assert(stored, Equals, "value")

	stored, err = databag.Get(parsePath(c, "new"))
	c.Assert(err, IsNil)
	c.Assert(stored, Equals, "value")
}

func (s *viewSuite) TestSetWritesToMoreNestedLast(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		// purposefully unordered to check that Set doesn't depend on well-ordered entries in assertions
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "snaps.snapd.name", "storage": "snaps.snapd.name"},
				map[string]any{"request": "snaps.snapd", "storage": "snaps.snapd"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "snaps.snapd", map[string]any{
		"name": "snapd",
	})
	c.Assert(err, IsNil)

	val, err := databag.Get(parsePath(c, "snaps"))
	c.Assert(err, IsNil)

	c.Assert(val, DeepEquals, map[string]any{
		"snapd": map[string]any{
			"name": "snapd",
		},
	})
}

func (s *viewSuite) TestReadWriteRead(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a.b.c", "storage": "a.b.c"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	initData := map[string]any{
		"b": map[string]any{
			"c": "end",
		},
	}

	err = databag.Set(parsePath(c, "a"), initData)
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
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a.b.c", "storage": "a.b.c"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	view := schema.View("foo")
	c.Assert(view, NotNil)

	initialData := map[string]any{
		"b": map[string]any{
			"c": "end",
		},
	}

	path := parsePath(c, "a")
	err = databag.Set(path, initialData)
	c.Assert(err, IsNil)

	for _, req := range []string{"a", "a.b", "a.b.c"} {
		val, err := view.Get(databag, req)
		c.Assert(err, IsNil)

		err = view.Set(databag, req, val)
		c.Assert(err, IsNil)
	}

	data, err := databag.Get(path)
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, initialData)
}

func (s *viewSuite) TestSetValueMissingNestedLevels(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a.b", "storage": "a.b"},
				map[string]any{"request": "b[{n}]", "storage": "b[{n}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "a", "foo")
	c.Assert(err, ErrorMatches, `cannot set "a" through confdb view acc/confdb/foo: expected map for unmatched request parts but got string`)

	err = view.Set(databag, "a", map[string]any{"c": "foo"})
	c.Assert(err, ErrorMatches, `cannot set "a" through confdb view acc/confdb/foo: cannot use unmatched part "b" as key in map\[c:foo\]`)

	err = view.Set(databag, "b", "foo")
	c.Assert(err, ErrorMatches, `cannot set "b" through confdb view acc/confdb/foo: expected list for unmatched request parts but got string`)
}

func (s *viewSuite) TestGetReadsStorageLessNestedNamespaceBefore(c *C) {
	// Get reads by order of namespace (not path) nestedness. This test explicitly
	// tests for this and showcases why it matters. In Get we care about building
	// a virtual document from locations in the storage that may evolve over time.
	// In this example, the storage evolve to have version data in a different place
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "snaps.snapd", "storage": "snaps.snapd"},
				map[string]any{"request": "snaps.snapd.version", "storage": "anewversion"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set(parsePath(c, "snaps"), map[string]any{
		"snapd": map[string]any{
			"version": 1,
		},
	})
	c.Assert(err, IsNil)

	err = databag.Set(parsePath(c, "anewversion"), 2)
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	data, err := view.Get(databag, "snaps")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]any{
		"snapd": map[string]any{
			"version": float64(2),
		},
	})
}

func (s *viewSuite) TestSetValidateError(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "bar", "storage": "bar"},
			},
		},
	}, &failingSchema{err: errors.New("expected error")})
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "bar", "baz")
	c.Assert(err, ErrorMatches, "cannot write data: expected error")
}

func (s *viewSuite) TestSetOverwriteValueWithNewLevel(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a", "storage": "a"},
				map[string]any{"request": "c", "storage": "c"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	view := schema.View("foo")

	// Note: this shouldn't be possible if the rules haven't changed but let's be
	// robust in case a confdb-schema is evolved to add nested values to previous paths
	bag := confdb.NewJSONDatabag()
	err = view.Set(bag, "a", "foo")
	c.Assert(err, IsNil)

	err = view.Set(bag, "c", []any{"bar"})
	c.Assert(err, IsNil)

	// we publish a new schema adding some nesting to our rules
	schema, err = confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a.b", "storage": "a.b"},
				map[string]any{"request": "c[{n}][{m}]", "storage": "c[{n}][{m}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	view = schema.View("foo")

	// we can overwrite existing scalar values with nested maps and lists
	err = view.Set(bag, "a.b", "foo")
	c.Assert(err, IsNil)

	err = view.Set(bag, "c[0][0]", "bar")
	c.Assert(err, IsNil)

	data, err := view.Get(bag, "")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]any{
		"a": map[string]any{
			"b": "foo",
		},
		"c": []any{[]any{"bar"}},
	})
}

func (s *viewSuite) TestSetValidatesDataWithSchemaPass(c *C) {
	schema, err := confdb.ParseStorageSchema([]byte(`{
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
		"foo": "${int-map}",
		"bar": "${str-array}"
	}
}`))
	c.Assert(err, IsNil)

	databag := confdb.NewJSONDatabag()
	confdbSchema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
				map[string]any{"request": "bar", "storage": "bar"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)

	view := confdbSchema.View("foo")
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
		value     any
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

			schema, err := confdb.ParseStorageSchema([]byte(fmt.Sprintf(`{
	"schema": {
		"foo": %s,
		"bar": %s
	}
}`, one.schemaStr, other.schemaStr)))
			c.Assert(err, IsNil)

			_, err = confdb.NewSchema("acc", "confdb", map[string]any{
				"foo": map[string]any{
					"rules": []any{
						map[string]any{"request": "foo", "storage": "foo", "access": "write"},
						map[string]any{"request": "foo", "storage": "bar", "access": "write"},
					},
				},
			}, schema)
			c.Assert(err, ErrorMatches, fmt.Sprintf(`.*storage paths "foo" and "bar" for request "foo" require incompatible types: %s != %s`, one.typ, other.typ))
		}
	}
}

func (s *viewSuite) TestSetPreCheckValueAllowsIntNumberMismatch(c *C) {
	schema, err := confdb.ParseStorageSchema([]byte(`{
	"schema": {
		"foo": "int",
		"bar": "number"
	}
}`))
	c.Assert(err, IsNil)

	databag := confdb.NewJSONDatabag()
	confdbSchema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo", "access": "write"},
				map[string]any{"request": "foo", "storage": "bar", "access": "write"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)

	view := confdbSchema.View("foo")
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
	schema, err := confdb.ParseStorageSchema(schemaStr)
	c.Assert(err, IsNil)

	_, err = confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo", "access": "write"},
				map[string]any{"request": "foo", "storage": "bar", "access": "write"},
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
	schema, err := confdb.ParseStorageSchema(schemaStr)
	c.Assert(err, IsNil)

	confdbSchema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo.b.c", "access": "write"},
				map[string]any{"request": "foo", "storage": "bar.b.c", "access": "write"},
			},
		},
	}, schema)
	c.Assert(err, ErrorMatches, `.*storage path "foo.b.c" for request "foo" is invalid after "foo": cannot follow path beyond "int" type`)
	c.Assert(confdbSchema, IsNil)
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
	schema, err := confdb.ParseStorageSchema(schemaStr)
	c.Assert(err, IsNil)

	_, err = confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "snaps.{snap}", "storage": "snaps.{snap}"},
				map[string]any{"request": "snaps.{snap}.status", "storage": "snaps.{snap}.status"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)
}

func (*viewSuite) TestSchemaMismatchPlaceholder(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"schema": {
				"a": "string",
				"b": {
					"type": "array",
					"values": "string"
				}
			}
		},
		"baz": {
			"schema": {
				"a": "string",
				"b": "int"
			}
		}
	}
}`)
	schema, err := confdb.ParseStorageSchema(schemaStr)
	c.Assert(err, IsNil)

	_, err = confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo.{bar}[{n}]", "storage": "foo.{bar}[{n}]"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)

	schemaStr = []byte(`{
	"schema": {
		"baz": {
			"schema": {
				"b": "int"
			}
		}
	}
}`)
	schema, err = confdb.ParseStorageSchema(schemaStr)
	c.Assert(err, IsNil)

	_, err = confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "baz.{bar}[{n}]", "storage": "baz.{bar}[{n}]"},
			},
		},
	}, schema)
	c.Assert(err, ErrorMatches, `.*storage path "baz.{bar}\[{n}\]" for request "baz.{bar}\[{n}\]" is invalid after "baz.{bar}": cannot follow path beyond "int" type`)
}

func (*viewSuite) TestSchemaMismatchCheckMultipleAlternativeTypesHappy(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": ["int", "bool"],
		"bar": ["string", "bool"]
	}
}`)
	schema, err := confdb.ParseStorageSchema(schemaStr)
	c.Assert(err, IsNil)

	databag := confdb.NewJSONDatabag()
	confdbSchema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo", "access": "write"},
				map[string]any{"request": "foo", "storage": "bar", "access": "write"},
			},
		},
	}, schema)
	c.Assert(err, IsNil)

	view := confdbSchema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", true)
	c.Assert(err, IsNil)
}

func (s *viewSuite) TestSetUnmatchedPlaceholderLeaf(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo.{bar}", "storage": "foo.{bar}"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]any{
		"bar": "value",
		"baz": "other",
	})
	c.Assert(err, IsNil)

	data, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]any{
		"bar": "value",
		"baz": "other",
	})
}

func (s *viewSuite) TestSetUnmatchedPlaceholderMidPath(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo.{bar}.nested", "storage": "foo.{bar}.nested"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]any{
		"bar": map[string]any{"nested": "value"},
		"baz": map[string]any{"nested": "other"},
	})
	c.Assert(err, IsNil)

	data, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]any{
		"bar": map[string]any{"nested": "value"},
		"baz": map[string]any{"nested": "other"},
	})
}

func (s *viewSuite) TestSetManyUnmatchedPlaceholders(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo.{bar}.a.{baz}", "storage": "foo.{bar}.{baz}"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]any{
		"a": map[string]any{"a": map[string]any{
			"c": "value",
			"d": "other",
		}},
		"b": map[string]any{"a": map[string]any{
			"e": "value",
			"f": "other",
		}},
	})
	c.Assert(err, IsNil)

	data, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(data, DeepEquals, map[string]any{
		"a": map[string]any{"a": map[string]any{
			"c": "value",
			"d": "other",
		}},
		"b": map[string]any{"a": map[string]any{
			"e": "value",
			"f": "other",
		}},
	})
}

func (s *viewSuite) TestUnsetUnmatchedPlaceholderLast(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo.{bar}", "storage": "foo.{bar}"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]any{
		"bar": "value",
		"baz": "other",
	})
	c.Assert(err, IsNil)

	err = view.Unset(databag, "foo")
	c.Assert(err, IsNil)

	_, err = view.Get(databag, "foo")
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})
	c.Assert(err, ErrorMatches, `cannot get "foo" through acc/confdb/foo: no data`)
}

func (s *viewSuite) TestUnsetUnmatchedPlaceholderMid(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "all.{bar}", "storage": "foo.{bar}"},
				map[string]any{"request": "one.{bar}", "storage": "foo.{bar}.one"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "all", map[string]any{
		// should remove only the "one" path
		"a": map[string]any{
			"one": "value",
			"two": "other",
		},
		// the nested value should be removed, leaving an empty map
		"b": map[string]any{
			"one": "value",
		},
		// should be untouched (no "one" path)
		"c": map[string]any{
			"two": "value",
		},
	})
	c.Assert(err, IsNil)

	err = view.Unset(databag, "one")
	c.Assert(err, IsNil)

	val, err := view.Get(databag, "all")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, map[string]any{
		"a": map[string]any{
			"two": "other",
		},
		"b": map[string]any{},
		"c": map[string]any{
			"two": "value",
		},
	})
}

func (s *viewSuite) TestIndexPlaceholders(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a[{n}]", "storage": "b[{n}]"},
				map[string]any{"request": "a[{n}].c", "storage": "b[{n}].c"},
				map[string]any{"request": "a[{n}][{m}]", "storage": "b[{m}][{n}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	c.Assert(schema, NotNil)
}

func (s *viewSuite) TestGetValuesThroughPaths(c *C) {
	type testcase struct {
		path     string
		suffix   string
		value    any
		expected map[string]any
		err      string
	}

	tcs := []testcase{
		{
			path:     "foo.bar",
			value:    "value",
			expected: map[string]any{"foo.bar": "value"},
		},
		{
			path:     "foo.{bar}",
			suffix:   "{bar}",
			value:    map[string]any{"a": "value", "b": "other"},
			expected: map[string]any{"foo.a": "value", "foo.b": "other"},
		},
		{
			path:   "foo.{bar}.baz",
			suffix: "{bar}.baz",
			value: map[string]any{
				"a": map[string]any{"baz": "value"},
				"b": map[string]any{"baz": "other"},
			},
			expected: map[string]any{"foo.a.baz": "value", "foo.b.baz": "other"},
		},
		{
			path:   "foo.{bar}.{baz}.last",
			suffix: "{bar}.{baz}",
			value: map[string]any{
				"a": map[string]any{"b": "value"},
				"c": map[string]any{"d": "other"},
			},
			expected: map[string]any{"foo.a.b.last": "value", "foo.c.d.last": "other"},
		},

		{
			path:   "foo.{bar}",
			suffix: "{bar}.baz",
			value: map[string]any{
				"a": map[string]any{"baz": "value", "ignore": 1},
				"b": map[string]any{"baz": "other", "ignore": 1},
			},
			expected: map[string]any{"foo.a": "value", "foo.b": "other"},
		},
		{
			path:   "foo.{bar}",
			suffix: "{bar}",
			value:  "a",
			err:    "expected map for unmatched request parts but got string",
		},
		{
			path:   "foo.{bar}",
			suffix: "{bar}.baz",
			value: map[string]any{
				"a": map[string]any{"notbaz": 1},
				"b": map[string]any{"notbaz": 1},
			},
			err: `cannot use unmatched part "baz" as key in map\[notbaz:1\]`,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("failed test number %d", i+1)
		opts := confdb.ParseOptions{AllowPlaceholders: true}
		suffix, err := confdb.ParsePathIntoAccessors(tc.suffix, opts)
		c.Assert(err, IsNil)

		path, err := confdb.ParsePathIntoAccessors(tc.path, opts)
		c.Assert(err, IsNil)

		pathValuePairs, err := confdb.GetValuesThroughPaths(path, suffix, tc.value)

		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err, cmt)
			c.Check(pathValuePairs, IsNil, cmt)
		} else {
			pathsToValues := confdb.PathValuePairsIntoMap(pathValuePairs)
			c.Check(err, IsNil, cmt)
			c.Check(pathsToValues, DeepEquals, tc.expected, cmt)
		}
	}
}

func (s *viewSuite) TestViewSetErrorIfValueContainsUnusedParts(c *C) {
	type testcase struct {
		request string
		value   any
		err     string
	}

	tcs := []testcase{
		{
			request: "a",
			value: map[string]any{
				"b": map[string]any{"d": "value", "u": 1},
			},
			err: `cannot set "a" through confdb view acc/confdb/foo: value contains unused data: map[b:map[u:1]]`,
		},
		{
			request: "a",
			value: map[string]any{
				"b": map[string]any{"d": "value", "u": 1},
				"c": map[string]any{"d": "value"},
			},
			err: `cannot set "a" through confdb view acc/confdb/foo: value contains unused data: map[b:map[u:1]]`,
		},
		{
			request: "b",
			value: map[string]any{
				"e": []any{"a"},
				"f": 1,
			},
			err: `cannot set "b" through confdb view acc/confdb/foo: value contains unused data: map[e:[a]]`,
		},
		{
			request: "c",
			value: map[string]any{
				"d": map[string]any{
					"e": map[string]any{
						"f": "value",
					},
					"f": 1,
				},
			},
			err: `cannot set "c" through confdb view acc/confdb/foo: value contains unused data: map[d:map[f:1]]`,
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("failed test number %d", i+1)
		databag := confdb.NewJSONDatabag()
		schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
			"foo": map[string]any{
				"rules": []any{
					map[string]any{"request": "a.{x}.d", "storage": "a.{x}"},
					map[string]any{"request": "c.d.e.f", "storage": "d"},
					map[string]any{"request": "b.f", "storage": "b.f"},
				},
			},
		}, confdb.NewJSONSchema())
		c.Assert(err, IsNil)

		view := schema.View("foo")
		c.Assert(view, NotNil)

		err = view.Set(databag, tc.request, tc.value)
		if tc.err != "" {
			c.Check(err.Error(), Equals, tc.err, cmt)
		} else {
			c.Check(err, IsNil, cmt)
		}
	}
}

func (*viewSuite) TestViewSummaryWrongType(c *C) {
	for _, val := range []any{
		1,
		true,
		[]any{"foo"},
		map[string]any{"foo": "bar"},
	} {
		schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
			"foo": map[string]any{
				"summary": val,
				"rules": []any{
					map[string]any{"request": "foo", "storage": "foo"},
				},
			},
		}, nil)
		c.Check(err.Error(), Equals, fmt.Sprintf(`cannot define view "foo": view summary must be a string but got %T`, val))
		c.Check(schema, IsNil)
	}
}

func (*viewSuite) TestViewSummary(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"summary": "some summary of the view",
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	c.Assert(schema, NotNil)
}

func (s *viewSuite) TestGetEntireView(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo.{bar}", "storage": "foo-path.{bar}"},
				map[string]any{"request": "abc", "storage": "abc-path"},
				map[string]any{"request": "write-only", "storage": "write-only", "access": "write"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]any{
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

	c.Assert(result, DeepEquals, map[string]any{
		"foo": map[string]any{
			"bar": "value",
			"baz": "other",
		},
		"abc": "cba",
	})
}

func (*viewSuite) TestViewContentRule(c *C) {
	views := map[string]any{
		"bar": map[string]any{
			"rules": []any{
				map[string]any{
					"request": "a",
					"storage": "c",
					"content": []any{
						map[string]any{
							"request": "b",
							"storage": "d",
						},
					},
				},
			},
		},
	}

	schema, err := confdb.NewSchema("acc", "foo", views, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	databag := confdb.NewJSONDatabag()
	err = databag.Set(parsePath(c, "c.d"), "value")
	c.Assert(err, IsNil)

	view := schema.View("bar")
	val, err := view.Get(databag, "a.b")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "value")

	err = view.Set(databag, "a.b", "other")
	c.Assert(err, IsNil)

	val, err = view.Get(databag, "a.b")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "other")
}

func (*viewSuite) TestContentInheritsAccess(c *C) {
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
			getErr: `cannot get "foo.bar" through acc/confdb/foo: no data`,
			setErr: `cannot set "foo.bar" through acc/confdb/foo: no matching rule`,
		},
		{
			access: "write",
			getErr: `cannot get "foo.bar" through acc/confdb/foo: no matching rule`,
		},
	} {
		cmt := Commentf("sub-test with %q access failed", t.access)
		databag := confdb.NewJSONDatabag()
		schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
			"foo": map[string]any{
				"rules": []any{
					map[string]any{
						"request": "foo",
						"storage": "foo",
						"access":  t.access,
						"content": []any{
							map[string]any{
								"storage": "bar",
							}}},
				},
			},
		}, confdb.NewJSONSchema())
		c.Assert(err, IsNil)

		view := schema.View("foo")
		err = view.Set(databag, "foo.bar", "thing")
		if t.setErr != "" {
			c.Assert(err, NotNil)
			c.Assert(err.Error(), Equals, t.setErr, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
		}

		_, err = view.Get(databag, "foo.bar")
		if t.getErr != "" {
			c.Assert(err, NotNil)
			c.Assert(err.Error(), Equals, t.getErr, cmt)
		} else {
			c.Assert(err, IsNil, cmt)
		}
	}
}

func (*viewSuite) TestInheritedAccessInSeveralNestedContents(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{
					"storage": "foo",
					"access":  "read",
					"content": []any{
						map[string]any{
							"storage": "bar",
							"content": []any{
								map[string]any{
									"storage": "baz",
								}}}}}}},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	err = databag.Set(parsePath(c, "foo.bar.baz"), "abc")
	c.Assert(err, IsNil)

	view := schema.View("foo")
	val, err := view.Get(databag, "foo.bar.baz")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "abc")

	err = view.Set(databag, "foo.bar.baz", "abc")
	c.Assert(err, ErrorMatches, `cannot set "foo.bar.baz" through acc/confdb/foo: no matching rule`)
}

func (*viewSuite) TestContentForbidsOverridingAccess(c *C) {
	for _, acc := range []string{"", "read-write", "read", "write"} {
		_, err := confdb.NewSchema("acc", "confdb", map[string]any{
			"foo": map[string]any{
				"rules": []any{
					map[string]any{
						"request": "foo",
						"storage": "foo",
						"access":  acc,
						"content": []any{
							map[string]any{
								"storage": "bar",
								"access":  "read",
							}}},
				},
			},
		}, confdb.NewJSONSchema())
		c.Assert(err, ErrorMatches, `cannot define view "foo": cannot override "access" in nested "content" rule: "content" rules inherit parent "access"`)
	}
}

func (*viewSuite) TestViewInvalidContentRules(c *C) {
	type testcase struct {
		content any
		err     string
	}

	tcs := []testcase{
		{
			content: []any{},
			err:     `.*"content" must be a non-empty list`,
		},
		{
			content: map[string]any{},
			err:     `.*"content" must be a non-empty list`,
		},
		{
			content: []any{map[string]any{"request": "a"}},
			err:     `.*view rules must have a "storage" field`,
		},
	}

	for _, tc := range tcs {
		rules := map[string]any{
			"bar": map[string]any{
				"rules": []any{
					map[string]any{
						"request": "a",
						"storage": "c",
						"content": tc.content,
					},
				},
			},
		}

		_, err := confdb.NewSchema("acc", "foo", rules, confdb.NewJSONSchema())
		c.Assert(err, ErrorMatches, tc.err)
	}
}

func (*viewSuite) TestViewSeveralNestedContentRules(c *C) {
	rules := map[string]any{
		"bar": map[string]any{
			"rules": []any{
				map[string]any{
					"request": "a",
					"storage": "a",
					"content": []any{
						map[string]any{
							"request": "b.c",
							"storage": "b.c",
							"content": []any{
								map[string]any{
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

	schema, err := confdb.NewSchema("acc", "foo", rules, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	databag := confdb.NewJSONDatabag()
	view := schema.View("bar")
	err = view.Set(databag, "a.b.c.d", "value")
	c.Assert(err, IsNil)

	val, err := view.Get(databag, "a.b.c.d")
	c.Assert(err, IsNil)
	c.Assert(val, Equals, "value")
}

func (*viewSuite) TestViewInvalidMapKeys(c *C) {
	schema, err := confdb.NewSchema("acc", "foo", map[string]any{
		"bar": map[string]any{
			"rules": []any{
				map[string]any{
					"request": "foo",
					"storage": "foo",
				},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	databag := confdb.NewJSONDatabag()
	view := schema.View("bar")

	type testcase struct {
		value      any
		invalidKey string
	}

	tcs := []testcase{
		{
			value:      map[string]any{"-foo": 2},
			invalidKey: "-foo",
		},
		{
			value:      map[string]any{"foo--bar": 2},
			invalidKey: "foo--bar",
		},
		{
			value:      map[string]any{"foo-": 2},
			invalidKey: "foo-",
		},
		{
			value:      map[string]any{"foo": map[string]any{"-bar": 2}},
			invalidKey: "-bar",
		},
		{
			value:      map[string]any{"foo": map[string]any{"bar": map[string]any{"baz-": 2}}},
			invalidKey: "baz-",
		},
		{
			value:      []any{map[string]any{"foo": 2}, map[string]any{"bar-": 2}},
			invalidKey: "bar-",
		},
		{
			value:      []any{nil, map[string]any{"bar-": 2}},
			invalidKey: "bar-",
		},
		{
			value:      map[string]any{"foo": nil, "bar": map[string]any{"-baz": 2}},
			invalidKey: "-baz",
		},
	}

	for _, tc := range tcs {
		cmt := Commentf("expected invalid key err for value: %v", tc.value)
		err = view.Set(databag, "foo", tc.value)
		c.Assert(err, ErrorMatches, fmt.Sprintf("cannot set \"foo\" through confdb view acc/foo/bar: key %q doesn't conform to required format: .*", tc.invalidKey), cmt)
	}
}

func (s *viewSuite) TestSetUsingMapWithNilValuesAtLeaves(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
				map[string]any{"request": "foo.a", "storage": "foo.a"},
				map[string]any{"request": "foo.b", "storage": "foo.b"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]any{
		"a": "value",
		"b": "other",
	})
	c.Assert(err, IsNil)

	err = view.Set(databag, "foo", map[string]any{
		"a": nil,
		"b": nil,
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]any{})
}

func (s *viewSuite) TestSetWithMultiplePathsNestedAtLeaves(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo.a", "storage": "foo.a"},
				map[string]any{"request": "foo.b", "storage": "foo.b"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]any{
		"a": map[string]any{
			"c": "value",
			"d": "other",
		},
		"b": "other",
	})
	c.Assert(err, IsNil)

	err = view.Set(databag, "foo", map[string]any{
		"a": map[string]any{
			"d": nil,
		},
		"b": nil,
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	c.Assert(value, DeepEquals, map[string]any{
		// consistent with the previous configuration mechanism
		"a": map[string]any{},
	})
}

func (s *viewSuite) TestSetWithNilAndNonNilLeaves(c *C) {
	databag := confdb.NewJSONDatabag()
	schema, err := confdb.NewSchema("acc", "db", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "foo", "storage": "foo"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	view := schema.View("foo")
	c.Assert(view, NotNil)

	err = view.Set(databag, "foo", map[string]any{
		"a": "value",
		"b": "other",
	})
	c.Assert(err, IsNil)

	err = view.Set(databag, "foo", map[string]any{
		"a": nil,
		"c": "value",
	})
	c.Assert(err, IsNil)

	value, err := view.Get(databag, "foo")
	c.Assert(err, IsNil)
	// nil values aren't stored but non-nil values are
	c.Assert(value, DeepEquals, map[string]any{
		"c": "value",
	})
}

func (*viewSuite) TestSetEnforcesNestednessLimit(c *C) {
	restore := confdb.MockMaxValueDepth(2)
	defer restore()

	schema, err := confdb.NewSchema("acc", "foo", map[string]any{
		"bar": map[string]any{
			"rules": []any{
				map[string]any{
					"request": "foo",
					"storage": "foo",
				},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	databag := confdb.NewJSONDatabag()
	view := schema.View("bar")

	err = view.Set(databag, "foo", map[string]any{
		"bar": "baz",
	})
	c.Assert(err, IsNil)

	err = view.Set(databag, "foo", map[string]any{
		"bar": map[string]any{
			"baz": "value",
		},
	})
	c.Assert(err, ErrorMatches, `cannot set "foo" through confdb view acc/foo/bar: value cannot have more than 2 nested levels`)
}

func (*viewSuite) TestGetAffectedViews(c *C) {
	type testcase struct {
		views    map[string]any
		affected []string
		modified string
	}

	tcs := []testcase{
		{
			// same path
			views: map[string]any{
				"view-1": map[string]any{
					"rules": []any{
						map[string]any{"request": "a", "storage": "a"},
					},
				},
			},
			affected: []string{"view-1"},
			modified: "a",
		},
		{
			// view path is more specific
			views: map[string]any{
				"view-1": map[string]any{
					"rules": []any{
						map[string]any{"request": "a", "storage": "a.b"},
					},
				},
			},
			affected: []string{"view-1"},
			modified: "a",
		},
		{
			// view path is more generic
			views: map[string]any{
				"view-1": map[string]any{
					"rules": []any{
						map[string]any{"request": "a", "storage": "a"},
					},
				},
			},
			affected: []string{"view-1"},
			modified: "a.b",
		},
		{
			// unrelated
			views: map[string]any{
				"view-1": map[string]any{
					"rules": []any{
						map[string]any{"request": "a", "storage": "a"},
					},
				},
			},
			modified: "b",
		},
		{
			// partially shared path but diverges at the end
			views: map[string]any{
				"view-1": map[string]any{
					"rules": []any{
						map[string]any{"request": "a", "storage": "a.b"},
					},
				},
			},
			modified: "a.c",
		},
		{
			// view path contains placeholder
			views: map[string]any{
				"view-1": map[string]any{
					"rules": []any{
						map[string]any{"request": "a.{x}", "storage": "a.{x}.c"},
					},
				},
			},
			affected: []string{"view-1"},
			modified: "a.b",
		},
		{
			// view path ends in placeholder
			views: map[string]any{
				"view-1": map[string]any{
					"rules": []any{
						map[string]any{"request": "a.{x}", "storage": "a.{x}"},
					},
				},
			},
			affected: []string{"view-1"},
			modified: "a.b",
		},
		{
			// path has placeholder but diverges after
			views: map[string]any{
				"view-1": map[string]any{
					"rules": []any{
						map[string]any{"request": "a.{x}", "storage": "a.{x}.b"},
					},
				},
			},
			modified: "a.b.c",
		},
		{
			// several affected views
			views: map[string]any{
				"view-1": map[string]any{
					"rules": []any{
						map[string]any{"request": "d", "storage": "d"},
					},
				},
				"view-2": map[string]any{
					"rules": []any{
						map[string]any{"request": "{x}.b", "storage": "{x}.b"},
						map[string]any{"request": "{x}.c", "storage": "{x}.c"},
					},
				},
				"view-3": map[string]any{
					"rules": []any{
						map[string]any{"request": "a", "storage": "a"},
					},
				},
			},
			affected: []string{"view-2", "view-3"},
			modified: "a.b",
		},
	}

	for i, tc := range tcs {
		cmt := Commentf("test %d out of %d failed (1-indexed)", (i + 1), len(tcs))
		schema, err := confdb.NewSchema("acc", "db", tc.views, confdb.NewJSONSchema())
		c.Assert(err, IsNil, cmt)

		modified := parsePath(c, tc.modified)
		affectedViews := schema.GetViewsAffectedByPath(modified)
		c.Assert(affectedViews, HasLen, len(tc.affected), cmt)

		viewNames := make([]string, 0, len(affectedViews))
		for _, v := range affectedViews {
			viewNames = append(viewNames, v.Name)
		}
		c.Assert(viewNames, testutil.DeepUnsortedMatches, tc.affected, cmt)
	}
}

func (*viewSuite) TestCheckReadEphemeralAccess(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"schema": {
				"bar": {
					"schema": {
						"baz": "string",
						"eph": {
							"type": "string",
							"ephemeral": true
						}
					}
				}
			}
		}
	}
}`)
	storageSchema, err := confdb.ParseStorageSchema(schemaStr)
	c.Assert(err, IsNil)

	schema, err := confdb.NewSchema("acc", "foo", map[string]any{
		"my-view": map[string]any{
			"rules": []any{
				map[string]any{
					"storage": "foo.bar",
					"content": []any{
						map[string]any{"storage": "baz"},
						map[string]any{"storage": "eph"},
					},
				},
				map[string]any{
					"request": "a.b",
					"storage": "foo.bar",
				},
			},
		},
	}, storageSchema)
	c.Assert(err, IsNil)

	type testcase struct {
		requests  []string
		ephemeral bool
		err       string
	}

	tcs := []testcase{
		{
			requests:  []string{"foo.bar.eph"},
			ephemeral: true,
		},
		{
			requests:  []string{"foo.bar"},
			ephemeral: true,
		},
		{
			requests:  []string{"foo.bar.baz"},
			ephemeral: false,
		},
		{
			requests:  []string{"a.b"},
			ephemeral: true,
		},
		{
			// matches all
			ephemeral: true,
		},
		{
			// partial matches are ok
			requests:  []string{"non.existent", "foo"},
			ephemeral: true,
		},
		{
			requests: []string{"non.existent"},
			err:      `cannot get "non.existent" through acc/foo/my-view: no matching rule`,
		},
		{
			requests: []string{"abc[12]", "mk"},
			err:      `cannot get "abc[12]", "mk" through acc/foo/my-view: no matching rule`,
		},
	}

	v := schema.View("my-view")
	for i, tc := range tcs {
		cmt := Commentf("failed test number %d", i+1)
		if tc.err != "" {
			_, err := v.ReadAffectsEphemeral(tc.requests)
			c.Assert(err, NotNil)
			c.Assert(err.Error(), Equals, tc.err, cmt)
		} else {
			eph, err := v.ReadAffectsEphemeral(tc.requests)
			c.Assert(err, IsNil, cmt)
			c.Assert(eph, Equals, tc.ephemeral, cmt)
		}
	}
}

func (*viewSuite) TestCheckWriteEphemeralAccess(c *C) {
	schemaStr := []byte(`{
	"schema": {
		"foo": {
			"schema": {
				"bar": {
					"schema": {
						"baz": "string",
						"eph": {
							"type": "string",
							"ephemeral": true
						}
					}
				}
			}
		}
	}
}`)
	storageSchema, err := confdb.ParseStorageSchema(schemaStr)
	c.Assert(err, IsNil)

	schema, err := confdb.NewSchema("acc", "foo", map[string]any{
		"my-view": map[string]any{
			"rules": []any{
				map[string]any{
					"storage": "foo.bar",
					"content": []any{
						map[string]any{"storage": "baz"},
						map[string]any{"storage": "eph"},
					},
				},
				map[string]any{
					"request": "a.b",
					"storage": "foo.bar",
				},
			},
		},
	}, storageSchema)
	c.Assert(err, IsNil)

	type testcase struct {
		requests  []string
		ephemeral bool
		err       string
	}

	tcs := []testcase{
		{
			requests:  []string{"foo.bar.eph"},
			ephemeral: true,
		},
		{
			requests:  []string{"foo.bar"},
			ephemeral: true,
		},
		{
			requests:  []string{"foo.bar.baz"},
			ephemeral: false,
		},
		{
			// WriteAffects already takes a storage path
			requests: []string{"a.b"},
			err:      `cannot check if write affects ephemeral data: cannot use "a" as key in map`,
		},
	}

	v := schema.View("my-view")
	for i, tc := range tcs {
		cmt := Commentf("failed test number %d", i+1)
		var paths [][]confdb.Accessor
		for _, req := range tc.requests {
			path, err := confdb.ParsePathIntoAccessors(req, confdb.ParseOptions{})
			c.Assert(err, IsNil)
			paths = append(paths, path)
		}

		eph, err := v.WriteAffectsEphemeral(paths)
		if tc.err != "" {
			c.Check(err, ErrorMatches, tc.err, cmt)
		} else {
			c.Check(eph, Equals, tc.ephemeral, cmt)
		}
	}
}

func (*viewSuite) TestViewRequestPathCannotHaveIndexLiteral(c *C) {
	_, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a[0]", "storage": "a[0]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, NotNil)
	c.Assert(err.Error(), Equals, "cannot define view \"foo\": invalid request \"a[0]\": invalid subkey \"[0]\": view paths cannot have literal indexes (only index placeholders)")

	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a[{n}]", "storage": "a[{n}]"},
				map[string]any{"request": "b", "storage": "a[0]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)
	c.Assert(schema, NotNil)

}

func (*viewSuite) TestGetListLiteral(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a[{n}].bar", "storage": "a[{n}].bar"},
				map[string]any{"request": "a[{n}].baz", "storage": "a[{n}][0].baz"},
				map[string]any{"request": "top", "storage": "a"},
				map[string]any{"request": "nested", "storage": "a[1]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag := confdb.NewJSONDatabag()
	err = bag.Set(parsePath(c, "a"), []any{
		map[string]any{"bar": 1337},
		[]any{map[string]any{"baz": 999}},
	})
	c.Assert(err, IsNil)

	view := schema.View("foo")
	val, err := view.Get(bag, "a[0].bar")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, float64(1337))

	val, err = view.Get(bag, "a[1].baz")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, float64(999))

	val, err = view.Get(bag, "top")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{
		map[string]any{"bar": float64(1337)},
		[]any{map[string]any{"baz": float64(999)}},
	})

	// path with literal index ending at list
	val, err = view.Get(bag, "nested")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{map[string]any{"baz": float64(999)}})
}

func (*viewSuite) TestGetListPlaceholder(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a[{n}].bar", "storage": "a[{n}].bar"},
				map[string]any{"request": "b[{n}][{m}].baz", "storage": "b[{n}][{m}].baz"},
				map[string]any{"request": "nested[{n}]", "storage": "b[{n}]"},
				map[string]any{"request": "c[{n}].baz", "storage": "c[{n}].baz"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag := confdb.NewJSONDatabag()
	err = bag.Set(parsePath(c, "a"), []any{map[string]any{"bar": 1337}})
	c.Assert(err, IsNil)

	err = bag.Set(parsePath(c, "b"), []any{[]any{map[string]any{"baz": 1}}, []any{map[string]any{"baz": 999}}})
	c.Assert(err, IsNil)

	view := schema.View("foo")
	val, err := view.Get(bag, "a[0].bar")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, float64(1337))

	val, err = view.Get(bag, "b[1][0].baz")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, float64(999))

	val, err = view.Get(bag, "a")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{map[string]any{"bar": float64(1337)}})

	val, err = view.Get(bag, "b")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{
		[]any{map[string]any{"baz": float64(1)}},
		[]any{map[string]any{"baz": float64(999)}},
	})

	// path with placeholder ending at list
	val, err = view.Get(bag, "nested")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{
		[]any{map[string]any{"baz": float64(1)}},
		[]any{map[string]any{"baz": float64(999)}},
	})

	// read ending at path with two values (one container and one non-container)
	// to test merging of final results
	err = bag.Set(parsePath(c, "c"), []any{map[string]any{"baz": 1}, 999})
	c.Assert(err, IsNil)

	val, err = view.Get(bag, "c")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{map[string]any{"baz": float64(1)}})
}

func (*viewSuite) TestGetListPlaceholderValueNotFound(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "c[{n}].baz", "storage": "c[{n}].baz"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag := confdb.NewJSONDatabag()

	// value doesn't include path ending in ".baz"
	err = bag.Set(parsePath(c, "c"), []any{map[string]any{"bar": 1}, 999})
	c.Assert(err, IsNil)

	view := schema.View("foo")
	_, err = view.Get(bag, "c")
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})

	// path goes beyond stored list
	_, err = view.Get(bag, "c[2]")
	c.Assert(err, testutil.ErrorIs, &confdb.NoDataError{})
}

func (*viewSuite) TestDetectViewRulesExpectDifferentTypes(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				// we shouldn't allow contradictory schemas like this but for now ensure
				// we handle this gracefully
				map[string]any{"request": "a.b", "storage": "a.b"},
				map[string]any{"request": "a[{n}]", "storage": "a[{n}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag := confdb.NewJSONDatabag()
	view := schema.View("foo")

	err = view.Set(bag, "a.b", "bar")
	c.Assert(err, IsNil)

	// check that both Get and Set handle a path/container mismatch gracefully if
	// the container is a map
	_, err = view.Get(bag, "a[0]")
	c.Assert(err, ErrorMatches, `cannot use "\[0\]" to access map at path "a"`)

	err = view.Set(bag, "a[0]", "foo")
	c.Assert(err, ErrorMatches, `cannot use "\[0\]" to access map at path "a"`)

	err = view.Unset(bag, "a[0]")
	c.Assert(err, ErrorMatches, `cannot use "\[0\]" to access map at path "a"`)

	err = bag.Unset(parsePath(c, "a"))
	c.Assert(err, IsNil)

	err = bag.Set(parsePath(c, "a"), []any{"foo", "bar"})
	c.Assert(err, IsNil)

	// check that both Get and Set handle a path/container mismatch gracefully if
	// the container is a list
	_, err = view.Get(bag, "a.b")
	c.Assert(err, ErrorMatches, `cannot use "b" to index list at path "a"`)

	err = view.Set(bag, "a.b", "foo")
	c.Assert(err, ErrorMatches, `cannot use "b" to index list at path "a"`)

	err = view.Unset(bag, "a.b")
	c.Assert(err, ErrorMatches, `cannot use "b" to index list at path "a"`)
}

func (*viewSuite) TestSetListSetsOrAppends(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a[{n}]", "storage": "a[{n}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag := confdb.NewJSONDatabag()
	view := schema.View("foo")
	err = view.Set(bag, "a[0]", "foo")
	c.Assert(err, IsNil)

	val, err := view.Get(bag, "a[0]")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, "foo")

	// can overwrite
	err = view.Set(bag, "a[0]", "bar")
	c.Assert(err, IsNil)
	val, err = view.Get(bag, "a[0]")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, "bar")

	// can append
	err = view.Set(bag, "a[1]", "baz")
	c.Assert(err, IsNil)
	val, err = view.Get(bag, "a[1]")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, "baz")

	// cannot append if index is not next one
	err = view.Set(bag, "a[9]", "foo")
	c.Assert(err, testutil.ErrorIs, confdb.PathError(""))
	c.Assert(err.Error(), Equals, `cannot access "a[9]": list has length 2`)
}

func (*viewSuite) TestSetListNested(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a[{n}]", "storage": "a[{n}]"},
				map[string]any{"request": "a[{n}][{m}]", "storage": "a[{n}][{m}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag := confdb.NewJSONDatabag()
	view := schema.View("foo")

	// set nested list
	err = view.Set(bag, "a[0][0]", "foo")
	c.Assert(err, IsNil)

	// append in nested list
	err = view.Set(bag, "a[0][1]", "bar")
	c.Assert(err, IsNil)

	val, err := view.Get(bag, "a")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{[]any{"foo", "bar"}})
}

func (*viewSuite) TestSetListPlaceholder(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a[{n}]", "storage": "a[{n}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag := confdb.NewJSONDatabag()
	view := schema.View("foo")

	// can set entire list
	err = view.Set(bag, "a", []any{"foo"})
	c.Assert(err, IsNil)

	val, err := view.Get(bag, "a[0]")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, "foo")

	err = view.Set(bag, "a[0]", "bar")
	c.Assert(err, IsNil)

	val, err = view.Get(bag, "a[0]")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, "bar")

	// reset databag and set value that makes placeholder be extended into several values
	err = bag.Unset(parsePath(c, "a"))
	c.Assert(err, IsNil)

	err = view.Set(bag, "a", []any{"foo", "bar"})
	c.Assert(err, IsNil)

	val, err = view.Get(bag, "a")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{"foo", "bar"})

	// can overwrite list element with nested value
	err = view.Set(bag, "a[0]", map[string]any{"a": "b"})
	c.Assert(err, IsNil)

	val, err = view.Get(bag, "a[0]")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, map[string]any{"a": "b"})

	err = view.Set(bag, "a[1]", map[string]any{"c": "d"})
	c.Assert(err, IsNil)

	val, err = view.Get(bag, "a")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{
		map[string]any{"a": "b"},
		map[string]any{"c": "d"},
	})
}

func (*viewSuite) TestListMerge(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a", "storage": "a"},
				map[string]any{"request": "a[{n}]", "storage": "b[{n}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag := confdb.NewJSONDatabag()
	view := schema.View("foo")

	err = view.Set(bag, "a", []any{"foo", "bar"})
	c.Assert(err, IsNil)

	err = view.Set(bag, "a[2]", "baz")
	c.Assert(err, IsNil)

	res, err := view.Get(bag, "a")
	c.Assert(err, IsNil)
	c.Assert(res, DeepEquals, []any{"foo", "bar", "baz"})
}

func (*viewSuite) TestUnsetList(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a[{n}]", "storage": "a[{n}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag := confdb.NewJSONDatabag()
	view := schema.View("foo")

	err = view.Set(bag, "a", []any{"foo", "bar", "baz"})
	c.Assert(err, IsNil)

	// unset middle element
	err = view.Unset(bag, "a[1]")
	c.Assert(err, IsNil)

	val, err := view.Get(bag, "a")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{"foo", "baz"})

	// unset the rest
	err = view.Unset(bag, "a")
	c.Assert(err, IsNil)

	val, err = view.Get(bag, "a")
	c.Check(err, testutil.ErrorIs, &confdb.NoDataError{})
	c.Assert(val, IsNil)
}

func (*viewSuite) TestUnsetBeyondCurrentList(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a[{n}].b", "storage": "a[{n}].b"},
				map[string]any{"request": "c[{n}]", "storage": "c[{n}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag := confdb.NewJSONDatabag()
	view := schema.View("foo")

	for _, pref := range []string{"a", "c"} {
		err = view.Set(bag, pref, []any{map[string]any{"b": "foo"}, map[string]any{"b": "bar"}})
		c.Assert(err, IsNil)

		err = view.Unset(bag, pref+"[2]")
		c.Assert(err, IsNil)

		val, err := view.Get(bag, pref)
		c.Assert(err, IsNil)
		c.Assert(val, DeepEquals, []any{map[string]any{"b": "foo"}, map[string]any{"b": "bar"}})
	}
}

func (*viewSuite) TestPartialUnsetNestedInList(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a[{n}]", "storage": "a[{n}].a"},
				map[string]any{"request": "b[{n}]", "storage": "a[{n}].b"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag := confdb.NewJSONDatabag()
	view := schema.View("foo")

	err = view.Set(bag, "a", []any{map[string]any{"a": "foo"}, map[string]any{"a": "foo"}})
	c.Assert(err, IsNil)

	err = view.Set(bag, "b", []any{map[string]any{"b": "bar"}, map[string]any{"b": "bar"}})
	c.Assert(err, IsNil)

	err = view.Unset(bag, "a")
	c.Assert(err, IsNil)

	val, err := view.Get(bag, "")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, map[string]any{
		"b": []any{map[string]any{"b": "bar"}, map[string]any{"b": "bar"}},
	})
}

func (*viewSuite) TestUnsetNestedList(c *C) {
	schema, err := confdb.NewSchema("acc", "confdb", map[string]any{
		"foo": map[string]any{
			"rules": []any{
				map[string]any{"request": "a[{n}][{m}]", "storage": "a[{n}][{m}]"},
			},
		},
	}, confdb.NewJSONSchema())
	c.Assert(err, IsNil)

	bag := confdb.NewJSONDatabag()
	view := schema.View("foo")

	err = view.Set(bag, "a", []any{[]any{"foo", "bar", "baz"}, []any{"a", "b"}})
	c.Assert(err, IsNil)

	// unset from 1st list
	err = view.Unset(bag, "a[0][1]")
	c.Assert(err, IsNil)

	val, err := view.Get(bag, "a")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{[]any{"foo", "baz"}, []any{"a", "b"}})

	// unset from 2nd list
	err = view.Unset(bag, "a[1][0]")
	c.Assert(err, IsNil)

	val, err = view.Get(bag, "a")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{[]any{"foo", "baz"}, []any{"b"}})

	// unset entire nested list
	err = view.Unset(bag, "a[1]")
	c.Assert(err, IsNil)

	val, err = view.Get(bag, "a")
	c.Assert(err, IsNil)
	c.Assert(val, DeepEquals, []any{[]any{"foo", "baz"}})
}

// Match three different substrings:
//  1. what begins with a '[' and does not contain a '.' or a '['
//  2. what does not contain a '.' or a '['
//  3. what is a '.'
var subkeyDivide = regexp.MustCompile(`\[[^.\[]*|[^.\[]+|[.]`)

// Same as the above regex but doesn't match '.'
var subkeyOnly = regexp.MustCompile(`\[[^.\[]*|[^.\[]+`)

func (*viewSuite) TestSubkeyDivideRegex(c *C) {
	s := "[∀.∃*-l}].[[.."
	d := subkeyDivide.FindAllString(s, -1)
	c.Check(len(d), Equals, 8)
	c.Check(d[0], Equals, "[∀")
	c.Check(d[1], Equals, ".")
	c.Check(d[2], Equals, "∃*-l}]")
	c.Check(d[3], Equals, ".")
	c.Check(d[4], Equals, "[")
	c.Check(d[5], Equals, "[")
	c.Check(d[6], Equals, ".")
	c.Check(d[7], Equals, ".")
}

func hasValidSubkeys(s string, o confdb.ParseOptions) bool {
	subStrings := subkeyOnly.FindAllString(s, -1)
	for _, ss := range subStrings {
		// If the substring doesn't match the generic case
		if !confdb.ValidSubkey.MatchString(ss) &&
			// if it doesn't match the placeholder case when placeholders are allowed
			!(o.AllowPlaceholders && confdb.ValidPlaceholder.MatchString(ss)) &&
			// if it doesn't match the index case when partial paths are allowed and indices are not forbidden
			!(!o.ForbidIndexes && o.AllowPartialPath && confdb.ValidIndexSubkey.MatchString(ss)) &&
			// if it doesn't match index placeholders when placeholders and partial paths are allowed
			!(o.AllowPlaceholders && o.AllowPartialPath && confdb.ValidIndexPlaceholder.MatchString(ss)) {
			return false
		}
	}
	return true
}

func hasEmptySubkey(s string, o confdb.ParseOptions) bool {
	subStrings := subkeyDivide.FindAllString(s, -1)
	if len(subStrings) == 0 {
		return true
	}
	if subStrings[0] == "." || subStrings[len(subStrings)-1] == "." {
		return true
	}
	// Only if partial paths are allowed can a path begin with '['
	if strings.HasPrefix(subStrings[0], "[") && !o.AllowPartialPath {
		return true
	}
	for i, ss := range subStrings {
		// A subkey can never begin with a '[' unless it's the first rune
		// Two successive '.' means an empty string subkey
		if i > 0 && subStrings[i-1] == "." && (strings.HasPrefix(ss, "[") || ss == ".") {
			return true
		}
	}
	return false
}

func fuzzHelper(f *testing.F, o confdb.ParseOptions, seed string) {
	wrapper := func(s string) ([]confdb.Accessor, error) {
		return confdb.ParsePathIntoAccessors(s, o)
	}
	f.Add(seed)
	f.Fuzz(func(t *testing.T, s string) {
		accessors, err := wrapper(s)
		if err != nil && strings.Contains(err.Error(), "invalid subkey") && !hasValidSubkeys(s, o) {
			t.Skip()
		}
		if err != nil && err.Error() == "cannot have empty subkeys" && hasEmptySubkey(s, o) {
			t.Skip()
		}
		if err != nil {
			t.Errorf("encountered error %s with input %s", err, s)
		}
		expected := subkeyOnly.FindAllString(s, -1)
		if len(accessors) != len(expected) {
			t.Errorf("unexpected number of accessors %d vs. %d", len(accessors), len(expected))
		}
		for i, e := range expected {
			if strings.HasPrefix(e, "[{") {
				if accessors[i].Type() != confdb.IndexPlaceholderType {
					t.Errorf("unexpected type of accessor %v with name %s for element %s", accessors[i].Type(), accessors[i].Name(), e)
				}
			} else if strings.HasPrefix(e, "[") {
				if accessors[i].Type() != confdb.ListIndexType {
					t.Errorf("unexpected type of accessor %v with name %s for element %s", accessors[i].Type(), accessors[i].Name(), e)
				}
			} else if strings.HasPrefix(e, "{") {
				if accessors[i].Type() != confdb.KeyPlaceholderType {
					t.Errorf("unexpected type of accessor %v with name %s for element %s", accessors[i].Type(), accessors[i].Name(), e)
				}
			} else if accessors[i].Type() != confdb.MapKeyType {
				t.Errorf("unexpected type of accessor %v with name %s for element %s", accessors[i].Type(), accessors[i].Name(), e)
			}
		}
	})
}

func FuzzParsePathIntoAccessors(f *testing.F) {
	o := confdb.ParseOptions{AllowPlaceholders: false, AllowPartialPath: false, ForbidIndexes: false}
	fuzzHelper(f, o, "foo-bar.baz[3]")
}

func FuzzParsePathIntoAccessorsAllowPlaceholders(f *testing.F) {
	o := confdb.ParseOptions{AllowPlaceholders: true, AllowPartialPath: false, ForbidIndexes: false}
	fuzzHelper(f, o, "foo-bar.{baz}[{n}].foo[3]")
}

func FuzzParsePathIntoAccessorsAllowPlaceholdersAllowPartialPath(f *testing.F) {
	o := confdb.ParseOptions{AllowPlaceholders: true, AllowPartialPath: true, ForbidIndexes: false}
	fuzzHelper(f, o, "[{n}].foo-bar.{baz}[{n}].foo[3]")
}

func FuzzParsePathIntoAccessorsAllowPlaceholdersForbidIndexes(f *testing.F) {
	o := confdb.ParseOptions{AllowPlaceholders: true, AllowPartialPath: false, ForbidIndexes: true}
	fuzzHelper(f, o, "foo.{bar}.baz.foo[{n}]")
}
