// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2015-2022 Canonical Ltd
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

package asserts_test

import (
	"fmt"
	"regexp"

	. "gopkg.in/check.v1"
	"gopkg.in/yaml.v2"

	"github.com/ddkwork/golibrary/mylog"
	"github.com/snapcore/snapd/asserts"
	"github.com/snapcore/snapd/metautil"
	"github.com/snapcore/snapd/snap"
	"github.com/snapcore/snapd/testutil"
)

type attrMatcherSuite struct {
	testutil.BaseTest
}

var _ = Suite(&attrMatcherSuite{})

func vals(yml string) map[string]interface{} {
	var vs map[string]interface{}
	mylog.Check(yaml.Unmarshal([]byte(yml), &vs))

	v := mylog.Check2(metautil.NormalizeValue(vs))

	return v.(map[string]interface{})
}

func (s *attrMatcherSuite) SetUpTest(c *C) {
	s.BaseTest.SetUpTest(c)
	s.BaseTest.AddCleanup(snap.MockSanitizePlugsSlots(func(snapInfo *snap.Info) {}))
}

func (s *attrMatcherSuite) TestSimple(c *C) {
	m := mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  foo: FOO
  bar: BAR`)))


	domatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))


	values := map[string]interface{}{
		"foo": "FOO",
		"bar": "BAR",
		"baz": "BAZ",
	}
	mylog.Check(domatch(values, nil))
	c.Check(err, IsNil)

	values = map[string]interface{}{
		"foo": "FOO",
		"bar": "BAZ",
		"baz": "BAZ",
	}
	mylog.Check(domatch(values, nil))
	c.Check(err, ErrorMatches, `field "bar" value "BAZ" does not match \^\(BAR\)\$`)

	values = map[string]interface{}{
		"foo": "FOO",
		"baz": "BAZ",
	}
	mylog.Check(domatch(values, nil))
	c.Check(err, ErrorMatches, `field "bar" has constraints but is unset`)
}

func (s *attrMatcherSuite) TestSimpleAnchorsVsRegexpAlt(c *C) {
	m := mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  bar: BAR|BAZ`)))


	domatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))


	values := map[string]interface{}{
		"bar": "BAR",
	}
	mylog.Check(domatch(values, nil))
	c.Check(err, IsNil)

	values = map[string]interface{}{
		"bar": "BARR",
	}
	mylog.Check(domatch(values, nil))
	c.Check(err, ErrorMatches, `field "bar" value "BARR" does not match \^\(BAR|BAZ\)\$`)

	values = map[string]interface{}{
		"bar": "BBAZ",
	}
	mylog.Check(domatch(values, nil))
	c.Check(err, ErrorMatches, `field "bar" value "BAZZ" does not match \^\(BAR|BAZ\)\$`)

	values = map[string]interface{}{
		"bar": "BABAZ",
	}
	mylog.Check(domatch(values, nil))
	c.Check(err, ErrorMatches, `field "bar" value "BABAZ" does not match \^\(BAR|BAZ\)\$`)

	values = map[string]interface{}{
		"bar": "BARAZ",
	}
	mylog.Check(domatch(values, nil))
	c.Check(err, ErrorMatches, `field "bar" value "BARAZ" does not match \^\(BAR|BAZ\)\$`)
}

func (s *attrMatcherSuite) TestNested(c *C) {
	m := mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  foo: FOO
  bar:
    bar1: BAR1
    bar2: BAR2`)))


	domatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))

	mylog.Check(domatch(vals(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR2
  bar3: BAR3
baz: BAZ
`), nil))
	c.Check(err, IsNil)
	mylog.Check(domatch(vals(`
foo: FOO
bar: BAZ
baz: BAZ
`), nil))
	c.Check(err, ErrorMatches, `field "bar" must be a map`)
	mylog.Check(domatch(vals(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR22
  bar3: BAR3
baz: BAZ
`), nil))
	c.Check(err, ErrorMatches, `field "bar\.bar2" value "BAR22" does not match \^\(BAR2\)\$`)
	mylog.Check(domatch(vals(`
foo: FOO
bar:
  bar1: BAR1
  bar2:
    bar22: true
  bar3: BAR3
baz: BAZ
`), nil))
	c.Check(err, ErrorMatches, `field "bar\.bar2" must be a scalar or list`)
}

func (s *attrMatcherSuite) TestAlternative(c *C) {
	m := mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  -
    foo: FOO
    bar: BAR
  -
    foo: FOO
    bar: BAZ`)))


	domatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"], nil))


	values := map[string]interface{}{
		"foo": "FOO",
		"bar": "BAR",
		"baz": "BAZ",
	}
	mylog.Check(domatch(values, nil))
	c.Check(err, IsNil)

	values = map[string]interface{}{
		"foo": "FOO",
		"bar": "BAZ",
		"baz": "BAZ",
	}
	mylog.Check(domatch(values, nil))
	c.Check(err, IsNil)

	values = map[string]interface{}{
		"foo": "FOO",
		"bar": "BARR",
		"baz": "BAR",
	}
	mylog.Check(domatch(values, nil))
	c.Check(err, ErrorMatches, `no alternative matches: field "bar" value "BARR" does not match \^\(BAR\)\$`)
}

