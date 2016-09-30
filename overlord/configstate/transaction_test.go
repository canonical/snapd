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

package configstate_test

import (
	"encoding/json"
	"fmt"
	"testing"

	. "gopkg.in/check.v1"

	"github.com/snapcore/snapd/overlord/configstate"
	"github.com/snapcore/snapd/overlord/state"
	"strings"
)

func TestConfigState(t *testing.T) { TestingT(t) }

type transactionSuite struct {
	state       *state.State
	transaction *configstate.Transaction
}

var _ = Suite(&transactionSuite{})

func (s *transactionSuite) SetUpTest(c *C) {
	s.state = state.New(nil)
	s.state.Lock()
	defer s.state.Unlock()
	s.transaction = configstate.NewTransaction(s.state)
}

type setGetOp string

func (op setGetOp) kind() string {
	return strings.Fields(string(op))[0]
}

func (op setGetOp) args() map[string]interface{} {
	m := make(map[string]interface{})
	args := strings.Fields(string(op))
	for _, pair := range args[1:] {
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
}}

func (s *transactionSuite) TestSetGet(c *C) {
	s.state.Lock()
	defer s.state.Unlock()

	for _, test := range setGetTests {
		c.Logf("-----")
		t := configstate.NewTransaction(s.state)
		snap := "core"
		for _, op := range test {
			c.Logf("%s", op)
			switch op.kind() {
			case "set":
				for k, v := range op.args() {
					err := t.Set(snap, k, v)
					c.Assert(err, IsNil)
				}

			case "get":
				for k, expected := range op.args() {
					var obtained interface{}
					err := t.Get(snap, k, &obtained)
					if expected == "-" {
						if !configstate.IsNoOption(err) {
							c.Fatalf("Expected %q key to not exist, but it has value %v", k, obtained)
						}
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

	transaction := configstate.NewTransaction(s.state)
	c.Check(transaction.Set("test-snap", "foo", "break"), IsNil)

	// Pristine state is good, value in the transaction breaks.
	broken := brokenType{`"break"`}
	err := transaction.Get("test-snap", "foo", &broken)
	c.Assert(err, ErrorMatches, ".*BAM!.*")

	// Pristine state breaks, nothing in the transaction.
	transaction.Commit()
	err = transaction.Get("test-snap", "foo", &broken)
	c.Assert(err, ErrorMatches, ".*BAM!.*")
}
