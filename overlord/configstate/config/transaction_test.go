// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2016 Canonical Ltd
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

package config_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/jsonutil"
	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
)

func TestT(t *testing.T) { TestingT(t) }

type transactionSuite struct {
	state       *state.State
	transaction *config.Transaction
}

var _ = Suite(&transactionSuite{})

func (s *transactionSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
	s.state.Lock()
	defer s.state.Unlock()
	s.transaction = config.NewTransaction(s.state)
}

type setGetOp string

func (op setGetOp) kind() string {
	return strings.Fields(string(op))[0]
}

func (op setGetOp) list() []string {
	args := strings.Fields(string(op))
	return args[1:]
}

func (op setGetOp) args() map[string]interface{} {
	m := make(map[string]interface{})
	args := strings.Fields(string(op))
	for _, pair := range args[1:] {
		if pair == "=>" {
			break
		}
		kv := strings.SplitN(pair, "=", 2)
		var v interface{}
		if err := jsonutil.DecodeWithNumber(strings.NewReader(kv[1]), &v); err != nil {
			v = kv[1]
		}
		m[kv[0]] = v
	}
	return m
}

func (op setGetOp) error() string {
	if i := strings.Index(string(op), " => "); i >= 0 {
		return string(op[i+4:])
	}
	return ""
}

func (op setGetOp) fails() bool {
	return op.error() != ""
}