func (s *attrMatcherSuite) TestNestedAlternative(c *C) {
	m := mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  foo: FOO
  bar:
    bar1: BAR1
    bar2:
      - BAR2
      - BAR22`)))


	domatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))

	mylog.Check(domatch(vals(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR2
`), nil))
	c.Check(err, IsNil)
	mylog.Check(domatch(vals(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR22
`), nil))
	c.Check(err, IsNil)
	mylog.Check(domatch(vals(`
foo: FOO
bar:
  bar1: BAR1
  bar2: BAR3
`), nil))
	c.Check(err, ErrorMatches, `no alternative for field "bar\.bar2" matches: field "bar\.bar2" value "BAR3" does not match \^\(BAR2\)\$`)
}

func (s *attrMatcherSuite) TestAlternativeMatchingStringList(c *C) {
	toMatch := vals(`
write:
 - /var/tmp
 - /var/lib/snapd/snapshots
`)
	m := mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  write: /var/(tmp|lib/snapd/snapshots)`)))


	domatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))

	mylog.Check(domatch(toMatch, nil))
	c.Check(err, IsNil)

	m = mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  write:
    - /var/tmp
    - /var/lib/snapd/snapshots`)))


	domatchLst := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))

	mylog.Check(domatchLst(toMatch, nil))
	c.Check(err, IsNil)
}

func (s *attrMatcherSuite) TestAlternativeMatchingComplex(c *C) {
	toMatch := vals(`
mnt: [{what: "/dev/x*", where: "/foo/*", options: ["rw", "nodev"]}, {what: "/bar/*", where: "/baz/*", options: ["rw", "bind"]}]
`)

	m := mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  mnt:
    -
      what: /(bar/|dev/x)\*
      where: /(foo|baz)/\*
      options: rw|bind|nodev`)))


	domatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))

	mylog.Check(domatch(toMatch, nil))
	c.Check(err, IsNil)

	m = mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  mnt:
    -
      what: /dev/x\*
      where: /foo/\*
      options:
        - nodev
        - rw
    -
      what: /bar/\*
      where: /baz/\*
      options:
        - rw
        - bind`)))


	domatchExtensive := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))

	mylog.Check(domatchExtensive(toMatch, nil))
	c.Check(err, IsNil)

	// not matching case
	m = mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  mnt:
    -
      what: /dev/x\*
      where: /foo/\*
      options:
        - rw
    -
      what: /bar/\*
      where: /baz/\*
      options:
        - rw
        - bind`)))


	domatchExtensiveNoMatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))

	mylog.Check(domatchExtensiveNoMatch(toMatch, nil))
	c.Check(err, ErrorMatches, `no alternative for field "mnt\.0" matches: no alternative for field "mnt\.0.options\.1" matches:.*`)
}

