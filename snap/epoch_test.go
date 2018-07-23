// -*- Mode: Go; indent-tabs-mode: t -*-

/*
 * Copyright (C) 2017 Canonical Ltd
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

package snap_test

import (
	"encoding/json"
	"github.com/snapcore/snapd/snap"

	"gopkg.in/check.v1"
	"gopkg.in/yaml.v2"
)

type epochSuite struct{}

var _ = check.Suite(&epochSuite{})

var (
	// some duplication here maybe
	epochZeroStar                 = `0\* is an invalid epoch`
	hugeEpochNumber               = `epoch numbers must be less than 2³², but got .*`
	badEpochNumber                = `epoch numbers must be base 10 with no zero padding, but got .*`
	badEpochList                  = "epoch read/write attributes must be lists of epoch numbers"
	emptyEpochList                = "epoch list cannot be explicitly empty"
	epochListNotSorted            = "epoch list must be in ascending order"
	epochListJustRidiculouslyLong = "epoch list must not have more than 10 entries"
	noEpochIntersection           = "epoch read and write lists must have a non-empty intersection"
)

func (s epochSuite) TestBadEpochs(c *check.C) {
	type Tt struct {
		s string
		e string
		y int
	}

	tests := []Tt{
		{s: `"rubbish"`, e: badEpochNumber},                        // SISO
		{s: `0xA`, e: badEpochNumber, y: 1},                        // no hex
		{s: `"0xA"`, e: badEpochNumber},                            //
		{s: `001`, e: badEpochNumber, y: 1},                        // no octal, in fact no zero prefixes at all
		{s: `"001"`, e: badEpochNumber},                            //
		{s: `{"read": 5}`, e: badEpochList},                        // when split, must be list
		{s: `{"write": 5}`, e: badEpochList},                       //
		{s: `{"read": "5"}`, e: badEpochList},                      //
		{s: `{"write": "5"}`, e: badEpochList},                     //
		{s: `{"read": "1*"}`, e: badEpochList},                     // what
		{s: `{"read": [-1]}`, e: badEpochNumber},                   // negative not allowed
		{s: `{"write": [-1]}`, e: badEpochNumber},                  //
		{s: `{"read": ["-1"]}`, e: badEpochNumber},                 //
		{s: `{"write": ["-1"]}`, e: badEpochNumber},                //
		{s: `{"read": ["yes"]}`, e: badEpochNumber},                // must be numbers
		{s: `{"write": ["yes"]}`, e: badEpochNumber},               //
		{s: `{"read": ["Ⅰ","Ⅱ"]}`, e: badEpochNumber},              // not roman numerals you idiot
		{s: `{"read": [0xA]}`, e: badEpochNumber, y: 1},            //
		{s: `{"read": [010]}`, e: badEpochNumber, y: 1},            //
		{s: `{"read": [9999999999]}`, e: hugeEpochNumber},          // you done yet?
		{s: `"0*"`, e: epochZeroStar},                              // 0* means nothing
		{s: `"42**"`, e: badEpochNumber},                           // N** is dead
		{s: `{"read": []}`, e: emptyEpochList},                     // explicitly empty is bad
		{s: `{"write": []}`, e: emptyEpochList},                    //
		{s: `{"read": [1,2,4,3]}`, e: epochListNotSorted},          // must be ordered
		{s: `{"write": [4,3,2,1]}`, e: epochListNotSorted},         // ...in ascending order
		{s: `{"read": [0], "write": [1]}`, e: noEpochIntersection}, // must have at least one in common
		{s: `{"read": [0,1,2,3,4,5,6,7,8,9,10],
 "write": [0,1,2,3,4,5,6,7,8,9,10]}`, e: epochListJustRidiculouslyLong}, // must have <10 elements
	}

	for _, test := range tests {
		var v snap.Epoch
		err := yaml.Unmarshal([]byte(test.s), &v)
		c.Check(err, check.ErrorMatches, test.e, check.Commentf("YAML: %#q", test.s))

		if test.y == 1 {
			continue
		}
		err = json.Unmarshal([]byte(test.s), &v)
		c.Check(err, check.ErrorMatches, test.e, check.Commentf("JSON: %#q", test.s))
	}
}

func (s epochSuite) TestGoodEpochs(c *check.C) {
	type Tt struct {
		s string
		e snap.Epoch
		y int
	}

	tests := []Tt{
		{s: `0`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}, y: 1},
		{s: `""`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `"0"`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `{}`, e: snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: `"2*"`, e: snap.Epoch{Read: []uint32{1, 2}, Write: []uint32{2}}},
		{s: `{"read": [2]}`, e: snap.Epoch{Read: []uint32{2}, Write: []uint32{2}}},
		{s: `{"read": [1, 2]}`, e: snap.Epoch{Read: []uint32{1, 2}, Write: []uint32{2}}},
		{s: `{"write": [2]}`, e: snap.Epoch{Read: []uint32{2}, Write: []uint32{2}}},
		{s: `{"write": [1, 2]}`, e: snap.Epoch{Read: []uint32{1, 2}, Write: []uint32{1, 2}}},
		{s: `{"read": [2,4,8], "write": [2,3,5]}`, e: snap.Epoch{Read: []uint32{2, 4, 8}, Write: []uint32{2, 3, 5}}},
	}

	for _, test := range tests {
		var v snap.Epoch
		err := yaml.Unmarshal([]byte(test.s), &v)
		c.Check(err, check.IsNil, check.Commentf("YAML: %s", test.s))
		c.Check(v, check.DeepEquals, test.e)

		if test.y > 0 {
			continue
		}

		err = json.Unmarshal([]byte(test.s), &v)
		c.Check(err, check.IsNil, check.Commentf("JSON: %s", test.s))
		c.Check(v, check.DeepEquals, test.e)
	}
}

func (s *epochSuite) TestEpochValidate(c *check.C) {
	validEpochs := []*snap.Epoch{
		nil,
		{},
		{Read: []uint32{0}, Write: []uint32{0}},
		{Read: []uint32{0, 1}, Write: []uint32{1}},
		{Read: []uint32{1}, Write: []uint32{1}},
		{Read: []uint32{399, 400}, Write: []uint32{400}},
		{Read: []uint32{1, 2, 3}, Write: []uint32{1, 2, 3}},
	}
	for _, epoch := range validEpochs {
		err := epoch.Validate()
		c.Check(err, check.IsNil, check.Commentf("%s", epoch))
	}
	invalidEpochs := []struct {
		epoch *snap.Epoch
		err   string
	}{
		{epoch: &snap.Epoch{Read: []uint32{}}, err: emptyEpochList},
		{epoch: &snap.Epoch{Write: []uint32{}}, err: emptyEpochList},
		{epoch: &snap.Epoch{Read: []uint32{}, Write: []uint32{}}, err: emptyEpochList},
		{epoch: &snap.Epoch{Read: []uint32{1}, Write: []uint32{2}}, err: noEpochIntersection},
		{epoch: &snap.Epoch{Read: []uint32{1, 3, 5}, Write: []uint32{2, 4, 6}}, err: noEpochIntersection},
		{epoch: &snap.Epoch{Read: []uint32{1, 2, 3}, Write: []uint32{3, 2, 1}}, err: epochListNotSorted},
		{epoch: &snap.Epoch{Read: []uint32{3, 2, 1}, Write: []uint32{1, 2, 3}}, err: epochListNotSorted},
		{epoch: &snap.Epoch{Read: []uint32{3, 2, 1}, Write: []uint32{3, 2, 1}}, err: epochListNotSorted},
		{epoch: &snap.Epoch{
			Read:  []uint32{0},
			Write: []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		}, err: epochListJustRidiculouslyLong},
		{epoch: &snap.Epoch{
			Read:  []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			Write: []uint32{0},
		}, err: epochListJustRidiculouslyLong},
		{epoch: &snap.Epoch{
			Read:  []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
			Write: []uint32{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10},
		}, err: epochListJustRidiculouslyLong},
	}
	for _, test := range invalidEpochs {
		err := test.epoch.Validate()
		c.Check(err, check.ErrorMatches, test.err, check.Commentf("%s", test.epoch))
	}
}

func (s *epochSuite) TestEpochString(c *check.C) {
	tests := []struct {
		e *snap.Epoch
		s string
	}{
		{e: nil, s: "0"},
		{e: &snap.Epoch{}, s: "0"},
		{e: &snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}, s: "0"},
		{e: &snap.Epoch{Read: []uint32{0, 1}, Write: []uint32{1}}, s: "1*"},
		{e: &snap.Epoch{Read: []uint32{1}, Write: []uint32{1}}, s: "1"},
		{e: &snap.Epoch{Read: []uint32{399, 400}, Write: []uint32{400}}, s: "400*"},
		{e: &snap.Epoch{Read: []uint32{1, 2, 3}, Write: []uint32{1, 2, 3}}, s: `{"read":[1,2,3],"write":[1,2,3]}`},
	}
	for _, test := range tests {
		c.Check(test.e.String(), check.Equals, test.s, check.Commentf(test.s))
	}
}

func (s *epochSuite) TestEpochMarshal(c *check.C) {
	tests := []struct {
		e *snap.Epoch
		s string
	}{
		//		{e: nil, s: `"0"`},
		{e: &snap.Epoch{}, s: `"0"`},
		{e: &snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}, s: `"0"`},
		{e: &snap.Epoch{Read: []uint32{0, 1}, Write: []uint32{1}}, s: `"1*"`},
		{e: &snap.Epoch{Read: []uint32{1}, Write: []uint32{1}}, s: `"1"`},
		{e: &snap.Epoch{Read: []uint32{399, 400}, Write: []uint32{400}}, s: `"400*"`},
		{e: &snap.Epoch{Read: []uint32{1, 2, 3}, Write: []uint32{1, 2, 3}}, s: `{"read":[1,2,3],"write":[1,2,3]}`},
	}
	for _, test := range tests {
		bs, err := json.Marshal(test.e)
		c.Assert(err, check.IsNil)
		c.Check(string(bs), check.Equals, test.s, check.Commentf(test.s))
	}
}

func (s *epochSuite) TestE(c *check.C) {
	tests := []struct {
		e *snap.Epoch
		s string
	}{
		{s: "0", e: &snap.Epoch{Read: []uint32{0}, Write: []uint32{0}}},
		{s: "1", e: &snap.Epoch{Read: []uint32{1}, Write: []uint32{1}}},
		{s: "1*", e: &snap.Epoch{Read: []uint32{0, 1}, Write: []uint32{1}}},
		{s: "400*", e: &snap.Epoch{Read: []uint32{399, 400}, Write: []uint32{400}}},
	}
	for _, test := range tests {
		c.Check(snap.E(test.s), check.DeepEquals, test.e, check.Commentf(test.s))
		c.Check(test.e.String(), check.Equals, test.s, check.Commentf(test.s))
	}
}