var setGetTests = [][]setGetOp{{
	// Basics.
	`get foo=-`,
	`getroot => snap "core" has no configuration`,
	`set one=1 two=2`,
	`set big=1234567890`,
	`setunder three=3 big=9876543210`,
	`get one=1 big=1234567890 two=2 three=-`,
	`getunder one=- two=- three=3 big=9876543210`,
	`changes core.big core.one core.two`,
	`commit`,
	`getunder one=1 two=2 three=3`,
	`get one=1 two=2 three=3`,
	`set two=22 four=4 big=1234567890`,
	`changes core.big core.four core.two`,
	`get one=1 two=22 three=3 four=4 big=1234567890`,
	`getunder one=1 two=2 three=3 four=-`,
	`commit`,
	`getunder one=1 two=22 three=3 four=4`,
}, {
	// Trivial full doc.
	`set doc={"one":1,"two":2}`,
	`get doc={"one":1,"two":2}`,
	`changes core.doc.one core.doc.two`,
}, {
	// Nulls via dotted path
	`set doc={"one":1,"two":2}`,
	`commit`,
	`set doc.one=null`,
	`changes core.doc.one`,
	`get doc={"two":2}`,
	`getunder doc={"one":1,"two":2}`,
	`commit`,
	`get doc={"two":2}`,
	`getroot ={"doc":{"two":2}}`,
	`getunder doc={"two":2}`, // nils are not committed to state
}, {
	// Nulls via dotted path, resuling in empty map
	`set doc={"one":{"three":3},"two":2}`,
	`set doc.one.three=null`,
	`changes core.doc.one.three core.doc.two`,
	`get doc={"one":{},"two":2}`,
	`commit`,
	`get doc={"one":{},"two":2}`,
	`getunder doc={"one":{},"two":2}`, // nils are not committed to state
}, {
	// Nulls via dotted path in a doc
	`set doc={"one":1,"two":2}`,
	`set doc.three={"four":4}`,
	`get doc={"one":1,"two":2,"three":{"four":4}}`,
	`set doc.three={"four":null}`,
	`changes core.doc.one core.doc.three.four core.doc.two`,
	`get doc={"one":1,"two":2,"three":{}}`,
	`commit`,
	`get doc={"one":1,"two":2,"three":{}}`,
	`getunder doc={"one":1,"two":2,"three":{}}`, // nils are not committed to state
}, {
	// Nulls nested in a document
	`set doc={"one":{"three":3,"two":2}}`,
	`changes core.doc.one.three core.doc.one.two`,
	`set doc={"one":{"three":null,"two":2}}`,
	`changes core.doc.one.three core.doc.one.two`,
	`get doc={"one":{"two":2}}`,
	`commit`,
	`get doc={"one":{"two":2}}`,
	`getunder doc={"one":{"two":2}}`, // nils are not committed to state
}, {
	// Nulls with mutating
	`set doc={"one":{"two":2}}`,
	`set doc.one.two=null`,
	`changes core.doc.one.two`,
	`set doc.one="foo"`,
	`get doc.one="foo"`,
	`commit`,
	`get doc={"one":"foo"}`,
	`getunder doc={"one":"foo"}`, // nils are not committed to state
}, {
	// Nulls, intermediate temporary maps
	`set doc={"one":{"two":2}}`,
	`commit`,
	`set doc.one.three.four.five=null`,
	`get doc={"one":{"two":2,"three":{"four":{}}}}`,
	`commit`,
	`get doc={"one":{"two":2,"three":{"four":{}}}}`,
	`getrootunder ={"doc":{"one":{"two":2,"three":{"four":{}}}}}`, // nils are not committed to state
}, {
	// Nulls, same transaction
	`set doc={"one":{"two":2}}`,
	`set doc.one.three.four.five=null`,
	`changes core.doc.one.three.four.five core.doc.one.two`,
	`get doc={"one":{"two":2,"three":{"four":{}}}}`,
	`commit`,
	`get doc={"one":{"two":2,"three":{"four":{}}}}`,
	`getrootunder ={"doc":{"one":{"two":2,"three":{"four":{}}}}}`, // nils are not committed to state
}, {
	// Null leading to empty doc
	`set doc={"one":1}`,
	`set doc.one=null`,
	`changes core.doc.one`,
	`commit`,
	`get doc={}`,
}, {
	// Nulls leading to no snap configuration
	`set doc="foo"`,
	`set doc=null`,
	`changes core.doc`,
	`commit`,
	`get doc=-`,
	`getroot => snap "core" has no configuration`,
}, {
	// set null over non-existing path
	`set x.y.z=null`,
	`changes core.x.y.z`,
	`commit`,
	`get x.y.z=-`,
}, {
	// set null over non-existing path with initial config
	`set foo=bar`,
	`commit`,
	`set x=null`,
	`changes core.x`,
	`commit`,
	`get x=-`,
}, {
	// Root doc
	`set doc={"one":1,"two":2}`,
	`changes core.doc.one core.doc.two`,
	`getroot ={"doc":{"one":1,"two":2}}`,
	`commit`,
	`getroot ={"doc":{"one":1,"two":2}}`,
	`getrootunder ={"doc":{"one":1,"two":2}}`,
}, {
	// Nested mutations.
	`set one.two.three=3`,
	`changes core.one.two.three`,
	`set one.five=5`,
	`changes core.one.five core.one.two.three`,
	`setunder one={"two":{"four":4}}`,
	`get one={"two":{"three":3},"five":5}`,
	`get one.two={"three":3}`,
	`get one.two.three=3`,
	`get one.five=5`,
	`commit`,
	`getunder one={"two":{"three":3,"four":4},"five":5}`,
	`get one={"two":{"three":3,"four":4},"five":5}`,
	`get one.two={"three":3,"four":4}`,
	`get one.two.three=3`,
	`get one.two.four=4`,
	`get one.five=5`,
}, {
	// Nested partial update with full get
	`set one={"two":2,"three":3}`,
	`commit`,
	// update just one subkey
	`set one.two=0`,
	// both subkeys are returned
	`get one={"two":0,"three":3}`,
	`getroot ={"one":{"two":0,"three":3}}`,
	`get one.two=0`,
	`get one.three=3`,
	`getunder one={"two":2,"three":3}`,
	`changes core.one.two`,
	`commit`,
	`getroot ={"one":{"two":0,"three":3}}`,
	`get one={"two":0,"three":3}`,
	`getunder one={"two":0,"three":3}`,
}, {
	// Replacement with nested mutation.
	`set one={"two":{"three":3}}`,
	`changes core.one.two.three`,
	`set one.five=5`,
	`changes core.one.five core.one.two.three`,
	`get one={"two":{"three":3},"five":5}`,
	`get one.two={"three":3}`,
	`get one.two.three=3`,
	`get one.five=5`,
	`setunder one={"two":{"four":4},"six":6}`,
	`commit`,
	`getunder one={"two":{"three":3},"five":5}`,
}, {
	// Cannot go through known scalar implicitly.
	`set one.two=2`,
	`changes core.one.two`,
	`set one.two.three=3 => snap "core" option "one\.two" is not a map`,
	`get one.two.three=3 => snap "core" option "one\.two" is not a map`,
	`get one={"two":2}`,
	`commit`,
	`set one.two.three=3 => snap "core" option "one\.two" is not a map`,
	`get one.two.three=3 => snap "core" option "one\.two" is not a map`,
	`get one={"two":2}`,
	`getunder one={"two":2}`,
}, {
	// Unknown scalars may be overwritten though.
	`setunder one={"two":2}`,
	`set one.two.three=3`,
	`changes core.one.two.three`,
	`commit`,
	`getunder one={"two":{"three":3}}`,
}, {
	// Invalid option names.
	`set BAD=1 => invalid option name: "BAD"`,
	`set 42=1 => invalid option name: "42"`,
	`set .bad=1 => invalid option name: ""`,
	`set bad.=1 => invalid option name: ""`,
	`set bad..bad=1 => invalid option name: ""`,
	`set one.bad--bad.two=1 => invalid option name: "bad--bad"`,
	`set one.-bad.two=1 => invalid option name: "-bad"`,
	`set one.bad-.two=1 => invalid option name: "bad-"`,
}}