func (s *attrMatcherSuite) TestOtherScalars(c *C) {
	m := mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  foo: 1
  bar: true`)))


	domatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))

	mylog.Check(domatch(vals(`
foo: 1
bar: true
`), nil))
	c.Check(err, IsNil)

	values := map[string]interface{}{
		"foo": int64(1),
		"bar": true,
	}
	mylog.Check(domatch(values, nil))
	c.Check(err, IsNil)
}

func (s *attrMatcherSuite) TestCompileErrors(c *C) {
	_ := mylog.Check2(asserts.CompileAttrMatcher(1, nil))
	c.Check(err, ErrorMatches, `top constraint must be a key-value map, regexp or a list of alternative constraints: 1`)

	_ = mylog.Check2(asserts.CompileAttrMatcher(map[string]interface{}{
		"foo": 1,
	}, nil))
	c.Check(err, ErrorMatches, `constraint "foo" must be a key-value map, regexp or a list of alternative constraints: 1`)

	_ = mylog.Check2(asserts.CompileAttrMatcher(map[string]interface{}{
		"foo": "[",
	}, nil))
	c.Check(err, ErrorMatches, `cannot compile "foo" constraint "\[": error parsing regexp:.*`)

	_ = mylog.Check2(asserts.CompileAttrMatcher(map[string]interface{}{
		"foo": []interface{}{"foo", "["},
	}, nil))
	c.Check(err, ErrorMatches, `cannot compile "foo/alt#2/" constraint "\[": error parsing regexp:.*`)

	_ = mylog.Check2(asserts.CompileAttrMatcher(map[string]interface{}{
		"foo": []interface{}{"foo", []interface{}{"bar", "baz"}},
	}, nil))
	c.Check(err, ErrorMatches, `cannot nest alternative constraints directly at "foo/alt#2/"`)

	_ = mylog.Check2(asserts.CompileAttrMatcher("FOO", nil))
	c.Check(err, ErrorMatches, `first level of non alternative constraints must be a set of key-value contraints`)

	_ = mylog.Check2(asserts.CompileAttrMatcher([]interface{}{"FOO"}, nil))
	c.Check(err, ErrorMatches, `first level of non alternative constraints must be a set of key-value contraints`)

	_ = mylog.Check2(asserts.CompileAttrMatcher(map[string]interface{}{
		"foo": "$FOO()",
	}, nil))
	c.Check(err, ErrorMatches, `cannot compile "foo" constraint "\$FOO\(\)": no \$OP\(\) constraints supported`)

	wrongDollarConstraints := []string{
		"$",
		"$FOO(a)",
		"$SLOT",
		"$SLOT()",
		"$SLOT(x,y)",
		"$SLOT(x,y,z)",
	}

	for _, wrong := range wrongDollarConstraints {
		_ := mylog.Check2(asserts.CompileAttrMatcher(map[string]interface{}{
			"foo": wrong,
		}, []string{"SLOT", "OP"}))
		if wrong != "$SLOT(x,y)" {
			c.Check(err, ErrorMatches, fmt.Sprintf(`cannot compile "foo" constraint "%s": not a valid \$SLOT\(\)/\$OP\(\) constraint`, regexp.QuoteMeta(wrong)))
		} else {
			c.Check(err, ErrorMatches, fmt.Sprintf(`cannot compile "foo" constraint "%s": \$SLOT\(\) constraint expects 1 argument`, regexp.QuoteMeta(wrong)))
		}

	}
}

func (s *attrMatcherSuite) TestMatchingListsSimple(c *C) {
	m := mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  foo: /foo/.*`)))


	domatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))

	mylog.Check(domatch(vals(`
foo: ["/foo/x", "/foo/y"]
`), nil))
	c.Check(err, IsNil)
	mylog.Check(domatch(vals(`
foo: ["/foo/x", "/foo"]
`), nil))
	c.Check(err, ErrorMatches, `field "foo\.1" value "/foo" does not match \^\(/foo/\.\*\)\$`)
}

func (s *attrMatcherSuite) TestMissingCheck(c *C) {
	m := mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  foo: $MISSING`)))


	domatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))

	mylog.Check(domatch(vals(`
bar: baz
`), nil))
	c.Check(err, IsNil)
	mylog.Check(domatch(vals(`
foo: ["x"]
`), nil))
	c.Check(err, ErrorMatches, `field "foo" is constrained to be missing but is set`)
}

func (s *attrMatcherSuite) TestEvalCheck(c *C) {
	// TODO: consider rewriting once we have $WITHIN
	m := mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  foo: $SLOT(foo)
  bar: $PLUG(bar.baz)`)))


	domatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), []string{"SLOT", "PLUG"}))

	mylog.Check(domatch(vals(`
foo: foo
bar: bar
`), nil))
	c.Check(err, ErrorMatches, `field "(foo|bar)" cannot be matched without context`)

	calls := make(map[[2]string]bool)
	comp1 := func(op string, arg string) (interface{}, error) {
		calls[[2]string{op, arg}] = true
		return arg, nil
	}
	mylog.Check(domatch(vals(`
foo: foo
bar: bar.baz
`), testEvalAttr{comp1}))
	c.Check(err, IsNil)

	c.Check(calls, DeepEquals, map[[2]string]bool{
		{"slot", "foo"}:     true,
		{"plug", "bar.baz"}: true,
	})

	comp2 := func(op string, arg string) (interface{}, error) {
		if op == "plug" {
			return nil, fmt.Errorf("boom")
		}
		return arg, nil
	}
	mylog.Check(domatch(vals(`
foo: foo
bar: bar.baz
`), testEvalAttr{comp2}))
	c.Check(err, ErrorMatches, `field "bar" constraint \$PLUG\(bar\.baz\) cannot be evaluated: boom`)

	comp3 := func(op string, arg string) (interface{}, error) {
		if op == "slot" {
			return "other-value", nil
		}
		return arg, nil
	}
	mylog.Check(domatch(vals(`
foo: foo
bar: bar.baz
`), testEvalAttr{comp3}))
	c.Check(err, ErrorMatches, `field "foo" does not match \$SLOT\(foo\): foo != other-value`)
}

