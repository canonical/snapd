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
	"encoding/json"
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate/config"
	"github.com/snapcore/snapd/overlord/state"
	"strings"
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

func (op setGetOp) args() map[string]interface{} {
	m := make(map[string]interface{})
	args := strings.Fields(string(op))
	for _, pair := range args[1:] {
		if pair == "=>" {
			break
		}
		kv := strings.SplitN(pair, "=", 2)
		var v interface{}
		err := json.Unmarshal([]byte(kv[1]), &v)
		if err != nil {
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
	`set one=1 two=2`,
	`setunder three=3`,
	`get one=1 two=2 three=-`,
	`getunder one=- two=- three=3`,
	`commit`,
	`getunder one=1 two=2 three=3`,
	`get one=1 two=2 three=3`,
	`set two=22 four=4`,
	`get one=1 two=22 three=3 four=4`,
	`getunder one=1 two=2 three=3 four=-`,
	`commit`,
	`getunder one=1 two=22 three=3 four=4`,
}, {
	// Trivial full doc.
	`set doc={"one":1,"two":2}`,
	`get doc={"one":1,"two":2}`,
}, {
	// Nested mutations.
	`set one.two.three=3`,
	`set one.five=5`,
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
	// Replacement with nested mutation.
	`set one={"two":{"three":3}}`,
	`set one.five=5`,
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
				var config map[string]map[string]interface{}
				s.state.Get("config", &config)
				for k, expected := range op.args() {
					obtained, ok := config[snap][k]
					if expected == "-" {
						if ok {
							c.Fatalf("Expected %q key to not exist, but it has value %v", k, obtained)
						}
						continue
					}
					c.Assert(obtained, DeepEquals, expected)
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