func (s *transactionSuite) TestSetGet(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, test := range setGetTests {
		c.Logf("-----")
		s.state.Set("config", map[string]interface{}{})
		t := config.NewTransaction(s.state)
		snap := "core"
		for _, op := range test {
			c.Logf("%s", op)
			switch op.kind() {
			case "set":
				for k, v := range op.args() {
					err := t.Set(snap, k, v)
					if op.fails() {
						c.Assert(err, ErrorMatches, op.error())
					} else {
						c.Assert(err, IsNil)
					}
				}

			case "get":
				for k, expected := range op.args() {
					var obtained interface{}
					err := t.Get(snap, k, &obtained)
					if op.fails() {
						c.Assert(err, ErrorMatches, op.error())
						var nothing interface{}
						c.Assert(t.GetMaybe(snap, k, &nothing), ErrorMatches, op.error())
						c.Assert(nothing, IsNil)
						continue
					}
					if expected == "-" {
						if !config.IsNoOption(err) {
							c.Fatalf("Expected %q key to not exist, but it has value %v", k, obtained)
						}
						c.Assert(err, ErrorMatches, fmt.Sprintf("snap %q has no %q configuration option", snap, k))
						var nothing interface{}
						c.Assert(t.GetMaybe(snap, k, &nothing), IsNil)
						c.Assert(nothing, IsNil)
						continue
					}
					c.Assert(err, IsNil)
					c.Assert(obtained, DeepEquals, expected)

					obtained = nil
					c.Assert(t.GetMaybe(snap, k, &obtained), IsNil)
					c.Assert(obtained, DeepEquals, expected)
				}

			case "commit":
				t.Commit()

			case "changes":
				expected := op.list()
				obtained := t.Changes()
				c.Check(obtained, DeepEquals, expected)

			case "setunder":
				var config map[string]map[string]interface{}
				s.state.Get("config", &config)
				if config == nil {
					config = make(map[string]map[string]interface{})
				}
				if config[snap] == nil {
					config[snap] = make(map[string]interface{})
				}
				for k, v := range op.args() {
					if v == "-" {
						delete(config[snap], k)
						if len(config[snap]) == 0 {
							delete(config[snap], snap)
						}
					} else {
						config[snap][k] = v
					}
				}
				s.state.Set("config", config)

			case "getunder":
				var config map[string]map[string]*json.RawMessage
				s.state.Get("config", &config)
				for k, expected := range op.args() {
					obtained, ok := config[snap][k]
					if expected == "-" {
						if ok {
							c.Fatalf("Expected %q key to not exist, but it has value %v", k, obtained)
						}
						continue
					}
					var cfg interface{}
					c.Assert(jsonutil.DecodeWithNumber(bytes.NewReader(*obtained), &cfg), IsNil)
					c.Assert(cfg, DeepEquals, expected)
				}
			case "getroot":
				var obtained interface{}
				err := t.Get(snap, "", &obtained)
				if op.fails() {
					c.Assert(err, ErrorMatches, op.error())
					continue
				}
				c.Assert(err, IsNil)
				c.Assert(obtained, DeepEquals, op.args()[""])
			case "getrootunder":
				var config map[string]*json.RawMessage
				s.state.Get("config", &config)
				for _, expected := range op.args() {
					obtained, ok := config[snap]
					c.Assert(ok, Equals, true)
					var cfg interface{}
					c.Assert(jsonutil.DecodeWithNumber(bytes.NewReader(*obtained), &cfg), IsNil)
					c.Assert(cfg, DeepEquals, expected)
				}
			default:
				panic("unknown test op kind: " + op.kind())
			}
		}
	}
}