func (s *attrMatcherSuite) TestMatchingListsMap(c *C) {
	m := mylog.Check2(asserts.ParseHeaders([]byte(`attrs:
  foo:
    p: /foo/.*`)))


	domatch := mylog.Check2(asserts.CompileAttrMatcher(m["attrs"].(map[string]interface{}), nil))

	mylog.Check(domatch(vals(`
foo: [{p: "/foo/x"}, {p: "/foo/y"}]
`), nil))
	c.Check(err, IsNil)
	mylog.Check(domatch(vals(`
foo: [{p: "zzz"}, {p: "/foo/y"}]
`), nil))
	c.Check(err, ErrorMatches, `field "foo\.0\.p" value "zzz" does not match \^\(/foo/\.\*\)\$`)
}

type deviceScopeConstraintSuite struct {
	testutil.BaseTest
}

var _ = Suite(&deviceScopeConstraintSuite{})

func (s *deviceScopeConstraintSuite) TestCompile(c *C) {
	tests := []struct {
		m   map[string]interface{}
		exp *asserts.DeviceScopeConstraint
		err string
	}{
		{m: nil, exp: nil},
		{m: map[string]interface{}{"on-store": []interface{}{"foo", "bar"}}, exp: &asserts.DeviceScopeConstraint{Store: []string{"foo", "bar"}}},
		{m: map[string]interface{}{"on-brand": []interface{}{"foo", "bar"}}, exp: &asserts.DeviceScopeConstraint{Brand: []string{"foo", "bar"}}},
		{m: map[string]interface{}{"on-model": []interface{}{"foo/model1", "bar/model-2"}}, exp: &asserts.DeviceScopeConstraint{Model: []string{"foo/model1", "bar/model-2"}}},
		{
			m: map[string]interface{}{
				"on-brand": []interface{}{"foo", "bar"},
				"on-model": []interface{}{"foo/model1", "bar/model-2"},
				"on-store": []interface{}{"foo", "bar"},
			},
			exp: &asserts.DeviceScopeConstraint{
				Store: []string{"foo", "bar"},
				Brand: []string{"foo", "bar"},
				Model: []string{"foo/model1", "bar/model-2"},
			},
		},
		{m: map[string]interface{}{"on-store": ""}, err: `on-store in constraint must be a list of strings`},
		{m: map[string]interface{}{"on-brand": "foo"}, err: `on-brand in constraint must be a list of strings`},
		{m: map[string]interface{}{"on-model": map[string]interface{}{"brand": "x"}}, err: `on-model in constraint must be a list of strings`},
	}

	for _, t := range tests {
		dsc := mylog.Check2(asserts.CompileDeviceScopeConstraint(t.m, "constraint"))
		if t.err == "" {
			c.Check(err, IsNil)
			c.Check(dsc, DeepEquals, t.exp)
		} else {
			c.Check(err, ErrorMatches, t.err)
			c.Check(dsc, IsNil)
		}
	}
}