type brokenType struct {
	on string
}

func (b *brokenType) UnmarshalJSON(data []byte) error {
	if b.on == string(data) {
		return fmt.Errorf("BAM!")
	}
	return nil
}

func (s *transactionSuite) TestCommitOverNilSnapConfig(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// simulate invalid nil map created due to LP #1917870 by snap restore
	s.state.Set("config", map[string]interface{}{"test-snap": nil})
	t := config.NewTransaction(s.state)

	c.Assert(t.Set("test-snap", "foo", "bar"), IsNil)
	t.Commit()
	var v string
	t.Get("test-snap", "foo", &v)
	c.Assert(v, Equals, "bar")
}

func (s *transactionSuite) TestGetUnmarshalError(c *C) {
	s.state.Lock()
	defer s.state.Unlock()
	c.Check(s.transaction.Set("test-snap", "foo", "good"), IsNil)
	s.transaction.Commit()

	tr := config.NewTransaction(s.state)
	c.Check(tr.Set("test-snap", "foo", "break"), IsNil)

	// Pristine state is good, value in the transaction breaks.
	broken := brokenType{`"break"`}
	err := tr.Get("test-snap", "foo", &broken)
	c.Assert(err, ErrorMatches, ".*BAM!.*")

	// Pristine state breaks, nothing in the transaction.
	tr.Commit()
	err = tr.Get("test-snap", "foo", &broken)
	c.Assert(err, ErrorMatches, ".*BAM!.*")
}

func (s *transactionSuite) TestNoConfiguration(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	var res interface{}
	tr := config.NewTransaction(s.state)
	err := tr.Get("some-snap", "", &res)
	c.Assert(err, NotNil)
	c.Assert(config.IsNoOption(err), Equals, true)
	c.Assert(err, ErrorMatches, `snap "some-snap" has no configuration`)
}

func (s *transactionSuite) TestState(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Check(tr.State(), DeepEquals, s.state)
}

func (s *transactionSuite) TestPristineIsNotTainted(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	tr := config.NewTransaction(s.state)
	c.Check(tr.Set("test-snap", "foo.a.a", "a"), IsNil)
	tr.Commit()

	var data interface{}
	var result interface{}
	tr = config.NewTransaction(s.state)
	c.Check(tr.Set("test-snap", "foo.b", "b"), IsNil)
	c.Check(tr.Set("test-snap", "foo.a.a", "b"), IsNil)
	c.Assert(tr.Get("test-snap", "foo", &result), IsNil)
	c.Check(result, DeepEquals, map[string]interface{}{"a": map[string]interface{}{"a": "b"}, "b": "b"})

	pristine := tr.PristineConfig()
	c.Assert(json.Unmarshal([]byte(*pristine["test-snap"]["foo"]), &data), IsNil)
	c.Assert(data, DeepEquals, map[string]interface{}{"a": map[string]interface{}{"a": "a"}})
}

func (s *transactionSuite) TestPristineGet(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	// start with a pristine config
	s.state.Set("config", map[string]map[string]interface{}{
		"some-snap": {"opt1": "pristine-value"},
	})

	// change the config
	var res interface{}
	tr := config.NewTransaction(s.state)
	err := tr.Set("some-snap", "opt1", "changed-value")
	c.Assert(err, IsNil)

	// and get will get the changed value
	err = tr.Get("some-snap", "opt1", &res)
	c.Assert(err, IsNil)
	c.Assert(res, Equals, "changed-value")

	// but GetPristine will get the pristine value
	err = tr.GetPristine("some-snap", "opt1", &res)
	c.Assert(err, IsNil)
	c.Assert(res, Equals, "pristine-value")

	// and GetPristine errors for options that don't exist in pristine
	var res2 interface{}
	err = tr.Set("some-snap", "opt2", "other-value")
	c.Assert(err, IsNil)
	err = tr.GetPristine("some-snap", "opt2", &res2)
	c.Assert(err, ErrorMatches, `snap "some-snap" has no "opt2" configuration option`)
	// but GetPristineMaybe does not error but also give no value
	err = tr.GetPristineMaybe("some-snap", "opt2", &res2)
	c.Assert(err, IsNil)
	c.Assert(res2, IsNil)
	// but regular get works
	err = tr.Get("some-snap", "opt2", &res2)
	c.Assert(err, IsNil)
	c.Assert(res2, Equals, "other-value")
}