func (s *deviceScopeConstraintSuite) TestValidOnStoreBrandModel(c *C) {
	tests := []struct {
		constr string
		value  string
		valid  bool
	}{
		{"on-store", "", false},
		{"on-store", "foo", true},
		{"on-store", "F_o-O88", true},
		{"on-store", "foo!", false},
		{"on-store", "foo.", false},
		{"on-store", "foo/", false},
		{"on-brand", "", false},
		// custom set brands (length 2-28)
		{"on-brand", "dwell", true},
		{"on-brand", "Dwell", false},
		{"on-brand", "dwell-88", true},
		{"on-brand", "dwell_88", false},
		{"on-brand", "dwell.88", false},
		{"on-brand", "dwell:88", false},
		{"on-brand", "dwell!88", false},
		{"on-brand", "a", false},
		{"on-brand", "ab", true},
		{"on-brand", "0123456789012345678901234567", true},
		// snappy id brands (fixed length 32)
		{"on-brand", "01234567890123456789012345678", false},
		{"on-brand", "012345678901234567890123456789", false},
		{"on-brand", "0123456789012345678901234567890", false},
		{"on-brand", "01234567890123456789012345678901", true},
		{"on-brand", "abcdefghijklmnopqrstuvwxyz678901", true},
		{"on-brand", "ABCDEFGHIJKLMNOPQRSTUVWCYZ678901", true},
		{"on-brand", "ABCDEFGHIJKLMNOPQRSTUVWCYZ678901X", false},
		{"on-brand", "ABCDEFGHIJKLMNOPQ!STUVWCYZ678901", false},
		{"on-brand", "ABCDEFGHIJKLMNOPQ_STUVWCYZ678901", false},
		{"on-brand", "ABCDEFGHIJKLMNOPQ-STUVWCYZ678901", false},
		{"on-model", "", false},
		{"on-model", "/", false},
		{"on-model", "dwell/dwell1", true},
		{"on-model", "dwell", false},
		{"on-model", "dwell/", false},
		{"on-model", "dwell//dwell1", false},
		{"on-model", "dwell/-dwell1", false},
		{"on-model", "dwell/dwell1-", false},
		{"on-model", "dwell/dwell1-23", true},
		{"on-model", "dwell/dwell1!", false},
		{"on-model", "dwell/dwe_ll1", false},
		{"on-model", "dwell/dwe.ll1", false},
	}

	check := func(constr, value string, valid bool) {
		cMap := map[string]interface{}{
			constr: []interface{}{value},
		}

		_ := mylog.Check2(asserts.CompileDeviceScopeConstraint(cMap, "constraint"))
		if valid {
			c.Check(err, IsNil, Commentf("%v", cMap))
		} else {
			c.Check(err, ErrorMatches, fmt.Sprintf(`%s in constraint contains an invalid element: %q`, constr, value), Commentf("%v", cMap))
		}
	}

	for _, t := range tests {
		check(t.constr, t.value, t.valid)

		if t.constr == "on-brand" {
			// reuse and double check all brands also in the context of on-model!

			check("on-model", t.value+"/foo", t.valid)
		}
	}
}

func (s *deviceScopeConstraintSuite) TestCheck(c *C) {
	a := mylog.Check2(asserts.Decode([]byte(`type: model
authority-id: my-brand
series: 16
brand-id: my-brand
model: my-model1
store: store1
architecture: armhf
kernel: krnl
gadget: gadget
timestamp: 2018-09-12T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`)))

	myModel1 := a.(*asserts.Model)

	a = mylog.Check2(asserts.Decode([]byte(`type: model
authority-id: my-brand-subbrand
series: 16
brand-id: my-brand-subbrand
model: my-model2
store: store2
architecture: armhf
kernel: krnl
gadget: gadget
timestamp: 2018-09-12T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`)))

	myModel2 := a.(*asserts.Model)

	a = mylog.Check2(asserts.Decode([]byte(`type: model
authority-id: my-brand
series: 16
brand-id: my-brand
model: my-model3
store: substore1
architecture: armhf
kernel: krnl
gadget: gadget
timestamp: 2018-09-12T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`)))

	myModel3 := a.(*asserts.Model)

	a = mylog.Check2(asserts.Decode([]byte(`type: store
store: substore1
authority-id: canonical
operator-id: canonical
friendly-stores:
  - a-store
  - store1
  - store2
timestamp: 2018-09-12T12:00:00Z
sign-key-sha3-384: Jv8_JiHiIzJVcO9M55pPdqSDWUvuhfDIBJUS-3VW7F_idjix7Ffn5qMxB21ZQuij

AXNpZw==`)))

	substore1 := a.(*asserts.Store)

	tests := []struct {
		m                 map[string]interface{}
		model             *asserts.Model
		store             *asserts.Store
		useFriendlyStores bool
		err               string
	}{
		{m: map[string]interface{}{"on-store": []interface{}{"store1"}}, model: myModel1},
		{m: map[string]interface{}{"on-store": []interface{}{"a-store", "store1"}}, model: myModel1},
		{m: map[string]interface{}{"on-store": []interface{}{"store1"}}, model: myModel2, err: "on-store mismatch"},
		{m: map[string]interface{}{"on-store": []interface{}{"store1"}}, model: myModel3, store: substore1, useFriendlyStores: true},
		{m: map[string]interface{}{"on-store": []interface{}{"store1"}}, model: myModel3, store: substore1, useFriendlyStores: false, err: "on-store mismatch"},
		{m: map[string]interface{}{"on-store": []interface{}{"store1"}}, model: nil, err: `cannot match on-store/on-brand/on-model without model`},
		{m: map[string]interface{}{"on-store": []interface{}{"store1"}}, model: myModel2, store: substore1, err: `store assertion and model store must match`, useFriendlyStores: true},
		{m: map[string]interface{}{"on-store": []interface{}{"other-store"}}, model: myModel3, store: substore1, err: "on-store mismatch"},
		{m: map[string]interface{}{"on-brand": []interface{}{"my-brand"}}, model: myModel1},
		{m: map[string]interface{}{"on-brand": []interface{}{"my-brand", "my-brand-subbrand"}}, model: myModel2},
		{m: map[string]interface{}{"on-brand": []interface{}{"other-brand"}}, model: myModel2, err: "on-brand mismatch"},
		{m: map[string]interface{}{"on-model": []interface{}{"my-brand/my-model1"}}, model: myModel1},
		{m: map[string]interface{}{"on-model": []interface{}{"my-brand/other-model"}}, model: myModel1, err: "on-model mismatch"},
		{m: map[string]interface{}{"on-model": []interface{}{"my-brand/my-model", "my-brand-subbrand/my-model2", "other-brand/other-model"}}, model: myModel2},
		{
			m: map[string]interface{}{
				"on-store": []interface{}{"store2"},
				"on-brand": []interface{}{"my-brand", "my-brand-subbrand"},
				"on-model": []interface{}{"my-brand/my-model3", "my-brand-subbrand/my-model2"},
			},
			model: myModel2,
		},
		{
			m: map[string]interface{}{
				"on-store": []interface{}{"store2"},
				"on-brand": []interface{}{"my-brand", "my-brand-subbrand"},
				"on-model": []interface{}{"my-brand/my-model3", "my-brand-subbrand/my-model2"},
			},
			model: myModel3, store: substore1,
			useFriendlyStores: true,
		},
		{
			m: map[string]interface{}{
				"on-store": []interface{}{"other-store"},
				"on-brand": []interface{}{"my-brand", "my-brand-subbrand"},
				"on-model": []interface{}{"my-brand/my-model3", "my-brand-subbrand/my-model2"},
			},
			model: myModel3, store: substore1,
			useFriendlyStores: true,
			err:               "on-store mismatch",
		},
		{
			m: map[string]interface{}{
				"on-store": []interface{}{"store2"},
				"on-brand": []interface{}{"other-brand", "my-brand-subbrand"},
				"on-model": []interface{}{"my-brand/my-model3", "my-brand-subbrand/my-model2"},
			},
			model: myModel3, store: substore1,
			useFriendlyStores: true,
			err:               "on-brand mismatch",
		},
		{
			m: map[string]interface{}{
				"on-store": []interface{}{"store2"},
				"on-brand": []interface{}{"my-brand", "my-brand-subbrand"},
				"on-model": []interface{}{"my-brand/my-model1", "my-brand-subbrand/my-model2"},
			},
			model: myModel3, store: substore1,
			useFriendlyStores: true,
			err:               "on-model mismatch",
		},
		{
			m: map[string]interface{}{
				"on-store": []interface{}{"store2"},
				"on-brand": []interface{}{"my-brand", "my-brand-subbrand"},
				"on-model": []interface{}{"my-brand/my-model1", "my-brand-subbrand/my-model2"},
			},
			model: myModel3, store: substore1,
			useFriendlyStores: false,
			err:               "on-store mismatch",
		},
	}

	for _, t := range tests {
		constr := mylog.Check2(asserts.CompileDeviceScopeConstraint(t.m, "constraint"))


		var opts *asserts.DeviceScopeConstraintCheckOptions
		if t.useFriendlyStores {
			opts = &asserts.DeviceScopeConstraintCheckOptions{
				UseFriendlyStores: true,
			}
		}
		mylog.Check(constr.Check(t.model, t.store, opts))
		if t.err == "" {
			c.Check(err, IsNil, Commentf("%v", t.m))
		} else {
			c.Check(err, ErrorMatches, t.err)
		}
	}
}
